package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/JP0722/shortly/internal/link"
	"github.com/go-sql-driver/mysql"
)

// mysqlDuplicateEntry is MySQL's error number for "this violates a UNIQUE key".
// It is what the links.uq_links_code index returns when two links get the same
// short code.
const mysqlDuplicateEntry = 1062

// Store reads and writes links in MySQL.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Create inserts a link.
//
// It returns link.ErrCodeTaken if the code is already in use, which the caller
// should handle by generating a new code and calling Create again.
func (s *Store) Create(ctx context.Context, l link.Link) error {
	// The ? placeholders are not string formatting. The driver sends the query
	// and the values separately, so a target URL containing SQL syntax is data,
	// never code. Building this string with fmt.Sprintf would be an injection bug.
	const query = `INSERT INTO links (code, target_url, user_id) VALUES (?, ?, ?)`

	// A zero UserID means "no owner": store NULL rather than 0, which would
	// violate the foreign key.
	var owner any
	if l.UserID != 0 {
		owner = l.UserID
	}

	_, err := s.db.ExecContext(ctx, query, l.Code, l.TargetURL, owner)
	if err != nil {
		// errors.As unwraps the chain looking for a *mysql.MySQLError and, if it
		// finds one, assigns it to mysqlErr so we can read the fields.
		// (errors.Is asks "is this that error?"; errors.As asks "is there an
		// error of this type in here, and can I have it?")
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntry {
			return link.ErrCodeTaken
		}
		return fmt.Errorf("insert link: %w", err)
	}

	return nil
}

// ByCode returns the link with the given short code, or link.ErrNotFound.
func (s *Store) ByCode(ctx context.Context, code string) (link.Link, error) {
	const query = `SELECT code, target_url, created_at, user_id FROM links WHERE code = ?`

	var (
		l     link.Link
		owner sql.NullInt64 // user_id is NULL-able
	)

	// Scan writes into the variables whose addresses we pass — the same
	// "fill what I give you" pattern as rand.Read. The & are required: without
	// them Scan would receive copies and have nowhere to put the results.
	err := s.db.QueryRowContext(ctx, query, code).
		Scan(&l.Code, &l.TargetURL, &l.CreatedAt, &owner)

	// Translate the database's "no rows" into our own domain error, so nothing
	// outside this package has to know we use SQL at all.
	if errors.Is(err, sql.ErrNoRows) {
		return link.Link{}, link.ErrNotFound
	}
	if err != nil {
		return link.Link{}, fmt.Errorf("select link by code: %w", err)
	}

	l.UserID = owner.Int64 // zero when NULL

	return l, nil
}

// ListByUser returns a user's links, newest first.
func (s *Store) ListByUser(ctx context.Context, userID int64, limit int) ([]link.Link, error) {
	const query = `
		SELECT code, target_url, created_at
		FROM links
		WHERE user_id = ?
		ORDER BY id DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("select links by user: %w", err)
	}
	// Rows holds a database connection until closed. Forgetting this leaks a
	// connection per call and eventually exhausts the pool.
	defer rows.Close()

	links := []link.Link{}
	for rows.Next() {
		l := link.Link{UserID: userID}
		if err := rows.Scan(&l.Code, &l.TargetURL, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan link row: %w", err)
		}
		links = append(links, l)
	}

	// Iteration can stop early because of an error, not just because the rows
	// ran out. Without this check a partial result looks like a complete one.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate link rows: %w", err)
	}

	return links, nil
}

// DeleteByCode removes a link the user owns. It returns link.ErrNotFound both
// when the code does not exist and when it belongs to someone else — a 403
// would confirm that the link exists.
func (s *Store) DeleteByCode(ctx context.Context, code string, userID int64) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM links WHERE code = ? AND user_id = ?`, code, userID)
	if err != nil {
		return fmt.Errorf("delete link: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete result: %w", err)
	}
	if affected == 0 {
		return link.ErrNotFound
	}
	return nil
}
