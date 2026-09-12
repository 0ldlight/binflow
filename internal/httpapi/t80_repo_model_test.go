package httpapi_test

// T-80: the M3 repository REST wiring against the full harness stack — the
// PRD's M01..M05 acceptance commands, one test family per command. The rules
// all live in repo.Service (T-64); this file pins that they actually cross
// the REST plane: field transport on PUT/POST, the configuration echo on
// GET, and the ?type=/?packageType= filters on the list.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// repoModelHarness seeds the M-command fixture: one local generic repository
// (the virtual member and list baseline).
func repoModelHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	return h
}

// putRepoStatus issues the create/update PUT and returns (status, body).
func putRepoStatus(t *testing.T, h *harness, key, body string) (int, string) {
	t.Helper()
	resp := putRepo(t, h, key, body)
	return resp.StatusCode, mustGet(t, resp)
}

// getRepoJSON fetches one repository configuration as raw JSON.
func getRepoJSON(t *testing.T, h *harness, key string) (int, map[string]any) {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("GET %s body %q: %v", key, body, err)
	}
	return resp.StatusCode, m
}

// ---- M01: the three protocol package types (FR-15-AC1) ----

func TestM01LocalProtocolReposREST(t *testing.T) {
	for _, pt := range []string{"maven", "npm", "pypi"} {
		t.Run(pt, func(t *testing.T) {
			h := repoModelHarness(t)
			status, body := putRepoStatus(t, h, pt+"-local",
				fmt.Sprintf(`{"rclass":"local","packageType":%q}`, pt))
			if status != http.StatusOK {
				t.Fatalf("create status = %d; body=%s", status, body)
			}
			if strings.TrimSpace(body) != "Successfully created repository '"+pt+"-local'" {
				t.Fatalf("body = %q, want the created wording", body)
			}
			code, cfg := getRepoJSON(t, h, pt+"-local")
			if code != http.StatusOK {
				t.Fatalf("GET status = %d", code)
			}
			if cfg["packageType"] != pt || cfg["rclass"] != "local" {
				t.Fatalf("round trip = %v/%v, want local/%s", cfg["rclass"], cfg["packageType"], pt)
			}
		})
	}
}

// ---- M02/M02b: remote creation, echo and field validation (FR-15-AC2/AC3) ----

func TestM02RemoteCreateAndEchoREST(t *testing.T) {
	h := repoModelHarness(t)

	// M02: create with the URL and the SSRF exemption; the password rides in
	// the body and must never come back (NFR-S14).
	status, body := putRepoStatus(t, h, "generic-remote",
		`{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099",`+
			`"username":"ci","password":"s3cret-upstream","allowPrivateUpstream":true}`)
	if status != http.StatusOK {
		t.Fatalf("M02 create status = %d; body=%s", status, body)
	}

	code, cfg := getRepoJSON(t, h, "generic-remote")
	if code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	if cfg["rclass"] != "remote" {
		t.Fatalf("rclass = %v, want remote", cfg["rclass"])
	}
	conf, ok := cfg["configuration"].(map[string]any)
	if !ok {
		t.Fatalf("configuration missing: %v", cfg)
	}
	if conf["url"] != "http://127.0.0.1:9099" {
		t.Fatalf("configuration.url = %v, want the upstream verbatim", conf["url"])
	}
	for _, field := range []string{"password", "s3cret-upstream"} {
		if raw, _ := json.Marshal(cfg); strings.Contains(string(raw), field) {
			t.Fatalf("GET body leaks %q: %s", field, raw)
		}
	}
	// The defaults ride the echo (PRD v1.2 C4). socketTimeoutMillis is the
	// canonical ms spelling since T-346 (FR-113.1); the legacy
	// socketTimeoutMs never rides the echo.
	for k, v := range map[string]any{
		"retrievalCachePeriodSecs":       float64(7200),
		"missedRetrievalCachePeriodSecs": float64(1800),
		"socketTimeoutMillis":            float64(15000),
		"socketTimeoutSecs":              float64(15),
		"assumedOfflinePeriodSecs":       float64(300),
		"hardFail":                       false,
		"allowPrivateUpstream":           true,
	} {
		if conf[k] != v {
			t.Fatalf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}

	// The list plane carries the same masked configuration.
	resp := h.do(http.MethodGet, "/binflow/api/repositories?type=remote", adminUser, adminPass, nil, nil)
	listBody := mustGet(t, resp)
	if strings.Contains(listBody, "s3cret-upstream") {
		t.Fatalf("list body leaks the credential: %s", listBody)
	}
}

func TestM02bRemoteValidationREST(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantSubstr string
	}{
		{"missing url", `{"rclass":"remote","packageType":"generic"}`, "url"},
		{"file scheme", `{"rclass":"remote","packageType":"generic","url":"file:///etc"}`, "http or https"},
		{"ftp scheme", `{"rclass":"remote","packageType":"generic","url":"ftp://x"}`, "http or https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := repoModelHarness(t)
			status, body := putRepoStatus(t, h, "bad-remote", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, tt.wantSubstr) {
				t.Fatalf("body %q does not name %q", body, tt.wantSubstr)
			}
		})
	}
	// A private-address URL creates fine: create-time validation is
	// scheme/format only (FR-15-AC3, NFR-S13).
	t.Run("private upstream url creates", func(t *testing.T) {
		h := repoModelHarness(t)
		status, body := putRepoStatus(t, h, "lan-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://192.168.1.5:9099"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
	})
}

