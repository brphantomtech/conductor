package memory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestAPIProviderEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		require.Equal(t, "Bearer key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	ap := NewAPIProvider(config.ProviderConfig{BaseURL: srv.URL + "/v1", APIKey: "key", Model: "m"}, srv.Client())
	vec, err := ap.Embed(context.Background(), "hello")
	require.NoError(t, err)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, vec)
}

func TestAPIProviderSynthesize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"the lesson"}}]}`))
	}))
	defer srv.Close()

	ap := NewAPIProvider(config.ProviderConfig{BaseURL: srv.URL + "/v1", Model: "m"}, srv.Client())
	got, err := ap.Synthesize(context.Background(), []string{"a", "b", "c"})
	require.NoError(t, err)
	require.Equal(t, "the lesson", got)
}

func TestAPIProviderEmbedHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	ap := NewAPIProvider(config.ProviderConfig{BaseURL: srv.URL + "/v1", Model: "m"}, srv.Client())
	_, err := ap.Embed(context.Background(), "hello")
	require.Error(t, err)
}
