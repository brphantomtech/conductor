package provider

// TokenUsage is the per-session running total of prompt + completion
// tokens reported by the provider. Total is provider-reported when
// available and Prompt+Completion otherwise — adapters fill it from the
// provider's `usage` payload at turn end.
type TokenUsage struct {
	Prompt     int
	Completion int
	Total      int
}

// Add accumulates a per-turn delta into the running total. Adapters call
// it once per turn, after the final usage payload is observed.
func (u *TokenUsage) Add(delta TokenUsage) {
	u.Prompt += delta.Prompt
	u.Completion += delta.Completion
	if delta.Total > 0 {
		u.Total += delta.Total
	} else {
		u.Total += delta.Prompt + delta.Completion
	}
}