// ---- M03: virtual creation and member validation (FR-15-AC4) ----

func TestM03VirtualREST(t *testing.T) {
	seedVirtualFixture := func(t *testing.T) *harness {
		t.Helper()
		h := repoModelHarness(t)
		status, body := putRepoStatus(t, h, "maven-remote-x",
			`{"rclass":"remote","packageType":"maven","url":"http://127.0.0.1:9099/m2","allowPrivateUpstream":true}`)
		if status != http.StatusOK {
			t.Fatalf("seed maven remote: %d %s", status, body)
		}
		return h
	}

	t.Run("create over local+remote members", func(t *testing.T) {
		h := seedVirtualFixture(t)
		status, body := putRepoStatus(t, h, "maven-virtual",
			`{"rclass":"virtual","packageType":"maven","repositories":["generic-local","maven-remote-x"],`+
				`"defaultDeploymentRepo":"generic-local"}`)
		if status != http.StatusOK {
			t.Fatalf("create status = %d; body=%s", status, body)
		}
		code, cfg := getRepoJSON(t, h, "maven-virtual")
		if code != http.StatusOK || cfg["rclass"] != "virtual" {
			t.Fatalf("GET = %d %v", code, cfg["rclass"])
		}
		conf := cfg["configuration"].(map[string]any)
		members, _ := conf["repositories"].([]any)
		if len(members) != 2 || members[0] != "generic-local" || members[1] != "maven-remote-x" {
			t.Fatalf("configuration.repositories = %v", conf["repositories"])
		}
		if conf["defaultDeploymentRepo"] != "generic-local" {
			t.Fatalf("configuration.defaultDeploymentRepo = %v", conf["defaultDeploymentRepo"])
		}
	})

	refusals := []struct {
		name string
		body string
		want string
	}{
		{"missing repositories", `{"rclass":"virtual","packageType":"maven"}`, "repositories is required"},
		{"empty member array", `{"rclass":"virtual","packageType":"maven","repositories":[]}`, "repositories is required"},
		{"unknown member", `{"rclass":"virtual","packageType":"maven","repositories":["no-such-repo"]}`, "does not exist"},
		{"nested virtual", `{"rclass":"virtual","packageType":"maven","repositories":["generic-local","outer"]}`, "nested virtual"},
		{"default targets remote member", `{"rclass":"virtual","packageType":"maven","repositories":["generic-local","maven-remote-x"],"defaultDeploymentRepo":"maven-remote-x"}`, "must be a local repository member"},
	}
	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			h := seedVirtualFixture(t)
			// The nesting target must exist for the nested-virtual case.
			if s, b := putRepoStatus(t, h, "outer", `{"rclass":"virtual","packageType":"maven","repositories":["generic-local"]}`); s != http.StatusOK {
				t.Fatalf("seed outer virtual: %d %s", s, b)
			}
			status, body := putRepoStatus(t, h, "bad-virtual", tt.body)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d; body=%s", status, body)
			}
			if !strings.Contains(body, tt.want) {
				t.Fatalf("body %q does not contain %q", body, tt.want)
			}
		})
	}
}

// ---- M04: the list filters (FR-15-AC5) ----

