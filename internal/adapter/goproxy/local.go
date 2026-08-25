package goproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The local-repository face (goproxy.md sections 4 and 5): the version-file
// GET trio with the two synthesis chains, the @v/list zip-only aggregation
// and the @latest candidate order, plus the PUT trio's validation chain.

// sha256Hex is the measured-digest helper for synthesized bodies.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// versionOfZipNode extracts the version of one @v directory zip node (the
// S8 rule — a version exists in the list exactly when its .zip does — makes
// the file name the version register).
func versionOfZipNode(path string) (string, bool) {
	i := strings.LastIndex(path, segVersionMarker)
	if i < 0 {
		return "", false
	}
	file := path[i+len(segVersionMarker):]
	if !strings.HasSuffix(file, ".zip") {
		return "", false
	}
	return strings.TrimSuffix(file, ".zip"), true
}

// serveFile handles GET/HEAD of .info/.mod/.zip.
func (h *Handler) serveFile(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, t target) {
	path := t.nodePath()
	headMod := r.Method == http.MethodHead && t.ext == "mod"

	// The +incompatible .mod rule (section 4.2 step 2 / section 6.2): a
	// version without a go.mod gets the synthesized single module line. On a
	// REMOTE repository the synthesis happens WITHOUT contacting the
	// upstream (section 6.2's explicit branch); on a VIRTUAL repository only
	// the LOCAL members are probed — a remote member is never asked for a
	// +incompatible .mod (the same no-upstream rule holding through the
	// aggregation); on local a stored copy wins first.
	if t.ext == "mod" && isIncompatible(t.version) {
		switch class {
		case repo.TypeRemote:
			h.serveSyntheticMod(w, t)
			return
		case repo.TypeVirtual:
			if rc, node, ok := h.readLocalMemberNode(ctx, repoKey, path); ok {
				h.serveNode(ctx, w, r, node, rc, contentTypeOf("mod"))
				return
			}
			h.serveSyntheticMod(w, t)
			return
		default:
			if rc, node, err := h.svc.Get(ctx, p, repoKey, path); err == nil {
				h.serveNode(ctx, w, r, node, rc, contentTypeOf("mod"))
				return
			} else if !unfound(err) {
				h.writeError(w, err, repoKey, path)
				return
			}
			h.serveSyntheticMod(w, t)
			return
		}
	}

	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		// The local .info synthesis chain (section 4.1 step 2): .info absent
		// but the version's .zip present -> synthesize from the zip node's
		// timestamp and write the copy back (best effort — a read-only
		// caller must never fail the GET).
		if class == repo.TypeLocal && t.ext == "info" && unfound(err) {
			if h.serveSyntheticInfo(ctx, w, p, repoKey, t) {
				return
			}
		}
		// HEAD .mod is the existence probe whose miss is 410 Gone (the
		// endpoint table's HEAD row / S9), not the download family's 404.
		if headMod && unfound(err) {
			writePlain(w, http.StatusGone, "gone")
			return
		}
		h.writeError(w, err, repoKey, path)
		return
	}
	h.serveNode(ctx, w, r, node, rc, contentTypeOf(t.ext))
}

// unfound reports whether err is the not-found family (the plain sentinel,
// or a *repo.StatusError wrapping it — the remote engine's unfound shape).
func unfound(err error) bool {
	return errors.Is(err, repo.ErrNodeNotFound)
}

// nodeOf loads one node row without keeping the blob open (the synthesis
// chains' timestamp source). A missing node answers (nil, nil).
func (h *Handler) nodeOf(ctx context.Context, p *repo.Principal, repoKey, path string) (*metadata.Node, error) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if unfound(err) {
			return nil, nil
		}
		return nil, err
	}
	_ = rc.Close() //nolint:errcheck // read-only fd
	return node, nil
}

// serveSyntheticMod renders the +incompatible synthesized go.mod. It is
// served directly and never cached: on a remote repository the upstream is
// not contacted for it (section 6.2), and on local/virtual a stored copy
// would have won the lookup above.
func (h *Handler) serveSyntheticMod(w http.ResponseWriter, t target) {
	body := synthMod(t.module)
	h.serveSynth(w, body, contentTypeOf("mod"), digestTriple{sha256: sha256Hex(body)})
}

