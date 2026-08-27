package deb

// The virtual-repository face (debian.md section 8's virtual row, S10):
// the cross-member index aggregation. Downloads need nothing special —
// svc.Get on a virtual repository already walks the member order
// first-found (local members' nodes, remote members through the FR-20
// chain, X-Binflow-Resolved-From riding the reader). The AGGREGATION
// lives here, computed per request and never cached (the maven-metadata
// posture of repo-semantics section 8.3 — a virtual owns no storage in
// BinFlow, so there is nowhere to land a rendered aggregate anyway):
//
//   - dists/<dist>/Release: every member's own Release is read through
//     the ungated member seam; the checksum sections are the member's
//     file INVENTORY, so their Packages/Sources family paths union into
//     the set the virtual rebuilds. Each family re-renders merged, the
//     Release is recomputed at the virtual root with
//     Components/Architectures = the members' union, and the checksum
//     sections describe the VIRTUAL's own bytes (the section 7 server
//     obligation holds on the aggregate exactly as on a local render).
//   - dists/<dist>/<comp>/binary-<arch>/Packages[.ext] and
//     <comp>/source/Sources[.ext]: the members' copies merge at STANZA
//     level — first-seen member wins a row (see stanzaKey), each row
//     self-consistent with the first-found download walk that serves
//     its bytes. A member's copy reads in ANY served spelling (plain,
//     .gz, .xz, .bz2, .lzma — decoded on the fly: mirrors ship
//     differing compression sets, and the virtual re-renders its own).
//   - the signature family (InRelease / Release.gpg) and by-hash
//     addresses answer 404: a member's signature does not cover the
//     re-rendered aggregate, and the virtual keeps no by-hash history;
//     apt degrades to the canonical unsigned Release (trusted=yes) and
//     to canonical names (the official by-hash fallback). Registered.
//   - writes: debPUT rides the shared arms — the service routes a
//     configured defaultDeploymentRepo onto the member (the un-routed
//     C5 405 answers there), and the background recompute targets the
//     MEMBER (the index belongs to it). The generated family refuses
//     direct writes with DB-3's 403 (the aggregate is as server-owned
//     as the local engine's output); DELETE never propagates through a
//     virtual (RE-08).
//
// Member failures follow the npm/pypi/conan aggregation rule: an
// unfound member contributes nothing; a CLASSIFIED failure (the remote
// engine's 404-on-steroids family: hardFail 502, SSRF 400) is tolerated
// so one bad member cannot block the read — but an aggregate where
// NOTHING answered surfaces the remembered failure instead of masking
// it as a plain 404.

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- the index-target grammar (shapes under dists/ the aggregate serves) ----

// indexCompressions are the compression spellings an index family may
// carry on the wire (read side; the render side adds none beyond them).
var indexCompressions = []string{".gz", ".bz2", ".xz", ".lzma"}

// indexTarget is one parsed dists/ index address.
type indexTarget struct {
	dist      string // may itself contain '/' (wheezy/updates suites)
	comp      string // "" on the Release family
	arch      string // the binary-<arch> axis (Packages only)
	family    string // "Release" | "Packages" | "Sources"
	signature bool   // InRelease / Release.gpg (never served here)
	ext       string // "" | one of indexCompressions
}

