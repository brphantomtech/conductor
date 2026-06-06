// Package tools implements Conductor's tool-injection layer (SPEC §7.3,
// §20.5, §21.1, §21.2). It advertises a fixed set of built-in tools to every
// agent session over the provider's native tool-use mechanism, dispatches an
// agent's tool calls to the real engines with Conductor-mediated credentials
// (the agent never sees a token), enforces per-role approval policies, and
// exposes a ToolPlugin interface for third-party tools.
//
// The package is built around three types:
//
//   - Tool — the SPEC §20.5 shape every built-in and plugin satisfies
//     (Name / Description / ParametersSchema / Execute).
//   - Registry — registers tools and advertises them as provider.ToolSpecs,
//     looked up by name at dispatch time.
//   - Dispatcher — routes an agent's tool call to the matching tool, applies
//     the approval gate, marshals params/result, and emits ToolCalled /
//     ToolResult audit events through the redacting writer.
//
// Each built-in tool depends on a narrow consumer-side engine interface
// (declared in this package) so the package stays decoupled from the concrete
// engines and is testable with fakes. No tool makes a live API call in tests.
package tools
