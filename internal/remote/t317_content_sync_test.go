package remote

// T-317 (FR-101.1) — the smart remote pair's BEHAVIOR half at the fetcher:
//
//	enableTokenAuthentication     the upstream credential rides as
//	                              "Authorization: Bearer <password>"
//	                              (repo-semantics 7.1 "切 token 头") instead of
//	                              the Basic pair.
//	contentSynchronisation        enabled && propertiesEnabled attaches the
//	                              upstream instance's node properties to the
//	                              cached node ({mount}/api/storage/{repo}/{path}
//	                              ?properties=, derived from the repository
//	                              URL's repo-key segment) — content-class nodes
//	                              only, best-effort end to end.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// t317MetaProvider is the synthetic all-metadata protocol of the gate test
// (see the subtest comment).
type t317MetaProvider struct{}

func (t317MetaProvider) Protocol() string                     { return "t317meta" }
func (t317MetaProvider) Classify(string) adapter.MetadataKind { return adapter.KindMetadata }
func (t317MetaProvider) PackageName(string) (string, bool)    { return "", false }
func (t317MetaProvider) Versions() adapter.VersionComparator  { return nil }

// t317Upstream is a recording fake BinFlow-shaped upstream: fixed bodies per
// path, an optional bearer demand, a property-API arm keyed by request
// path, and the full request-path log the attach assertions count. base is
// the REPO-SCOPED URL ({listener}/up-repo) once wired — the BinFlow/
// Artifactory shape whose last path segment IS the upstream repo key.
type t317Upstream struct {
	mu      sync.Mutex
	base    string            // repo-scoped base URL, set by wire
	bearer  string            // "" = anonymous upstream; else required token
	bodies  map[string]string // request path -> body served
	propFor map[string]string // property-API request path -> JSON body
	paths   []string          // every request path, in order
}

func (u *t317Upstream) handler(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.paths = append(u.paths, r.URL.Path)
	if u.bearer != "" && r.Header.Get("Authorization") != "Bearer "+u.bearer {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if body, ok := u.propFor[r.URL.Path]; ok && r.URL.RawQuery != "" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
		return
	}
	if body, ok := u.bodies[r.URL.Path]; ok {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(body))
		return
	}
	http.NotFound(w, r)
}

// wire mounts the upstream on a fresh listener and stamps its repo-scoped
// base URL — the REAL BinFlow shape {listener}/binflow/{repo}, whose
// derivation is exactly what the attach must reproduce: property face at
// {listener}/binflow/api/storage/{repo}/{path}?properties=.
func (u *t317Upstream) wire(t *testing.T) *t317Upstream {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(u.handler))
	t.Cleanup(srv.Close)
	u.base = srv.URL + "/binflow/up-repo"
	return u
}

func (u *t317Upstream) requests() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.paths...)
}

// t317ConfigJSON renders the canonical remote config JSON with the T-317
// policy fields the test needs (extra is a raw JSON member list, "" none).
func t317ConfigJSON(base, extra string) string {
	cfg := `{"url":"` + base + `","username":"","retrievalCachePeriodSecs":7200,` +
		`"missedRetrievalCachePeriodSecs":1800,"socketTimeoutSecs":15,` +
		`"assumedOfflinePeriodSecs":300,"hardFail":false,` +
		`"allowPrivateUpstream":true,"priorityResolution":false`
	if extra != "" {
		cfg += "," + extra
	}
	return cfg + "}"
}

