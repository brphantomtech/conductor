// Package db is the database abstraction layer for Conductor. It exposes a
// connection pool, migration runner, and query helpers that work against
// SQLite (default, via modernc.org/sqlite) and PostgreSQL (optional, via pgx).
// Higher-tier packages construct their own repository types on top of this
// abstraction; this package contains no domain logic.
//
// Tier 0 (foundation). Imports nothing else under internal/.
package db
