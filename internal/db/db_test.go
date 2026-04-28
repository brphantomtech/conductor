package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/db"
)

func TestOpen_CreatesFileBackedDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "phase1.db")

	d, err := db.Open(ctx, db.Options{
		Driver:       db.DriverSQLite,
		DSN:          dbPath,
		MaxOpenConns: 1,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	require.Equal(t, db.DriverSQLite, d.Driver())
	require.NotNil(t, d.SQL())

	_, err = d.Exec(ctx, `CREATE TABLE t (k INTEGER PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	_, err = d.Exec(ctx, `INSERT INTO t (k, v) VALUES (?, ?), (?, ?)`,
		1, "first", 2, "second")
	require.NoError(t, err)

	rows, err := d.Query(ctx, `SELECT v FROM t ORDER BY k`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rows.Close() })

	var got []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		got = append(got, v)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"first", "second"}, got)

	var v string
	require.NoError(t, d.QueryRow(ctx, `SELECT v FROM t WHERE k = ?`, 2).Scan(&v))
	require.Equal(t, "second", v)
}

func TestOpen_DefaultDriverIsSQLite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	d, err := db.Open(ctx, db.Options{}) // no Driver, no DSN — anonymous in-memory
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.Equal(t, db.DriverSQLite, d.Driver())
}

func TestClose_NilSafe(t *testing.T) {
	t.Parallel()
	var d *db.DB
	require.NoError(t, d.Close())
}
