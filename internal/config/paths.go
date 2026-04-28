package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizePath resolves a path-typed config value as required by SPEC §6.2:
//
//   - "~" or "~/..." is expanded against the user's home directory.
//   - $VAR / ${VAR} indirection is expanded against lookup (defaulting to the
//     process environment when nil).
//   - The OS-specific separator is applied via filepath.FromSlash so that
//     POSIX-style "~/.conductor/foo" works on Windows.
//
// Empty input is returned unchanged. If $VAR expansion fails, the error
// wraps ErrUnsetVariable.
//
// NormalizePath does not call filepath.Abs; relative paths are preserved so
// the caller can decide whether they should be resolved against the
// workspace root, the harness directory, or some other anchor.
func NormalizePath(s string, lookup LookupFunc) (string, error) {
	if s == "" {
		return s, nil
	}

	expanded, err := Expand(s, lookup)
	if err != nil {
		return "", fmt.Errorf("config: normalize path %q: %w", s, err)
	}

	tilde, err := expandTilde(expanded)
	if err != nil {
		return "", err
	}

	return filepath.FromSlash(tilde), nil
}

// ErrTildeWithoutHome signals that os.UserHomeDir failed during ~ expansion;
// callers may surface it to the operator instead of silently returning a
// path that begins with "~".
var ErrTildeWithoutHome = errors.New("config: cannot expand ~ (no home directory)")

func expandTilde(s string) (string, error) {
	if s == "" || s[0] != '~' {
		return s, nil
	}
	// "~" alone or "~/..." or "~\..." — accept either separator since the
	// value may have been written for POSIX but is being consumed on Windows.
	if len(s) > 1 && s[1] != '/' && s[1] != '\\' {
		// "~user" form is not supported (SPEC §6.2 only mandates "~").
		return s, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrTildeWithoutHome, err)
	}

	rest := s[1:]
	rest = strings.TrimLeft(rest, "/\\")
	if rest == "" {
		return home, nil
	}
	return filepath.Join(home, rest), nil
}
