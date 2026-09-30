// Package store talks to MySQL.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// Blank import: we never call this package directly, but importing it runs
	// its init() function, which registers the "mysql" driver name with
	// database/sql. Without this line sql.Open("mysql", ...) fails with
	// "unknown driver". The _ tells the compiler the unused import is deliberate.
	_ "github.com/go-sql-driver/mysql"
)

// Open connects to MySQL and verifies the connection works.
func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	// sql.Open does NOT connect to anything. It only parses the DSN and builds
	// the pool object; the first real connection happens lazily on first use.
	// That is why the Ping below matters.
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	// *sql.DB is a CONNECTION POOL, not a connection. Create one per process and
	// share it; it is safe for concurrent use. Never open one per request.

	// Default is unlimited, which under load means opening connections until
	// MySQL refuses them (its own default limit is 151).
	db.SetMaxOpenConns(25)

	// Default is 2. If this is lower than MaxOpenConns, the pool constantly
	// closes and reopens connections under load — slow, for no reason.
	db.SetMaxIdleConns(25)

	// Recycle connections so we never hand out one that MySQL or a load balancer
	// has already closed on its side.
	db.SetConnMaxLifetime(5 * time.Minute)

	// Force a real connection now, so a misconfigured or down database fails at
	// startup rather than on the first user request.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return db, nil
}

// Ping reports whether the database is reachable. Used by the readiness check.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
