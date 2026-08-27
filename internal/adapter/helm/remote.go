package helm

// The REMOTE repository's protocol faces (helm.md section 6 / S10). The
// index.yaml and the path-aligned chart/prov downloads ride the shared
// FR-20 pull-through engine verbatim (svc.Get — cache, dual TTL, negative
// cache, stale downgrade; the provider's Classify already splits the
// repo-root index as regenerable metadata); the two faces owned HERE are
// the ones the engine's path-joined fetch cannot express:
//
//	_external/<protocol>/<url...>   the absolute-URL dependency proxy:
//	                                 fetch <protocol>://<url...> through
//	                                 the guarded outbound client
//	_transitive/<protocol>/<url...> the upstream's OWN _external face —
//	                                 repoURL + "_external/<protocol>/<url>"
//	                                 is a plain path-joined fetch, so it
//	                                 rides the engine (and caches).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
)

// RemoteConfigs is the remote_configs read seam (the member upstream URL
// the rewriting recognizes and the _external egress policy). Satisfied by
// metadata.Store.Remote() directly; nil degrades: remote members rewrite
// their absolute URLs through the external branch only, and _external
// egress runs on the client defaults.
type RemoteConfigs interface {
	GetConfig(ctx context.Context, repoKey string) (*metadata.RemoteConfig, error)
}

// msgNotExternalDependency is the pinned allow-list refusal (helm.md
// section 6, verbatim).
const msgNotExternalDependency = "Could not download HELM package at %s - URL is not configured as an external dependency"

// extClientPool caches one guarded outbound client per repository, keyed
// on the egress-relevant config (the engine's own clientFor posture in
// miniature — no credentials ever ride this face, so the signature is the
// exemption flag and the timeout).
type extClientPool struct {
	mu      sync.Mutex
	clients map[string]*extClientEntry
}

// extClientEntry is one cached client plus the signature it was built from.
type extClientEntry struct {
	sig    string
	client *remote.Client
}

// clientFor returns the repository's external-egress client, rebuilding on
// a signature change. allowPrivateUpstream is the repository's admin-set
// exemption (the same flag the pull-through egress honors); timeoutMs 0
// keeps the client default.
func (p *extClientPool) clientFor(repoKey string, allowPrivateUpstream bool, timeoutMs int64) (*remote.Client, error) {
	sig := fmt.Sprintf("%t\x00%d", allowPrivateUpstream, timeoutMs)
	p.mu.Lock()
	cached := p.clients[repoKey]
	p.mu.Unlock()
	if cached != nil && cached.sig == sig {
		return cached.client, nil
	}
	client, err := remote.NewClient(remote.Options{
		RepoKey:              repoKey + "-external",
		AllowPrivateUpstream: allowPrivateUpstream,
		SocketTimeout:        time.Duration(timeoutMs) * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("helm %s: external dependency egress client: %w", repoKey, err)
	}
	p.mu.Lock()
	if p.clients == nil {
		p.clients = map[string]*extClientEntry{}
	}
	old := p.clients[repoKey]
	p.clients[repoKey] = &extClientEntry{sig: sig, client: client}
	p.mu.Unlock()
	if old != nil {
		old.client.CloseIdleConnections()
	}
	return client, nil
}

// remoteEgressPolicy resolves one repository's external-egress policy off
// its remote_configs row (absent row / absent seam: the client defaults).
func (h *Handler) remoteEgressPolicy(ctx context.Context, repoKey string) (allowPrivate bool, timeoutMs int64) {
	if h.remotes == nil {
		return false, 0
	}
	cfg, err := h.remotes.GetConfig(ctx, repoKey)
	if err != nil || cfg == nil {
		return false, 0
	}
	return cfg.AllowPrivateUpstream, cfg.SocketTimeoutMs
}

// extClientFor builds (or reuses) the repository's external-egress client.
// The guard's system resolver runs; the client default timeout applies
// when the row carries none.
func (h *Handler) extClientFor(ctx context.Context, repoKey string) (*remote.Client, error) {
	allowPrivate, timeoutMs := h.remoteEgressPolicy(ctx, repoKey)
	return h.extClients.clientFor(repoKey, allowPrivate, timeoutMs)
}

// externalOutcome is one external fetch attempt's classified result.
type externalOutcome struct {
	served  bool         // the response was written
	miss    bool         // upstream unfound: the walk may continue
	status  int          // the written status when served
	writeFn func() error // deferred body streaming (called only when served)
}

// streamExternal fetches one absolute external URL through the guarded
// client and writes the classified response. The single-repository face
// (miss → 404) and the virtual member walk (miss → next member) share it.
func (h *Handler) streamExternal(ctx context.Context, w http.ResponseWriter, repoKey, target string) externalOutcome {
	client, err := h.extClientFor(ctx, repoKey)
	if err != nil {
		writeText(w, http.StatusInternalServerError, err.Error())
		return externalOutcome{served: true, status: http.StatusInternalServerError}
	}
	res, ferr := client.Stream(ctx, remote.Request{URL: target})
	if ferr != nil {
		var rej *remote.RejectionError
		if errors.As(ferr, &rej) {
			// The guard's screening refusal — the same 400 the engine's
			// chain answers with, never an offline mark.
			writeText(w, http.StatusBadRequest, fmt.Sprintf(
				"Cannot fetch external dependency '%s' for repository '%s': %v", target, repoKey, ferr))
			return externalOutcome{served: true, status: http.StatusBadRequest}
		}
		writeText(w, http.StatusBadGateway, fmt.Sprintf(
			"Failed to proxy external dependency '%s' for repository '%s': %v", target, repoKey, ferr))
		return externalOutcome{served: true, status: http.StatusBadGateway}
	}
	switch {
	case res.StatusCode == http.StatusNotFound,
		res.StatusCode == http.StatusUnauthorized,
		res.StatusCode == http.StatusForbidden:
		// The unfound family: nothing to serve (a walk continues, a bare
		// repository answers 404 below).
		drainExternal(res.Body)
		return externalOutcome{miss: true}
	case res.StatusCode != http.StatusOK:
		drainExternal(res.Body)
		writeText(w, http.StatusBadGateway, fmt.Sprintf(
			"Failed to proxy external dependency '%s' for repository '%s': upstream answered %s.",
			target, repoKey, res.Status))
		return externalOutcome{served: true, status: http.StatusBadGateway}
	}

	hdr := w.Header()
	if ct := res.Header.Get("Content-Type"); ct != "" {
		hdr.Set("Content-Type", ct)
	} else {
		hdr.Set("Content-Type", "application/octet-stream")
	}
	hdr.Set("X-Binflow-Upstream", target)
	if v := res.Header.Get("Content-Length"); v != "" {
		hdr.Set("Content-Length", v)
	}
	w.WriteHeader(http.StatusOK)
	out := externalOutcome{served: true, status: http.StatusOK}
	out.writeFn = func() error {
		defer func() { _ = res.Body.Close() }() //nolint:errcheck // streamed to the client
		_, cerr := io.Copy(w, res.Body)
		return cerr
	}
	return out
}

// drainExternal discards one unanswered upstream body (bounded) and closes
// it so the pooled connection survives.
func drainExternal(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 8<<10))
	_ = body.Close()
}

