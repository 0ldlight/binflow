package nuget

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves — the
// repositories.package_type value httpapi dispatches on. "nuget" (the Go
// package name may not carry a dot).
const Protocol = "nuget"

// hdrChecksum* is the content plane's client-checksum family (the same
// contract every adapter honors on PUT and download).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// BlobLedger is the read-only digest ledger the download headers consult
// (satisfied by metadata.Store.Blobs(); the goproxy seam verbatim).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// RemoteConfigReader resolves one remote repository's upstream base URL —
// the registration rewrite table's source (satisfied by
// metadata.Store.Remote()).
type RemoteConfigReader interface {
	GetConfig(ctx context.Context, repoKey string) (*metadata.RemoteConfig, error)
}

// Options tunes the handler (the npm Options posture).
type Options struct {
	// BaseURL is the externally visible origin (server.base_url). Empty
	// means derive from the request (X-Forwarded-Proto honored).
	BaseURL string
	// SpoolDir roots the package-push spool files (T-476, the T-474
	// family): a push body streams to disk first — the .nupkg reaches
	// hundreds of MB, and the zip validation plus the landing re-read the
	// staged bytes. Hardened deployments — read-only rootfs, the kubernetes
	// norm — mount no writable /tmp, which the OS-temp spool of the
	// pre-T-476 code assumed (the UAT nuget push incident: "spool upload:
	// open /tmp/binflow-nuget-*.nupkg: read-only file system" on a bare
	// 500). The cmd assembly passes <storage data_dir>/staging — the SAME
	// volume the blob store writes on. "" falls back to the OS temp dir
	// (the bare test-harness posture); every refusal names the root it
	// attempted (adapter.StagingDir/StageFile).
	SpoolDir string
}

// Handler is the NuGet protocol adapter. It owns the wire protocol only;
// every content operation goes through repo.Service — including the remote
// pull-through (svc.Get dispatches to the engine, whose upstream hop
// applies this protocol's path prefixes through the provider facet).
type Handler struct {
	svc    repo.Service
	repos  repo.ClassReader
	blobs  BlobLedger
	remote RemoteConfigReader
	opts   Options
	egress v3egressPool // the v3 search direct-egress clients (v3remote.go)
}

// New wires the handler. remote may be nil (the remote face's rewrite
// degrades to pass-through — a stack without the seam serves the cached
// bytes verbatim).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, remote RemoteConfigReader, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, remote: remote, opts: opts}
}

// Register builds the handler and enters both the handler registry and
// the metadata-provider registry under one literal (the pypi.Register
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, remote RemoteConfigReader, opts Options) *Handler {
	h := New(svc, repos, blobs, remote, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: the pilot serves all three
// classes — local in full, remote through the pull-through engine inside
// svc.Get (plus the marker-cached metadata documents), virtual through
// first-hit resolution plus this adapter's aggregations.
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler (see layout.go).
func (h *Handler) Layout(r *http.Request) (string, string, error) { return layout(r) }

// classOf resolves the repository class (the goproxy defensive arm).
func (h *Handler) classOf(ctx context.Context, repoKey string) (string, error) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return "", errRepoNotFound(repoKey)
		}
		return "", fmt.Errorf("load repository %s: %w", repoKey, err)
	}
	return row.Type, nil
}

// errRepoNotFound shapes the missing-repository refusal.
func errRepoNotFound(repoKey string) error {
	return fmt.Errorf("repository %s: %w", repoKey, repo.ErrRepoNotFound)
}

