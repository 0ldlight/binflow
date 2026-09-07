package httpapi_test

// The remote metadata TTL's wire knob (M17 T-495, FR-158 — T-461's
// registered leftover "枚举快照 TTL 非 wire 可调" closed): the
// metadataRetrievalCachePeriodSecs field on the remote repository PUT is
// the window BOTH the remote-browsing snapshot cache and the pull-through
// metadata cache rows honor (remote-browsing.md §1's "cached per the
// Metadata Retrieval Cache Period" mapping onto remote_configs.
// metadata_ttl_seconds). Until T-495 the column was written as a hardwired
// 600s constant no PUT could move — the degraded e2e had to swap the
// upstream URL to invalidate the snapshot signature. These tests pin:
//
//   - the round trip (PUT carries the knob, GET echoes it canonically, the
//     remote_configs row carries the value the browse engine reads);
//   - the 600s product default on a body without the field;
//   - the boundary 400 (a negative TTL refuses by name);
//   - the update plane (a new value replaces; a config-less update keeps).

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// ttlRow reads the repository's remote_configs row — the storage the
// enumeration engine's TTL window actually reads (browse.go), so the
// assertion is the effect, not just the echo.
func ttlRow(t *testing.T, h *harness, key string) int64 {
	t.Helper()
	row, err := h.md.Remote().GetConfig(context.Background(), key)
	if err != nil {
		t.Fatalf("remote config get %s: %v", key, err)
	}
	return row.MetadataTTLSeconds
}

// configField reads one canonical field out of the GET echo's
// "configuration" object.
func configField(t *testing.T, h *harness, key, field string) any {
	t.Helper()
	code, cfg := getRepoJSON(t, h, key)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d", key, code)
	}
	inner, _ := cfg["configuration"].(map[string]any)
	if inner == nil {
		t.Fatalf("GET %s carries no configuration object: %v", key, cfg)
	}
	return inner[field]
}

func TestRemoteMetadataTTLWireRoundTrip(t *testing.T) {
	h := newBrowseFlagHarness(t)

	// Create with an explicit window: echoed canonically, landed in the
	// row the enumeration engine reads.
	if status, body := putRepoStatus(t, h, "ttl-explicit",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","allowPrivateUpstream":true,"metadataRetrievalCachePeriodSecs":1200}`); status != http.StatusOK {
		t.Fatalf("create ttl-explicit: status=%d body=%s", status, body)
	}
	if got := configField(t, h, "ttl-explicit", "metadataRetrievalCachePeriodSecs"); got != float64(1200) {
		t.Fatalf("echo metadataRetrievalCachePeriodSecs = %v, want 1200", got)
	}
	if got := ttlRow(t, h, "ttl-explicit"); got != 1200 {
		t.Fatalf("row metadata_ttl_seconds = %d, want 1200", got)
	}

	// The create arm without the field: the 600s product default, echoed
	// and landed (the pre-T-495 hardwired value, now the family default).
	if status, body := putRepoStatus(t, h, "ttl-default",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","allowPrivateUpstream":true}`); status != http.StatusOK {
		t.Fatalf("create ttl-default: status=%d body=%s", status, body)
	}
	if got := configField(t, h, "ttl-default", "metadataRetrievalCachePeriodSecs"); got != float64(600) {
		t.Fatalf("default echo = %v, want 600", got)
	}
	if got := ttlRow(t, h, "ttl-default"); got != 600 {
		t.Fatalf("default row = %d, want 600", got)
	}

	// The update plane: a config-bearing PUT replaces the window (the
	// family's full-replace semantics — url rides along as the required
	// field), a config-less PUT keeps it.
	if status, body := putRepoStatus(t, h, "ttl-explicit",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","allowPrivateUpstream":true,"metadataRetrievalCachePeriodSecs":60}`); status != http.StatusOK {
		t.Fatalf("update ttl-explicit: status=%d body=%s", status, body)
	}
	if got := ttlRow(t, h, "ttl-explicit"); got != 60 {
		t.Fatalf("row after update = %d, want 60", got)
	}
	if status, body := putRepoStatus(t, h, "ttl-explicit", `{"description":"config-less touch"}`); status != http.StatusOK {
		t.Fatalf("config-less update: status=%d body=%s", status, body)
	}
	if got := ttlRow(t, h, "ttl-explicit"); got != 60 {
		t.Fatalf("row after config-less update = %d, want the kept 60", got)
	}
}

func TestRemoteMetadataTTLWireBoundary(t *testing.T) {
	h := newBrowseFlagHarness(t)
	// The boundary arm: a negative window is the by-name 400 (a TTL in
	// seconds cannot be negative — the same refusal the retrieval-cache
	// family gives), and nothing lands.
	status, body := putRepoStatus(t, h, "ttl-bad",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","allowPrivateUpstream":true,"metadataRetrievalCachePeriodSecs":-1}`)
	if status != http.StatusBadRequest {
		t.Fatalf("negative TTL create = %d (%s), want 400", status, body)
	}
	if !strings.Contains(body, "metadataRetrievalCachePeriodSecs") || !strings.Contains(body, "negative") {
		t.Fatalf("400 body %q must name the field and the refusal", body)
	}
	// An explicit 0 is the family's "keeps the default" spelling, not a
	// second boundary: the stored window is the 600s product default.
	if status, body := putRepoStatus(t, h, "ttl-zero",
		`{"rclass":"remote","packageType":"helm","url":"http://127.0.0.1:9099","allowPrivateUpstream":true,"metadataRetrievalCachePeriodSecs":0}`); status != http.StatusOK {
		t.Fatalf("zero TTL create: status=%d body=%s", status, body)
	}
	if got := ttlRow(t, h, "ttl-zero"); got != 600 {
		t.Fatalf("zero-TTL row = %d, want the 600s default", got)
	}
}
