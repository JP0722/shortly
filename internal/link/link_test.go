package link

import (
	"errors"
	"strings"
	"testing"
)

func TestNewCodeShape(t *testing.T) {
	code := NewCode()

	if len(code) != codeLength {
		t.Errorf("len(%q) = %d, want %d", code, len(code), codeLength)
	}

	for _, r := range code {
		if !strings.ContainsRune(alphabet, r) {
			t.Errorf("code %q contains %q, which is not in the alphabet", code, r)
		}
	}
}

func TestNewCodeIsUnique(t *testing.T) {
	// Not a proof of randomness — just a smoke test that we are not returning
	// the same value every time, which is what a broken implementation does.
	seen := make(map[string]bool)

	for i := range 1000 {
		code := NewCode()
		if seen[code] {
			t.Fatalf("duplicate code %q after %d generations", code, i)
		}
		seen[code] = true
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"https", "https://example.com/a/b", "https://example.com/a/b", false},
		{"http", "http://example.com", "http://example.com", false},
		{"query preserved", "https://example.com/s?q=1&r=2", "https://example.com/s?q=1&r=2", false},

		{"javascript scheme", "javascript:alert(1)", "", true},
		{"data scheme", "data:text/html,<h1>hi</h1>", "", true},
		{"ftp scheme", "ftp://example.com", "", true},
		{"relative path", "/just/a/path", "", true},
		{"empty", "", "", true},
		{"scheme but no host", "https://", "", true},
		{"too long", "https://example.com/" + strings.Repeat("a", maxURLLen), "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.raw)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeURL(%q) error = nil, want an error", tt.raw)
				}
				// Every rejection must wrap ErrInvalidURL so the HTTP layer can
				// turn it into a 400 without inspecting the message.
				if !errors.Is(err, ErrInvalidURL) {
					t.Errorf("error %v does not wrap ErrInvalidURL", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("NormalizeURL(%q) error = %v, want nil", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
