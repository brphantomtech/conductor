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
