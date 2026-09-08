package webhook_test

// T-362's closed-set, criteria, signature and request-shape legs: the
// closed-domain registry's shape (4 sourced domains, 12 wired types —
// M17-Q5 裁①), strict criteria parsing, the Ant matcher's two-level
// wildcards, the openssl-compatible HMAC vector, and the subscription
// request validation table (webhook.md section 2's OpenAPI constraints,
// one 400 per row).

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/webhook"
)

func TestT362EventClosedSet(t *testing.T) {
	// The closed-DOMAIN set (M17-Q5 裁①, ADR-0041 decision 7 errata): the
	// four domains with BinFlow trigger sources. The M13 interim posture
	// (13 domains, dormant labeling) was replaced by the ruling — the
	// sourceless nine are deregistered, asserted below and in
	// closed_domain_registry_test.go.
	if got := len(webhook.Domains()); got != 4 {
		t.Fatalf("domain count = %d, want 4", got)
	}
	// The registry's total is asserted through one domain's legal set plus
	// the pair lookups below; the wired twelve are the contract AC-2 pins
	// (M13's nine + the build domain's three, T-510).
	wired := map[[2]string]bool{}
	for _, d := range webhook.Domains() {
		for _, et := range webhook.EventTypesOfDomain(d) {
			def, ok := webhook.Lookup(d, et)
			if !ok {
				t.Fatalf("registered pair %s/%s fails Lookup", d, et)
			}
			if def.Source == webhook.SourceWired {
				wired[[2]string{d, et}] = true
			}
		}
	}
	if len(wired) != 12 {
		t.Fatalf("wired pair count = %d, want 12: %v", len(wired), wired)
	}
	for _, pair := range [][2]string{
		{"artifact", "deployed"}, {"artifact", "deleted"}, {"artifact", "moved"},
		{"artifact", "copied"}, {"artifact", "cached"},
		{"artifact_property", "added"}, {"artifact_property", "deleted"},
		{"docker", "pushed"}, {"docker", "deleted"},
		{"build", "uploaded"}, {"build", "deleted"}, {"build", "promoted"},
	} {
		if !wired[pair] {
			t.Errorf("pair %v must be wired", pair)
		}
	}
	// Domain-colliding names resolve through the PAIR, never the name.
	if _, ok := webhook.Lookup("docker", "deleted"); !ok {
		t.Error("docker/deleted must resolve")
	}
	if _, ok := webhook.Lookup("artifact", "pushed"); ok {
		t.Error("artifact/pushed must NOT resolve (pushed is docker's)")
	}
	// The one dormant survivor: docker/promoted stays subscribable-but-
	// silent inside a sourced domain (the promotion REST has no body).
	if _, ok := webhook.Lookup("docker", "promoted"); !ok {
		t.Error("docker/promoted must stay registered (dormant)")
	}
	if webhook.Wired("docker", "promoted") {
		t.Error("docker/promoted must be dormant")
	}
	// The deregistered nine: not domains, not pairs — unknown on every
	// validation face.
	for _, d := range webhook.DeregisteredDomains() {
		if webhook.ValidDomain(d) {
			t.Errorf("deregistered domain %q must not validate", d)
		}
	}
	for _, pair := range [][2]string{
		{"release_bundle", "created"},
		{"distribution", "delete_failed"}, {"destination", "delete_failed"},
		{"user", "locked"}, {"xray_scan_status", "not_supported"},
		{"app_trust", "release_started"},
		{"curation", "Package was blocked by Curation"},
	} {
		if _, ok := webhook.Lookup(pair[0], pair[1]); ok {
			t.Errorf("deregistered pair %v must not resolve", pair)
		}
		if webhook.Wired(pair[0], pair[1]) {
			t.Errorf("deregistered pair %v must never report wired", pair)
		}
	}
}

