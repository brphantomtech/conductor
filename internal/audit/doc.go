// Package audit writes the provenance graph of every significant action in
// the system. Each AuditEvent (SPEC §4.1.10) carries a parent_event_id link
// so the full causal chain — from issue dispatch to a single tool call —
// can be reconstructed. This package owns the `audit_events` table schema,
// the per-workspace .conductor/audit.jsonl log, and the optional outbound
// webhook sink.
//
// Tier 1 (cross-cutting infra). May be imported by every higher tier. Only
// imports config and db.
package audit