// setCredential rewrites the live remote_configs credential pair.
func (e *fetchEnv) setCredential(t *testing.T, username, password string) {
	t.Helper()
	cfg, err := e.md.Remote().GetConfig(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	cfg.Username, cfg.Password = username, password
	if err := e.md.Remote().UpdateConfig(context.Background(), cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
}

// unfoundFetch reports whether the fetch error is the unfound family (the
// upstream's 401 answer mapped through mapUpstreamStatus).
func unfoundFetch(err error) bool {
	var fe *FetchError
	return errors.As(err, &fe) && fe.Unfound
}

// TestT317EnableTokenAuthenticationHeader: with the flag on, the password
// rides as a Bearer token (a bearer-demanding upstream serves the fetch);
// with the flag off the same credential stays Basic and the same upstream
// refuses it — the flag is the discriminator, not the credential.
func TestT317EnableTokenAuthenticationHeader(t *testing.T) {
	up := (&t317Upstream{
		bearer: "tok-777",
		bodies: map[string]string{"/binflow/up-repo/a.bin": "token-authed bytes"},
	}).wire(t)
	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.URL = up.base
		row.Config = t317ConfigJSON(up.base, `"enableTokenAuthentication":true`)
	})
	e.setCredential(t, "", "tok-777")
	res := mustFetch(t, e, "a.bin")
	if got := readAll(t, res); got != "token-authed bytes" {
		t.Fatalf("body = %q, want the upstream payload", got)
	}

	// Contrast arm: same credential, Basic spelling — the bearer-demanding
	// upstream must refuse it, proving the header really switched.
	up2 := (&t317Upstream{bearer: "tok-777", bodies: map[string]string{"/binflow/up-repo/a.bin": "x"}}).wire(t)
	e2 := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.URL = up2.base
		row.Config = t317ConfigJSON(up2.base, "") // enableTokenAuthentication absent
	})
	e2.setCredential(t, "admin", "tok-777")
	if _, err := e2.eng.Fetch(context.Background(), "generic-remote", "a.bin"); err == nil {
		t.Fatal("Basic credential passed a bearer-only upstream; the flag did not discriminate")
	} else if !unfoundFetch(err) {
		t.Fatalf("Basic arm error = %v, want the 401 unfound class", err)
	}
}

// TestT317ContentSyncPropertiesAttach: enabled && propertiesEnabled queries
// the upstream's property face for every CONTENT-class landing and merges
// what it serves onto the cached node — the full derivation (repo-key
// segment → {mount}/api/storage/{repo}/{path}?properties=) and the merge
// result both asserted.
func TestT317ContentSyncPropertiesAttach(t *testing.T) {
	up := (&t317Upstream{
		bodies: map[string]string{"/binflow/up-repo/org/app-1.0.bin": "payload-with-props"},
		propFor: map[string]string{
			"/binflow/api/storage/up-repo/org/app-1.0.bin": `{"properties":{"build":["77"],"team":["core","edge"]}}`,
		},
	}).wire(t)
	e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
		cfg.URL = up.base
		row.Config = t317ConfigJSON(up.base, `"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}`)
	})
	mustFetch(t, e, "org/app-1.0.bin")

	props, err := e.md.NodeProps().List(context.Background(), "generic-remote", "org/app-1.0.bin")
	if err != nil {
		t.Fatalf("NodeProps List: %v", err)
	}
	if len(props) != 2 || props["build"][0] != "77" || len(props["team"]) != 2 {
		t.Fatalf("cached node properties = %v, want build=77 and team=core,edge", props)
	}
	// The wire shape: content fetch, then exactly one property query.
	reqs := up.requests()
	if len(reqs) != 2 || reqs[1] != "/binflow/api/storage/up-repo/org/app-1.0.bin" {
		t.Fatalf("upstream requests = %v, want content fetch + property query", reqs)
	}
}

