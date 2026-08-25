package maven

import (
	"context"
	"net/http"
	"testing"
)

// The maven plane's matrix-parameter wiring (M10 T-286, architecture
// section 15.3.1): the peel runs inside the shared Layout chain, so the
// maven PUT family carries deploy properties exactly like the generic
// plane, and the legacy non-paired ';' names (the seed-m10 maven fixture
// among them) keep their literal reachability through Parse's layout
// validation.
func TestMavenMatrixPropsDeploy(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	// A layout-compliant artifact path with a paired suffix: the artifact
	// lands at the clean path and the properties ride the deploy.
	resp := hs.serve(http.MethodPut, "/maven-local/com/example/app/1.0/app-1.0.jar;build=77;env=prod",
		[]byte("jar-bytes"), nil, true)
	if httpStatus(resp) != http.StatusCreated {
		t.Fatalf("matrix PUT status = %d; body=%s", httpStatus(resp), string(drain(t, resp)))
	}

	props, err := hs.md.NodeProps().List(ctx, "maven-local", "com/example/app/1.0/app-1.0.jar")
	if err != nil {
		t.Fatalf("list props: %v", err)
	}
	if len(props) != 2 || props["build"][0] != "77" || props["env"][0] != "prod" {
		t.Fatalf("props = %+v", props)
	}

	// The clean path serves; the suffixed spelling re-peels to the same node.
	get := hs.serve(http.MethodGet, "/maven-local/com/example/app/1.0/app-1.0.jar", nil, nil, true)
	if httpStatus(get) != http.StatusOK || string(drain(t, get)) != "jar-bytes" {
		t.Fatalf("clean GET = %d %q", httpStatus(get), string(drain(t, get)))
	}
	suffixed := hs.serve(http.MethodGet, "/maven-local/com/example/app/1.0/app-1.0.jar;build=77", nil, nil, true)
	if httpStatus(suffixed) != http.StatusOK {
		t.Fatalf("suffixed GET = %d", httpStatus(suffixed))
	}

	// The illegal key answers the 400 before anything lands. httptest's
	// request builder cannot carry a raw space in the target, so the
	// refusal is exercised through the percent-encoded spelling — Layout
	// decodes before the peel, the same "bad key" reaches the validator.
	bad := hs.serve(http.MethodPut, "/maven-local/com/example/app/1.0/bad-1.0.jar;bad%20key=1",
		[]byte("x"), nil, true)
	if httpStatus(bad) != http.StatusBadRequest {
		t.Fatalf("illegal key status = %d", httpStatus(bad))
	}
}

func TestMavenLegacySemicolonLiteral(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	// The seed-m10 maven fixture shape: non-paired ';' in group and
	// artifactId segments stays a literal layout-compliant path.
	const fixture = "com/acme;lib/1.0/acme;lib-1.0.jar"
	resp := hs.serve(http.MethodPut, "/maven-local/"+fixture, []byte("legacy-bytes"), nil, true)
	if httpStatus(resp) != http.StatusCreated {
		t.Fatalf("legacy PUT status = %d; body=%s", httpStatus(resp), string(drain(t, resp)))
	}
	get := hs.serve(http.MethodGet, "/maven-local/"+fixture, nil, nil, true)
	if httpStatus(get) != http.StatusOK || string(drain(t, get)) != "legacy-bytes" {
		t.Fatalf("legacy GET = %d %q", httpStatus(get), string(drain(t, get)))
	}
	// No properties materialized on the literal node.
	props, err := hs.md.NodeProps().List(ctx, "maven-local", fixture)
	if err != nil || len(props) != 0 {
		t.Fatalf("legacy node carries props: %+v err=%v", props, err)
	}
}

// httpStatus is the *http.Response spelling of the recorder-style checks
// (the harness hands back real responses).
func httpStatus(resp *http.Response) int { return resp.StatusCode }
