package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchRequiresRunningServiceText(t *testing.T) {
	var out bytes.Buffer
	err := runLiveOp(&out, "dispatch", "ABC-1", outputFormatText)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequiresRunningService))
	require.Contains(t, out.String(), "not performed")
	require.Contains(t, out.String(), "Phase 14")
}

func TestDispatchRequiresRunningServiceJSON(t *testing.T) {
	var out bytes.Buffer
	err := runLiveOp(&out, "dispatch", "ABC-1", outputFormatJSON)
	require.Error(t, err)

	var rep liveOpReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	require.Equal(t, "dispatch", rep.Operation)
	require.Equal(t, "ABC-1", rep.IssueID)
	require.False(t, rep.Performed)
	require.Contains(t, rep.Notice, "Phase 14")
}

func TestCancelRequiresRunningService(t *testing.T) {
	var out bytes.Buffer
	err := runLiveOp(&out, "cancel", "ABC-2", outputFormatText)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequiresRunningService))
	require.Contains(t, out.String(), "cancel ABC-2")
}

func TestLiveOpEmptyID(t *testing.T) {
	err := runLiveOp(&bytes.Buffer{}, "dispatch", "", outputFormatText)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrRequiresRunningService))
}

func TestLiveOpBadFormat(t *testing.T) {
	err := runLiveOp(&bytes.Buffer{}, "dispatch", "ABC-1", "yaml")
	require.Error(t, err)
}

func TestDispatchCancelCommandsRegistered(t *testing.T) {
	d := newDispatchCommand()
	require.Equal(t, "dispatch", d.Name())
	require.NotNil(t, d.Flags().Lookup("format"))

	c := newCancelCommand()
	require.Equal(t, "cancel", c.Name())
	require.NotNil(t, c.Flags().Lookup("format"))
}
