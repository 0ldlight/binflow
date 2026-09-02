package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// T-440 (FR-148.1 / aql.md §14): GET /binflow/api/search/usage over the
// real stack, plus the AQL statistics domain's nested wire block. Every
// download fixture is a REAL content-plane GET — the T-438 single counting
// channel — and the single-source recheck compares the usage row's count
// against the ?stats face's projection of the same column.

// t440UsageBody is the five-field usage row decode target (aql.md §14.2,
// live v8m — five fields, no more).
type t440UsageBody struct {
	Results []struct {
		URI                  string `json:"uri"`
		DownloadCount        int64  `json:"downloadCount"`
		LastDownloaded       string `json:"lastDownloaded"`
		RemoteDownloadCount  int64  `json:"remoteDownloadCount"`
		RemoteLastDownloaded string `json:"remoteLastDownloaded"`
	} `json:"results"`
}

// t440UsageDo issues one usage query and decodes status + body.
func t440UsageDo(t *testing.T, h *harness, query, user, pass string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/search/usage"+query, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, mustGet(t, resp)
}

// t440Download lands n real downloads of one artifact as the given caller.
// The path argument is the full "<repo>/<path>" content URL segment.
func t440Download(t *testing.T, h *harness, repoPath, user, pass string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		resp := h.do(http.MethodGet, "/binflow/"+repoPath, user, pass, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download GET %s: status %d", repoPath, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// t440BackdateCreated rewrites one node's creation instant (the content
// plane stamps "now"; the created-arm legs need an artifact created in the
// past relative to notUsedSince).
func t440BackdateCreated(t *testing.T, h *harness, repo, path, at string) {
	t.Helper()
	node, err := h.md.Nodes().Get(context.Background(), repo, path)
	if err != nil {
		t.Fatalf("node get %s/%s: %v", repo, path, err)
	}
	node.CreatedAt = at
	if err := h.md.Nodes().Put(context.Background(), node); err != nil {
		t.Fatalf("node put %s/%s: %v", repo, path, err)
	}
}

// TestT440UsageEndpointWire pins the endpoint's happy path (AC1): the
// five-field row over a really-downloaded artifact, the count agreement
// with the ?stats face (single source), the uri form, and the constant
// smart-remote pair.
func TestT440UsageEndpointWire(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"downloader", "downloader-pw"}})
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	grant(t, h, "usage-read", "generic-local", "**", "downloader", true, false, false)
	const downloads = 3
	t440Download(t, h, "generic-local/acme/artifact.bin", "downloader", "downloader-pw", downloads)

	notUsed := time.Now().Add(time.Hour).UnixMilli()
	status, body := t440UsageDo(t, h, "?notUsedSince="+itoa64(notUsed), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%s", status, body)
	}
	var got t440UsageBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(got.Results) != 1 {
		t.Fatalf("results = %d, want the one downloaded artifact: %s", len(got.Results), body)
	}
	row := got.Results[0]
	if !strings.HasSuffix(row.URI, "/api/storage/generic-local/acme/artifact.bin") {
		t.Fatalf("uri = %q", row.URI)
	}
	if row.DownloadCount != downloads {
		t.Fatalf("downloadCount = %d, want %d (the real GETs)", row.DownloadCount, downloads)
	}
	if row.LastDownloaded == "" {
		t.Fatal("lastDownloaded must carry the last landing instant")
	}
	if !strings.HasSuffix(row.LastDownloaded, "Z") || !strings.Contains(row.LastDownloaded, "T") {
		t.Fatalf("lastDownloaded = %q, want the ISO8601-millis Z form", row.LastDownloaded)
	}
	// The smart-remote pair is the constant stub (aql.md §14.1/§14.2):
	// 0 and epoch-0, data never fabricated.
	if row.RemoteDownloadCount != 0 {
		t.Fatalf("remoteDownloadCount = %d, want 0", row.RemoteDownloadCount)
	}
	if row.RemoteLastDownloaded != "1970-01-01T00:00:00.000Z" {
		t.Fatalf("remoteLastDownloaded = %q, want the epoch-0 literal", row.RemoteLastDownloaded)
	}

	// The single-source recheck (AC1): the ?stats face reads the same
	// counting column — the two faces cannot disagree.
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin?stats",
		adminUser, adminPass, nil, nil)
	statsBody := mustGet(t, resp)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("?stats status = %d body=%s", resp.StatusCode, statsBody)
	}
	var stats struct {
		DownloadCount int64 `json:"downloadCount"`
	}
	if err := json.Unmarshal([]byte(statsBody), &stats); err != nil {
		t.Fatalf("stats body %q: %v", statsBody, err)
	}
	if stats.DownloadCount != row.DownloadCount {
		t.Fatalf("usage downloadCount %d != ?stats downloadCount %d — the faces drifted",
			row.DownloadCount, stats.DownloadCount)
	}

	// repos narrowing: an unknown key matches nothing — the 404 family.
	status, body = t440UsageDo(t, h, "?notUsedSince="+itoa64(notUsed)+"&repos=no-such-repo", adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("repos narrowing = %d %s, want the 404 family", status, body)
	}
}