// serveSyntheticInfo implements section 4.1's step 2 for the local class:
// synthesize {"Version","Time"} from the version's zip node and write the
// copy back through the regenerable-content exemption (best effort — the
// GET succeeds regardless). It reports whether it served a response.
func (h *Handler) serveSyntheticInfo(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey string, t target) bool {
	zipNode, err := h.nodeOf(ctx, p, repoKey, t.module+segVersionMarker+t.version+".zip")
	if err != nil || zipNode == nil {
		return false
	}
	body, merr := json.Marshal(infoBody{Version: t.version, Time: firstNonEmptyRFC3339(zipNode.UpdatedAt, zipNode.CreatedAt)})
	if merr != nil {
		return false
	}
	h.writeBackSyntheticInfo(ctx, p, repoKey, t, body)
	h.serveSynth(w, body, contentTypeOf("info"), digestTriple{sha256: sha256Hex(body)})
	return true
}

// writeBackSyntheticInfo lands the synthesized .info (section 4.1: the copy
// is system-generated and expirable). The write rides the TRIGGER
// principal's write grant (the maven metadata calculator's posture), so an
// anonymous or read-only reader simply re-synthesizes on the next GET — the
// body is deterministic, the two readers are indistinguishable. Failures
// are logged, never propagated: a read-only principal (or a full disk) must
// not turn a servable GET into an error.
func (h *Handler) writeBackSyntheticInfo(ctx context.Context, p *repo.Principal, repoKey string, t target, body []byte) {
	_, err := h.svc.PutWithOptions(ctx, p, repoKey, t.nodePath(),
		bytes.NewReader(body), storage.BlobRef{Sha256: sha256Hex(body)},
		contentTypeOf("info"), repo.PutOptions{SkipOverwriteCheck: true})
	if err != nil && !errors.Is(err, repo.ErrForbidden) && !errors.Is(err, repo.ErrUnauthorized) {
		slog.WarnContext(ctx, "goproxy: synthesized .info write-back failed (serving anyway)",
			slog.String("repo", repoKey), slog.String("path", t.nodePath()), slog.String("error", err.Error()))
	}
}

// firstNonEmptyRFC3339 returns the first parseable RFC 3339 value.
func firstNonEmptyRFC3339(vals ...string) string {
	for _, v := range vals {
		if _, err := time.Parse(time.RFC3339, v); err == nil {
			return v
		}
	}
	return ""
}

// serveList handles GET @v/list for all three classes (the proxy faces live
// in remote.go/virtual.go; local is here).
func (h *Handler) serveList(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, t target) {
	switch class {
	case repo.TypeRemote:
		h.serveRemoteList(ctx, w, r, p, repoKey, t)
	case repo.TypeVirtual:
		h.serveVirtualList(ctx, w, repoKey, t)
	default:
		versions, err := h.localVersions(ctx, p, repoKey, t.module)
		if err != nil {
			h.writeError(w, err, repoKey, t.module)
			return
		}
		if len(versions) == 0 {
			// The local tightening (S13): a module with no zip is an unknown
			// module — 404, not an empty 200.
			writePlain(w, http.StatusNotFound, msgNotFound)
			return
		}
		writeVersionList(w, versions)
	}
}

// writeVersionList renders the list body: one version per line, the pure
// version column (S7's BinFlow decision — no timestamp column), stable
// lexicographic order.
func writeVersionList(w http.ResponseWriter, versions []string) {
	sorted := append([]string(nil), versions...)
	sort.Strings(sorted)
	var b strings.Builder
	for _, v := range sorted {
		b.WriteString(v)
		b.WriteByte('\n')
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", strconv.Itoa(b.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String())) //nolint:gosec // G705: version spellings under nosniff + text/plain
}

// localVersions aggregates a local repository's registered versions for one
// module (section 4.4): the @v directory's .zip files, nothing else.
func (h *Handler) localVersions(ctx context.Context, p *repo.Principal, repoKey, module string) ([]string, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, module+segVersionMarker)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, n := range nodes {
		if v, ok := versionOfZipNode(n.Path); ok {
			versions = append(versions, v)
		}
	}
	return versions, nil
}

