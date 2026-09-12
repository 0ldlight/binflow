// L006-B (D04-R17/R18): the classic /api/security/permissions family —
// the v1 alias of BinFlow's /api/v1/permissions plane. The wire contract
// pinned here is the live reference's own (:8082, 7.161.20, verified
// 2026-09-12): the {name, uri} skeleton list with URL-escaped names, the
// v1 detail's letter actions and flat patterns, the keyed PUT (path key
// governs, a disagreeing body name answers the reference's 409, a nameless
// body takes the path key), the shared DELETE, and the deliberate absence
// of POST {name} (the reference answers it with a bare 400 — a dead verb).

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// v1ListGet fetches the classic list and decodes the skeleton entries.
func v1ListGet(t *testing.T, h *harness, user, pass string) (int, []map[string]any) {
	t.Helper()
	resp := t215As(t, h, http.MethodGet, "api/security/permissions", user, pass, "")
	raw := readAllT444(t, resp)
	var entries []map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			t.Fatalf("decode classic list %q: %v", raw, err)
		}
	}
	return resp.StatusCode, entries
}

// TestPermissionsV1AliasList: the skeleton list shape and the read gate.
func TestPermissionsV1AliasList(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"dev", "dev"}})
	seedRepo(t, h, "generic-local")
	putTargetWire(t, h, "l006b-alias",
		`{"users":{"admin":["read"]}}`, http.StatusCreated)

	code, entries := v1ListGet(t, h, adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("GET /api/security/permissions = %d, want 200", code)
	}
	var found bool
	for _, e := range entries {
		name, _ := e["name"].(string)
		uri, _ := e["uri"].(string)
		if len(e) != 2 {
			t.Errorf("list entry %v is not the {name, uri} skeleton", e)
		}
		if name == "l006b-alias" {
			found = true
			if !strings.HasSuffix(uri, "/binflow/api/security/permissions/l006b-alias") {
				t.Errorf("uri = %q, want the classic self-reference suffix", uri)
			}
		}
	}
	if !found {
		t.Fatalf("list %v lacks the created target", entries)
	}

	// A space-named target escapes in the uri and round-trips through the
	// detail (the JAX-RS path-decode parity withNameUnescape carries).
	putTargetWire(t, h, "l006b two words", `{"users":{"admin":["read"]}}`, http.StatusCreated)
	_, entries = v1ListGet(t, h, adminUser, adminPass)
	for _, e := range entries {
		if e["name"] == "l006b two words" {
			if !strings.HasSuffix(e["uri"].(string), "/l006b%20two%20words") {
				t.Errorf("space-name uri = %v, want percent-escaped", e["uri"])
			}
		}
	}

	// The read gate: anonymous 401, a plain user 403 (security:read).
	if code, _ := v1ListGet(t, h, "", ""); code != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d, want 401", code)
	}
	if code, _ := v1ListGet(t, h, "dev", "dev"); code != http.StatusForbidden {
		t.Errorf("plain-user list = %d, want 403", code)
	}
}

