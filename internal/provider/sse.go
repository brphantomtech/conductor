package provider

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// sseFrame is one logical Server-Sent Events frame. The Event field is
// the optional `event:` line (empty for OpenAI's unnamed frames); Data
// is the concatenation of any `data:` lines in the frame, joined by
// newlines per the SSE spec.
type sseFrame struct {
	Event string
	Data  string
}

// maxSSELineBytes caps any single line in a frame at 1 MB. Anthropic
// content blocks can be long; tool-call argument JSON has been observed
// up to ~250 KB in practice. 1 MB leaves headroom without enabling
// pathological memory growth on a misbehaving server.
const maxSSELineBytes = 1 << 20

// sseReader wraps an io.Reader and yields sseFrames terminated by blank
// lines. It honours context cancellation between frames.
type sseReader struct {
	scanner *bufio.Scanner
	closer  io.Closer
}

// newSSEReader wraps an io.ReadCloser so callers can Close it once the
// turn finishes. The underlying Scanner uses a custom buffer to grow up
// to maxSSELineBytes per line.
func newSSEReader(rc io.ReadCloser) *sseReader {
	s := bufio.NewScanner(rc)
	s.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	return &sseReader{scanner: s, closer: rc}
}

// Close releases the underlying ReadCloser.
func (r *sseReader) Close() error {
	if r.closer == nil {
		return nil
	}
	if err := r.closer.Close(); err != nil {
		return fmt.Errorf("provider: sse close: %w", err)
	}
	return nil
}

// Next reads the next frame. Returns io.EOF on a clean stream end. On
// context cancellation, returns ctx.Err() wrapped via ErrResponseTimeout
// when the cause is DeadlineExceeded or Canceled.
func (r *sseReader) Next(ctx context.Context) (sseFrame, error) {
	if err := ctx.Err(); err != nil {
		return sseFrame{}, wrapContextErr(err)
	}

	var (
		frame sseFrame
		dataB strings.Builder
		any   bool
	)

	for r.scanner.Scan() {
		line := r.scanner.Text()
		if line == "" {
			// Blank line — frame terminator. Only emit if we accumulated
			// data (consecutive blank lines collapse to one separator).
			if any {
				frame.Data = dataB.String()
				return frame, nil
			}
			continue
		}

		any = true

		// Comment line — ignore (`:keep-alive`).
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, ok := splitSSEField(line)
		if !ok {
			// Malformed line — no colon. Per the spec we should treat
			// the whole line as the field name with empty value; we
			// treat that as a stream-level fault since real providers
			// never emit such frames.
			return sseFrame{}, fmt.Errorf(
				"provider: sse: malformed frame line %q: %w",
				truncate(line, 80), ErrStreamError,
			)
		}

		switch field {
		case "event":
			frame.Event = value
		case "data":
			if dataB.Len() > 0 {
				dataB.WriteByte('\n')
			}
			dataB.WriteString(value)
		default:
			// `id:`, `retry:`, anything else — ignore.
		}
	}

	if err := r.scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return sseFrame{}, fmt.Errorf("provider: sse: line exceeds %d bytes: %w", maxSSELineBytes, ErrStreamError)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return sseFrame{}, wrapContextErr(err)
		}
		return sseFrame{}, fmt.Errorf("provider: sse read: %w", joinErrs(err, ErrRequestFailed))
	}

	if any {
		// Final frame without a trailing blank line.
		frame.Data = dataB.String()
		return frame, nil
	}

	return sseFrame{}, io.EOF
}

// splitSSEField splits a `field: value` line into its two parts. A
// leading space after the colon is consumed per the SSE spec.
func splitSSEField(line string) (field, value string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	field = line[:idx]
	value = line[idx+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return field, value, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
