package replication

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// The npm push plane (T-195 D3). Semantic adjudication, recorded in the
// T-195 report: the npm client-plane restrictions that broke the generic
// addressing (T-175 D3: tarball PUT 405 "tarballs are read-only", raw
// packument PUT parsed as a dist-tag body 400) are PROTOCOL invariants —
// tarballs and packuments are only writable as a publish document pairing
// them, and BinFlow enforces that on every client. Replication therefore
// does NOT get a privileged raw-node face on the target (no security
// surface is added, no invariant is bypassed): the engine drives the SAME
// publish and dist-tag faces a real npm client drives, with the config's
// ordinary write credentials. The Q6 read-only virtual facade is untouched
// by this — the engine addresses the target's backing local repository,
// exactly like the generic plane.
//
// Convergence model: every npm task (tarball OR packument node) resolves
// its package name and syncs the whole package:
//
//  1. read the SOURCE packument node (the stored, protocol-neutral
//     document — dist.tarball holds repo-relative paths, versions hold
//     their dist.integrity/shasum);
//  2. GET the TARGET packument (404 = empty);
//  3. for every version the target lacks, publish it as a one-version
//     document with the tarball attached (the face re-verifies the
//     integrity and shasum the manifest declares, and merges the version
//     into the target's packument — no wholesale overwrite, versions
//     published by target-side users survive);
//  4. for every dist-tag that differs, PUT the single-tag face.
//
// npm state is fully node-resident (dist-tags live inside the packument),
// so this converges the package completely; the redundancy of running the
// same sync from both task kinds is the price of task-per-node granularity
// and is idempotent by construction.

// npm plane read caps: packuments are JSON documents (a package with
// thousands of versions stays in the single-digit MBs); tarballs ride the
// publish document inline as base64 (the npm protocol itself — the target's
// own publish cap is 512MB), so the memory bound matches the protocol's.
const (
	npmPackumentCap = 64 << 20
	npmTarballCap   = 256 << 20
)

// npmPackageName maps a repo-relative npm node path onto its package name:
// "<name>/packument.json" (the packument node) and "<name>/-/..." (any
// tarball path) both reduce to <name>; anything else is not the npm layout.
func npmPackageName(nodePath string) (string, error) {
	if strings.HasSuffix(nodePath, "/packument.json") {
		if name := strings.TrimSuffix(nodePath, "/packument.json"); name != "" && !strings.Contains(name, "/-/") {
			return name, nil
		}
	}
	if i := strings.Index(nodePath, "/-/"); i > 0 {
		return nodePath[:i], nil
	}
	return "", fmt.Errorf("npm node path %q: neither a packument nor a tarball layout", nodePath)
}

// npmDoc is a parsed packument document (numbers kept literal so unknown
// fields round-trip byte-stably — the same representation the npm adapter
// stores).
type npmDoc map[string]any

// parseNpmDoc decodes a packument body.
func parseNpmDoc(raw []byte) (npmDoc, error) {
	var doc npmDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse packument: %w", err)
	}
	return doc, nil
}

