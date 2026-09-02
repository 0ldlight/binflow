package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// T-438 (FR-146.2 / ADR-0044 K69): the ?stats wire face — StatsInfo over
// the four nodes counting columns, with the lastDownloadedBy visibility
// gate (CapSystemRead, the audit log read's capability) and the zero-leak
// posture for everyone else.

// statsBody is the StatsInfo decode target (remoteLastDownloaded /
// remoteLastDownloadedBy are unsourced and never present — pinned below).
type statsBody struct {
	URI                 string `json:"uri"`
	DownloadCount       int64  `json:"downloadCount"`
	LastDownloaded      string `json:"lastDownloaded"`
	LastDownloadedBy    string `json:"lastDownloadedBy"`
	RemoteDownloadCount int64  `json:"remoteDownloadCount"`
}

// seedStatsArtifact seeds one downloaded artifact and returns its content
// path. One GET through the real content plane = one counted download; the
// downloader is a distinct non-admin principal (read-granted) so the
// identity arm's assertions and the zero-leak probe have a real name to
// look for.
func seedStatsArtifact(t *testing.T, h *harness, repoKey, path, body string) {
	t.Helper()
	seedRepo(t, h, repoKey)
	if resp := h.do(http.MethodPut, "/binflow/"+repoKey+"/"+path, adminUser, adminPass, []byte(body), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed PUT: %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
	grant(t, h, "stats-download-read", repoKey, "**", "downloader", true, false, false)
	if resp := h.do(http.MethodGet, "/binflow/"+repoKey+"/"+path, "downloader", "downloader-pw", nil, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("download GET: %d", resp.StatusCode)
	} else {
		_ = resp.Body.Close()
	}
}

// TestT438StatsFaceShape pins the StatsInfo contract: the five projected
// fields over one real download, the unsourced remote-pair's absence, the
// folder row's structural zeros, and the 404 ladder (missing item,
// repository root, non-GET verb).
func TestT438StatsFaceShape(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"downloader", "downloader-pw"}})
	seedStatsArtifact(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")

	t.Run("file answers the five-field StatsInfo", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin?stats", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body=%s", resp.StatusCode, body)
		}
		var got statsBody
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if got.DownloadCount != 1 {
			t.Fatalf("downloadCount = %d, want 1 (one real download — the probe itself never counts)", got.DownloadCount)
		}
		if got.RemoteDownloadCount != 0 {
			t.Fatalf("remoteDownloadCount = %d, want 0 on a local row", got.RemoteDownloadCount)
		}
		if got.LastDownloadedBy != "downloader" {
			t.Fatalf("lastDownloadedBy = %q, want downloader (admin sees the gate)", got.LastDownloadedBy)
		}
		if got.LastDownloaded == "" {
			t.Fatal("lastDownloaded must carry the landing instant")
		}
		if !strings.HasSuffix(got.URI, "/api/storage/generic-local/acme/artifact.bin") {
			t.Fatalf("uri = %q", got.URI)
		}
		// The unsourced remote pair is never fabricated (11.49).
		for _, absent := range []string{"remoteLastDownloaded", "remoteLastDownloadedBy"} {
			if strings.Contains(body, absent) {
				t.Fatalf("body must omit %s: %s", absent, body)
			}
		}
		// A second download moves the wire count — single source, no cache,
		// and the LATEST downloader's identity takes the row.
		if resp := h.do(http.MethodGet, "/binflow/generic-local/acme/artifact.bin", adminUser, adminPass, nil, nil); resp.StatusCode != http.StatusOK {
			t.Fatalf("second GET: %d", resp.StatusCode)
		} else {
			_ = resp.Body.Close()
		}
		resp = h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin?stats", adminUser, adminPass, nil, nil)
		body = mustGet(t, resp)
		var again statsBody
		if err := json.Unmarshal([]byte(body), &again); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if again.DownloadCount != 2 {
			t.Fatalf("downloadCount after second GET = %d, want 2", again.DownloadCount)
		}
		if again.LastDownloadedBy != "admin" {
			t.Fatalf("lastDownloadedBy = %q, want the LATEST downloader", again.LastDownloadedBy)
		}
	})

	t.Run("folder row answers structural zeros", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/?stats", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("folder ?stats status = %d body=%s", resp.StatusCode, body)
		}
		var got statsBody
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if got.DownloadCount != 0 || got.RemoteDownloadCount != 0 || got.LastDownloaded != "" || got.LastDownloadedBy != "" {
			t.Fatalf("folder stats = %+v, want the structural zeros", got)
		}
	})

	t.Run("repository root has no row and answers 404", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local?stats", adminUser, adminPass, nil, nil)
		_ = decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("root ?stats = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing item answers the item 404", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/nope.bin?stats", adminUser, adminPass, nil, nil)
		eb := decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("missing ?stats = %d, want 404", resp.StatusCode)
		}
		if !strings.Contains(eb.Errors[0].Message, "Unable to find item") {
			t.Fatalf("message = %q", eb.Errors[0].Message)
		}
	})

	t.Run("non-GET verb is the E-26 404", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/binflow/api/storage/generic-local/acme/artifact.bin?stats", adminUser, adminPass, nil, nil)
		_ = decodeError(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST ?stats = %d, want 404", resp.StatusCode)
		}
	})
}

// TestT438StatsLastDownloadedByVisibilityGate pins K69 decision 5: the
// identity arm rides CapSystemRead — admin and readonly_admin see
// lastDownloadedBy; a plain user (even one holding read on the repository)
// and an anonymous reader get the field OMITTED while the counts stay
// ungated. The zero-leak probe checks the raw body, not just the decode.
func TestT438StatsLastDownloadedByVisibilityGate(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"plain", "plain-pw"}, {"downloader", "downloader-pw"}})
	seedStatsArtifact(t, h, "generic-local", "acme/artifact.bin", "artifact-bytes")
	// The plain user holds r+w on the repository — maximum non-admin reach.
	grant(t, h, "stats-read", "generic-local", "**", "plain", true, true, false)
	seedLicenseUser(t, h.md, "roat", "roat-pw", "readonly_admin")

	tests := []struct {
		name        string
		user, pass  string
		wantVisible bool
	}{
		{"admin sees the identity arm", adminUser, adminPass, true},
		{"readonly_admin sees the identity arm (CapSystemRead)", "roat", "roat-pw", true},
		{"plain user with r+w gets the field omitted", "plain", "plain-pw", false},
		{"anonymous reader gets the field omitted", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/acme/artifact.bin?stats", tc.user, tc.pass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d body=%s", resp.StatusCode, body)
			}
			var got statsBody
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("body %q: %v", body, err)
			}
			// The counts are never gated (counting is not identity).
			if got.DownloadCount != 1 {
				t.Fatalf("downloadCount = %d, want 1 for every caller", got.DownloadCount)
			}
			if tc.wantVisible {
				if got.LastDownloadedBy != "downloader" {
					t.Fatalf("lastDownloadedBy = %q, want downloader", got.LastDownloadedBy)
				}
				return
			}
			// Zero-leak probe: the key must be absent from the raw body and
			// the downloader's name must not appear anywhere in it.
			if strings.Contains(body, "lastDownloadedBy") {
				t.Fatalf("leaked lastDownloadedBy: %s", body)
			}
			if strings.Contains(body, "downloader") {
				t.Fatalf("leaked the downloader identity: %s", body)
			}
		})
	}
}
