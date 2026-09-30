package httpapi_test

// T-622 (BIN-106) — repo create/update success-message BYTES, pinned to the
// live reference 7.161.26 (2026-09-30, two deterministic A-side rounds,
// repos difftest-t622-*): the create face carries exactly one space between
// the closing quote and the newline ("…'<key>' \n"), while the update face
// has none after its period ("… update successfully.\n"). The legacy
// assertions elsewhere (compat_test, t80_repo_model_test, curl_compat_test)
// trim or substring, so they stay valid under both spellings.

import (
	"net/http"
	"testing"
)

func TestRepoCreateSuccessMessageTrailingSpace(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "t622-msg-local", `{"rclass":"local","packageType":"generic"}`)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	want := "Successfully created repository 't622-msg-local' \n"
	if body != want {
		t.Fatalf("create body = %q, want %q (one space before the newline)", body, want)
	}
}

func TestRepoUpdateSuccessMessageNoTrailingSpace(t *testing.T) {
	h := newHarness(t)
	resp := putRepo(t, h, "t622-msg-local", `{"rclass":"local","packageType":"generic"}`)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	resp = postRepo(t, h, "t622-msg-local",
		`{"rclass":"local","packageType":"generic","description":"t622 update"}`)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", resp.StatusCode, body)
	}
	want := "Repository t622-msg-local update successfully.\n"
	if body != want {
		t.Fatalf("update body = %q, want %q (no space before the newline)", body, want)
	}
}
