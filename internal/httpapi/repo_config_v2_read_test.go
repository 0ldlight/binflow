package httpapi_test

// L025-3A (D02 read family, rest-api.md sections 2.1.3/2.1.4): the v2
// single-repository configuration read (type-for-rclass dialect, vendor
// content types, the Content-Type negotiation quirk, the non-admin
// five-key partial view) and the v2 batch read.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const (
	v2LocalCT   = "application/vnd.org.jfrog.artifactory.repositories.LocalRepositoryConfiguration+json"
	v2RemoteCT  = "application/vnd.org.jfrog.artifactory.repositories.RemoteRepositoryConfiguration+json"
	v2VirtualCT = "application/vnd.org.jfrog.artifactory.repositories.VirtualRepositoryConfiguration+json"
)

// TestRepoConfigV2ReadDialects: the v2 body carries type (never rclass),
// the vendor content type follows the row class, Cache-Control is no-store,
// and the remote/virtual arms project their class fields off the canonical
// config.
func TestRepoConfigV2ReadDialects(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "lib", `{"rclass":"local","packageType":"generic","description":"the lib","blackedOut":true,"repoLayoutRef":"simple-default","environments":["DEV"]}`)
	seedConfigFamilyRepo(t, h, "rem", `{"rclass":"remote","packageType":"generic","url":"https://example.com/up","hardFail":true}`)
	seedConfigFamilyRepo(t, h, "virt", `{"rclass":"virtual","packageType":"generic","repositories":["lib"]}`)

	tests := []struct {
		key    string
		wantCT string
		check  func(t *testing.T, m map[string]any)
	}{
		{"lib", v2LocalCT, func(t *testing.T, m map[string]any) {
			if m["type"] != "local" || m["key"] != "lib" || m["packageType"] != "generic" || m["description"] != "the lib" {
				t.Errorf("common fields = %v", m)
			}
			if m["blackedOut"] != true || m["repoLayoutRef"] != "simple-default" {
				t.Errorf("blob fields = %v", m)
			}
			if _, has := m["rclass"]; has {
				t.Errorf("v2 must rename rclass to type: %v", m)
			}
			if _, has := m["url"]; has {
				t.Errorf("v2 local carries no url: %v", m)
			}
		}},
		{"rem", v2RemoteCT, func(t *testing.T, m map[string]any) {
			if m["type"] != "remote" || m["url"] != "https://example.com/up" {
				t.Errorf("remote fields = %v", m)
			}
			if m["hardFail"] != true {
				t.Errorf("remote knobs = %v", m)
			}
		}},
		{"virt", v2VirtualCT, func(t *testing.T, m map[string]any) {
			if m["type"] != "virtual" || fmt.Sprint(m["repositories"]) != "[lib]" {
				t.Errorf("virtual fields = %v", m)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/"+tt.key, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d body=%s", resp.StatusCode, body)
			}
			if ct := resp.Header.Get("Content-Type"); ct != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", ct, tt.wantCT)
			}
			if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", cc)
			}
			m := decodeJSONMap(t, body)
			tt.check(t, m)
		})
	}
}

// TestRepoConfigV2NotFound: the unknown-key arm answers the spec's verbatim
// 404 envelope wording.
func TestRepoConfigV2NotFound(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/nope", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	m := decodeJSONMap(t, body)
	errs, _ := m["errors"].([]any)
	entry, _ := errs[0].(map[string]any)
	if entry["message"] != "The repository nope was not found" {
		t.Errorf("message = %v", entry["message"])
	}
	if entry["status"] != float64(404) {
		t.Errorf("status = %v", entry["status"])
	}
}

// TestRepoConfigV2ContentTypeNegotiation: the negotiation input is the
// request's Content-Type, not Accept — a mismatched vendor Content-Type
// answers 406 "Not Acceptable" while a mismatched Accept header passes.
func TestRepoConfigV2ContentTypeNegotiation(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "lib", `{"rclass":"local","packageType":"generic"}`)

	resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/lib", adminUser, adminPass, nil,
		map[string]string{"Content-Type": strings.ToLower(v2RemoteCT)})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotAcceptable {
		t.Fatalf("mismatched Content-Type status %d body=%s", resp.StatusCode, body)
	}
	m := decodeJSONMap(t, body)
	errs, _ := m["errors"].([]any)
	entry, _ := errs[0].(map[string]any)
	if entry["message"] != "Not Acceptable" {
		t.Errorf("message = %v", entry["message"])
	}

	// The matching vendor Content-Type still serves; any Accept passes.
	resp = h.do(http.MethodGet, "/binflow/api/v2/repositories/lib", adminUser, adminPass, nil,
		map[string]string{"Content-Type": strings.ToLower(v2LocalCT)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("matching Content-Type status %d", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/api/v2/repositories/lib", adminUser, adminPass, nil,
		map[string]string{"Accept": strings.ToLower(v2RemoteCT)})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mismatched Accept must not negotiate: status %d", resp.StatusCode)
	}
}

