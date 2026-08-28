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
// Failure policy is CG-2's final ruling (T-316, flipping T-294's unified
// 4xx/5xx): the processing-failure family answers 200 + the warnings
// envelope with every error string riding warnings.other — the Artifactory
// wire (CargoPublishResponse has no top-level errors member; cargo 1.98
// treats a present errors key as failure, so the dual form MUST ride the
// warnings lists). The dual track: the PERMISSION family (no write
// permission; a duplicate version the principal may not delete) keeps the
// honest 401 (anonymous) / 403 (named) + the errors[] envelope, and a
// length-prefix defect answers 500 (the RuntimeException family Artifactory
// never catches — its Jersey mapping's degraded declaration). A duplicate
// version the principal MAY delete is an overwrite, never a conflict
// (D-3: the 409 arm is gone; the service's repo-semantics section 3 gate
// owns the decision). Nothing in the landing chain turns a 200 into a
// failure response: a sidecar or index-rewrite fault appends its error
// string to warnings.other and the publish still answers 200.

// errFramingDefect marks the framing family that answers 500 + envelope
// (CG-2 class 7): a truncated length prefix, an absurd declared length, an
// empty crate frame or trailing bytes — shape violations the reader asserts
// without any transport fault, the analog of Artifactory's uncaught
// RuntimeException. Every truncation that surfaces as a read ending
// (EOF mid-frame, a reset connection, a spool fault) is NOT this family:
// those are the IOException track and ride the 200 + warnings.other arm.
var errFramingDefect = errors.New("invalid publish frame")

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
// memory whole). Shape defects (errFramingDefect) answer 500; every error
// the READING of the stream produces — a frame that ends early (the
// EOFException family), a reset connection, a spool fault — carries no
// sentinel and rides the handler's 200 + warnings.other arm (CG-2's
// IOException track).
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
		return nil, "", fmt.Errorf("%w: crate frame of %d bytes exceeds the %d limit", errFramingDefect, crateLen, maxFrameLen)
	}
	if crateLen == 0 {
		return nil, "", fmt.Errorf("%w: crate frame is empty", errFramingDefect)
	}
	copied, cerr := io.Copy(f, io.LimitReader(body, int64(crateLen)))
	if cerr != nil {
		return nil, "", fmt.Errorf("read crate frame: %w", cerr)
	}
	if copied != int64(crateLen) {
		return nil, "", fmt.Errorf("crate frame truncated: %d of %d bytes", copied, crateLen)
	}
	// The frame must end exactly at the crate: one extra byte is a framing
	// violation, not a trailing-newline tolerance case.
	var probe [1]byte
	n, perr := body.Read(probe[:])
	if perr != nil && !errors.Is(perr, io.EOF) {
		return nil, "", fmt.Errorf("probe trailing bytes: %w", perr)
	}
	if n != 0 {
		return nil, "", fmt.Errorf("%w: %d trailing byte(s) after the crate frame", errFramingDefect, n)
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
		return nil, fmt.Errorf("%w: frame of %d bytes exceeds the %d limit", errFramingDefect, n, maxLen)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(body, buf); err != nil {
		return nil, fmt.Errorf("frame truncated: %w", err)
	}
	return buf, nil
}

// readFrameLen reads the 4-byte little-endian length prefix. A short read
// here is the errFramingDefect family (CG-2 class 7): Artifactory's
// readLittleEndianInt throws a RuntimeException that no catch absorbs.
func readFrameLen(body io.Reader) (uint64, error) {
	var le [4]byte
	if _, err := io.ReadFull(body, le[:]); err != nil {
		return 0, fmt.Errorf("%w: length prefix truncated: %w", errFramingDefect, err)
	}
	return uint64(binary.LittleEndian.Uint32(le[:])), nil
}

// parsePublishMeta validates the metadata frame: the name against the
// crate-name rules, the version against strict SemVer 2.0 (spec section
// 5.3), deps/features present-or-absent both legal (the index line
// normalizes them). Validation faults carry no sentinel: they are the
// metadata-parse family CG-2 maps onto the 200 + warnings.other arm
// (Artifactory's Jackson parse errors are IOExceptions).
func parsePublishMeta(raw []byte) (*publishMeta, error) {
	var m publishMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("metadata frame is not valid JSON: %w", err)
	}
	if !validCrateName(m.Name) {
		return nil, fmt.Errorf("crate name %q is not a legal crate name (ASCII letter first, [A-Za-z0-9_-], <= %d characters)",
			m.Name, maxCrateNameLen)
	}
	if _, err := parseSemver(m.Vers); err != nil {
		return nil, fmt.Errorf("version %q is not valid SemVer 2.0: %w", m.Vers, err)
	}
	return &m, nil
}

