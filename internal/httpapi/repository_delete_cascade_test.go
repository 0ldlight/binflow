package httpapi_test

// T-555 / BIN-37 (D-3 user ruling 2026-09-29): DELETE /api/repositories/{key}
// is a SILENT CASCADE aligned to the reference face. Everything pinned here
// was live-probed on Artifactory 7.161.26 (192.168.120.38:8082, 2026-09-29,
// repos difftest-d3-*): 200 + application/json body
// {repoKey, statusMsg, deletedArtifactsCount, success}; deletedArtifactsCount
// counts FILES + FOLDER rows with the repo root excluded (seven probe shapes:
// empty=0, one root file=1, 3 files nested 3 folders=6, pom+jar(+A-side
// auto-metadata)=7, calibration pom=6, flag spelling identical); the
// ?deleteContent=true spelling is accepted and ignored; local/remote report
// "and all its content have been removed successfully.", virtual the plain
// "has been removed successfully." The 404 (missing key) and 403 (non-admin)
// arms predate this ticket and must not regress.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// deleteReport is the v1 delete's success body (the reference report shape).
type deleteReport struct {
	RepoKey               string `json:"repoKey"`
	StatusMsg             string `json:"statusMsg"`
	DeletedArtifactsCount int    `json:"deletedArtifactsCount"`
	Success               bool   `json:"success"`
}

