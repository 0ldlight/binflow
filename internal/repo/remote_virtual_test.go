package repo_test

// T-64's FR-15 repository-model suite: the remote/virtual configuration
// surface, defaults, validation and teardown at the Service layer — the
// service-level equivalent of the PRD's M01..M05 acceptance commands (the
// REST plane's curl wording is httpapi's to render; every rule the curls
// probe lives here).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// eventsOf snapshots the recorded audit events (details included).
func eventsOf(l *auditLog) []repo.AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]repo.AuditEvent, len(l.events))
	copy(out, l.events)
	return out
}

// mustCreateTypedRepo creates one local repository of the given package type.
func mustCreateTypedRepo(t TB, e *env, key, packageType string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: packageType,
	}); err != nil {
		t.Fatalf("CreateRepo(%s, %s): %v", key, packageType, err)
	}
}

// mustCreateRemote creates one remote repository around the given config.
func mustCreateRemote(t TB, e *env, key, config string) *metadata.Repo {
	t.Helper()
	r, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: config,
	})
	if err != nil {
		t.Fatalf("CreateRepo(remote %s): %v", key, err)
	}
	return r
}

// remoteCfgOf decodes one remote row's canonical config for assertions.
func remoteCfgOf(t testing.TB, row *metadata.Repo) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Config), &m); err != nil {
		t.Fatalf("canonical config %q: %v", row.Config, err)
	}
	return m
}

// ---- M01: the three protocol package types on local repositories ----

// TestM01LocalProtocolRepos: maven/npm/pypi local repositories create and
// round-trip their packageType (FR-15-AC1; the M01 curls).
func TestM01LocalProtocolRepos(t *testing.T) {
	ctx := context.Background()
	for _, pt := range []string{repo.PackageMaven, repo.PackageNpm, repo.PackagePypi} {
		t.Run(pt, func(t *testing.T) {
			e := newEnv(t)
			key := pt + "-local"
			created, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeLocal, PackageType: pt,
			})
			if err != nil {
				t.Fatalf("CreateRepo(%s): %v", key, err)
			}
			if created.Type != repo.TypeLocal || created.PackageType != pt {
				t.Fatalf("created = %s/%s, want local/%s", created.Type, created.PackageType, pt)
			}
			got, err := e.svc.GetRepo(ctx, admin(), key)
			if err != nil {
				t.Fatalf("GetRepo: %v", err)
			}
			if got.PackageType != pt {
				t.Fatalf("round-trip packageType = %q, want %q", got.PackageType, pt)
			}
		})
	}
}

// ---- M02/M02b: remote repository fields, defaults and validation ----

