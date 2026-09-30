// Package config loads and validates application settings from the environment.
//
// It deliberately has no dependencies: reading environment variables is simple
// enough that a library would hide more than it saves, and configuration is the
// one place you most want to understand exactly what happened.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"
)

// Config holds every setting the application needs.
//
// It is a plain value, not a global. Load builds one at startup and it is
// passed down explicitly to whatever needs it. A package-level singleton would
// make tests order-dependent and hide which components actually depend on what.
//
// Note the field types: LogLevel and ShutdownTimeout are already parsed. Nothing
// downstream ever receives a string that still needs interpreting, so nothing
// downstream can fail to interpret it.
type Config struct {
	Env             string        // "dev" or "prod"
	HTTPAddr        string        // listen address for the HTTP server, e.g. ":8080"
	BaseURL         string        // public base URL used to build short links
	DatabaseDSN     string        // go-sql-driver/mysql DSN — note: not a URL
	RedisAddr       string        // host:port
	LogLevel        slog.Level    // debug | info | warn | error
	ShutdownTimeout time.Duration // how long to wait for in-flight requests on exit

	// Base64-encoded 32-byte Ed25519 seed used to sign access tokens.
	// Generate one with: go run ./cmd/genkey
	JWTPrivateKey   string
	AccessTokenTTL  time.Duration // how long an access token stays valid
	RefreshTokenTTL time.Duration // how long a refresh token stays valid
}

// Load reads SHORTLY_* environment variables, applies defaults, and validates
// the result.
//
// It returns an error rather than exiting. A package that calls log.Fatal
// cannot be tested and steals the decision from its caller; main is the only
// place that gets to decide what a configuration failure means.
//
// Every problem found is reported, not just the first one.
func Load() (Config, error) {
	var p parser

	cfg := Config{
		Env:             p.str("SHORTLY_ENV", "dev"),
		HTTPAddr:        p.str("SHORTLY_HTTP_ADDR", ":8080"),
		BaseURL:         p.str("SHORTLY_BASE_URL", "http://localhost:8080"),
		RedisAddr:       p.str("SHORTLY_REDIS_ADDR", "localhost:6383"),
		DatabaseDSN:     p.required("SHORTLY_DATABASE_DSN"),
		LogLevel:        p.level("SHORTLY_LOG_LEVEL", slog.LevelInfo),
		ShutdownTimeout: p.dur("SHORTLY_SHUTDOWN_TIMEOUT", 15*time.Second),
		JWTPrivateKey:   p.str("SHORTLY_JWT_PRIVATE_KEY", ""),

		// Short on purpose. An access token cannot be revoked, so its lifetime
		// is the window an attacker gets with a stolen one. Refresh tokens
		// (step 3) are the long-lived, revocable half.
		AccessTokenTTL: p.dur("SHORTLY_ACCESS_TOKEN_TTL", 15*time.Minute),

		// Long, because rotation and reuse detection (not expiry) are what keep
		// refresh tokens safe.
		RefreshTokenTTL: p.dur("SHORTLY_REFRESH_TOKEN_TTL", 30*24*time.Hour),
	}

	// errors.Join ignores nil arguments and returns nil when every argument is
	// nil, so this one line covers all four cases: parse errors only, validation
	// errors only, both, or a clean config.
	if err := errors.Join(p.err(), cfg.validate()); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate checks constraints that need the whole Config, not a single variable.
//
// The receiver is a value (c Config) because validate only reads. A pointer
// receiver is only needed when a method mutates its receiver — see parser below.
func (c Config) validate() error {
	var errs []error

	if c.Env != "dev" && c.Env != "prod" {
		errs = append(errs, fmt.Errorf("SHORTLY_ENV must be %q or %q, got %q", "dev", "prod", c.Env))
	}

	// url.Parse returns a nil *URL when err != nil. The || short-circuits, so
	// u.IsAbs() is never reached in that case.
	if u, err := url.Parse(c.BaseURL); err != nil || !u.IsAbs() {
		errs = append(errs, fmt.Errorf("SHORTLY_BASE_URL must be an absolute URL, got %q", c.BaseURL))
	}

	// In production a missing signing key is fatal. In development we generate an
	// ephemeral one at startup (see main), which is convenient but means every
	// restart invalidates every token.
	if c.Env == "prod" && c.JWTPrivateKey == "" {
		errs = append(errs, errors.New("SHORTLY_JWT_PRIVATE_KEY is required when SHORTLY_ENV=prod"))
	}

	return errors.Join(errs...)
}

// parser accumulates problems while reading environment variables instead of
// aborting on the first one, so a developer with three bad values learns about
// all three in a single run.
//
// Every method has a POINTER receiver (p *parser). With a value receiver each
// call would mutate its own copy, the appends would be silently discarded, and
// the code would still compile — a genuinely nasty class of bug.
type parser struct {
	errs []error
}

// fail records a problem. The parser always returns a usable zero/default value
// after calling this, so Load can keep going and collect the rest.
func (p *parser) fail(format string, args ...any) {
	// append returns a NEW slice; it must be assigned back.
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

// err returns all accumulated problems as one error, or nil if there were none.
func (p *parser) err() error {
	return errors.Join(p.errs...)
}

// str returns the value of key, or def if it is unset or empty.
//
// Treating empty as unset is a judgement call: SHORTLY_ENV="" almost always
// means "I forgot this", not "I want the empty string".
//
// This one cannot fail, so it never touches p.errs. It stays a method purely so
// that every line of the Config literal in Load reads the same way.
func (p *parser) str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// required returns the value of key, recording an error if it is missing.
func (p *parser) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		p.fail("%s is required", key)
	}
	return v
}

// dur parses a Go duration string such as "15s" or "2m".
func (p *parser) dur(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		p.fail("%s: %q is not a duration (want e.g. 15s, 2m)", key, raw)
		return def
	}
	if d <= 0 {
		p.fail("%s must be positive, got %s", key, d)
		return def
	}
	return d
}

// level parses a slog level name: debug, info, warn or error (case-insensitive).
func (p *parser) level(key string, def slog.Level) slog.Level {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}

	// slog.Level already knows how to parse itself. UnmarshalText has a pointer
	// receiver, so it can write the result back into lvl.
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		p.fail("%s: %q is not a log level (want debug, info, warn or error)", key, raw)
		return def
	}
	return lvl
}
