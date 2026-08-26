package cargo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The publish wire and its chain (spec section 5).
//
// Wire frame, byte for byte the official form:
//
//	[u32 LE = JSON length][publish metadata JSON]
//	[u32 LE = crate length][.crate gzip tarball]
//
// Failure policy is CG-2: every refusal is 4xx/5xx + the errors[] envelope
// — malformed framing/metadata is the client's 400, a body-read or landing
// failure the server's 500, a duplicate name+version (build metadata
// ignored) the 409 (CG-3). The Artifactory 200+errors form is not
// implemented.

// errInvalidPackage marks the client-fixable validation family (the 400s).
var errInvalidPackage = errors.New("invalid package")

// maxMetaJSON bounds the metadata frame (the publish JSON is manifest
// facts, never the readme body — a megabyte is an order of magnitude
// beyond any real manifest, and a hostile length must fail fast instead
// of allocating).
const maxMetaJSON = 1 << 20

// maxFrameLen bounds one declared frame length (the crate arm spools to
// disk, so the bound only stops absurd allocations upstream; the quota
// gate owns the real ceiling).
const maxFrameLen = 1 << 40

// publishMeta is the metadata frame's field set BinFlow consumes; every
// other field rides along verbatim in the stored sidecar.
type publishMeta struct {
	Name        string          `json:"name"`
	Vers        string          `json:"vers"`
	Deps        json.RawMessage `json:"deps"`
	Features    json.RawMessage `json:"features"`
	Authors     []string        `json:"authors"`
	Description string          `json:"description"`
	Keywords    []string        `json:"keywords"`
	Categories  []string        `json:"categories"`
	Links       string          `json:"links"`
}

// decodePublishFrame splits the publish body into its metadata JSON and
// the .crate bytes (spooled to a temp file — the crate is never held in
// memory whole). Truncated frames, oversize lengths and trailing bytes
// are the client's errInvalidPackage; a body-read failure is a distinct
// error the handler maps to 500.
func decodePublishFrame(body io.Reader) (rawJSON []byte, cratePath string, err error) {
	f, err := os.CreateTemp("", "binflow-cargo-*.crate")
	if err != nil {
		return nil, "", fmt.Errorf("spool crate: %w", err)
	}
	spooled := false
	defer func() {
		if !spooled {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()

	rawJSON, err = readFramedBytes(body, maxMetaJSON)
	if err != nil {
		return nil, "", err
	}
	crateLen, err := readFrameLen(body)
	if err != nil {
		return nil, "", err
	}
	if crateLen > maxFrameLen {
		return nil, "", fmt.Errorf("%w: crate frame of %d bytes exceeds the %d limit", errInvalidPackage, crateLen, maxFrameLen)
	}
	if crateLen == 0 {
		return nil, "", fmt.Errorf("%w: crate frame is empty", errInvalidPackage)
	}
	copied, cerr := io.Copy(f, io.LimitReader(body, int64(crateLen)))
	if cerr != nil {
		return nil, "", fmt.Errorf("read crate frame: %w", cerr)
	}
	if copied != int64(crateLen) {
		return nil, "", fmt.Errorf("%w: crate frame truncated: %d of %d bytes", errInvalidPackage, copied, crateLen)
	}
	// The frame must end exactly at the crate: one extra byte is a framing
	// violation, not a trailing-newline tolerance case.
	var probe [1]byte
	n, err := body.Read(probe[:])
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, "", fmt.Errorf("probe trailing bytes: %w", err)
	}
	if n != 0 {
		return nil, "", fmt.Errorf("%w: %d trailing byte(s) after the crate frame", errInvalidPackage, n)
	}
	if err := f.Close(); err != nil {
		return nil, "", fmt.Errorf("close spool: %w", err)
	}
	spooled = true
	return rawJSON, f.Name(), nil
}

// readFramedBytes reads one [u32 LE length][payload] frame fully into
// memory, bounded by maxLen.
func readFramedBytes(body io.Reader, maxLen uint64) ([]byte, error) {
	n, err := readFrameLen(body)
	if err != nil {
		return nil, err
	}
	if n > maxLen {
		return nil, fmt.Errorf("%w: frame of %d bytes exceeds the %d limit", errInvalidPackage, n, maxLen)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(body, buf); err != nil {
		return nil, fmt.Errorf("%w: frame truncated: %w", errInvalidPackage, err)
	}
	return buf, nil
}

// readFrameLen reads the 4-byte little-endian length prefix.
func readFrameLen(body io.Reader) (uint64, error) {
	var le [4]byte
	if _, err := io.ReadFull(body, le[:]); err != nil {
		return 0, fmt.Errorf("%w: length prefix truncated: %w", errInvalidPackage, err)
	}
	return uint64(binary.LittleEndian.Uint32(le[:])), nil
}

// parsePublishMeta validates the metadata frame: the name against the
// crate-name rules, the version against strict SemVer 2.0 (spec section
// 5.3), deps/features present-or-absent both legal (the index line
// normalizes them).
func parsePublishMeta(raw []byte) (*publishMeta, error) {
	var m publishMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: metadata frame is not valid JSON: %w", errInvalidPackage, err)
	}
	if !validCrateName(m.Name) {
		return nil, fmt.Errorf("%w: crate name %q is not a legal crate name (ASCII letter first, [A-Za-z0-9_-], <= %d characters)",
			errInvalidPackage, m.Name, maxCrateNameLen)
	}
	if _, err := parseSemver(m.Vers); err != nil {
		return nil, fmt.Errorf("%w: version %q is not valid SemVer 2.0: %w", errInvalidPackage, m.Vers, err)
	}
	return &m, nil
}

// servePublish implements PUT api/v1/crates/new: deframe, validate, the
// duplicate-version 409 (CG-3), the crate + sidecar landings, the index
// rewrite, the 200.
//
// The ADDON GATE already ran at dispatchContent (write verb); the adapter
// never re-asks. A publish onto a remote repository dies at the class
// door in ServeHTTP; a publish onto a virtual repository is the M11
// routing ticket's, refused the same way.
func (h *Handler) servePublish(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string) {
	rawJSON, spoolPath, err := decodePublishFrame(r.Body)
	if err != nil {
		h.writeFramingError(w, err)
		return
	}
	defer func() { _ = os.Remove(spoolPath) }()

	meta, err := parsePublishMeta(rawJSON)
	if err != nil {
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}
	// The protocol properties the landing writes must satisfy the closed
	// property rules (a >1KiB description would die inside Put after the
	// duplicate check — refuse here, with the property wording).
	if err := metadata.ValidatePropSet(crateProps(meta)); err != nil {
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}

	// The duplicate-version refusal (CG-3): same name+version IGNORING
	// build metadata is the index uniqueness rule, checked BEFORE any
	// landing so a refused publish leaves no bytes behind.
	nodes, err := h.svc.List(ctx, p, repoKey, crateDir(meta.Name))
	if err != nil {
		h.writeError(w, err, repoKey, cratePath(meta.Name, meta.Vers))
		return
	}
	for _, n := range nodes {
		if _, v, ok := splitCrateNode(n.Path); ok && sameVersionIgnoringBuild(v, meta.Vers) {
			writeEnvelope(w, http.StatusConflict,
				fmt.Sprintf("crate %s version %s already exists (build metadata does not distinguish versions)", meta.Name, meta.Vers))
			return
		}
	}

	target := cratePath(meta.Name, meta.Vers)
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}

	// The crate blob (content class; cargo clients declare no checksums —
	// the server measures, spec section 5.3).
	crateFile, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		writeEnvelope(w, http.StatusInternalServerError, fmt.Sprintf("reopen spool: %v", err))
		return
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, target, crateFile, expect,
		"application/octet-stream", repo.PutOptions{Properties: crateProps(meta)})
	_ = crateFile.Close()
	if err != nil {
		h.writeError(w, err, repoKey, target)
		return
	}

	// The metadata sidecar: the raw frame verbatim (the index rewrite and
	// the property backfill read it back). Regenerable family — a later
	// import rewrites it without a delete-permission demand.
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, metaPath(meta.Name, meta.Vers),
		bytes.NewReader(rawJSON), blobRefOf(rawJSON), "application/json",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		slog.WarnContext(ctx, "cargo: metadata sidecar write failed",
			slog.String("repo", repoKey), slog.String("path", metaPath(meta.Name, meta.Vers)),
			slog.String("error", err.Error()))
	}

	// The index rewrite (spec section 3.3): the whole file, one line per
	// version, cksum = the storage-measured sha256.
	if err := h.rewriteIndex(ctx, p, repoKey, meta.Name); err != nil {
		slog.WarnContext(ctx, "cargo: index rewrite failed",
			slog.String("repo", repoKey), slog.String("crate", meta.Name),
			slog.String("error", err.Error()))
		writeEnvelope(w, http.StatusInternalServerError, fmt.Sprintf("index rewrite: %v", err))
		return
	}

	// The response body is the official warnings-only shape. Probed live
	// (cargo 1.98, T-294): the spec section 2 sketch appended an
	// "errors":[] member, but a present — even empty — errors key is
	// rendered by cargo as "the remote server responded with an error";
	// crates.io's actual 200 body carries warnings only.
	body, _ := json.Marshal(map[string]any{
		"warnings": map[string]any{
			"invalid_categories": []any{},
			"invalid_badges":     []any{},
			"other":              []any{},
		},
	})
	if node != nil && node.Sha256 != "" {
		w.Header().Set(hdrChecksumSha256, node.Sha256)
	}
	writeJSON(w, http.StatusOK, body)
}