// parseIndexTarget recognizes the aggregate-servable shapes on a
// decoded repo-relative path already routed kindIndex. ok=false for the
// rest (by-hash history, the legacy per-component Release, stray
// names): the caller answers the plain 404 apt degrades from.
func parseIndexTarget(rel string) (indexTarget, bool) {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[1:] {
		if seg == dirByHash {
			return indexTarget{}, false // the virtual keeps no digest history
		}
	}
	last := segs[len(segs)-1]
	var t indexTarget
	for _, ext := range indexCompressions {
		if strings.HasSuffix(last, ext) {
			t.ext, last = ext, strings.TrimSuffix(last, ext)
			break
		}
	}
	distOf := func(n int) string { return strings.Join(segs[1:len(segs)-n], "/") }
	switch last {
	case "InRelease", "Release.gpg":
		t.family, t.signature = "Release", true
		t.dist = distOf(1)
		return t, t.dist != ""
	case "Release":
		t.family = "Release"
		t.dist = distOf(1)
		return t, t.dist != ""
	case "Packages", "Sources":
	default:
		return indexTarget{}, false
	}
	if len(segs) < 4 {
		return indexTarget{}, false
	}
	t.family = last
	parent, comp := segs[len(segs)-2], segs[len(segs)-3]
	t.comp, t.dist = comp, distOf(3)
	if t.comp == "" || t.dist == "" {
		return indexTarget{}, false
	}
	switch {
	case last == "Packages" && strings.HasPrefix(parent, "binary-") && len(parent) > len("binary-"):
		t.arch = parent[len("binary-"):]
		return t, true
	case last == "Sources" && parent == archSource:
		return t, true
	default:
		return indexTarget{}, false
	}
}

// familyPath renders the target's index family path (the member-read
// address, compression-free).
func (t indexTarget) familyPath() string {
	switch t.family {
	case "Release":
		return dirDists + "/" + t.dist + "/Release"
	case "Packages":
		return dirDists + "/" + t.dist + "/" + t.comp + "/binary-" + t.arch + "/Packages"
	case "Sources":
		return dirDists + "/" + t.dist + "/" + t.comp + "/" + archSource + "/Sources"
	}
	return ""
}

// ---- the dispatch ----

// serveVirtual dispatches the content plane on a virtual repository.
func (h *Handler) serveVirtual(ctx context.Context, w http.ResponseWriter, r *http.Request,
	repoKey, rel string, rt route, props adapter.DeployProps) {
	switch rt.kind {
	case kindRoot:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	case kindIndex:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveVirtualIndex(ctx, w, r, repoKey, rel)
		default:
			// DB-3's posture on the aggregate: a hand-written index would
			// desync the recomputed Release exactly as on a local render.
			writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
		}
	case kindDeb:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "application/vnd.debian.binary-package")
		case http.MethodPut:
			h.serveUploadDeb(ctx, w, r, repoKey, rel, props, h.recomputeTarget(ctx, repoKey))
		case http.MethodDelete:
			h.refuseVirtualDelete(w, repoKey)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindDsc:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "text/plain; charset=utf-8")
		case http.MethodPut:
			h.serveUploadDsc(ctx, w, r, repoKey, rel, props, h.recomputeTarget(ctx, repoKey))
		case http.MethodDelete:
			h.refuseVirtualDelete(w, repoKey)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindDists, kindBare:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "application/octet-stream")
		case http.MethodPut:
			h.servePlainFile(ctx, w, r, repoKey, rel)
		case http.MethodDelete:
			h.refuseVirtualDelete(w, repoKey)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	default:
		writeText(w, http.StatusNotFound, "not found")
	}
}

// serveVirtualIndex serves the aggregate's index family (the shape
// grammar above; anything unservable is the plain 404).
func (h *Handler) serveVirtualIndex(ctx context.Context, w http.ResponseWriter, r *http.Request, virtualKey, rel string) {
	t, ok := parseIndexTarget(rel)
	if !ok || t.signature {
		writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", virtualKey, rel))
		return
	}
	if t.family == "Release" {
		h.serveVirtualRelease(ctx, w, r, virtualKey, t)
		return
	}
	h.serveVirtualFamily(ctx, w, r, virtualKey, t)
}

// ---- member reads (the aggregation seam) ----

// memberFile reads one member's copy of a path through the ungated
// member seam (the virtual read gate has already run). (nil, false,
// nil) = the member carries no such document; a classified failure
// keeps its rendering for the caller's tolerate-or-surface decision.
func (h *Handler) memberFile(ctx context.Context, virtualKey, member, path string) ([]byte, bool, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexReadBytes))
	if rerr != nil {
		return nil, false, fmt.Errorf("read member document %s/%s: %w", member, path, rerr)
	}
	return raw, true, nil
}

