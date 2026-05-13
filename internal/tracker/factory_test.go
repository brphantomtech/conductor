package tracker

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestNew_Linear(t *testing.T) {
	// Linear adapter pings the viewer endpoint on construction; spin
	// up a fake server so the factory call resolves cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"viewer-test"}}}`))
	}))
	defer srv.Close()

	cfg := config.Tracker{
		Kind:        "linear",
		APIKey:      "lin_api_test",
		ProjectSlug: "p",
		Endpoint:    srv.URL,
	}
	a, err := New(cfg, WithHTTPClient(srv.Client()))
	require.NoError(t, err)
	require.NotNil(t, a)
	_, ok := a.(*linearAdapter)
	require.True(t, ok, "factory must return *linearAdapter for kind=linear")
}

func TestNew_GitHub(t *testing.T) {
	cfg := config.Tracker{
		Kind:      "github",
		APIKey:    "ghp_test",
		ProjectID: "owner/repo",
	}
	a, err := New(cfg)
	require.NoError(t, err)
	require.NotNil(t, a)
	_, ok := a.(*githubAdapter)
	require.True(t, ok, "factory must return *githubAdapter for kind=github")
}

func TestNew_DeferredKindsReturnUnsupported(t *testing.T) {
	cases := []string{"jira", "plane", "shortcut"}
	for _, k := range cases {
		t.Run(k, func(t *testing.T) {
			cfg := config.Tracker{Kind: k, APIKey: "k", ProjectID: "p"}
			a, err := New(cfg)
			require.Nil(t, a)
			require.True(t, errors.Is(err, ErrUnsupportedKind),
				"deferred kind %q must wrap ErrUnsupportedKind", k)
			require.Contains(t, err.Error(), k)
		})
	}
}

func TestNew_UnknownKindReturnsUnsupported(t *testing.T) {
	a, err := New(config.Tracker{Kind: "made-up-tracker", APIKey: "k"})
	require.Nil(t, a)
	require.True(t, errors.Is(err, ErrUnsupportedKind))
}