func TestT362RegistryTotal(t *testing.T) {
	// The closed-domain registry: 4 domains x their counts = 13 types
	// (M17-Q5 裁①; the nine sourceless domains left the set).
	want := map[string]int{
		"artifact": 5, "artifact_property": 2, "docker": 3, "build": 3,
	}
	total := 0
	for d, n := range want {
		if got := len(webhook.EventTypesOfDomain(d)); got != n {
			t.Errorf("domain %s carries %d types, want %d", d, got, n)
		}
		total += n
	}
	if total != 13 {
		t.Fatalf("total event types = %d, want 13", total)
	}
	if got := len(webhook.Domains()); got != len(want) {
		t.Fatalf("domain count = %d, want %d", got, len(want))
	}
}

func TestT362ParseCriteriaStrict(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{"empty object", `{}`, ""},
		{"full known set", `{"anyLocal":true,"anyRemote":false,"repoKeys":["r"],"includePatterns":["a/**"],"excludePatterns":[],"selectedBuilds":[],"registeredReleaseBundlesNames":[],"selectedEnvironments":[],"applicationKeys":[],"stages":[],"selectedReleaseBundles":{"b":["p*"]}}`, ""},
		{"unknown key", `{"repoKeys":["r"],"typo":1}`, "unknown key"},
		{"bool mistype", `{"anyLocal":"yes"}`, "must be a boolean"},
		{"array mistype", `{"repoKeys":"r"}`, "must be a string array"},
		{"map mistype", `{"selectedReleaseBundles":["x"]}`, "object of string arrays"},
		{"not an object", `[]`, "not a JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := webhook.ParseCriteria(json.RawMessage(tc.raw))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseCriteria(%s) = %v, want ok", tc.raw, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ParseCriteria(%s) = %v, want error containing %q", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestT362AntMatcher(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**", "a/b/c.bin", true},
		{"**/*", "a/b/c.bin", true},
		{"**/c.bin", "a/b/c.bin", true},
		{"a/**", "a/b/c.bin", true},
		{"a/*", "a/b/c.bin", false},  // '*' spans one segment
		{"a/*.bin", "a/c.bin", true}, // in-segment wildcard
		{"a/*.bin", "a/b/c.bin", false},
		{"*/c.bin", "b/c.bin", true},
		// Ant '**' spans ZERO or more segments: the directory itself matches.
		{"com/acme/**", "com/acme", true},
		{"com/acme/**", "com/acme/x/y", true},
		{"exact.bin", "exact.bin", true},
		{"exact.bin", "other.bin", false},
	}
	for _, tc := range cases {
		if got := antMatchExported(t, tc.pattern, tc.path); got != tc.want {
			t.Errorf("antMatch(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

// antMatchExported drives the matcher through the include filter (the
// exported surface the bus itself uses).
func antMatchExported(t *testing.T, pattern, path string) bool {
	t.Helper()
	f, err := webhook.ParseCriteria(json.RawMessage(
		`{"includePatterns":["` + pattern + `"]}`))
	if err != nil {
		t.Fatalf("parse include filter: %v", err)
	}
	return f.MatchesPath(path)
}

func TestT362MatchesRepo(t *testing.T) {
	cases := []struct {
		name       string
		criteria   string
		repo, kind string
		want       bool
	}{
		{"exact repoKeys hit", `{"repoKeys":["maven-local"]}`, "maven-local", "local", true},
		{"exact repoKeys miss", `{"repoKeys":["maven-local"]}`, "npm-local", "local", false},
		{"anyLocal", `{"anyLocal":true}`, "npm-local", "local", true},
		{"anyLocal vs remote", `{"anyLocal":true}`, "npm-remote", "remote", false},
		{"anyRemote", `{"anyRemote":true}`, "npm-remote", "remote", true},
		{"anyFederated never", `{"anyFederated":true}`, "npm-local", "local", false},
		{"nothing selected admits nothing", `{}`, "npm-local", "local", false},
		{"union", `{"anyRemote":true,"repoKeys":["maven-local"]}`, "maven-local", "local", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := webhook.ParseCriteria(json.RawMessage(tc.criteria))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := f.MatchesRepo(tc.repo, tc.kind); got != tc.want {
				t.Errorf("matchesRepo(%q, %q) = %v, want %v", tc.repo, tc.kind, got, tc.want)
			}
		})
	}
}

func TestT362SignatureVector(t *testing.T) {
	// The well-known HMAC-SHA256 of "The quick brown fox jumps over the
	// lazy dog" under "key" — the openssl -hmac output form the official
	// verification command produces (webhook.md section 6).
	got := webhook.Signature("key", []byte("The quick brown fox jumps over the lazy dog"))
	want := "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	if got != want {
		t.Fatalf("Signature = %s, want %s", got, want)
	}
	if webhook.EventAuthHeader != "X-JFrog-Event-Auth" {
		t.Fatalf("auth header = %q", webhook.EventAuthHeader)
	}
}

func TestT362RequestValidation(t *testing.T) {
	good := `{
		"key": "ci-deploys",
		"description": "CI deploy notifications",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
	}`
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"good", good, ""},
		{"empty body", "", "required"},
		{"key missing", strings.Replace(good, `"ci-deploys"`, `""`, 1), "key is required"},
		{"key pattern", strings.Replace(good, `"ci-deploys"`, `"1ci"`, 1), "key must match"},
		{"key dot", strings.Replace(good, `"ci-deploys"`, `"ci.deploys"`, 1), "key must match"},
		{"unknown domain", strings.Replace(good, `"artifact"`, `"artifacts"`, 1), "unknown event domain"},
		{"empty event_types", strings.Replace(good, `["deployed"]`, `[]`, 1), "at least one"},
		{"type of wrong domain", strings.Replace(good, `"deployed"`, `"pushed"`, 1), "not registered for domain"},
		{"unknown criteria key", strings.Replace(good, `"anyLocal": true`, `"anyLocal": true, "x": 1`, 1), "unknown key"},
		{"two handlers", strings.Replace(good, `"url": "https://ci.example.com/hook"}]`, `"url": "https://ci.example.com/hook"},{"handler_type":"webhook","url":"https://x.example.com"}]`, 1), "exactly one"},
		{"no handlers", `{"key":"kk","enabled":true,"event_filter":{"domain":"artifact","event_types":["deployed"]}}`, "exactly one"},
		{"bad handler type", strings.Replace(good, `"webhook"`, `"slack"`, 1), `handler_type`},
		{"missing url", strings.Replace(good, `"url": "https://ci.example.com/hook"`, `"proxy": ""`, 1), "url is required"},
		{"file scheme", strings.Replace(good, `https://ci.example.com/hook`, `file:///etc/passwd`, 1), "scheme"},
		{"userinfo", strings.Replace(good, `https://ci.example.com/hook`, `https://u:p@ci.example.com/hook`, 1), "userinfo"},
		{"fragment", strings.Replace(good, `https://ci.example.com/hook`, `https://ci.example.com/hook#f`, 1), "fragment"},
		{"custom bad method", strings.Replace(good, `"handler_type": "webhook", "url": "https://ci.example.com/hook"`, `"handler_type": "custom-webhook", "url": "https://ci.example.com/hook", "method": "TRACE"`, 1), "method"},
		{"custom bad secret name", strings.Replace(good, `"handler_type": "webhook", "url": "https://ci.example.com/hook"`, `"handler_type": "custom-webhook", "url": "https://ci.example.com/hook", "secrets": [{"name": "1bad", "value": "x"}]`, 1), "secret name"},
		{"secret without cipher", strings.Replace(good, `"url": "https://ci.example.com/hook"`, `"url": "https://ci.example.com/hook", "secret": "s3cr3t"`, 1), "master key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := webhook.ParseSubscriptionRequest([]byte(tc.body), nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("parse = %v, want ok", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parse(%s) = %v, want error containing %q", tc.name, err, tc.wantErr)
			}
		})
	}
}
