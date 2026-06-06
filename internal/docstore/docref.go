package docstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// DocRef is the canonical reference to one document in a Doc Store (SPEC
// §4.1.9). It carries the change-detection metadata (ContentHash,
// LastSyncedAt) the scheduler uses for incremental sync and the optional dense
// Embedding produced when the document is indexed into the Knowledge Engine.
type DocRef struct {
	// ID is a stable identifier of the document: hash(<store_id>/<path_or_id>).
	ID string `json:"id"`
	// Title is the human-readable document title (often the first heading or
	// the base filename).
	Title string `json:"title"`
	// StoreID is the id of the configured Doc Store this document belongs to.
	StoreID string `json:"store_id"`
	// PathOrID is the backend-relative path (local_fs/git_repo) or object key
	// (s3) the backend uses to Fetch the document.
	PathOrID string `json:"path_or_id"`
	// ContentHash is the SHA-256 of the document content, used to detect
	// changes between syncs.
	ContentHash string `json:"content_hash"`
	// Tags are the store-level tags propagated to every document plus any
	// backend-derived tags.
	Tags []string `json:"tags,omitempty"`
	// Embedding is the optional dense vector populated when the document is
	// indexed into the Knowledge Engine.
	Embedding []float32 `json:"embedding,omitempty"`
	// LastSyncedAt is the time the document was last successfully synced.
	LastSyncedAt time.Time `json:"last_synced_at"`
}

// DocRefID computes the stable document identifier hash(<store_id>/<path_or_id>)
// so re-syncing an unchanged document yields the same ID.
func DocRefID(storeID, pathOrID string) string {
	sum := sha256.Sum256([]byte(storeID + "/" + pathOrID))
	return hex.EncodeToString(sum[:])
}

// HashContent computes the SHA-256 content hash used for change detection.
func HashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// DocFilter restricts a List query (SPEC §20.4). All fields are optional; a
// zero-value DocFilter matches every document. Slice fields are OR-within /
// AND-across.
type DocFilter struct {
	// Tags restricts to documents carrying at least one of the given tags
	// (empty means any).
	Tags []string
	// PathPrefix restricts to documents whose PathOrID has the given prefix
	// (empty means any).
	PathPrefix string
}

// matches reports whether ref satisfies the filter.
func (f DocFilter) matches(ref DocRef) bool {
	if f.PathPrefix != "" && !hasPrefix(ref.PathOrID, f.PathPrefix) {
		return false
	}
	if len(f.Tags) > 0 && !anyTag(ref.Tags, f.Tags) {
		return false
	}
	return true
}

// Backend is the Doc Store extension point (SPEC §20.4): the contract a
// backend implements to expose a documentation source to the manager. It is a
// cross-cutting public extension point, so it lives in the implementer's home
// package per docs/conventions.md §3.
type Backend interface {
	// Sync lists the current document set from the backend and returns a DocRef
	// for each document (with its ContentHash populated). It performs whatever
	// transport-level refresh the backend needs (a git pull, an S3 list) but
	// does not itself decide what changed — that is the scheduler's job.
	Sync(ctx context.Context) ([]DocRef, error)

	// Fetch returns the raw content of the document referenced by ref.
	Fetch(ctx context.Context, ref DocRef) (string, error)

	// List returns the documents matching filter without forcing a transport
	// refresh, drawing on the backend's most recent Sync.
	List(ctx context.Context, filter DocFilter) ([]DocRef, error)
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func anyTag(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}
