package validation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// contextHeading is the SPEC §16.1 step 2 section title the formatter emits so
// the router can insert validation results at the correct prompt-assembly slot.
const contextHeading = "## Validation Results (Previous Turn)"

// statusGlyph maps a check status to its SPEC §15.4 display glyph.
func statusGlyph(s Status) string {
	switch s {
	case StatusPassed:
		return "✅"
	case StatusFailed:
		return "❌"
	case StatusTimeout:
		return "⚠️"
	default:
		return "❔"
	}
}

// FormatContext renders the SPEC §15.4 "## Validation Results (Previous Turn)"
// block for prepending to the next turn prompt: one status line per check
// followed by a failure-detail section for each failed or timed-out check, all
// truncated to maxBytes. It is a pure function so the Phase 7 router can call it
// at the prompt-assembly insertion point without this package touching the turn
// loop. An empty result set returns "" so the section is omitted (SPEC §16.1).
func FormatContext(results []ValidationResult, maxBytes int) string {
	if len(results) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(contextHeading)
	b.WriteString("\n\n")
	for _, r := range results {
		fmt.Fprintf(&b, "%s %s — %s\n", statusGlyph(r.Status), checkLabel(r), r.Status)
	}

	for _, r := range results {
		if r.Status != StatusFailed && r.Status != StatusTimeout {
			continue
		}
		detail := strings.TrimSpace(r.Output)
		if detail == "" {
			continue
		}
		fmt.Fprintf(&b, "\n### %s failures\n\n", checkLabel(r))
		b.WriteString(detail)
		b.WriteString("\n")
	}

	return truncateBytes(b.String(), maxBytes)
}

// checkLabel prefers the human-readable name, falling back to the check id.
func checkLabel(r ValidationResult) string {
	if r.Name != "" {
		return r.Name
	}
	return r.CheckID
}

// FormatContext renders the pipeline result for next-turn injection. maxBytes
// of 0 or below applies no byte budget.
func (r ValidationPipelineResult) FormatContext(maxBytes int) string {
	return FormatContext(r.Checks, maxBytes)
}

// truncateBytes bounds s to a byte budget, appending a SPEC §15.4 marker when
// it cuts. A non-positive budget means no limit. The cut is rune-aligned so a
// multi-byte glyph is never split.
func truncateBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const marker = "\n[... truncated to %d bytes]"
	suffix := fmt.Sprintf(marker, maxBytes)
	budget := maxBytes - len(suffix)
	if budget < 0 {
		// Budget too small to fit the marker; hard-cut to maxBytes with no
		// marker so the byte limit is always honored.
		suffix = ""
		budget = maxBytes
	}
	cut := runeAlignedCut(s, budget)
	return cut + suffix
}

// runeAlignedCut returns the longest prefix of s that is at most n bytes and
// ends on a UTF-8 rune boundary.
func runeAlignedCut(s string, n int) string {
	if n >= len(s) {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
