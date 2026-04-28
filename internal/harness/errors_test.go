package harness_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/docstore"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
	"github.com/conductor-sh/conductor/internal/memory"
	"github.com/conductor-sh/conductor/internal/orchestrator"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// TestSpecErrorRegistry asserts that every SPEC §23 identifier is reachable
// through exactly one exported sentinel and that no two sentinels share the
// same string value. The test lives in package harness_test rather than in
// the package whose sentinels it cross-checks because it pulls in every
// owner package; placing it in any single package would create artificial
// coupling.
func TestSpecErrorRegistry(t *testing.T) {
	registry := map[string]error{
		// §23.1
		"missing_harness_file":           harness.ErrMissingHarnessFile,
		"harness_parse_error":            harness.ErrHarnessParse,
		"harness_front_matter_not_a_map": harness.ErrHarnessFrontMatterShape,
		"template_parse_error":           harness.ErrTemplateParse,
		"template_render_error":          harness.ErrTemplateRender,

		// §23.2
		"unsupported_tracker_kind":   tracker.ErrUnsupportedKind,
		"missing_tracker_api_key":    tracker.ErrMissingAPIKey,
		"missing_tracker_project_id": tracker.ErrMissingProjectID,
		"tracker_request_failed":     tracker.ErrRequestFailed,
		"tracker_response_error":     tracker.ErrResponseError,
		"tracker_graphql_errors":     tracker.ErrGraphQLErrors,
		"tracker_unknown_payload":    tracker.ErrUnknownPayload,

		// §23.3
		"unsupported_provider":      provider.ErrUnsupportedProvider,
		"missing_provider_api_key":  provider.ErrMissingAPIKey,
		"provider_request_failed":   provider.ErrRequestFailed,
		"provider_response_timeout": provider.ErrResponseTimeout,
		"provider_stream_error":     provider.ErrStreamError,
		"turn_input_required":       provider.ErrTurnInputRequired,
		"context_budget_exceeded":   provider.ErrContextBudgetExceeded,

		// §23.4
		"workspace_creation_failed":   orchestrator.ErrWorkspaceCreationFailed,
		"hook_failed":                 orchestrator.ErrHookFailed,
		"prompt_render_failed":        orchestrator.ErrPromptRenderFailed,
		"turn_timeout":                orchestrator.ErrTurnTimeout,
		"turn_failed":                 orchestrator.ErrTurnFailed,
		"turn_cancelled":              orchestrator.ErrTurnCancelled,
		"stall_timeout":               orchestrator.ErrStallTimeout,
		"validation_pipeline_failed":  orchestrator.ErrValidationPipelineFailed,
		"cancelled_by_reconciliation": orchestrator.ErrCancelledByReconciliation,

		// §23.5
		"knowledge_index_failed":   knowledge.ErrIndexFailed,
		"knowledge_search_failed":  knowledge.ErrSearchFailed,
		"embedding_request_failed": knowledge.ErrEmbeddingRequestFailed,
		"memory_read_failed":       memory.ErrReadFailed,
		"memory_write_failed":      memory.ErrWriteFailed,
		"doc_store_sync_failed":    docstore.ErrSyncFailed,
	}

	for ident, err := range registry {
		require.NotNil(t, err, "sentinel for %q is nil", ident)
		require.Equal(t, ident, err.Error(),
			"sentinel for %q must have its SPEC identifier as its message", ident)
	}

	// No two sentinels share a string. The map keys above guarantee this on
	// the SPEC side; this loop verifies it on the Go side.
	seen := map[string]string{}
	for ident, err := range registry {
		if prev, ok := seen[err.Error()]; ok {
			t.Fatalf("duplicate sentinel string %q (used by %q and %q)",
				err.Error(), prev, ident)
		}
		seen[err.Error()] = ident
	}

	// Sanity: errors.Is finds itself.
	require.ErrorIs(t, harness.ErrMissingHarnessFile, harness.ErrMissingHarnessFile)
	require.False(t, errors.Is(harness.ErrMissingHarnessFile, harness.ErrHarnessParse))
}
