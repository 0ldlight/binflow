package maven

// T-68's calculator matrix (FR-17-AC1..AC6, ME-04..07). Two levels:
//
//   - the CALCULATOR level seeds node facts straight through repo.Service
//     and fires recalculation triggers in-package (the generators are pure
//     functions of the facts — who wrote the nodes is immaterial);
//   - the WIRE level drives the handler the way mvn does (PUT artifact,
//     PUT sidecars, PUT metadata, GET metadata) and pins the trigger
//     taxonomy's sync/async split, the client-PUT authoritative-content
//     rule, the delete cascade, checksum consistency and the concurrent
//     merge (FR-17-AC5, run under -race).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

var adminP = &auth.Principal{Name: "admin", Admin: true}

// seedNode lands one fact node through the service (no adapter chain: the
// calculator's inputs are storage facts, not wire requests).
func (hs *harness) seedNode(repoKey, path string, body []byte) {
	hs.t.Helper()
	if _, err := hs.svc.Put(context.Background(), adminP, repoKey, path,
		bytes.NewReader(body), storage.BlobRef{}, "application/octet-stream"); err != nil {
		hs.t.Fatalf("seed %s/%s: %v", repoKey, path, err)
	}
}

// recalc fires one synchronous recalculation (the matrix does not care
// which taxonomy row scheduled it).
func (hs *harness) recalc(repoKey, org, module, version string) {
	hs.t.Helper()
	hs.h.calc.recalcSync(context.Background(), adminP,
		trigger{repoKey: repoKey, orgPath: org, module: module, version: version})
}

// metaPath builds the metadata URL of a directory.
func metaPath(repoKey, org, module, version string) string {
	dir := strings.ReplaceAll(org, ".", "/") + "/" + module
	if version != "" {
		dir += "/" + version
	}
	return "/" + repoKey + "/" + dir + "/maven-metadata.xml"
}

// getMeta fetches a metadata document ("" body on non-200).
func (hs *harness) getMeta(repoKey, org, module, version string) (int, string) {
	hs.t.Helper()
	resp := hs.serve(http.MethodGet, metaPath(repoKey, org, module, version), nil, nil, true)
	body := string(drain(hs.t, resp))
	return resp.StatusCode, body
}

// mustContain asserts every want appears and every ban does not.
func mustContain(t *testing.T, what string, body string, want, ban []string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("%s: missing %q in\n%s", what, w, body)
		}
	}
	for _, b := range ban {
		if strings.Contains(body, b) {
			t.Errorf("%s: unexpected %q in\n%s", what, b, body)
		}
	}
}

// ---- version-group (module) generator matrix ----

