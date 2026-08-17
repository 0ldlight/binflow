package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b) //nolint:gosec // test fixture digest
	return hex.EncodeToString(sum[:])
}

// TestSystemEndpoints: the four M1 system/management endpoints — ping
// (anonymous, plain text), version (honest identity, Q4), v1 health
// (subsystem verdicts) and v1 storage stats (E-23 numbers).
func TestSystemEndpoints(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	t.Run("ping is 200 text OK without auth", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/system/ping", "", "", nil, nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
			t.Fatalf("content-type = %q", got)
		}
		if body := mustGet(t, resp); body != "OK" {
			t.Fatalf("body = %q, want OK", body)
		}
	})

	t.Run("version is honest BinFlow identity", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/system/version", "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var v struct {
			Version  string `json:"version"`
			Revision string `json:"revision"`
			Product  string `json:"product"`
		}
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if v.Product != "BinFlow" {
			t.Fatalf("product = %q, want BinFlow (Q4: never emulate Artifactory)", v.Product)
		}
		if v.Version == "" {
			t.Fatal("version is empty")
		}
	})

	t.Run("v1 health reports ok with subsystems", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v1/health", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var health struct {
			Status   string                  `json:"status"`
			Storage  struct{ Status string } `json:"storage"`
			Metadata struct{ Status string } `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(body), &health); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if health.Status != "ok" {
			t.Fatalf("status = %q, want ok; body=%s", health.Status, body)
		}
		if health.Storage.Status != "ok" || health.Metadata.Status != "ok" {
			t.Fatalf("subsystems = storage:%s metadata:%s", health.Storage.Status, health.Metadata.Status)
		}
	})

	t.Run("v1 storage stats counts blobs and bytes", func(t *testing.T) {
		// Two nodes with identical content: one blob physically.
		content := []byte("dedup-me")
		for _, p := range []string{"a/x.bin", "b/y.bin"} {
			resp := h.do(http.MethodPut, "/binflow/generic-local/"+p, adminUser, adminPass, content, nil)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT %s status = %d", p, resp.StatusCode)
			}
			defer func() { _ = resp.Body.Close() }()
		}
		resp := h.do(http.MethodGet, "/binflow/api/v1/storage/stats", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
		}
		var stats struct {
			Blobs         int64 `json:"blobs"`
			LogicalBytes  int64 `json:"logical_bytes"`
			PhysicalBytes int64 `json:"physical_bytes"`
		}
		if err := json.Unmarshal([]byte(body), &stats); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		if stats.Blobs != 1 {
			t.Fatalf("blobs = %d, want 1 (dedup observable, C12)", stats.Blobs)
		}
		if want := int64(len(content)) * 2; stats.LogicalBytes != want {
			t.Fatalf("logical_bytes = %d, want %d", stats.LogicalBytes, want)
		}
		if stats.PhysicalBytes != int64(len(content)) {
			t.Fatalf("physical_bytes = %d, want %d", stats.PhysicalBytes, len(content))
		}
	})
}

// TestProbes: /healthz and /readyz answer 200 without credentials and
// without the /binflow prefix (architecture section 7.1 probe exception).
func TestProbes(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		if body != "OK" {
			t.Fatalf("%s body = %q", path, body)
		}
	}
}

// TestConsolePlaceholder: /binflow and /binflow/ serve the console JSON.
func TestConsolePlaceholder(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/binflow", "/binflow/"} {
		resp := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, resp.StatusCode)
		}
		var c struct {
			Service string `json:"service"`
			Console string `json:"console"`
		}
		if err := json.Unmarshal([]byte(body), &c); err != nil || c.Service != "binflow" || c.Console != "M4" {
			t.Fatalf("%s body = %q", path, body)
		}
	}
}