// TestRepoConfigV2NonAdminPartial: a non-admin caller gets the five-key
// partial projection {key,type,packageType,description,url} — measured on
// a remote row in the spec; BinFlow serves the same set for every class
// (the local/virtual projection is the spec-pending arm).
func TestRepoConfigV2NonAdminPartial(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedConfigFamilyRepo(t, h, "rem", `{"rclass":"remote","packageType":"generic","url":"https://example.com/up","hardFail":true}`)

	resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/rem", "u1", "p1", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != v2RemoteCT {
		t.Errorf("Content-Type = %q", ct)
	}
	m := decodeJSONMap(t, body)
	want := map[string]any{
		"key": "rem", "type": "remote", "packageType": "generic",
		"description": "", "url": "https://example.com/up",
	}
	if compactJSON(t, body) != compactJSON(t, mustJSON(t, want)) {
		t.Errorf("partial view = %s, want the five-key projection %v", body, want)
	}
	if _, has := m["hardFail"]; has {
		t.Errorf("partial view must strip class fields: %s", body)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// TestRepoBatchRead: the batch map keys onto the v1 single-get schema
// (deep-equal with GET /api/repositories/{key}), unknown names are
// silently omitted, a comma inside one value is a single key, and the
// missing-names / over-limit arms answer their verbatim 400s.
func TestRepoBatchRead(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "lib", `{"rclass":"local","packageType":"generic","description":"the lib"}`)
	seedConfigFamilyRepo(t, h, "rem", `{"rclass":"remote","packageType":"generic","url":"https://example.com/up"}`)

	t.Run("repeated names map with v1 schema", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/batch?names=lib&names=rem&names=ghost", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		m := decodeJSONMap(t, body)
		if len(m) != 2 {
			t.Fatalf("ghost must be omitted: %s", body)
		}
		for _, key := range []string{"lib", "rem"} {
			single := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
			got, err := json.Marshal(m[key])
			if err != nil {
				t.Fatalf("marshal batch[%s]: %v", key, err)
			}
			if compactJSON(t, string(got)) != compactJSON(t, mustGet(t, single)) {
				t.Errorf("batch[%s] = %s, want the v1 single-get body", key, got)
			}
		}
	})

	t.Run("comma string is one key", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/batch?names=lib,rem", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		if compactJSON(t, body) != "{}" {
			t.Errorf("comma names must fall through as one unknown key: %s", body)
		}
	})

	t.Run("missing names 400 verbatim", func(t *testing.T) {
		for _, q := range []string{"", "?names=", "?names=&names="} {
			resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/batch"+q, adminUser, adminPass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%q status %d body=%s", q, resp.StatusCode, body)
			}
			m := decodeJSONMap(t, body)
			errs, _ := m["errors"].([]any)
			entry, _ := errs[0].(map[string]any)
			if entry["message"] != "Repository keys are missing." {
				t.Errorf("%q message = %v", q, entry["message"])
			}
		}
	})

	t.Run("over limit 400", func(t *testing.T) {
		names := make([]string, 101)
		for i := range names {
			names[i] = "names=r" + fmt.Sprint(i)
		}
		resp := h.do(http.MethodGet, "/binflow/api/v2/repositories/batch?"+strings.Join(names, "&"), adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		m := decodeJSONMap(t, body)
		errs, _ := m["errors"].([]any)
		entry, _ := errs[0].(map[string]any)
		if entry["message"] != "Repository item limit exceeded: 101. Limit: 100" {
			t.Errorf("message = %v", entry["message"])
		}
	})

	t.Run("non-admin authenticated read", func(t *testing.T) {
		h2 := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
		seedConfigFamilyRepo(t, h2, "lib", `{"rclass":"local","packageType":"generic"}`)
		resp := h2.do(http.MethodGet, "/binflow/api/v2/repositories/batch?names=lib", "u1", "p1", nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, mustGet(t, resp))
		}
	})
}