func TestM04ListFiltersREST(t *testing.T) {
	h := repoModelHarness(t) // generic-local (local/generic)
	for key, body := range map[string]string{
		"maven-local":    `{"rclass":"local","packageType":"maven"}`,
		"generic-remote": `{"rclass":"remote","packageType":"generic","url":"http://127.0.0.1:9099"}`,
		"maven-remote":   `{"rclass":"remote","packageType":"maven","url":"http://127.0.0.1:9099/m2"}`,
	} {
		if s, b := putRepoStatus(t, h, key, body); s != http.StatusOK {
			t.Fatalf("seed %s: %d %s", key, s, b)
		}
	}
	// maven-virtual makes the ?packageType=maven page three-wide.
	if s, b := putRepoStatus(t, h, "maven-virtual",
		`{"rclass":"virtual","packageType":"maven","repositories":["maven-local","maven-remote"]}`); s != http.StatusOK {
		t.Fatalf("seed maven-virtual: %d %s", s, b)
	}

	keys := func(t *testing.T, query string) []string {
		t.Helper()
		resp := h.do(http.MethodGet, "/binflow/api/repositories"+query, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list%s status = %d; body=%s", query, resp.StatusCode, body)
		}
		var items []struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(body), &items); err != nil {
			t.Fatalf("list body %q: %v", body, err)
		}
		out := make([]string, len(items))
		for i, it := range items {
			out[i] = it.Key
		}
		return out
	}

	tests := []struct {
		query string
		want  string
	}{
		{"?type=remote", "[generic-remote maven-remote]"},
		{"?type=virtual", "[maven-virtual]"},
		{"?type=local", "[generic-local maven-local]"},
		{"?packageType=maven", "[maven-local maven-remote maven-virtual]"},
		{"?type=remote&packageType=maven", "[maven-remote]"},
		{"?type=federated", "[]"},
		{"?packageType=conda", "[]"},
		{"", "[generic-local maven-local generic-remote maven-remote maven-virtual]"},
	}
	for _, tt := range tests {
		t.Run("list "+tt.query, func(t *testing.T) {
			if got := fmt.Sprint(keys(t, tt.query)); got != tt.want {
				t.Fatalf("keys = %s, want %s", got, tt.want)
			}
		})
	}
}

// ---- M05: the docker combination boundary (FR-15-AC7) ----

