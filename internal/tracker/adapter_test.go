package tracker

import (
	"net/http"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestDefaultOptions(t *testing.T) {
	o := defaultOptions()

	require.NotNil(t, o.httpClient, "default HTTP client must be non-nil")
	require.Equal(t, 30*time.Second, o.httpClient.Timeout,
		"default HTTP client must use the 30s tracker timeout")
	require.NotNil(t, o.clock, "default clock must be non-nil")
}

func TestApplyOptions_OverridesAndIgnoresNil(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	logger := zerolog.New(nil)
	fixedTime := time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return fixedTime }

	o := applyOptions([]Option{
		WithHTTPClient(custom),
		WithHTTPClient(nil), // must be ignored, custom must persist
		WithLogger(logger),
		WithClock(clock),
		WithClock(nil), // must be ignored, clock must persist
		nil,            // must be ignored
	})

	require.Same(t, custom, o.httpClient, "WithHTTPClient must replace default")
	require.Equal(t, fixedTime, o.clock(), "WithClock must replace default")
	// zerolog.Logger has no useful Equal helper; just confirm it accepted
	// the value without panicking by writing one event.
	o.logger.Info().Msg("test")
}
