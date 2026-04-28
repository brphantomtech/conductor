package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migration is one parsed entry in the migrations directory.
type migration struct {
	version int
	name    string
	body    string
}

// ErrMigrationFormat is returned when a migration file name does not follow
// the `<NNNN>_<name>.sql` convention.
var ErrMigrationFormat = errors.New("db: migration filename must look like 0001_name.sql")

// Migrate applies every embedded migration whose version number is greater
// than the highest version recorded in schema_migrations. Migrations run
// strictly forward; there is no `down` step. Each migration is applied in
// its own transaction so a partial failure leaves the previous migration
// intact and the failing one absent from schema_migrations.
//
// Migrate is idempotent: calling it on an up-to-date database is a no-op.
func Migrate(ctx context.Context, d *DB) (int, error) {
	if d == nil || d.sql == nil {
		return 0, errors.New("db: Migrate called with nil DB")
	}

	migrations, err := loadMigrations(migrationFS, "migrations")
	if err != nil {
		return 0, err
	}

	if _, err := d.sql.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		)`); err != nil {
		return 0, fmt.Errorf("db: bootstrap schema_migrations: %w", err)
	}

	current, err := currentVersion(ctx, d)
	if err != nil {
		return 0, err
	}

	applied := 0
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := applyMigration(ctx, d, m); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// CurrentSchemaVersion returns the highest applied migration version, or 0
// if no migrations have been applied yet.
func CurrentSchemaVersion(ctx context.Context, d *DB) (int, error) {
	return currentVersion(ctx, d)
}

func currentVersion(ctx context.Context, d *DB) (int, error) {
	var v *int
	row := d.sql.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`)
	if err := row.Scan(&v); err != nil {
		// Table might not exist yet. The caller bootstraps it before
		// invoking currentVersion in the normal path; this fallback covers
		// callers that probe the version before Migrate has run.
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, fmt.Errorf("db: read schema_migrations: %w", err)
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

func applyMigration(ctx context.Context, d *DB, m migration) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %d: %w", m.version, err)
	}

	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: apply migration %d (%s): %w", m.version, m.name, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES (?, ?)`,
		m.version, m.name,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("db: record migration %d (%s): %w", m.version, m.name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %d: %w", m.version, err)
	}
	return nil
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("db: read migrations dir: %w", err)
	}

	var out []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, label, err := parseMigrationName(name)
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("db: read migration %s: %w", name, err)
		}
		out = append(out, migration{
			version: version,
			name:    label,
			body:    string(body),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })

	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("db: duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

func parseMigrationName(name string) (int, string, error) {
	trimmed := strings.TrimSuffix(name, ".sql")
	idx := strings.IndexByte(trimmed, '_')
	if idx <= 0 || idx == len(trimmed)-1 {
		return 0, "", fmt.Errorf("%w: %s", ErrMigrationFormat, name)
	}
	v, err := strconv.Atoi(trimmed[:idx])
	if err != nil {
		return 0, "", fmt.Errorf("%w: %s", ErrMigrationFormat, name)
	}
	return v, trimmed[idx+1:], nil
}
