package httpapi_test

// L025-3B (D02 batch write family, rest-api.md sections 2.1.5-2.1.7): the
// v2 batch verbs — PUT batch-create (all-or-nothing + the byte-exact 201
// text body), POST batch-modify (merge dialect, bare-text 404/403 arms)
// and DELETE batch-delete (the 207 state machine: ghost and concurrent
// entries report success, all-ghost 200, mixed 207, all-fail first-status).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// putBatch issues the v2 batch-create request.
func putBatch(t *testing.T, h *harness, body string, user, pass string) *http.Response {
	t.Helper()
	return h.do(http.MethodPut, "/binflow/api/v2/repositories/batch", user, pass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
}

// postBatch issues the v2 batch-modify request.
func postBatch(t *testing.T, h *harness, body string, user, pass string, hdr map[string]string) *http.Response {
	t.Helper()
	if hdr == nil {
		hdr = map[string]string{"Content-Type": "application/json"}
	}
	return h.do(http.MethodPost, "/binflow/api/v2/repositories/batch", user, pass, []byte(body), hdr)
}

// deleteBatch issues the v2 batch-delete request.
func deleteBatch(t *testing.T, h *harness, body, user, pass string) *http.Response {
	t.Helper()
	return h.do(http.MethodDelete, "/binflow/api/v2/repositories/batch", user, pass,
		[]byte(body), map[string]string{"Content-Type": "application/json"})
}

// uploadBatchArtifact lands one file node (the deletedArtifactsCount seed).
func uploadBatchArtifact(t *testing.T, h *harness, repoKey, path string) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/"+repoKey+"/"+path, adminUser, adminPass,
		[]byte("content-of-"+path), nil)
	if out, code := mustGet(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("upload %s/%s: status %d body=%s", repoKey, path, code, out)
	}
}

// repoExists asserts the v1 read's verdict on one key.
func repoExists(t *testing.T, h *harness, key string) bool {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	return resp.StatusCode == http.StatusOK
}

// repoDescription reads one row's stored description over the v1 face.
func repoDescription(t *testing.T, h *harness, key string) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read %s: status %d body=%s", key, resp.StatusCode, body)
	}
	var row struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(body), &row); err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return row.Description
}

// batchReportFor fetches one per-key report out of a decoded aggregate body.
func batchReportFor(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	reps, _ := body["reports"].([]any)
	for _, re := range reps {
		m, _ := re.(map[string]any)
		if m["repoKey"] == key {
			return m
		}
	}
	t.Fatalf("no report for %q in %v", key, body)
	return nil
}

// batchDeleteStore wraps the metadata store for the DELETE state machine's
// injected arms: one repository whose content teardown fails (the 207
// mixed / all-fail aggregation) and one whose teardown sleeps (holding the
// in-flight lock window open for the concurrent arm).
type batchDeleteStore struct {
	metadata.Store
	failRepo, delayRepo string
	delay               time.Duration
}

type batchDeleteNodes struct {
	metadata.NodeStore
	failRepo, delayRepo string
	delay               time.Duration
}

func (s *batchDeleteStore) Nodes() metadata.NodeStore {
	return &batchDeleteNodes{
		NodeStore: s.Store.Nodes(), failRepo: s.failRepo,
		delayRepo: s.delayRepo, delay: s.delay,
	}
}

func (n *batchDeleteNodes) DeleteByPrefix(ctx context.Context, repoKey, prefix string) (int64, error) {
	if repoKey == n.delayRepo {
		time.Sleep(n.delay)
	}
	if repoKey == n.failRepo {
		return 0, errors.New("injected content teardown failure")
	}
	return n.NodeStore.DeleteByPrefix(ctx, repoKey, prefix)
}

// ---- PUT batch-create (spec 2.1.5) ----

// TestRepoBatchPutCreates: the 201 text/plain body is byte-exact (trailing
// space, blank line between and after each block), the repositories exist,
// and the empty array is a trivially legal batch (201, empty body — the
// spec-pending arm pinned as implemented).
func TestRepoBatchPutCreates(t *testing.T) {
	h := newHarness(t)
	resp := putBatch(t, h, `[{"key":"wb1","rclass":"local","packageType":"generic"},
	                         {"key":"wb2","rclass":"local","packageType":"generic"}]`, adminUser, adminPass)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	want := "Successfully created repository 'wb1' \n\nSuccessfully created repository 'wb2' \n\n"
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
	if !repoExists(t, h, "wb1") || !repoExists(t, h, "wb2") {
		t.Errorf("both repositories must exist after the batch")
	}

	resp = putBatch(t, h, `[]`, adminUser, adminPass)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated || body != "" {
		t.Errorf("empty array: status %d body=%q (spec-pending arm, pinned as implemented)", resp.StatusCode, body)
	}
}