// TestT317ContentSyncGates: the property query fires ONLY under
// enabled&&propertiesEnabled, ONLY for content-class nodes, and NEVER for a
// repository URL without a repo-key segment (nothing derivable — skipped);
// a down property face never breaks the artifact fetch.
func TestT317ContentSyncGates(t *testing.T) {
	t.Run("policy absent: no property query", func(t *testing.T) {
		up := (&t317Upstream{
			bodies:  map[string]string{"/binflow/up-repo/a.bin": "x"},
			propFor: map[string]string{"/binflow/api/storage/up-repo/a.bin": `{"properties":{"build":["77"]}}`},
		}).wire(t)
		e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = up.base
			row.Config = t317ConfigJSON(up.base, "")
		})
		mustFetch(t, e, "a.bin")
		if reqs := up.requests(); len(reqs) != 1 {
			t.Fatalf("upstream requests = %v, want the content fetch only", reqs)
		}
	})
	t.Run("enabled without propertiesEnabled: no property query", func(t *testing.T) {
		up := (&t317Upstream{
			bodies:  map[string]string{"/binflow/up-repo/a.bin": "x"},
			propFor: map[string]string{"/binflow/api/storage/up-repo/a.bin": `{"properties":{"build":["77"]}}`},
		}).wire(t)
		e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = up.base
			row.Config = t317ConfigJSON(up.base, `"contentSynchronisation":{"enabled":true}`)
		})
		mustFetch(t, e, "a.bin")
		if reqs := up.requests(); len(reqs) != 1 {
			t.Fatalf("upstream requests = %v, want the content fetch only", reqs)
		}
	})
	t.Run("metadata-class node: no property query", func(t *testing.T) {
		up := (&t317Upstream{
			bodies: map[string]string{"/binflow/up-repo/index.xml": `<index/>`},
		}).wire(t)
		e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = up.base
			row.Config = t317ConfigJSON(up.base, "")
		})
		// A synthetic protocol whose every path is regenerable METADATA
		// (the class the gate keys on — the real maven provider is the
		// production instance of this shape; importing it here would cycle
		// remote→maven→repo→remote). The attach must skip it even with the
		// policy on.
		if _, ok := adapter.ForProtocol("t317meta"); !ok {
			adapter.RegisterMetadata(t317MetaProvider{})
		}
		e.createRemote(t, "meta-remote", "t317meta", func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = up.base
			row.Config = t317ConfigJSON(up.base, `"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}`)
		})
		if _, err := e.eng.Fetch(context.Background(), "meta-remote", "index.xml"); err != nil {
			t.Fatalf("metadata-class fetch: %v", err)
		}
		if reqs := up.requests(); len(reqs) != 1 {
			t.Fatalf("upstream requests = %v, want the metadata fetch only", reqs)
		}
	})
	t.Run("property face down: fetch still lands", func(t *testing.T) {
		up := (&t317Upstream{bodies: map[string]string{"/binflow/up-repo/a.bin": "resilient"}}).wire(t)
		e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = up.base
			row.Config = t317ConfigJSON(up.base, `"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}`)
		})
		// The derived property path answers 404 (no propFor entry): the
		// attach logs and the artifact fetch succeeds anyway.
		res := mustFetch(t, e, "a.bin")
		if got := readAll(t, res); got != "resilient" {
			t.Fatalf("body = %q, want the artifact bytes", got)
		}
		props, err := e.md.NodeProps().List(context.Background(), "generic-remote", "a.bin")
		if err != nil {
			t.Fatalf("NodeProps List: %v", err)
		}
		if len(props) != 0 {
			t.Fatalf("properties = %v, want none attached", props)
		}
	})
	t.Run("bare-host URL: nothing derivable, fetch lands", func(t *testing.T) {
		up := &t317Upstream{bodies: map[string]string{"/a.bin": "bare"}}
		srv := httptest.NewServer(http.HandlerFunc(up.handler))
		t.Cleanup(srv.Close)
		e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
			cfg.URL = srv.URL // no repo segment
			row.Config = t317ConfigJSON(srv.URL, `"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}`)
		})
		res := mustFetch(t, e, "a.bin")
		if got := readAll(t, res); got != "bare" {
			t.Fatalf("body = %q, want the artifact bytes", got)
		}
		props, err := e.md.NodeProps().List(context.Background(), "generic-remote", "a.bin")
		if err != nil {
			t.Fatalf("NodeProps List: %v", err)
		}
		if len(props) != 0 {
			t.Fatalf("properties = %v, want none attached", props)
		}
	})
}

// TestT317PropertyBodyShapes: the attach tolerates the shapes a real
// upstream answers with — the wrapper form ({"properties":{…}}) attaches;
// an empty set and a malformed body both land the artifact with no props.
func TestT317PropertyBodyShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int // attached key count
	}{
		{"wrapper form", `{"properties":{"build":["77"]}}`, 1},
		{"empty set", `{"properties":{}}`, 0},
		{"malformed body", `not-json`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := (&t317Upstream{
				bodies:  map[string]string{"/binflow/up-repo/a.bin": "x"},
				propFor: map[string]string{"/binflow/api/storage/up-repo/a.bin": tt.body},
			}).wire(t)
			e := newFetchEnv(t, func(row *metadata.Repo, cfg *metadata.RemoteConfig) {
				cfg.URL = up.base
				row.Config = t317ConfigJSON(up.base, `"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}`)
			})
			mustFetch(t, e, "a.bin")
			props, err := e.md.NodeProps().List(context.Background(), "generic-remote", "a.bin")
			if err != nil {
				t.Fatalf("NodeProps List: %v", err)
			}
			if len(props) != tt.want {
				t.Fatalf("properties = %v, want %d keys", props, tt.want)
			}
		})
	}
}
