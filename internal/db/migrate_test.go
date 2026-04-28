package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/db"
)

func TestMigrate_AppliesInitialSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	d, err := db.Open(ctx, db.Options{
		Driver: db.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "test.db"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	applied, err := db.Migrate(ctx, d)
	require.NoError(t, err)
	require.GreaterOrEqual(t, applied, 1)

	v, err := db.CurrentSchemaVersion(ctx, d)
	require.NoError(t, err)
	require.Equal(t, 1, v)

	// audit_events must exist after migration 0001.
	row := d.SQL().QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name='audit_events'`)
	var name string
	require.NoError(t, row.Scan(&name))
	require.Equal(t, "audit_events", name)
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	d, err := db.Open(ctx, db.Options{
		Driver: db.DriverSQLite,
		DSN:    filepath.Join(t.TempDir(), "test.db"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	applied1, err := db.Migrate(ctx, d)
	require.NoError(t, err)
	require.Greater(t, applied1, 0)

	applied2, err := db.Migrate(ctx, d)
	require.NoError(t, err)
	require.Equal(t, 0, applied2, "second Migrate must be a no-op")
}

func TestOpen_RejectsPostgresInPhase1(t *testing.T) {
	t.Parallel()
	_, err := db.Open(context.Background(),
		db.Options{Driver: db.DriverPostgres, DSN: "postgres://nowhere"})
	require.Error(t, err)
	require.ErrorIs(t, err, db.ErrUnsupportedDriver)
}