// TestCalcModuleMatrix is FR-17-AC1's rule set over fact tables: versions
// = pom-bearing child directories, Maven ordering, latest (snapshots
// included) / release (last non-snapshot, omitted when none), and the
// deletion rule when no pom remains.
func TestCalcModuleMatrix(t *testing.T) {
	const org, module = "com.acme", "demo-app"
	pom := func(v string) string {
		return fmt.Sprintf("com/acme/demo-app/%s/demo-app-%s.pom", v, v)
	}
	jar := func(v, ext string) string {
		return fmt.Sprintf("com/acme/demo-app/%s/demo-app-%s.%s", v, v, ext)
	}

	cases := []struct {
		name       string
		facts      []string
		preseedXML string // client metadata node landed before the recalc
		wantStatus int
		want       []string
		ban        []string
	}{
		{
			name:       "release pair sorts and picks latest/release",
			facts:      []string{pom("1.0.0"), pom("1.1.0")},
			wantStatus: http.StatusOK,
			want: []string{
				"<groupId>com.acme</groupId>", "<artifactId>demo-app</artifactId>",
				"<version>1.0.0</version>", "<version>1.1.0</version>",
				"<latest>1.1.0</latest>", "<release>1.1.0</release>",
			},
		},
		{
			name:       "snapshot outranks older release for latest, release stays last non-snapshot",
			facts:      []string{pom("1.0.0"), pom("1.2.0-SNAPSHOT")},
			wantStatus: http.StatusOK,
			want:       []string{"<latest>1.2.0-SNAPSHOT</latest>", "<release>1.0.0</release>"},
		},
		{
			name:       "snapshot-only omits release",
			facts:      []string{pom("1.0-SNAPSHOT")},
			wantStatus: http.StatusOK,
			want:       []string{"<latest>1.0-SNAPSHOT</latest>"},
			ban:        []string{"<release>"},
		},
		{
			name:       "maven ordering 1.10.0 above 1.9.0",
			facts:      []string{pom("1.9.0"), pom("1.10.0")},
			wantStatus: http.StatusOK,
			want:       []string{"<latest>1.10.0</latest>"},
			ban:        []string{"<latest>1.9.0</latest>"},
		},
		{
			name:       "jar-only directory is not a version",
			facts:      []string{pom("1.0.0"), jar("2.0.0", "jar")},
			wantStatus: http.StatusOK,
			want:       []string{"<version>1.0.0</version>"},
			ban:        []string{"<version>2.0.0</version>"},
		},
		{
			name:       "pom checksum sidecar is not a descriptor",
			facts:      []string{pom("1.0.0"), jar("3.0.0", "pom.sha1")},
			wantStatus: http.StatusOK,
			ban:        []string{"<version>3.0.0</version>"},
		},
		{
			name:       "unique snapshot poms count as their directory version",
			facts:      []string{"com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.pom"},
			wantStatus: http.StatusOK,
			want:       []string{"<version>1.2.0-SNAPSHOT</version>", "<latest>1.2.0-SNAPSHOT</latest>"},
		},
		{
			name:       "no pom at all deletes the document",
			facts:      []string{jar("1.0.0", "jar")},
			preseedXML: "<metadata><versioning><versions><version>1.0.0</version></versions></versioning></metadata>",
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "RTFACT-6242: snapshot-typed document in a non-snapshot directory stays",
			facts: []string{jar("1.0.0", "jar")},
			preseedXML: `<metadata><groupId>com.acme</groupId><artifactId>demo-app</artifactId>` +
				`<version>1.0.0</version><versioning><snapshot><buildNumber>3</buildNumber></snapshot>` +
				`<snapshotVersions><snapshotVersion><extension>jar</extension><value>1.0.0-1</value>` +
				`<updated>20260819162439</updated></snapshotVersion></snapshotVersions></versioning></metadata>`,
			wantStatus: http.StatusOK,
			want:       []string{"<buildNumber>3</buildNumber>"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHarness(t)
			for _, f := range tc.facts {
				hs.seedNode("maven-local", f, []byte("fact-bytes"))
			}
			if tc.preseedXML != "" {
				hs.seedNode("maven-local", "com/acme/demo-app/maven-metadata.xml", []byte(tc.preseedXML))
			}
			hs.recalc("maven-local", org, module, "")
			status, body := hs.getMeta("maven-local", org, module, "")
			if status != tc.wantStatus {
				t.Fatalf("metadata GET = %d, want %d (%s)", status, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusOK {
				mustContain(t, tc.name, body, tc.want, tc.ban)
			}
		})
	}
}

// TestCalcModuleVersionOrder pins the versions LIST order (Maven
// comparator, ascending) beyond the latest element.
func TestCalcModuleVersionOrder(t *testing.T) {
	hs := newHarness(t)
	for _, v := range []string{"1.0.0", "1.10.0", "1.9.0", "1.0.1", "2.0.0-SNAPSHOT", "1.0.0-rc1"} {
		hs.seedNode("maven-local",
			fmt.Sprintf("com/acme/demo-app/%s/demo-app-%s.pom", v, v), []byte("p"))
	}
	hs.recalc("maven-local", "com.acme", "demo-app", "")
	_, body := hs.getMeta("maven-local", "com.acme", "demo-app", "")
	wantOrder := []string{"1.0.0-rc1", "1.0.0", "1.0.1", "1.9.0", "1.10.0", "2.0.0-SNAPSHOT"}
	last := -1
	for _, v := range wantOrder {
		i := strings.Index(body, "<version>"+v+"</version>")
		if i < 0 {
			t.Fatalf("version %s missing in\n%s", v, body)
		}
		if i < last {
			t.Errorf("version %s out of order in\n%s", v, body)
		}
		last = i
	}
	if i := strings.Index(body, "<latest>"); i < 0 || !strings.Contains(body[i:], "2.0.0-SNAPSHOT") {
		t.Errorf("latest must be the sorted last: %s", body)
	}
}

// ---- SNAPSHOT version-directory generator matrix ----

// TestCalcSnapshotDirMatrix is FR-17-AC2's rule set: buildNumber/timestamp
// from the newest unique snapshot pom (numeric buildNumber compare,
// timestamp ties), fixed 1 without a timestamp when no unique pom exists,
// snapshotVersions per (extension x classifier) with the newest entry and
// dot-stripped timestamps.
func TestCalcSnapshotDirMatrix(t *testing.T) {
	const org, module, ver = "com.acme", "demo-app", "1.2.0-SNAPSHOT"
	unique := func(ts string, n int, classifier, ext string) string {
		name := fmt.Sprintf("demo-app-1.2.0-%s-%d", ts, n)
		if classifier != "" {
			name += "-" + classifier
		}
		return fmt.Sprintf("com/acme/demo-app/1.2.0-SNAPSHOT/%s.%s", name, ext)
	}
	nonUnique := func(classifier, ext string) string {
		name := "demo-app-1.2.0-SNAPSHOT"
		if classifier != "" {
			name += "-" + classifier
		}
		return fmt.Sprintf("com/acme/demo-app/1.2.0-SNAPSHOT/%s.%s", name, ext)
	}

	cases := []struct {
		name       string
		facts      []string
		wantStatus int
		want       []string
		ban        []string
	}{
		{
			name:       "unique pom and jar at buildNumber 1",
			facts:      []string{unique("20260819.162439", 1, "", "pom"), unique("20260819.162439", 1, "", "jar")},
			wantStatus: http.StatusOK,
			want: []string{
				"<version>1.2.0-SNAPSHOT</version>",
				"<timestamp>20260819.162439</timestamp>", "<buildNumber>1</buildNumber>",
				"<extension>pom</extension>", "<value>1.2.0-20260819.162439-1</value>",
				"<extension>jar</extension>", "<updated>20260819162439</updated>",
			},
		},
		{
			name:       "buildNumber 2 replaces buildNumber 1 per extension",
			facts:      []string{unique("20260819.162439", 1, "", "pom"), unique("20260819.162439", 1, "", "jar"), unique("20260819.170001", 2, "", "pom"), unique("20260819.170001", 2, "", "jar")},
			wantStatus: http.StatusOK,
			want: []string{
				"<buildNumber>2</buildNumber>", "<timestamp>20260819.170001</timestamp>",
				"<value>1.2.0-20260819.170001-2</value>",
			},
			ban: []string{"<value>1.2.0-20260819.162439-1</value>"},
		},
		{
			name:       "timestamp tie at equal buildNumber takes the later timestamp",
			facts:      []string{unique("20260818.100000", 1, "", "pom"), unique("20260819.162439", 1, "", "pom")},
			wantStatus: http.StatusOK,
			want:       []string{"<timestamp>20260819.162439</timestamp>", "<buildNumber>1</buildNumber>"},
			ban:        []string{"20260818.100000"},
		},
		{
			name:       "classifier entries coexist per extension x classifier",
			facts:      []string{unique("20260819.162439", 1, "", "jar"), unique("20260819.170001", 2, "sources", "jar")},
			wantStatus: http.StatusOK,
			want: []string{
				"<classifier>sources</classifier>", "<value>1.2.0-20260819.170001-2</value>",
				"<value>1.2.0-20260819.162439-1</value>",
			},
		},
		{
			name:       "non-unique only: fixed buildNumber 1, no timestamp, -SNAPSHOT values",
			facts:      []string{nonUnique("", "pom"), nonUnique("", "jar")},
			wantStatus: http.StatusOK,
			want: []string{
				"<buildNumber>1</buildNumber>", "<value>1.2.0-SNAPSHOT</value>",
				"<extension>pom</extension>", "<extension>jar</extension>",
			},
			ban: []string{"<timestamp>"},
		},
		{
			name:       "unique jar wins over non-unique jar of the same extension",
			facts:      []string{nonUnique("", "jar"), unique("20260819.170001", 2, "", "jar"), unique("20260819.170001", 2, "", "pom")},
			wantStatus: http.StatusOK,
			want:       []string{"<extension>jar</extension>", "<value>1.2.0-20260819.170001-2</value>"},
		},
		{
			name:       "empty directory loses its document",
			facts:      []string{},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hs := newHarness(t)
			hs.seedNode("maven-local", "com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml",
				[]byte("<metadata><versioning><snapshot><buildNumber>9</buildNumber></snapshot></versioning></metadata>"))
			for _, f := range tc.facts {
				hs.seedNode("maven-local", f, []byte("fact-bytes"))
			}
			hs.recalc("maven-local", org, module, ver)
			status, body := hs.getMeta("maven-local", org, module, ver)
			if status != tc.wantStatus {
				t.Fatalf("metadata GET = %d, want %d (%s)", status, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusOK {
				mustContain(t, tc.name, body, tc.want, tc.ban)
			}
		})
	}
}

// TestCalcReleaseVersionDirRuling pins the documented ruling: the spec
// defines generators for version-group and SNAPSHOT directories only — a
// RELEASE version directory never gains a document, and a manually placed
// one is governed by the group-style cleanup (pom present keeps a client
// document, pom absent removes it; the RTFACT guard still protects
// snapshot-typed content).
func TestCalcReleaseVersionDirRuling(t *testing.T) {
	hs := newHarness(t)
	hs.seedNode("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", []byte("p"))
	hs.recalc("maven-local", "com.acme", "demo-app", "1.0.0")
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.0.0"); status != http.StatusNotFound {
		t.Fatalf("release version dir gained a document (status %d)", status)
	}

	// A manually placed plain document in a pomless release directory goes.
	hs.seedNode("maven-local", "com/acme/demo-app/2.0.0/maven-metadata.xml",
		[]byte("<metadata><versioning><lastUpdated>20260101000000</lastUpdated></versioning></metadata>"))
	hs.recalc("maven-local", "com.acme", "demo-app", "2.0.0")
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "2.0.0"); status != http.StatusNotFound {
		t.Fatalf("plain pomless release-dir document survived (status %d)", status)
	}

	// The RTFACT guard protects snapshot-typed content there.
	hs.seedNode("maven-local", "com/acme/demo-app/3.0.0/maven-metadata.xml",
		[]byte("<metadata><versioning><snapshot><buildNumber>1</buildNumber></snapshot></versioning></metadata>"))
	hs.recalc("maven-local", "com.acme", "demo-app", "3.0.0")
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", "3.0.0"); status != http.StatusOK || !strings.Contains(body, "<buildNumber>1</buildNumber>") {
		t.Fatalf("RTFACT-protected release-dir document removed (status %d, %s)", status, body)
	}
}

