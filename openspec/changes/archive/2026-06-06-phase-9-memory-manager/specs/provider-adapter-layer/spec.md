## ADDED Requirements

### Requirement: Context budget compaction at 95%

When `ProviderConfig.ContextBudget` is greater than zero, the adapter SHALL apply the configured
`compaction_strategy` the first time cumulative session usage crosses 95% of the budget (SPEC §7.4),
completing the accounting and 80%-warning groundwork from Phase 3. For `summarize`, the adapter SHALL
invoke the provider with the SPEC §7.4 summarization prompt and restart the session context with the
summary as a system message, emitting a `ContextCompacted` audit event. For `sliding_window`, the
adapter SHALL drop the oldest message pairs while keeping the system message and the most recent
turns, emitting a `ContextSlid` event. For `none`, the adapter SHALL emit a `ContextLimitApproaching`
event and continue. The compaction target SHALL bring usage to approximately 60% of the budget. When
`ContextBudget` is zero, no compaction SHALL occur.

#### Scenario: Summarize strategy compacts and restarts context

- **WHEN** a session configured with `compaction_strategy: summarize` crosses 95% of its budget
- **THEN** the provider is invoked with the summarization prompt, the session context is restarted
  with the summary as a system message, and a `ContextCompacted` event is emitted

#### Scenario: Sliding window drops oldest pairs

- **WHEN** a session configured with `compaction_strategy: sliding_window` crosses 95% of its budget
- **THEN** the oldest message pairs are dropped, the system message and most recent turns are
  retained, and a `ContextSlid` event is emitted

#### Scenario: No compaction when budget is zero

- **WHEN** a session is configured with `ContextBudget: 0`
- **THEN** no compaction occurs and no compaction event is emitted
