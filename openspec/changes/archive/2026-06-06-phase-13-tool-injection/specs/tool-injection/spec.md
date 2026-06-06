## ADDED Requirements

### Requirement: Tool Registry and Advertisement

Conductor SHALL provide a tool registry that advertises a fixed set of built-in tools to every agent
session as provider tool specifications (SPEC §7.3), injected via the provider's native tool-use
mechanism on session creation. The registry SHALL also advertise any registered plugins alongside
the built-ins.

#### Scenario: Built-in tools advertised on a session

- **WHEN** a session is created with the registry wired
- **THEN** the eight built-in tools are advertised to the model as tool specs with names, descriptions, and parameter schemas

#### Scenario: Registered plugin is advertised

- **WHEN** a plugin tool is registered before session creation
- **THEN** it is advertised alongside the built-in tools

### Requirement: Built-in Tools Backed by Engines

The registry SHALL provide the SPEC §7.3 built-in tools — `conductor_tracker_query`,
`conductor_tracker_mutate`, `conductor_knowledge_search`, `conductor_doc_search`,
`conductor_memory_read`, `conductor_memory_write`, `conductor_harness_check`, and
`conductor_validation_run` — each dispatching to its corresponding engine and returning a structured
result.

#### Scenario: Knowledge search tool returns engine results

- **WHEN** the agent calls `conductor_knowledge_search` with a query
- **THEN** the dispatcher invokes the Knowledge Engine and returns the ranked results to the model

#### Scenario: Memory write tool persists an entry

- **WHEN** the agent calls `conductor_memory_write` with a memory entry
- **THEN** the dispatcher writes it through the Memory Manager and returns confirmation

#### Scenario: Unknown tool name is rejected

- **WHEN** a tool call names a tool not in the registry
- **THEN** the dispatcher returns an error result identifying the unknown tool without affecting other calls

### Requirement: Tool-Call Execution Loop

When a turn emits one or more tool calls, the system SHALL execute each call through the dispatcher
and continue the same session with the tool results, repeating until the model stops calling tools
or a tool-turn cap is reached. A tool execution failure SHALL be returned to the model as an error
result rather than aborting the attempt. Each tool call SHALL emit a `ToolCalled` audit event and
each result a `ToolResult` audit event.

#### Scenario: Tool result continues the turn

- **WHEN** the model calls a tool and the dispatcher executes it
- **THEN** the session is continued with the tool result and the model proceeds

#### Scenario: Tool failure surfaces to the model

- **WHEN** a tool's execution returns an error
- **THEN** an error result is returned to the model and the attempt is not aborted

#### Scenario: Tool loop is bounded

- **WHEN** the model calls tools beyond the tool-turn cap
- **THEN** the loop stops and the turn ends with a diagnostic

### Requirement: Approval Policy Enforcement

The system SHALL enforce the role's `approval_policy` (SPEC §21.2): `auto` allows all tool calls;
`review_destructive` denies destructive tools (such as `conductor_tracker_mutate`); `manual` denies
all tool calls. A denied call SHALL return a structured "approval required" result rather than
executing or being silently skipped.

#### Scenario: review_destructive denies a destructive tool

- **WHEN** the policy is `review_destructive` and the agent calls `conductor_tracker_mutate`
- **THEN** the call is denied with an "approval required" result and no mutation is performed

#### Scenario: auto allows tool calls

- **WHEN** the policy is `auto`
- **THEN** tool calls execute without an approval gate

#### Scenario: manual denies all tool calls

- **WHEN** the policy is `manual` and the agent calls any tool
- **THEN** the call is denied with an "approval required" result

### Requirement: Credential Isolation for Tools

Tools SHALL resolve credentials from the execution context sourced from Conductor configuration, not
from agent-supplied parameters, so the agent never receives a raw credential (SPEC §21.1). Audit
payloads for tool calls and results SHALL have known secret fields redacted.

#### Scenario: Agent never receives credentials

- **WHEN** a tool that calls an authenticated service is dispatched
- **THEN** the credential is injected by Conductor and is not present in the tool parameters or result returned to the agent

#### Scenario: Audit redacts tool secrets

- **WHEN** a `ToolCalled` or `ToolResult` event is written
- **THEN** known secret fields in its payload are redacted

### Requirement: Tool Plugin Interface

Conductor SHALL define a `ToolPlugin` interface (`Name`, `Description`, `ParametersSchema`,
`Execute`) per SPEC §20.5 and a registration mechanism so plugin tools are advertised to all agent
sessions alongside the built-ins.

#### Scenario: Plugin executes like a built-in

- **WHEN** a registered plugin tool is called by the agent
- **THEN** the dispatcher routes the call to the plugin's `Execute` and returns its result like any built-in tool