// TestT440UsageSemantics pins the hit-set semantics through the endpoint
// (aql.md §14.2): strict-< on the downloaded arm, the never-downloaded
// inclusion, the createdBefore fallback, and the created arm's veto.
func TestT440UsageSemantics(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "recent.bin", "recent-bytes")
	deposit(t, h, "generic-local", "old-fresh.bin", "old-bytes")
	backdated := "2020-01-01T00:00:00Z"
	t440BackdateCreated(t, h, "generic-local", "old-fresh.bin", backdated)
	// One download on recent.bin lands "now".
	t440Download(t, h, "generic-local/recent.bin", adminUser, adminPass, 1)

	past := time.Now().Add(-time.Hour).UnixMilli()
	future := time.Now().Add(time.Hour).UnixMilli()

	// notUsedSince in the past: recent.bin's download is NOT before it;
	// old-fresh.bin (never downloaded, created 2020) still qualifies.
	status, body := t440UsageDo(t, h, "?notUsedSince="+itoa64(past), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%s", status, body)
	}
	var got t440UsageBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(got.Results) != 1 || !strings.HasSuffix(got.Results[0].URI, "/old-fresh.bin") {
		t.Fatalf("results = %+v, want only the never-downloaded 2020 artifact: %s", got.Results, body)
	}
	if got.Results[0].LastDownloaded != "1970-01-01T00:00:00.000Z" {
		t.Fatalf("a never-downloaded row's lastDownloaded = %q, want the epoch-0 form", got.Results[0].LastDownloaded)
	}

	// notUsedSince in the future: both files qualify (recent's download is
	// before it; old-fresh is null) — but the created arm keeps only rows
	// created before the fallback bound... both are (now/2020 < future),
	// so two rows.
	status, body = t440UsageDo(t, h, "?notUsedSince="+itoa64(future), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%s", status, body)
	}
	got = t440UsageBody{}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("results = %d, want both artifacts: %s", len(got.Results), body)
	}

	// createdBefore as an explicit override: a bound before BOTH creation
	// instants empties the set through the created arm alone (the fallback
	// would have kept both).
	status, body = t440UsageDo(t, h,
		"?notUsedSince="+itoa64(future)+"&createdBefore="+itoa64(time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()),
		adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("createdBefore veto = %d %s, want the 404 family", status, body)
	}
}

