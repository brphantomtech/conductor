package provider

import "sync"

// TurnStream is the per-turn streaming surface SPEC §7.1 specifies.
// Consumers read events until the channel closes and may call Wait to
// retrieve the aggregated TurnResult. The two methods are independent —
// a bulk consumer that only needs the final text can call Wait without
// draining Events; the events are buffered up to the channel capacity.
type TurnStream interface {
	// Events returns the channel of AgentEvents the adapter publishes
	// during the turn. The channel is closed when the turn terminates.
	Events() <-chan AgentEvent

	// Wait blocks until the turn terminates and returns the aggregated
	// result. Safe to call multiple times — subsequent calls return the
	// same value.
	Wait() TurnResult
}

// TurnResult is the aggregated outcome of a single turn. Adapters build
// it from the AgentEvent stream and hand it back via Wait.
type TurnResult struct {
	// Text is the concatenation of every EventAssistantDelta in the turn.
	Text string

	// ToolCalls is the ordered list of complete EventToolCall payloads.
	ToolCalls []ToolCall

	// Usage is the per-turn token delta the provider reported.
	Usage TokenUsage

	// Err is the terminal error (if any). Non-nil iff the stream emitted
	// an EventError before EventTurnEnd.
	Err error
}

// streamCapacity is the size of every adapter's event channel. Set small
// so a misbehaving consumer surfaces backpressure quickly; experiments
// with Symphony showed that 16 is enough for normal traffic.
const streamCapacity = 16

// bufferedStream is the concrete TurnStream every adapter uses. Adapters
// write to its channel via emit and signal completion via close.
type bufferedStream struct {
	events chan AgentEvent

	mu     sync.Mutex
	result TurnResult
	done   chan struct{}
}

// newBufferedStream constructs a stream with the default capacity.
func newBufferedStream() *bufferedStream {
	return &bufferedStream{
		events: make(chan AgentEvent, streamCapacity),
		done:   make(chan struct{}),
	}
}

// Events satisfies TurnStream.Events.
func (s *bufferedStream) Events() <-chan AgentEvent {
	return s.events
}

// Wait blocks until close was called and returns the aggregated result.
func (s *bufferedStream) Wait() TurnResult {
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.result
}

// emit publishes an event on the channel, blocking only if the consumer
// has fallen behind by streamCapacity events.
func (s *bufferedStream) emit(evt AgentEvent) {
	switch evt.Type {
	case EventAssistantDelta:
		s.mu.Lock()
		s.result.Text += evt.Text
		s.mu.Unlock()
	case EventToolCall:
		s.mu.Lock()
		s.result.ToolCalls = append(s.result.ToolCalls, evt.ToolCall)
		s.mu.Unlock()
	case EventUsage:
		s.mu.Lock()
		s.result.Usage = evt.Usage
		s.mu.Unlock()
	case EventError:
		s.mu.Lock()
		s.result.Err = evt.Err
		s.mu.Unlock()
	}
	s.events <- evt
}

// close marks the stream terminal: closes the events channel and unblocks
// any Wait callers. Idempotent — safe to call twice (the adapter's defer
// path may overlap with an error path that already closed).
func (s *bufferedStream) close() {
	select {
	case <-s.done:
		return
	default:
	}
	close(s.events)
	close(s.done)
}