// TestM02RemoteCreateStoresConfig: the M02 flow — a remote repository with a
// URL and the SSRF exemption creates, the remote_configs row carries the
// product defaults, and the GET echo is the canonical form without any
// password (NFR-S14: the credential is ACCEPTED on input and never
// persisted — T-66 owns the encrypted chain, the T-62 review closed the
// plaintext window).
func TestM02RemoteCreateStoresConfig(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	created, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey:     "generic-remote",
		Type:        repo.TypeRemote,
		PackageType: repo.PackageGeneric,
		Config: `{"url":"http://127.0.0.1:9099/","username":"ci","password":"s3cret",` +
			`"allowPrivateUpstream":true}`,
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if created.Type != repo.TypeRemote {
		t.Fatalf("type = %s, want remote", created.Type)
	}

	// The remote_configs row: url trimmed of the trailing slash, product
	// TTL defaults (7200/600 — the DDL's 86400 fallback never applies to
	// service-created rows), the exemption recorded, the password EMPTY.
	row, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.URL != "http://127.0.0.1:9099" {
		t.Fatalf("row url = %q, want the trimmed form", row.URL)
	}
	if row.Username != "ci" {
		t.Fatalf("row username = %q", row.Username)
	}
	if row.Password != "" {
		t.Fatalf("row password = %q, want empty (no plaintext window before T-66)", row.Password)
	}
	if !row.AllowPrivateUpstream {
		t.Fatal("row allowPrivateUpstream = false, want true")
	}
	if row.ContentTTLSeconds != 7200 || row.MetadataTTLSeconds != 600 {
		t.Fatalf("row ttls = %d/%d, want 7200/600", row.ContentTTLSeconds, row.MetadataTTLSeconds)
	}

	// The GET echo: canonical config, url verbatim (trimmed), every default
	// materialized, and NO password anywhere in the body.
	got, err := e.svc.GetRepo(ctx, admin(), "generic-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if strings.Contains(got.Config, "s3cret") || strings.Contains(got.Config, "password") {
		t.Fatalf("echoed config leaks the credential: %s", got.Config)
	}
	cfg := remoteCfgOf(t, got)
	want := map[string]any{
		"url":                            "http://127.0.0.1:9099",
		"retrievalCachePeriodSecs":       float64(7200),
		"missedRetrievalCachePeriodSecs": float64(1800),
		"socketTimeoutSecs":              float64(15),
		"assumedOfflinePeriodSecs":       float64(300),
		"hardFail":                       false,
		"allowPrivateUpstream":           true,
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Fatalf("config[%s] = %v (%T), want %v", k, cfg[k], cfg[k], v)
		}
	}

	// The create audit carries the exemption value (NFR-S14 point 5).
	found := false
	for _, ev := range eventsOf(e.au) {
		if ev.Action == repo.AuditActionRepoCreate && ev.Repo == "generic-remote" &&
			strings.Contains(ev.Detail, `"allowPrivateUpstream":true`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no repo.create audit event carrying allowPrivateUpstream")
	}
}

