package generic_test

// T-593 / BIN-75 (R12's C2 joint ruling, ledger
// generic/checksum-get-virtual-ondemand): the terminal-checksum GET face's
// on-demand model, sha256-only, on BOTH read planes — the sha256 face
// answers the node's PRIMARY digest (the reference "computes" by table
// lookup under sha256 addressing, ADR-0006) and KEEPS answering it over a
// registered declaration (the live differential's l-/v-sha256-registered
// legs: a wrong-value write-through registration does not surface on the
// GET face; a correct registration is the same bytes either way), an unset
// sha1/md5 keeps the bare Checksum-not-found family, and a missing source
// keeps each plane's miss family (local: File-not-found colon form /
// virtual: Could-not-find-resource colon form), both SOURCE-addressed.
// 权威口径：reports/compatibility/L040-maven-sidecar-planes.md N3
// （c2-v-get-*）+ reports/compatibility/L041-policy-ct-header-faces.md
// Arm 1（g1-get-sha256-unset）/ Arm 7（#9a/#9b 白名单）+ T-593 活体差分
// （l-/v-sha256-registered 腿，reports/agents/T-593.md）。

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestChecksumGetOndemandSha256Matrix walks the C2 matrix on the local and
// the virtual plane: the value face (registered echo > computed sha256 >
// the bare 404 family) and the miss face (each plane's colon-form wording,
// source-addressed). Every value leg seeds its OWN source artifact so
// registrations never bleed across legs.
func TestChecksumGetOndemandSha256Matrix(t *testing.T) {
	e := newVirtualEnv(t)
	payload := "t593-ondemand-payload"
	sha256S, _, _ := digestsOf(payload)
	wrongSha256 := strings.Repeat("e", 64)

	// seed lands the source artifact through the given repo key (the
	// virtual key routes onto its member) and, when register is set, rides
	// the PUT family's 409 write-through to register a WRONG sha256.
	seed := func(repoKey, src string, register bool) {
		t.Helper()
		if resp := e.do(t, http.MethodPut, "/binflow/"+repoKey+"/"+src,
			strings.NewReader(payload), nil); resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed PUT %s/%s = %d (body=%s)", repoKey, src, resp.StatusCode, body(t, resp))
		}
		if !register {
			return
		}
		resp := e.do(t, http.MethodPut, "/binflow/"+repoKey+"/"+src+".sha256",
			strings.NewReader(wrongSha256), nil)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("register PUT %s/%s.sha256 = %d, want 409 write-through (body=%s)",
				repoKey, src, resp.StatusCode, body(t, resp))
		}
	}

	tests := []struct {
		name     string
		repo     string
		src      string // source artifact path
		suffix   string // terminal checksum suffix the GET/HEAD addresses
		seed     bool   // seed the source (and the registration) first
		register bool   // register a wrong sha256 via the 409 write-through
		head     bool   // exercise the HEAD rendering instead of GET
		want     int
		wantMsg  string // errors[] envelope substring on 404 legs
		wantOut  string // bare digest body on 200 GET legs
	}{
		// ---- local plane ----
		{name: "local sha256 unset answers computed digest on demand",
			repo: "generic-local", src: "t593/a.bin", suffix: ".sha256", seed: true,
			want: http.StatusOK, wantOut: sha256S},
		{name: "local sha256 registered wrong still answers computed (primary digest wins)",
			repo: "generic-local", src: "t593/b.bin", suffix: ".sha256", seed: true, register: true,
			want: http.StatusOK, wantOut: sha256S},
		{name: "local md5 unset keeps bare Checksum-not-found",
			repo: "generic-local", src: "t593/c.bin", suffix: ".md5", seed: true,
			want: http.StatusNotFound, wantMsg: `"Checksum not found for t593/c.bin"`},
		{name: "local sha1 unset keeps bare Checksum-not-found",
			repo: "generic-local", src: "t593/d.bin", suffix: ".sha1", seed: true,
			want: http.StatusNotFound, wantMsg: `"Checksum not found for t593/d.bin"`},
		{name: "local source absent keeps File-not-found colon form",
			repo: "generic-local", src: "t593/never.txt", suffix: ".sha256",
			want: http.StatusNotFound, wantMsg: `"File not found.; Path: 'generic-local:t593/never.txt'"`},
		{name: "local HEAD renders the same on-demand face",
			repo: "generic-local", src: "t593/a.bin", suffix: ".sha256", seed: true,
			head: true, want: http.StatusOK},
		// ---- virtual plane ----
		{name: "virtual sha256 unset answers computed digest on demand",
			repo: "gvirt", src: "t593/v-a.bin", suffix: ".sha256", seed: true,
			want: http.StatusOK, wantOut: sha256S},
		{name: "virtual sha256 registered wrong still answers computed (primary digest wins)",
			repo: "gvirt", src: "t593/v-b.bin", suffix: ".sha256", seed: true, register: true,
			want: http.StatusOK, wantOut: sha256S},
		{name: "virtual md5 unset keeps bare Checksum-not-found",
			repo: "gvirt", src: "t593/v-c.bin", suffix: ".md5", seed: true,
			want: http.StatusNotFound, wantMsg: `"Checksum not found for t593/v-c.bin"`},
		{name: "virtual sha1 unset keeps bare Checksum-not-found",
			repo: "gvirt", src: "t593/v-d.bin", suffix: ".sha1", seed: true,
			want: http.StatusNotFound, wantMsg: `"Checksum not found for t593/v-d.bin"`},
		{name: "virtual source unresolvable sha256 keeps Could-not-find colon form",
			repo: "gvirt", src: "t593/v-never.txt", suffix: ".sha256",
			want: http.StatusNotFound, wantMsg: `"Could not find resource; Path: 'gvirt:t593/v-never.txt'"`},
		{name: "virtual source unresolvable md5 same miss family",
			repo: "gvirt", src: "t593/v-never.txt", suffix: ".md5",
			want: http.StatusNotFound, wantMsg: `"Could not find resource; Path: 'gvirt:t593/v-never.txt'"`},
		{name: "virtual un-routed plane renders the same miss through the read plane",
			repo: "gvirt-noroute", src: "t593/n-never.txt", suffix: ".sha256",
			want: http.StatusNotFound, wantMsg: `"Could not find resource; Path: 'gvirt-noroute:t593/n-never.txt'"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.seed {
				seed(tt.repo, tt.src, tt.register)
			}
			method := http.MethodGet
			if tt.head {
				method = http.MethodHead
			}
			get := e.do(t, method, "/binflow/"+tt.repo+"/"+tt.src+tt.suffix, nil, nil)
			got := body(t, get)
			if get.StatusCode != tt.want {
				t.Fatalf("%s = %d, want %d (body=%s)", method, get.StatusCode, tt.want, got)
			}
			if tt.wantMsg != "" && !strings.Contains(got, tt.wantMsg) {
				t.Fatalf("body = %s, want message %q", got, tt.wantMsg)
			}
			if tt.want == http.StatusOK {
				if ct := get.Header.Get("Content-Type"); ct != "application/x-checksum" {
					t.Fatalf("Content-Type = %q, want application/x-checksum", ct)
				}
				if tt.wantOut != "" {
					if cl := get.Header.Get("Content-Length"); cl != strconv.Itoa(len(tt.wantOut)) {
						t.Fatalf("Content-Length = %q, want %d", cl, len(tt.wantOut))
					}
				}
				if tt.head && got != "" {
					t.Fatalf("HEAD body = %q, want empty", got)
				}
			}
		})
	}
}

// TestChecksumGetOndemandSrvgenGuardFirst pins ADR-0052 decision 6's order
// on the completed face: the srvgen guard runs BEFORE the registered and
// on-demand arms, so a registered-but-wrong sha256 still answers the
// COMPUTED digest under server-generated-checksums (zero disturbance to the
// T-584 policy face from the sha256 on-demand arm).
func TestChecksumGetOndemandSrvgenGuardFirst(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t593-srvgen", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"checksumPolicyType":"server-generated-checksums"}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	src := "t593/pol/src.bin"
	payload := "t593-srvgen-payload"
	sha256S, _, md5C := digestsOf(payload)
	if resp := e.do(t, http.MethodPut, "/binflow/t593-srvgen/"+src,
		strings.NewReader(payload), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (body=%s)", resp.StatusCode, body(t, resp))
	}
	// Under srvgen the wrong sha256 registers with a 201 (comparison
	// skipped) — the registration the guard must not let surface.
	wrong := strings.Repeat("e", 64)
	if resp := e.do(t, http.MethodPut, "/binflow/t593-srvgen/"+src+".sha256",
		strings.NewReader(wrong), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("srvgen register = %d, want 201 (body=%s)", resp.StatusCode, body(t, resp))
	}
	for _, tc := range []struct{ algo, want string }{
		{"sha256", sha256S}, // guard first: computed beats the registered wrong value
		{"md5", md5C},       // unchanged srvgen posture for an unset algorithm
	} {
		get := e.do(t, http.MethodGet, "/binflow/t593-srvgen/"+src+"."+tc.algo, nil, nil)
		if get.StatusCode != http.StatusOK {
			t.Fatalf("srvgen GET .%s = %d (body=%s)", tc.algo, get.StatusCode, body(t, get))
		}
		if got := body(t, get); got != tc.want {
			t.Errorf("srvgen GET .%s = %q, want computed %q (guard precedes the arms)", tc.algo, got, tc.want)
		}
	}
}