// memberIndexDoc reads one member's copy of an index FAMILY in any
// served spelling — plain first, then the compressed spellings decoded
// on the fly (mirrors differ in what they ship; the virtual re-renders
// its own set anyway, so the member's best spelling is merely the first
// present). A corrupt compressed copy contributes nothing (WARN, never
// a broken aggregate nor an honest-looking stanza set).
func (h *Handler) memberIndexDoc(ctx context.Context, virtualKey, member, familyPath string) ([]byte, bool, error) {
	for _, ext := range append([]string{""}, indexCompressions...) {
		raw, ok, err := h.memberFile(ctx, virtualKey, member, familyPath+ext)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			continue
		}
		if ext == "" {
			return raw, true, nil
		}
		out, derr := decompressIndex(ext, raw)
		if derr != nil {
			slog.WarnContext(ctx, "deb: virtual member index copy undecodable — skipped",
				slog.String("virtual", virtualKey), slog.String("member", member),
				slog.String("path", familyPath+ext), slog.String("error", derr.Error()))
			return nil, false, nil
		}
		return out, true, nil
	}
	return nil, false, nil
}

// decompressIndex decodes one compressed index spelling.
func decompressIndex(ext string, body []byte) ([]byte, error) {
	switch ext {
	case ".gz":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gzip: %w", err)
		}
		return io.ReadAll(io.LimitReader(zr, maxIndexReadBytes))
	case ".bz2":
		return io.ReadAll(io.LimitReader(bzip2.NewReader(bytes.NewReader(body)), maxIndexReadBytes))
	case ".xz":
		xr, err := xz.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("xz: %w", err)
		}
		return io.ReadAll(io.LimitReader(xr, maxIndexReadBytes))
	case ".lzma":
		lr, err := lzma.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("lzma: %w", err)
		}
		return io.ReadAll(io.LimitReader(lr, maxIndexReadBytes))
	default:
		return nil, fmt.Errorf("unknown index compression %q", ext)
	}
}

// ---- the stanza merge (S10) ----

// parseStanzas parses every paragraph of one Packages/Sources document
// off a single line reader (stanza after stanza — see
// readControlParagraph for why the reader must be shared).
func parseStanzas(body []byte) []*controlDoc {
	br := newLineReader(bytes.NewReader(body))
	var out []*controlDoc
	for {
		doc, err := readControlParagraph(br)
		if err != nil || doc == nil {
			return out // a trailing garbage tail keeps what parsed
		}
		out = append(out, doc)
	}
}

// stanzaIndexable reports whether a member stanza joins the merge: the
// identity fields the keying and apt both need.
func stanzaIndexable(doc *controlDoc) bool {
	return doc != nil && doc.Get("Package") != "" && doc.Get("Version") != ""
}

// stanzaKey is the merge identity — the S10 detail the spec marks
// unverified, ruled here as: Package + Version + Architecture + the
// DOWNLOAD ADDRESS (Filename on a Packages stanza, Directory on a
// Sources stanza). Keying on the address keeps every surviving row
// self-consistent with the first-found member walk that serves its
// bytes: two members carrying the same address keep the FIRST member's
// row (its checksums describe the file the walk actually serves), while
// the same package version under different pool paths stays as separate
// rows (each address resolves to its own member's copy).
func stanzaKey(doc *controlDoc) string {
	return strings.Join([]string{
		doc.Get("Package"), doc.Get("Version"), doc.Get("Architecture"),
		doc.Get("Filename"), doc.Get("Directory"),
	}, "\x00")
}

