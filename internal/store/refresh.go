package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/JP0722/shortly/internal/auth"
)

// CreateRefreshToken stores a new refresh token.
func (s *Store) CreateRefreshToken(ctx context.Context, userID int64, familyID, tokenHash string, expiresAt time.Time) error {
	const query = `
		INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
		VALUES (?, ?, ?, ?)`

	if _, err := s.db.ExecContext(ctx, query, userID, familyID, tokenHash, expiresAt); err != nil {
		return fmt.Errorf("insert refresh token: %w", err)
	}
	return nil
}

// RefreshTokenByHash looks up a stored token. It returns the row even when the
// token is used, revoked or expired — the caller needs to tell those apart,
// because a *used* token coming back is the reuse signal.
func (s *Store) RefreshTokenByHash(ctx context.Context, tokenHash string) (auth.RefreshToken, error) {
	const query = `
		SELECT id, user_id, family_id, expires_at, used_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = ?`

	var (
		t                 auth.RefreshToken
		usedAt, revokedAt sql.NullTime // NULL-able columns cannot scan into time.Time
	)

	err := s.db.QueryRowContext(ctx, query, tokenHash).
		Scan(&t.ID, &t.UserID, &t.FamilyID, &t.ExpiresAt, &usedAt, &revokedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return auth.RefreshToken{}, auth.ErrRefreshNotFound
	}
	if err != nil {
		return auth.RefreshToken{}, fmt.Errorf("select refresh token: %w", err)
	}

	if usedAt.Valid {
		t.UsedAt = &usedAt.Time
	}
	if revokedAt.Valid {
		t.RevokedAt = &revokedAt.Time
	}

	return t, nil
}

// RotateRefreshToken marks the old token used and inserts its replacement, in
// one transaction.
//
// Both must happen or neither. If the insert failed after the update, the user
// would be logged out; if the update failed after the insert, the old token
// would stay valid and reuse detection would never fire.
//
// The UPDATE ... WHERE used_at IS NULL is the important part: if two requests
// present the same token simultaneously, only one of them matches, so only one
// gets a new token. The database decides the winner, not our code.
func (s *Store) RotateRefreshToken(
	ctx context.Context,
	oldID, userID int64,
	familyID, newHash string,
	expiresAt time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rotate transaction: %w", err)
	}
	// Rollback after a successful Commit is a no-op, so this is safe to always defer.
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET used_at = NOW() WHERE id = ? AND used_at IS NULL`,
		oldID)
	if err != nil {
		return fmt.Errorf("mark refresh token used: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rotate result: %w", err)
	}
	if affected == 0 {
		// Someone else rotated this token between our read and this update.
		return auth.ErrRefreshInvalid
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		userID, familyID, newHash, expiresAt); err != nil {
		return fmt.Errorf("insert rotated refresh token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rotate: %w", err)
	}
	return nil
}

// RevokeRefreshFamily invalidates every token descended from one login.
//
// Called when a token is presented twice: one of the two holders is an attacker
// and we cannot tell which, so everyone in that family is logged out.
func (s *Store) RevokeRefreshFamily(ctx context.Context, familyID string) error {
	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE family_id = ? AND revoked_at IS NULL`

	if _, err := s.db.ExecContext(ctx, query, familyID); err != nil {
		return fmt.Errorf("revoke refresh family: %w", err)
	}
	return nil
}

// DeleteExpiredRefreshTokens clears out rows nobody can use any more.
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < NOW()`)
	if err != nil {
		return 0, fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return result.RowsAffected()
}