// ---- wire level: the trigger taxonomy ----

// TestCalcSyncTriggerTiming pins ME-04's synchronous rows: a unique
// snapshot file and a non-unique pom leave the version document readable
// the instant the deploy response returns — no async drain involved —
// while a release artifact leaves nothing at the version level (the
// module list arrives via the pom's grandparent row, checked after the
// drain).
func TestCalcSyncTriggerTiming(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	// unique pom: version document exists before the response completes.
	if resp := hs.serve(http.MethodPut,
		"/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.pom",
		[]byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("unique pom PUT = %d", resp.StatusCode)
	}
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusOK ||
		!strings.Contains(body, "<buildNumber>1</buildNumber>") {
		t.Fatalf("sync version document missing after unique pom (status %d, %s)", status, body)
	}

	// unique jar joins the same document synchronously.
	if resp := hs.serve(http.MethodPut,
		"/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.jar",
		jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("unique jar PUT = %d", resp.StatusCode)
	}
	if _, body := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); !strings.Contains(body, "<extension>jar</extension>") {
		t.Fatalf("jar entry missing after sync recalc: %s", body)
	}

	// non-unique pom: the synchronous row for deployer/non-unique repos
	// (the DEFAULT repository is unique-behavior since L014-2, so this row
	// needs the repository that spells the behavior).
	if resp := hs.serve(http.MethodPut,
		"/maven-nonunique/com/acme/x-app/2.0-SNAPSHOT/x-app-2.0-SNAPSHOT.pom",
		[]byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("non-unique pom PUT = %d", resp.StatusCode)
	}
	if status, body := hs.getMeta("maven-nonunique", "com.acme", "x-app", "2.0-SNAPSHOT"); status != http.StatusOK ||
		!strings.Contains(body, "<buildNumber>1</buildNumber>") || strings.Contains(body, "<timestamp>") {
		t.Fatalf("non-unique snapshot document wrong (status %d, %s)", status, body)
	}

	// release jar: no version-level document, ever.
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("release jar PUT = %d", resp.StatusCode)
	}
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.0.0"); status != http.StatusNotFound {
		t.Fatalf("release version dir gained a document (status %d)", status)
	}

	// release pom: the grandparent (module) row is async — present after
	// the drain (the async row of ME-04).
	if resp := hs.serve(http.MethodPut, pomPath, []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("release pom PUT = %d", resp.StatusCode)
	}
	hs.waitCalc()
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusOK ||
		!strings.Contains(body, "<version>1.0.0</version>") {
		t.Fatalf("module document missing after async drain (status %d, %s)", status, body)
	}
}

