package tools

import (
	"fmt"
	"sort"

	"github.com/conductor-sh/conductor/internal/provider"
)

// Registry holds the set of tools advertised to every agent session — the
// built-ins plus any registered plugins (SPEC §7.3, §20.5). It advertises them
// as provider.ToolSpecs the router injects into StartTurn and looks them up by
// name at dispatch time. A Registry is constructed once at wiring time and is
// read-only thereafter; Register is expected before the first session.
type Registry struct {
	byName map[string]Tool
	order  []string
}

// NewRegistry constructs an empty Registry. Tools are added with Register or
// MustRegister; built-ins are added by RegisterBuiltins.
func NewRegistry() *Registry {
	return &Registry{byName: map[string]Tool{}}
}

// Register adds a tool to the registry. It returns ErrDuplicateTool when a tool
// with the same name is already registered, or an error when the tool or its
// name is empty.
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return fmt.Errorf("tools: register: nil tool")
	}
	name := t.Name()
	if name == "" {
		return fmt.Errorf("tools: register: empty tool name")
	}
	if _, exists := r.byName[name]; exists {
		return fmt.Errorf("tools: register %q: %w", name, ErrDuplicateTool)
	}
	r.byName[name] = t
	r.order = append(r.order, name)
	return nil
}

// MustRegister is Register that panics on error. It is used at wiring time
// where a duplicate registration is a programmer error (matches the
// construction-time panic convention in docs/conventions.md §2).
func (r *Registry) MustRegister(t Tool) {
	if err := r.Register(t); err != nil {
		panic(err)
	}
}

// Lookup returns the tool registered under name and whether it was found.
func (r *Registry) Lookup(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

// Names returns the registered tool names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Len returns the number of registered tools.
func (r *Registry) Len() int { return len(r.order) }

// Specs advertises every registered tool as a provider.ToolSpec, in
// registration order, for injection into StartTurn. The router passes the
// result straight through to the adapter's native tool payload.
func (r *Registry) Specs() []provider.ToolSpec {
	out := make([]provider.ToolSpec, 0, len(r.order))
	for _, name := range r.order {
		t := r.byName[name]
		out = append(out, provider.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.ParametersSchema(),
		})
	}
	return out
}

// sortedNames returns the registered names sorted lexically. Used by tests and
// diagnostics that need a deterministic order independent of registration.
func (r *Registry) sortedNames() []string {
	out := r.Names()
	sort.Strings(out)
	return out
}