// TestRepoBatchPutAllOrNothing: an existing key fails the whole batch with
// the verbatim name-validation message and nothing new is created; several
// problems join with \n into one envelope message; a within-batch duplicate
// is the same conflict; and a creation-time config failure unwinds the
// created prefix.
func TestRepoBatchPutAllOrNothing(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "wbtaken", `{"rclass":"local","packageType":"generic"}`)

	t.Run("existing key refuses whole batch", func(t *testing.T) {
		resp := putBatch(t, h, `[{"key":"wbnew","rclass":"local","packageType":"generic"},
		                         {"key":"wbtaken","rclass":"local","packageType":"generic"}]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		if msg := envelopeMessage(t, body); msg !=
			"error when validating repository name: wbtaken : Repository key already exists" {
			t.Errorf("message = %q", msg)
		}
		if repoExists(t, h, "wbnew") {
			t.Errorf("the batch's new key must not be created")
		}
	})

	t.Run("multiple problems join with newline", func(t *testing.T) {
		resp := putBatch(t, h, `[{"rclass":"local","packageType":"generic"},
		                         {"key":"wbtaken","rclass":"local","packageType":"generic"}]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		want := "Repository key are missing in configuration\n" +
			"error when validating repository name: wbtaken : Repository key already exists"
		if msg := envelopeMessage(t, body); msg != want {
			t.Errorf("message = %q, want %q", msg, want)
		}
	})

	t.Run("within-batch duplicate is the same conflict", func(t *testing.T) {
		resp := putBatch(t, h, `[{"key":"wbdup","rclass":"local","packageType":"generic"},
		                         {"key":"wbdup","rclass":"local","packageType":"generic"}]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		if msg := envelopeMessage(t, body); msg !=
			"error when validating repository name: wbdup : Repository key already exists" {
			t.Errorf("message = %q", msg)
		}
		if repoExists(t, h, "wbdup") {
			t.Errorf("duplicate key must not be created")
		}
	})

	t.Run("creation-time failure rolls back the prefix", func(t *testing.T) {
		resp := putBatch(t, h, `[{"key":"wbok","rclass":"local","packageType":"generic"},
		                         {"key":"wbvirt","rclass":"virtual","packageType":"generic",
		                          "repositories":["wbghostmember"]}]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		if msg := envelopeMessage(t, body); !strings.Contains(msg, "wbghostmember") {
			t.Errorf("message = %q, want the virtual-member refusal", msg)
		}
		if repoExists(t, h, "wbok") {
			t.Errorf("the created prefix must be rolled back")
		}
	})
}

// TestRepoBatchPutLimitsAndGates: the >100 limit message verbatim, the
// malformed-body 400, the admin gate (403 non-admin: the BARE Forbidden
// envelope —
// reference's own was never captured) and the anonymous 401 challenge.
func TestRepoBatchPutLimitsAndGates(t *testing.T) {
	h := newHarness(t)
	items := make([]string, 101)
	for i := range items {
		items[i] = fmt.Sprintf(`{"key":"wblim%d","rclass":"local","packageType":"generic"}`, i)
	}
	resp := putBatch(t, h, "["+strings.Join(items, ",")+"]", adminUser, adminPass)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if msg := envelopeMessage(t, body); msg != "Repository item limit exceeded: 101. Limit: 100" {
		t.Errorf("message = %q", msg)
	}

	resp = putBatch(t, h, `{"not":"an array"}`, adminUser, adminPass)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed body status %d", resp.StatusCode)
	}

	h2 := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	resp = putBatch(t, h2, `[{"key":"wbu","rclass":"local","packageType":"generic"}]`, "u1", "p1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status %d body=%s", resp.StatusCode, mustGet(t, resp))
	}
	// L025-5 / diff G4: the envelope carries the BARE "Forbidden" (the
	// configurations face's own wording, live).
	if eb := decodeError(t, resp); eb.Errors[0].Message != "Forbidden" {
		t.Fatalf("non-admin message = %q, want the bare Forbidden", eb.Errors[0].Message)
	}

	resp = putBatch(t, h, `[{"key":"wba","rclass":"local","packageType":"generic"}]`, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status %d", resp.StatusCode)
	}
}

