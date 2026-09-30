// Package httpapi holds the HTTP layer: routing, request decoding, and turning
// domain results into responses.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JP0722/shortly/internal/auth"
	"github.com/JP0722/shortly/internal/store"
	"github.com/JP0722/shortly/internal/web"
)

// API carries everything the handlers need.
//
// Handlers are methods on this struct rather than plain functions, because they
// now need a database, a logger and the base URL. The alternative — package
// level variables — would make the dependencies invisible and the handlers
// impossible to test in isolation.
type API struct {
	store         *store.Store
	tokens        *auth.TokenIssuer
	logger        *slog.Logger
	baseURL       string
	refreshTTL    time.Duration
	secureCookies bool
}

// Options groups the handler's dependencies.
//
// Once a constructor reaches five or six positional parameters, call sites stop
// being readable and a wrong-order mistake compiles fine. A struct makes each
// value named at the call site.
type Options struct {
	Store         *store.Store
	Tokens        *auth.TokenIssuer
	Logger        *slog.Logger
	BaseURL       string
	RefreshTTL    time.Duration
	SecureCookies bool
}

// NewHandler builds the router and wraps it in middleware.
func NewHandler(opts Options) http.Handler {
	api := &API{
		store:  opts.Store,
		tokens: opts.Tokens,
		logger: opts.Logger,
		// Trim once here so we never produce "http://host//abc".
		baseURL:       strings.TrimRight(opts.BaseURL, "/"),
		refreshTTL:    opts.RefreshTTL,
		secureCookies: opts.SecureCookies,
	}

	mux := http.NewServeMux()

	// The browser UI. {$} matches ONLY the exact path "/" — without it, the
	// pattern would also swallow every other path as a prefix.
	mux.HandleFunc("GET /{$}", web.Index())
	mux.Handle("GET /static/", web.Static())

	// healthz: "the process is alive". Used by a restart policy — it must not
	// depend on anything external, or a database blip restarts every instance.
	mux.HandleFunc("GET /healthz", handleHealthz)

	// readyz: "this instance can serve traffic". Checks the database, so a load
	// balancer stops sending requests here while MySQL is unreachable.
	mux.HandleFunc("GET /readyz", api.handleReadyz)
	mux.HandleFunc("POST /v1/auth/register", api.handleRegister)
	mux.HandleFunc("POST /v1/auth/login", api.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", api.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout", api.handleLogout)

	// Creating a link works without a token; it just has no owner then.
	// optionalAuth attaches the caller when there IS a token, so the link gets
	// attributed to them.
	mux.HandleFunc("POST /v1/links", api.optionalAuth(api.handleCreateLink))

	// These require a token: they are about "my" links.
	mux.HandleFunc("GET /v1/links", api.requireAuth(api.handleListLinks))
	mux.HandleFunc("DELETE /v1/links/{code}", api.requireAuth(api.handleDeleteLink))

	// {code} is a wildcard: it matches exactly one path segment and the value is
	// read back with r.PathValue("code"). The literal /healthz above still wins
	// for that path — ServeMux prefers the more specific pattern.
	mux.HandleFunc("GET /{code}", api.handleRedirect)

	// Order is outermost first, and it matters:
	//
	//   RequestID  must be first, so everything below can log the ID.
	//   AccessLog  must be outside Recover, otherwise a panicking request is
	//              logged before Recover writes the 500 and the line says 200.
	//   Recover    innermost, closest to the handler that might panic.
	return chain(mux,
		RequestID,
		AccessLog(opts.Logger),
		Recover(opts.Logger),
	)
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (a *API) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := a.store.Ping(ctx); err != nil {
		a.logger.Warn("readiness check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not ready",
			"reason": "database unreachable",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// writeJSON sends v as a JSON response.
//
// Header().Set must come before WriteHeader: once the status code is written the
// headers are already on the wire and later changes are silently ignored.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError sends a JSON error with a consistent shape.
//
// The message here is always something we chose. Internal error text never
// reaches the client: it leaks table names, file paths and query structure.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
