package harness

import (
	"os"

	"github.com/conductor-sh/conductor/internal/config"
)

// SPEC §5.1 — Discovery and Path Resolution.

// HarnessPathEnvVar is the environment variable consulted after the
// `--harness` CLI flag and before the working-directory default when
// resolving the HARNESS.md location.
const HarnessPathEnvVar = "CONDUCTOR_HARNESS_PATH"

// ResolvePath implements the SPEC §5.1 discovery precedence:
//
//  1. flagPath, when non-empty — the `--harness <path>` CLI flag.
//  2. $CONDUCTOR_HARNESS_PATH, when set to a non-empty value.
//  3. config.DefaultHarnessPath ("HARNESS.md") in the current directory.
//
// Doc Store resolution (precedence step 4) resolves a HARNESS.md stored in a
// configured backend; it is owned by the Doc Store Manager (Phase 11) and is
// intentionally not consulted here.
//
// lookupEnv defaults to os.LookupEnv; tests inject their own to avoid
// mutating process environment. ResolvePath performs discovery only — it does
// not check that the returned path exists. Callers that need a hard
// missing_harness_file failure use ParseFile or EnsureExists on the result.
func ResolvePath(flagPath string, lookupEnv func(string) (string, bool)) string {
	if flagPath != "" {
		return flagPath
	}
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	if v, ok := lookupEnv(HarnessPathEnvVar); ok && v != "" {
		return v
	}
	return config.DefaultHarnessPath
}
