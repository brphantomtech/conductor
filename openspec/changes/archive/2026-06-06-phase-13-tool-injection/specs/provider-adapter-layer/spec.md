## ADDED Requirements

### Requirement: Tool-result continuation

The provider adapter SHALL support continuing an existing session with the results of one or more
tool calls (SPEC §7.1, §7.3), distinct from text continuation. The adapter SHALL render tool results
in the provider's native shape (Anthropic `tool_result` content blocks; OpenAI-compatible
`role: "tool"` messages) and return a `TurnStream` for the model's follow-up turn. Tool-result
continuation against a session that has not emitted a tool call SHALL return an error.

#### Scenario: Session continued with a tool result

- **WHEN** a session has emitted a tool call and the caller continues it with the corresponding tool result
- **THEN** the adapter sends the result in the provider's native format and returns a TurnStream for the model's next turn

#### Scenario: OpenAI-compatible adapters share the tool-result path

- **WHEN** an OpenAI, OpenRouter, or other OpenAI-compatible adapter continues a session with a tool result
- **THEN** the result is sent as a `role: "tool"` message via the shared OpenAI-compatible code path

#### Scenario: Tool-result continuation without a prior tool call errors

- **WHEN** tool-result continuation is called on a session that has not emitted a tool call
- **THEN** the adapter returns an error