// TestM02bRemoteConfigValidation: the M02b refusal family — every branch is
// a 400-shaped ErrInvalidRepoConfig whose message names the offending field
// (FR-15-AC3), and the private-address URLs that must PASS because
// create-time validation is scheme/format only (the SSRF chain runs per
// request, NFR-S13).
func TestM02bRemoteConfigValidation(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantSubstr string // "" means the create must SUCCEED
	}{
		{"missing url (no config)", "", "url is required"},
		{"missing url (empty object)", `{}`, "url is required"},
		{"missing url (blank)", `{"url":"   "}`, "url is required"},
		{"file scheme", `{"url":"file:///etc"}`, "scheme must be http or https"},
		{"ftp scheme", `{"url":"ftp://x"}`, "scheme must be http or https"},
		{"no host", `{"url":"http://"}`, "host is required"},
		{"not json", `nonsense`, "remote repository config"},
		{"negative retrieval", `{"url":"http://u","retrievalCachePeriodSecs":-1}`, "must not be negative"},
		{"negative missed", `{"url":"http://u","missedRetrievalCachePeriodSecs":-5}`, "must not be negative"},
		{"negative socket", `{"url":"http://u","socketTimeoutSecs":-15}`, "must not be negative"},
		{"negative assumed offline", `{"url":"http://u","assumedOfflinePeriodSecs":-300}`, "must not be negative"},
		{"private loopback passes (scheme-only check)", `{"url":"http://127.0.0.1:9099"}`, ""},
		{"private rfc1918 passes", `{"url":"http://192.168.1.5/up"}`, ""},
		{"link-local metadata IP passes (request-time chain owns it)", `{"url":"http://169.254.169.254/latest"}`, ""},
		{"unknown Artifactory fields tolerated (scenario D)", `{"url":"http://u","proxyRef":"","shareConfiguration":false}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
				Config: tt.config,
			})
			if tt.wantSubstr == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("error = %v, want ErrInvalidRepoConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not name %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestM02RemoteExplicitValues: caller-supplied periods override the product
// defaults in both the canonical config and the remote_configs row (an
// explicit 0 IS the value since ADR-0050 — see TestM02ZeroPeriodsStoreZero).
func TestM02RemoteExplicitValues(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "tuned-remote", `{"url":"https://up.example.org/m2",`+
		`"retrievalCachePeriodSecs":3600,"missedRetrievalCachePeriodSecs":60,`+
		`"socketTimeoutSecs":30,"assumedOfflinePeriodSecs":120,"hardFail":true,`+
		`"retrievalZeroKeepsDefault":true}`)

	row, err := e.md.Remote().GetConfig(ctx, "tuned-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.ContentTTLSeconds != 3600 {
		t.Fatalf("content ttl = %d, want 3600", row.ContentTTLSeconds)
	}
	got, err := e.svc.GetRepo(ctx, admin(), "tuned-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	cfg := remoteCfgOf(t, got)
	for k, v := range map[string]any{
		"retrievalCachePeriodSecs":       float64(3600),
		"missedRetrievalCachePeriodSecs": float64(60),
		"socketTimeoutSecs":              float64(30),
		"assumedOfflinePeriodSecs":       float64(120),
		"hardFail":                       true,
	} {
		if cfg[k] != v {
			t.Fatalf("config[%s] = %v, want %v", k, cfg[k], v)
		}
	}
}

// TestM02ZeroPeriodsStoreZero: since ADR-0050 decision 3 an explicit 0 IS
// the stored value on the create face too (the reference has no
// zero-means-default rule); the fetch side keeps its own 0=unset fallback
// chain — wire/storage and effect are layered.
func TestM02ZeroPeriodsStoreZero(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "zero-remote", `{"url":"http://u","retrievalCachePeriodSecs":0,"socketTimeoutSecs":0}`)
	row, err := e.md.Remote().GetConfig(ctx, "zero-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.ContentTTLSeconds != 0 {
		t.Fatalf("content ttl = %d, want the explicit 0", row.ContentTTLSeconds)
	}
	if row.SocketTimeoutMs != 0 {
		t.Fatalf("socket timeout ms = %d, want the explicit 0", row.SocketTimeoutMs)
	}
}

// ---- M03: virtual repositories ----

// TestM03VirtualCreateAndMembers: the M03 flow — a virtual repository over a
// local and a remote member creates, the member list lands in declaration
// order (position = bucket-internal declaration order, ADR-0013 as amended),
// and the canonical echo round-trips.
func TestM03VirtualCreateAndMembers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://127.0.0.1:9099/m2"}`)

	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
		Config: `{"repositories":["maven-remote-x","maven-local"],"defaultDeploymentRepo":"maven-local"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual): %v", err)
	}

	members, err := e.md.Virtual().ListMembers(ctx, "maven-virtual")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 ||
		members[0].MemberRepo != "maven-remote-x" || members[0].Position != 0 ||
		members[1].MemberRepo != "maven-local" || members[1].Position != 1 {
		t.Fatalf("members = %+v, want declaration order remote-x(0), local(1)", members)
	}

	got, err := e.svc.GetRepo(ctx, admin(), "maven-virtual")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	var cfg struct {
		Repositories          []string `json:"repositories"`
		DefaultDeploymentRepo string   `json:"defaultDeploymentRepo"`
	}
	if err := json.Unmarshal([]byte(got.Config), &cfg); err != nil {
		t.Fatalf("canonical config %q: %v", got.Config, err)
	}
	if fmt.Sprint(cfg.Repositories) != "[maven-remote-x maven-local]" {
		t.Fatalf("repositories = %v", cfg.Repositories)
	}
	if cfg.DefaultDeploymentRepo != "maven-local" {
		t.Fatalf("defaultDeploymentRepo = %q", cfg.DefaultDeploymentRepo)
	}
}

// TestM03VirtualValidation: the M03 refusal family (FR-15-AC4).
func TestM03VirtualValidation(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) *env {
		t.Helper()
		e := newEnv(t)
		mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
		mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://127.0.0.1:9099/m2"}`)
		return e
	}
	tests := []struct {
		name       string
		config     string
		wantSubstr string
	}{
		{"no config", "", "repositories is required"},
		{"empty object", `{}`, "repositories is required"},
		{"empty member list", `{"repositories":[]}`, "repositories is required"},
		{"unknown member", `{"repositories":["no-such-repo"]}`, `"no-such-repo" does not exist`},
		{"nested virtual", `{"repositories":["maven-local","wrap"]}`, "nested virtual"},
		{"duplicate member", `{"repositories":["maven-local","maven-local"]}`, "more than once"},
		{"default targets non-member", `{"repositories":["maven-local"],"defaultDeploymentRepo":"maven-remote-x"}`, "is not a member"},
		{"default targets remote member", `{"repositories":["maven-local","maven-remote-x"],"defaultDeploymentRepo":"maven-remote-x"}`, "must be a local repository member"},
		{"aliases disagree", `{"repositories":["maven-local"],"defaultDeploymentRepo":"maven-local","deploymentRepository":"maven-remote-x"}`, "aliases disagree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := seed(t)
			if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: "wrap", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
				Config: `{"repositories":["maven-local"]}`,
			}); err != nil {
				t.Fatalf("seed nested virtual: %v", err)
			}
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
				Config: tt.config,
			})
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("error = %v, want ErrInvalidRepoConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestM03VirtualSelfReferenceOnUpdate: create runs before the row exists so
// self-listing is impossible there, but an UPDATE can try it — the refusal
// keeps both paths one shape.
func TestM03VirtualSelfReferenceOnUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
		Config: `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	_, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Config: `{"repositories":["maven-virtual"]}`,
	})
	if !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "cannot list itself") {
		t.Fatalf("self-reference error = %v", err)
	}
}

// TestM03VirtualDeploymentRepoAliases: the three Artifactory spellings of
// the write-route target are all accepted ("并收") and canonicalized onto
// the primary name.
func TestM03VirtualDeploymentRepoAliases(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		key    string
		config string
	}{
		{"primary", "v-primary", `{"repositories":["maven-local"],"defaultDeploymentRepo":"maven-local"}`},
		{"spec name defaultDeploymentRepoRef", "v-spec-ref", `{"repositories":["maven-local"],"defaultDeploymentRepoRef":"maven-local"}`},
		{"console name deploymentRepository", "v-console", `{"repositories":["maven-local"],"deploymentRepository":"maven-local"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
			key := tc.key
			if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeVirtual, PackageType: repo.PackageMaven, Config: tc.config,
			}); err != nil {
				t.Fatalf("CreateRepo: %v", err)
			}
			got, err := e.svc.GetRepo(ctx, admin(), key)
			if err != nil {
				t.Fatalf("GetRepo: %v", err)
			}
			if !strings.Contains(got.Config, `"defaultDeploymentRepo":"maven-local"`) {
				t.Fatalf("canonical config = %s, want the primary alias spelling", got.Config)
			}
		})
	}
}

