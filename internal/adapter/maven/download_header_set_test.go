package maven

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-608 / BIN-90 — the maven artifact-download header set against the live
// A model T-601's dual-round probe pinned (7.161.26): the
// Content-Disposition / X-Artifactory-Filename pair on jar and pom body
// faces (GET and HEAD), the 416's bare set, the 304 keeping the pair while
// CT/CL go, and virtual member-header inheritance. The sidecar face is
// writeSidecarBody's own verb model (T-598 / BIN-80) and stays out of this
// file.

func TestDownloadDispositionHeaderForms(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", jarPath[len("/maven-local/"):], jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("deploy jar: %d %s", resp.StatusCode, drain(t, resp))
	}
	if resp := hs.serve(http.MethodPut, pomPath, []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("deploy pom: %d %s", resp.StatusCode, drain(t, resp))
	}

	tests := []struct {
		name string
		path string
		base string
	}{
		{"jar GET", jarPath, "demo-app-1.0.0.jar"},
		{"jar HEAD", jarPath, "demo-app-1.0.0.jar"},
		{"pom GET", pomPath, "demo-app-1.0.0.pom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := http.MethodGet
			if strings.Contains(tt.name, "HEAD") {
				method = http.MethodHead
			}
			resp := hs.serve(method, tt.path, nil, nil, true)
			defer drain(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			wantCD := `attachment; filename="` + tt.base + `"; filename*=UTF-8''` + tt.base
			if got := resp.Header.Get("Content-Disposition"); got != wantCD {
				t.Fatalf("Content-Disposition = %q, want %q", got, wantCD)
			}
			if got := resp.Header.Get("X-Artifactory-Filename"); got != tt.base {
				t.Fatalf("X-Artifactory-Filename = %q, want %q", got, tt.base)
			}
		})
	}
}

func TestDownloadRange416BareHeaderSet(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", jarPath[len("/maven-local/"):], jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("deploy jar: %d %s", resp.StatusCode, drain(t, resp))
	}
	resp := hs.serve(http.MethodGet, jarPath, nil,
		map[string]string{"Range": "bytes=999999-"}, true)
	defer drain(t, resp)
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != fmt.Sprintf("bytes */%d", len(jarBytes)) {
		t.Fatalf("Content-Range = %q, want bytes */%d", got, len(jarBytes))
	}
	if got := resp.Header.Get("Content-Length"); got != "0" {
		t.Fatalf("Content-Length = %q, want 0", got)
	}
	for _, k := range []string{
		"Content-Type", "Etag", "Last-Modified", "Accept-Ranges",
		"Content-Disposition", "X-Artifactory-Filename",
		"X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256",
	} {
		if got := resp.Header.Get(k); got != "" {
			t.Fatalf("416 keeps %s = %q, want the bare set (absent)", k, got)
		}
	}
}

func TestDownloadConditional304KeepsDispositionPair(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", jarPath[len("/maven-local/"):], jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("deploy jar: %d %s", resp.StatusCode, drain(t, resp))
	}
	head := hs.serve(http.MethodHead, jarPath, nil, nil, true)
	etag := head.Header.Get("ETag")
	drain(t, head)
	if etag == "" {
		t.Fatal("fixture ETag missing")
	}

	resp := hs.serve(http.MethodGet, jarPath, nil,
		map[string]string{"If-None-Match": etag}, true)
	defer drain(t, resp)
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", resp.StatusCode)
	}
	wantCD := `attachment; filename="demo-app-1.0.0.jar"; filename*=UTF-8''demo-app-1.0.0.jar`
	if got := resp.Header.Get("Content-Disposition"); got != wantCD {
		t.Fatalf("304 Content-Disposition = %q, want %q", got, wantCD)
	}
	if got := resp.Header.Get("X-Artifactory-Filename"); got != "demo-app-1.0.0.jar" {
		t.Fatalf("304 X-Artifactory-Filename = %q, want demo-app-1.0.0.jar", got)
	}
	if got := resp.Header.Get("ETag"); got != etag {
		t.Fatalf("304 ETag = %q, want %q", got, etag)
	}
	// CT/CL suppression on 304 is the real server's chunkWriter behavior
	// (net/http suppressedHeaders304), not the handler's — this harness
	// renders through a bare recorder, so that face is pinned by the
	// generic plane's real-server test and the live differential instead.
}

func TestDownloadVirtualInheritsDispositionPair(t *testing.T) {
	hs := newHarness(t)
	gav := jarPath[len("/maven-local/"):]
	if resp := hs.deployJar("maven-local", gav, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("deploy jar: %d %s", resp.StatusCode, drain(t, resp))
	}
	// A member-routed virtual (mvv → maven-local) for the inheritance leg.
	if _, err := hs.svc.CreateRepo(t.Context(), &repo.Principal{Name: "admin", Admin: true}, &metadata.Repo{
		RepoKey: "mvv608", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("seed mvv608: %v", err)
	}
	resp := hs.serve(http.MethodGet, "/mvv608/"+gav, nil, nil, true)
	defer drain(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	wantCD := `attachment; filename="demo-app-1.0.0.jar"; filename*=UTF-8''demo-app-1.0.0.jar`
	if got := resp.Header.Get("Content-Disposition"); got != wantCD {
		t.Fatalf("virtual Content-Disposition = %q, want %q (member inherit)", got, wantCD)
	}
	if got := resp.Header.Get("X-Artifactory-Filename"); got != "demo-app-1.0.0.jar" {
		t.Fatalf("virtual X-Artifactory-Filename = %q, want demo-app-1.0.0.jar", got)
	}
	if got := resp.Header.Get(repo.HdrResolvedFrom); got != "maven-local" {
		t.Fatalf("%s = %q, want maven-local (F4 keep)", repo.HdrResolvedFrom, got)
	}
}
