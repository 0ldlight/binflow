package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// T-452 (FR-148.3 / aql.md §14.3): the dates/creation doors over the real
// stack — the 404-empty family's verbatim copy, the epoch-milliseconds
// parameters, the uri thin row with the V-o echo fallback, the closed
// dateFields set, and the ACL zero-leak probe (the same weave every query
// plane takes).

// t452DateRow is the doors' thin row decode target.
type t452DateRow struct {
	URI     string `json:"uri"`
	Created string `json:"created"`
}

// t452DatesDo issues one dates-family request and decodes status + body.
func t452DatesDo(t *testing.T, h *harness, path, user, pass string) (int, string) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/search/"+path, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, mustGet(t, resp)
}

// t452DecodeRows decodes a 200 body into rows.
func t452DecodeRows(t *testing.T, body string) []t452DateRow {
	t.Helper()
	var got struct {
		Results []t452DateRow `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return got.Results
}

// t452SeedRangeFixture deposits one backdated artifact (created 2020), one
// freshly created artifact and one maven-metadata.xml, returning the fresh
// deposit's creation instant.
func t452SeedRangeFixture(t *testing.T, h *harness) time.Time {
	t.Helper()
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/old.bin", "old-bytes")
	t440BackdateCreated(t, h, "generic-local", "acme/old.bin", "2020-01-01T00:00:00Z")
	deposit(t, h, "generic-local", "acme/fresh.bin", "fresh-bytes")
	deposit(t, h, "generic-local", "com/acme/maven-metadata.xml", "metadata-bytes")
	fresh, err := h.md.Nodes().Get(t.Context(), "generic-local", "acme/fresh.bin")
	if err != nil {
		t.Fatalf("fresh node: %v", err)
	}
	created, err := time.Parse(time.RFC3339, fresh.CreatedAt)
	if err != nil {
		t.Fatalf("fresh created %q: %v", fresh.CreatedAt, err)
	}
	return created
}

// TestSearchCreationWire pins the creation door (§14.3): the created arm
// hit, the modified arm hit WITH the V-o echo fallback, the metadata
// exclusion, and the from/to boundary grammar.
func TestSearchCreationWire(t *testing.T) {
	h := newHarness(t)
	created := t452SeedRangeFixture(t, h)

	// The created arm: a range around the backdated instant hits old.bin
	// (and only it — fresh.bin's created is 2026, the metadata file is
	// excluded).
	from2020 := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	to2021 := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	status, body := t452DatesDo(t, h, "creation?from="+itoa64(from2020)+"&to="+itoa64(to2021),
		adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("created arm status = %d body=%s", status, body)
	}
	rows := t452DecodeRows(t, body)
	if len(rows) != 1 || !strings.HasSuffix(rows[0].URI, "/api/storage/generic-local/acme/old.bin") {
		t.Fatalf("created arm rows = %+v", rows)
	}
	// The echo is the created instant (V-o's primary arm), ISO8601.
	if rows[0].Created != "2020-01-01T00:00:00.000Z" {
		t.Fatalf("created echo = %q, want the backdated instant in the ISO-millis form", rows[0].Created)
	}

	// The modified arm + the V-o fallback: a narrow range around NOW. Both
	// artifacts were modified at deposit time (inside), old.bin's CREATED
	// (2020) is outside — so old.bin hits through modified and its created
	// FIELD must echo the modified instant (§14.3's decompiled quirk, V-o);
	// fresh.bin's created is inside, so its echo is the created instant.
	before := created.Add(-time.Minute).UnixMilli()
	after := created.Add(time.Minute).UnixMilli()
	status, body = t452DatesDo(t, h, "creation?from="+itoa64(before)+"&to="+itoa64(after),
		adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("modified arm status = %d body=%s", status, body)
	}
	rows = t452DecodeRows(t, body)
	if len(rows) != 2 {
		t.Fatalf("modified arm rows = %+v, want both artifacts (their deposits fall inside; the descriptor is excluded)", rows)
	}
	echoOf := func(suffix string) string {
		for _, r := range rows {
			if strings.HasSuffix(r.URI, suffix) {
				return r.Created
			}
		}
		t.Fatalf("no row ends in %s: %+v", suffix, rows)
		return ""
	}
	if got := echoOf("/acme/old.bin"); got == "2020-01-01T00:00:00.000Z" || !strings.HasSuffix(got, "Z") {
		t.Fatalf("old.bin echo = %q — the V-o fallback must surface the MODIFIED instant, not the out-of-range created", got)
	}

	// The `to` default is now (V-j's official ruling): an open-ended range
	// from 2019 still hits (no upper bound missing).
	status, body = t452DatesDo(t, h, "creation?from="+itoa64(from2020), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("to-absent status = %d body=%s", status, body)
	}
	if rows = t452DecodeRows(t, body); len(rows) != 2 {
		t.Fatalf("to-absent rows = %+v, want both artifacts", rows)
	}

	// A range nothing falls inside: the 404 family's verbatim copy.
	status, body = t452DatesDo(t, h,
		"creation?from="+itoa64(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())+
			"&to="+itoa64(time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()),
		adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("miss = %d %s, want the 404 family verbatim", status, body)
	}
	// The metadata exclusion alone: a range that would contain only the
	// descriptor answers the 404 family (nothing else matches).
	status, body = t452DatesDo(t, h,
		"creation?from="+itoa64(before)+"&to="+itoa64(after)+"&repos=no-such-repo",
		adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("repos narrowing miss = %d %s", status, body)
	}
}

// TestSearchCreationMetadataExclusion pins the maven-metadata.xml veto in
// isolation: a repository holding ONLY the descriptor answers the 404
// family on any range.
func TestSearchCreationMetadataExclusion(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "meta-local")
	deposit(t, h, "meta-local", "maven-metadata.xml", "metadata-bytes")
	now := time.Now().UnixMilli()
	status, body := t452DatesDo(t, h,
		"creation?from=0&to="+itoa64(now+3_600_000), adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("descriptor-only repository = %d %s, want the 404 family (the exclusion)", status, body)
	}
}

// TestSearchDatesWire pins the dates door (§14.3): the closed dateFields
// set, the default pair, the download arm through a REAL download, and the
// unknown-field verbatim 400.
func TestSearchDatesWire(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"downloader", "downloader-pw"}})
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	grant(t, h, "dates-read", "generic-local", "**", "downloader", true, false, false)
	t440Download(t, h, "generic-local/acme/artifact.bin", "downloader", "downloader-pw", 1)

	past := time.Now().Add(-time.Hour).UnixMilli()
	future := time.Now().Add(time.Hour).UnixMilli()

	// dateFields=lastDownloaded: the real download lands inside — hit.
	status, body := t452DatesDo(t, h,
		"dates?from="+itoa64(past)+"&to="+itoa64(future)+"&dateFields=lastDownloaded",
		adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("lastDownloaded arm status = %d body=%s", status, body)
	}
	rows := t452DecodeRows(t, body)
	if len(rows) != 1 || !strings.HasSuffix(rows[0].URI, "/api/storage/generic-local/acme/artifact.bin") {
		t.Fatalf("lastDownloaded rows = %+v", rows)
	}

	// dateFields=remote_last_downloaded: the smart-remote stub is constant
	// null — nothing can fall inside a range, the honest 404 family.
	status, body = t452DatesDo(t, h,
		"dates?from=0&to="+itoa64(future)+"&dateFields=remote_last_downloaded",
		adminUser, adminPass)
	if status != http.StatusNotFound || !strings.Contains(body, "No results found.") {
		t.Fatalf("remote stub arm = %d %s, want the 404 family", status, body)
	}

	// No dateFields: the default pair {created, lastModified} (V-k) — the
	// deposit's created is inside.
	status, body = t452DatesDo(t, h, "dates?from="+itoa64(past)+"&to="+itoa64(future), adminUser, adminPass)
	if status != http.StatusOK {
		t.Fatalf("default pair status = %d body=%s", status, body)
	}
	if rows = t452DecodeRows(t, body); len(rows) != 1 {
		t.Fatalf("default pair rows = %+v", rows)
	}

	// An unknown field name: the decompiled verbatim 400 with the enum
	// echo ("unknown!, possible" spelling preserved).
	status, body = t452DatesDo(t, h,
		"dates?from="+itoa64(past)+"&dateFields=created_at", adminUser, adminPass)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", status)
	}
	want := "Date field name 'created_at' unknown!, possible values are: [created, lastModified, lastDownloaded, remote_last_downloaded]"
	if !strings.Contains(body, want) {
		t.Fatalf("unknown field body = %s, want the verbatim copy %q", body, want)
	}
}

// TestSearchDatesAuthAndParamArms pins the doors' gates (§14.3): the
// anonymous 401 challenge, the from-missing verbatim 400, and the
// unparsable-epoch 400s (BinFlow C-layer, the usage precedent).
func TestSearchDatesAuthAndParamArms(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")

	tests := []struct {
		name       string
		path       string
		user, pass string
		wantStatus int
		wantBody   string
	}{
		{"anonymous meets the 401 challenge", "creation?from=1", "", "",
			http.StatusUnauthorized, "Authentication is required"},
		{"anonymous on dates too", "dates?from=1", "", "",
			http.StatusUnauthorized, "Authentication is required"},
		{"creation without from", "creation", adminUser, adminPass,
			http.StatusBadRequest, "'from' parameter cannot be empty!"},
		{"dates without from", "dates", adminUser, adminPass,
			http.StatusBadRequest, "'from' parameter cannot be empty!"},
		{"non-numeric from", "creation?from=whenever", adminUser, adminPass,
			http.StatusBadRequest, "from"},
		{"non-numeric to", "creation?from=1&to=soon", adminUser, adminPass,
			http.StatusBadRequest, "to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := t452DatesDo(t, h, tt.path, tt.user, tt.pass)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d\nbody: %s", status, tt.wantStatus, body)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Fatalf("body %q does not carry %q", body, tt.wantBody)
			}
		})
	}

	// Foreign verbs keep the family's E-26 404.
	for _, path := range []string{"creation", "dates"} {
		resp := h.do(http.MethodPost, "/binflow/api/search/"+path, adminUser, adminPass, nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want the E-26 404", path, resp.StatusCode)
		}
	}
}

// TestSearchDatesACLZeroLeakProbe is the doors' zero-leak leg: a caller
// granted only repo A never sees repo B's rows — neither in the decoded
// results nor anywhere in the raw body — while the admin sees both.
func TestSearchDatesACLZeroLeakProbe(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"scoped", "scoped-pw"}})
	seedRepo(t, h, "alpha-local")
	seedRepo(t, h, "beta-local")
	deposit(t, h, "alpha-local", "a.bin", "alpha-bytes")
	deposit(t, h, "beta-local", "b.bin", "beta-bytes")
	grant(t, h, "dates-alpha", "alpha-local", "**", "scoped", true, false, false)

	past := time.Now().Add(-time.Hour).UnixMilli()
	future := time.Now().Add(time.Hour).UnixMilli()
	for _, door := range []string{"creation", "dates"} {
		status, body := t452DatesDo(t, h,
			door+"?from="+itoa64(past)+"&to="+itoa64(future), "scoped", "scoped-pw")
		if status != http.StatusOK {
			t.Fatalf("scoped %s status = %d body=%s", door, status, body)
		}
		rows := t452DecodeRows(t, body)
		if len(rows) != 1 || !strings.HasSuffix(rows[0].URI, "/alpha-local/a.bin") {
			t.Fatalf("scoped %s rows = %+v, want only the granted repository's row", door, rows)
		}
		for _, leak := range []string{"beta-local", "b.bin"} {
			if strings.Contains(body, leak) {
				t.Fatalf("leaked %q into the scoped %s result: %s", leak, door, body)
			}
		}
	}

	// The admin sees both rows on both doors.
	for _, door := range []string{"creation", "dates"} {
		status, body := t452DatesDo(t, h,
			door+"?from="+itoa64(past)+"&to="+itoa64(future), adminUser, adminPass)
		if status != http.StatusOK {
			t.Fatalf("admin %s status = %d", door, status)
		}
		if !strings.Contains(body, "beta-local") || !strings.Contains(body, "alpha-local") {
			t.Fatalf("admin %s body must span both repositories: %s", door, body)
		}
	}
}
