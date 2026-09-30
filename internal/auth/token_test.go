package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testIssuer(t *testing.T, ttl time.Duration) *TokenIssuer {
	t.Helper()

	seed, err := GenerateKeySeed()
	if err != nil {
		t.Fatalf("GenerateKeySeed() error = %v", err)
	}
	issuer, err := NewTokenIssuer(seed, ttl)
	if err != nil {
		t.Fatalf("NewTokenIssuer() error = %v", err)
	}
	return issuer
}

var testUser = User{ID: 42, Email: "bob@example.com", Role: RoleAdmin}

func TestIssueThenVerify(t *testing.T) {
	issuer := testIssuer(t, 15*time.Minute)

	token, expires, err := issuer.Issue(testUser)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !expires.After(time.Now()) {
		t.Error("token expires in the past")
	}

	principal, err := issuer.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if principal.UserID != testUser.ID {
		t.Errorf("UserID = %d, want %d", principal.UserID, testUser.ID)
	}
	if principal.Email != testUser.Email {
		t.Errorf("Email = %q, want %q", principal.Email, testUser.Email)
	}
	if !principal.IsAdmin() {
		t.Error("IsAdmin() = false, want true")
	}
}

// A JWT is signed, not encrypted. Anyone holding it can read the payload, so
// nothing secret may go in it. This test documents that fact.
func TestTokenPayloadIsReadableByAnyone(t *testing.T) {
	issuer := testIssuer(t, time.Minute)
	token, _, _ := issuer.Issue(testUser)

	payload := strings.Split(token, ".")[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if claims["email"] != testUser.Email {
		t.Errorf("email claim = %v, want %v", claims["email"], testUser.Email)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	// Negative TTL: already expired, and beyond the one minute of leeway.
	issuer := testIssuer(t, -2*time.Minute)

	token, _, err := issuer.Issue(testUser)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	_, err = issuer.Verify(token)
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify(expired) error = %v, want ErrExpiredToken", err)
	}
}

// THE alg=none ATTACK.
//
// A JWT declares its own algorithm in its header. A verifier that trusts that
// header can be handed a token with alg=none and no signature at all. Without
// jwt.WithValidMethods pinning EdDSA, this test would pass an unsigned,
// attacker-authored token as a valid admin.
func TestAlgNoneTokenIsRejected(t *testing.T) {
	issuer := testIssuer(t, 15*time.Minute)

	forged := jwt.NewWithClaims(jwt.SigningMethodNone, accessClaims{
		Email: "attacker@evil.com",
		Role:  RoleAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})

	// jwt.UnsafeAllowNoneSignatureType is the library making you say out loud
	// that you are creating an unsigned token.
	tokenString, err := forged.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("forge token: %v", err)
	}

	if _, err := issuer.Verify(tokenString); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("an alg=none token was accepted! error = %v", err)
	}
}

// A token signed by a different key must not verify — the obvious case, but
// worth pinning down.
func TestTokenFromAnotherKeyIsRejected(t *testing.T) {
	mine := testIssuer(t, 15*time.Minute)
	theirs := testIssuer(t, 15*time.Minute)

	token, _, _ := theirs.Issue(testUser)

	if _, err := mine.Verify(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("token signed by another key verified: err = %v", err)
	}
}

func TestTamperedTokenIsRejected(t *testing.T) {
	issuer := testIssuer(t, 15*time.Minute)
	token, _, _ := issuer.Issue(User{ID: 1, Email: "bob@example.com", Role: RoleUser})

	// Re-encode the payload with role=admin, keeping the original signature.
	parts := strings.Split(token, ".")
	decoded, _ := base64.RawURLEncoding.DecodeString(parts[1])

	var claims map[string]any
	json.Unmarshal(decoded, &claims)
	claims["role"] = RoleAdmin

	repacked, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(repacked)
	tampered := strings.Join(parts, ".")

	if _, err := issuer.Verify(tampered); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("privilege escalation succeeded! err = %v", err)
	}
}

func TestBadSeedIsRejected(t *testing.T) {
	for _, seed := range []string{"", "not base64!!", base64.RawStdEncoding.EncodeToString([]byte("too short"))} {
		if _, err := NewTokenIssuer(seed, time.Minute); err == nil {
			t.Errorf("NewTokenIssuer(%q) error = nil, want an error", seed)
		}
	}
}
