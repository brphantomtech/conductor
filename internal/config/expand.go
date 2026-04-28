package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// LookupFunc returns the value of an environment variable. The second return
// distinguishes "set but empty" (true, "") from "unset" (false, ""). Callers
// rely on this distinction so SPEC §6.4 can fail on a missing tracker key
// even when the harness file contains `api_key: $LINEAR_API_KEY` and the
// variable is genuinely missing.
type LookupFunc func(name string) (value string, ok bool)

// OSLookup is the default LookupFunc. It delegates to os.LookupEnv.
func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// ErrUnsetVariable is returned by Expand when a referenced environment
// variable is unset. Callers that want to treat unset-as-empty should pass a
// LookupFunc that always returns ok=true.
var ErrUnsetVariable = errors.New("config: unset environment variable")

// Expand resolves $VAR and ${VAR} references in s against lookup. A bare
// dollar sign ("$") not followed by an identifier is preserved as-is. A
// `$$` sequence emits a single literal `$`.
//
// If a referenced variable is unset, Expand returns an error wrapping
// ErrUnsetVariable. Set-but-empty variables expand to "" and do not error.
//
// Expand is intentionally a single-pass walk; the result is not re-expanded
// against itself to keep the substitution model predictable.
func Expand(s string, lookup LookupFunc) (string, error) {
	if lookup == nil {
		lookup = OSLookup
	}
	if !strings.Contains(s, "$") {
		return s, nil
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}

		// $$ → literal $
		if i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}

		name, end, ok := readVarName(s, i+1)
		if !ok {
			// "$" not followed by an identifier or `{...}`: emit verbatim.
			b.WriteByte('$')
			i++
			continue
		}

		val, found := lookup(name)
		if !found {
			return "", fmt.Errorf("%w: %s", ErrUnsetVariable, name)
		}
		b.WriteString(val)
		i = end
	}

	return b.String(), nil
}

// readVarName reads an environment variable reference starting at s[start].
// On success it returns the variable name, the index just past the reference,
// and ok=true. The reference may be either bare ($FOO) or braced (${FOO}).
func readVarName(s string, start int) (name string, next int, ok bool) {
	if start >= len(s) {
		return "", 0, false
	}
	if s[start] == '{' {
		// ${NAME}
		closing := strings.IndexByte(s[start+1:], '}')
		if closing <= 0 {
			return "", 0, false
		}
		raw := s[start+1 : start+1+closing]
		if !validVarName(raw) {
			return "", 0, false
		}
		return raw, start + 1 + closing + 1, true
	}

	// $NAME (bare)
	end := start
	for end < len(s) && isVarChar(s[end], end == start) {
		end++
	}
	if end == start {
		return "", 0, false
	}
	return s[start:end], end, true
}

func validVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isVarChar(name[i], i == 0) {
			return false
		}
	}
	return true
}

func isVarChar(c byte, first bool) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c == '_':
		return true
	case !first && c >= '0' && c <= '9':
		return true
	}
	return false
}