// serveLatest handles GET @latest for all three classes.
func (h *Handler) serveLatest(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string, t target) {
	switch class {
	case repo.TypeRemote:
		h.serveRemoteLatest(ctx, w, r, p, repoKey, t)
	case repo.TypeVirtual:
		h.serveVirtualLatest(ctx, w, repoKey, t)
	default:
		versions, err := h.localVersions(ctx, p, repoKey, t.module)
		if err != nil {
			h.writeError(w, err, repoKey, t.module)
			return
		}
		best, ok := pickLatest(versions)
		if !ok {
			writePlain(w, http.StatusNotFound, msgNotFound)
			return
		}
		h.serveLocalVersionInfo(ctx, w, r, p, repoKey, t.module, best)
	}
}

// serveLocalVersionInfo answers with one version's .info — the stored copy
// byte-for-byte, else the synthesis chain (section 4.5 includes section
// 4.1's chain, write-back included).
func (h *Handler) serveLocalVersionInfo(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, module, version string) {
	infoPath := module + segVersionMarker + version + ".info"
	if rc, node, err := h.svc.Get(ctx, p, repoKey, infoPath); err == nil {
		h.serveNode(ctx, w, r, node, rc, contentTypeOf("info"))
		return
	} else if !unfound(err) {
		h.writeError(w, err, repoKey, infoPath)
		return
	}
	zipNode, err := h.nodeOf(ctx, p, repoKey, module+segVersionMarker+version+".zip")
	if err != nil || zipNode == nil {
		writePlain(w, http.StatusNotFound, msgNotFound)
		return
	}
	body, merr := json.Marshal(infoBody{Version: version, Time: firstNonEmptyRFC3339(zipNode.UpdatedAt, zipNode.CreatedAt)})
	if merr != nil {
		writePlain(w, http.StatusInternalServerError, merr.Error())
		return
	}
	h.writeBackSyntheticInfo(ctx, p, repoKey, target{module: module, version: version, ext: "info", kind: kindFile}, body)
	h.serveSynth(w, body, contentTypeOf("info"), digestTriple{sha256: sha256Hex(body)})
}

// ---- PUT (the vendor upload extension, section 4.6 / section 5) ----

// bodyReadLimits bound the buffered upload validation (the .zip body is
// NEVER buffered — it streams straight through the checksum chain; the
// official module-zip ceiling is 500MiB and buffering would break that).
const (
	infoBodyLimit = 1 << 20  // .info metadata JSON: generous vs the ~100B real shape
	modBodyLimit  = 16 << 20 // go.mod ceiling per the official module zip rules
)

// servePut implements the PUT trio's validation chain: version grammar and
// module-major consistency first (400), then the per-extension content
// validation (.info JSON / .mod module directive, 400), then the service's
// checksum + permission + overwrite semantics (409/403/201). Writes onto a
// remote repository never reach here's validation stage questions — the
// service answers its read-only 405 (RE-05) and a virtual repository routes
// onto its deployment member (the existing faces, untouched).
func (h *Handler) servePut(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, _ string, t target) {
	if !validPutVersion(t.version) {
		writePlain(w, http.StatusBadRequest, fmt.Sprintf(
			"invalid version %q: PUT requires a canonical version (vX.Y.Z[-pre][+incompatible] or a pseudo-version)", t.version))
		return
	}
	if err := checkMajorConsistency(t.module, t.version); err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}

	var body io.Reader = r.Body
	switch t.ext {
	case "info":
		buf, ierr := readBounded(r.Body, infoBodyLimit)
		if ierr != nil {
			writePlain(w, http.StatusBadRequest, ierr.Error())
			return
		}
		if verr := validateInfoBody(buf, t.version); verr != nil {
			writePlain(w, http.StatusBadRequest, verr.Error())
			return
		}
		body = bytes.NewReader(buf)
	case "mod":
		buf, merr := readBounded(r.Body, modBodyLimit)
		if merr != nil {
			writePlain(w, http.StatusBadRequest, merr.Error())
			return
		}
		if verr := validateModBody(buf, t.module); verr != nil {
			writePlain(w, http.StatusBadRequest, verr.Error())
			return
		}
		body = bytes.NewReader(buf)
	case "zip":
		// No internal structure validation (section 4.3: the zip's internal
		// shape is the client's domain; the server chain is checksums only).
	default:
		writePlain(w, http.StatusNotFound, msgNotFound)
		return
	}

	node, perr := h.svc.Put(ctx, p, repoKey, t.nodePath(), body, expect, contentTypeOf(t.ext))
	if perr != nil {
		h.writeError(w, perr, repoKey, t.nodePath())
		return
	}
	hdr := w.Header()
	hdr.Set("Location", t.wirePath())
	if node.Sha256 != "" {
		hdr.Set(hdrChecksumSha256, node.Sha256)
	}
	w.WriteHeader(http.StatusCreated)
}

