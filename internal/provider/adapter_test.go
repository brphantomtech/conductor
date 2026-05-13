package provider

import (
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// Compile-time conformance assertions for the three Phase-3 adapters.
var (
	_ Adapter = (*anthropicAdapter)(nil)
	_ Adapter = (*openaiAdapter)(nil)
	_ Adapter = (*openrouterAdapter)(nil)
)

func TestApplyOptions_AllOverridesApplied(t *testing.T) {
	clientSentinel := &http.Client{}
	clock := func() time.Time { return time.Unix(1700000000, 0) }
	log := zerolog.New(nil)

	opt := applyOptions([]Option{
		WithHTTPClient(clientSentinel),
		WithClock(clock),
		WithLogger(log),
	})

	require.Same(t, clientSentinel, opt.httpClient)
	require.Equal(t, time.Unix(1700000000, 0), opt.clock())
}

func TestApplyOptions_NilsAreIgnored(t *testing.T) {
	opt := applyOptions([]Option{
		WithHTTPClient(nil),
		WithClock(nil),
		nil,
	})
	require.NotNil(t, opt.httpClient)
	require.NotNil(t, opt.clock)
}

func TestSession_UniqueIDs(t *testing.T) {
	s1 := newSession("openai", "ws", nil)
	s2 := newSession("openai", "ws", nil)
	require.NotEqual(t, s1.ID(), s2.ID())
	require.Equal(t, "openai", s1.Provider())
	require.Equal(t, "ws", s1.Workspace())
}

func TestSession_MarkClosedAndPayload(t *testing.T) {
	s := newSession("openai", "ws", "payload")
	require.Equal(t, "payload", s.loadPayload())
	require.False(t, s.isClosed())
	s.markClosed()
	require.True(t, s.isClosed())
	require.Nil(t, s.loadPayload())
}