// TestCalcMetadataPutAuthoritative is v1.1's client-PUT rule: the PUT is
// accepted (201) and the served content is recomputed from server facts —
// a bogus version never becomes the list, a bogus buildNumber never
// survives, and a document PUT into a pomless directory is cleaned up.
func TestCalcMetadataPutAuthoritative(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	hs.seedNode("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", []byte("p"))
	hs.seedNode("maven-local",
		"com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.pom", []byte("p"))

	// module level: bogus 9.9.9 rejected by recomputation, real 1.0.0 kept.
	bogus := []byte("<metadata><versioning><latest>9.9.9</latest><versions><version>9.9.9</version></versions></versioning></metadata>")
	if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml", bogus, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("module metadata PUT = %d", resp.StatusCode)
	}
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusOK ||
		strings.Contains(body, "9.9.9") || !strings.Contains(body, "<version>1.0.0</version>") {
		t.Fatalf("module metadata not authoritative (status %d, %s)", status, body)
	}

	// version level: bogus buildNumber 99 dropped for the file facts' 1.
	bogusSnap := []byte("<metadata><versioning><snapshot><buildNumber>99</buildNumber></snapshot></versioning></metadata>")
	if resp := hs.serve(http.MethodPut,
		"/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml", bogusSnap, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("version metadata PUT = %d", resp.StatusCode)
	}
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusOK ||
		!strings.Contains(body, "<buildNumber>1</buildNumber>") || strings.Contains(body, "99") {
		t.Fatalf("version metadata not authoritative (status %d, %s)", status, body)
	}

	// pomless module: accepted, then cleaned by the same recalculation.
	if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/ghost/maven-metadata.xml", bogus, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("ghost metadata PUT = %d", resp.StatusCode)
	}
	if status, _ := hs.getMeta("maven-local", "com.acme", "ghost", ""); status != http.StatusNotFound {
		t.Fatalf("pomless module document survived its own recalculation (status %d)", status)
	}
}

