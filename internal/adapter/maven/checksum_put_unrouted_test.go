// T-587 / BIN-69 (ledger maven/checksum-put-unrouted-virtual-plane): the
// sidecar intercept arm passes the deploy routing gate — an unrouted
// virtual answers the C5 405 A form for every checksum-suffix write
// (L040 Arm 1a mv-unrouted-*, A 7.161.26: sidecar included, zero side
// effects), while a ROUTED virtual's intercept penetrates to the
// deployment-target member (Arm 1b mv2-*: miss 404's repo segment = member
// key, 201 Location = member source, SET registration and the GET echo
// through the virtual read plane — the generic plane's C1 model, T-583).
package maven

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedRoutedVirtual creates a virtual whose write route targets memberKey.
func seedRoutedVirtual(t *testing.T, hs *harness, virtualKey, memberKey string) {
	t.Helper()
	if _, err := hs.svc.CreateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey:     virtualKey,
		Type:        repo.TypeVirtual,
		PackageType: Protocol,
		Config:      `{"repositories":["` + memberKey + `"],"defaultDeploymentRepo":"` + memberKey + `"}`,
	}); err != nil {
		t.Fatalf("seed routed virtual %s: %v", virtualKey, err)
	}
}

// TestChecksumPutUnroutedVirtualRefusal pins the deploy routing gate: a
// virtual repository with NO defaultDeploymentRepo answers the C5 405's A
// form for the whole {.sha1,.md5,.sha256} family — value-ok, source-miss
// and value-wrong legs alike (A: the terminal suffix buys no exemption) —
// byte-identical to the plain PUT face's refusal, with zero side effects
// on the member that serves the virtual's reads.
func TestChecksumPutUnroutedVirtualRefusal(t *testing.T) {
	hs := newHarness(t)
	// The member holds the artifact the virtual's read plane resolves; the
	// declared values are chosen WRONG where a probe would 409 and correct
	// where it would 201, so any non-405 answer is the old bypass showing.
	// The seed declares NO header digests, so any client column seen after
	// the family ran is a side effect of a refused PUT.
	jar := "com/acme/t587/1.0.0/t587-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed member jar = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	wrong := strings.Repeat("0", 40)

	// The plain PUT control: its 405 body is the family's expected shape.
	plain := hs.serve(http.MethodPut, "/maven-virtual/com/acme/t587/2.0.0/t587-2.0.0.jar", jarBytes, nil, true)
	plainBody := string(drain(t, plain))
	if plain.StatusCode != http.StatusMethodNotAllowed ||
		!strings.Contains(plainBody, unroutedVirtualWriteMessage("maven-virtual")) {
		t.Fatalf("plain PUT control = %d %q, want the C5 405 verbatim", plain.StatusCode, plainBody)
	}

	legs := []struct {
		name string
		path string
		body string
	}{
		{"sha1-value-ok", jar + ".sha1", s1},
		{"sha1-source-miss", "com/acme/t587/9.9.9/t587-9.9.9.jar.sha1", s1},
		{"md5-value-wrong", jar + ".md5", wrong},
		{"sha256-value-ok", jar + ".sha256", strings.Repeat("ab", 32)},
	}
	for _, lg := range legs {
		resp := hs.serve(http.MethodPut, "/maven-virtual/"+lg.path, []byte(lg.body), nil, true)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: PUT = %d (body=%s), want 405", lg.name, resp.StatusCode, drain(t, resp))
			continue
		}
		if got := string(drain(t, resp)); !strings.Contains(got, unroutedVirtualWriteMessage("maven-virtual")) {
			t.Errorf("%s: 405 body = %q, want the C5 wording verbatim", lg.name, got)
		}
		if allow := resp.Header.Get("Allow"); allow != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET (the service refusal's own header)", lg.name, allow)
		}
	}

	// Zero side effects (L040 Arm 1a's refusal face): the member's node
	// carries no client declaration, and the member's own GET face serves
	// the computed digests — the wrong md5 never wrote through.
	node, err := hs.md.Nodes().Get(context.Background(), "maven-local", jar)
	if err != nil {
		t.Fatalf("member node: %v", err)
	}
	if node.ClientSha1 != "" || node.ClientMd5 != "" || node.ClientSha256 != "" {
		t.Errorf("member client columns after the 405 family = sha1:%q md5:%q sha256:%q, want all empty",
			node.ClientSha1, node.ClientMd5, node.ClientSha256)
	}
	for _, algo := range []string{"sha1", "md5"} {
		want := map[string]string{"sha1": s1, "md5": digests2(jarBytes)}[algo]
		got := string(drain(t, hs.serve(http.MethodGet, "/maven-local/"+jar+"."+algo, nil, nil, true)))
		if got != want {
			t.Errorf("member GET .%s after the 405 family = %q, want the computed %q", algo, got, want)
		}
	}
}