// renderStanzas renders the merged set verbatim: field order and
// continuation lines byte-faithful to the member's own rendering (the
// member stanzas are complete documents — nothing server-owned
// appends).
func renderStanzas(docs []*controlDoc) []byte {
	var b strings.Builder
	for _, doc := range docs {
		for _, f := range doc.fieldsOf() {
			fmt.Fprintf(&b, "%s: %s\n", f.Key, f.Value)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// aggregationFailure remembers the first classified member failure of
// one collection (tolerated, but never masked when nothing else
// answered — the npm/pypi/conan rule).
type aggregationFailure struct{ err *repo.StatusError }

// note keeps the FIRST classified failure; false means the error is the
// caller's honest 500.
func (a *aggregationFailure) note(err error) bool {
	var se *repo.StatusError
	if !errors.As(err, &se) {
		return false
	}
	if a.err == nil {
		a.err = se
	}
	return true
}

// tolerateVirtualMemberFailure logs one member's aggregation failure
// and moves on.
func tolerateVirtualMemberFailure(ctx context.Context, virtualKey, member, what string, err error) {
	slog.WarnContext(ctx, "deb: virtual member failed during aggregation (tolerated)",
		"virtual", virtualKey, "member", member, "what", what, "error", err.Error())
}

// virtualAggregate merges one index family across the member order:
// every member's copy (any spelling) parses, the stanzas key-dedupe
// first-seen, and found reports whether ANY member carried the family.
// An aggregate where nothing answered surfaces the remembered failure.
func (h *Handler) virtualAggregate(ctx context.Context, virtualKey string, order []repo.VirtualMember, familyPath string) ([]*controlDoc, bool, error) {
	var merged []*controlDoc
	var failure aggregationFailure
	seen := map[string]bool{}
	found := false
	for _, m := range order {
		raw, ok, err := h.memberIndexDoc(ctx, virtualKey, m.Key, familyPath)
		if err != nil {
			if failure.note(err) {
				tolerateVirtualMemberFailure(ctx, virtualKey, m.Key, familyPath, err)
				continue
			}
			return nil, false, err
		}
		if !ok {
			continue
		}
		found = true
		for _, doc := range parseStanzas(raw) {
			if !stanzaIndexable(doc) {
				continue
			}
			key := stanzaKey(doc)
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, doc)
		}
	}
	if !found && failure.err != nil {
		return nil, false, failure.err
	}
	return merged, found, nil
}

// ---- the Release aggregate ----

// releaseFamilies parses one member Release's checksum sections into
// the set of index FAMILY paths it serves (relative to dists/<dist>/,
// compression suffixes stripped, the non-stanza families — i18n,
// dep11, cnf, Contents — filtered: the aggregate rebuilds only
// Packages/Sources, and the virtual's own Release lists only what it
// serves).
func releaseFamilies(release []byte) map[string]bool {
	families := map[string]bool{}
	inSection := false
	for _, ln := range strings.Split(string(release), "\n") {
		switch {
		case ln == "":
			continue
		case ln[0] == ' ' || ln[0] == '\t':
			if !inSection {
				continue
			}
			fields := strings.Fields(ln)
			if len(fields) != 3 {
				continue
			}
			if fam := indexFamilyOf(fields[2]); fam != "" {
				families[fam] = true
			}
		default:
			inSection = strings.HasSuffix(ln, ":") // MD5Sum: / SHA1: / SHA256:
		}
	}
	return families
}

// indexFamilyOf recognizes `<comp>/binary-<arch>/Packages` and
// `<comp>/source/Sources` (any compression suffix) — the families the
// aggregate rebuilds; everything else answers "".
func indexFamilyOf(path string) string {
	stem := path
	for _, ext := range indexCompressions {
		if strings.HasSuffix(stem, ext) {
			stem = strings.TrimSuffix(stem, ext)
			break
		}
	}
	segs := strings.Split(stem, "/")
	if len(segs) != 3 {
		return ""
	}
	switch {
	case segs[2] == "Packages" && len(segs[1]) > len("binary-") && strings.HasPrefix(segs[1], "binary-"):
		return stem
	case segs[2] == "Sources" && segs[1] == archSource:
		return stem
	default:
		return ""
	}
}

// familyAxes splits the family set into the Release lines' Components
// and Architectures values (the members' union; pseudo architectures
// filter per section 4.1).
func familyAxes(families []string) (comps, arches []string) {
	compSet, archSet := map[string]bool{}, map[string]bool{}
	for _, fam := range families {
		segs := strings.Split(fam, "/")
		compSet[segs[0]] = true
		if arch, ok := strings.CutPrefix(segs[1], "binary-"); ok && arch != "" {
			archSet[arch] = true
		}
	}
	return sortedKeys(compSet), archLine(sortedKeys(archSet), nil)
}

// serveVirtualRelease recomputes the meta-index at the virtual root:
// the members' Release inventories union into the family set, every
// family re-renders merged (plus the compression set the VIRTUAL's own
// deb section configures — plain + .gz mandatory), and the checksum
// sections describe the aggregate's own bytes. Unsigned by
// construction (the registered no-signature posture) — no
// Acquire-By-Hash line either: the virtual keeps no digest history.
func (h *Handler) serveVirtualRelease(ctx context.Context, w http.ResponseWriter, r *http.Request, virtualKey string, t indexTarget) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, t.familyPath())
		return
	}
	releasePath := t.familyPath()
	familySet := map[string]bool{}
	contributed := 0
	var failure aggregationFailure
	for _, m := range order {
		raw, ok, merr := h.memberFile(ctx, virtualKey, m.Key, releasePath)
		if merr != nil {
			if failure.note(merr) {
				tolerateVirtualMemberFailure(ctx, virtualKey, m.Key, releasePath, merr)
				continue
			}
			h.writeError(w, merr, virtualKey, releasePath)
			return
		}
		if !ok {
			continue // the member does not serve this distribution
		}
		contributed++
		for fam := range releaseFamilies(raw) {
			familySet[fam] = true
		}
	}
	if contributed == 0 {
		if failure.err != nil {
			h.writeError(w, failure.err, virtualKey, releasePath)
			return
		}
		writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", virtualKey, releasePath))
		return
	}

	// The virtual's own deb section feeds the compression set and the
	// Origin/Label fields (defaults: the repository key, as the local
	// renderer's).
	cfg, err := h.configFor(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, releasePath)
		return
	}

	families := sortedKeys(familySet)
	var files []indexFile
	for _, fam := range families {
		docs, found, aerr := h.virtualAggregate(ctx, virtualKey, order, dirDists+"/"+t.dist+"/"+fam)
		if aerr != nil {
			h.writeError(w, aerr, virtualKey, releasePath)
			return
		}
		if !found {
			continue // an inventory/file drift mid-aggregation: the family simply does not serve
		}
		body := renderStanzas(docs)
		files = append(files,
			newIndexFile(fam, body, indexContentType(fam)),
			newIndexFile(fam+".gz", gzipBody(body), indexContentType(fam+".gz")),
		)
		for _, name := range cfg.OptionalIndexCompressionFormats {
			files = append(files, newIndexFile(fam+"."+name, companionBody(name, body), indexContentType(fam+"."+name)))
		}
	}
	comps, arches := familyAxes(families)
	origin, label := cfg.Origin, cfg.Label
	if origin == "" {
		origin = virtualKey
	}
	if label == "" {
		label = virtualKey
	}
	body := renderReleaseBody(releaseDoc{
		dist:       t.dist,
		components: comps,
		arches:     arches,
		policy:     byHashNone, // no by-hash history on the aggregate (registered)
		origin:     origin,
		label:      label,
		date:       h.now(),
		files:      files,
	})
	h.writeRendered(w, r, body, indexContentType("Release"))
	slog.DebugContext(ctx, "deb: virtual Release rendered",
		"virtual", virtualKey, "dist", t.dist, "families", len(families), "members", contributed)
}