// TestT440UsageAuthAndErrorArms pins the endpoint's gates (§14.2): the
// anonymous 401, the missing-parameter 404 quirk (same copy as the empty
// set — decompiled), and the unparsable-epoch 400 (BinFlow C-layer: the
// decompile never registered a non-numeric arm).
func TestT440UsageAuthAndErrorArms(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	future := time.Now().Add(time.Hour).UnixMilli()

	tests := []struct {
		name       string
		query      string
		user, pass string
		wantStatus int
		wantBody   string
	}{
		{"anonymous meets the 401 challenge", "?notUsedSince=" + itoa64(future), "", "",
			http.StatusUnauthorized, "Authentication is required"},
		{"missing notUsedSince answers the 404 family copy", "", adminUser, adminPass,
			http.StatusNotFound, "No results found."},
		{"empty hit set answers the 404 family copy", "?notUsedSince=1000", adminUser, adminPass,
			http.StatusNotFound, "No results found."},
		{"non-numeric epoch is the C-layer 400", "?notUsedSince=yesterday", adminUser, adminPass,
			http.StatusBadRequest, "notUsedSince"},
		{"negative epoch is the C-layer 400", "?notUsedSince=-5", adminUser, adminPass,
			http.StatusBadRequest, "notUsedSince"},
		{"non-numeric createdBefore is the C-layer 400", "?notUsedSince=" + itoa64(future) + "&createdBefore=soon",
			adminUser, adminPass, http.StatusBadRequest, "createdBefore"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := t440UsageDo(t, h, tt.query, tt.user, tt.pass)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d\nbody: %s", status, tt.wantStatus, body)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Fatalf("body %q does not carry %q", body, tt.wantBody)
			}
		})
	}

	// Foreign verbs keep the family's E-26 404.
	resp := h.do(http.MethodPost, "/binflow/api/search/usage?notUsedSince=1", adminUser, adminPass, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST usage = %d, want the E-26 404", resp.StatusCode)
	}
}

// TestT440UsageACLZeroLeakProbe is AC2: a restricted caller granted only
// repo A never sees repo B's rows — neither in the decoded results nor
// anywhere in the raw body — while the admin sees both (the weave cannot
// widen or leak, and the query plane answers exactly what a download
// would).
func TestT440UsageACLZeroLeakProbe(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"scoped", "scoped-pw"}})
	seedRepo(t, h, "alpha-local")
	seedRepo(t, h, "beta-local")
	deposit(t, h, "alpha-local", "a.bin", "alpha-bytes")
	deposit(t, h, "beta-local", "b.bin", "beta-bytes")
	grant(t, h, "scoped-alpha", "alpha-local", "**", "scoped", true, false, false)
	t440Download(t, h, "alpha-local/a.bin", "scoped", "scoped-pw", 2)
	t440Download(t, h, "beta-local/b.bin", adminUser, adminPass, 5)
	future := time.Now().Add(time.Hour).UnixMilli()

	status, body := t440UsageDo(t, h, "?notUsedSince="+itoa64(future), "scoped", "scoped-pw")
	if status != http.StatusOK {
		t.Fatalf("scoped status = %d body=%s", status, body)
	}
	var got t440UsageBody
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(got.Results) != 1 || !strings.HasSuffix(got.Results[0].URI, "/alpha-local/a.bin") {
		t.Fatalf("scoped results = %+v, want only the granted repository's row", got.Results)
	}
	if got.Results[0].DownloadCount != 2 {
		t.Fatalf("scoped downloadCount = %d, want 2", got.Results[0].DownloadCount)
	}
	// The zero-leak probe: the forbidden repository's key, its artifact
	// path — nothing of beta-local may appear in the raw body.
	for _, leak := range []string{"beta-local", "b.bin"} {
		if strings.Contains(body, leak) {
			t.Fatalf("leaked %q into the scoped result: %s", leak, body)
		}
	}

	// The admin sees both rows.
	status, body = t440UsageDo(t, h, "?notUsedSince="+itoa64(future), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", status, body)
	}
	if !strings.Contains(body, "beta-local") || !strings.Contains(body, "alpha-local") {
		t.Fatalf("admin body must span both repositories: %s", body)
	}
}

