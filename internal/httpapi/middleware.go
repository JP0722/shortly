package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// A middleware wraps a handler and returns a new one. This shape —
// func(http.Handler) http.Handler — is the whole convention. Rails' before_action
// is a framework feature; here it is just a function, and you can see exactly
// where it sits in the chain.
type middleware func(http.Handler) http.Handler

// chain wraps h in the given middleware, outermost first.
//
// chain(h, A, B, C) produces A(B(C(h))): a request passes through A, then B,
// then C, then the handler, and responses unwind back out in reverse.
func chain(h http.Handler, mw ...middleware) http.Handler {
	// Apply in reverse so the first argument ends up outermost.
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// contextKey is unexported and has its own type so that a key defined here can
// never collide with a key defined in another package — even one using the same
// underlying string. Using a bare string as a context key is a real bug source.
type contextKey int

const requestIDKey contextKey = 0

// RequestIDFrom returns the request ID stored in ctx, or "" if there is none.
func RequestIDFrom(ctx context.Context) string {
	// The comma-ok form: the second value reports whether the assertion held.
	id, ok := ctx.Value(requestIDKey).(string)
	if !ok {
		return ""
	}
	return id
}

// RequestID gives every request an identifier, puts it in the request context,
// and echoes it in a response header so a user can quote it in a bug report.
func RequestID(next http.Handler) http.Handler {
	// http.HandlerFunc converts a plain function into an http.Handler by giving
	// it a ServeHTTP method. It is a type conversion, not a function call.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()

		w.Header().Set("X-Request-ID", id)

		// Contexts are immutable: WithValue returns a NEW context, so we must
		// build a new request carrying it.
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// statusRecorder remembers the status code on its way past.
//
// http.ResponseWriter has no way to read back what was written, so logging the
// status means intercepting it. The embedded interface means statusRecorder
// automatically has every ResponseWriter method; we only override WriteHeader.
// That is Go's composition in place of inheritance.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// AccessLog logs one line per request once it has completed.
//
// It takes a logger and RETURNS a middleware, because the middleware signature
// has no room for extra arguments. A function that builds a function.
func AccessLog(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Default to 200: a handler that writes a body without calling
			// WriteHeader has implicitly sent 200.
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFrom(r.Context()),
			)
		})
	}
}

// Recover stops a panic in a handler from killing the whole process.
//
// Without this, one nil map write in one handler takes down the server for
// every user. net/http does recover panics per connection, but it logs them
// unhelpfully and the client gets a dropped connection rather than a 500.
func Recover(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A deferred function still runs while a panic unwinds the stack,
			// and recover() stops the unwinding.
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic in handler",
						"panic", rec,
						"method", r.Method,
						"path", r.URL.Path,
						"request_id", RequestIDFrom(r.Context()),
					)
					writeError(w, http.StatusInternalServerError, "internal error")
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
