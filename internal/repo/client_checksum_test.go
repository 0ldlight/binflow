// T-578 / BIN-60 — the client-checksum persistence seam (ADR-0052):
// repo.Service.SetClientChecksums is the adapter-facing registration face
// (per-algorithm absolute overwrite, no node creation, no usage change, no
// calculator trigger, write-gated, anonymous refused) and repo.OriginalChecksums
// is the single-source overlay rule every consumer renders through.
package repo_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// seam resolves the optional ClientChecksumWriter segment off the assembled
// service (ADR-0052 decision 1: an optional SPI segment, the RemoteV2Plane
// precedent — the concrete service always implements it).
func seam(t *testing.T, e *env) repo.ClientChecksumWriter {
	t.Helper()
	w, ok := e.svc.(repo.ClientChecksumWriter)
	if !ok {
		t.Fatalf("assembled service does not implement ClientChecksumWriter")
	}
	return w
}

// TestSetClientChecksumsSemantics walks the seam's whole ruled surface
// (ADR-0052 decision 1, semantics 1-6).
func TestSetClientChecksumsSemantics(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	w := seam(t, e)

	seed := func(path, mime, body string) {
		t.Helper()
		if _, err := e.svc.Put(ctx, admin(), "lib", path, strings.NewReader(body),
			storage.BlobRef{}, mime); err != nil {
			t.Fatalf("seed %s: %v", path, err)
		}
	}
	seed("g/a.jar", "application/java-archive", "artifact")
	before, err := e.md.Nodes().Get(ctx, "lib", "g/a.jar")
	if err != nil {
		t.Fatalf("node: %v", err)
	}

	sha1A, sha1B := strings.Repeat("11", 20), strings.Repeat("22", 20)

	// Per-algorithm overwrite: each SET touches only its column.
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/a.jar", storage.BlobRef{Sha1: sha1A}); err != nil {
		t.Fatalf("sha1 registration: %v", err)
	}
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/a.jar", storage.BlobRef{Md5: strings.Repeat("33", 16)}); err != nil {
		t.Fatalf("md5 registration: %v", err)
	}
	n, err := e.md.Nodes().Get(ctx, "lib", "g/a.jar")
	if err != nil {
		t.Fatalf("node after registrations: %v", err)
	}
	if n.ClientSha1 != sha1A || n.ClientMd5 != strings.Repeat("33", 16) || n.ClientSha256 != "" {
		t.Errorf("client columns = (%q,%q,%q), want sha1+md5 set, sha256 untouched",
			n.ClientSha1, n.ClientMd5, n.ClientSha256)
	}

	// Overwrite is absolute per algorithm, siblings untouched.
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/a.jar", storage.BlobRef{Sha1: sha1B}); err != nil {
		t.Fatalf("sha1 overwrite: %v", err)
	}
	n, _ = e.md.Nodes().Get(ctx, "lib", "g/a.jar")
	if n.ClientSha1 != sha1B || n.ClientMd5 != strings.Repeat("33", 16) {
		t.Errorf("client columns after overwrite = (%q,%q), want sha1 replaced, md5 kept", n.ClientSha1, n.ClientMd5)
	}

	// The empty ref is a no-op (the .sha512 family's posture), not a clear.
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/a.jar", storage.BlobRef{}); err != nil {
		t.Fatalf("empty ref: %v", err)
	}
	n, _ = e.md.Nodes().Get(ctx, "lib", "g/a.jar")
	if n.ClientSha1 != sha1B || n.ClientMd5 != strings.Repeat("33", 16) {
		t.Errorf("client columns after empty ref = (%q,%q), want unchanged", n.ClientSha1, n.ClientMd5)
	}

	// A registration is a pure metadata write: no updated_at, no usage.
	if n.UpdatedAt != before.UpdatedAt || n.CreatedAt != before.CreatedAt {
		t.Errorf("timestamps moved: updated %q->%q, created %q->%q",
			before.UpdatedAt, n.UpdatedAt, before.CreatedAt, n.CreatedAt)
	}
	if st, serr := e.md.Nodes().Stats(ctx, "lib", "g/a.jar"); serr == nil && st.DownloadCount != 0 {
		t.Errorf("download_count = %d, want 0 (no usage side effects)", st.DownloadCount)
	}

	// No node creation: a miss reports ErrNodeNotFound, nothing lands.
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/missing.jar", storage.BlobRef{Sha1: sha1A}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Errorf("missing node err = %v, want ErrNodeNotFound", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "lib", "g/missing.jar"); err == nil {
		t.Error("missing node materialized")
	}

	// Folder targets refuse (a folder is never a client-checksum target).
	seed("g/dir/", "application/octet-stream", "")
	if err := w.SetClientChecksums(ctx, admin(), "lib", "g/dir/", storage.BlobRef{Sha1: sha1A}); err == nil {
		t.Error("folder target: want error")
	}

	// Invalid paths refuse before any store consult.
	for _, path := range []string{"../escape", "a/../../b", "/abs"} {
		if err := w.SetClientChecksums(ctx, admin(), "lib", path, storage.BlobRef{Sha1: sha1A}); err == nil {
			t.Errorf("path %q: want ErrInvalidPath", path)
		}
	}
}

