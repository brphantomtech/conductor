package audit

import "strings"

// redactedValue is the placeholder substituted for any payload field whose key
// matches a known secret name or whose value matches a registered secret
// string (SPEC §21.1). It is a fixed marker so audit consumers can tell a
// redaction apart from a genuinely empty value.
const redactedValue = "[REDACTED]"

// secretKeySubstrings is the allowlist of payload-key fragments that mark a
// field as secret (SPEC §21.1: "redact known secret fields"). Matching is
// case-insensitive and substring-based so api_key, tracker_api_key, and
// X-Api-Key all redact while non-secret keys (path, key, repos) are preserved.
//
// "key" alone is deliberately NOT in the list: it collides with innocuous
// fields (workspace "key"). Only the compound secret-bearing forms are listed.
var secretKeySubstrings = []string{
	"api_key",
	"apikey",
	"secret",
	"token",
	"password",
	"passwd",
	"auth",
	"credential",
	"private_key",
}

// Redactor scrubs secret material from audit-event payloads before any sink
// persists them. It redacts on two axes (SPEC §21.1):
//
//   - known secret field names (api_key, token, password, …), and
//   - configuration-resolved secret values registered via AddSecret, so a
//     leaked literal token surfacing in an unexpected field is still removed.
//
// A zero Redactor (no registered secret values) still redacts by field name.
// Redactor is safe for read concurrent use after construction; AddSecret is
// expected to be called during wiring, before the first Write.
type Redactor struct {
	// secretValues holds the literal resolved secret strings to scrub from
	// any payload value, regardless of the field name carrying them.
	secretValues []string
}

// NewRedactor constructs a Redactor seeded with the given resolved secret
// values (for example, the tracker API key and provider keys after $VAR
// expansion). Empty strings are ignored so an unset optional secret does not
// cause every empty payload value to be redacted.
func NewRedactor(secretValues ...string) *Redactor {
	r := &Redactor{}
	for _, v := range secretValues {
		r.AddSecret(v)
	}
	return r
}

// AddSecret registers one resolved secret value to scrub from payload values.
// Empty or whitespace-only values are ignored.
func (r *Redactor) AddSecret(value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	r.secretValues = append(r.secretValues, value)
}

// Redact returns a deep-ish copy of evt with secret payload fields scrubbed.
// The original event is never mutated, so callers (and other sinks) keep the
// unredacted in-memory copy if they hold one. Non-secret fields are preserved
// verbatim, including nested maps and slices.
func (r *Redactor) Redact(evt AuditEvent) AuditEvent {
	if evt.Payload == nil {
		return evt
	}
	out := evt
	out.Payload = r.redactMap(evt.Payload)
	return out
}

// redactMap copies m, redacting any value whose key is a known secret field
// and recursively scrubbing nested containers.
func (r *Redactor) redactMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		if isSecretKey(k) {
			cp[k] = redactedValue
			continue
		}
		cp[k] = r.redactValue(v)
	}
	return cp
}

// redactValue recursively scrubs a payload value: nested maps and slices are
// walked, strings are checked against the registered secret values, and all
// other scalars pass through unchanged.
func (r *Redactor) redactValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return r.redactMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = r.redactValue(e)
		}
		return out
	case string:
		return r.redactString(t)
	default:
		return v
	}
}

// redactString replaces the whole value with the marker when it contains a
// registered secret. Whole-value replacement (rather than substring masking)
// keeps the rule simple and avoids leaking secret length or surrounding context.
func (r *Redactor) redactString(s string) string {
	for _, secret := range r.secretValues {
		if strings.Contains(s, secret) {
			return redactedValue
		}
	}
	return s
}

// isSecretKey reports whether a payload key names a known secret field. The
// match is case-insensitive substring containment against secretKeySubstrings.
func isSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, frag := range secretKeySubstrings {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}