// readBounded reads at most limit bytes; an over-limit body is a 400 (the
// validation ceilings are protocol grammar, not storage policy).
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read upload body: %w", err)
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("upload body exceeds the %d byte ceiling for this file type", limit)
	}
	return buf, nil
}

// validateInfoBody enforces section 5.2's .info contract: a legal JSON
// object whose Version is canonical and equals the URL version, with an
// optional RFC 3339 Time.
func validateInfoBody(body []byte, version string) error {
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return fmt.Errorf("invalid .info body: not a JSON object: %w", err)
	}
	v, ok := probe["Version"].(string)
	if !ok || v == "" {
		return fmt.Errorf("invalid .info body: Version is required and must be a string")
	}
	if !validPutVersion(v) {
		return fmt.Errorf("invalid .info body: Version %q is not a canonical version", v)
	}
	if v != version {
		return fmt.Errorf("invalid .info body: Version %q does not match the request version %q", v, version)
	}
	if ts, has := probe["Time"]; has && ts != nil {
		s, ok := ts.(string)
		if !ok {
			return fmt.Errorf("invalid .info body: Time must be an RFC 3339 string")
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return fmt.Errorf("invalid .info body: Time %q is not RFC 3339: %w", s, err)
		}
	}
	return nil
}

// validateModBody enforces section 5.2's .mod contract: the body must parse
// a `module <path>` directive whose (unquoted) path equals the request's
// decoded module path.
func validateModBody(body []byte, module string) error {
	path, ok := moduleDirectiveOf(body)
	if !ok {
		return fmt.Errorf("invalid .mod body: no module directive found")
	}
	if path != module {
		return fmt.Errorf("invalid .mod body: module %q does not match the request module %q", path, module)
	}
	return nil
}

// moduleDirectiveOf extracts the first module directive's path (quoted or
// bare), tolerating comments and blank lines the way the go.mod grammar
// does. It deliberately does not validate the rest of the file — the
// original-unmodified contract means BinFlow stores whatever the client
// sent, checking only the identity line.
func moduleDirectiveOf(body []byte) (string, bool) {
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "module "))
		if strings.HasPrefix(rest, `"`) && strings.HasSuffix(rest, `"`) && len(rest) >= 2 {
			rest = rest[1 : len(rest)-1]
		}
		// A trailing comment is not part of the path.
		if i := strings.Index(rest, " //"); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		if rest == "" || strings.ContainsAny(rest, " \t") {
			return "", false
		}
		return rest, true
	}
	return "", false
}

// declaredDigests parses the X-Checksum-* headers into a BlobRef (the
// generic adapter's contract: malformed -> adapter.ErrInvalidChecksum ->
// 400; a well-formed disagreement is the storage layer's 409).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				adapter.ErrInvalidChecksum, name, width, v)
		}
		return v, nil
	}
	sha256, err := parse(hdrChecksumSha256, 64)
	if err != nil {
		return storage.BlobRef{}, err
	}
	sha1, err := parse(hdrChecksumSha1, 40)
	if err != nil {
		return storage.BlobRef{}, err
	}
	md5, err := parse(hdrChecksumMd5, 32)
	if err != nil {
		return storage.BlobRef{}, err
	}
	return storage.BlobRef{Sha256: sha256, Sha1: sha1, Md5: md5}, nil
}

// isHex reports whether s is exactly n hex characters.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