// ServeHTTP dispatches on the parsed wire target. Errors render as
// text/plain bodies (the writePlain posture — no client in the matrix
// parses a JSON error envelope on these faces; dotnet prints the body
// verbatim on push failures, so the wording is the operator surface).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	if rel == "" {
		// The repository-root probe: 200 with an empty body.
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the repository root")
		}
		return
	}

	rt, ok := parseRoute(rel)
	if !ok {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	class, err := h.classOf(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}

	switch rt.kind {
	case kindServiceIndex:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveServiceIndex(w, r, repoKey)
	case kindVersions:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveVersions(ctx, w, r, p, repoKey, class, rt)
	case kindPackageFile:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.servePackageFile(ctx, w, r, p, repoKey, class, rt)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on package files")
		}
	case kindPush, kindPushDirect:
		switch r.Method {
		case http.MethodPut:
			h.servePush(ctx, w, r, p, repoKey, class, rt)
		case http.MethodDelete:
			if rt.kind == kindPushDirect {
				w.Header().Set("Allow", "PUT")
				writePlain(w, http.StatusMethodNotAllowed, "the publish base carries no version to delete (address flatcontainer/<id>/<version>)")
				return
			}
			h.serveDelete(ctx, w, r, p, repoKey, class, rt)
		case http.MethodPost:
			// The official publish family's relist verb: BinFlow has no
			// listed bit (delete is a hard delete — the T-287 ruling), so
			// relist is an honest 405.
			w.Header().Set("Allow", "PUT, DELETE")
			writePlain(w, http.StatusMethodNotAllowed, "relist is not supported (delete is a hard delete on BinFlow)")
		default:
			w.Header().Set("Allow", "PUT, DELETE")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the publish target")
		}
	case kindRegistration:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRegistration(ctx, w, r, p, repoKey, class, rt)
	case kindRegistrationPage:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRegistrationPage(ctx, w, r, p, repoKey, class, rt)
	case kindRegistrationLeaf:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRegistrationLeaf(ctx, w, r, p, repoKey, class, rt)
	case kindSearch:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveSearch(ctx, w, r, p, repoKey, class)
	case kindV2ServiceDoc:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveV2ServiceDoc(w, r, repoKey)
		case http.MethodPut:
			h.serveV2Publish(ctx, w, r, p, repoKey, class, rt)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the v2 base")
		}
	case kindV2Metadata:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveV2Metadata(w, r, repoKey)
	case kindV2Search, kindV2FindPackages, kindV2Packages, kindV2GetUpdates:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveV2Collection(ctx, w, r, p, repoKey, class, rt)
	case kindV2Batch:
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on $batch")
			return
		}
		h.serveV2Batch(ctx, w, r, p, repoKey, class)
	case kindV2Download:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveV2Download(ctx, w, r, p, repoKey, class, rt)
	case kindV2BareNupkg:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveV2BareNupkg(ctx, w, r, p, repoKey, rt.path)
	case kindV2Push:
		switch r.Method {
		case http.MethodPut:
			h.serveV2Publish(ctx, w, r, p, repoKey, class, rt)
		case http.MethodDelete:
			h.serveV2Delete(ctx, w, p, repoKey, class, rt)
		default:
			// The path form carries no read face — the unknown-resource 404.
			writePlain(w, http.StatusNotFound, "not found")
		}
	case kindBareContent:
		h.serveBareContent(ctx, w, r, p, repoKey, rel)
	default:
		writePlain(w, http.StatusNotFound, "not found")
	}
}

// requireMethod renders the 405 (with Allow) unless the method matches;
// false means the request was refused and the caller returns.
func (h *Handler) requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on this route")
	return false
}

// writePlain renders one protocol error: text/plain, trailing newline,
// nosniff (the writePlainText posture — several messages interpolate
// client-supplied identifiers).
func writePlain(w http.ResponseWriter, status int, msg string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg+"\n") //nolint:gosec // G705: protocol error text under nosniff + text/plain
}

// writeJSON renders one JSON document with the shared success posture.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed JSON, never client bytes
}

