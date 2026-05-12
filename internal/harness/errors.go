package harness

import "errors"

// SPEC §23.1 — Harness errors. The string value of each sentinel matches the
// SPEC identifier exactly so it appears verbatim in audit events and API
// responses.
var (
	// ErrMissingHarnessFile signals that the configured HARNESS.md path does
	// not exist on disk or in the configured Doc Store.
	ErrMissingHarnessFile = errors.New("missing_harness_file")

	// ErrHarnessParse signals a generic failure to parse HARNESS.md (front
	// matter delimiter missing, malformed YAML body, etc.).
	ErrHarnessParse = errors.New("harness_parse_error")

	// ErrHarnessFrontMatterShape signals that the parsed front matter is not
	// a YAML mapping at the root level.
	ErrHarnessFrontMatterShape = errors.New("harness_front_matter_not_a_map")

	// ErrTemplateParse signals a Liquid template that fails to compile.
	ErrTemplateParse = errors.New("template_parse_error")

	// ErrTemplateRender signals a Liquid template that fails at render time
	// (unknown variable, unknown filter, etc.).
	ErrTemplateRender = errors.New("template_render_error")
)

// Validation sub-class sentinels. These are not in SPEC §23 literally —
// SPEC §6.4 expresses the rules in prose — but the CLI uses these
// sentinels with errors.As to categorize and pretty-print each underlying
// failure separately. Their string values are stable so they can be
// matched against and embedded in audit payloads.
var (
	// ErrProjectIDMissing — project.id is empty or unset.
	ErrProjectIDMissing = errors.New("project_id_missing")

	// ErrTrackerKindMissing — tracker.kind is empty.
	ErrTrackerKindMissing = errors.New("tracker_kind_missing")

	// ErrTrackerKindUnsupported — tracker.kind is not in the SPEC §5.3.2
	// supported list.
	ErrTrackerKindUnsupported = errors.New("tracker_kind_unsupported")

	// ErrTrackerAPIKeyMissing — tracker.api_key resolves to empty after
	// $VAR expansion.
	ErrTrackerAPIKeyMissing = errors.New("tracker_api_key_missing")

	// ErrTrackerProjectRefMissing — neither project_id nor project_slug
	// is set for a tracker that requires one.
	ErrTrackerProjectRefMissing = errors.New("tracker_project_ref_missing")

	// ErrProviderUnsupported — providers.default.provider is not in the
	// SPEC §7.2 list.
	ErrProviderUnsupported = errors.New("provider_unsupported")

	// ErrProviderAPIKeyMissing — providers.default.api_key is empty for
	// a provider that requires one.
	ErrProviderAPIKeyMissing = errors.New("provider_api_key_missing")

	// ErrKnowledgeBackendUnsupported — knowledge.store_backend is not a
	// recognized backend.
	ErrKnowledgeBackendUnsupported = errors.New("knowledge_backend_unsupported")

	// ErrMemoryBackendUnsupported — memory.store_backend is not a
	// recognized backend.
	ErrMemoryBackendUnsupported = errors.New("memory_backend_unsupported")

	// ErrDocStoreBackendUnsupported — one of docs.stores[].backend is
	// not a recognized backend.
	ErrDocStoreBackendUnsupported = errors.New("doc_store_backend_unsupported")

	// ErrPipelineRoleMissingTemplate — a role named in routing.pipeline
	// or routing.rules[*].pipeline has no matching `## <role>` section in
	// HARNESS.md.
	ErrPipelineRoleMissingTemplate = errors.New("pipeline_role_missing_template")
)