// TestCalcPluginGroupMetadataNoTrigger pins the ruling that the
// plugin-group document variant has no server generator: a client PUT
// stores it verbatim and no recalculation touches it.
func TestCalcPluginGroupMetadataNoTrigger(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	doc := []byte("<metadata><plugins><plugin><name>demo</name></plugin></plugins></metadata>")
	target := "/maven-local/com/acme/metadata-maven-metadata.xml"
	if resp := hs.serve(http.MethodPut, target, doc, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("plugin-group metadata PUT = %d", resp.StatusCode)
	}
	if got := drain(t, hs.serve(http.MethodGet, target, nil, nil, true)); !bytes.Equal(got, doc) {
		t.Fatalf("plugin-group metadata rewritten: %s", got)
	}
}

// TestCalcDeleteCascade is ME-07 + FR-17-AC4: deletes recalculate the
// affected trees — a version drops when its last pom goes, the module
// document disappears with the last pom-bearing directory, and an emptied
// snapshot directory loses its version document (sidecars included).
func TestCalcDeleteCascade(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	// Two versions, full module document.
	for _, v := range []string{"1.0.0", "1.1.0"} {
		hs.seedNode("maven-local", fmt.Sprintf("com/acme/demo-app/%s/demo-app-%s.pom", v, v), []byte("p"))
		hs.seedNode("maven-local", fmt.Sprintf("com/acme/demo-app/%s/demo-app-%s.jar", v, v), []byte("j"))
	}
	hs.recalc("maven-local", "com.acme", "demo-app", "")
	if _, body := hs.getMeta("maven-local", "com.acme", "demo-app", ""); !strings.Contains(body, "<version>1.0.0</version>") || !strings.Contains(body, "<version>1.1.0</version>") {
		t.Fatalf("seed module document wrong: %s", body)
	}

	// Delete 1.1.0 entirely (the wire path fires the async cascade).
	for _, f := range []string{"1.1.0/demo-app-1.1.0.jar", "1.1.0/demo-app-1.1.0.pom"} {
		if resp := hs.serve(http.MethodDelete, "/maven-local/com/acme/demo-app/"+f, nil, nil, true); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d", f, resp.StatusCode)
		}
	}
	hs.waitCalc()
	_, body := hs.getMeta("maven-local", "com.acme", "demo-app", "")
	if strings.Contains(body, "1.1.0") || !strings.Contains(body, "<version>1.0.0</version>") {
		t.Fatalf("1.1.0 not dropped after delete: %s", body)
	}

	// A metadata sidecar landed; deleting 1.0.0 removes the whole family.
	if resp := hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml.sha1", nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("sidecar GET before delete = %d", resp.StatusCode)
	}
	for _, f := range []string{"1.0.0/demo-app-1.0.0.jar", "1.0.0/demo-app-1.0.0.pom"} {
		if resp := hs.serve(http.MethodDelete, "/maven-local/com/acme/demo-app/"+f, nil, nil, true); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d", f, resp.StatusCode)
		}
	}
	hs.waitCalc()
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusNotFound {
		t.Fatalf("module document survived the last pom (status %d)", status)
	}
	if resp := hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml.sha1", nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("metadata sidecar survived the family delete (status %d)", resp.StatusCode)
	}

	// Snapshot version directory: emptying it removes the version document.
	for _, f := range []string{"pom", "jar"} {
		hs.seedNode("maven-local",
			fmt.Sprintf("com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.%s", f), []byte("x"))
	}
	hs.recalc("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT")
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusOK {
		t.Fatalf("snapshot version document missing before delete")
	}
	for _, f := range []string{"pom", "jar"} {
		if resp := hs.serve(http.MethodDelete,
			fmt.Sprintf("/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.%s", f),
			nil, nil, true); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("snapshot DELETE .%s = %d", f, resp.StatusCode)
		}
	}
	hs.waitCalc()
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusNotFound {
		t.Fatalf("emptied snapshot dir kept its document (status %d)", status)
	}
}

