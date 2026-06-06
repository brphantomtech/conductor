package tools

// This file documents the ToolPlugin extension point (SPEC §20.5). ToolPlugin is
// a type alias for Tool (see tool.go) so a plugin and a built-in are the same
// type to the registry and dispatcher — a plugin executes through the exact same
// dispatch path as a built-in.
//
// Plugins are registered at startup via RegisterPlugin and advertised to all
// agent sessions alongside the built-ins (the registry's Specs include them).
// Dynamic loading (Go plugins / external processes) is deferred to Phase 18; the
// interface is the stable contract.

// RegisterPlugin registers a third-party ToolPlugin on the registry alongside
// the built-ins. It is Register with intent-revealing naming for plugin call
// sites; it returns ErrDuplicateTool when the plugin's name collides with an
// already-registered tool (including a built-in), so a plugin can never silently
// shadow a built-in.
func (r *Registry) RegisterPlugin(p ToolPlugin) error {
	return r.Register(p)
}