// TestVirtualMemberUpdateImmediateEffect: member-list changes land
// atomically and are visible to the very next read — there is no resolution
// cache between SetMembers and ListMembers (FR-15-AC6's second half: the
// T-71 resolver computes per request off this ledger).
func TestVirtualMemberUpdateImmediateEffect(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	mustCreateTypedRepo(t, e, "npm-local", repo.PackageNpm)
	mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://127.0.0.1:9099/m2"}`)

	create := func(config string) {
		t.Helper()
		if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven, Config: config,
		}); err != nil {
			t.Fatalf("CreateRepo: %v", err)
		}
	}
	create(`{"repositories":["maven-local","maven-remote-x"]}`)

	// Reorder: remote first.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Config: `{"repositories":["maven-remote-x","maven-local"]}`,
	}); err != nil {
		t.Fatalf("UpdateRepo(reorder): %v", err)
	}
	members, err := e.md.Virtual().ListMembers(ctx, "maven-virtual")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].MemberRepo != "maven-remote-x" || members[1].MemberRepo != "maven-local" {
		t.Fatalf("members after reorder = %+v", members)
	}

	// Description-only update: the member list must NOT move (no config, no
	// member rewrite).
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: "maven-virtual", Description: "renamed"}); err != nil {
		t.Fatalf("UpdateRepo(description): %v", err)
	}
	members, err = e.md.Virtual().ListMembers(ctx, "maven-virtual")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].MemberRepo != "maven-remote-x" {
		t.Fatalf("members after description-only update = %+v", members)
	}

	// Shrink to one member: the ledger reflects exactly the new list.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Config: `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("UpdateRepo(shrink): %v", err)
	}
	members, err = e.md.Virtual().ListMembers(ctx, "maven-virtual")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].MemberRepo != "maven-local" {
		t.Fatalf("members after shrink = %+v", members)
	}
}

