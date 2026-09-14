package maven

// The pom prerequisite over the WIRE flow (docs/reverse/
// maven-metadata-pom-prerequisite section 1.1/1.2, L019): a jar-only
// SNAPSHOT directory never gains a version document, the first pom's
// deploy generates it synchronously, the last pom's delete removes it —
// and the module (version-group) document tracks pom-bearing children the
// same way. These three arms are the differential re-verification legs
// (jar-only / pom delete / pom arrival).

import (
	"net/http"
	"strings"
	"testing"
)

func TestPomPrerequisiteFlow(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	const (
		dir   = "/maven-local/com/acme/demo-app/1.2.0-SNAPSHOT/"
		tsJar = "demo-app-1.2.0-20260819.162439-1.jar"
		tsPom = "demo-app-1.2.0-20260819.162439-1.pom"
	)

	// Arm 1 — jar-only: the deploy succeeds, the version document never
	// appears (404 steady state, no timeout window), and the module
	// document stays absent with it (no pom-bearing child).
	if resp := hs.serve(http.MethodPut, dir+tsJar, []byte("jar-bytes"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("jar PUT = %d, want 201", resp.StatusCode)
	}
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusNotFound {
		t.Fatalf("jar-only version document = %d, want the steady 404", status)
	}
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusNotFound {
		t.Fatalf("jar-only module document = %d, want 404", status)
	}

	// Arm 2 — pom arrival: the version document exists by the time the
	// deploy response returns (the pom's synchronous recalc), the module
	// document lists the child, and both jar and pom ride the same
	// buildNumber's timestamp.
	if resp := hs.serve(http.MethodPut, dir+tsPom, []byte("pom-bytes"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom PUT = %d, want 201", resp.StatusCode)
	}
	status, body := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT")
	if status != http.StatusOK || !strings.Contains(body, "<buildNumber>1</buildNumber>") ||
		!strings.Contains(body, "<extension>jar</extension>") || !strings.Contains(body, "<extension>pom</extension>") {
		t.Fatalf("post-pom version document = (%d, %s), want buildNumber 1 with jar+pom entries", status, body)
	}
	// The pom's module recalculation is asynchronous (ME-04's taxonomy).
	hs.waitCalc()
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusOK ||
		!strings.Contains(body, "<version>1.2.0-SNAPSHOT</version>") {
		t.Fatalf("post-pom module document = (%d, %s), want the snapshot version listed", status, body)
	}

	// Arm 3 — last pom deleted (jar stays): the recompute trigger removes
	// the version document with its sidecars, the module document loses
	// its only pom-bearing child, and the jar itself is untouched.
	if resp := hs.serve(http.MethodDelete, dir+tsPom, nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("pom DELETE = %d, want 204", resp.StatusCode)
	}
	hs.waitCalc()
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusNotFound {
		t.Fatalf("post-delete version document = %d, want the removed 404", status)
	}
	if resp := hs.serve(http.MethodGet, dir+"maven-metadata.xml.sha1", nil, nil, true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("metadata sidecar survived the pom delete (status %d)", resp.StatusCode)
	}
	if status, _ := hs.getMeta("maven-local", "com.acme", "demo-app", ""); status != http.StatusNotFound {
		t.Fatalf("post-delete module document = %d, want 404 (no pom-bearing child)", status)
	}
	if resp := hs.serve(http.MethodGet, dir+tsJar, nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("jar after pom delete = %d, want the file untouched", resp.StatusCode)
	}
}

// TestPomPrerequisiteSeededDocument pins the recompute-triggered removal
// against a document that predates the ruling: a version document already
// on storage with only jars left in the directory is deleted, not served.
func TestPomPrerequisiteSeededDocument(t *testing.T) {
	hs := newHarness(t)
	defer hs.waitCalc()

	hs.seedNode("maven-local", "com/acme/demo-app/1.2.0-SNAPSHOT/maven-metadata.xml",
		[]byte("<metadata><versioning><snapshot><timestamp>20260101.000000</timestamp><buildNumber>7</buildNumber></snapshot></versioning></metadata>"))
	hs.seedNode("maven-local", "com/acme/demo-app/1.2.0-SNAPSHOT/demo-app-1.2.0-20260819.162439-1.jar", []byte("j"))
	hs.recalc("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT")
	if status, body := hs.getMeta("maven-local", "com.acme", "demo-app", "1.2.0-SNAPSHOT"); status != http.StatusNotFound {
		t.Fatalf("seeded document survived a jar-only recompute = (%d, %s), want 404", status, body)
	}
}
