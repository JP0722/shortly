package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrRefreshNotFound = errors.New("refresh token not found")
	ErrRefreshInvalid  = errors.New("refresh token is expired, used or revoked")
)

// RefreshToken is the stored half of a refresh credential. The plaintext token
// exists only in the user's cookie; we keep a hash, exactly as with passwords.
type RefreshToken struct {
	ID        int64
	UserID    int64
	FamilyID  string
	ExpiresAt time.Time
	UsedAt    *time.Time // nil until exchanged
	RevokedAt *time.Time // nil unless the family was revoked
}

func (r RefreshToken) Used() bool    { return r.UsedAt != nil }
func (r RefreshToken) Revoked() bool { return r.RevokedAt != nil }
func (r RefreshToken) Expired() bool { return time.Now().After(r.ExpiresAt) }

// Usable reports whether this token may be exchanged right now.
func (r RefreshToken) Usable() bool {
	return !r.Used() && !r.Revoked() && !r.Expired()
}

// NewRefreshToken returns a new random token and its hash.
//
// The caller stores the hash and gives the plaintext to the browser. We can
// never show it again, and a database dump does not yield usable tokens.
func NewRefreshToken() (plaintext, hash string, err error) {
	raw := make([]byte, 32) // 256 bits
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}

	plaintext = base64.RawURLEncoding.EncodeToString(raw)
	return plaintext, HashRefreshToken(plaintext), nil
}

// HashRefreshToken hashes a refresh token for storage and lookup.
//
// SHA-256, not argon2. Argon2 is slow and memory-hard on purpose, to defend
// human-chosen passwords against brute force. A refresh token is 256 bits of
// randomness — there is nothing to guess — so a fast hash is correct, and it
// also means we can look the token up by its hash with an indexed query rather
// than hashing every row.
func HashRefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// NewFamilyID returns an identifier shared by every token descended from one
// login, so the whole chain can be revoked together.
func NewFamilyID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}