// TestSetClientChecksumsGates pins the seam's permission surface: anonymous
// is refused with the unauthorized sentinel, a non-writer with the forbidden
// one (the gate is the WRITE action, ADR-0052 decision 1.4).
func TestSetClientChecksumsGates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	w := seam(t, e)
	if _, err := e.svc.Put(ctx, admin(), "lib", "g/a.jar", strings.NewReader("artifact"),
		storage.BlobRef{}, "application/java-archive"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ref := storage.BlobRef{Sha1: strings.Repeat("11", 20)}

	if err := w.SetClientChecksums(ctx, nil, "lib", "g/a.jar", ref); err == nil {
		t.Error("anonymous: want ErrUnauthorized")
	}
	if err := w.SetClientChecksums(ctx, alice(), "lib", "g/a.jar", ref); err == nil {
		t.Error("non-writer: want ErrForbidden")
	}
	n, _ := e.md.Nodes().Get(ctx, "lib", "g/a.jar")
	if n.ClientSha1 != "" {
		t.Errorf("refused registrations leaked a column: %q", n.ClientSha1)
	}
	// A granted writer registers.
	e.az.add("alice", repo.ActionWrite, "")
	if err := w.SetClientChecksums(ctx, alice(), "lib", "g/a.jar", ref); err != nil {
		t.Fatalf("granted writer: %v", err)
	}
}

// TestOriginalChecksumsOverlay pins the shared overlay rule (ADR-0052
// decision 4): a non-empty Client column wins per algorithm, everything
// else falls to the caller-supplied server triple — the one function every
// consumer face renders through.
func TestOriginalChecksumsOverlay(t *testing.T) {
	node := &metadata.Node{
		ClientMd5:    "cm5",
		ClientSha1:   "cs1",
		ClientSha256: "c256",
	}
	cases := []struct {
		name         string
		node         *metadata.Node
		s256, s1, s5 string
		w256, w1, w5 string
	}{
		{"client wins per algo", node, "s256", "s1", "s5", "c256", "cs1", "cm5"},
		{"per-algo partial", &metadata.Node{ClientSha1: "cs1"}, "s256", "s1", "s5", "s256", "cs1", "s5"},
		{"nil node passes through", nil, "s256", "s1", "s5", "s256", "s1", "s5"},
		{"zero node passes through", &metadata.Node{}, "s256", "s1", "s5", "s256", "s1", "s5"},
		{"empty server values", node, "", "", "", "c256", "cs1", "cm5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g256, g1, g5 := repo.OriginalChecksums(tc.node, tc.s256, tc.s1, tc.s5)
			if g256 != tc.w256 || g1 != tc.w1 || g5 != tc.w5 {
				t.Errorf("OriginalChecksums = (%q,%q,%q), want (%q,%q,%q)",
					g256, g1, g5, tc.w256, tc.w1, tc.w5)
			}
		})
	}
}
