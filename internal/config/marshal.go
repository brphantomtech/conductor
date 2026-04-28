package config

import (
	"encoding/json"
	"fmt"
)

// structToMap converts a Config (or any struct) into a generic
// map[string]any whose keys match the mapstructure tags. Viper's
// MergeConfigMap consumes such maps directly.
//
// We detour through encoding/json — which respects the same struct tag
// surface area Viper expects via mapstructure — instead of pulling in the
// full mapstructure dependency just for this conversion. The yaml/mapstructure
// tag names on Config are aligned to the JSON-exported names by convention.
func structToMap(v any) map[string]any {
	// Marshal/Unmarshal through JSON so all nested struct fields collapse to
	// plain map[string]any without any special encoder hooks.
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("config: marshal defaults to json: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("config: unmarshal defaults from json: %v", err))
	}
	return lowerKeysToSnake(out)
}

// lowerKeysToSnake walks the map and rewrites Go-style keys (PascalCase) into
// the snake_case form used by SPEC §5.3 / Viper. JSON marshalling preserves
// the field names verbatim because Config has no `json:"..."` tags; this
// helper bridges the two conventions in one place rather than scattering
// per-field tag annotations.
func lowerKeysToSnake(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nk := pascalToSnake(k)
		switch t := v.(type) {
		case map[string]any:
			out[nk] = lowerKeysToSnake(t)
		case []any:
			out[nk] = lowerSliceKeys(t)
		default:
			out[nk] = v
		}
	}
	return out
}

func lowerSliceKeys(s []any) []any {
	for i, v := range s {
		if m, ok := v.(map[string]any); ok {
			s[i] = lowerKeysToSnake(m)
		}
	}
	return s
}

// pascalToSnake converts "MaxConcurrentAgentsByState" to
// "max_concurrent_agents_by_state". Acronyms run together (URL → url, ID →
// id) which matches SPEC §5.3 keys (`project_id`, `qdrant_url`).
func pascalToSnake(s string) string {
	if s == "" {
		return s
	}
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			isAcronymRun := i > 0 &&
				((s[i-1] >= 'A' && s[i-1] <= 'Z') ||
					(s[i-1] >= '0' && s[i-1] <= '9'))
			if i > 0 && !isAcronymRun {
				b = append(b, '_')
			}
			// Lookahead: if the next char is lowercase, this acronym run is
			// ending and we want a separator before the current char.
			if isAcronymRun && i+1 < len(s) && s[i+1] >= 'a' && s[i+1] <= 'z' {
				b = append(b, '_')
			}
			b = append(b, c+('a'-'A'))
			continue
		}
		b = append(b, c)
	}
	return string(b)
}
