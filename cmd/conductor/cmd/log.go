package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// LogFormat selects between machine-friendly JSON and human-friendly console
// output. SPEC §19.2 specifies these two formats verbatim.
type LogFormat string

// LogFormat values supported by the SPEC §19.2 --log-format flag.
const (
	LogFormatJSON    LogFormat = "json"
	LogFormatConsole LogFormat = "text"
)

// ErrUnknownLogLevel is returned when --log-level does not name one of the
// SPEC §19.2 levels. Returning an error rather than silently defaulting
// surfaces typos before they hide in production.
var ErrUnknownLogLevel = errors.New("log: unknown level (expected debug, info, warn, error)")

// ErrUnknownLogFormat is returned when --log-format does not name one of
// the SPEC §19.2 formats.
var ErrUnknownLogFormat = errors.New("log: unknown format (expected json or text)")

// LogOptions controls logger construction. Both fields are validated:
// unknown values produce an error rather than silently defaulting.
type LogOptions struct {
	Level  string
	Format string
	Out    io.Writer // defaults to os.Stderr
}

// NewLogger builds a zerolog.Logger from CLI flags. The returned logger
// includes a UTC timestamp on every record.
func NewLogger(opts LogOptions) (zerolog.Logger, error) {
	level, err := parseLogLevel(opts.Level)
	if err != nil {
		return zerolog.Nop(), err
	}

	format, err := parseLogFormat(opts.Format)
	if err != nil {
		return zerolog.Nop(), err
	}

	out := opts.Out
	if out == nil {
		out = os.Stderr
	}

	var writer io.Writer = out
	if format == LogFormatConsole {
		writer = zerolog.ConsoleWriter{
			Out:        out,
			TimeFormat: time.RFC3339,
		}
	}

	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
	zerolog.TimeFieldFormat = time.RFC3339Nano

	return zerolog.New(writer).Level(level).With().Timestamp().Logger(), nil
}

func parseLogLevel(s string) (zerolog.Level, error) {
	if s == "" {
		return zerolog.InfoLevel, nil
	}
	switch strings.ToLower(s) {
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn", "warning":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	}
	return zerolog.NoLevel, fmt.Errorf("%w: %q", ErrUnknownLogLevel, s)
}

func parseLogFormat(s string) (LogFormat, error) {
	if s == "" {
		return LogFormatJSON, nil
	}
	switch strings.ToLower(s) {
	case string(LogFormatJSON):
		return LogFormatJSON, nil
	case string(LogFormatConsole), "console":
		return LogFormatConsole, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownLogFormat, s)
}
