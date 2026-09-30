package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// The .sql files are compiled into the binary, so a deployed container carries
// its own schema and does not need the repository mounted next to it.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockName is a MySQL named lock. Two instances starting at the same
// time (a rolling deploy) would otherwise both try to migrate, and MySQL has no
// transactional DDL to sort that out afterwards.
const migrationLockName = "shortly_migrations"

// Migrate applies any migration files that have not run yet.
//
// Deliberately simple: forward only, one file at a time, recorded by filename.
// There are no "down" migrations — rolling a schema change back in production
// almost always means writing a new forward migration anyway.
func Migrate(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	if err := acquireLock(ctx, db); err != nil {
		return err
	}
	defer releaseLock(ctx, db)

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (filename)
		) ENGINE=InnoDB`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedMigrations(ctx, db)
	if err != nil {
		return err
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}

	count := 0
	for _, name := range names {
		if applied[name] {
			continue
		}

		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
		logger.Info("migration applied", "file", name)
		count++
	}

	if count == 0 {
		logger.Debug("schema is up to date")
	}
	return nil
}

func acquireLock(ctx context.Context, db *sql.DB) error {
	// GET_LOCK returns 1 on success, 0 on timeout, NULL on error.
	var got sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT GET_LOCK(?, 10)`, migrationLockName).Scan(&got); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if !got.Valid || got.Int64 != 1 {
		return fmt.Errorf("could not acquire migration lock — another instance may be migrating")
	}
	return nil
}

func releaseLock(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, `SELECT RELEASE_LOCK(?)`, migrationLockName)
}

func appliedMigrations(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[name] = true
	}
	return applied, rows.Err()
}

func migrationNames() ([]string, error) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// Filenames are zero-padded (0001_, 0002_), so lexical order is apply order.
	sort.Strings(names)
	return names, nil
}

func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	body, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	// MySQL has NO transactional DDL: a CREATE TABLE commits immediately and
	// cannot be rolled back. So each statement is applied on its own and the
	// file is only recorded once every statement in it succeeded. A failure
	// half way leaves the schema partly changed — that is a property of MySQL,
	// not a bug here, and it is why each file should stay small.
	for _, statement := range splitStatements(string(body)) {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migration %s failed (schema may be partly applied): %w", name, err)
		}
	}

	if _, err := db.ExecContext(ctx,
		`INSERT INTO schema_migrations (filename) VALUES (?)`, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

// splitStatements breaks a file into individual statements.
//
// The driver sends one statement per call unless multiStatements is enabled,
// and enabling that on the main connection would widen the blast radius of any
// injection bug elsewhere. Comments are stripped first so a semicolon inside
// one cannot split a statement in half.
func splitStatements(body string) []string {
	var cleaned strings.Builder

	for line := range strings.Lines(body) {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned.WriteString(line)
	}

	var statements []string
	for _, part := range strings.Split(cleaned.String(), ";") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			statements = append(statements, trimmed)
		}
	}
	return statements
}
