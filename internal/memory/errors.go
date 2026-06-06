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

// ErrInvalidEntry signals a programmer error: a write was attempted with a
// malformed entry (unknown layer/source, missing project id). It is not a
// SPEC §23.5 classification; the write path wraps it in ErrWriteFailed at
// the store boundary.
var ErrInvalidEntry = errors.New("memory: invalid entry")