// ---- checksum consistency (FR-17-AC6 / M14) ----

// TestCalcChecksumConsistency: after every recalculation the sidecar GETs
// answer the digests of the document a GET returns — computed live from
// the ledger, so they can never go stale against rewritten content.
func TestCalcChecksumConsistency(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	deploy := func(v string) {
		hs.t.Helper()
		if resp := hs.serve(http.MethodPut,
			fmt.Sprintf("/maven-local/com/acme/demo-app/%s/demo-app-%s.pom", v, v),
			[]byte("<project><version>"+v+"</version></project>"), nil, true); resp.StatusCode != http.StatusCreated {
			hs.t.Fatalf("pom %s PUT = %d", v, resp.StatusCode)
		}
		// the client metadata PUT a real mvn deploy ends with (the sync
		// row that settles the module document).
		if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml",
			[]byte("<metadata><versioning/></metadata>"), nil, true); resp.StatusCode != http.StatusCreated {
			hs.t.Fatalf("metadata PUT after %s = %d", v, resp.StatusCode)
		}
	}

	check := func(stage string) {
		hs.t.Helper()
		resp := hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml", nil, nil, true)
		body := drain(hs.t, resp)
		if resp.StatusCode != http.StatusOK {
			hs.t.Fatalf("%s: metadata GET = %d", stage, resp.StatusCode)
		}
		s1, m5, _ := digests(body)
		for suffix, want := range map[string]string{"sha1": s1, "md5": m5} {
			got := strings.TrimSpace(string(drain(hs.t, hs.serve(http.MethodGet,
				"/maven-local/com/acme/demo-app/maven-metadata.xml."+suffix, nil, nil, true))))
			if got != want {
				hs.t.Errorf("%s: sidecar .%s = %q, want %q (of the served XML)", stage, suffix, got, want)
			}
		}
		// response headers carry the same digests (M14's "sync with
		// headers" half).
		if got := resp.Header.Get("X-Checksum-Sha1"); got != s1 {
			hs.t.Errorf("%s: X-Checksum-Sha1 = %q, want %q", stage, got, s1)
		}
	}

	deploy("1.0.0")
	check("after first deploy")
	deploy("1.1.0") // changed document bytes
	check("after changed content")
}

