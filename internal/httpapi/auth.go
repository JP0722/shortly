package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/JP0722/shortly/internal/auth"
)

const maxAuthBodyBytes = 4 << 10 // 4 KiB

type registerRequest struct {
	Email string `json:"email"`
	// No `json:"-"` tricks needed: this struct is only ever decoded FROM json,
	// never encoded to it, so the password cannot leak through a response.
	Password string `json:"password"`
}

type registerResponse struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleRegister handles POST /v1/auth/register.
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)

	var req registerRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"email": "...", "password": "..."}`)
		return
	}

	email, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid email address")
		return
	}

	// Hashing takes ~50ms and 64 MiB by design. That cost is the point, but it
	// also means an unauthenticated endpoint can be used to burn CPU and memory
	// — step 7 puts a rate limit in front of this.
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			writeError(w, http.StatusBadRequest, "password must be at least 12 characters")
			return
		}
		a.logger.Error("hash password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not register")
		return
	}

	created, err := a.store.CreateUser(r.Context(), auth.User{
		Email:        email,
		PasswordHash: hash,
		Role:         auth.RoleUser,
	})
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			// NOTE: this tells an attacker which emails are registered. Step 7
			// replaces it with a uniform response, which requires sending a
			// confirmation email to be usable. Leaving the honest version here
			// so the tradeoff is visible rather than hidden.
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		a.logger.Error("create user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not register")
		return
	}

	// The response deliberately carries no hash and no token: registering is not
	// logging in.
	writeJSON(w, http.StatusCreated, registerResponse{
		ID:    created.ID,
		Email: created.Email,
		Role:  created.Role,
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"` // seconds, as OAuth2 does it
}

// handleLogin handles POST /v1/auth/login.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)

	var req loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, `body must be {"email": "...", "password": "..."}`)
		return
	}

	// Every failure below returns exactly this: same status, same message.
	// Telling the caller "no such user" versus "wrong password" hands an
	// attacker a list of valid accounts for free.
	const failure = "invalid email or password"

	email, err := auth.NormalizeEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	user, err := a.store.UserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			// Spend the same ~50ms we would have spent hashing, so response
			// time does not reveal whether the account exists.
			auth.BurnPasswordTime(req.Password)
			writeError(w, http.StatusUnauthorized, failure)
			return
		}
		a.logger.Error("lookup user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	valid, err := auth.VerifyPassword(user.PasswordHash, req.Password)
	if err != nil {
		// A malformed stored hash is our bug, not the user's.
		a.logger.Error("stored password hash is invalid", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}
	if !valid {
		writeError(w, http.StatusUnauthorized, failure)
		return
	}

	token, expiresAt, err := a.tokens.Issue(user)
	if err != nil {
		a.logger.Error("issue token failed", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	// A new login starts a new token family.
	if err := a.setRefreshCookie(r.Context(), w, user.ID, auth.NewFamilyID()); err != nil {
		a.logger.Error("create refresh token failed", "error", err, "user_id", user.ID)
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	a.logger.Info("login succeeded", "user_id", user.ID)

	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(expiresAt).Seconds()),
	})
}
