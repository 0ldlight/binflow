package httpapi_test

// The /api/storage FileInfo mimeType render under the BIN-53 / T-571
// ownership ruling: the factory table (mimetypes.xml v17,
// docs/reverse/mime-ownership.md section 2) answers for the node's path
// at request time; the stored mime column takes no part — so rows seeded
// with other adapters' storage constants or the old model's declared
// values re-render per the table exactly like fresh deploys. The one
// carve-out is the OCI content-negotiation mediaType the docker/helmoci
// manifest nodes store verbatim (T-32 R3 pass-through, excluded from the
// ruling): those keep the stored value.
import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/deb"
	"github.com/lzwzzy/binflow/internal/adapter/nuget"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// fileInfoMimeType fetches one node's FileInfo mimeType through the API.
func fileInfoMimeType(t *testing.T, h *harness, repo, path string) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/storage/"+repo+"/"+path,
		adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FileInfo %s/%s = %d", repo, path, resp.StatusCode)
	}
	var fi struct {
		MimeType string `json:"mimeType"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fi); err != nil {
		t.Fatalf("FileInfo body: %v", err)
	}
	return fi.MimeType
}

// TestFileInfoMimeTypeAdjacentStorageChains proves the BIN-53 acceptance-④
// convergence end to end on the two license-gated faces (the scratch
// instance's community tier cannot create debian/nuget repositories, so
// the chains run in-process against the same stack the harness builds:
// the deb and nuget handlers drive directly, the way their own package
// tests do, and the FileInfo face renders through the real httpapi
// server). Each chain stores its own constant — debPUT lands
// application/vnd.debian.binary-package (handler.go serveUploadDeb), the
// bare nupkg PUT lands application/octet-stream (flat.go serveBareContent)
// — and the FileInfo face must still render the table value
// (x-debian-package / x-nupkg). The npm leg of the same family converged
// on the live scratch wire (T-571 log); its storage constant is identical
// in shape (octet-stream on a .tgz) to the nuget arm here.
func TestFileInfoMimeTypeAdjacentStorageChains(t *testing.T) {
	h := newHarness(t)
	// The two package types are license-gated (pro tier) and the harness
	// wires no license manager, so the static enum alone would refuse the
	// repos. Attach an all-unlocked verdict stub — the licensed posture
	// these chains run under on a real instance — through repo's own
	// assembly seam (the AttachReplicator precedent).
	repo.AttachPackageTypeGate(h.svc, unlockAllGate{})
	admin := &auth.Principal{Name: adminUser, Admin: true}
	for _, r := range []struct {
		key, ptype string
	}{
		{"mime-adj-deb", "debian"},
		{"mime-adj-nuget", "nuget"},
	} {
		if _, err := h.svc.CreateRepo(t.Context(), admin, &metadata.Repo{
			RepoKey: r.key, Type: repo.TypeLocal, PackageType: r.ptype,
		}); err != nil {
			t.Fatalf("CreateRepo %s: %v", r.key, err)
		}
	}

	// deb face: one real-shaped .deb through the debPUT binary chain
	// (matrix coordinates on the path, control paragraph inside).
	debPath := "pool/main/t/t571chain/t571chain_1.0_amd64.deb"
	debH := deb.New(h.svc, h.md.Repos(), h.md.Blobs(), h.md.NodeProps(), deb.Options{})
	req := httptest.NewRequest(http.MethodPut,
		"/mime-adj-deb/"+debPath+";deb.distribution=stable;deb.component=main;deb.architecture=amd64",
		bytes.NewReader(fixtureDeb(t)))
	req = req.WithContext(adapter.WithPrincipal(req.Context(), admin))
	rr := httptest.NewRecorder()
	debH.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("debPUT = %d: %s", rr.Code, rr.Body.String())
	}
	if got, want := fileInfoMimeType(t, h, "mime-adj-deb", debPath),
		"application/x-debian-package"; got != want {
		t.Errorf("deb FileInfo mimeType = %q, want %q (stored constant re-rendered)", got, want)
	}

	// nuget face: the bare content PUT stores octet-stream verbatim.
	nugetPath := "pkgs/t571chain.1.0.0.nupkg"
	nugetH := nuget.New(h.svc, h.md.Repos(), h.md.Blobs(), nil, nuget.Options{})
	req2 := httptest.NewRequest(http.MethodPut,
		"/mime-adj-nuget/"+nugetPath, bytes.NewReader([]byte("nupkg-bytes")))
	req2 = req2.WithContext(adapter.WithPrincipal(req2.Context(), admin))
	rr2 := httptest.NewRecorder()
	nugetH.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusCreated {
		t.Fatalf("nupkg PUT = %d: %s", rr2.Code, rr2.Body.String())
	}
	if got, want := fileInfoMimeType(t, h, "mime-adj-nuget", nugetPath),
		"application/x-nupkg"; got != want {
		t.Errorf("nupkg FileInfo mimeType = %q, want %q (octet-stream storage re-rendered)", got, want)
	}
}

// unlockAllGate is the license-verdict stub for the adjacent-chain test:
// every package type known and unlocked (the pro-tier posture).
type unlockAllGate struct{}

func (unlockAllGate) Verdict(context.Context, string) repo.PackageTypeVerdict {
	return repo.PackageTypeVerdict{Known: true, Unlocked: true}
}

// fixtureDeb assembles one real-shaped .deb (debian-binary +
// control.tar.gz + data.tar.gz) — the deb package's own test fixture
// shape, rebuilt here because these helpers live in that package's
// internal test files.
func fixtureDeb(t *testing.T) []byte {
	t.Helper()
	member := func(name string, data []byte) []byte {
		hdr := name + "/"
		for len(hdr) < 16 {
			hdr += " "
		}
		size := strconv.Itoa(len(data))
		for len(size) < 10 {
			size = " " + size
		}
		out := []byte(hdr + "0           0     0     644     " + size + "`\n")
		out = append(out, data...)
		if len(data)%2 == 1 {
			out = append(out, '\n')
		}
		return out
	}
	tarSingle := func(path, content string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{
			Name: path, Mode: 0o644, Size: int64(len(content)),
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("tar body: %v", err)
		}
		if err := tw.Close(); err != nil {
			t.Fatalf("tar close: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		return buf.Bytes()
	}
	control := "Package: t571chain\nVersion: 1.0\nArchitecture: amd64\n" +
		"Maintainer: T <t@binflow.dev>\nDescription: mime render chain probe\n"
	out := []byte("!<arch>\n")
	out = append(out, member("debian-binary", []byte("2.0\n"))...)
	out = append(out, member("control.tar.gz", tarSingle("./control", control))...)
	out = append(out, member("data.tar.gz", tarSingle("./usr/bin/dummy", "#!/bin/sh\n"))...)
	return out
}

// seedStored puts one node with an arbitrary STORED mime straight through
// the service — the honest simulation of rows the render must re-derive:
// other adapters' constants (deb/nuget/npm storage legs) and hand-migrated
// old-model rows.
func seedStored(t *testing.T, h *harness, repo, path, stored string) {
	t.Helper()
	admin := &auth.Principal{Name: adminUser, Admin: true}
	if _, err := h.svc.Put(t.Context(), admin, repo, path,
		strings.NewReader("mime-render-fixture"), storage.BlobRef{}, stored); err != nil {
		t.Fatalf("seed %s/%s (stored %q): %v", repo, path, stored, err)
	}
}

func TestFileInfoMimeTypeRender(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "mime-render")

	cases := []struct {
		name   string
		path   string
		stored string
		want   string
	}{
		// Adjacent-storage legs (BIN-53 acceptance ④): constants the other
		// adapters store keep their columns but render per the table.
		{"deb storage constant renders table value", "pkg/t571/a_1.0.deb",
			"application/vnd.debian.binary-package", "application/x-debian-package"},
		{"nuget nupkg octet-stream renders table value", "pkg/t571/a.1.0.0.nupkg",
			"application/octet-stream", "application/x-nupkg"},
		{"npm tgz octet-stream renders table value", "pkg/t571/a-1.0.0.tgz",
			"application/octet-stream", "application/x-gzip"},
		// Old-model rows (declared CT stored verbatim) re-render per table.
		{"old declared text/csv on .csv re-renders floor", "legacy/t571/data.csv",
			"text/csv; charset=utf-8", "application/octet-stream"},
		{"old declared application/json on .txt re-renders table", "legacy/t571/data.txt",
			"application/json", "text/plain"},
		// The OCI mediaType carve-out: docker/helmoci manifest nodes keep the
		// stored (client-negotiated) mediaType — T-32 R3, excluded from the
		// ruling.
		{"docker manifest mediaType passes through", "app/manifests/abc123",
			"application/vnd.docker.distribution.manifest.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json"},
		{"helmoci manifest mediaType passes through", "charts/app/manifests/def456",
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.oci.image.manifest.v1+json"},
		// Verbatim factory spellings on the wire.
		{"swift keeps the factory trailing space", "swift/t571/a.swift",
			"", "text/x-swift "},
		{"xsl first registration wins", "xsl/t571/a.xsl",
			"", "text/xsl"},
		{"multi-segment uses the final segment", "pack/t571/a.jar.pack.gz",
			"", "application/x-gzip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stored != "" {
				seedStored(t, h, "mime-render", tc.path, tc.stored)
			} else {
				resp := h.do(http.MethodPut, "/binflow/mime-render/"+tc.path,
					adminUser, adminPass, []byte("mime-render"), nil)
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("PUT = %d", resp.StatusCode)
				}
			}
			if got := fileInfoMimeType(t, h, "mime-render", tc.path); got != tc.want {
				t.Errorf("mimeType = %q, want %q", got, tc.want)
			}
		})
	}
}
