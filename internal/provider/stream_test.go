package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufferedStream_AggregatesText(t *testing.T) {
	s := newBufferedStream()
	go func() {
		s.emit(AgentEvent{Type: EventAssistantStart})
		s.emit(AgentEvent{Type: EventAssistantDelta, Text: "Hello"})
		s.emit(AgentEvent{Type: EventAssistantDelta, Text: " "})
		s.emit(AgentEvent{Type: EventAssistantDelta, Text: "world"})
		s.emit(AgentEvent{Type: EventTurnEnd})
		s.close()
	}()

	// Drain the events channel.
	collected := []EventType{}
	for ev := range s.Events() {
		collected = append(collected, ev.Type)
	}

	result := s.Wait()
	require.Equal(t, "Hello world", result.Text)
	require.Equal(t, []EventType{
		EventAssistantStart,
		EventAssistantDelta,
		EventAssistantDelta,
		EventAssistantDelta,
		EventTurnEnd,
	}, collected)
}

func TestBufferedStream_StoresToolCallsAndError(t *testing.T) {
	s := newBufferedStream()
	want := errors.New("boom")
	go func() {
		s.emit(AgentEvent{Type: EventToolCall, ToolCall: ToolCall{ID: "x", Name: "y"}})
		s.emit(AgentEvent{Type: EventError, Err: want})
		s.close()
	}()
	for range s.Events() {
	}
	r := s.Wait()
	require.Len(t, r.ToolCalls, 1)
	require.Equal(t, "x", r.ToolCalls[0].ID)
	require.ErrorIs(t, r.Err, want)
}

func TestBufferedStream_CloseIsIdempotent(t *testing.T) {
	s := newBufferedStream()
	s.close()
	s.close() // must not panic
	require.NotNil(t, s.Wait())
}

func TestTokenUsage_AddBasic(t *testing.T) {
	u := TokenUsage{}
	u.Add(TokenUsage{Prompt: 5, Completion: 10, Total: 15})
	u.Add(TokenUsage{Prompt: 1, Completion: 2})
	require.Equal(t, 6, u.Prompt)
	require.Equal(t, 12, u.Completion)
	// Second delta had Total=0; fallback computes Prompt+Completion.
	require.Equal(t, 18, u.Total)
}
