// Package auth handles credentials: password hashing, and later tokens.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Cost parameters for argon2id.
//
// A password hash must be deliberately slow and memory-hungry. SHA-256 is the
// wrong tool because it is fast: a GPU computes billions per second, so a stolen
// database is cracked cheaply. argon2id is also memory-hard — each hash needs
// 64 MiB — which is what makes GPU and ASIC attacks expensive instead of free.
//
// Raising these later is safe: every hash stores the parameters it was created
// with, so old passwords still verify.
const (
	argonMemoryKiB = 64 * 1024 // 64 MiB
	argonTime      = 1         // passes over memory
	argonThreads   = 4
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// dummyHash is a real argon2id hash of a random password nobody knows.
//
// Login verifies against this when the account does not exist, so that a
// request for an unknown email costs the same ~50ms as one for a real account.
// Without it, response time alone tells an attacker which emails are registered.
const dummyHash = `$argon2id$v=19$m=65536,t=1,p=4$GXVzfWRjwYF6nEFgA28BTw$b5df/FMBUYxYRPT+ZsuPwuz0Fk+GS6roCxkGaXbzdCQ`

// BurnPasswordTime does the same work as VerifyPassword but always fails. Call
// it when the user was not found, to keep login timing uniform.
func BurnPasswordTime(plain string) {
	_, _ = VerifyPassword(dummyHash, plain)
}

var (
	ErrInvalidHash = errors.New("password hash is not valid PHC format")
	// ErrWeakPassword is returned by HashPassword, not by the HTTP layer, so the
	// rule lives in one place.
	ErrWeakPassword = errors.New("password must be at least 12 characters")
)

const minPasswordLen = 12

// HashPassword returns a PHC-format argon2id hash:
//
//	$argon2id$v=19$m=65536,t=1,p=4$<base64 salt>$<base64 hash>
//
// Everything needed to verify the password is inside the string, so nothing else
// has to be stored or kept in sync.
func HashPassword(plain string) (string, error) {
	if len(plain) < minPasswordLen {
		return "", ErrWeakPassword
	}

	// A fresh random salt per user. Without it, two people with the same
	// password get the same hash — so one cracked password cracks them all, and
	// precomputed rainbow tables work.
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plain), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)

	// RawStdEncoding = standard base64 without '=' padding, which is what the
	// PHC string format specifies.
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword reports whether plain matches the given PHC-format hash.
//
// It returns an error only when the stored hash is malformed — a wrong password
// is (false, nil), not an error.
func VerifyPassword(encoded, plain string) (bool, error) {
	memory, time, threads, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	// Recompute using the parameters this hash was created with, not today's.
	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))

	// ConstantTimeCompare always inspects every byte. A plain == returns as soon
	// as two bytes differ, and that timing difference leaks how much of the
	// value the attacker got right — enough to guess a secret byte by byte.
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func decodeHash(encoded string) (memory, time uint32, threads uint8, salt, hash []byte, err error) {
	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	// Leading $ means parts[0] is empty.
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("%w: unsupported argon2 version %d", ErrInvalidHash, version)
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}
	if hash, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return 0, 0, 0, nil, nil, ErrInvalidHash
	}

	return memory, time, threads, salt, hash, nil
}
