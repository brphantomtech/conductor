package audit_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
)

func TestRedactor_RedactsSecretFields(t *testing.T) {
	t.Parallel()

	r := audit.NewRedactor()
	evt := audit.AuditEvent{
		EventType: audit.EventToolCalled,
		Payload: map[string]any{
			"api_key":  "sk-live-123",
			"token":    "ghp_abcdef",
			"password": "hunter2",
			"path":     "/work/ABC-1",
			"key":      "ABC-1",
			"repos":    3,
		},
	}

	out := r.Redact(evt)

	require.Equal(t, "[REDACTED]", out.Payload["api_key"])
	require.Equal(t, "[REDACTED]", out.Payload["token"])
	require.Equal(t, "[REDACTED]", out.Payload["password"])
	// Non-secret fields are preserved verbatim, including the innocuous "key".
	require.Equal(t, "/work/ABC-1", out.Payload["path"])
	require.Equal(t, "ABC-1", out.Payload["key"])
	require.Equal(t, 3, out.Payload["repos"])
}

func TestRedactor_DoesNotMutateOriginal(t *testing.T) {
	t.Parallel()

	r := audit.NewRedactor()
	evt := audit.AuditEvent{
		Payload: map[string]any{"api_key": "sk-secret"},
	}
	_ = r.Redact(evt)
	require.Equal(t, "sk-secret", evt.Payload["api_key"], "original payload must be untouched")
}

func TestRedactor_RedactsNestedAndSliced(t *testing.T) {
	t.Parallel()

	r := audit.NewRedactor()
	evt := audit.AuditEvent{
		Payload: map[string]any{
			"auth": map[string]any{"token": "abc"},
			"providers": []any{
				map[string]any{"api_key": "k1", "model": "gpt"},
			},
		},
	}

	out := r.Redact(evt)

	// "auth" is itself a secret key → whole subtree redacted.
	require.Equal(t, "[REDACTED]", out.Payload["auth"])
	providers := out.Payload["providers"].([]any)
	first := providers[0].(map[string]any)
	require.Equal(t, "[REDACTED]", first["api_key"])
	require.Equal(t, "gpt", first["model"])
}

func TestRedactor_RedactsResolvedSecretValues(t *testing.T) {
	t.Parallel()

	// A config-resolved secret value leaking into a non-secret field is still
	// scrubbed by value match.
	r := audit.NewRedactor("super-secret-value")
	evt := audit.AuditEvent{
		Payload: map[string]any{
			"message": "calling api with super-secret-value embedded",
			"safe":    "nothing here",
		},
	}

	out := r.Redact(evt)
	require.Equal(t, "[REDACTED]", out.Payload["message"])
	require.Equal(t, "nothing here", out.Payload["safe"])
}

func TestRedactor_IgnoresEmptySecretValues(t *testing.T) {
	t.Parallel()

	r := audit.NewRedactor("", "   ")
	evt := audit.AuditEvent{Payload: map[string]any{"msg": ""}}
	out := r.Redact(evt)
	require.Equal(t, "", out.Payload["msg"], "empty registered secret must not redact every empty value")
}

// TestWriter_RedactsBeforeSinks asserts redaction happens at the writer
// boundary so every sink — here a JSONL file sink — persists the scrubbed
// payload.
func TestWriter_RedactsBeforeSinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	sink, err := audit.NewJSONLSink(path)
	require.NoError(t, err)

	w := audit.NewWriter(zerolog.Nop(), audit.WithRedactor(audit.NewRedactor("resolved-key")))
	w.AddSink(sink)
	capture := &fakeSink{}
	w.AddSink(capture)

	require.NoError(t, w.Write(context.Background(), audit.AuditEvent{
		ProjectID: "proj",
		EventType: audit.EventToolCalled,
		Payload: map[string]any{
			"api_key": "sk-live",
			"detail":  "uses resolved-key inside",
			"path":    "/safe",
		},
	}))
	require.NoError(t, w.Close())

	// JSONL sink got the redacted copy.
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	sc := bufio.NewScanner(f)
	require.True(t, sc.Scan())
	var persisted audit.AuditEvent
	require.NoError(t, json.Unmarshal(sc.Bytes(), &persisted))
	require.Equal(t, "[REDACTED]", persisted.Payload["api_key"])
	require.Equal(t, "[REDACTED]", persisted.Payload["detail"])
	require.Equal(t, "/safe", persisted.Payload["path"])

	// The in-memory fake sink saw the same redacted event (uniform across sinks).
	require.Len(t, capture.events, 1)
	require.Equal(t, "[REDACTED]", capture.events[0].Payload["api_key"])
}
