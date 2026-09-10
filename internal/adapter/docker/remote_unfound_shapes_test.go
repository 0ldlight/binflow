package docker

// L001-1 (L000-B C12/C13, evidence E6-1/E6-2): the remote plane's
// unfound family renders Artifactory's exact docker-v2 error bodies —
// a STATIC message and the detail keys the reference observes: the
// manifest arm's detail.manifest carries the IMAGE path (never the
// requested reference), the blob arm's detail.blobSum carries the
// digest spelling verbatim. Both the tag and the digest reference land
// on the same image-path detail.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestRemoteUnfoundBodiesMatchArtifactory: the 404 shapes of the remote
// read plane, decoded and compared field by field against the E6 evidence.
func TestRemoteUnfoundBodiesMatchArtifactory(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // the upstream answers nothing
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", up.URL)

	for _, tc := range []struct {
		name, path, wantCode, wantMessage string
		wantDetail                        map[string]any
	}{
		{
			name:        "unknown tag",
			path:        "/v2/docker-remote/myimg/manifests/nope",
			wantCode:    ErrCodeManifestUnknown,
			wantMessage: "The named manifest is not known to the registry.",
			wantDetail:  map[string]any{"manifest": "myimg"},
		},
		{
			name:        "unknown digest reference",
			path:        "/v2/docker-remote/myimg/manifests/sha256:" + sha256Hex([]byte("absent-manifest")),
			wantCode:    ErrCodeManifestUnknown,
			wantMessage: "The named manifest is not known to the registry.",
			wantDetail:  map[string]any{"manifest": "myimg"},
		},
		{
			name:        "unknown blob",
			path:        "/v2/docker-remote/myimg/blobs/sha256:" + sha256Hex([]byte("absent-blob")),
			wantCode:    ErrCodeBlobUnknown,
			wantMessage: "blob unknown to registry",
			wantDetail:  map[string]any{"blobSum": "sha256:" + sha256Hex([]byte("absent-blob"))},
		},
	} {
		code, body, _ := down.get(tc.path, nil)
		if code != http.StatusNotFound {
			t.Errorf("%s = (%d, %s), want 404", tc.name, code, body)
			continue
		}
		var eb specErrorBody
		if err := json.Unmarshal([]byte(body), &eb); err != nil {
			t.Errorf("%s body %q: %v", tc.name, body, err)
			continue
		}
		if len(eb.Errors) != 1 {
			t.Errorf("%s body %q: want exactly one error entry", tc.name, body)
			continue
		}
		got := eb.Errors[0]
		if got.Code != tc.wantCode {
			t.Errorf("%s code = %q, want %q", tc.name, got.Code, tc.wantCode)
		}
		if got.Message != tc.wantMessage {
			t.Errorf("%s message = %q, want %q", tc.name, got.Message, tc.wantMessage)
		}
		detail, _ := got.Detail.(map[string]any)
		if len(detail) != len(tc.wantDetail) {
			t.Errorf("%s detail = %v, want %v", tc.name, got.Detail, tc.wantDetail)
			continue
		}
		for k, v := range tc.wantDetail {
			if detail[k] != v {
				t.Errorf("%s detail[%q] = %v, want %v", tc.name, k, detail[k], v)
			}
		}
	}
}

// TestVirtualUnfoundBodyCarriesRemoteShape: the unfound SHAPES are shared
// construction (manifestUnfound/blobUnfound), so the virtual walk's
// terminal 404 carries the same L000-B E6 body as the direct remote plane
// — the E6 evidence itself was collected on a remote repository, and the
// evidence report's section 8 walk (the rendering lives in the shared v2
// REST layer, not a remote-only handler) is INFERENCE; this pins the
// propagated shape on the virtual face explicitly (L001-1 review B1).
// The message keeps the walk's upstream-summary suffix — only the prefix
// and the detail key/value are pinned here.
func TestVirtualUnfoundBodyCarriesRemoteShape(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound) // the member's upstream answers nothing
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "virt-remote-member", up.URL)
	if _, err := down.svc.CreateRepo(context.Background(), down.admin, &metadata.Repo{
		RepoKey: "docker-virt", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
		Config: `{"repositories":["virt-remote-member"]}`,
	}); err != nil {
		t.Fatalf("create virtual repository: %v", err)
	}

	for _, tc := range []struct {
		name, path, wantCode, wantMessagePrefix string
		wantDetailKey, wantDetailValue          string
	}{
		{
			name:              "virtual manifest 404, tag arm",
			path:              "/v2/docker-virt/myimg/manifests/never-tagged",
			wantCode:          ErrCodeManifestUnknown,
			wantMessagePrefix: "The named manifest is not known to the registry.",
			wantDetailKey:     "manifest",
			wantDetailValue:   "myimg",
		},
		{
			name:              "virtual blob 404",
			path:              "/v2/docker-virt/myimg/blobs/sha256:" + sha256Hex([]byte("virt-absent")),
			wantCode:          ErrCodeBlobUnknown,
			wantMessagePrefix: "blob unknown to registry",
			wantDetailKey:     "blobSum",
			wantDetailValue:   "sha256:" + sha256Hex([]byte("virt-absent")),
		},
	} {
		code, body, _ := down.serveReq(http.MethodGet, tc.path, nil, nil)
		if code != http.StatusNotFound {
			t.Errorf("%s = (%d, %s), want 404", tc.name, code, body)
			continue
		}
		var eb specErrorBody
		if err := json.Unmarshal([]byte(body), &eb); err != nil || len(eb.Errors) != 1 {
			t.Errorf("%s body %q: not one spec entry (%v)", tc.name, body, err)
			continue
		}
		got := eb.Errors[0]
		if got.Code != tc.wantCode {
			t.Errorf("%s code = %q, want %q", tc.name, got.Code, tc.wantCode)
		}
		if !strings.HasPrefix(got.Message, tc.wantMessagePrefix) {
			t.Errorf("%s message = %q, want the E6 static prefix %q", tc.name, got.Message, tc.wantMessagePrefix)
		}
		detail, _ := got.Detail.(map[string]any)
		if detail[tc.wantDetailKey] != tc.wantDetailValue || len(detail) != 1 {
			t.Errorf("%s detail = %v, want {%q: %q}", tc.name, got.Detail, tc.wantDetailKey, tc.wantDetailValue)
		}
	}
}
