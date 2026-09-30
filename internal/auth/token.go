package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Who issued the token and who it is for. Both are checked on every request:
	// a token minted by some other service, or meant for a different audience,
	// must not be accepted here.
	tokenIssuer   = "shortly"
	tokenAudience = "shortly-api"

	// EdDSA (Ed25519) rather than HS256.
	//
	// HS256 uses one shared secret for both signing and verifying, so anything
	// able to check a token can also mint one. EdDSA splits them: the private
	// key signs, and the public key — which can be handed to anyone — only
	// verifies.
	tokenAlgorithm = "EdDSA"

	// Real clocks disagree by a second or two. Without leeway, a token issued on
	// one machine can look "not yet valid" on another.
	clockSkewLeeway = time.Minute
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// Principal is who the caller is, as established by a verified token.
type Principal struct {
	UserID int64
	Email  string
	Role   string
}

func (p Principal) IsAdmin() bool { return p.Role == RoleAdmin }

// accessClaims is what goes inside the token.
//
// Keep it small: a JWT is sent on every request and anyone holding it can read
// the contents — it is signed, not encrypted. Never put secrets in here.
type accessClaims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer mints and verifies access tokens.
type TokenIssuer struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	ttl     time.Duration
}

// GenerateKeySeed returns a new base64 Ed25519 seed, for SHORTLY_JWT_PRIVATE_KEY.
func GenerateKeySeed() (string, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	return base64.RawStdEncoding.EncodeToString(private.Seed()), nil
}

// NewTokenIssuer builds an issuer from a base64-encoded 32-byte Ed25519 seed.
func NewTokenIssuer(seedBase64 string, ttl time.Duration) (*TokenIssuer, error) {
	seed, err := base64.RawStdEncoding.DecodeString(seedBase64)
	if err != nil {
		// Do not include the value in the error — it would end up in logs.
		return nil, errors.New("JWT private key is not valid base64")
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("JWT private key must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}

	private := ed25519.NewKeyFromSeed(seed)

	return &TokenIssuer{
		private: private,
		public:  private.Public().(ed25519.PublicKey),
		ttl:     ttl,
	}, nil
}

// Issue returns a signed access token for u, and the time it expires.
func (t *TokenIssuer) Issue(u User) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(t.ttl)

	claims := accessClaims{
		Email: u.Email,
		Role:  u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			// Subject is the user ID. It is a string by spec.
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    tokenIssuer,
			Audience:  jwt.ClaimStrings{tokenAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(t.private)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}

	return signed, expires, nil
}

// Verify checks a token and returns who it belongs to.
func (t *TokenIssuer) Verify(tokenString string) (Principal, error) {
	var claims accessClaims

	_, err := jwt.ParseWithClaims(tokenString, &claims,
		func(*jwt.Token) (any, error) { return t.public, nil },

		// This is the important line.
		//
		// A JWT states its own algorithm in its header. A verifier that trusts
		// that header can be attacked: set alg=none and strip the signature, and
		// a naive implementation accepts the token. Or set alg=HS256 against an
		// RSA/Ed25519 verifier, and the public key gets used as an HMAC secret —
		// and the public key is public.
		//
		// WithValidMethods pins what we will accept, so the token's own claim
		// about itself is irrelevant.
		jwt.WithValidMethods([]string{tokenAlgorithm}),

		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkewLeeway),
	)
	if err != nil {
		// Expiry is worth distinguishing: the client should refresh rather than
		// send the user back to the login screen.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Principal{}, ErrExpiredToken
		}
		return Principal{}, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}

	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: subject is not a user id", ErrInvalidToken)
	}

	return Principal{
		UserID: userID,
		Email:  claims.Email,
		Role:   claims.Role,
	}, nil
}