// ---- priorityResolution: the member marking T-71's two buckets read ----

// TestPriorityResolutionMemberMarking: the per-repo flag (repo-semantics
// sections 7.1/8.1, PRD C3) persists on local configs verbatim (the M1
// passthrough contract) and canonicalizes into the remote form; a non-bool
// value is refused so the T-71 reader never trips over a mangled blob.
func TestPriorityResolutionMemberMarking(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-local", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("local create with the mark: %v", err)
	}
	got, err := e.svc.GetRepo(ctx, admin(), "maven-local")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if !strings.Contains(got.Config, `"priorityResolution":true`) {
		t.Fatalf("local config = %s, want the mark preserved verbatim", got.Config)
	}

	mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://u","priorityResolution":true}`)
	row, err := e.md.Remote().GetConfig(ctx, "maven-remote-x")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	_ = row // the mark lives in the canonical config; the row carries no column
	got, err = e.svc.GetRepo(ctx, admin(), "maven-remote-x")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if !strings.Contains(got.Config, `"priorityResolution":true`) {
		t.Fatalf("remote canonical config = %s, want the mark", got.Config)
	}

	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "bad-mark", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Config: `{"priorityResolution":"yes"}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("non-bool mark error = %v, want ErrInvalidRepoConfig", err)
	}
}

// ---- M04: the E-04 filters ----

// TestM04ListReposFiltered: exact-match type/packageType filters; unknown
// values match nothing (rest-api.md section 2's empty-array-not-error
// contract).
func TestM04ListReposFiltered(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	mustCreateTypedRepo(t, e, "docker-local", repo.PackageDocker)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://127.0.0.1:9099"}`)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
		Config: `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual): %v", err)
	}

	keysOf := func(rows []*metadata.Repo) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.RepoKey
		}
		return out
	}
	tests := []struct {
		name        string
		repoType    string
		packageType string
		want        string
	}{
		{"type=remote", repo.TypeRemote, "", "[generic-remote]"},
		{"type=virtual", repo.TypeVirtual, "", "[maven-virtual]"},
		{"type=local", repo.TypeLocal, "", "[docker-local maven-local]"},
		{"packageType=maven", "", repo.PackageMaven, "[maven-local maven-virtual]"},
		{"type+packageType", repo.TypeRemote, repo.PackageGeneric, "[generic-remote]"},
		{"type+packageType mismatch", repo.TypeRemote, repo.PackageMaven, "[]"},
		{"unknown type matches nothing", "federated", "", "[]"},
		{"unknown packageType matches nothing", "", "conda", "[]"},
		{"no filters = everything", "", "", "[docker-local generic-remote maven-local maven-virtual]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := e.svc.ListReposFiltered(ctx, admin(), tt.repoType, tt.packageType)
			if err != nil {
				t.Fatalf("ListReposFiltered: %v", err)
			}
			if got := fmt.Sprint(keysOf(rows)); got != tt.want {
				t.Fatalf("keys = %s, want %s", got, tt.want)
			}
		})
	}

	// ListRepos (the unfiltered spelling) is the same read plus nothing.
	all, err := e.svc.ListRepos(ctx, admin())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("ListRepos len = %d, want 4", len(all))
	}
}

// ---- NFR-S14: the read-boundary mask ----

// TestGetRepoMasksInjectedPassword: the canonical form never carries a
// password, but a row written by any other means (DB surgery, a future
// migration) must not leak one through the echo either.
func TestGetRepoMasksInjectedPassword(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://127.0.0.1:9099"}`)

	row, err := e.md.Repos().Get(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("store Get: %v", err)
	}
	row.Config = `{"url":"http://127.0.0.1:9099","password":"injected-secret"}`
	if err := e.md.Repos().Update(ctx, row); err != nil {
		t.Fatalf("store Update: %v", err)
	}

	got, err := e.svc.GetRepo(ctx, admin(), "generic-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if strings.Contains(got.Config, "injected-secret") || strings.Contains(got.Config, "password") {
		t.Fatalf("echoed config = %s, want the credential stripped", got.Config)
	}
	if !strings.Contains(got.Config, `"url"`) {
		t.Fatalf("echoed config = %s, want the url kept", got.Config)
	}

	// The list plane masks the same way.
	rows, err := e.svc.ListReposFiltered(ctx, admin(), repo.TypeRemote, "")
	if err != nil {
		t.Fatalf("ListReposFiltered: %v", err)
	}
	if len(rows) != 1 || strings.Contains(rows[0].Config, "injected-secret") {
		t.Fatalf("list row config = %+v, want masked", rows)
	}
}

