package httpapi_test

// ADR-0050 (LOOP 008 L008-1b): the wire-level arms of update-merge that
// live in the TRANSPORT layer — the credential seats ride raw JSON so an
// explicit null survives the decode (a typed string/pointer would flatten
// it onto absent), and the description merge reads the raw body's key
// presence inside handleRepoPost. The PUT create-only contract (400 on an
// existing key) is pinned in t80_repo_model_test.go; the seat-by-seat
// matrix at the service layer is remote_update_merge_test.go (repo pkg).

import (
	"net/http"
	"testing"
)

// TestRepoUpdateMergeCredentialWire: username through the raw-JSON
// transport — null clears, a value writes, omit keeps.
func TestRepoUpdateMergeCredentialWire(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "cred-wire",
		`{"rclass":"remote","packageType":"generic","url":"http://u","username":"wire-user"}`); status != http.StatusOK {
		t.Fatalf("create: %d %s", status, body)
	}

	// Omit keeps the username (a hardFail-only update).
	if status, body := postRepoStatus(t, h, "cred-wire", `{"hardFail":true}`); status != http.StatusOK {
		t.Fatalf("omit update: %d %s", status, body)
	}
	if got := configField(t, h, "cred-wire", "username"); got != "wire-user" {
		t.Fatalf("username after omit = %v, want kept", got)
	}

	// Explicit null clears (the raw transport's whole reason to exist).
	if status, body := postRepoStatus(t, h, "cred-wire", `{"username":null}`); status != http.StatusOK {
		t.Fatalf("null update: %d %s", status, body)
	}
	if got := configField(t, h, "cred-wire", "username"); got != nil {
		t.Fatalf("username after null = %v, want cleared", got)
	}

	// A value writes (and the url was never re-sent once).
	if status, body := postRepoStatus(t, h, "cred-wire", `{"username":"bot"}`); status != http.StatusOK {
		t.Fatalf("write update: %d %s", status, body)
	}
	if got := configField(t, h, "cred-wire", "username"); got != "bot" {
		t.Fatalf("username after write = %v, want bot", got)
	}
	if got := configField(t, h, "cred-wire", "url"); got != "http://u" {
		t.Fatalf("url after credential updates = %v, want kept", got)
	}
}

// TestRepoUpdateDescriptionMergeWire: the description seat merges on the
// raw body's key presence — omit keeps, null and "" clear, a value writes.
func TestRepoUpdateDescriptionMergeWire(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "desc-wire",
		`{"rclass":"remote","packageType":"generic","url":"http://u","description":"original words"}`); status != http.StatusOK {
		t.Fatalf("create: %d %s", status, body)
	}

	// Omit: a config-only update keeps the stored description.
	if status, body := postRepoStatus(t, h, "desc-wire", `{"hardFail":true}`); status != http.StatusOK {
		t.Fatalf("omit update: %d %s", status, body)
	}
	_, cfg := getRepoJSON(t, h, "desc-wire")
	if cfg["description"] != "original words" {
		t.Fatalf("description after omit = %v, want kept", cfg["description"])
	}

	// null clears.
	if status, body := postRepoStatus(t, h, "desc-wire", `{"description":null}`); status != http.StatusOK {
		t.Fatalf("null update: %d %s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "desc-wire")
	if cfg["description"] != "" {
		t.Fatalf("description after null = %v, want cleared", cfg["description"])
	}

	// A value writes; "" clears again; the local arm works the same way.
	if status, body := postRepoStatus(t, h, "desc-wire", `{"description":"third"}`); status != http.StatusOK {
		t.Fatalf("write update: %d %s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "desc-wire")
	if cfg["description"] != "third" {
		t.Fatalf("description after write = %v, want third", cfg["description"])
	}
	if status, body := postRepoStatus(t, h, "desc-wire", `{"description":""}`); status != http.StatusOK {
		t.Fatalf("empty update: %d %s", status, body)
	}
	_, cfg = getRepoJSON(t, h, "desc-wire")
	if cfg["description"] != "" {
		t.Fatalf("description after empty = %v, want cleared", cfg["description"])
	}
}

// TestRepoUpdateEmptyBodyKeepsAll: POST {} is a legal no-op update (the A9
// family: even the url may be omitted).
func TestRepoUpdateEmptyBodyKeepsAll(t *testing.T) {
	h := newHarness(t)
	if status, body := putRepoStatus(t, h, "bare-wire",
		`{"rclass":"remote","packageType":"generic","url":"http://u","hardFail":true,"username":"keepme"}`); status != http.StatusOK {
		t.Fatalf("create: %d %s", status, body)
	}
	if status, body := postRepoStatus(t, h, "bare-wire", `{}`); status != http.StatusOK {
		t.Fatalf("empty-body update: %d %s", status, body)
	}
	for field, want := range map[string]any{
		"url": "http://u", "hardFail": true, "username": "keepme",
	} {
		if got := configField(t, h, "bare-wire", field); got != want {
			t.Fatalf("%s after {} update = %v, want %v", field, got, want)
		}
	}
}
