package docker

// LOOP 006 L006-2: the virtual walk's cold-miss negative memory — the
// member-facts seam (V2MemberManifest's !ok arm) probes the reference-keyed
// miss row the walk itself wrote on the upstream 404, so a repeated ask
// through the VIRTUAL reuses the MEMBER's miss record instead of re-running
// the member's upstream conversation inside the missedTTL window — the same
// memory the member's own DIRECT face consults (cross-face consistent, the
// C14 verdict extended to the aggregation). Both reference shapes ride:
// the tag keys the image's tags/ namespace, the digest the manifest node
// path — and because the WRITE side spells the key in the adapter
// (manifestMissNodePath) while the READ probe spells it in the repo seam,
// this walk-level test is what locks the two spellings together end to
// end (a drift would silently degrade to the re-ask posture, no error).
//
// The upstream counter is the oracle; the miss rows are read straight from
// the member's remote cache table.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedDockerVirtualRepo creates the virtual docker repository through the
// REAL service with its member ledger in the config (docker is a core-five
// slot: the T-431 matrix cell, no license gate on this stack).
func (rs *remotePullStack) seedDockerVirtualRepo(t *testing.T, key string, members ...string) {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{"repositories": members})
	if err != nil {
		t.Fatalf("marshal the member config: %v", err)
	}
	if _, err := rs.svc.CreateRepo(context.Background(), rs.admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: repo.PackageDocker, Config: string(cfg),
	}); err != nil {
		t.Fatalf("create virtual docker repository %s: %v", key, err)
	}
}

// TestVirtualColdMissNegativeCache: the virtual walk's cold-miss memory —
// tag and digest miss rows, the frozen upstream inside the window, the
// expiry re-ask, and the pull-through escape (a tag the upstream HAS still
// serves through the same virtual after misses were recorded).
func TestVirtualColdMissNegativeCache(t *testing.T) {
	known := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"%s","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:%s","size":2}}`,
		mediaTypeDockerManifest, sha256Hex([]byte("{}"))))
	knownDgst := "sha256:" + sha256Hex(known)

	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/up/known/manifests/1.0" {
			hits.Add(1)
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.Header().Set("Docker-Content-Digest", knownDgst)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(known)
			return
		}
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound) // every other reference misses upstream
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "virt-remote", up.URL)
	down.seedDockerVirtualRepo(t, "docker-virt", "virt-remote")
	virt := "docker-virt"
	member := "virt-remote"

	assert404 := func(t *testing.T, path string) {
		t.Helper()
		code, body, _ := down.get(path, acceptManifests)
		if code != http.StatusNotFound {
			t.Fatalf("GET %s = (%d, %s), want the unfound family", path, code, body)
		}
		var eb specErrorBody
		if err := json.Unmarshal([]byte(body), &eb); err != nil || len(eb.Errors) != 1 ||
			eb.Errors[0].Code != ErrCodeManifestUnknown {
			t.Fatalf("GET %s body %q: want one %s entry (%v)", path, body, ErrCodeManifestUnknown, err)
		}
	}

	t.Run("tag cold miss reuses the member's miss row", func(t *testing.T) {
		path := "/v2/" + virt + "/up/known/manifests/l006miss"
		assert404(t, path)
		if got := hits.Load(); got != 1 {
			t.Fatalf("first virtual tag miss: upstream contacts = %d, want 1", got)
		}
		assert404(t, path)
		assert404(t, path)
		if got := hits.Load(); got != 1 {
			t.Fatalf("repeat virtual tag misses: upstream contacts = %d, want the count frozen at 1", got)
		}
		row, err := down.md.Remote().GetCache(context.Background(), member, "up/known/tags/l006miss")
		if err != nil || row == nil {
			t.Fatalf("miss row up/known/tags/l006miss on the member: (%v, %v), want the negative record", row, err)
		}
		if row.Kind != "negative" {
			t.Errorf("miss row kind = %q, want %q", row.Kind, "negative")
		}
	})

	t.Run("digest cold miss keys the member's manifest node path", func(t *testing.T) {
		absent := sha256Hex([]byte("l0062-absent-manifest"))
		path := "/v2/" + virt + "/up/known/manifests/sha256:" + absent
		before := hits.Load()
		assert404(t, path)
		assert404(t, path)
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("virtual digest miss x3: upstream contacts = %d, want 1", got)
		}
		row, err := down.md.Remote().GetCache(context.Background(), member, "up/known/manifests/"+absent)
		if err != nil || row == nil || row.Kind != "negative" {
			t.Fatalf("digest miss row: (%v, %v), want the negative record at the member's manifest node path", row, err)
		}
	})

	t.Run("the expired window re-asks the member's upstream", func(t *testing.T) {
		path := "/v2/" + virt + "/up/known/manifests/l006miss"
		past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
		if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
			RepoKey: member, Path: "up/known/tags/l006miss", Kind: "negative", FetchedAt: past, ExpiresAt: past,
		}); err != nil {
			t.Fatalf("expire the miss row: %v", err)
		}
		before := hits.Load()
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("post-expiry virtual miss: upstream contacts = %d, want the re-ask (1)", got)
		}
		assert404(t, path)
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("post-expiry repeat: upstream contacts = %d, want frozen", got)
		}
	})

	t.Run("a tag the upstream has still pulls through the virtual", func(t *testing.T) {
		before := hits.Load()
		code, body, hdr := down.get("/v2/"+virt+"/up/known/manifests/1.0", acceptManifests)
		if code != http.StatusOK || body != string(known) {
			t.Fatalf("virtual pull of the existing tag = (%d, %d bytes), want the proxied copy", code, len(body))
		}
		if got := hdr.Get("X-BinFlow-Resolved-From"); got != member {
			t.Errorf("resolved-from = %q, want %q (the winning member)", got, member)
		}
		if got := hits.Load() - before; got != 1 {
			t.Fatalf("existing tag virtual pull: upstream contacts = %d, want 1", got)
		}
	})
}
