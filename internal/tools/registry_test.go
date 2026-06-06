package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubTool is a minimal Tool used across the package's tests.
type stubTool struct {
	name   string
	desc   string
	schema json.RawMessage
	result ToolResult
	err    error
	calls  int
}

func (s *stubTool) Name() string                      { return s.name }
func (s *stubTool) Description() string               { return s.desc }
func (s *stubTool) ParametersSchema() json.RawMessage { return s.schema }

func (s *stubTool) Execute(_ context.Context, _ map[string]any, _ ExecutionContext) (ToolResult, error) {
	s.calls++
	return s.result, s.err
}

func newStub(name string) *stubTool {
	return &stubTool{
		name:   name,
		desc:   name + " description",
		schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
		result: ToolResult{Content: map[string]any{"ok": true}},
	}
}

func TestRegistry_AdvertisesSpecs(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newStub("conductor_a")))
	require.NoError(t, r.Register(newStub("conductor_b")))

	specs := r.Specs()
	require.Len(t, specs, 2)
	require.Equal(t, "conductor_a", specs[0].Name)
	require.Equal(t, "conductor_a description", specs[0].Description)
	require.JSONEq(t, `{"type":"object","properties":{"q":{"type":"string"}}}`, string(specs[0].Parameters))
	require.Equal(t, "conductor_b", specs[1].Name)
}

func TestRegistry_LookupByName(t *testing.T) {
	r := NewRegistry()
	a := newStub("conductor_a")
	require.NoError(t, r.Register(a))

	got, ok := r.Lookup("conductor_a")
	require.True(t, ok)
	require.Same(t, a, got)

	_, ok = r.Lookup("conductor_missing")
	require.False(t, ok)
}

func TestRegistry_DuplicateRegistrationErrors(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newStub("conductor_a")))
	err := r.Register(newStub("conductor_a"))
	require.ErrorIs(t, err, ErrDuplicateTool)
}

func TestRegistry_RejectsEmptyAndNil(t *testing.T) {
	r := NewRegistry()
	require.Error(t, r.Register(nil))
	require.Error(t, r.Register(newStub("")))
}

func TestRegistry_MustRegisterPanicsOnDuplicate(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(newStub("conductor_a"))
	require.Panics(t, func() { r.MustRegister(newStub("conductor_a")) })
}

func TestRegistry_NamesAndOrder(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(newStub("conductor_b")))
	require.NoError(t, r.Register(newStub("conductor_a")))
	require.Equal(t, []string{"conductor_b", "conductor_a"}, r.Names())
	require.Equal(t, []string{"conductor_a", "conductor_b"}, r.sortedNames())
	require.Equal(t, 2, r.Len())
}

// ensure ErrUnknownTool is wired (sanity for the sentinel set).
func TestSentinels_Distinct(t *testing.T) {
	require.False(t, errors.Is(ErrUnknownTool, ErrDuplicateTool))
}
