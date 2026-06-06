## ADDED Requirements

### Requirement: Check Execution and Classification

The Validation Pipeline SHALL execute each check in `validation.checks` in order, in the workspace
root, under a per-check timeout of `check.timeout_ms` (SPEC §15.2). It SHALL capture stdout, stderr,
and exit code, and classify each check as `passed` (exit 0), `failed` (non-zero exit), or `timeout`
(deadline exceeded). Captured output SHALL be truncated to `check.output_max_bytes`. A
`ValidationCheckRun` audit event SHALL be emitted per check.

#### Scenario: Passing check classified as passed

- **WHEN** a check command exits 0
- **THEN** its result is `passed` and a `ValidationCheckRun` event is emitted

#### Scenario: Failing check classified as failed

- **WHEN** a check command exits non-zero
- **THEN** its result is `failed` with captured output truncated to `output_max_bytes`

#### Scenario: Slow check classified as timeout

- **WHEN** a check command does not finish within `timeout_ms`
- **THEN** it is cancelled and its result is `timeout`

### Requirement: Per-Turn Result Persistence

The pipeline SHALL collect per-check results into the `RunAttempt.validation_results` slot (SPEC
§4.1.12) and persist them to `.conductor/validation/<turn_index>.json` in the workspace. The file
write SHALL be atomic.

#### Scenario: Results written per turn

- **WHEN** the pipeline runs for turn index N
- **THEN** `.conductor/validation/N.json` contains the per-check results for that turn

#### Scenario: Results populate the run attempt slot

- **WHEN** the pipeline runs during a dispatched turn
- **THEN** the run attempt's `validation_results` holds the same per-check results

### Requirement: Severity-Based Turn Failure

When `validation.fail_on_severity` is set, the pipeline SHALL mark the turn failed if any check at or
above the threshold is `failed` or `timeout`, with failure reason
`validation_pipeline_failed:<check_id>` (SPEC §15.3). The orchestrator SHALL treat this identically
to any other turn failure so retry and backoff apply.

#### Scenario: At-threshold failure fails the turn

- **WHEN** `fail_on_severity` is `error` and an `error`-severity check fails
- **THEN** the turn is marked failed with reason `validation_pipeline_failed:<check_id>`

#### Scenario: Below-threshold failure does not fail the turn

- **WHEN** `fail_on_severity` is `error` and only a `warning`-severity check fails
- **THEN** the turn is not marked failed by validation

#### Scenario: No threshold means no validation-driven failure

- **WHEN** `fail_on_severity` is unset
- **THEN** validation results never mark the turn failed

### Requirement: Context Injection Formatting

When `validation.inject_results_into_context` is true, the pipeline SHALL format the previous turn's
results as a `## Validation Results (Previous Turn)` block per SPEC §15.4, with a status indicator
per check and failure detail, truncated to the configured byte budget, suitable for prepending to
the next turn prompt.

#### Scenario: Formatted block lists check outcomes

- **WHEN** the formatter runs over a mixed pass/fail/timeout result set
- **THEN** it returns a `## Validation Results (Previous Turn)` block with one status line per check
  and failure detail for failed checks, truncated to the budget

### Requirement: Validation CLI

The pipeline SHALL provide a `conductor validation run` subcommand that executes the configured
checks against a workspace ad hoc and reports per-check results.

#### Scenario: Ad-hoc run reports results

- **WHEN** `conductor validation run` is invoked against a workspace with configured checks
- **THEN** it executes each check and reports each result with its classification
