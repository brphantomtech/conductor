package knowledge

import "errors"

// SPEC §23.5 — Knowledge engine errors. Sentinel string values match the SPEC
// identifiers verbatim.
var (
	// ErrIndexFailed signals that a (re-)index cycle aborted before
	// completion.
	ErrIndexFailed = errors.New("knowledge_index_failed")

	// ErrSearchFailed signals that a hybrid search query could not be
	// executed against the configured backend.
	ErrSearchFailed = errors.New("knowledge_search_failed")

	// ErrEmbeddingRequestFailed signals that the configured embedding
	// provider rejected the request.
	ErrEmbeddingRequestFailed = errors.New("embedding_request_failed")
)