// ---- the Packages/Sources aggregate ----

// serveVirtualFamily merges one binary/source index across the members
// and serves it in the requested spelling (any compression the member
// READERS know renders on demand; the Release lists the enabled set,
// and apt fetches only what the Release lists).
func (h *Handler) serveVirtualFamily(ctx context.Context, w http.ResponseWriter, r *http.Request, virtualKey string, t indexTarget) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, t.familyPath())
		return
	}
	familyPath := t.familyPath()
	docs, found, aerr := h.virtualAggregate(ctx, virtualKey, order, familyPath)
	if aerr != nil {
		h.writeError(w, aerr, virtualKey, familyPath)
		return
	}
	if !found {
		writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", virtualKey, familyPath))
		return
	}
	body := renderStanzas(docs)
	switch t.ext {
	case "":
		h.writeRendered(w, r, body, indexContentType(t.family))
	case ".gz":
		h.writeRendered(w, r, gzipBody(body), indexContentType(t.family+".gz"))
	case ".xz":
		h.writeRendered(w, r, xzBody(body), indexContentType(t.family+".xz"))
	case ".lzma":
		h.writeRendered(w, r, lzmaBody(body), indexContentType(t.family+".lzma"))
	default:
		// ".bz2" — the registered writer gap: the family serves, this
		// spelling does not (the virtual's Release never lists it).
		writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", virtualKey, familyPath+t.ext))
	}
}