func TestM05DockerBoundaryREST(t *testing.T) {
	t.Run("remote+docker creates (FR-129, T-392)", func(t *testing.T) {
		h := repoModelHarness(t)
		status, body := putRepoStatus(t, h, "docker-remote",
			`{"rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io/v2"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
		if !strings.Contains(body, "Successfully created repository 'docker-remote'") {
			t.Fatalf("body = %q, want the created wording", body)
		}
		if cfg, err := h.md.Remote().GetConfig(context.Background(), "docker-remote"); err != nil || cfg.URL != "https://registry-1.docker.io/v2" {
			t.Fatalf("remote config = (%v, %+v), want the typed url row", err, cfg)
		}
	})
	t.Run("virtual+docker creates (T-431, M15 Q6)", func(t *testing.T) {
		h := repoModelHarness(t)
		if s, b := putRepoStatus(t, h, "docker-local", `{"rclass":"local","packageType":"docker"}`); s != http.StatusOK {
			t.Fatalf("seed docker local: %d %s", s, b)
		}
		status, body := putRepoStatus(t, h, "docker-virtual",
			`{"rclass":"virtual","packageType":"docker","repositories":["docker-local"]}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
		if !strings.Contains(body, "Successfully created repository 'docker-virtual'") {
			t.Fatalf("body = %q, want the created wording", body)
		}
		if members, err := h.md.Virtual().ListMembers(context.Background(), "docker-virtual"); err != nil || len(members) != 1 || members[0].MemberRepo != "docker-local" {
			t.Fatalf("virtual members = (%v, %+v), want the seeded docker-local", err, members)
		}
	})
	t.Run("local+docker does not regress", func(t *testing.T) {
		h := repoModelHarness(t)
		status, body := putRepoStatus(t, h, "docker-local", `{"rclass":"local","packageType":"docker"}`)
		if status != http.StatusOK {
			t.Fatalf("status = %d; body=%s", status, body)
		}
	})
}

// ---- update path since ADR-0050: POST is the update spelling; the remote
// arm MERGES on omit, PUT onto an existing key is the create-only 400 ----

func TestRemoteVirtualUpdateREST(t *testing.T) {
	t.Run("remote POST updates the url, merge keeps the rest", func(t *testing.T) {
		h := repoModelHarness(t)
		if s, b := putRepoStatus(t, h, "generic-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://old.example.org","hardFail":true,"username":"keepme"}`); s != http.StatusOK {
			t.Fatalf("create: %d %s", s, b)
		}
		if s, b := postRepoStatus(t, h, "generic-remote",
			`{"url":"http://new.example.org/m2"}`); s != http.StatusOK {
			t.Fatalf("update: %d %s", s, b)
		}
		_, cfg := getRepoJSON(t, h, "generic-remote")
		conf := cfg["configuration"].(map[string]any)
		if conf["url"] != "http://new.example.org/m2" {
			t.Fatalf("configuration.url after update = %v", conf["url"])
		}
		if conf["hardFail"] != true {
			t.Fatalf("hardFail after url-only update = %v, want the kept true (ADR-0050 merge)", conf["hardFail"])
		}
		if conf["username"] != "keepme" {
			t.Fatalf("username after url-only update = %v, want the kept value", conf["username"])
		}
	})
	t.Run("virtual POST rewrites the member list", func(t *testing.T) {
		h := repoModelHarness(t)
		if s, b := putRepoStatus(t, h, "another-local", `{"rclass":"local","packageType":"generic"}`); s != http.StatusOK {
			t.Fatalf("seed second member: %d %s", s, b)
		}
		if s, b := putRepoStatus(t, h, "aggregated",
			`{"rclass":"virtual","packageType":"generic","repositories":["generic-local"]}`); s != http.StatusOK {
			t.Fatalf("create: %d %s", s, b)
		}
		if s, b := postRepoStatus(t, h, "aggregated",
			`{"repositories":["another-local","generic-local"]}`); s != http.StatusOK {
			t.Fatalf("update: %d %s", s, b)
		}
		_, cfg := getRepoJSON(t, h, "aggregated")
		conf := cfg["configuration"].(map[string]any)
		members, _ := conf["repositories"].([]any)
		if len(members) != 2 || members[0] != "another-local" || members[1] != "generic-local" {
			t.Fatalf("members after update = %v", conf["repositories"])
		}
	})
	t.Run("remote partial update needs no url since ADR-0050", func(t *testing.T) {
		// The pre-ADR-0050 url-required 400 of a partial update is gone:
		// omit = keep. The full matrix lives in the update-merge suites.
		h := repoModelHarness(t)
		if s, b := putRepoStatus(t, h, "generic-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://u"}`); s != http.StatusOK {
			t.Fatalf("create: %d %s", s, b)
		}
		status, body := postRepoStatus(t, h, "generic-remote", `{"username":"bot"}`)
		if status != http.StatusOK {
			t.Fatalf("partial update = %d %q, want 200 (merge-on-omit)", status, body)
		}
		_, cfg := getRepoJSON(t, h, "generic-remote")
		conf := cfg["configuration"].(map[string]any)
		if conf["url"] != "http://u" {
			t.Fatalf("configuration.url after partial update = %v", conf["url"])
		}
		if conf["username"] != "bot" {
			t.Fatalf("configuration.username after partial update = %v", conf["username"])
		}
		// A description-only update keeps the configuration (no
		// type-relevant field in the body).
		status, body = postRepoStatus(t, h, "generic-remote", `{"description":"just words"}`)
		if status != http.StatusOK {
			t.Fatalf("description-only update = %d %s", status, body)
		}
		_, cfg = getRepoJSON(t, h, "generic-remote")
		conf = cfg["configuration"].(map[string]any)
		if conf["url"] != "http://u" {
			t.Fatalf("configuration.url after description-only update = %v", conf["url"])
		}
	})
	t.Run("POST unknown key is 404", func(t *testing.T) {
		h := repoModelHarness(t)
		status, _ := postRepoStatus(t, h, "never-created", `{"description":"x"}`)
		if status != http.StatusNotFound {
			t.Fatalf("POST unknown key = %d, want 404", status)
		}
	})
	t.Run("PUT onto an existing key is the create-only 400", func(t *testing.T) {
		h := repoModelHarness(t)
		if s, b := putRepoStatus(t, h, "generic-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://u"}`); s != http.StatusOK {
			t.Fatalf("create: %d %s", s, b)
		}
		// Complete body: the reference's literal create-conflict refusal,
		// zero side effects (GET below re-checks the stored config).
		status, body := putRepoStatus(t, h, "generic-remote",
			`{"rclass":"remote","packageType":"generic","url":"http://moved.example.org","hardFail":true}`)
		if status != http.StatusBadRequest || !strings.Contains(body,
			"error when validating repository name: generic-remote : Repository key already exists") {
			t.Fatalf("PUT-on-existing = (%d, %q), want the 400 key-exists literal", status, body)
		}
		_, cfg := getRepoJSON(t, h, "generic-remote")
		conf := cfg["configuration"].(map[string]any)
		if conf["url"] != "http://u" || conf["hardFail"] != false {
			t.Fatalf("refused PUT left side effects: %v", conf)
		}
		// No rclass: the type refusal fires FIRST (the reference's probe
		// A16 order) — BinFlow's own type wording, the key-exists question
		// is never reached.
		status, body = putRepoStatus(t, h, "generic-remote", `{"url":"http://moved.example.org"}`)
		if status != http.StatusBadRequest || !strings.Contains(body, "repository type") {
			t.Fatalf("rclass-less PUT-on-existing = (%d, %q), want the type-first 400", status, body)
		}
	})
}