// serveRemoteExternal answers GET _external/<protocol>/<url...> on a
// remote repository (helm.md section 6): the folded URL re-inflates, the
// allow list gates it (the pinned 400 on a miss), the guarded client
// streams it. Nothing lands in the cache namespace — the engine's
// land() is the only cache writer, and absolute-URL fetches have no
// engine seam (registered as a deliberate scope line in the ticket
// report: correctness first, the landing rides a future engine facet).
func (h *Handler) serveRemoteExternal(ctx context.Context, w http.ResponseWriter, repoKey, rel string) {
	target, err := parseExternalPath(rel, segExternal)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	if !externalAllowed(h.externalPatterns(), target) {
		writeText(w, http.StatusBadRequest, fmt.Sprintf(msgNotExternalDependency, target))
		return
	}
	out := h.streamExternal(ctx, w, repoKey, target)
	if out.served && out.writeFn != nil {
		_ = out.writeFn()
		return
	}
	if out.miss {
		writeText(w, http.StatusNotFound, fmt.Sprintf(
			"Failed to find the external dependency '%s' for repository '%s'.", target, repoKey))
	}
}

// serveRemoteTransitive answers GET _transitive/<protocol>/<url...> on a
// remote repository: the upstream's OWN _external face is a path-joined
// upstream fetch (repoURL + _external/<protocol>/<url>), so the shared
// engine serves it — with the cache, TTL and stale downgrade the engine
// owns — under the storage path the wire spelling maps onto.
func (h *Handler) serveRemoteTransitive(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	fetchPath := transitiveFetchPath(rel)
	rc, node, err := h.svc.Get(ctx, p, repoKey, fetchPath)
	if err != nil {
		h.writeError(w, err, repoKey, fetchPath)
		return
	}
	h.serveNode(ctx, w, r, node, rc, nodeCType(node))
}

// transitiveFetchPath maps one _transitive/<...> wire path onto the
// storage/upstream path the fetch runs at: the upstream's _external face.
func transitiveFetchPath(rel string) string {
	return segExternal + strings.TrimPrefix(rel, segTransitive)
}

// nodeCType picks a stored node's content type with the generic fallback.
func nodeCType(node *metadata.Node) string {
	if node == nil || node.Mime == "" {
		return "application/octet-stream"
	}
	return node.Mime
}