// writeRendered streams one in-memory rendered body: the checksum
// header family, Content-Length and Content-Type, body suppressed on
// HEAD.
func (h *Handler) writeRendered(w http.ResponseWriter, r *http.Request, body []byte, ctype string) {
	hdr := w.Header()
	hdr.Set(hdrChecksumSha256, sha256OfBody(body))
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("Content-Type", ctype)
	hdr.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body) //nolint:gosec // G304-class: server-rendered bytes, never client input
}

// sha256OfBody digests one rendered body (the X-Checksum-Sha256 of the
// aggregate faces — the download-header family's member).
func sha256OfBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ---- the write family ----

// refuseVirtualDelete answers DELETE on a virtual repository: deletes
// never propagate through the member resolution (RE-08), the wording
// keyed on whether a write route exists (the repo package's own
// refusal, restated — the conan posture).
func (h *Handler) refuseVirtualDelete(w http.ResponseWriter, repoKey string) {
	w.Header().Set("Allow", http.MethodGet)
	routed := false
	if h.repos != nil {
		if row, err := h.repos.Get(context.Background(), repoKey); err == nil {
			routed = debRouteTarget(row.Config) != ""
		}
	}
	msg := fmt.Sprintf("No local repository was configured as local deployment repository for the (%s) virtual repository.", repoKey)
	if routed {
		msg = fmt.Sprintf("Deletes are not propagated through the virtual repository '%s'; delete the artifact in its member repository directly.", repoKey)
	}
	writeText(w, http.StatusMethodNotAllowed, msg)
}

// debRouteTarget is the tolerant write-route probe of a virtual
// repository's config JSON (the repo package's own reader restated —
// adapter packages share no unexported code, the area rule): the
// primary spelling plus the two Artifactory aliases raw-seeded rows may
// carry.
func debRouteTarget(config string) string {
	var probe struct {
		DefaultDeploymentRepo    string `json:"defaultDeploymentRepo"`
		DefaultDeploymentRepoRef string `json:"defaultDeploymentRepoRef"`
		DeploymentRepository     string `json:"deploymentRepository"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return ""
	}
	for _, alias := range []string{probe.DefaultDeploymentRepo, probe.DefaultDeploymentRepoRef, probe.DeploymentRepository} {
		if alias != "" {
			return alias
		}
	}
	return ""
}

// recomputeTarget resolves the index-recompute target of a virtual
// write: the write-routed MEMBER (the service routes the landing itself;
// the index belongs to the member). An un-routed repository never
// reaches a recompute (the service's Put refuses the write first); a
// probe that drifts falls back to the virtual key, where the recompute
// harmlessly no-ops (the virtual class collects no coordinates).
func (h *Handler) recomputeTarget(ctx context.Context, virtualKey string) string {
	if h.repos == nil {
		return virtualKey
	}
	row, err := h.repos.Get(ctx, virtualKey)
	if err != nil {
		return virtualKey
	}
	if target := debRouteTarget(row.Config); target != "" {
		return target
	}
	return virtualKey
}
