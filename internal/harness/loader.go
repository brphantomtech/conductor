package harness

import (
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// LoadResult bundles everything a hot-reload consumer needs after a single
// load cycle: the parsed Definition, the typed Config decoded from its
// front matter, and the structured ValidationResult.
//
// Two consumers read this:
//   - The CLI's `conductor harness validate` subcommand renders Result.Issues.
//   - The Watcher publishes Result via callback so the orchestrator can
//     swap in the new Config and PromptTemplates atomically.
type LoadResult struct {
	Definition *Definition
	Config     config.Config
	Validation ValidationResult
}

// Load is the high-level entry point: parse HARNESS.md from path, decode
// its front matter into the typed Config (with $VAR expansion and the
// SPEC §6.1 precedence chain applied), and run Validate.
//
// Load returns LoadResult even on validation failure so the caller can
// inspect Issues — only parser/config-decode errors short-circuit.
//
// Errors classified per SPEC §23.1 and §23.x via wrapped sentinels:
//   - ErrMissingHarnessFile, ErrHarnessParse, ErrHarnessFrontMatterShape
//     come from ParseFile.
//   - config.ErrUnsetVariable comes from $VAR expansion when an env-var
//     reference resolves to nothing.
//   - ErrTemplateParse, ErrTemplateRender come from Validate.
func Load(path string, opts config.LoadOptions) (LoadResult, error) {
	def, err := ParseFile(path)
	if err != nil {
		return LoadResult{}, err
	}

	opts.FrontMatter = def.FrontMatter
	cfg, err := config.Load(opts)
	if err != nil {
		return LoadResult{Definition: def}, fmt.Errorf("harness: load config from front matter: %w", err)
	}

	res, vErr := Validate(def, cfg)
	out := LoadResult{
		Definition: def,
		Config:     cfg,
		Validation: res,
	}
	if vErr != nil {
		return out, vErr
	}
	return out, nil
}

// LoadBytes is the in-memory equivalent of Load, used by the watcher when a
// reload event delivers content via fsnotify (the fsnotify event already
// triggered a re-read; LoadBytes lets tests pass content directly).
//
// Source on the returned Definition is left as the empty string; callers
// can populate it themselves if they need a path for diagnostic messages.
func LoadBytes(data []byte, opts config.LoadOptions) (LoadResult, error) {
	def, err := ParseBytes(data)
	if err != nil {
		return LoadResult{}, err
	}

	opts.FrontMatter = def.FrontMatter
	cfg, err := config.Load(opts)
	if err != nil {
		return LoadResult{Definition: def}, fmt.Errorf("harness: load config from front matter: %w", err)
	}

	res, vErr := Validate(def, cfg)
	out := LoadResult{
		Definition: def,
		Config:     cfg,
		Validation: res,
	}
	if vErr != nil {
		return out, vErr
	}
	return out, nil
}
