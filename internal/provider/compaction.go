package provider

import (
	"context"

	"github.com/conductor-sh/conductor/internal/config"
)

// Compaction strategies (SPEC §4.1.3 / §7.4). The default is summarize.
const (
	compactionSummarize     = "summarize"
	compactionSlidingWindow = "sliding_window"
	compactionNone          = "none"
)

// summarizationPrompt is the fixed SPEC §7.4 summarization instruction sent
// to the provider under compaction_strategy: summarize.
const summarizationPrompt = "Summarize the key decisions, code changes, and open " +
	"questions from this conversation in under 500 words."

// compactionWarnThreshold and compactionTrigger are the SPEC §7.4 budget
// fractions (80% warning, 95% compaction). compactionTarget is the post-
// compaction usage goal (~60%).
const (
	compactionWarnThreshold = 80
	compactionTrigger       = 95
	compactionTarget        = 60
)

// shouldCompact reports whether cumulative usage has crossed 95% of the
// budget. Budget 0 means "no budget" — never compact.
func shouldCompact(budget int, usage TokenUsage) bool {
	if budget <= 0 {
		return false
	}
	return usage.Total*100 >= budget*compactionTrigger
}

// summarizer produces the SPEC §7.4 conversation summary. It is injectable so
// tests supply a deterministic summary without a live provider call; the
// production default issues a non-streaming provider request.
type summarizer interface {
	summarize(ctx context.Context, cfg config.ProviderConfig, transcript string) (string, error)
}

// compactor is the per-session compaction surface. Each adapter's session
// type implements it over its own message representation so the strategy
// decision in applyCompaction stays provider-agnostic.
type compactor interface {
	// transcript renders the current message history as plain text for the
	// summarization prompt.
	transcript() string
	// restartWithSummary replaces the entire message history with a single
	// system message carrying the summary (summarize strategy).
	restartWithSummary(summary string)
	// dropOldestPairs removes the oldest user/assistant pairs, keeping any
	// system message and the most recent keep turns (sliding_window).
	dropOldestPairs(keep int)
}

// targetUsage scales the running usage down to the compaction target (~60% of
// budget). It is an estimate: the next turn's provider-reported usage
// reconciles the real count.
func targetUsage(budget int, current TokenUsage) TokenUsage {
	if budget <= 0 {
		return current
	}
	scaled := budget * compactionTarget / 100
	if scaled >= current.Total {
		return current
	}
	return TokenUsage{Total: scaled}
}

// applyCompaction runs the configured strategy when usage crosses 95% of the
// budget. It returns the AgentEvent the caller should emit (with the
// post-compaction usage), the new usage to store on the session, and whether
// any compaction happened. A budget of 0, an already-compacted session, or a
// sub-threshold usage yields ok=false and no event.
func applyCompaction(
	ctx context.Context,
	cfg config.ProviderConfig,
	sum summarizer,
	c compactor,
	usage TokenUsage,
) (evt AgentEvent, newUsage TokenUsage, ok bool, err error) {
	if !shouldCompact(cfg.ContextBudget, usage) {
		return AgentEvent{}, usage, false, nil
	}

	strategy := cfg.CompactionStrategy
	if strategy == "" {
		strategy = compactionSummarize
	}

	switch strategy {
	case compactionSummarize:
		summary, serr := sum.summarize(ctx, cfg, c.transcript())
		if serr != nil {
			return AgentEvent{}, usage, false, serr
		}
		c.restartWithSummary(summary)
		nu := targetUsage(cfg.ContextBudget, usage)
		return AgentEvent{Type: EventContextCompacted, Usage: nu}, nu, true, nil

	case compactionSlidingWindow:
		c.dropOldestPairs(slidingKeepTurns)
		nu := targetUsage(cfg.ContextBudget, usage)
		return AgentEvent{Type: EventContextSlid, Usage: nu}, nu, true, nil

	case compactionNone:
		return AgentEvent{Type: EventContextLimitApproaching, Usage: usage}, usage, true, nil

	default:
		// Unknown strategy: behave like summarize (the SPEC default) so an
		// unexpected value never silently disables compaction.
		summary, serr := sum.summarize(ctx, cfg, c.transcript())
		if serr != nil {
			return AgentEvent{}, usage, false, serr
		}
		c.restartWithSummary(summary)
		nu := targetUsage(cfg.ContextBudget, usage)
		return AgentEvent{Type: EventContextCompacted, Usage: nu}, nu, true, nil
	}
}

// slidingKeepTurns is how many of the most recent user/assistant turns the
// sliding_window strategy retains. Kept small so the dropped prefix brings
// usage well under the ~60% target, leaving headroom for the next turn.
const slidingKeepTurns = 2
