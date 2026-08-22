package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The docker push plane (T-195 D2, over T-175's finding that the generic
// REST addressing dies with 404 UNSUPPORTED on the target's /v2 face).
//
// Docker repositories keep two node layouts (adapter/docker, architecture
// section 6): <image>/blobs/<hex> for layers, configs and manifest BODIES,
// and <image>/manifests/<hex> for the manifest node plus — in the docker
// index, NOT in node rows — the tag pointers. A push that lands only node
// rows leaves the target unable to resolve a single tag, so the plane speaks
// the registry protocol the target's own clients use:
//
//   - blob tasks: HEAD /v2/<repo>/<image>/blobs/sha256:<hex> as the
//     idempotence probe, then the single-request monolithic upload
//     POST /v2/<repo>/<image>/blobs/uploads/?digest=sha256:<hex> (DE-02's
//     one-round-trip arm — oras/crane style, and the arm the target streams
//     without buffering);
//   - manifest tasks: PUT /v2/<repo>/<image>/manifests/sha256:<hex> with
//     the node row's stored mime as the wire Content-Type, then one
//     PUT /v2/<repo>/<image>/manifests/<tag> per source tag pointer so the
//     target's docker_tags index converges (docker pull by tag is the QA
//     acceptance). Tag pointers are metadata, not content: the source is
//     authoritative and a moved tag REPOINTS the target (the Q7 interim's
//     first-write-wins governs blob bytes, not tag resolution).
//
// Task ordering does the rest: layer/config tasks are enqueued while the
// client pushes (PutLandedBlob tail), the manifest-body task when the
// adapter lands the body (Put tail), the manifest-node task last
// (PutManifest tail) — FIFO drain therefore satisfies the target's
// reference-integrity gate (manifest PUT verifies every referenced blob is
// already served) except across crash windows, where the gate's
// MANIFEST_BLOB_UNKNOWN is a plain retryable failure and the next drain
// (blobs first, older created_at) heals it.

// dockerDigestPrefix is the wire spelling of the only served algorithm.
const dockerDigestPrefix = "sha256:"

// manifestBodyCap mirrors the target's own manifest body limit
// (adapter/docker manifestMaxBytes, PRD section 6.5 ruling ③): reading past
// it means the source row can never be pushed, so the read itself refuses.
const manifestBodyCap = 4 << 20

// dockerRouteSegments are the layout markers that end an image name.
var dockerRouteSegments = map[string]bool{"blobs": true, "manifests": true}

// splitDockerNodePath parses <image>/(blobs|manifests)/<hex> out of a
// repo-relative docker node path. The marker scan mirrors adapter/docker's
// parseV2Name: the FIRST route segment with at least one image segment
// before it wins, so an image whose own name would contain "blobs" cannot
// shift the boundary the adapter itself would not shift.
func splitDockerNodePath(nodePath string) (image, kind, hex string, err error) {
	segs := strings.Split(nodePath, "/")
	for i := 1; i < len(segs)-1; i++ {
		if !dockerRouteSegments[segs[i]] {
			continue
		}
		if i+1 != len(segs)-1 || segs[len(segs)-1] == "" {
			break
		}
		hex = segs[len(segs)-1]
		if len(hex) != 64 || !isLowerHex(hex) {
			return "", "", "", fmt.Errorf("docker node path %q: digest segment is not a sha256 hex", nodePath)
		}
		return strings.Join(segs[:i], "/"), segs[i], hex, nil
	}
	return "", "", "", fmt.Errorf("docker node path %q: not the <image>/(blobs|manifests)/<hex> layout", nodePath)
}

// isLowerHex reports whether s is non-empty lowercase hex.
func isLowerHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return len(s) > 0
}

