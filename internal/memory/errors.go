package memory

import "errors"

// SPEC §23.5 — Memory manager errors. Sentinel string values match the SPEC
// identifiers verbatim.
var (
	// ErrReadFailed signals that a memory retrieval (episodic, semantic, or
	// procedural) failed at the storage layer.
	ErrReadFailed = errors.New("memory_read_failed")

	// ErrWriteFailed signals that a memory write was rejected by the storage
	// layer.
	ErrWriteFailed = errors.New("memory_write_failed")
)
