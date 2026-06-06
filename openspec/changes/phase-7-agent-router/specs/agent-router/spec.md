## ADDED Requirements

### Requirement: Issue Classification

On first dispatch of an unclassified issue, the router SHALL invoke the `default` provider with the
SPEC §12.2 classification prompt, set `issue.task_type` to exactly one of `feature`, `bug`,
`refactor`, `investigation`, `docs`, `gc_task`, or `unknown`, and record a classification audit
event. Classification SHALL run at most once per issue; an already-classified issue SHALL NOT be
re-classified. The router SHALL supply this behavior through the orchestrator's classification seam.

#### Scenario: Unclassified issue is classified once

- **WHEN** an unclassified issue is dispatched
- **THEN** the provider is invoked with the classification prompt, `issue.task_type` is set to a
  valid task type, and a classification audit event is recorded

#### Scenario: Already-classified issue is not re-classified

- **WHEN** an issue that already has a `task_type` is dispatched
- **THEN** no classification provider call is made

### Requirement: Pipeline Selection

The router SHALL select an execution pipeline by evaluating `routing.rules` in order against the
issue (matching on labels, any_label, task_type, state, title regex, and complexity as a conjunction
of the conditions present), using the first matching rule's pipeline. When no rule matches, the
router SHALL use `routing.pipeline`.

#### Scenario: First matching rule wins

- **WHEN** multiple routing rules match an issue
- **THEN** the pipeline of the first matching rule (in declared order) is used

#### Scenario: Fallback to default pipeline

- **WHEN** no routing rule matches an issue
- **THEN** the `routing.pipeline` default is used

### Requirement: Pipeline Execution

For each role in the selected pipeline, in order, the router SHALL resolve the role's
`ProviderConfig` (falling back to `providers.default`), build the turn prompt, execute the turn via
the provider adapter, run the Validation Pipeline when enabled, and on success capture the role's
output as a `## Output from Previous Role (<role>)` section passed to the next role before advancing
`pipeline_index`. A `PipelineRoleStarted` and a `PipelineRoleEnded` audit event SHALL be written per
role. When a role's turn fails, the attempt SHALL fail so the orchestrator's retry and backoff apply.

#### Scenario: Roles execute in order with output hand-off

- **WHEN** a two-role pipeline runs and the first role succeeds
- **THEN** the second role's prompt includes the first role's output as
  `## Output from Previous Role (<role>)` and `pipeline_index` advances

#### Scenario: Per-role provider resolution

- **WHEN** a role has its own `ProviderConfig` and another role has none
- **THEN** the first role uses its own config and the second uses `providers.default`

#### Scenario: Failed role fails the attempt

- **WHEN** a role's turn fails
- **THEN** the attempt fails and the orchestrator applies retry/backoff

### Requirement: Continuation Handling

After the last role completes, the router SHALL re-fetch the issue state from the tracker. If the
issue is still in an active state and `turn_count < max_turns`, the router SHALL start another
pipeline iteration using continuation prompt templates (the HARNESS `## continuation` section if
present, otherwise the built-in continuation prompt). If the issue left the active state or
`turn_count >= max_turns`, the router SHALL exit. The full original task prompt SHALL NOT be re-sent
on continuation turns.

#### Scenario: Continuation while active and under the turn cap

- **WHEN** the pipeline completes, the issue is still active, and `turn_count < max_turns`
- **THEN** another iteration runs using the continuation prompt, without re-sending the original
  prompt

#### Scenario: Exit at the turn cap

- **WHEN** the pipeline completes and `turn_count >= max_turns`
- **THEN** the router exits without another iteration

### Requirement: Prompt Construction

The router SHALL assemble each turn prompt in the SPEC §16.1 order: role template (Liquid-rendered
with the SPEC §16.2 variables including `pipeline`, `pipeline_index`, `pipeline_length`,
`agent_role`, and `attempt`), then validation results, previous-role output, codebase context,
documentation, memory, and harness-violation sections. Sections whose source is disabled or empty
SHALL be omitted. If the assembled prompt exceeds `context_budget * 0.7`, it SHALL be truncated from
the bottom (the harness-violation sections first). An unknown Liquid variable or filter SHALL produce
a `template_render_error` that fails the attempt.

#### Scenario: Sections assembled in order, absent ones omitted

- **WHEN** a prompt is built with only the role template and previous-role output available
- **THEN** the result contains the rendered role template followed by the previous-role output, and
  omits the knowledge, docs, memory, and harness sections

#### Scenario: Over-budget prompt is truncated bottom-up

- **WHEN** the assembled prompt exceeds `context_budget * 0.7`
- **THEN** the lowest-priority sections are dropped first while the role template is retained

#### Scenario: Unknown variable fails the attempt

- **WHEN** a role template references an unknown Liquid variable
- **THEN** a `template_render_error` is produced and the run attempt fails
