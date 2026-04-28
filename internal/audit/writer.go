package audit

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// Writer fans out audit events to one or more sinks. The Writer is the
// public entry point for higher tiers; downstream code never talks to a
// sink directly.
//
// Sinks can be added at any time. Write is safe for concurrent use; the
// orchestrator and per-turn workers all call it.
type Writer struct {
	mu    sync.RWMutex
	sinks []Sink
	log   zerolog.Logger
	now   func() time.Time
	id    func() string
}

// NewWriter constructs a Writer with no sinks attached. Add sinks via
// AddSink before the first Write. The logger is used to record sink
// failures so an unreachable webhook does not silently swallow events.
func NewWriter(log zerolog.Logger) *Writer {
	return &Writer{
		log: log.With().Str("subsystem", "audit").Logger(),
		now: time.Now,
		id:  newUUID,
	}
}

// AddSink registers a Sink for fan-out. Order is preserved; sinks are
// invoked in the order they were added.
func (w *Writer) AddSink(s Sink) {
	if s == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sinks = append(w.sinks, s)
}

// Write fills in missing identifiers (ID, Timestamp) and dispatches to
// every registered sink. Per-sink failures are logged but do not abort
// fan-out: a flaky sink should never silence the others.
//
// Returns errors.Join of every sink failure so callers that care can
// inspect them with errors.Is / errors.As.
func (w *Writer) Write(ctx context.Context, evt AuditEvent) error {
	if evt.ID == "" {
		evt.ID = w.id()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = w.now().UTC()
	}
	if evt.Payload == nil {
		evt.Payload = map[string]any{}
	}

	w.mu.RLock()
	sinks := append([]Sink(nil), w.sinks...)
	w.mu.RUnlock()

	if len(sinks) == 0 {
		return nil
	}

	var errs []error
	for _, s := range sinks {
		if err := s.Write(ctx, evt); err != nil {
			w.log.Error().
				Err(err).
				Str("event_id", evt.ID).
				Str("event_type", string(evt.EventType)).
				Msg("audit sink write failed")
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// Close closes every registered sink. Returns errors.Join of every Close
// failure.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var errs []error
	for _, s := range w.sinks {
		if err := s.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	w.sinks = nil
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// newUUID generates a RFC 4122 version 4 UUID. Using crypto/rand avoids
// pulling in a separate UUID library for the single use site we have.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is a system-level event we cannot
		// meaningfully recover from; fall back to a timestamp-tagged
		// pseudo-id so the audit pipeline never blocks on entropy.
		return fmt.Sprintf("audit-%d", time.Now().UnixNano())
	}
	// RFC 4122 §4.4: set version (4) and variant (10xx).
	b[6] = (b[6] & 0x0F) | 0x40
	b[8] = (b[8] & 0x3F) | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 36)
	for i, j := 0, 0; i < 16; i++ {
		if j == 8 || j == 13 || j == 18 || j == 23 {
			out[j] = '-'
			j++
		}
		out[j] = hex[b[i]>>4]
		out[j+1] = hex[b[i]&0x0F]
		j += 2
	}
	return string(out)
}
