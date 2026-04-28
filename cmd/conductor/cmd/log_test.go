package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLogger_DefaultLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log, err := NewLogger(LogOptions{Out: &buf})
	require.NoError(t, err)

	log.Info().Msg("hello")
	require.Contains(t, buf.String(), "hello")
	require.Contains(t, buf.String(), `"level":"info"`)
}

func TestNewLogger_DebugLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log, err := NewLogger(LogOptions{Level: "debug", Out: &buf})
	require.NoError(t, err)

	log.Debug().Msg("debug-line")
	require.Contains(t, buf.String(), "debug-line")
}

func TestNewLogger_FiltersBelowLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log, err := NewLogger(LogOptions{Level: "warn", Out: &buf})
	require.NoError(t, err)

	log.Info().Msg("info-suppressed")
	log.Warn().Msg("warn-emitted")
	out := buf.String()
	require.NotContains(t, out, "info-suppressed")
	require.Contains(t, out, "warn-emitted")
}

func TestNewLogger_UnknownLevelFails(t *testing.T) {
	t.Parallel()

	_, err := NewLogger(LogOptions{Level: "trace"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnknownLogLevel),
		"unknown level must surface as ErrUnknownLogLevel, got %v", err)
}

func TestNewLogger_UnknownFormatFails(t *testing.T) {
	t.Parallel()

	_, err := NewLogger(LogOptions{Format: "yaml"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnknownLogFormat))
}

func TestNewLogger_TextFormatProducesConsoleOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log, err := NewLogger(LogOptions{Format: "text", Out: &buf})
	require.NoError(t, err)

	log.Info().Str("k", "v").Msg("hello")
	out := buf.String()
	require.Contains(t, out, "hello")
	// Console writer is human-oriented (not JSON). It may emit ANSI color
	// codes between the key and value, so we assert on key+value presence
	// independently rather than matching `k=v` as a contiguous substring.
	require.NotContains(t, out, `"k":"v"`, "text format must not be JSON")
	require.Contains(t, out, "k=")
	require.Contains(t, out, "v")
}
