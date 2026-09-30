package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JP0722/shortly/internal/auth"
	"github.com/JP0722/shortly/internal/config"
	"github.com/JP0722/shortly/internal/httpapi"
	"github.com/JP0722/shortly/internal/logging"
	"github.com/JP0722/shortly/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := logging.New(cfg.LogLevel, cfg.Env)

	// ctx becomes "done" when someone presses Ctrl-C or the platform sends
	// SIGTERM (which is how Kubernetes asks a pod to stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Connect to MySQL before we start serving, so a bad database configuration
	// is an immediate startup failure rather than a 500 on the first request.
	// The timeout is a child of ctx, so Ctrl-C during a slow connect still works.
	dbCtx, cancelDB := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDB()

	db, err := store.Open(dbCtx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("connect to mysql: %w", err)
	}
	defer db.Close()

	logger.Info("connected to mysql")

	// Run migrations at startup so a deploy is just "ship the new binary".
	// The GET_LOCK inside keeps two instances from racing during a rolling deploy.
	if err := store.Migrate(dbCtx, db, logger); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// In dev, generate a throwaway signing key so there is nothing to set up.
	// The cost is that every restart invalidates every issued token. In prod,
	// config.Load has already refused to start without a real key.
	jwtKey := cfg.JWTPrivateKey
	if jwtKey == "" {
		jwtKey, err = auth.GenerateKeySeed()
		if err != nil {
			return fmt.Errorf("generate ephemeral signing key: %w", err)
		}
		logger.Warn("SHORTLY_JWT_PRIVATE_KEY is unset — using an ephemeral key; " +
			"all tokens become invalid on restart. Generate one with: go run ./cmd/genkey")
	}

	tokens, err := auth.NewTokenIssuer(jwtKey, cfg.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("build token issuer: %w", err)
	}

	// Build the server ourselves instead of calling http.ListenAndServe, for two
	// reasons: we need the timeouts below, and we need to hold onto srv so we
	// can call srv.Shutdown later.
	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewHandler(httpapi.Options{
			Store:         store.New(db),
			Tokens:        tokens,
			Logger:        logger,
			BaseURL:       cfg.BaseURL,
			RefreshTTL:    cfg.RefreshTokenTTL,
			SecureCookies: cfg.Env == "prod",
		}),

		// Without these there are no deadlines at all, so a client that opens a
		// connection and sends its headers one byte at a time holds it forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ListenAndServe blocks forever, so run it in a goroutine and have it report
	// back on a channel. The buffer size of 1 means the goroutine can send and
	// exit even if nobody is reading yet.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		serverErr <- srv.ListenAndServe()
	}()

	// Wait for whichever comes first: the server falling over, or a shutdown signal.
	select {
	case err := <-serverErr:
		// ErrServerClosed means a clean shutdown, not a failure.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server: %w", err)

	case <-ctx.Done():
		logger.Info("shutdown signal received", "timeout", cfg.ShutdownTimeout)
	}

	// A FRESH context, not a child of ctx. ctx is already cancelled — that is why
	// we are here — so a child of it would expire instantly and shut down hard.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Stops accepting new connections, then waits for in-flight requests.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("forced shutdown after %s: %w", cfg.ShutdownTimeout, err)
	}

	logger.Info("shutdown complete")
	return nil
}
