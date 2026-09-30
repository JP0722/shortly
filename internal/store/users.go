package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/JP0722/shortly/internal/auth"
	"github.com/go-sql-driver/mysql"
)

// CreateUser inserts a user and returns it with its generated ID.
//
// Returns auth.ErrEmailTaken if the email is already registered — that comes
// from the UNIQUE index, not from a prior SELECT, so two simultaneous
// registrations for the same email cannot both succeed.
func (s *Store) CreateUser(ctx context.Context, u auth.User) (auth.User, error) {
	const query = `INSERT INTO users (email, password_hash, role) VALUES (?, ?, ?)`

	result, err := s.db.ExecContext(ctx, query, u.Email, u.PasswordHash, u.Role)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlDuplicateEntry {
			return auth.User{}, auth.ErrEmailTaken
		}
		return auth.User{}, fmt.Errorf("insert user: %w", err)
	}

	// MySQL has no RETURNING clause, so the generated ID comes back separately.
	id, err := result.LastInsertId()
	if err != nil {
		return auth.User{}, fmt.Errorf("read inserted user id: %w", err)
	}
	u.ID = id

	return u, nil
}

// UserByID looks up a user by primary key.
func (s *Store) UserByID(ctx context.Context, id int64) (auth.User, error) {
	const query = `
		SELECT id, email, password_hash, role, created_at
		FROM users
		WHERE id = ?`

	var u auth.User
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("select user by id: %w", err)
	}
	return u, nil
}

// UserByEmail looks up a user, returning auth.ErrUserNotFound if there is none.
func (s *Store) UserByEmail(ctx context.Context, email string) (auth.User, error) {
	const query = `
		SELECT id, email, password_hash, role, created_at
		FROM users
		WHERE email = ?`

	var u auth.User
	err := s.db.QueryRowContext(ctx, query, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("select user by email: %w", err)
	}

	return u, nil
}