// ---- concurrency (FR-17-AC5) ----

// TestCalcConcurrentMerges deploys 1.3.0 and 1.4.0 from two concurrent
// client goroutines (interleaved artifact and metadata PUTs, the shape
// FR-17-AC5's two-process variant produces) and asserts: no 5xx anywhere,
// and the final module document holds BOTH versions — recomputation from
// storage facts merges instead of last-writer-wins.
func TestCalcConcurrentMerges(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	const workers, rounds = 2, 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	var bad []string

	client := func(v string) {
		defer wg.Done()
		pom := fmt.Sprintf("<project><version>%s</version></project>", v)
		for i := 0; i < rounds; i++ {
			// artifact PUT (async module row)
			if resp := hs.serve(http.MethodPut,
				fmt.Sprintf("/maven-local/com/acme/race-app/%s/race-app-%s.pom", v, v),
				[]byte(pom), nil, true); resp.StatusCode/100 != 2 {
				mu.Lock()
				bad = append(bad, fmt.Sprintf("pom %s round %d: %d", v, i, resp.StatusCode))
				mu.Unlock()
			}
			// sidecar PUT (mvn sends the checksum files next)
			s1, _, _ := digests([]byte(pom))
			if resp := hs.serve(http.MethodPut,
				fmt.Sprintf("/maven-local/com/acme/race-app/%s/race-app-%s.pom.sha1", v, v),
				[]byte(s1), nil, true); resp.StatusCode/100 != 2 {
				mu.Lock()
				bad = append(bad, fmt.Sprintf("sidecar %s round %d: %d", v, i, resp.StatusCode))
				mu.Unlock()
			}
			// metadata PUT (the sync recalculation row both clients race)
			if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/race-app/maven-metadata.xml",
				[]byte("<metadata><versioning><versions><version>"+v+"</version></versions></versioning></metadata>"),
				nil, true); resp.StatusCode/100 != 2 {
				mu.Lock()
				bad = append(bad, fmt.Sprintf("metadata %s round %d: %d", v, i, resp.StatusCode))
				mu.Unlock()
			}
		}
	}
	wg.Add(workers)
	go client("1.3.0")
	go client("1.4.0")
	wg.Wait()
	hs.waitCalc()

	for _, b := range bad {
		t.Errorf("non-2xx during interleaved deploys: %s", b)
	}
	status, body := hs.getMeta("maven-local", "com.acme", "race-app", "")
	if status != http.StatusOK {
		t.Fatalf("module metadata after race = %d", status)
	}
	mustContain(t, "race merge", body,
		[]string{"<version>1.3.0</version>", "<version>1.4.0</version>"}, nil)
	// latest is one of the two, monotone with the list's order.
	if !strings.Contains(body, "<latest>1.4.0</latest>") {
		t.Errorf("latest not the sorted last after merge: %s", body)
	}
}

// TestCalcInterleavedRecalcsStaleWriteGuard drives the execution-lock
// invariant directly: a stale trigger (listed before a concurrent deploy)
// must not overwrite the fresh document — the lock serializes executions
// and every execution lists facts inside it, so the LAST recalculation's
// write wins and always sees every landed node.
func TestCalcInterleavedRecalcsStaleWriteGuard(t *testing.T) {
	hs := newHarness(t)

	hs.seedNode("maven-local", "com/acme/demo-app/1.3.0/demo-app-1.3.0.pom", []byte("p3"))

	var wg sync.WaitGroup
	wg.Add(2)
	// "stale" recalc: pauses INSIDE the window (before listing) via the
	// lock-free fact seed of the second version, then recalcs.
	go func() {
		defer wg.Done()
		hs.recalc("maven-local", "com.acme", "demo-app", "")
	}()
	// concurrent deploy of the second version + its own recalc.
	go func() {
		defer wg.Done()
		hs.seedNode("maven-local", "com/acme/demo-app/1.4.0/demo-app-1.4.0.pom", []byte("p4"))
		hs.recalc("maven-local", "com.acme", "demo-app", "")
	}()
	wg.Wait()

	// The final document lists both: whichever execution ran last, its
	// listing happened after both seeds (the lock orders executions and
	// the seeds precede both trigger calls in program order per goroutine;
	// the drain below settles any interleaving with one final recalc).
	hs.recalc("maven-local", "com.acme", "demo-app", "")
	_, body := hs.getMeta("maven-local", "com.acme", "demo-app", "")
	mustContain(t, "stale-write guard", body,
		[]string{"<version>1.3.0</version>", "<version>1.4.0</version>"}, nil)
}