// writeError maps service errors onto the protocol surface (the goproxy
// matrix verbatim — one shared mapping across the faces).
func (h *Handler) writeError(w http.ResponseWriter, err error, repoKey, path string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writePlain(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writePlain(w, http.StatusConflict, fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, path, err))
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writePlain(w, http.StatusNotFound, "not found")
	case errors.Is(err, repo.ErrRepoNotFound):
		writePlain(w, http.StatusNotFound, fmt.Sprintf("repository %s not found", repoKey))
	case errors.Is(err, repo.ErrInvalidPath), errors.Is(err, adapter.ErrInvalidChecksum), errors.Is(err, errInvalidPackage):
		writePlain(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writePlain(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrQuotaExceeded):
		writePlain(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writePlain(w, http.StatusBadRequest, err.Error())
	default:
		writePlain(w, http.StatusInternalServerError, err.Error())
	}
}

// baseURLFor resolves the absolute origin the generated documents cite
// (Options.BaseURL wins; otherwise the request's scheme+host, the npm
// packument posture).
func (h *Handler) baseURLFor(r *http.Request) string {
	if h.opts.BaseURL != "" {
		return strings.TrimRight(h.opts.BaseURL, "/")
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// apiBase is the per-repository protocol base:
// <origin>/binflow/api/nuget/<plane>/<repoKey>.
func apiBase(origin, plane, repoKey string) string {
	return origin + "/binflow/api/nuget/" + plane + "/" + repoKey
}

// flatBase is the flatcontainer resource base (trailing slash — the
// official resource spelling clients join against).
func flatBase(origin, repoKey string) string {
	return apiBase(origin, planeV3, repoKey) + "/" + segFlat + "/"
}

// regBase is the registration resource base (trailing slash).
func regBase(origin, repoKey string) string {
	return apiBase(origin, planeV3, repoKey) + "/" + segRegistration + "/"
}

// regSemVer2Base is the SemVer2 registration family's base (nuget.md
// section 9.1: RegistrationsBaseUrl/3.6.0|Versioned announce here).
func regSemVer2Base(origin, repoKey string) string {
	return apiBase(origin, planeV3, repoKey) + "/" + segRegistrationSemVer + "/"
}

// v2Base is the v2 feed base (no trailing slash).
func v2Base(origin, repoKey string) string {
	return apiBase(origin, planeV2, repoKey)
}

// contentTypeOfPackageFile maps the sidecar family onto wire content
// types (the official flatcontainer renderings).
func contentTypeOfPackageFile(file string) string {
	switch file {
	case "nupkg":
		return "application/octet-stream"
	case "sha512":
		return "text/plain; charset=utf-8"
	case "nuspec":
		return "application/xml"
	default:
		return "application/octet-stream"
	}
}

// digestTriple and digestsOf: the download-header family (goproxy
// verbatim — sha256 from the node, sha1/md5 from the ledger, degraded).
type digestTriple struct{ sha256, sha1, md5 string }

func (h *Handler) digestsOf(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.blobs == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.blobs.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// serveNode streams one stored node: hint merge, checksum headers,
// Content-Length, Content-Type, body suppressed on HEAD.
func (h *Handler) serveNode(ctx context.Context, w http.ResponseWriter, r *http.Request, node *metadata.Node, rc io.ReadSeekCloser, ctype string) {
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		for k, vv := range extra.ExtraHeaders() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	sums := h.digestsOf(ctx, node)
	hdr := w.Header()
	if sums.sha256 != "" {
		hdr.Set(hdrChecksumSha256, sums.sha256)
	}
	if sums.sha1 != "" {
		hdr.Set(hdrChecksumSha1, sums.sha1)
	}
	if sums.md5 != "" {
		hdr.Set(hdrChecksumMd5, sums.md5)
	}
	if node.Size > 0 {
		hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	}
	hdr.Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// declaredDigests parses the X-Checksum-* headers (the shared contract:
// malformed → adapter.ErrInvalidChecksum → 400; a well-formed
// disagreement is storage's 409).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := lowerASCII(trimSpaceASCII(hdr.Get(name)))
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