// TestT440AQLStatsWireBlock pins the AQL face's nested statistics output
// (aql.md §3.3/§14.1, live v16 shape): include names the stat members,
// the row carries ONE "stats" array whose object holds exactly those
// members, and downloaded_by reads "unknown" for a non-admin caller (§6
// masking) while the admin reads the identity.
func TestT440AQLStatsWireBlock(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"scoped", "scoped-pw"}, {"downloader", "downloader-pw"}})
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	grant(t, h, "stats-wire-read", "generic-local", "**", "scoped", true, false, false)
	grant(t, h, "stats-wire-download", "generic-local", "**", "downloader", true, false, false)
	t440Download(t, h, "generic-local/acme/artifact.bin", "downloader", "downloader-pw", 2)

	query := `items.find({"repo":"generic-local"}).include("name","stat.downloads","stat.downloaded","stat.downloaded_by")`

	status, body := aqlDo(t, h, query, adminUser, adminPass, "")
	if status != http.StatusOK {
		t.Fatalf("admin status = %d body=%s", status, body)
	}
	// The pretty nesting of live v16: the stats member at the row level,
	// its fields at four spaces, the closing bracket at two.
	if !strings.Contains(body, "\"stats\" : [ {\n") || !strings.Contains(body, "\n  } ]") {
		t.Fatalf("stats block shape wrong\nbody: %s", body)
	}
	var parsed struct {
		Results []struct {
			Name  string `json:"name"`
			Stats []struct {
				Downloaded   string `json:"downloaded"`
				Downloads    int64  `json:"downloads"`
				DownloadedBy string `json:"downloaded_by"`
			} `json:"stats"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(parsed.Results) != 1 || len(parsed.Results[0].Stats) != 1 {
		t.Fatalf("results = %+v, want one row with one stats object: %s", parsed.Results, body)
	}
	st := parsed.Results[0].Stats[0]
	if st.Downloads != 2 || st.Downloaded == "" || st.DownloadedBy != "downloader" {
		t.Fatalf("admin stats = %+v, want downloads=2, a stamped instant, the identity", st)
	}

	// The non-admin caller: counts and dates ungated, the identity masked.
	_, scopedBody := aqlDo(t, h, query, "scoped", "scoped-pw", "")
	parsed = struct {
		Results []struct {
			Name  string `json:"name"`
			Stats []struct {
				Downloaded   string `json:"downloaded"`
				Downloads    int64  `json:"downloads"`
				DownloadedBy string `json:"downloaded_by"`
			} `json:"stats"`
		} `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(scopedBody), &parsed); err != nil {
		t.Fatalf("scoped body %q: %v", scopedBody, err)
	}
	if len(parsed.Results) != 1 || len(parsed.Results[0].Stats) != 1 {
		t.Fatalf("scoped results = %+v: %s", parsed.Results, scopedBody)
	}
	st = parsed.Results[0].Stats[0]
	if st.Downloads != 2 {
		t.Fatalf("scoped downloads = %d, counts are never gated", st.Downloads)
	}
	if st.DownloadedBy != "unknown" {
		t.Fatalf("scoped downloaded_by = %q, want the masked literal", st.DownloadedBy)
	}
	if strings.Contains(scopedBody, "downloader") {
		t.Fatalf("leaked the downloader identity: %s", scopedBody)
	}
}

// TestT440AQLStubMembersOnWire pins the constant stub members' wire values
// inside the stats block: 0 for the counter, null for the rest — the spec's
// mapping ruling, no fabricated data.
func TestT440AQLStubMembersOnWire(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")

	status, body := aqlDo(t, h,
		`items.find({"repo":"generic-local"}).include("stat.remote_downloads","stat.remote_downloaded","stat.remote_origin")`,
		adminUser, adminPass, "")
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%s", status, body)
	}
	if !strings.Contains(body, `"remote_downloads" : 0`) ||
		!strings.Contains(body, `"remote_downloaded" : null`) ||
		!strings.Contains(body, `"remote_origin" : null`) {
		t.Fatalf("stub members wrong\nbody: %s", body)
	}
	// The stub query arms answer the honest sets: eq-0 matches everything.
	status, body = aqlDo(t, h,
		`items.find({"repo":"generic-local","stat.remote_downloads":{"$eq":0}}).include("name")`,
		adminUser, adminPass, "")
	if status != http.StatusOK || !strings.Contains(body, "artifact.bin") {
		t.Fatalf("stub eq-0 status = %d body=%s", status, body)
	}
	status, body = aqlDo(t, h,
		`items.find({"repo":"generic-local","stat.remote_downloads":{"$gt":0}}).include("name")`,
		adminUser, adminPass, "")
	if status != http.StatusOK || strings.Contains(body, "artifact.bin") {
		t.Fatalf("stub gt-0 status = %d body=%s", status, body)
	}
}

// itoa64 renders an epoch-milliseconds parameter.
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
