package tracker

import (
	"bytes"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestValidate_LinearClean(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:           "linear",
		APIKey:         "lin_api_test",
		ProjectSlug:    "test-project",
		ActiveStates:   []string{"Todo", "In Progress"},
		TerminalStates: []string{"Done"},
	})
	require.NoError(t, err)
}

func TestValidate_GitHubClean(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:           "github",
		APIKey:         "ghp_test",
		ProjectID:      "owner/repo",
		ActiveStates:   []string{"open"},
		TerminalStates: []string{"closed"},
	})
	require.NoError(t, err)
}

func TestValidate_MissingAPIKey(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:         "linear",
		ProjectSlug:  "test-project",
		ActiveStates: []string{"Todo"},
	})
	require.True(t, errors.Is(err, ErrMissingAPIKey))
}

func TestValidate_UnsupportedKind(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:         "made-up-tracker",
		APIKey:       "k",
		ActiveStates: []string{"x"},
	})
	require.True(t, errors.Is(err, ErrUnsupportedKind))
}

func TestValidate_EmptyKind(t *testing.T) {
	err := Validate(config.Tracker{
		APIKey:       "k",
		ActiveStates: []string{"x"},
	})
	require.True(t, errors.Is(err, ErrUnsupportedKind))
}

func TestValidate_LinearMissingProjectSlug(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:         "linear",
		APIKey:       "k",
		ActiveStates: []string{"Todo"},
	})
	require.True(t, errors.Is(err, ErrMissingProjectID))
}

func TestValidate_GitHubMissingProjectID(t *testing.T) {
	err := Validate(config.Tracker{
		Kind:         "github",
		APIKey:       "k",
		ActiveStates: []string{"open"},
	})
	require.True(t, errors.Is(err, ErrMissingProjectID))
}

func TestValidate_EmptyActiveStates_WarnsButPasses(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	err := Validate(
		config.Tracker{
			Kind:        "linear",
			APIKey:      "k",
			ProjectSlug: "p",
			// ActiveStates intentionally empty
		},
		WithLogger(logger),
	)
	require.NoError(t, err, "empty active_states is a warning, not an error")
	require.Contains(t, buf.String(), "tracker.active_states_empty")
}

func TestValidate_JoinsAllErrors(t *testing.T) {
	// Multiple problems must surface in the joined error so the
	// operator sees them all in one shot.
	err := Validate(config.Tracker{
		Kind: "linear",
		// APIKey missing
		// ProjectSlug missing
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingAPIKey))
	require.True(t, errors.Is(err, ErrMissingProjectID))
}