// TestPermissionsV1AliasDetail: the v1 detail shape — flat patterns with
// the ** default, letter actions, groups/users split — and the 404.
func TestPermissionsV1AliasDetail(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putTargetWire(t, h, "l006b-det", `{"users":{"admin":["read","deploy-cache","annotate","delete","manage"]}}`, http.StatusCreated)

	resp := t215As(t, h, http.MethodGet, "api/security/permissions/l006b-det", adminUser, adminPass, "")
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail = %d body=%s, want 200", resp.StatusCode, raw)
	}
	var d struct {
		Name            string                         `json:"name"`
		IncludesPattern string                         `json:"includesPattern"`
		ExcludesPattern string                         `json:"excludesPattern"`
		Repositories    []string                       `json:"repositories"`
		Principals      map[string]map[string][]string `json:"principals"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode detail %q: %v", raw, err)
	}
	// putTargetWire stores the include pattern sec/** — the flat rendering
	// carries it verbatim; the five-word grant renders as the letter set.
	if d.Name != "l006b-det" || d.IncludesPattern != "sec/**" || d.ExcludesPattern != "" {
		t.Errorf("detail header = %q/%q/%q, want l006b-det/sec/**/\"\"", d.Name, d.IncludesPattern, d.ExcludesPattern)
	}
	letters := d.Principals["users"]["admin"]
	if strings.Join(letters, "") != "rwndm" {
		t.Errorf("admin letters = %v, want [r w n d m] (full %v)", letters, d.Principals)
	}

	// Unknown name: 404, plain.
	resp = t215As(t, h, http.MethodGet, "api/security/permissions/nope", adminUser, adminPass, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown detail = %d, want 404", resp.StatusCode)
	}
	readAllT444(t, resp)

	// The space-named target through the unescaped seam.
	putTargetWire(t, h, "l006b two words", `{"users":{"admin":["read"]}}`, http.StatusCreated)
	resp = t215As(t, h, http.MethodGet, "api/security/permissions/l006b%20two%20words", adminUser, adminPass, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("escaped-name detail = %d, want 200", resp.StatusCode)
	}
	readAllT444(t, resp)
}

// TestPermissionsV1AliasKeyedPut: the path-keyed create-or-replace — 201 on
// both arms, the 409 name-mismatch rule, the nameless-body injection.
func TestPermissionsV1AliasKeyedPut(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"dev", "dev"}})
	seedRepo(t, h, "generic-local")

	body := `{"name":"l006b-keyed","repos":["generic-local"],"principals":{"users":{"dev":["read"]}}}`
	resp := t215As(t, h, http.MethodPut, "api/security/permissions/l006b-keyed", adminUser, adminPass, body)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("keyed create = %d body=%s, want 201", code, raw)
	}

	// Replace arm: same 201 (the reference's PUT answers 201 on replace).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-keyed", adminUser, adminPass, body)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("keyed replace = %d body=%s, want 201", code, raw)
	}

	// Name mismatch: the reference's 409 wording.
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-keyed", adminUser, adminPass,
		`{"name":"other","repos":["generic-local"],"principals":{}}`)
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mismatch = %d body=%s, want 409", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "does not match the permission name") {
		t.Errorf("mismatch body %q lacks the reference wording", raw)
	}

	// Nameless body: the path key governs.
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-anon-name", adminUser, adminPass,
		`{"repos":["generic-local"],"principals":{"users":{"dev":["read"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("nameless keyed create = %d body=%s, want 201", code, raw)
	}
	detail := t215As(t, h, http.MethodGet, "api/security/permissions/l006b-anon-name", adminUser, adminPass, "")
	if raw, code := readAllT444(t, detail), detail.StatusCode; code != http.StatusOK || !strings.Contains(raw, "l006b-anon-name") {
		t.Fatalf("nameless keyed detail = %d body=%s, want 200 naming the path key", code, raw)
	}

	// POST {name} answers the reference's own bare 400 (Review B rework:
	// the addon layer's updateSecurityEntity serves only users/groups —
	// the same answer, mounted, is the wire alignment).
	resp = t215As(t, h, http.MethodPost, "api/security/permissions/l006b-keyed", adminUser, adminPass, body)
	raw = readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "Bad Request") {
		t.Fatalf("POST {name} = %d body=%s, want 400 Bad Request (the reference's own answer)", resp.StatusCode, raw)
	}
}

// TestPermissionsV1AliasDelete: the keyed delete under the classic path —
// L007-1 arm 1 closed the recorded rendering divergence: the reference's
// 200 + plain-text confirmation, verbatim.
func TestPermissionsV1AliasDelete(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putTargetWire(t, h, "l006b-del", `{"users":{"admin":["read"]}}`, http.StatusCreated)

	resp := t215As(t, h, http.MethodDelete, "api/security/permissions/l006b-del", adminUser, adminPass, "")
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("classic delete = %d body=%s, want 200", resp.StatusCode, raw)
	}
	if raw != "Successfully deleted permission Target 'l006b-del'" {
		t.Errorf("classic delete body = %q, want the reference's confirmation text", raw)
	}
	resp = t215As(t, h, http.MethodGet, "api/security/permissions/l006b-del", adminUser, adminPass, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted detail = %d, want 404", resp.StatusCode)
	}
	readAllT444(t, resp)
}

// TestPermissionsV1AliasDialectPut (Review A rework, B-①): the classic face
// accepts the reference's OWN v1 dialect — the repositories key, the flat
// comma-separated pattern strings, and the backend LETTER actions
// (PermissionTargetConfigurationImpl + RestSecurityRequestHandler:527-570,
// decompiled). Letters map onto the grant columns; mxm/x and unknown tokens
// silently contribute nothing (AceImpl#setPermissionsFromStrings' own arm);
// the alias-disagreement 400s mirror the house dual-spelling posture.
func TestPermissionsV1AliasDialectPut(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"dev", "dev"}})
	seedRepo(t, h, "generic-local")

	// The full v1 dialect body: repositories key, flat patterns, letters.
	resp := t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1dialect", adminUser, adminPass,
		`{"repositories":["generic-local"],"includesPattern":"sec/**,dist/**","excludesPattern":"tmp/**",`+
			`"principals":{"users":{"dev":["r","w","n","d","m","mxm","x","zzz"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("v1-dialect create = %d body=%s, want 201", code, raw)
	}
	// The detail renders the mapped grant back as the exact letter set —
	// mxm/x/zzz contributed nothing (the reference's silent-clear arm).
	raw := t215Admin(t, h, http.MethodGet, "api/security/permissions/l006b-v1dialect", "", 200)
	var d struct {
		IncludesPattern string                         `json:"includesPattern"`
		ExcludesPattern string                         `json:"excludesPattern"`
		Repositories    []string                       `json:"repositories"`
		Principals      map[string]map[string][]string `json:"principals"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("decode detail %q: %v", raw, err)
	}
	if d.IncludesPattern != "sec/**,dist/**" || d.ExcludesPattern != "tmp/**" {
		t.Errorf("flat patterns = %q/%q, want sec/**,dist/** / tmp/**", d.IncludesPattern, d.ExcludesPattern)
	}
	if len(d.Repositories) != 1 || d.Repositories[0] != "generic-local" {
		t.Errorf("repositories = %v, want [generic-local]", d.Repositories)
	}
	if letters := strings.Join(d.Principals["users"]["dev"], ""); letters != "rwndm" {
		t.Errorf("dev letters = %v, want rwndm (mxm/x/zzz silently cleared)", d.Principals["users"]["dev"])
	}

	// The dual-spelling disagreement arms (the house alias posture).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1dialect", adminUser, adminPass,
		`{"repos":["generic-local"],"repositories":["other-local"],"principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "disagree") {
		t.Fatalf("repos/repositories disagreement = %d body=%s, want 400 naming the disagreement", code, raw)
	}
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1dialect", adminUser, adminPass,
		`{"includePatterns":["a/**"],"includesPattern":"b/**","principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "disagree") {
		t.Fatalf("pattern disagreement = %d body=%s, want 400 naming the disagreement", code, raw)
	}

	// The reference handler's dual repos 400s (decompiled lines 545-550):
	// neither spelling present → "missing repositories."; an explicit empty
	// array → "must contain at least one repository."
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1none", adminUser, adminPass,
		`{"principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "Permission target request missing repositories.") {
		t.Fatalf("absent repositories = %d body=%s, want the missing-repositories 400", code, raw)
	}
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1empty", adminUser, adminPass,
		`{"repositories":[],"principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "Permission target must contain at least one repository.") {
		t.Fatalf("empty repositories = %d body=%s, want the must-contain 400", code, raw)
	}

	// The reference's unknown-repository wording on the classic face.
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1repo", adminUser, adminPass,
		`{"repositories":["no-such-repo"],"principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "Permission target contains a reference to a non-existing repository 'no-such-repo'.") {
		t.Fatalf("unknown repository = %d body=%s, want the reference wording", code, raw)
	}

	// …and its unknown-user wording (the differential leg's third arm).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-v1user", adminUser, adminPass,
		`{"repositories":["generic-local"],"principals":{"users":{"no-such-user":["r"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusBadRequest ||
		!strings.Contains(raw, "Permission target contains a reference to a non-existing user: 'no-such-user'.") {
		t.Fatalf("unknown user = %d body=%s, want the reference wording", code, raw)
	}

	// A rich-dialect body still works on the classic face (the hybrid arm —
	// the alias is additive, BinFlow-native consumers keep their spelling).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l006b-rich", adminUser, adminPass,
		`{"repos":["generic-local"],"principals":{"users":{"dev":["read","deploy-cache"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("rich-dialect on classic face = %d body=%s, want 201", code, raw)
	}
}

// TestPermissionsV1AliasKeyedNegative (Review A rework, B-②): the keyed
// face's negative legs — the path-key injection happens BEFORE the family-4
// gate, so a manage holder cannot dodge the replace-time coverage union (B1)
// by omitting the body name; a plain user meets the 403 on the keyed verb.
func TestPermissionsV1AliasKeyedNegative(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h) // app-local in coverage, other-local out; carol2 = manage-only holder; dave = plain

	// The out-of-coverage victim, created by admin on other-local.
	t217PutTarget(t, h, adminUser, adminPass, "victim",
		`["other-local"]`, `{"users":{"dave":["read"]}}`, http.StatusCreated)

	// Leg 1: the manage holder keyed-PUTs the victim with a NAMELESS v1
	// body whose repositories sit inside coverage — the path key injects
	// "victim" before the gate, the replace-time union check then demands
	// the STORED target's repos sit in coverage too (other-local does not)
	// → 403, and the stored target is untouched.
	resp := t215As(t, h, http.MethodPut, "api/security/permissions/victim", "carol2", "carol2-pw",
		`{"repositories":["app-local"],"principals":{"users":{"carol2":["r","w"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("holder keyed replace of out-of-coverage target = %d body=%s, want 403", code, raw)
	}
	for _, tg := range t217ListTargets(t, h) {
		if tg.Name == "victim" {
			if len(tg.Repos) != 1 || tg.Repos[0] != "other-local" {
				t.Errorf("victim repos after the refused replace = %v, want [other-local] (list must be unchanged)", tg.Repos)
			}
			if got := tg.Principals.Users["dave"]; len(got) != 1 || got[0] != "read" {
				t.Errorf("victim principals after the refused replace = %v, want dave:[read]", tg.Principals.Users)
			}
		}
	}

	// Leg 2: a plain user keyed-PUT meets the family-4 403.
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/t-app", "dave", "dave-pw",
		`{"repositories":["app-local"],"principals":{}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusForbidden {
		t.Fatalf("plain-user keyed PUT = %d body=%s, want 403", code, raw)
	}
}