// ---- POST batch-modify (spec 2.1.6) ----

// TestRepoBatchPostMergeDialect: success is the bare string under
// application/json, the description merges on key presence, and the remote
// arm merges its config off the stored baseline (an omitted url keeps the
// upstream while an explicit knob lands).
func TestRepoBatchPostMergeDialect(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "wbrem",
		`{"rclass":"remote","packageType":"generic","url":"https://example.com/up","description":"orig-desc"}`)

	resp := postBatch(t, h, `[{"key":"wbrem","hardFail":true}]`, adminUser, adminPass, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body != "Repositories updated successfully." {
		t.Errorf("body = %q, want the bare success string", body)
	}

	get := h.do(http.MethodGet, "/binflow/api/repositories/wbrem", adminUser, adminPass, nil, nil)
	row := decodeJSONMap(t, mustGet(t, get))
	if row["description"] != "orig-desc" {
		t.Errorf("omitted description must keep the stored value: %v", row["description"])
	}
	cfg, _ := row["configuration"].(map[string]any)
	if cfg["url"] != "https://example.com/up" {
		t.Errorf("remote merge must keep the baseline url: %v", cfg["url"])
	}
	if cfg["hardFail"] != true {
		t.Errorf("explicit knob must land: %v", cfg["hardFail"])
	}
}

// TestRepoBatchPostMissingAndVendor: a ghost key fails the whole batch with
// the bare-text 404 (application/json, no envelope, no side effects), and a
// vendor Content-Type naming another rclass drops matching repositories
// into the same 404 list.
func TestRepoBatchPostMissingAndVendor(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "wbkeep",
		`{"rclass":"local","packageType":"generic","description":"keep-me"}`)

	resp := postBatch(t, h, `[{"key":"wbkeep","description":"changed"},{"key":"wbghost"}]`, adminUser, adminPass, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body != "No repositories found for the following keys: wbghost" {
		t.Errorf("body = %q", body)
	}
	if strings.HasPrefix(body, `{"errors"`) {
		t.Errorf("the 404 must be bare text, not an envelope: %s", body)
	}
	if d := repoDescription(t, h, "wbkeep"); d != "keep-me" {
		t.Errorf("the resolved key must be untouched: %q", d)
	}

	resp = postBatch(t, h, `[{"key":"wbkeep","description":"changed"}]`, adminUser, adminPass,
		map[string]string{"Content-Type": strings.ToLower(v2RemoteCT)})
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("vendor filter status %d body=%s", resp.StatusCode, body)
	}
	if body != "No repositories found for the following keys: wbkeep" {
		t.Errorf("vendor filter body = %q", body)
	}
}

