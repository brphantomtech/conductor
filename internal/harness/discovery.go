package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// EnvHarnessPath is the canonical environment variable name SPEC §5.1
// uses to override the default discovery path.
const EnvHarnessPath = "CONDUCTOR_HARNESS_PATH"

// DefaultHarnessFilename is the file looked up in the current working
// directory when no flag and no environment override is set.
const DefaultHarnessFilename = "HARNESS.md"

// Source records which branch of the SPEC §5.1 precedence chain produced
// the resolved path. It is surfaced in audit context so operators can see
// at a glance whether the active harness came from the CLI, the
// environment, the working directory, or a Doc Store.
type Source string

// Discovery sources. The string values are stable identifiers that flow
// into log records and audit payloads.
const (
	SourceFlag     Source = "flag"
	SourceEnv      Source = "env"
	SourceCwd      Source = "cwd"
	SourceDocStore Source = "doc_store"
)

// EnvLookup is the function shape the resolver uses to read environment
// variables. It mirrors os.LookupEnv: ok=false means the variable is
// unset, ok=true with an empty value means the variable is set but empty.
type EnvLookup func(name string) (string, bool)

// Resolve picks the active HARNESS.md path using the SPEC §5.1 precedence:
//
//  1. flag        — the value of --harness (empty string = unused).
//  2. environment — CONDUCTOR_HARNESS_PATH.
//  3. cwd file    — HARNESS.md inside cwd.
//  4. doc store   — deferred to Phase 11; currently returns
//     ErrMissingHarnessFile so callers behave the same as if no source
//     produced a path.
//
// The first source whose file exists wins. A flag or env value that
// points at a non-existent file is a hard failure: returning an error
// rather than falling through to the next source means typos surface
// loudly instead of silently picking up a different harness.
//
// env may be nil — Resolve falls back to os.LookupEnv. cwd may also be
// empty; Resolve substitutes os.Getwd().
func Resolve(flag string, env EnvLookup, cwd string) (string, Source, error) {
	if env == nil {
		env = os.LookupEnv
	}
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", "", fmt.Errorf("harness: resolve cwd: %w", err)
		}
		cwd = wd
	}

	if flag != "" {
		if _, err := os.Stat(flag); err != nil {
			return "", "", classifyStatError(flag, err)
		}
		return flag, SourceFlag, nil
	}

	if envPath, ok := env(EnvHarnessPath); ok && envPath != "" {
		if _, err := os.Stat(envPath); err != nil {
			return "", "", classifyStatError(envPath, err)
		}
		return envPath, SourceEnv, nil
	}

	cwdPath := filepath.Join(cwd, DefaultHarnessFilename)
	if _, err := os.Stat(cwdPath); err == nil {
		return cwdPath, SourceCwd, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", "", classifyStatError(cwdPath, err)
	}

	// Phase 11 will resolve docs:// references against the Doc Store
	// Manager here. Until then the doc-store branch is structurally
	// present but always returns missing_harness_file so the rest of the
	// system already treats "nothing found" as a single error class.
	return "", SourceDocStore, fmt.Errorf("%w: tried flag, env, cwd, doc-store stub", ErrMissingHarnessFile)
}

// classifyStatError maps a Stat error to a sentinel-wrapped error.
// Non-existence wraps ErrMissingHarnessFile so callers can match it with
// errors.Is regardless of which discovery branch produced it.
func classifyStatError(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrMissingHarnessFile, path)
	}
	return fmt.Errorf("harness: stat %q: %w", path, err)
}
