package memory

import (
	"context"
	"strings"

	"github.com/conductor-sh/conductor/internal/audit"
)

// WriteInput is the validated request shape shared by every write path
// (agent-initiated, auto-extracted, consolidation, post-session). The
// manager fills the id, timestamps, embedding, and TTL.
type WriteInput struct {
	Layer     Layer
	ProjectID string
	IssueID   string
	TaskType  string
	Content   string
	Tags      []string
	Source    Source
}

// Write is the single validated write core (SPEC §9.4). It validates the
// input, scopes it, computes created_at/expires_at from the clock, generates
// an embedding when an Embedder is wired, persists, and emits MemoryWritten.
// A disabled manager is a no-op returning a zero entry and nil error.
func (m *Manager) Write(ctx context.Context, in WriteInput) (*MemoryEntry, error) {
	if !m.Enabled() {
		return nil, nil
	}

	in.ProjectID = m.resolveProject(in.ProjectID)
	if err := validateWrite(in); err != nil {
		return nil, wrapWrite(err)
	}

	now := m.clock().UTC()
	entry := MemoryEntry{
		ID:        newID(),
		Layer:     in.Layer,
		ProjectID: in.ProjectID,
		Content:   strings.TrimSpace(in.Content),
		Tags:      in.Tags,
		Source:    in.Source,
		CreatedAt: now,
	}
	switch in.Layer {
	case LayerEpisodic:
		entry.IssueID = in.IssueID
		exp := now.AddDate(0, 0, m.cfg.EpisodicTTLDays)
		entry.ExpiresAt = &exp
	case LayerProcedural:
		entry.TaskType = in.TaskType
	case LayerSemantic:
		// project-scoped only; no TTL.
	}

	if m.embed != nil {
		vec, err := m.embed.Embed(ctx, entry.Content)
		if err != nil {
			return nil, wrapWrite(err)
		}
		entry.Embedding = vec
	}

	if err := m.store.Put(ctx, entry); err != nil {
		return nil, wrapWrite(err)
	}

	m.emit(ctx, audit.AuditEvent{
		ProjectID: entry.ProjectID,
		IssueID:   entry.IssueID,
		EventType: audit.EventMemoryWritten,
		Payload: map[string]any{
			"memory_id": entry.ID,
			"layer":     string(entry.Layer),
			"source":    string(entry.Source),
			"task_type": entry.TaskType,
		},
	})
	return &entry, nil
}

// Retire deletes an entry by id so it no longer appears in retrieval
// (SPEC §9 CLI: `conductor memory retire`). A disabled manager is a no-op.
func (m *Manager) Retire(ctx context.Context, id string) error {
	if !m.Enabled() {
		return nil
	}
	if err := m.store.Retire(ctx, id); err != nil {
		return wrapWrite(err)
	}
	return nil
}

// validateWrite enforces the layer/source/scope invariants (SPEC §4.1.5).
func validateWrite(in WriteInput) error {
	if !in.Layer.Valid() {
		return ErrInvalidEntry
	}
	if !in.Source.Valid() {
		return ErrInvalidEntry
	}
	if in.ProjectID == "" {
		return ErrInvalidEntry
	}
	if strings.TrimSpace(in.Content) == "" {
		return ErrInvalidEntry
	}
	return nil
}
