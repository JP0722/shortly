package auth

import (
	"errors"
	"strings"
	"testing"
)

const goodPassword = "correct horse battery staple"

func TestHashAndVerify(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	ok, err := VerifyPassword(hash, goodPassword)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !ok {
		t.Error("correct password did not verify")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	ok, err := VerifyPassword(hash, "correct horse battery stapl")
	// A wrong password is a false, not an error.
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v, want nil", err)
	}
	if ok {
		t.Error("wrong password verified successfully")
	}
}

// The same password must never produce the same hash twice, or one cracked
// password cracks every account that shares it.
func TestHashIsSaltedPerCall(t *testing.T) {
	first, _ := HashPassword(goodPassword)
	second, _ := HashPassword(goodPassword)

	if first == second {
		t.Fatal("two hashes of the same password are identical — salt is not random")
	}

	// Both must still verify.
	for i, h := range []string{first, second} {
		ok, err := VerifyPassword(h, goodPassword)
		if err != nil || !ok {
			t.Errorf("hash %d did not verify: ok=%v err=%v", i, ok, err)
		}
	}
}

func TestHashFormatIsPHC(t *testing.T) {
	hash, _ := HashPassword(goodPassword)

	// $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 $-separated parts, got %d: %s", len(parts), hash)
	}
	if parts[1] != "argon2id" {
		t.Errorf("algorithm = %q, want argon2id", parts[1])
	}
	if parts[3] != "m=65536,t=1,p=4" {
		t.Errorf("parameters = %q, want m=65536,t=1,p=4", parts[3])
	}
	if strings.Contains(hash, goodPassword) {
		t.Error("the plaintext password appears in the hash")
	}
}

func TestShortPasswordRejected(t *testing.T) {
	_, err := HashPassword("short")
	if !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("error = %v, want ErrWeakPassword", err)
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	tests := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=1,p=4$only-four-parts",
		"$bcrypt$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA",
	}

	for _, stored := range tests {
		t.Run(stored, func(t *testing.T) {
			ok, err := VerifyPassword(stored, goodPassword)
			if ok {
				t.Error("malformed hash verified successfully")
			}
			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("error = %v, want ErrInvalidHash", err)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{"bob@example.com", "bob@example.com", false},
		{"  Bob@Example.COM  ", "bob@example.com", false},
		{"Bob Smith <bob@example.com>", "bob@example.com", false},
		{"", "", true},
		{"not-an-email", "", true},
		{"@example.com", "", true},
		{strings.Repeat("a", 320) + "@example.com", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := NormalizeEmail(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEmail) {
					t.Fatalf("error = %v, want ErrInvalidEmail", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("= %q, want %q", got, tt.want)
			}
		})
	}
}