// v2URL builds a target /v2 address: {base}/v2/{repo}/{image...}/{tail}.
// Every segment is escaped individually; dot and empty segments are refused
// (the same defense targetURL runs for the generic plane).
func (e *Engine) v2URL(cfg *ReplicationConfig, nodePath string, segments ...string) (*url.URL, error) {
	base, err := e.targetBase(cfg)
	if err != nil {
		return nil, err
	}
	all := make([]string, 0, len(segments)+2)
	all = append(all, "v2", cfg.TargetRepo)
	all = append(all, segments...)
	for i, seg := range all {
		if seg == "" || seg == "." || seg == ".." {
			return nil, notRetryable(fmt.Errorf("config %q: docker path %q: illegal segment %q", cfg.Name, nodePath, seg))
		}
		all[i] = url.PathEscape(seg)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.Join(all, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

// classifyV2 maps a non-2xx /v2 response onto a task error. The registry's
// deterministic client faults (400 DIGEST_INVALID — the pushed bytes
// disagree with their own digest; 405 on the read-only faces) are terminal;
// everything else (401/403 credential trouble an admin may fix, 404
// NAME_UNKNOWN until the target repo appears, 5xx) stays retryable — the
// backoff schedule and the D1 revival keep those converging.
func classifyV2(resp *http.Response, what string) error {
	err := fmt.Errorf("%s: target answered %d %s", what, resp.StatusCode, strings.TrimSpace(readSnippet(resp)))
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusMethodNotAllowed {
		return notRetryable(err)
	}
	return err
}

// digestHeader extracts the response's blob/manifest digest
// (Docker-Content-Digest, falling back to X-Checksum-Sha256) as bare hex;
// "" when the target served no comparable digest.
func digestHeader(resp *http.Response) string {
	v := resp.Header.Get("Docker-Content-Digest")
	if v == "" {
		v = resp.Header.Get("X-Checksum-Sha256")
	}
	return strings.TrimPrefix(strings.ToLower(v), dockerDigestPrefix)
}

// pushDocker is the docker plane entry: route by node layout, refuse layout
// disagreements with the task's sha256 deterministically (a mangled row
// must not turn into a wrong-digest push the target would 400 anyway).
func (e *Engine) pushDocker(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath string) error {
	image, kind, hex, err := splitDockerNodePath(nodePath)
	if err != nil {
		return notRetryable(err)
	}
	if hex != sha256 {
		return notRetryable(fmt.Errorf("docker node path %q: digest %s disagrees with the task's blob sha256 %s", nodePath, hex, sha256))
	}
	if kind == "manifests" {
		return e.pushDockerManifest(ctx, cfg, sha256, nodePath, image, hex)
	}
	return e.pushDockerBlob(ctx, cfg, sha256, nodePath, image, hex)
}

// pushDockerBlob lands one layer/config/manifest-body blob.
func (e *Engine) pushDockerBlob(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath, image, hex string) error {
	segs := append(strings.Split(image, "/"), "blobs", dockerDigestPrefix+hex)
	u, err := e.v2URL(cfg, nodePath, segs...)
	if err != nil {
		return err
	}

	// Step 1: existence probe (ADR-0021 idempotency clause on the /v2 face).
	resp, err := e.do(ctx, http.MethodHead, u, cfg, nil, 0, nil)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		_ = resp.Body.Close()
		if sum := digestHeader(resp); sum != "" && sum != hex {
			return notRetryable(fmt.Errorf("conflict: target holds digest %s at %s, source digest is %s; target left untouched (Q7 interim)",
				sum, u.Redacted(), hex))
		}
		return nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
	default:
		return classifyV2(resp, "blob probe")
	}

	// Step 2: single-request monolithic upload (the body streams straight
	// through, never buffered).
	rc, ref, err := e.blobs.Open(ctx, sha256)
	if err != nil {
		return notRetryable(fmt.Errorf("source blob %s: %w", sha256, err))
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	usegs := append(strings.Split(image, "/"), "blobs", "uploads")
	up, err := e.v2URL(cfg, nodePath, usegs...)
	if err != nil {
		return err
	}
	q := up.Query()
	q.Set("digest", dockerDigestPrefix+hex)
	up.RawQuery = q.Encode()
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/octet-stream")
	resp, err = e.do(ctx, http.MethodPost, up, cfg, rc, ref.Size, hdr)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusCreated:
		_ = resp.Body.Close()
		if sum := digestHeader(resp); sum != "" && sum != hex {
			return notRetryable(fmt.Errorf("target confirmed digest %s, expected %s", sum, hex))
		}
		return nil
	default:
		return classifyV2(resp, "blob upload")
	}
}

// pushDockerManifest lands one manifest node plus its tag pointers.
func (e *Engine) pushDockerManifest(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath, image, hex string) error {
	// The wire Content-Type is the node row's stored mime (FR-7-AC4); a
	// missing one falls back to the body's own mediaType member, the field
	// every docker/OCI manifest carries.
	mime := ""
	var tags []string
	if e.cfg.meta != nil {
		if n, err := e.cfg.meta.Node(ctx, cfg.SourceRepo, nodePath); err == nil {
			mime = n.Mime
		} else if !errors.Is(err, ErrMetaNotFound) && !errors.Is(err, ErrNotRetryable) {
			return err
		}
		if t, err := e.cfg.meta.DockerTags(ctx, cfg.SourceRepo, image, hex); err == nil {
			tags = t
		} else {
			return err
		}
	}

	body, err := e.readAllBounded(ctx, sha256, manifestBodyCap)
	if err != nil {
		return err
	}
	if mime == "" {
		mime = manifestMediaTypeOf(body)
	}
	if mime == "" {
		return notRetryable(fmt.Errorf("manifest %s: no stored mime and the body carries no mediaType; refusing to push untyped", nodePath))
	}

	// Digest PUT (idempotent skip when the target already serves the exact
	// manifest; tags still converge below).
	msegs := append(strings.Split(image, "/"), "manifests", dockerDigestPrefix+hex)
	u, err := e.v2URL(cfg, nodePath, msegs...)
	if err != nil {
		return err
	}
	accept := http.Header{}
	accept.Set("Accept", mime)
	resp, err := e.do(ctx, http.MethodHead, u, cfg, nil, 0, accept)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		_ = resp.Body.Close()
	case http.StatusNotFound:
		_ = resp.Body.Close()
		if err := e.putDockerManifestBody(ctx, cfg, nodePath, image, dockerDigestPrefix+hex, mime, body); err != nil {
			return err
		}
	default:
		return classifyV2(resp, "manifest probe")
	}

	// Tag pointers: source-authoritative convergence — a tag already
	// pointing here is skipped, anything else (missing, or pointing at an
	// older manifest after a re-push on the source) is (re)written.
	for _, tag := range tags {
		if tag == "" || tag == "." || tag == ".." || strings.ContainsAny(tag, "/%") {
			continue // defensive: the source's own validator forbids these
		}
		tsegs := append(strings.Split(image, "/"), "manifests", tag)
		tu, err := e.v2URL(cfg, nodePath, tsegs...)
		if err != nil {
			return err
		}
		resp, err := e.do(ctx, http.MethodGet, tu, cfg, nil, 0, accept)
		if err != nil {
			return err
		}
		repoint := true
		if resp.StatusCode == http.StatusOK {
			present := digestHeader(resp)
			// Bounded drain of the probe body (protocol caps manifests at 4MB;
			// a target streaming past that is broken and the read stops).
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, manifestBodyCap+1)) //nolint:errcheck // probe body is throwaway
			_ = resp.Body.Close()
			repoint = present != hex
		} else {
			snippet := readSnippet(resp)
			if resp.StatusCode != http.StatusNotFound {
				return fmt.Errorf("tag probe: target answered %d %s", resp.StatusCode, snippet)
			}
		}
		if !repoint {
			continue
		}
		if err := e.putDockerManifestBody(ctx, cfg, nodePath, image, tag, mime, body); err != nil {
			return err
		}
	}
	return nil
}

// putDockerManifestBody PUTs the manifest bytes at one reference (digest or
// tag) with the wire Content-Type.
func (e *Engine) putDockerManifestBody(ctx context.Context, cfg *ReplicationConfig, nodePath, image, ref, mime string, body []byte) error {
	psegs := append(strings.Split(image, "/"), "manifests", ref)
	u, err := e.v2URL(cfg, nodePath, psegs...)
	if err != nil {
		return err
	}
	hdr := http.Header{}
	hdr.Set("Content-Type", mime)
	resp, err := e.do(ctx, http.MethodPut, u, cfg, strings.NewReader(string(body)), int64(len(body)), hdr)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // terminal read below
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	default:
		return classifyV2(resp, "manifest push at "+ref)
	}
}

// manifestMediaTypeOf extracts a manifest body's top-level mediaType member
// (the field docker schema2, manifest lists and OCI manifests carry); "" when
// absent or not a string.
func manifestMediaTypeOf(body []byte) string {
	var probe struct {
		MediaType string `json:"mediaType"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.MediaType
}
