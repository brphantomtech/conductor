package harness

import (
	"github.com/spf13/pflag"

	"github.com/conductor-sh/conductor/internal/config"
)

// LoadOptions controls a single Load call. The zero value is usable: a
// nil Env falls back to os.LookupEnv, an empty Cwd falls back to
// os.Getwd, and a nil CLIFlags simply skips flag binding (useful for
// offline / test callers).
type LoadOptions struct {
	// Flag is the literal value of the --harness flag. Empty means
	// "unset, fall back to env / cwd".
	Flag string

	// Env is the environment lookup function. nil → os.LookupEnv.
	Env EnvLookup

	// Cwd overrides the working directory used for cwd-relative lookups.
	// Empty → os.Getwd. Tests pass a temp dir here.
	Cwd string

	// CLIFlags is the Cobra/pflag flag set whose values should win over
	// env vars and YAML when config.Load merges sources. nil → skipped.
	CLIFlags *pflag.FlagSet

	// ConfigLookup is the environment lookup function used by
	// config.Load when expanding `$VAR` references inside YAML values.
	// Distinct from Env so tests can isolate the two — production
	// callers leave both nil.
	ConfigLookup config.LookupFunc
}

// Result bundles everything Load produces. The Definition is the parsed
// HARNESS.md, the Config is the merged-and-expanded settings, and the
// Source tells the caller which discovery branch produced the file.
//
// All three fields are populated on success. On error, Definition and
// Source may still be populated (so callers like `harness validate` can
// pretty-print the path that failed) while Config is the zero value.
type Result struct {
	Definition *Definition
	Config     config.Config
	Source     Source
	Path       string
}

// Load implements the SPEC §5 + §6 end-to-end startup path:
//
//  1. Resolve the HARNESS.md path per §5.1.
//  2. Parse the file per §5.2.
//  3. Merge config (defaults → front matter → env → flags) per §6.1.
//  4. Validate per §6.4 + the role/template coverage rule.
//
// Each stage's failure short-circuits the next. The returned Result
// always reflects what stages did complete: a parse error returns the
// resolved path and Source so the CLI can name the file in its error
// output.
func Load(opts LoadOptions) (Result, error) {
	path, source, err := Resolve(opts.Flag, opts.Env, opts.Cwd)
	if err != nil {
		return Result{Source: source, Path: path}, err
	}

	def, err := Parse(path)
	if err != nil {
		return Result{Source: source, Path: path}, err
	}

	cfg, err := config.Load(config.LoadOptions{
		Flags:       opts.CLIFlags,
		FrontMatter: def.FrontMatter,
		Lookup:      opts.ConfigLookup,
	})
	if err != nil {
		return Result{Definition: def, Source: source, Path: path}, err
	}

	if err := Validate(def, cfg); err != nil {
		return Result{Definition: def, Config: cfg, Source: source, Path: path}, err
	}

	return Result{
		Definition: def,
		Config:     cfg,
		Source:     source,
		Path:       path,
	}, nil
}