// versionsOf returns the doc's versions map (never nil).
func (d npmDoc) versionsOf() map[string]any {
	if m, ok := d["versions"].(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}

// distTagsOf returns the doc's dist-tags (never nil).
func (d npmDoc) distTagsOf() map[string]string {
	out := map[string]string{}
	if m, ok := d["dist-tags"].(map[string]any); ok {
		for k, v := range m {
			if s, ok := v.(string); ok && s != "" {
				out[k] = s
			}
		}
	}
	return out
}

// npmVersionTarball resolves the repo-relative tarball path of one version:
// the stored dist.tarball when it reduces to a path inside a BinFlow mount
// (the adapter's own resolution order), else the canonical
// <name>/-/<name>-<version>.tgz layout.
func npmVersionTarball(name, version string, manifest map[string]any) string {
	if dist, ok := manifest["dist"].(map[string]any); ok {
		if u, ok := dist["tarball"].(string); ok && u != "" {
			if rel, ok := npmRelativeTarball(u); ok {
				return rel
			}
		}
	}
	return name + "/-/" + name + "-" + version + ".tgz"
}

// npmRelativeTarball reduces an absolute tarball URL to the repo-relative
// path when it points at any BinFlow mount of the
// .../binflow/api/npm/<repo>/<rel> or .../binflow/<repo>/<rel> shape (the
// adapter's relativeTarballPath rule, restated locally: adapter packages
// share no unexported code).
func npmRelativeTarball(u string) (string, bool) {
	pu, err := url.Parse(u)
	if err != nil {
		return "", false
	}
	p := pu.Path
	for _, head := range []string{"/binflow/api/npm/", "/binflow/"} {
		if i := strings.Index(p, head); i >= 0 {
			rest := p[i+len(head):]
			if _, tail, found := strings.Cut(rest, "/"); found && tail != "" {
				if rel, err := url.PathUnescape(tail); err == nil {
					return rel, true
				}
			}
		}
	}
	return "", false
}

// npmURL builds a target content-plane address under
// {base}/binflow/{TargetRepo}/{segments...} (the /binflow/api/npm mount is
// a rewrite onto this same plane, so the direct address is the canonical
// one). Segments are escaped individually; dot/empty segments are refused.
func (e *Engine) npmURL(cfg *ReplicationConfig, nodePath string, segments ...string) (*url.URL, error) {
	return e.planeURL(cfg, nodePath, append([]string{cfg.TargetRepo}, segments...)...)
}

// pushNpm is the npm plane entry: reduce the node path to a package name
// and converge the package on the target.
func (e *Engine) pushNpm(ctx context.Context, cfg *ReplicationConfig, _, nodePath string) error {
	name, err := npmPackageName(nodePath)
	if err != nil {
		return notRetryable(err)
	}
	return e.syncNpmPackage(ctx, cfg, name)
}

// syncNpmPackage implements the convergence model described in the file
// comment. The task's own sha256 is deliberately not asserted against the
// packument node: the sync reads the source's CURRENT packument, so a task
// created before a later publish still converges the package to the newest
// state (and the later publish enqueues its own task anyway).
func (e *Engine) syncNpmPackage(ctx context.Context, cfg *ReplicationConfig, name string) error {
	if e.cfg.meta == nil {
		return notRetryable(fmt.Errorf("npm plane requires the metadata seam"))
	}
	// The packument read is deliberately RETRYABLE on a miss: the tarball
	// task fires from the publish's step-9 Put, BEFORE step 10 writes the
	// packument node — a tarball task that cannot yet see the packument
	// backs off, and by the time it retries (or the packument's own task
	// arrives) the node is there. (A missing TARBALL inside publishNpmVersion
	// stays terminal — the packument references it, so its absence is real
	// corruption.)
	node, err := e.cfg.meta.Node(ctx, cfg.SourceRepo, name+"/packument.json")
	if err != nil {
		if errors.Is(err, ErrMetaNotFound) {
			return fmt.Errorf("source packument %s/%s not visible yet (the publish's document write may still be in flight)", cfg.SourceRepo, name)
		}
		return err
	}
	srcRaw, err := e.readAllBounded(ctx, node.Sha256, npmPackumentCap)
	if err != nil {
		return err
	}
	srcDoc, err := parseNpmDoc(srcRaw)
	if err != nil {
		return notRetryable(fmt.Errorf("source packument %s/%s: %w", cfg.SourceRepo, name, err))
	}

	tgtDoc, err := e.fetchTargetPackument(ctx, cfg, name)
	if err != nil {
		return err
	}
	tgtVersions := tgtDoc.versionsOf()

	// Missing versions, sorted for deterministic task behavior.
	var missing []string
	for v := range srcDoc.versionsOf() {
		if _, ok := tgtVersions[v]; !ok {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	for _, v := range missing {
		if err := e.publishNpmVersion(ctx, cfg, name, v, srcDoc); err != nil {
			return err
		}
	}

	// Dist-tag convergence (after the versions exist — the tag face
	// validates that the referenced version is served).
	srcTags := srcDoc.distTagsOf()
	tgtTags := tgtDoc.distTagsOf()
	var drifted []string
	for tag, v := range srcTags {
		if tgtTags[tag] != v {
			drifted = append(drifted, tag)
		}
	}
	sort.Strings(drifted)
	for _, tag := range drifted {
		if err := e.putNpmDistTag(ctx, cfg, name, tag, srcTags[tag]); err != nil {
			return err
		}
	}
	return nil
}

// readAllBoundedMeta resolves a node path to its current blob through the
// metadata seam and reads it under cap (a vanished node is terminal).
func (e *Engine) readAllBoundedMeta(ctx context.Context, repoKey, path string, limit int64) ([]byte, error) {
	n, err := e.cfg.meta.Node(ctx, repoKey, path)
	if err != nil {
		return nil, err
	}
	return e.readAllBounded(ctx, n.Sha256, limit)
}

// fetchTargetPackument GETs the target's packument; a 404 answers a fresh
// empty document, any other non-200 is classified (retryable unless the
// plane says otherwise).
func (e *Engine) fetchTargetPackument(ctx context.Context, cfg *ReplicationConfig, name string) (npmDoc, error) {
	u, err := e.npmURL(cfg, name, strings.Split(name, "/")...)
	if err != nil {
		return nil, err
	}
	accept := http.Header{}
	accept.Set("Accept", "application/json")
	resp, err := e.do(ctx, http.MethodGet, u, cfg, nil, 0, accept)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, npmPackumentCap+1))
		_ = resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("read target packument: %w", rerr)
		}
		if int64(len(raw)) > npmPackumentCap {
			return nil, fmt.Errorf("target packument exceeds %d bytes", npmPackumentCap)
		}
		doc, perr := parseNpmDoc(raw)
		if perr != nil {
			// A target document we cannot parse cannot be diffed against:
			// deterministic for as long as it stands, so terminal.
			return nil, notRetryable(fmt.Errorf("target packument: %w", perr))
		}
		return doc, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return npmDoc{}, nil
	default:
		return nil, classifyNpm(resp, "packument fetch")
	}
}