// TestRepoBatchPostUnauthorizedAndValidation: the per-key update gate
// aggregates into the bare-text 403 (a plain user, nothing touched), the
// missing-key and limit 400s are verbatim, and a mid-batch update failure
// restores the touched snapshots before answering.
func TestRepoBatchPostUnauthorizedAndValidation(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedConfigFamilyRepo(t, h, "wbsec", `{"rclass":"local","packageType":"generic","description":"locked"}`)

	resp := postBatch(t, h, `[{"key":"wbsec","description":"nope"}]`, "u1", "p1", nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if body != "User is not authorized to update the following repositories: wbsec" {
		t.Errorf("body = %q", body)
	}
	if d := repoDescription(t, h, "wbsec"); d != "locked" {
		t.Errorf("unauthorized update must not touch the row: %q", d)
	}

	resp = postBatch(t, h, `[{"description":"no key"}]`, adminUser, adminPass, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest || envelopeMessage(t, body) != "Repository key are missing in configuration" {
		t.Errorf("missing key: status %d body=%s", resp.StatusCode, body)
	}

	items := make([]string, 101)
	for i := range items {
		items[i] = fmt.Sprintf(`{"key":"wbl%d"}`, i)
	}
	resp = postBatch(t, h, "["+strings.Join(items, ",")+"]", adminUser, adminPass, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest || envelopeMessage(t, body) != "Repository item limit exceeded: 101. Limit: 100" {
		t.Errorf("limit: status %d body=%s", resp.StatusCode, body)
	}

	// Mid-batch failure: the immutable-type refusal on the second item must
	// restore the first item's pre-batch snapshot.
	seedConfigFamilyRepo(t, h, "wbxa", `{"rclass":"local","packageType":"generic","description":"a-orig"}`)
	seedConfigFamilyRepo(t, h, "wbxb", `{"rclass":"local","packageType":"generic"}`)
	resp = postBatch(t, h, `[{"key":"wbxa","description":"a-changed"},{"key":"wbxb","rclass":"virtual"}]`, adminUser, adminPass, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mid-batch failure status %d body=%s", resp.StatusCode, body)
	}
	if msg := envelopeMessage(t, body); !strings.Contains(msg, "immutable") {
		t.Errorf("message = %q, want the immutability refusal", msg)
	}
	if d := repoDescription(t, h, "wbxa"); d != "a-orig" {
		t.Errorf("the touched snapshot must be restored: %q", d)
	}
}

// ---- DELETE batch-delete (spec 2.1.7) ----

// TestRepoBatchDeleteEmptyArm: the empty array, a missing body and a
// malformed body all answer the 400 statusMessage-only object (no reports
// key, not an errors envelope).
func TestRepoBatchDeleteEmptyArm(t *testing.T) {
	h := newHarness(t)
	for _, body := range []string{`[]`, ``, `{"not":"an array"}`, `["a", 5]`} {
		resp := deleteBatch(t, h, body, adminUser, adminPass)
		out := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q status %d body=%s", body, resp.StatusCode, out)
		}
		m := decodeJSONMap(t, out)
		if m["statusMessage"] != "No repository keys were provided for deletion" {
			t.Errorf("%q statusMessage = %v", body, m["statusMessage"])
		}
		if _, has := m["reports"]; has {
			t.Errorf("%q must carry no reports key: %s", body, out)
		}
		if _, has := m["errors"]; has {
			t.Errorf("%q must not be an errors envelope: %s", body, out)
		}
	}
}

// TestRepoBatchDeleteReports: the ghost arm reports success with the
// config-does-not-exist statusMsg, duplicates collapse, a real local
// delete carries the artifact count and the local wording, a virtual
// delete carries the virtual wording, and every-success batches (ghosts
// included) answer 200.
func TestRepoBatchDeleteReports(t *testing.T) {
	h := newHarness(t)
	seedConfigFamilyRepo(t, h, "wbl", `{"rclass":"local","packageType":"generic"}`)
	seedConfigFamilyRepo(t, h, "wbl2", `{"rclass":"local","packageType":"generic"}`)
	seedConfigFamilyRepo(t, h, "wbv", `{"rclass":"virtual","packageType":"generic","repositories":["wbl2"]}`)
	uploadBatchArtifact(t, h, "wbl", "one.txt")
	uploadBatchArtifact(t, h, "wbl", "two.txt")

	t.Run("ghost-only answers 200 all-success", func(t *testing.T) {
		resp := deleteBatch(t, h, `["wbg","wbg"]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		m := decodeJSONMap(t, body)
		if m["statusMessage"] != "All repositories were removed successfully" {
			t.Errorf("statusMessage = %v", m["statusMessage"])
		}
		reps, _ := m["reports"].([]any)
		if len(reps) != 1 {
			t.Fatalf("duplicates must collapse: %s", body)
		}
		rep := batchReportFor(t, m, "wbg")
		if rep["success"] != true || rep["deletedArtifactsCount"] != float64(0) {
			t.Errorf("ghost report = %v", rep)
		}
		if rep["statusMsg"] != "Cannot delete repository: 'wbg', repository config does not exist" {
			t.Errorf("ghost statusMsg = %v", rep["statusMsg"])
		}
		if _, has := rep["errors"]; has {
			t.Errorf("ghost report must carry no errors: %v", rep)
		}
	})

	t.Run("local with content and virtual wording", func(t *testing.T) {
		seedConfigFamilyRepo(t, h, "wbrem2",
			`{"rclass":"remote","packageType":"generic","url":"https://example.com/up"}`)
		resp := deleteBatch(t, h, `["wbl","wbv","wbl2","wbrem2"]`, adminUser, adminPass)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body=%s", resp.StatusCode, body)
		}
		m := decodeJSONMap(t, body)
		if m["statusMessage"] != "All repositories were removed successfully" {
			t.Errorf("statusMessage = %v", m["statusMessage"])
		}
		local := batchReportFor(t, m, "wbl")
		if local["statusMsg"] != "Repository 'wbl' and all its content have been removed successfully." {
			t.Errorf("local statusMsg = %v", local["statusMsg"])
		}
		if local["deletedArtifactsCount"] != float64(2) {
			t.Errorf("local deletedArtifactsCount = %v, want 2", local["deletedArtifactsCount"])
		}
		virt := batchReportFor(t, m, "wbv")
		if virt["statusMsg"] != "Repository 'wbv' has been removed successfully." {
			t.Errorf("virtual statusMsg = %v", virt["statusMsg"])
		}
		// Remote rides the content-bearing wording — live-verified against
		// the reference (L025-3B probe, 2026-09-17).
		rem := batchReportFor(t, m, "wbrem2")
		if rem["statusMsg"] != "Repository 'wbrem2' and all its content have been removed successfully." {
			t.Errorf("remote statusMsg = %v", rem["statusMsg"])
		}
		for _, key := range []string{"wbl", "wbv", "wbl2", "wbrem2"} {
			if repoExists(t, h, key) {
				t.Errorf("%s must be gone", key)
			}
		}
	})
}

// TestRepoBatchDeletePreValidation: the permission arm aborts the whole
// batch with the verbatim statusMessage form and nothing is deleted; the
// anonymous caller meets the 401 challenge; the blank key and the system
// trash key abort with the single-report form.
func TestRepoBatchDeletePreValidation(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"u1", "p1"}})
	seedConfigFamilyRepo(t, h, "wbpv", `{"rclass":"local","packageType":"generic"}`)

	resp := deleteBatch(t, h, `["wbpv"]`, "u1", "p1")
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	m := decodeJSONMap(t, body)
	if m["statusMessage"] != "Cannot delete repository: 'wbpv', Reason: User: ('u1') has insufficient permission to delete repositories: wbpv" {
		t.Errorf("statusMessage = %v", m["statusMessage"])
	}
	if _, has := m["reports"]; has {
		t.Errorf("abort body must carry no reports key: %s", body)
	}
	if !repoExists(t, h, "wbpv") {
		t.Errorf("the aborted batch must delete nothing")
	}

	resp = deleteBatch(t, h, `["wbpv"]`, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous status %d", resp.StatusCode)
	}

	// L025-5 / diff G2: a blank key rides the per-repo ghost arm — the
	// batch answers 200 with the '' row's success:true "repository config
	// does not exist" report (spec §2.1.7-2's whole-batch abort is voided
	// by the live probe).
	resp = deleteBatch(t, h, `[""]`, adminUser, adminPass)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blank-key batch status %d body=%s", resp.StatusCode, body)
	}
	var blankBatch struct {
		Reports []struct {
			RepoKey   string `json:"repoKey"`
			Success   bool   `json:"success"`
			StatusMsg string `json:"statusMsg"`
		} `json:"reports"`
	}
	if json.Unmarshal([]byte(body), &blankBatch) != nil || len(blankBatch.Reports) != 1 ||
		blankBatch.Reports[0].RepoKey != "" || !blankBatch.Reports[0].Success ||
		blankBatch.Reports[0].StatusMsg != "Cannot delete repository: '', repository config does not exist" {
		t.Fatalf("blank-key batch reports = %s", body)
	}

	resp = deleteBatch(t, h, `["auto-trashcan"]`, adminUser, adminPass)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("trash key status %d body=%s", resp.StatusCode, body)
	}
	if m := decodeJSONMap(t, body); !strings.HasPrefix(m["statusMessage"].(string), "The trash can ('auto-trashcan') is a system repository") {
		t.Errorf("trash key statusMessage = %v", m["statusMessage"])
	}
}

// TestRepoBatchDeleteMixedAndAllFail: an injected content-teardown failure
// on one repository of a mixed batch answers 207 with the failure report's
// full field set; an all-failed batch answers the first failure's status
// (the injected error maps to 500).
func TestRepoBatchDeleteMixedAndAllFail(t *testing.T) {
	h := newHarnessAuth(t, nil, nil, func(md metadata.Store) metadata.Store {
		return &batchDeleteStore{Store: md, failRepo: "wbboom"}
	}, nil)
	seedConfigFamilyRepo(t, h, "wbmix", `{"rclass":"local","packageType":"generic"}`)
	seedConfigFamilyRepo(t, h, "wbboom", `{"rclass":"local","packageType":"generic"}`)
	uploadBatchArtifact(t, h, "wbmix", "f.txt")
	uploadBatchArtifact(t, h, "wbboom", "f.txt") // the teardown must fire to fail

	resp := deleteBatch(t, h, `["wbmix","wbboom"]`, adminUser, adminPass)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	m := decodeJSONMap(t, body)
	if m["statusMessage"] != "Some repositories failed to be removed" {
		t.Errorf("statusMessage = %v", m["statusMessage"])
	}
	ok := batchReportFor(t, m, "wbmix")
	if ok["success"] != true || ok["deletedArtifactsCount"] != float64(1) {
		t.Errorf("mixed success report = %v", ok)
	}
	fail := batchReportFor(t, m, "wbboom")
	if fail["success"] != false {
		t.Errorf("failure report success = %v", fail["success"])
	}
	if fail["deleteArtifactsFailureCount"] != float64(0) {
		t.Errorf("failure count field = %v", fail["deleteArtifactsFailureCount"])
	}
	errs, _ := fail["errors"].([]any)
	entry, _ := errs[0].(map[string]any)
	if entry["status"] != float64(http.StatusInternalServerError) || entry["message"] != "repository operation failed" {
		t.Errorf("failure errors entry = %v", entry)
	}
	if repoExists(t, h, "wbmix") {
		t.Errorf("wbmix must be deleted")
	}

	// wbboom's row survived its failed teardown (the refusal precedes the
	// repositories-row delete), so deleting it alone is the all-fail arm.
	resp = deleteBatch(t, h, `["wbboom"]`, adminUser, adminPass)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("all-fail status %d body=%s (want the first failure's status)", resp.StatusCode, body)
	}
	if m := decodeJSONMap(t, body); m["statusMessage"] != "Some repositories failed to be removed" {
		t.Errorf("all-fail statusMessage = %v (spec-pending wording, pinned as implemented)", m["statusMessage"])
	}
}

// TestRepoBatchDeleteConcurrentInProgress: while one batch delete is inside
// a repository's teardown, a second batch delete naming the same key gets
// the in-progress SUCCESS report — never a block, never a failure. The 2s
// injected teardown holds the lock window far past the second request's
// 300ms send time, so exactly one side sees each arm whichever request
// acquires first.
func TestRepoBatchDeleteConcurrentInProgress(t *testing.T) {
	h := newHarnessAuth(t, nil, nil, func(md metadata.Store) metadata.Store {
		return &batchDeleteStore{Store: md, delayRepo: "wbslow", delay: 2 * time.Second}
	}, nil)
	seedConfigFamilyRepo(t, h, "wbslow", `{"rclass":"local","packageType":"generic"}`)
	uploadBatchArtifact(t, h, "wbslow", "f.txt")

	var wg sync.WaitGroup
	var firstBody string
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp := deleteBatch(t, h, `["wbslow"]`, adminUser, adminPass)
		firstBody = mustGet(t, resp)
	}()
	time.Sleep(300 * time.Millisecond)
	second := deleteBatch(t, h, `["wbslow"]`, adminUser, adminPass)
	secondBody := mustGet(t, second)
	wg.Wait()

	if second.StatusCode != http.StatusOK {
		t.Fatalf("second status %d body=%s", second.StatusCode, secondBody)
	}
	msgs := []string{secondBody, firstBody}
	var inProgress, removed int
	for _, b := range msgs {
		m := decodeJSONMap(t, b)
		if m["statusMessage"] != "All repositories were removed successfully" {
			t.Fatalf("aggregate statusMessage = %v in %s", m["statusMessage"], b)
		}
		rep := batchReportFor(t, m, "wbslow")
		switch rep["statusMsg"] {
		case "Cannot delete repository: 'wbslow', repository deletion is already in progress":
			inProgress++
			if rep["success"] != true {
				t.Errorf("in-progress report must be success:true: %v", rep)
			}
		case "Repository 'wbslow' and all its content have been removed successfully.":
			removed++
		default:
			t.Fatalf("unexpected statusMsg %v", rep["statusMsg"])
		}
	}
	if inProgress != 1 || removed != 1 {
		t.Errorf("want exactly one in-progress and one real delete, got %d/%d", inProgress, removed)
	}
	if repoExists(t, h, "wbslow") {
		t.Errorf("wbslow must be gone")
	}
}