// ---- FR-15-AC6: remote teardown ----

// seedRemoteContent lands one node plus one cache-validator row inside a
// remote repository directly through the store (the content plane refuses
// remote repositories until the T-66 engine lands, so the rows come from the
// fetcher's future write path — which is exactly what these tables hold).
func seedRemoteContent(t TB, e *env, repoKey, path string) {
	t.Helper()
	ctx := context.Background()
	sha := shaOf("cached upstream bytes " + path)
	if err := e.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: sha, Sha1: "aa", Md5: "bb", Size: 20, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed blob row: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := e.md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha, Size: 20,
		Mime: "application/octet-stream", CreatedBy: "fetcher", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node row: %v", err)
	}
	if err := e.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: repoKey, Path: path, ETag: "etag-1", FetchedAt: now, ExpiresAt: now, Kind: metadata.RemoteCacheKindContent,
	}); err != nil {
		t.Fatalf("seed cache row: %v", err)
	}
}

// TestFR15AC6RemoteDeleteCascades: deleteContent=true removes the nodes AND
// the remote_cache rows; afterwards the repository (and its config row) is
// gone and content reads answer the repository-not-found branch.
func TestFR15AC6RemoteDeleteCascades(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://127.0.0.1:9099"}`)
	seedRemoteContent(t, e, "generic-remote", "dir/up.bin")

	// Non-empty without the flag: the nodes demand deleteContent.
	if err := e.svc.DeleteRepo(ctx, admin(), "generic-remote", false); !errors.Is(err, repo.ErrRepoNotEmpty) {
		t.Fatalf("DeleteRepo(no flag) error = %v, want ErrRepoNotEmpty", err)
	}

	if err := e.svc.DeleteRepo(ctx, admin(), "generic-remote", true); err != nil {
		t.Fatalf("DeleteRepo(deleteContent): %v", err)
	}
	if _, err := e.svc.GetRepo(ctx, admin(), "generic-remote"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("GetRepo after delete = %v, want ErrRepoNotFound", err)
	}
	if _, err := e.md.Remote().GetConfig(ctx, "generic-remote"); !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("remote_configs row survived: %v", err)
	}
	if _, err := e.md.Remote().GetCache(ctx, "generic-remote", "dir/up.bin"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("remote_cache row survived: %v", err)
	}
	if rows, err := e.md.Nodes().ListByPrefix(ctx, "generic-remote", ""); err != nil || len(rows) != 0 {
		t.Fatalf("nodes survived: %v %+v", err, rows)
	}
	// The content read answers the repo-not-found branch (the AC's 404).
	if _, _, err := e.svc.Get(ctx, admin(), "generic-remote", "dir/up.bin"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("Get after repo delete = %v, want ErrRepoNotFound", err)
	}
}

// TestRemoteDeletePurgesCacheWithoutContent: cache-validator rows are
// DERIVED state — an otherwise-empty remote repository deletes without the
// flag, and its cache rows go with it (negative-cache rows have no nodes).
func TestRemoteDeletePurgesCacheWithoutContent(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://127.0.0.1:9099"}`)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := e.md.Remote().PutCache(ctx, &metadata.RemoteCacheEntry{
		RepoKey: "generic-remote", Path: "miss.bin", FetchedAt: now, ExpiresAt: now,
		Kind: metadata.RemoteCacheKindContent,
	}); err != nil {
		t.Fatalf("seed negative-cache row: %v", err)
	}

	if err := e.svc.DeleteRepo(ctx, admin(), "generic-remote", false); err != nil {
		t.Fatalf("DeleteRepo(plain): %v", err)
	}
	if _, err := e.md.Remote().GetCache(ctx, "generic-remote", "miss.bin"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("cache row survived the plain delete: %v", err)
	}
}

