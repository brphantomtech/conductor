## ADDED Requirements

### Requirement: Harness Rule Runner

The Harness Enforcer SHALL execute each configured `HarnessRule` (SPEC §4.1.8) by running its
`check` shell command in the workspace root under a timeout, classifying exit code 0 as a pass and
any non-zero exit as a violation at the rule's `severity` (`warning`, `error`, or `blocking`). Each
violation SHALL carry the rule's `fix_hint`, and a `HarnessViolationDetected` audit event SHALL be
emitted per violation.

#### Scenario: Failing check produces a severity-tagged violation

- **WHEN** a rule's `check` command exits non-zero
- **THEN** a violation is produced at the rule's severity with its `fix_hint` and a
  `HarnessViolationDetected` event is emitted

#### Scenario: Passing check produces no violation

- **WHEN** a rule's `check` command exits 0
- **THEN** no violation is produced for that rule

### Requirement: Pre-Dispatch Check

When `enforcement.drift_check_on_dispatch` is true, the enforcer SHALL run all rules before dispatch
and collect results by severity (SPEC §11.2): `warning` violations formatted as `## Known Technical
Debt`, `error` violations as `## Architectural Issues You Must Fix`. When a `blocking` violation
exists and `enforcement.blocking_violations_halt_dispatch` is true, the enforcer SHALL report
`enforcer_status = blocked`, dispatch SHALL be skipped for that tick, and a `HarnessEnforcerBlocked`
audit event SHALL be emitted. The enforcer SHALL be supplied through the orchestrator's pre-dispatch
seam.

#### Scenario: Blocking violation halts dispatch

- **WHEN** a blocking violation exists and `blocking_violations_halt_dispatch` is true
- **THEN** `enforcer_status` is `blocked`, no issue is dispatched that tick, and a
  `HarnessEnforcerBlocked` event is emitted

#### Scenario: Warning and error violations do not halt dispatch

- **WHEN** only warning and error violations exist
- **THEN** dispatch proceeds and the violations are formatted as the technical-debt and
  architectural-issues prompt sections

#### Scenario: Drift check disabled is a no-op

- **WHEN** `drift_check_on_dispatch` is false
- **THEN** the enforcer reports `clear` and does not run rules pre-dispatch

### Requirement: Scheduled Garbage Collection

When `enforcement.gc_schedule_cron` fires, the enforcer SHALL run all rules and, for each violation
of a rule with `auto_fix == true`, create a tracker issue (title `[GC] <name>: <summary>`,
description including `fix_hint` and affected files, labeled `enforcement.gc_issue_label`, in state
`enforcement.gc_issue_state`) only when no open GC issue already exists for the same `rule_id`. A
`GCTaskCreated` audit event SHALL be emitted per created issue.

#### Scenario: GC creates an issue for an auto-fix violation

- **WHEN** the GC schedule fires and an `auto_fix` rule has a violation with no existing GC issue
- **THEN** a tracker issue is created with the configured label and state and a `GCTaskCreated`
  event is emitted

#### Scenario: GC deduplicates against existing issues

- **WHEN** an open GC issue already exists for the same `rule_id`
- **THEN** no duplicate issue is created

### Requirement: Layer Violation Translation

The enforcer SHALL translate the Knowledge Engine's `CheckLayerViolations` output into
`dependency`-category HarnessRule violation records (SPEC §8.6, §11.4).

#### Scenario: Layer violation becomes a dependency-category violation

- **WHEN** the Knowledge Engine reports a layer violation
- **THEN** the enforcer produces a `dependency`-category violation record for it

### Requirement: On-Demand Harness Check CLI

The enforcer SHALL provide a `conductor harness check` subcommand that runs all rules on demand and
reports the violations grouped by severity.

#### Scenario: harness check reports violations

- **WHEN** `conductor harness check` is invoked against a workspace with failing rules
- **THEN** it reports the violations grouped by severity
