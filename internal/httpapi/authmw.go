package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/JP0722/shortly/internal/auth"
)

const principalKey contextKey = iota + 1

// PrincipalFrom returns the authenticated caller, if there is one.
func PrincipalFrom(ctx context.Context) (auth.Principal, bool) {
	p, ok := ctx.Value(principalKey).(auth.Principal)
	return p, ok
}

// optionalAuth attaches the caller's identity when a valid bearer token is
// present, and lets the request through unchanged when it is not.
//
// Needed because POST /v1/links works both ways: anonymous callers get an
// unowned link, signed-in callers get one attached to their account. Without
// this, a logged-in user's link would silently be created with no owner —
// it would work, and then never appear in their list.
func (a *API) optionalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if found && token != "" {
			// A bad token is ignored rather than rejected: the endpoint is
			// usable without one, so a broken token is no worse than none.
			if principal, err := a.tokens.Verify(token); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), principalKey, principal))
			}
		}
		next(w, r)
	}
}

// requireAuth wraps a handler so it only runs for an authenticated caller.
//
// This is AUTHENTICATION only — it answers "who are you?". Whether that person
// may touch a particular link is AUTHORIZATION, and it lives in the handler,
// because only the handler knows which resource is being touched.
func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")

		// Exactly "Bearer <token>". Anything else is rejected rather than
		// guessed at.
		token, found := strings.CutPrefix(header, "Bearer ")
		if !found || token == "" {
			// WWW-Authenticate tells a well-behaved client what scheme we want.
			w.Header().Set("WWW-Authenticate", `Bearer realm="shortly"`)
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}

		principal, err := a.tokens.Verify(token)
		if err != nil {
			// Distinguishing expiry is safe and useful: it tells the client to
			// refresh rather than send the user back to a login form.
			if errors.Is(err, auth.ErrExpiredToken) {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="expired"`)
				writeError(w, http.StatusUnauthorized, "access token expired")
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid access token")
			return
		}

		ctx := context.WithValue(r.Context(), principalKey, principal)
		next(w, r.WithContext(ctx))
	}
}
