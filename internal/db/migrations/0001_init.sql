-- 0001_init.sql
--
-- Initial schema for Conductor. Creates the schema_migrations bookkeeping
-- table and the audit_events table required by SPEC §17.3 (the central
-- audit store). Domain tables (issues, run_attempts, memories, knowledge
-- nodes, etc.) land in subsequent migrations as their owning packages are
-- implemented. Phase 1 only needs audit_events because the binary writes
-- one placeholder event in `conductor start` to validate the pipeline.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    applied_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS audit_events (
    id              TEXT PRIMARY KEY,
    timestamp       TEXT NOT NULL,
    project_id      TEXT NOT NULL,
    issue_id        TEXT,
    session_id      TEXT,
    agent_role      TEXT,
    event_type      TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',
    parent_event_id TEXT,
    duration_ms     INTEGER
);

CREATE INDEX IF NOT EXISTS idx_audit_events_project_timestamp
    ON audit_events (project_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_events_issue_timestamp
    ON audit_events (issue_id, timestamp);

CREATE INDEX IF NOT EXISTS idx_audit_events_event_type
    ON audit_events (event_type);

CREATE INDEX IF NOT EXISTS idx_audit_events_parent
    ON audit_events (parent_event_id);
