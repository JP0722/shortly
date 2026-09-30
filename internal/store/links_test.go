package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/JP0722/shortly/internal/link"
)

// newTestStore connects to a real MySQL. There is no mock here on purpose: the
// whole job of this package is talking to MySQL correctly, and a mock would only
// prove that our idea of MySQL matches our idea of MySQL.
//
// `make test` sets SHORTLY_TEST_DSN. Running `go test ./...` bare skips these.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("SHORTLY_TEST_DSN")
	if dsn == "" {
		t.Skip("SHORTLY_TEST_DSN not set — run `make test` (needs `make up` first)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	// t.Cleanup runs when this test finishes — like defer, but it survives being
	// called from a helper rather than from the test function itself.
	t.Cleanup(func() { db.Close() })

	return New(db)
}

// insertLink creates a link and removes it again when the test ends, so tests
// can run repeatedly without colliding.
func insertLink(t *testing.T, s *Store, target string) link.Link {
	t.Helper()

	l := link.Link{Code: link.NewCode(), TargetURL: target}

	if err := s.Create(context.Background(), l); err != nil {
		t.Fatalf("Create(%q) error = %v", l.Code, err)
	}
	t.Cleanup(func() {
		s.db.ExecContext(context.Background(), `DELETE FROM links WHERE code = ?`, l.Code)
	})

	return l
}

func TestCreateAndReadBack(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	want := insertLink(t, s, "https://example.com/a/b?c=1")

	got, err := s.ByCode(ctx, want.Code)
	if err != nil {
		t.Fatalf("ByCode(%q) error = %v", want.Code, err)
	}

	if got.Code != want.Code {
		t.Errorf("Code = %q, want %q", got.Code, want.Code)
	}
	if got.TargetURL != want.TargetURL {
		t.Errorf("TargetURL = %q, want %q", got.TargetURL, want.TargetURL)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero — the database default did not come back")
	}
}

func TestByCodeMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)

	_, err := s.ByCode(context.Background(), "nosuch")

	// The caller must be able to recognise this without knowing about SQL.
	if !errors.Is(err, link.ErrNotFound) {
		t.Fatalf("ByCode(missing) error = %v, want link.ErrNotFound", err)
	}
	// And it must NOT leak the database's own error.
	if errors.Is(err, errors.New("sql: no rows in result set")) {
		t.Error("sql.ErrNoRows leaked out of the store")
	}
}

func TestCreateDuplicateCodeReturnsErrCodeTaken(t *testing.T) {
	s := newTestStore(t)

	first := insertLink(t, s, "https://example.com/first")

	// Same code, different target — MySQL error 1062 must become a domain error.
	second := link.Link{Code: first.Code, TargetURL: "https://example.com/second"}

	err := s.Create(context.Background(), second)
	if !errors.Is(err, link.ErrCodeTaken) {
		t.Fatalf("Create(duplicate) error = %v, want link.ErrCodeTaken", err)
	}
}
