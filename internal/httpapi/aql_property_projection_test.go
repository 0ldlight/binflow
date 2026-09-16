package httpapi_test

// L024-3A: the AQL wire legs jf build-publish depends on (aql.md §16.1, the
// client-captured query frozen verbatim): the bare include("property")
// operand renders the "properties" [{key,value}] array, a property-less row
// OMITS the key entirely (§16.1-4), and {"path":{"$ne":"."}} excludes
// root-level rows whose path value is the literal dot (§16.1-1). D11's
// "Unknown AQL field: property" rejection must be gone.

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// seedJFStack lands the jf shape playground: a build artifact with the two
// properties jf itself attaches, a plain sibling, and a root-level file.
func seedJFStack(t *testing.T, h *harness) {
	t.Helper()
	seedRepo(t, h, "jf-local")
	deposit(t, h, "jf-local", "mybuild/1/artifact.zip", "zip-bytes")
	deposit(t, h, "jf-local", "mybuild/1/plain.bin", "plain-bytes")
	deposit(t, h, "jf-local", "root.bin", "root-bytes")
	if err := h.md.NodeProps().Merge(context.Background(), "jf-local", "mybuild/1/artifact.zip",
		map[string][]string{"build.name": {"mybuild"}, "build.number": {"1"}}); err != nil {
		t.Fatalf("merge props: %v", err)
	}
}

func TestAQLBarePropertyInclude(t *testing.T) {
	h := newHarness(t)
	seedJFStack(t, h)

	t.Run("the verbatim jf query parses and answers", func(t *testing.T) {
		query := `items.find({"path":{"$ne":"."},"$or":[{"$and":[{"repo":"jf-local","path":"mybuild/1" ,"name":"artifact.zip"}]}]}).include("name","repo","path","actual_md5","actual_sha1","sha256","size","type","modified","created","property")`
		status, body := aqlDo(t, h, query, adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, "Unknown AQL field") {
			t.Fatalf("the D11 rejection survived:\n%s", body)
		}
		if !strings.Contains(body, "\"properties\" : [ {\n    \"key\" : \"build.name\",\n    \"value\" : \"mybuild\"\n  },{\n    \"key\" : \"build.number\",\n    \"value\" : \"1\"\n  } ]") {
			t.Fatalf("properties member wrong\nbody: %s", body)
		}
		// The row carries the ten named item fields (spot anchors).
		for _, anchor := range []string{`"name" : "artifact.zip"`, `"path" : "mybuild/1"`, `"type" : "file"`, `"size" : 9`} {
			if !strings.Contains(body, anchor) {
				t.Fatalf("missing anchor %q\nbody: %s", anchor, body)
			}
		}
	})

	t.Run("property-less rows omit the whole key", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"jf-local","path":"mybuild/1","name":"plain.bin"}).include("name","property")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, "properties") {
			t.Fatalf("property-less row must omit the key (§16.1-4)\nbody: %s", body)
		}
		if !strings.Contains(body, `"name" : "plain.bin"`) {
			t.Fatalf("plain row missing\nbody: %s", body)
		}
	})

	t.Run("the @key catch-all omits the key on property-less rows (diff L5)", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"jf-local","name":"plain.bin"}).include("name","@*")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, "properties") {
			t.Fatalf("@* on a property-less row must OMIT the key\nbody: %s", body)
		}
	})
}

func TestAQLPathNeDotExcludesRootRows(t *testing.T) {
	h := newHarness(t)
	seedJFStack(t, h)

	t.Run("path $ne dot drops the root-level file", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"jf-local","path":{"$ne":"."}}).include("path","name")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if strings.Contains(body, "root.bin") {
			t.Fatalf("root-level row survived path $ne \".\"\nbody: %s", body)
		}
		if !strings.Contains(body, `"name" : "artifact.zip"`) {
			t.Fatalf("nested rows must survive the exclusion\nbody: %s", body)
		}
	})

	t.Run("a root file's path value is the literal dot", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"jf-local","name":"root.bin"}).include("path","name")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if !strings.Contains(body, `"path" : "."`) {
			t.Fatalf("root parent must echo the literal dot (§16.1-1)\nbody: %s", body)
		}
	})

	t.Run("path eq dot matches only root-level rows", func(t *testing.T) {
		status, body := aqlDo(t, h,
			`items.find({"repo":"jf-local","path":"."}).include("name")`,
			adminUser, adminPass, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200\nbody: %s", status, body)
		}
		if !strings.Contains(body, `"name" : "root.bin"`) || strings.Contains(body, "artifact.zip") {
			t.Fatalf("path \".\" must match only the root row\nbody: %s", body)
		}
	})
}