// seedCascadeRepo creates one local generic repo and lands the given paths
// through the content plane (folder rows materialize with them, ADR-0016).
func seedCascadeRepo(t *testing.T, h *harness, key string, paths []string) {
	t.Helper()
	resp := putRepo(t, h, key, `{"rclass":"local","packageType":"generic"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create %s: %d body=%s", key, resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
	for _, p := range paths {
		r := h.do(http.MethodPut, "/binflow/"+key+"/"+p, adminUser, adminPass,
			[]byte("content-of-"+p), nil)
		if r.StatusCode != http.StatusCreated {
			t.Fatalf("seed %s/%s: %d body=%s", key, p, r.StatusCode, mustGet(t, r))
		}
		_ = r.Body.Close()
	}
}

// doRepoDelete issues the v1 repository delete and decodes its body.
func doRepoDelete(t *testing.T, h *harness, target string, user, pass string) (int, http.Header, deleteReport, string) {
	t.Helper()
	resp := h.do(http.MethodDelete, "/binflow/api/repositories/"+target, user, pass, nil, nil)
	body := mustGet(t, resp)
	var rep deleteReport
	_ = json.Unmarshal([]byte(body), &rep)
	return resp.StatusCode, resp.Header, rep, body
}

// TestRepositoryDeleteCascade: the D-3 alignment table — every non-empty leg
// cascades with the live-pinned count (files + folder rows), the flag is
// accepted-and-ignored, and the refusal arms keep their status.
func TestRepositoryDeleteCascade(t *testing.T) {
	const contentWording = "Repository '%s' and all its content have been removed successfully."

	tests := []struct {
		name string
		// setup seeds the repo(s) this leg deletes; nil = no seeding (the
		// caller arranges the rest).
		setup func(t *testing.T, h *harness)
		// target is the key (or key?query) the DELETE addresses.
		target string
		// user/pass: admin for the happy arms, u1 for the 403 regression.
		user, pass string
		wantStatus int
		wantCount  int    // -1 = not asserted (error arms).
		wantMsg    string // "" = not asserted.
	}{
		{
			name: "non-empty nested repo cascades with items count",
			setup: func(t *testing.T, h *harness) {
				// Probe shape s3: 3 files + 3 folder rows = 6 items.
				seedCascadeRepo(t, h, "casc-local", []string{
					"a/b/c/f1.bin", "a/b/f2.bin", "a/f3.bin"})
			},
			target: "casc-local", user: adminUser, pass: adminPass,
			wantStatus: http.StatusOK, wantCount: 6,
			wantMsg: fmt.Sprintf(contentWording, "casc-local"),
		},
		{
			name: "empty repo answers count 0",
			setup: func(t *testing.T, h *harness) {
				seedCascadeRepo(t, h, "casc-empty", nil)
			},
			target: "casc-empty", user: adminUser, pass: adminPass,
			wantStatus: http.StatusOK, wantCount: 0,
			wantMsg: fmt.Sprintf(contentWording, "casc-empty"),
		},
		{
			name: "deleteContent=true is accepted and cascades identically",
			setup: func(t *testing.T, h *harness) {
				seedCascadeRepo(t, h, "casc-flag", []string{"x/y/f.bin"})
			},
			target: "casc-flag?deleteContent=true", user: adminUser, pass: adminPass,
			wantStatus: http.StatusOK, wantCount: 3, // 1 file + 2 folders (probe s5)
			wantMsg: fmt.Sprintf(contentWording, "casc-flag"),
		},
		{
			name: "single root file counts 1 (no folders, no root)",
			setup: func(t *testing.T, h *harness) {
				seedCascadeRepo(t, h, "casc-root", []string{"single.bin"})
			},
			target: "casc-root", user: adminUser, pass: adminPass,
			wantStatus: http.StatusOK, wantCount: 1,
			wantMsg: fmt.Sprintf(contentWording, "casc-root"),
		},
		{
			name: "deep pair counts files plus every folder level",
			setup: func(t *testing.T, h *harness) {
				// Probe shape s4's BinFlow twin (no A-side auto-metadata):
				// 2 files + 4 folder rows = 6.
				seedCascadeRepo(t, h, "casc-deep", []string{
					"com/ex/thing/1.0/thing-1.0.pom",
					"com/ex/thing/1.0/thing-1.0.jar"})
			},
			target: "casc-deep", user: adminUser, pass: adminPass,
			wantStatus: http.StatusOK, wantCount: 6,
			wantMsg: fmt.Sprintf(contentWording, "casc-deep"),
		},
		{
			name:   "missing key keeps its 404",
			target: "casc-ghost",
			user:   adminUser, pass: adminPass,
			wantStatus: http.StatusNotFound, wantCount: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
			if tt.setup != nil {
				tt.setup(t, h)
			}
			status, hdr, rep, body := doRepoDelete(t, h, tt.target, tt.user, tt.pass)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", status, tt.wantStatus, body)
			}
			if tt.wantCount < 0 {
				return // refusal arm: status only
			}
			if ct := hdr.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if !rep.Success {
				t.Errorf("success = false; body=%s", body)
			}
			if rep.DeletedArtifactsCount != tt.wantCount {
				t.Errorf("deletedArtifactsCount = %d, want %d (files + folder rows)", rep.DeletedArtifactsCount, tt.wantCount)
			}
			if tt.wantMsg != "" && rep.StatusMsg != tt.wantMsg {
				t.Errorf("statusMsg = %q, want %q", rep.StatusMsg, tt.wantMsg)
			}
		})
	}
}

// TestRepositoryDeleteCascadeWordingByRclass: the per-rclass success wording
// (T-555 probe, 2026-09-29) — virtual repositories hold no content of their
// own and report the plain removal; local and remote the content-bearing
// form.
func TestRepositoryDeleteCascadeWordingByRclass(t *testing.T) {
	h := newHarness(t)
	seedCascadeRepo(t, h, "casc-member", nil)
	if resp := putRepo(t, h, "casc-virt",
		`{"rclass":"virtual","packageType":"generic","repositories":["casc-member"]}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("create virtual: %d body=%s", resp.StatusCode, mustGet(t, resp))
	} else {
		_ = resp.Body.Close()
	}
	if resp := putRepo(t, h, "casc-rem",
		`{"rclass":"remote","packageType":"generic","url":"https://example.com/up"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("create remote: %d body=%s", resp.StatusCode, mustGet(t, resp))
	} else {
		_ = resp.Body.Close()
	}

	for _, tt := range []struct{ target, wantMsg string }{
		{"casc-virt", "Repository 'casc-virt' has been removed successfully."},
		{"casc-member", "Repository 'casc-member' and all its content have been removed successfully."},
		{"casc-rem", "Repository 'casc-rem' and all its content have been removed successfully."},
	} {
		status, _, rep, body := doRepoDelete(t, h, tt.target, adminUser, adminPass)
		if status != http.StatusOK {
			t.Fatalf("%s: status = %d body=%s", tt.target, status, body)
		}
		if rep.StatusMsg != tt.wantMsg {
			t.Errorf("%s: statusMsg = %q, want %q", tt.target, rep.StatusMsg, tt.wantMsg)
		}
		if rep.DeletedArtifactsCount != 0 {
			t.Errorf("%s: count = %d, want 0", tt.target, rep.DeletedArtifactsCount)
		}
	}
}

// TestRepositoryDeleteCascadeForbidden: the route gate's 403 for a non-admin
// survives the cascade change (family 6: deletion is admin-only, T-217).
func TestRepositoryDeleteCascadeForbidden(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedCascadeRepo(t, h, "casc-gated", []string{"f.bin"})
	status, _, _, body := doRepoDelete(t, h, "casc-gated", "u1", "p1")
	if status != http.StatusForbidden {
		t.Fatalf("non-admin delete status = %d body=%s", status, body)
	}
	// The refused delete must have destroyed nothing.
	resp := h.do(http.MethodGet, "/binflow/api/repositories/casc-gated", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repo must survive the refused delete: %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
}
