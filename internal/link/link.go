// Package link holds the core idea of a shortened link: the entity, how codes
// are generated, and what counts as a valid target URL.
//
// It deliberately knows nothing about HTTP or databases, which is what makes it
// easy to test.
package link

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"time"
)

// Link is a single shortened URL.
type Link struct {
	Code      string
	TargetURL string
	CreatedAt time.Time

	// UserID is 0 for links created before ownership existed, or anonymously.
	UserID int64
}

// Sentinel errors. Callers match these with errors.Is rather than comparing
// error strings, and in Phase 1 step 6 they decide the HTTP status code.
var (
	ErrNotFound   = errors.New("link not found")
	ErrInvalidURL = errors.New("invalid url")

	// ErrCodeTaken means the generated code already exists. The caller should
	// generate a new one and try again — see NewCode.
	ErrCodeTaken = errors.New("code already taken")
)

const (
	alphabet   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength = 7
	maxURLLen  = 2048
)

// NewCode returns a random short code, e.g. "aB3xK9p".
//
// It uses crypto/rand rather than math/rand. math/rand produces a predictable
// sequence, so anyone who saw a handful of codes could generate the rest and
// enumerate every link in the database.
//
// Codes can still collide — 62^7 is large but finite — so the database has a
// unique index on the column and the caller retries. That is the real defence;
// the odds are only a reason it rarely happens.
func NewCode() string {
	b := make([]byte, codeLength)

	// Since Go 1.24 rand.Read never returns an error: it crashes the program
	// instead. Assigning to _ makes it clear that is a deliberate choice.
	_, _ = rand.Read(b)

	// Map each random byte onto a character in the alphabet.
	//
	// 256 is not a multiple of 62 (256 = 4*62 + 8), so the first 8 characters of
	// the alphabet come up 5 times in 256 while the rest come up 4 — making
	// a-h about 25% more likely than the others. This is modulo bias.
	//
	// It costs roughly 0.3 bits out of ~41.7 bits of entropy, so the codes are
	// still far too large to guess, and the fix (rejection sampling) is not worth
	// the complexity here. Worth knowing the term: in a key or token generator it
	// would matter.
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}

	return string(b)
}

// NormalizeURL checks that raw is a URL we are willing to redirect to, and
// returns its canonical form.
func NormalizeURL(raw string) (string, error) {
	if len(raw) > maxURLLen {
		return "", fmt.Errorf("%w: longer than %d characters", ErrInvalidURL, maxURLLen)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidURL, err)
	}

	// Allow-list the two schemes we want instead of blocking bad ones. Accepting
	// javascript: or data: here would turn the redirect into a cross-site
	// scripting vector.
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}

	// "https://" parses fine but has nowhere to go.
	if u.Host == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidURL)
	}

	return u.String(), nil
}
