package tools

import (
	"fmt"
	"strings"
)

// stringParam reads a string parameter by key. ok is false when the key is
// absent or not a string.
func stringParam(params map[string]any, key string) (string, bool) {
	v, present := params[key]
	if !present {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// requiredString reads a required, non-empty string parameter. It returns an
// ErrInvalidParams-wrapped error naming the missing/blank key.
func requiredString(params map[string]any, key string) (string, error) {
	s, ok := stringParam(params, key)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("tools: missing required string param %q: %w", key, ErrInvalidParams)
	}
	return s, nil
}

// intParam reads an integer parameter. JSON numbers decode to float64, so both
// float64 and int are accepted. The fallback is returned when absent.
func intParam(params map[string]any, key string, fallback int) int {
	v, ok := params[key]
	if !ok {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return fallback
	}
}

// stringSliceParam reads a []string parameter. A single string is accepted as a
// one-element slice; missing/wrong-typed values yield nil.
func stringSliceParam(params map[string]any, key string) []string {
	v, ok := params[key]
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// mapParam reads a map[string]any parameter (e.g. tracker GraphQL variables).
// Missing/wrong-typed values yield nil.
func mapParam(params map[string]any, key string) map[string]any {
	v, ok := params[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}