// servePublish implements PUT api/v1/crates/new: deframe, validate, the
// crate + sidecar landings, the index rewrite, the 200 — with CG-2's
// dual-track failure face (see the package comment): processing failures
// ride warnings.other on a 200, permission failures keep 401/403, framing
// defects answer 500. There is no conflict arm: the duplicate-version
// decision is the service's repo-semantics section 3 gate (D-3 — an
// existing version the principal may delete is overwritten, one they may
// not is the 401/403 refusal).
//
// The ADDON GATE already ran at dispatchContent (write verb); the adapter
// never re-asks. A publish onto a remote repository dies at the class
// door in ServeHTTP; a publish onto a virtual repository is the M11
// routing ticket's, refused the same way.
func (h *Handler) servePublish(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string) {
	rawJSON, spoolPath, err := decodePublishFrame(r.Body)
	if err != nil {
		if errors.Is(err, errFramingDefect) {
			// CG-2 class 7: the uncaught-RuntimeException family. The exact
			// Jersey error page Artifactory's generic mapper renders is not
			// reverse-engineered (T-304's degraded declaration) — BinFlow
			// answers the errors envelope.
			writeEnvelope(w, http.StatusInternalServerError, err.Error())
			return
		}
		// CG-2 class 4: the body-read/IOException family — the Artifactory
		// catch absorbs these into the published(errors) body.
		writePublished(w, publishFailure(err))
		return
	}
	defer func() { _ = os.Remove(spoolPath) }()

	meta, err := parsePublishMeta(rawJSON)
	if err != nil {
		writePublished(w, publishFailure(err))
		return
	}
	// The protocol properties the landing writes must satisfy the closed
	// property rules (a >1KiB description would die inside Put AFTER the
	// blob drained — refuse here, on the parse family's 200 + warnings
	// face, with the property wording).
	if err := metadata.ValidatePropSet(crateProps(meta)); err != nil {
		writePublished(w, publishFailure(err))
		return
	}

	target := cratePath(meta.Name, meta.Vers)
	expect, err := declaredDigests(r.Header)
	if err != nil {
		// The X-Checksum-* family is BinFlow's own curl-deployment
		// extension, outside the cargo publish chain — it keeps its 400.
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}

	// The crate blob (content class; cargo clients declare no checksums —
	// the server measures, spec section 5.3). The service answers the
	// permission family (401 anonymous / 403) and the overwrite decision;
	// every other landing fault is the IOException track's 200 arm.
	crateFile, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		writePublished(w, publishFailure(fmt.Errorf("reopen spool: %w", err)))
		return
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, target, crateFile, expect,
		"application/octet-stream", repo.PutOptions{Properties: crateProps(meta)})
	_ = crateFile.Close()
	if err != nil {
		switch {
		case errors.Is(err, repo.ErrUnauthorized), errors.Is(err, repo.ErrForbidden):
			h.writeError(w, err, repoKey, target) // 401/403 + envelope (CG-2's permission track)
		case errors.Is(err, storage.ErrChecksumMismatch), errors.Is(err, repo.ErrQuotaExceeded),
			errors.Is(err, repo.ErrInvalidPath), errors.Is(err, errInvalidChecksum), errors.Is(err, metadata.ErrInvalidProperties):
			// BinFlow's own policy faces (checksum-contract, quota,
			// path/property rules) keep their 4xx — they are deployment
			// policy, not the publish processing chain Artifactory maps
			// onto the 200 arm.
			h.writeError(w, err, repoKey, target)
		default:
			writePublished(w, publishFailure(err)) // CG-2 class 5: the landing IOException family
		}
		return
	}

	// The post-landing faults ride warnings.other too: Artifactory's
	// publish has already answered by the time its async index work runs
	// (D-4), so no sidecar/rewrite fault may turn the landed crate into an
	// HTTP failure — the error string stays observable instead.
	failures := []string{}

	// The metadata sidecar: the raw frame verbatim (the index rewrite and
	// the property backfill read it back). Regenerable family — a later
	// import rewrites it without a delete-permission demand.
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, metaPath(meta.Name, meta.Vers),
		bytes.NewReader(rawJSON), blobRefOf(rawJSON), "application/json",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		slog.WarnContext(ctx, "cargo: metadata sidecar write failed",
			slog.String("repo", repoKey), slog.String("path", metaPath(meta.Name, meta.Vers)),
			slog.String("error", err.Error()))
		failures = append(failures, publishFailureString(fmt.Errorf("write the metadata sidecar: %w", err)))
	}

	// The index rewrite (spec section 3.3): the whole file, one line per
	// version, cksum = the storage-measured sha256.
	if err := h.rewriteIndex(ctx, p, repoKey, meta.Name); err != nil {
		slog.WarnContext(ctx, "cargo: index rewrite failed",
			slog.String("repo", repoKey), slog.String("crate", meta.Name),
			slog.String("error", err.Error()))
		failures = append(failures, publishFailureString(fmt.Errorf("index rewrite: %w", err)))
	}

	if node != nil && node.Sha256 != "" {
		w.Header().Set(hdrChecksumSha256, node.Sha256)
	}
	writePublished(w, failures)
}

// publishFailureString renders one error the Artifactory wording carries:
// "Failed to publish with error '<msg>'" (T-304 §3's anchored form).
func publishFailureString(err error) string {
	return fmt.Sprintf("Failed to publish with error '%v'", err)
}

// publishFailure wraps one error into the single-entry failure list.
func publishFailure(err error) []string {
	if err == nil {
		return nil
	}
	return []string{publishFailureString(err)}
}

// writePublished renders the publish outcome: 200 + the official
// warnings-only body, the processing-failure strings riding warnings.other
// (CG-2's exact wire — a top-level errors key NEVER appears: cargo 1.98
// reads its mere presence as failure, R-1).
func writePublished(w http.ResponseWriter, failures []string) {
	other := make([]any, len(failures))
	for i, f := range failures {
		other[i] = f
	}
	body, err := json.Marshal(map[string]any{
		"warnings": map[string]any{
			"invalid_categories": []any{},
			"invalid_badges":     []any{},
			"other":              other,
		},
	})
	if err != nil {
		// unreachable: flat string map
		body = []byte(`{"warnings":{"invalid_categories":[],"invalid_badges":[],"other":[]}}`)
	}
	writeJSON(w, http.StatusOK, body)
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
