package auth

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

// Roles. Kept as constants rather than bare strings so a typo is a compile error
// in most places rather than a silent authorization hole.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

var (
	ErrUserNotFound  = errors.New("user not found")
	ErrEmailTaken    = errors.New("email already registered")
	ErrInvalidEmail  = errors.New("invalid email address")
	ErrBadCredential = errors.New("invalid email or password")
)

// User is an account.
//
// PasswordHash is deliberately part of the struct but must never be serialised
// to JSON — there is no `json:` tag on it and no handler returns a User directly.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

// NormalizeEmail validates an address and returns its canonical form.
//
// Lowercasing matters: without it "Bob@x.com" and "bob@x.com" are two accounts,
// and the UNIQUE index would not stop it.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 320 {
		return "", ErrInvalidEmail
	}

	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", ErrInvalidEmail
	}

	return strings.ToLower(addr.Address), nil
}
