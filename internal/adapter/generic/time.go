package generic

import "time"

// isoMillis renders an RFC3339 UTC timestamp as ISO8601 with milliseconds
// and zone offset (rest-api.md section 0: "2026-08-17T12:34:56.789+08:00").
// BinFlow stores UTC, so the zone arm prints as +00:00 — the same shape
// class as the spec's example, which is also what a UTC-configured
// Artifactory emits. Empty input renders as the given fallback's rendering
// (never an empty string: clients parse these with strict codecs).
func isoMillis(stored, fallback string) string {
	t := parseRFC3339(stored)
	if t.IsZero() {
		// Fall back to the injected clock's rendering rather than inventing
		// a timestamp out of thin air for a malformed row.
		if fb := parseRFC3339(fallback); !fb.IsZero() {
			return fb.Format("2006-01-02T15:04:05.000Z07:00")
		}
		return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
	}
	return t.Format("2006-01-02T15:04:05.000Z07:00")
}
