package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// samplePlugin is a third-party ToolPlugin used to exercise the plugin path.
type samplePlugin struct{ executed bool }

func (p *samplePlugin) Name() string        { return "acme_echo" }
func (p *samplePlugin) Description() string { return "Echo a message back." }
func (p *samplePlugin) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`)
}
func (p *samplePlugin) Execute(_ context.Context, params map[string]any, _ ExecutionContext) (ToolResult, error) {
	p.executed = true
	msg, _ := params["msg"].(string)
	return ToolResult{Content: map[string]any{"echo": msg}}, nil
}

func TestPlugin_RegisteredAndAdvertisedAlongsideBuiltins(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltins(Engines{})
	require.NoError(t, r.RegisterPlugin(&samplePlugin{}))

	require.Equal(t, 9, r.Len())
	names := r.Names()
	require.Contains(t, names, "acme_echo")

	// Advertised in the spec set with its schema.
	var found bool
	for _, s := range r.Specs() {
		if s.Name == "acme_echo" {
			found = true
			require.Contains(t, string(s.Parameters), "msg")
		}
	}
	require.True(t, found)
}

func TestPlugin_ExecutesViaDispatcherLikeBuiltin(t *testing.T) {
	plugin := &samplePlugin{}
	r := NewRegistry()
	require.NoError(t, r.RegisterPlugin(plugin))
	d := NewDispatcher(r)

	res := d.Dispatch(context.Background(),
		Call{Name: "acme_echo", Arguments: json.RawMessage(`{"msg":"hi"}`)},
		PolicyAuto, ExecutionContext{})

	require.False(t, res.IsError)
	require.True(t, plugin.executed)
	require.Equal(t, "hi", res.Content["echo"])
}

func TestPlugin_CannotShadowBuiltin(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltins(Engines{})
	// A plugin claiming a built-in name is rejected.
	shadow := &stubTool{name: ToolKnowledgeSearch, schema: json.RawMessage(`{}`)}
	require.ErrorIs(t, r.RegisterPlugin(shadow), ErrDuplicateTool)
}