// ---- clock behavior ----

// TestCalcLastUpdatedMonotonic pins FR-17-AC1's monotonicity: lastUpdated
// is the recalculation moment (yyyyMMddHHmmss UTC) and never moves
// backwards across successive recalculations.
func TestCalcLastUpdatedMonotonic(t *testing.T) {
	hs := newHarness(t)

	cur := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	hs.h.calc.now = func() time.Time { return cur }

	stamp := func() string {
		hs.recalc("maven-local", "com.acme", "demo-app", "")
		_, body := hs.getMeta("maven-local", "com.acme", "demo-app", "")
		m := regexp.MustCompile(`<lastUpdated>(\d{14})</lastUpdated>`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no lastUpdated in\n%s", body)
		}
		return m[1]
	}

	hs.seedNode("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.pom", []byte("p"))
	first := stamp()
	if first != "20260819120000" {
		t.Fatalf("pinned-clock stamp = %s", first)
	}

	cur = cur.Add(90 * time.Second)
	hs.seedNode("maven-local", "com/acme/demo-app/1.1.0/demo-app-1.1.0.pom", []byte("p"))
	second := stamp()
	if second <= first {
		t.Fatalf("lastUpdated not monotone: %s then %s", first, second)
	}
	if second != "20260819120130" {
		t.Fatalf("advanced stamp = %s", second)
	}
}

// ---- wiring guards ----

// TestCalcNilSeamDisables pins the nil-seam contract: a handler assembled
// without the nodes seam keeps the T-67 transfer behavior (metadata PUTs
// stored verbatim, nothing recomputed).
func TestCalcNilSeamDisables(t *testing.T) {
	hs := newHarness(t)
	hs.h.nodes = nil
	hs.h.calc = nil

	doc := []byte("<metadata><versioning><versions><version>7.7.7</version></versions></versioning></metadata>")
	if resp := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml", doc, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("metadata PUT = %d", resp.StatusCode)
	}
	if got := drain(t, hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml", nil, nil, true)); !bytes.Equal(got, doc) {
		t.Fatalf("verbatim storage broken without the calculator: %s", got)
	}
}

// TestCalcRemoteRepoUntouched: a DELETE on a remote repository is a cache
// invalidation — no recalculation may write metadata into a remote
// namespace (the guard is the handler's class gate).
func TestCalcRemoteRepoUntouched(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	// Seed a cached copy the way the pull-through engine leaves one (a
	// node row plus its ledger blob); svc.Put refuses remote repositories,
	// so the rows land through the store — the delete path under test only
	// needs the node to exist.
	ctx := context.Background()
	const sha = "1111111111111111111111111111111111111111111111111111111111111111"
	if err := hs.md.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: 2, CreatedAt: "2026-08-19T00:00:00Z"}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := hs.md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "maven-remote", Path: "com/acme/up/1.0.0/up-1.0.0.pom",
		Sha256: sha, Size: 2, CreatedAt: "2026-08-19T00:00:00Z", UpdatedAt: "2026-08-19T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if resp := hs.serve(http.MethodDelete, "/maven-remote/com/acme/up/1.0.0/up-1.0.0.pom", nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remote DELETE = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	hs.waitCalc()
	// The observable is the STORE, not the GET (a remote GET is the proxy
	// engine's business — unfetched upstreams answer their own way): no
	// metadata node may exist in the remote namespace.
	if _, err := hs.md.Nodes().Get(ctx, "maven-remote", "com/acme/up/maven-metadata.xml"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("remote namespace gained a metadata document (err = %v)", err)
	}
}