// TestChecksumPutRoutedVirtualPenetration pins the routed face: every
// sidecar action — the existence probe, the SET, the miss 404's repo
// segment and the 201 Location — names the deployment-target member, and
// the virtual GET face echoes the registered client value (the write-
// through included), an unset algorithm keeping the computed fallback.
func TestChecksumPutRoutedVirtualPenetration(t *testing.T) {
	hs := newHarness(t)
	seedRoutedVirtual(t, hs, "t587-virt", "maven-local")
	jar := "com/acme/t587r/1.0.0/t587r-1.0.0.jar"
	if resp := hs.deployJar("maven-local", jar, jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed member jar = %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	wrong := strings.Repeat("0", 32)

	// Miss: the 404's repo segment is the MEMBER key (the intercept
	// penetrates the virtual), never the virtual's own.
	miss := hs.serve(http.MethodPut, "/t587-virt/com/acme/t587r/9.9.9/t587r-9.9.9.jar.sha1",
		[]byte(s1), nil, true)
	if miss.StatusCode != http.StatusNotFound {
		t.Fatalf("routed miss = %d (body=%s), want 404", miss.StatusCode, drain(t, miss))
	}
	if got := string(drain(t, miss)); !strings.Contains(got, targetMissingMsg("maven-local", "com/acme/t587r/9.9.9/t587r-9.9.9.jar")) {
		t.Fatalf("routed miss 404 = %q, want the member-keyed wording %q",
			got, targetMissingMsg("maven-local", "com/acme/t587r/9.9.9/t587r-9.9.9.jar"))
	}
	if got := string(drain(t, miss)); strings.Contains(got, "t587-virt:") {
		t.Fatalf("routed miss 404 cites the virtual key: %q", got)
	}

	// Value ok: 201 with the MEMBER source in Location (A's
	// Location-to-member face; the local control below keeps its own key).
	ok := hs.serve(http.MethodPut, "/t587-virt/"+jar+".sha1", []byte(s1), nil, true)
	if ok.StatusCode != http.StatusCreated {
		t.Fatalf("routed value-ok = %d (body=%s), want 201", ok.StatusCode, drain(t, ok))
	}
	if loc := ok.Header.Get("Location"); !strings.HasSuffix(loc, "/maven-local/"+jar) {
		t.Fatalf("routed 201 Location = %q, want the member source path", loc)
	}

	// Value wrong: the 409 keeps the family's repo-less relPath wording and
	// the wrong value WRITES THROUGH — the member's column first, then the
	// VIRTUAL GET face echoes it (the overlay resolved through the virtual
	// read plane).
	bad := hs.serve(http.MethodPut, "/t587-virt/"+jar+".md5", []byte(wrong), nil, true)
	if bad.StatusCode != http.StatusConflict {
		t.Fatalf("routed value-wrong = %d (body=%s), want 409", bad.StatusCode, drain(t, bad))
	}
	want409 := "Checksum error for '" + jar + ".md5': received '" + wrong + "' but actual is '" + digests2(jarBytes) + "'"
	if got := string(drain(t, bad)); !strings.Contains(got, want409) {
		t.Fatalf("routed 409 body = %s, want fragment %q", got, want409)
	}
	node, err := hs.md.Nodes().Get(context.Background(), "maven-local", jar)
	if err != nil {
		t.Fatalf("member node: %v", err)
	}
	if node.ClientMd5 != wrong {
		t.Fatalf("member ClientMd5 after the routed 409 = %q, want the written-through %q", node.ClientMd5, wrong)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/t587-virt/"+jar+".md5", nil, nil, true))); got != wrong {
		t.Fatalf("virtual GET .md5 after write-through = %q, want %q", got, wrong)
	}
	if got := string(drain(t, hs.serve(http.MethodGet, "/t587-virt/"+jar+".sha1", nil, nil, true))); got != s1 {
		t.Fatalf("virtual GET .sha1 after registration = %q, want the client value %q", got, s1)
	}

	// An unset algorithm through the virtual keeps the computed fallback
	// (maven's own posture — the C2 on-demand family's ruling untouched).
	if got := string(drain(t, hs.serve(http.MethodGet, "/t587-virt/"+jar+".sha256", nil, nil, true))); got == "" {
		t.Fatal("virtual GET .sha256 (never registered) = empty, want the computed fallback")
	}

	// Local control: the same faces on the member itself keep the local
	// spelling — the miss cites the addressed key, the 201 Location stays
	// the local path.
	lmiss := hs.serve(http.MethodPut, "/maven-local/com/acme/t587r/8.8.8/t587r-8.8.8.jar.md5",
		[]byte(wrong), nil, true)
	if got := string(drain(t, lmiss)); lmiss.StatusCode != http.StatusNotFound ||
		!strings.Contains(got, targetMissingMsg("maven-local", "com/acme/t587r/8.8.8/t587r-8.8.8.jar")) {
		t.Fatalf("local miss control = %d %q, want the local-keyed 404", lmiss.StatusCode, got)
	}
	lok := hs.serve(http.MethodPut, "/maven-local/"+jar+".md5", []byte(digests2(jarBytes)), nil, true)
	if lok.StatusCode != http.StatusCreated {
		t.Fatalf("local value-ok control = %d, want 201", lok.StatusCode)
	}
	if loc := lok.Header.Get("Location"); !strings.HasSuffix(loc, "/maven-local/"+jar) {
		t.Fatalf("local 201 Location = %q, want the local source path", loc)
	}
}
