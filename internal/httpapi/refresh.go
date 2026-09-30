package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/JP0722/shortly/internal/auth"
)

const (
	refreshCookieName = "shortly_refresh"

	// The cookie is only ever sent to the auth endpoints, so it does not ride
	// along on every request to the rest of the API.
	refreshCookiePath = "/v1/auth"
)

// setRefreshCookie issues a fresh login session and writes the cookie.
func (a *API) setRefreshCookie(ctx context.Context, w http.ResponseWriter, userID int64, familyID string) error {
	plaintext, hash, err := auth.NewRefreshToken()
	if err != nil {
		return err
	}

	expires := time.Now().Add(a.refreshTTL)
	if err := a.store.CreateRefreshToken(ctx, userID, familyID, hash, expires); err != nil {
		return err
	}

	a.writeRefreshCookie(w, plaintext, expires)
	return nil
}

func (a *API) writeRefreshCookie(w http.ResponseWriter, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:  refreshCookieName,
		Value: value,
		Path:  refreshCookiePath,

		// HttpOnly: JavaScript cannot read it, so an XSS bug cannot steal the
		// long-lived credential. This is why the refresh token lives in a cookie
		// while the access token stays in memory.
		HttpOnly: true,

		// Secure: HTTPS only. Off in dev because localhost is plain HTTP —
		// which is exactly the kind of thing that must not leak into prod.
		Secure: a.secureCookies,

		// Lax: sent on top-level navigation but not on cross-site POSTs, which
		// blocks the basic CSRF shape.
		SameSite: http.SameSiteLaxMode,

		Expires: expires,
		MaxAge:  int(time.Until(expires).Seconds()),
	})
}

func (a *API) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   a.secureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1, // delete now
	})
}

// handleRefresh handles POST /v1/auth/refresh.
//
// Exchanges a refresh token for a new access token AND a new refresh token.
// The old one is marked used and can never be exchanged again.
func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	const failure = "invalid or expired session"

	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	stored, err := a.store.RefreshTokenByHash(r.Context(), auth.HashRefreshToken(cookie.Value))
	if err != nil {
		if errors.Is(err, auth.ErrRefreshNotFound) {
			a.clearRefreshCookie(w)
			writeError(w, http.StatusUnauthorized, failure)
			return
		}
		a.logger.Error("lookup refresh token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not refresh session")
		return
	}

	// REUSE DETECTION.
	//
	// This token was already exchanged once, yet here it is again. Either the
	// legitimate user replayed an old cookie, or someone stole it — and we
	// cannot tell which. So we revoke every token in the family and force a
	// fresh login. The thief gets nothing, and the real user finds out that
	// something happened.
	//
	// This is what makes short-lived access tokens worth the trouble: without
	// it, a stolen refresh token grants indefinite access, silently.
	if stored.Used() {
		a.logger.Warn("refresh token reuse detected — revoking family",
			"user_id", stored.UserID,
			"family_id", stored.FamilyID,
		)
		if err := a.store.RevokeRefreshFamily(r.Context(), stored.FamilyID); err != nil {
			a.logger.Error("revoke refresh family failed", "error", err)
		}
		a.clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	if !stored.Usable() {
		a.clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	user, err := a.store.UserByID(r.Context(), stored.UserID)
	if err != nil {
		a.clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	// Rotate: the replacement stays in the same family, so a later reuse of any
	// token in the chain still revokes everything.
	plaintext, newHash, err := auth.NewRefreshToken()
	if err != nil {
		a.logger.Error("generate refresh token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not refresh session")
		return
	}

	expires := time.Now().Add(a.refreshTTL)
	err = a.store.RotateRefreshToken(r.Context(), stored.ID, stored.UserID, stored.FamilyID, newHash, expires)
	if err != nil {
		if errors.Is(err, auth.ErrRefreshInvalid) {
			// Lost a race with a concurrent refresh.
			writeError(w, http.StatusUnauthorized, failure)
			return
		}
		a.logger.Error("rotate refresh token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not refresh session")
		return
	}

	accessToken, accessExpires, err := a.tokens.Issue(user)
	if err != nil {
		a.logger.Error("issue token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not refresh session")
		return
	}

	a.writeRefreshCookie(w, plaintext, expires)

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(accessExpires).Seconds()),
	})
}

// handleLogout handles POST /v1/auth/logout.
//
// Revokes the whole family, not just the presented token: logging out on one
// device should not leave a chain alive that could still be refreshed.
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(refreshCookieName); err == nil && cookie.Value != "" {
		stored, err := a.store.RefreshTokenByHash(r.Context(), auth.HashRefreshToken(cookie.Value))
		if err == nil {
			if err := a.store.RevokeRefreshFamily(r.Context(), stored.FamilyID); err != nil {
				a.logger.Error("revoke refresh family failed", "error", err)
			}
		}
	}

	a.clearRefreshCookie(w)

	// Always 204, even if there was no session. Logout must not report whether
	// a token was valid.
	w.WriteHeader(http.StatusNoContent)
}