// writeFramingError splits the deframe failure family: a malformed frame
// is the client's 400, a body-read failure the server's 500 (CG-2 — both
// carry the envelope; neither ever rides a 200).
func (h *Handler) writeFramingError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidPackage) {
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}
	writeEnvelope(w, http.StatusInternalServerError, err.Error())
}

// crateProps renders the .crate node's protocol properties (spec section
// 4): name/version always; description/keywords/categories when present
// (keywords/categories joined with ';'). The property-value rules refuse
// empty values, so absent facts are simply absent keys.
func crateProps(m *publishMeta) map[string][]string {
	props := map[string][]string{
		propName:    {m.Name},
		propVersion: {m.Vers},
	}
	if m.Description != "" {
		props[propDescription] = []string{m.Description}
	}
	if len(m.Keywords) > 0 {
		props[propKeywords] = []string{joinSemi(m.Keywords)}
	}
	if len(m.Categories) > 0 {
		props[propCategories] = []string{joinSemi(m.Categories)}
	}
	return props
}

// joinSemi joins with the spec's multi-value convention.
func joinSemi(parts []string) string {
	var b bytes.Buffer
	for i, s := range parts {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(s)
	}
	return b.String()
}

// splitSemi is joinSemi's inverse (the property-backfill arm, spec
// section 4's external-import scenario).
func splitSemi(joined string) []string {
	if joined == "" {
		return nil
	}
	return strings.Split(joined, ";")
}

// declaredDigests parses the X-Checksum-* family (the shared contract:
// cargo sends none, curl deployments may; malformed → 400).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				errInvalidChecksum, name, width, v)
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

// blobRefOf declares the measured sha256 of one small in-memory body (the
// sidecar write's expect argument).
func blobRefOf(b []byte) storage.BlobRef {
	sum := sha256.Sum256(b)
	return storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}
}