// TestVirtualDeleteCascadesMembers: deleting a MEMBER repository cascades
// its virtual_members rows (the FK), so the virtual's next member read is
// already the new truth — no stale membership.
func TestVirtualDeleteCascadesMembers(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://u"}`)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
		Config: `{"repositories":["maven-local","maven-remote-x"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual): %v", err)
	}

	if err := e.svc.DeleteRepo(ctx, admin(), "maven-remote-x", false); err != nil {
		t.Fatalf("DeleteRepo(member): %v", err)
	}
	members, err := e.md.Virtual().ListMembers(ctx, "maven-virtual")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].MemberRepo != "maven-local" {
		t.Fatalf("members after member delete = %+v", members)
	}
}

// ---- remote update path ----

// TestRemoteUpdateReplacesConfig: a provided config fully replaces the
// remote configuration (Artifactory PUT semantics) in both the canonical
// form and the remote_configs row; the exemption flip carries from/to in the
// audit detail (NFR-S14 point 5).
func TestRemoteUpdateReplacesConfig(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://old.example.org","allowPrivateUpstream":false}`)

	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-remote",
		Config:  `{"url":"http://new.example.org/m2","username":"bot","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	row, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.URL != "http://new.example.org/m2" || row.Username != "bot" || !row.AllowPrivateUpstream {
		t.Fatalf("row after update = %+v", row)
	}

	found := false
	for _, ev := range eventsOf(e.au) {
		if ev.Action == repo.AuditActionRepoUpdate && ev.Repo == "generic-remote" &&
			strings.Contains(ev.Detail, `"allowPrivateUpstream":{"from":false,"to":true}`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no repo.update audit event recording the exemption transition")
	}

	// Description-only update: the config row stays as the last full write.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: "generic-remote", Description: "d"}); err != nil {
		t.Fatalf("UpdateRepo(description): %v", err)
	}
	row, err = e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.URL != "http://new.example.org/m2" {
		t.Fatalf("row after description-only update = %+v", row.URL)
	}
}

// TestRemoteUpdateHealsMissingConfigRow: a remote_configs row lost to the
// create crash window is re-created by the next config-carrying update (the
// documented healer).
func TestRemoteUpdateHealsMissingConfigRow(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://u"}`)
	if err := e.md.Remote().DeleteConfig(ctx, "generic-remote"); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}

	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-remote", Config: `{"url":"http://healed"}`,
	}); err != nil {
		t.Fatalf("UpdateRepo(heal): %v", err)
	}
	row, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig after heal: %v", err)
	}
	if row.URL != "http://healed" {
		t.Fatalf("healed row url = %q", row.URL)
	}
}

// TestRemoteUpdateRefusesBadConfig: the update path validates exactly like
// create.
func TestRemoteUpdateRefusesBadConfig(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "generic-remote", `{"url":"http://u"}`)
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-remote", Config: `{"url":"gopher://x"}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("UpdateRepo(bad scheme) error = %v", err)
	}
	// The refusal changed nothing.
	row, err := e.md.Remote().GetConfig(ctx, "generic-remote")
	if err != nil || row.URL != "http://u" {
		t.Fatalf("row after refused update = %+v err=%v", row, err)
	}
}

// ---- concurrency smoke: parallel remote/virtual creates stay independent ----

// TestParallelTypedRepoCreation: the batch-3 protocol tickets will seed
// remote and virtual fixtures concurrently; the create path (two table
// writes each) must stay race-clean under that load.
func TestParallelTypedRepoCreation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("remote-%d", i)
			if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageMaven,
				Config: fmt.Sprintf(`{"url":"http://127.0.0.1:9099/m%d"}`, i),
			}); err != nil {
				t.Errorf("CreateRepo(%s): %v", key, err)
			}
		}(i)
	}
	wg.Wait()
}