// classifyNpm maps a non-2xx npm-face response onto a task error. The npm
// plane's deterministic refusals — 403 (duplicate version / cannot modify:
// the target already serves that version with different content, the Q7
// conflict equivalent) and 400 (the document itself is rejected: integrity
// disagreement, malformed) — are terminal; 401/404/5xx stay retryable.
func classifyNpm(resp *http.Response, what string) error {
	err := fmt.Errorf("%s: target answered %d %s", what, resp.StatusCode, strings.TrimSpace(readSnippet(resp)))
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
		return notRetryable(err)
	}
	return err
}

// publishNpmVersion composes and PUTs the one-version publish document for
// a version the target lacks.
func (e *Engine) publishNpmVersion(ctx context.Context, cfg *ReplicationConfig, name, version string, srcDoc npmDoc) error {
	manifest, ok := srcDoc.versionsOf()[version].(map[string]any)
	if !ok {
		// A version entry that is not an object cannot be published by any
		// face; deterministic.
		return notRetryable(fmt.Errorf("source version %s of %s is not a manifest object", version, name))
	}
	tarPath := npmVersionTarball(name, version, manifest)
	tarball, err := e.readAllBoundedMeta(ctx, cfg.SourceRepo, tarPath, npmTarballCap)
	if err != nil {
		return fmt.Errorf("tarball %s: %w", tarPath, err)
	}
	// The attachment key is the tarball FILENAME — everything after the
	// "/-/" marker, which for scoped packages is itself two segments
	// ("@scope/name-1.0.0.tgz"). The key never selects the storage path
	// (the publish face derives it from name+version); it only pairs the
	// attachment with the version npm clients expect.
	file := tarPath
	if i := strings.LastIndex(file, "/-/"); i >= 0 {
		file = file[i+len("/-"):]
	}
	file = strings.TrimPrefix(file, "/")
	doc := map[string]any{
		"_id":  name,
		"name": name,
		"versions": map[string]any{
			version: manifest,
		},
		"dist-tags": map[string]any{},
		"_attachments": map[string]any{
			file: map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tarball),
			},
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return notRetryable(fmt.Errorf("compose publish document for %s@%s: %w", name, version, err))
	}
	u, err := e.npmURL(cfg, tarPath, strings.Split(name, "/")...)
	if err != nil {
		return err
	}
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	resp, err := e.do(ctx, http.MethodPut, u, cfg, bytes.NewReader(body), int64(len(body)), hdr)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // status decides
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	default:
		return classifyNpm(resp, "publish "+name+"@"+version)
	}
}

// putNpmDistTag PUTs the single-tag face
// (/binflow/{repo}/-/package/{name}/dist-tags/{tag}) with the JSON-string
// version body the face parses.
func (e *Engine) putNpmDistTag(ctx context.Context, cfg *ReplicationConfig, name, tag, version string) error {
	segments := append([]string{"-", "package"}, strings.Split(name, "/")...)
	segments = append(segments, "dist-tags", tag)
	u, err := e.npmURL(cfg, name, segments...)
	if err != nil {
		return err
	}
	body, err := json.Marshal(version)
	if err != nil {
		return notRetryable(fmt.Errorf("encode dist-tag %s: %w", tag, err))
	}
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/json")
	resp, err := e.do(ctx, http.MethodPut, u, cfg, bytes.NewReader(body), int64(len(body)), hdr)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // status decides
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusNotFound:
		// The referenced version is not (yet) served on the target — most
		// plausibly a race with a concurrent publish cycle; retryable.
		return fmt.Errorf("dist-tag %s references version %s the target does not serve yet", tag, version)
	default:
		return classifyNpm(resp, "dist-tag "+tag)
	}
}
