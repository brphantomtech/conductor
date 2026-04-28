package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	// modernc.org/sqlite registers itself as the "sqlite" driver. It is a
	// pure-Go translation of SQLite, so the binary stays cgo-free.
	_ "modernc.org/sqlite"
)

// Driver enumerates the supported database backends.
type Driver string

const (
	// DriverSQLite is the default backend. Zero external dependencies.
	DriverSQLite Driver = "sqlite"

	// DriverPostgres is reserved for the team profile (Phase 18). The
	// constructor returns ErrUnsupportedDriver for now.
	DriverPostgres Driver = "postgres"
)

// ErrUnsupportedDriver is returned by Open for drivers that are valid in the
// SPEC §3.2 deployment matrix but not yet wired into the binary.
var ErrUnsupportedDriver = errors.New("db: driver not implemented")

// Options controls how Open constructs the connection pool.
type Options struct {
	// Driver selects the backend. DriverSQLite is the default.
	Driver Driver

	// DSN is the data source name. For SQLite this is the file path; an
	// empty DSN with DriverSQLite opens an in-memory database that survives
	// only for the lifetime of the *DB.
	DSN string

	// MaxOpenConns caps the connection pool. Zero means "use the driver
	// default": SQLite is single-writer so we pin it to 1, Postgres takes
	// the database/sql default (unlimited).
	MaxOpenConns int

	// MaxIdleConns caps idle connections retained in the pool. Zero falls
	// back to MaxOpenConns (matching database/sql semantics).
	MaxIdleConns int

	// ConnMaxLifetime caps the age of a single connection before the pool
	// recycles it. Zero disables the cap.
	ConnMaxLifetime time.Duration
}

// DB wraps a *sql.DB and exposes the small surface the rest of Conductor
// needs. Higher-tier packages compose their own repositories on top of DB
// rather than reaching into the standard library directly so this module
// can swap drivers (or insert a tracing layer) in one place.
type DB struct {
	driver Driver
	dsn    string
	sql    *sql.DB
}

// Open creates the connection pool and verifies it with a Ping. It does
// NOT run migrations; callers run migrations explicitly via Migrate so
// the migration pass is observable in logs and dry-runs.
func Open(ctx context.Context, opts Options) (*DB, error) {
	if opts.Driver == "" {
		opts.Driver = DriverSQLite
	}

	switch opts.Driver {
	case DriverSQLite:
		return openSQLite(ctx, opts)
	case DriverPostgres:
		return nil, fmt.Errorf("%w: %s (Phase 18)", ErrUnsupportedDriver, opts.Driver)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, opts.Driver)
	}
}

func openSQLite(ctx context.Context, opts Options) (*DB, error) {
	dsn := opts.DSN
	switch {
	case dsn == "" || dsn == ":memory:":
		// Anonymous in-memory DB. ":memory:" with shared cache so multiple
		// goroutines can talk to it via the connection pool; without
		// `cache=shared` SQLite gives each connection a private database.
		dsn = "file::memory:?cache=shared"
	case strings.HasPrefix(dsn, "file:"):
		// Caller already supplied a SQLite URI — pass it through.
	default:
		dsn = sqliteFileDSN(dsn)
	}

	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: sql.Open(sqlite, %q): %w", dsn, err)
	}

	maxOpen := opts.MaxOpenConns
	if maxOpen == 0 {
		// SQLite serializes writes; one open connection avoids "database
		// is locked" under any concurrent write workload.
		maxOpen = 1
	}
	d.SetMaxOpenConns(maxOpen)
	if opts.MaxIdleConns > 0 {
		d.SetMaxIdleConns(opts.MaxIdleConns)
	}
	if opts.ConnMaxLifetime > 0 {
		d.SetConnMaxLifetime(opts.ConnMaxLifetime)
	}

	if err := d.PingContext(ctx); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	if err := applyPragmas(ctx, d); err != nil {
		_ = d.Close()
		return nil, err
	}

	return &DB{driver: DriverSQLite, dsn: dsn, sql: d}, nil
}

// sqliteFileDSN turns a file path into a modernc-compatible DSN. The driver
// accepts either a bare path or a `file:` URI; we prefer the URI form so we
// can attach pragma-style query parameters in one place.
func sqliteFileDSN(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// modernc accepts the canonical SQLite URI directly. Forward slashes
	// work on Windows because SQLite normalizes the path internally.
	return "file:" + strings.ReplaceAll(abs, "\\", "/") + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)" //nolint:lll // DSN must be a single token
}

// applyPragmas issues the runtime PRAGMAs that cannot be embedded in the DSN
// (or that we want to verify are sticky). For SQLite this is a no-op when
// the DSN already carried the pragmas, but issuing them again is cheap and
// keeps behavior consistent across sub-drivers.
func applyPragmas(ctx context.Context, d *sql.DB) error {
	stmts := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
	}
	for _, s := range stmts {
		if _, err := d.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("db: pragma %q: %w", s, err)
		}
	}
	return nil
}

// Close releases the underlying connection pool.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	if err := db.sql.Close(); err != nil {
		return fmt.Errorf("db: close: %w", err)
	}
	return nil
}

// Driver returns the active backend driver.
func (db *DB) Driver() Driver { return db.driver }

// SQL returns the wrapped *sql.DB. Higher-tier code should prefer the
// Exec / Query / QueryRow helpers below; SQL is exposed only for migrations
// and tests that need to drive the connection directly.
func (db *DB) SQL() *sql.DB { return db.sql }

// Exec runs a parameterized statement and returns the result.
func (db *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	res, err := db.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db: exec: %w", err)
	}
	return res, nil
}

// Query runs a parameterized SELECT and returns the rows iterator.
func (db *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db: query: %w", err)
	}
	return rows, nil
}

// QueryRow runs a parameterized SELECT expected to return at most one row.
func (db *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return db.sql.QueryRowContext(ctx, query, args...)
}
