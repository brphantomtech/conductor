package orchestrator

import "time"

// continuationDelay is the fixed retry delay after a clean worker exit
// (SPEC §13.4 continuation retry).
const continuationDelay = 1000 * time.Millisecond

// failureBackoffBase is the base unit of the failure-driven exponential
// backoff (SPEC §13.4).
const failureBackoffBase = 10000 * time.Millisecond

// failureBackoff computes the failure-driven retry delay for the given
// attempt number (1-based): min(10000 * 2^(attempt-1), maxBackoff)
// (SPEC §13.4). A non-positive attempt is treated as the first attempt; a
// non-positive maxBackoff disables the cap.
func failureBackoff(attempt int, maxBackoff time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Compute 10000ms * 2^(attempt-1) with overflow-safe doubling so a
	// runaway attempt count cannot wrap the duration negative.
	d := failureBackoffBase
	for i := 1; i < attempt; i++ {
		d *= 2
		if maxBackoff > 0 && d >= maxBackoff {
			return maxBackoff
		}
	}
	if maxBackoff > 0 && d > maxBackoff {
		return maxBackoff
	}
	return d
}
