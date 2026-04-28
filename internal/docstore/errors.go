package docstore

import "errors"

// SPEC §23.5 — Doc store errors. Sentinel string values match the SPEC
// identifiers verbatim.
var (
	// ErrSyncFailed signals that a Doc Store sync run failed before the new
	// snapshot could be committed.
	ErrSyncFailed = errors.New("doc_store_sync_failed")
)
