package knowledge

import "time"

// storeTimeLayout is the canonical timestamp format persisted in the knowledge
// store, matching the audit subsystem (RFC 3339 with millisecond precision).
const storeTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// parseStoreTime parses a timestamp persisted by the store, tolerating the
// nanosecond RFC 3339 form as a fallback.
func parseStoreTime(s string) (time.Time, error) {
	if t, err := time.Parse(storeTimeLayout, s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
