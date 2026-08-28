package repo

// The archive operations family (M12 T-343, FR-105.3; the behavior spec is
// docs/reverse/repo-operations.md sections 2/3/4). Three faces share this
// file and the Q4-final entitlement slot (addons.RepoOperations — gated at
// the httpapi handlers, never here):
//
//	folder download   GET /api/archive/download/{repo}[/{path}]
//	                   (section 2: folderDownloadConfig, the §2.2 execution
//	                   order, streaming zip/tar/tar.gz/tgz, never staged)
//	member read       GET /{repo}/{archive}!/{entry}
//	                   (section 3: the FIRST "!/" split, nested recursion,
//	                   member checksum suffixes, pure streaming)
//	exploded upload   PUT /{repo}/{path} + X-Explode-Archive[-Atomic]: true
//	                   (section 4: the four-extension whitelist, per-entry
//	                   deploy through Put, the archive itself never lands)
//
// The family reuses the T-339 posture: capability faces on the concrete
// service (asserted by consumers, never added to the big Service
// interface), the spec's verbatim messages as *StatusError, and audit rows
// through the same best-effort seam.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"  //nolint:gosec // G501: compatibility digest only (ADR-0003, storage/digest.go precedent)
	"crypto/sha1" //nolint:gosec // G505: compatibility digest only (ADR-0003, storage/digest.go precedent)
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// AuditActionExplode is the exploded-upload family's op-level audit action
// (the AuditActionCopy precedent of T-339: spelled under the domain facade;
// joining audit.Actions()' picker list is the audit owner's one-liner). The
// per-entry deploys audit their own artifact.deploy rows through Put — this
// row records the operation as a whole.
const AuditActionExplode = "artifact.explode"

// The archiveType values the folder download accepts (section 0 row 5:
// REQUIRED, the enum zip/tar/tar.gz/tgz — no default, no aliasing on the
// wire; "tgz" and "tar.gz" are two spellings of one format).
const (
	archiveFormatZip    = "zip"
	archiveFormatTar    = "tar"
	archiveFormatTgz    = "tar.gz"
	archiveFormatTgzAlt = "tgz"
)

// archiveDownloadFormats maps the archiveType enum onto the response
// Content-Type (application/gzip for both tar.gz spellings — BinFlow's
// pick; the spec fixes no media types, registered).
var archiveDownloadFormats = map[string]string{
	archiveFormatZip:    "application/zip",
	archiveFormatTar:    "application/x-tar",
	archiveFormatTgz:    "application/gzip",
	archiveFormatTgzAlt: "application/gzip",
}

// mbBytes is the MB unit of maxDownloadSizeMb (binary MB — the JFrog
// convention; registered BinFlow reading, the spec carries no divisor).
const mbBytes = 1024 * 1024

// FolderDownloadConfig is section 2.1's folderDownloadConfig: six fields,
// every default verbatim from the spec table.
type FolderDownloadConfig struct {
	// Enabled is the master switch; off answers 403 "Download Folder
	// functionality is disabled." for every path (after the anonymous and
	// qualification steps — the §2.2 order).
	Enabled bool
	// EnabledForAnonymous is the anonymous sub-switch; an anonymous caller
	// with it off answers 401 before anything else runs.
	EnabledForAnonymous bool
	// MaxDownloadSizeMb bounds the subtree's total file size; 0 = unlimited.
	MaxDownloadSizeMb int64
	// MaxFiles bounds the subtree's file count; 0 = unlimited.
	MaxFiles int
	// MaxConcurrentRequests bounds in-flight folder downloads; 0 =
	// unlimited. A full slot answers the §2.2 step-8 400.
	MaxConcurrentRequests int
	// EnabledEmptyDirectories includes empty folder rows in the archive.
	EnabledEmptyDirectories bool
}

// defaultFolderDownloadConfig is the §2.1 column: every knob off/limited
// exactly as Artifactory ships it (enabled false, anonymous false, 1024MB,
// 5000 files, 10 concurrent, empty directories false).
var defaultFolderDownloadConfig = FolderDownloadConfig{
	Enabled:                 false,
	EnabledForAnonymous:     false,
	MaxDownloadSizeMb:       1024,
	MaxFiles:                5000,
	MaxConcurrentRequests:   10,
	EnabledEmptyDirectories: false,
}

// DefaultFolderDownloadConfig returns the spec-default configuration (the
// value every service starts with).
func DefaultFolderDownloadConfig() FolderDownloadConfig { return defaultFolderDownloadConfig }

// ConfigureFolderDownload replaces the folder-download configuration on a
// Service built by New/NewWithClock (the AttachReplicator posture: assembly
// or tests call it before the first request is served). BinFlow has no
// hot-reload config plane, so a change is restart-or-pre-serve effective —
// the §2.1 reload clause is a REGISTERED divergence, not a silent drop.
func ConfigureFolderDownload(s Service, cfg FolderDownloadConfig) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: ConfigureFolderDownload: service is not the concrete implementation; configuration not applied")
		return
	}
	impl.setFolderDownloadConfig(cfg)
}

// setFolderDownloadConfig installs the configuration and rebuilds the
// concurrency semaphore.
func (s *service) setFolderDownloadConfig(cfg FolderDownloadConfig) {
	s.folderMu.Lock()
	defer s.folderMu.Unlock()
	s.folderCfg = cfg
	if cfg.MaxConcurrentRequests > 0 {
		s.folderSlots = make(chan struct{}, cfg.MaxConcurrentRequests)
	} else {
		s.folderSlots = nil
	}
}

// folderConfig snapshots the live configuration.
func (s *service) folderConfig() FolderDownloadConfig {
	s.folderMu.Lock()
	defer s.folderMu.Unlock()
	return s.folderCfg
}

// acquireFolderSlot takes one concurrency slot (§2.2 step 8). false means
// the slot set is full — the caller answers the step-8 400. A nil channel
// (unlimited) always passes.
func (s *service) acquireFolderSlot() (release func(), ok bool) {
	s.folderMu.Lock()
	ch := s.folderSlots
	s.folderMu.Unlock()
	if ch == nil {
		return func() {}, true
	}
	select {
	case ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() { <-ch })
		}, true
	default:
		return nil, false
	}
}

// ---- section 2: folder download ----

// ArchiveDownloadRequest is the parsed addressing of one folder download.
type ArchiveDownloadRequest struct {
	// RepoKey and Path address the subtree root; Path "" is the whole
	// repository (section 0 row 6's empty-string form).
	RepoKey string
	Path    string
	// Type is the REQUIRED archiveType query value (zip/tar/tar.gz/tgz).
	Type string
	// IncludeChecksumFiles additionally emits .sha1/.md5/.sha256 companion
	// entries derived from the blobs ledger. BinFlow has no sidecar FILES
	// (ADR-0006: checksums are ledger facts), so the flag GENERATES the
	// entries Artifactory's "按仓 checksum 策略" would pick off disk — the
	// registered BinFlow semantics for that clause.
	IncludeChecksumFiles bool
}

// ArchiveDownloadResult is the streaming archive. The body releases the
// concurrency slot on Close — the handler MUST close it.
type ArchiveDownloadResult struct {
	Body        io.ReadCloser
	ContentType string
	// Filename is the suggested download name (<root>.<ext>).
	Filename string
	// Files and Bytes are the §2.2 step-7 statistics (files only).
	Files int
	Bytes int64
}

// ArchiveDownload implements the folder-download face (section 2): the
// §2.2 execution order verbatim — anonymous gate, archiveType parse,
// repository qualification, path qualification, read permission, master
// switch, size/file pre-statistics, concurrency slot — then the streaming
// pack (step 9): entries walk the subtree in path order, blobs stream
// straight from the engine, nothing is staged on disk.
func (s *service) ArchiveDownload(ctx context.Context, p *Principal, req ArchiveDownloadRequest) (*ArchiveDownloadResult, error) {
	cfg := s.folderConfig()

	// Step 1: the anonymous gate (first — the §2.2 order).
	if p == nil && !cfg.EnabledForAnonymous {
		return nil, &StatusError{
			Code:    http.StatusUnauthorized,
			Message: "You must be logged in to download a folder or repository.",
			cause:   fmt.Errorf("folder download: %w", ErrUnauthorized),
		}
	}

	// Step 2: archiveType is required and enum-bounded.
	contentType, ok := archiveDownloadFormats[req.Type]
	if !ok {
		return nil, &StatusError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("Unsupported archive type: '%s' of possible types : 'zip, tar, tar.gz, tgz'", req.Type),
			cause:   fmt.Errorf("folder download archiveType %q: %w", req.Type, ErrInvalidPath),
		}
	}

	// Step 3: repository qualification — local only. BinFlow has no cache
	// rclass (the remote cache lives in the remote's own namespace), so
	// remote and virtual both answer the spec's non-local 404.
	row, err := s.loadRepoRow(ctx, req.RepoKey)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			return nil, &StatusError{
				Code:    http.StatusNotFound,
				Message: req.RepoKey + " is not a repository.",
				cause:   err,
			}
		}
		return nil, err
	}
	if row.Type != TypeLocal {
		return nil, &StatusError{
			Code:    http.StatusNotFound,
			Message: "Downloading a folder or a repository's root is only available for local (or cache) repositories",
			cause:   fmt.Errorf("folder download on a %s repository: %w", row.Type, ErrRepoTypeNotSupported),
		}
	}

	// Step 4: path qualification. The root ("") always passes; a non-root
	// path must be a FOLDER row (a file row is the 400, anything else the
	// 404 — both verbatim).
	root := strings.Trim(req.Path, "/")
	if root != "" {
		if cmHasFileRow(ctx, s, req.RepoKey, root) {
			return nil, &StatusError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("Path '%s/%s' is not a folder, aborting folder download", req.RepoKey, root),
				cause:   fmt.Errorf("folder download target is a file: %w", ErrInvalidPath),
			}
		}
		if !cmHasFolderRow(ctx, s, req.RepoKey, root) {
			return nil, &StatusError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("Path '%s/%s' does not exist, aborting folder download", req.RepoKey, root),
				cause:   fmt.Errorf("node %s/%s: %w", req.RepoKey, root+"/", ErrNodeNotFound),
			}
		}
	}

	// Step 5: read permission on the addressed path (an anonymous caller
	// that passed step 1 is judged here like any principal).
	if !s.allow(ctx, p, req.RepoKey, root, ActionRead) {
		return nil, &StatusError{
			Code:    http.StatusForbidden,
			Message: fmt.Sprintf("You don't have the required permissions to download %s.", archiveDisplayPath(req.RepoKey, root)),
			cause:   fmt.Errorf("read %s/%s: %w", req.RepoKey, root, ErrForbidden),
		}
	}

	// Step 6: the master switch.
	if !cfg.Enabled {
		return nil, &StatusError{
			Code:    http.StatusForbidden,
			Message: "Download Folder functionality is disabled.",
			cause:   fmt.Errorf("folder download disabled: %w", ErrForbidden),
		}
	}

	// The subtree listing feeds both the step-7 statistics and the step-9
	// walk (path-ordered, the store's contract).
	rows, err := s.listSubtree(ctx, req.RepoKey, root)
	if err != nil {
		return nil, err
	}
	files := 0
	var total int64
	for _, n := range rows {
		if !isFolderNode(n.Path) {
			files++
			total += n.Size
		}
	}

	// Step 7: the size and file-count pre-statistics (two-decimal MB, both
	// messages verbatim).
	if cfg.MaxFiles > 0 && files > cfg.MaxFiles {
		return nil, &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("Number of files under the path '%s' (%d) exceeds the max allowed file count for folder download (%d).",
				archiveDisplayPath(req.RepoKey, root), files, cfg.MaxFiles),
			cause: fmt.Errorf("folder download file count %d > %d: %w", files, cfg.MaxFiles, ErrInvalidPath),
		}
	}
	if cfg.MaxDownloadSizeMb > 0 && total > cfg.MaxDownloadSizeMb*mbBytes {
		return nil, &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("Size of path '%s' (%.2fMB) exceeds the max allowed folder download size (%dMB).",
				archiveDisplayPath(req.RepoKey, root), float64(total)/mbBytes, cfg.MaxDownloadSizeMb),
			cause: fmt.Errorf("folder download size %d > %dMB: %w", total, cfg.MaxDownloadSizeMb, ErrInvalidPath),
		}
	}

	// Step 8: the concurrency slot.
	release, ok := s.acquireFolderSlot()
	if !ok {
		return nil, &StatusError{
			Code:    http.StatusBadRequest,
			Message: "There are too many folder download requests currently running. Try again later.",
			cause:   fmt.Errorf("folder download concurrency slots full: %w", ErrInvalidPath),
		}
	}

	// Step 9: the streaming pack. One goroutine walks the plan and writes
	// the archive into the pipe; the handler streams it out chunked. A
	// mid-stream fault aborts the pipe — the status line is committed by
	// then, the honest cost of the spec's no-staging rule.
	pr, pw := io.Pipe()
	go func() {
		if err := s.writeArchiveStream(ctx, pw, req, rows); err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				// The consumer closed the body: a client abort, not a
				// server fault (the disconnect-demotion posture).
				slog.InfoContext(ctx, "repo: folder download aborted by client",
					"repo", req.RepoKey, "path", root)
			} else {
				slog.ErrorContext(ctx, "repo: folder download stream failed",
					"repo", req.RepoKey, "path", root, "error", err.Error())
			}
			_ = pw.CloseWithError(fmt.Errorf("folder download stream: %w", err))
			return
		}
		_ = pw.Close()
	}()

	name := req.RepoKey
	if root != "" {
		name = path.Base(root)
	}
	ext := req.Type
	if ext == archiveFormatTgzAlt {
		ext = archiveFormatTgz
	}
	s.audit(ctx, AuditEvent{
		Actor: actor(p), Action: AuditActionDownload, Repo: req.RepoKey, Path: root,
		Detail: fmt.Sprintf(`{"archiveType":%q,"files":%d,"bytes":%d}`, req.Type, files, total),
	})
	return &ArchiveDownloadResult{
		Body:        &slotReleasingReader{r: pr, release: release},
		ContentType: contentType,
		Filename:    name + "." + ext,
		Files:       files,
		Bytes:       total,
	}, nil
}

// archiveDisplayPath renders the "repo/path" clause the §2.2 messages carry
// (root spelled as the bare repository key).
func archiveDisplayPath(repoKey, root string) string {
	if root == "" {
		return repoKey
	}
	return repoKey + "/" + root
}

// listSubtree returns the path-ordered node rows of one folder root: the
// folder row itself plus everything beneath it ("" = the whole repository).
func (s *service) listSubtree(ctx context.Context, repoKey, root string) ([]*metadata.Node, error) {
	all, err := s.md.Nodes().ListByPrefix(ctx, repoKey, root)
	if err != nil {
		return nil, fmt.Errorf("list %s/%s: %w", repoKey, root, err)
	}
	if root == "" {
		return all, nil
	}
	rows := make([]*metadata.Node, 0, len(all))
	for _, n := range all {
		if n.Path == root+"/" || strings.HasPrefix(n.Path, root+"/") {
			rows = append(rows, n)
		}
	}
	return rows, nil
}

// folderPlanEntry is one planned archive entry: the node it comes from
// (nil for generated checksum companions), the entry name, and the dir/
// checksum markers.
type folderPlanEntry struct {
	name string
	node *metadata.Node
	// checksumText, when non-empty, is the whole entry body (the generated
	// checksum companions).
	checksumText string
	dir          bool
}

// archiveEntryPlan flattens the rows into the entry sequence: nodes in path
// order with the generated checksum companions right after their file when
// includeChecksumFiles asked for them. Folder rows ride the plan; the
// writers filter them on the empty-directories knob.
func (s *service) archiveEntryPlan(ctx context.Context, req ArchiveDownloadRequest, rows []*metadata.Node) ([]folderPlanEntry, error) {
	plan := make([]folderPlanEntry, 0, 2*len(rows)+1)
	for _, n := range rows {
		if isFolderNode(n.Path) {
			plan = append(plan, folderPlanEntry{name: n.Path, node: n, dir: true})
			continue
		}
		plan = append(plan, folderPlanEntry{name: n.Path, node: n})
		if !req.IncludeChecksumFiles {
			continue
		}
		b, err := s.md.Blobs().Get(ctx, n.Sha256)
		if err != nil {
			return nil, fmt.Errorf("ledger blob %s: %w", n.Sha256, err)
		}
		for _, c := range []struct{ ext, val string }{
			{"sha1", b.Sha1}, {"md5", b.Md5}, {"sha256", n.Sha256},
		} {
			if c.val == "" {
				continue
			}
			plan = append(plan, folderPlanEntry{name: n.Path + "." + c.ext, checksumText: c.val})
		}
	}
	return plan, nil
}

// writeArchiveStream packs the plan into w. Blob opens happen here, inside
// the stream goroutine (nothing is staged).
func (s *service) writeArchiveStream(ctx context.Context, w io.Writer, req ArchiveDownloadRequest, rows []*metadata.Node) error {
	emptyDirs := s.folderConfig().EnabledEmptyDirectories
	plan, err := s.archiveEntryPlan(ctx, req, rows)
	if err != nil {
		return err
	}
	switch req.Type {
	case archiveFormatZip:
		return writeZipStream(ctx, s, w, plan, emptyDirs)
	case archiveFormatTar, archiveFormatTgz, archiveFormatTgzAlt:
		return writeTarStream(ctx, s, w, req.Type, plan, emptyDirs)
	default:
		return fmt.Errorf("folder download: unknown archive type %q", req.Type)
	}
}

// writeZipStream writes the zip flavor.
func writeZipStream(ctx context.Context, s *service, w io.Writer, plan []folderPlanEntry, emptyDirs bool) error {
	zw := zip.NewWriter(w)
	for _, e := range plan {
		if e.dir && !emptyDirs {
			continue
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if t := parseArchiveStamp(e.node); !t.IsZero() {
			hdr.Modified = t
		}
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return fmt.Errorf("zip header %s: %w", e.name, err)
		}
		if err := writeFolderEntryBody(ctx, s, fw, e); err != nil {
			return err
		}
	}
	return zw.Close()
}

// writeTarStream writes the tar and tar.gz/tgz flavors.
func writeTarStream(ctx context.Context, s *service, w io.Writer, kind string, plan []folderPlanEntry, emptyDirs bool) error {
	out := w
	if kind != archiveFormatTar {
		gz := gzip.NewWriter(w)
		defer gz.Close() //nolint:errcheck // flushes the footer after tw.Close below
		out = gz
	}
	tw := tar.NewWriter(out)
	for _, e := range plan {
		if e.dir && !emptyDirs {
			continue
		}
		hdr := &tar.Header{Name: e.name, Mode: 0o644}
		if e.dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
		} else {
			hdr.Size = int64(len(e.checksumText))
			if e.node != nil {
				hdr.Size = e.node.Size
			}
		}
		if t := parseArchiveStamp(e.node); !t.IsZero() {
			hdr.ModTime = t
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("tar header %s: %w", e.name, err)
		}
		if err := writeFolderEntryBody(ctx, s, tw, e); err != nil {
			return err
		}
	}
	return tw.Close()
}

// writeFolderEntryBody streams one planned entry's body into the archive
// writer: the generated checksum text, or the blob behind the node.
func writeFolderEntryBody(ctx context.Context, s *service, w io.Writer, e folderPlanEntry) error {
	if e.checksumText != "" {
		if _, err := io.WriteString(w, e.checksumText); err != nil {
			return fmt.Errorf("write checksum entry %s: %w", e.name, err)
		}
		return nil
	}
	if e.dir || e.node == nil {
		return nil
	}
	rc, _, err := s.st.Open(ctx, e.node.Sha256)
	if err != nil {
		return fmt.Errorf("open blob %s for %s: %w", e.node.Sha256, e.name, err)
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	if _, err := io.Copy(w, rc); err != nil {
		return fmt.Errorf("stream %s: %w", e.name, err)
	}
	return nil
}

// parseArchiveStamp parses a node timestamp for the archive headers'
// modified field (zero on failure — an unset stamp simply stays absent).
func parseArchiveStamp(n *metadata.Node) time.Time {
	if n == nil {
		return time.Time{}
	}
	stamp := n.UpdatedAt
	if stamp == "" {
		stamp = n.CreatedAt
	}
	if stamp == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}
	}
	return t
}

// slotReleasingReader couples the pipe reader with the concurrency slot's
// release (exactly once, on Close).
type slotReleasingReader struct {
	r       *io.PipeReader
	release func()
	once    sync.Once
}

func (b *slotReleasingReader) Read(p []byte) (int, error) { return b.r.Read(p) }

func (b *slotReleasingReader) Close() error {
	err := b.r.Close()
	b.once.Do(b.release)
	return err
}

// ---- section 3: archive!/ member reads ----

// The extension families the two readers understand.
//
// The READ set (§3.1: the mimetypes archive extension family) carries the
// zip container aliases — jar/war/ear/nupkg/conda/apk are zip files — plus
// the tar family. The bz2/xz/7z families stay unsupported in BinFlow (V-4
// open, no stdlib reader): their spellings answer the
// unsupported-extension 400, a REGISTERED divergence from the mimetypes
// full set.
//
// The EXPLODE whitelist (§4.1) is the PRD-anchored four-type closed set —
// the two-evidence intersection (docs say four, code defaults nine).
var (
	zipFamilyExts = map[string]bool{
		"zip": true, "jar": true, "war": true, "ear": true,
		"nupkg": true, "conda": true, "apk": true,
	}
	tarFamilyExts   = map[string]bool{"tar": true}
	tarGzFamilyExts = map[string]bool{"tar.gz": true, "tgz": true}
	archiveReadExts = "zip, jar, war, ear, nupkg, conda, apk, tar, tar.gz, tgz"

	explodeWhitelist    = []string{"zip", "tar", "tar.gz", "tgz"}
	explodeWhitelistSet = map[string]bool{"zip": true, "tar": true, "tar.gz": true, "tgz": true}
)

// archiveMemberMaxBytes bounds the in-memory buffering of NESTED archive
// members (the outer archive streams off the blob store; only a member that
// is itself addressed as an archive is buffered, so its central directory
// becomes reachable). The spec sets no nesting bound; 256MiB is the
// registered BinFlow ceiling and its overrun answers a 400.
const archiveMemberMaxBytes = 256 << 20

// ArchiveMemberRequest is one member read: the archive artifact's node path
// and the member addressing (which may itself carry "!/" recursion and a
// trailing .sha1/.md5/.sha256 checksum suffix).
type ArchiveMemberRequest struct {
	RepoKey     string
	ArchivePath string
	Entry       string
}

// ArchiveMemberResult is the extracted member: the streaming body (checksum
// mode answers the computed digest text instead), the uncompressed size
// and the member's extension-derived content type.
type ArchiveMemberResult struct {
	Body        io.ReadCloser
	Size        int64
	ContentType string
	// ChecksumText marks checksum mode: Body carries the digest text.
	ChecksumText bool
}

// ArchiveMember implements the §3 face. Resolution deliberately rides
// s.Get on the ARCHIVE path: the read gate (401 anonymous / 403
// authenticated), the remote pull-through, the virtual member routing and
// the download audit row are all inherited from the content plane — the
// spec's rule that member reads carry only the archive path's read
// permission, and V-6's negative assertion (no archiveBrowsingEnabled gate
// exists anywhere on this path, in BinFlow or upstream).
func (s *service) ArchiveMember(ctx context.Context, p *Principal, req ArchiveMemberRequest) (*ArchiveMemberResult, error) {
	if err := validateNodePath(req.ArchivePath); err != nil {
		return nil, err
	}
	if req.Entry == "" {
		return nil, fmt.Errorf("%w: archive member path is empty", ErrInvalidPath)
	}

	kind := archiveKindOf(req.ArchivePath)
	if kind == "" {
		// §0 row 7's 400 family borrows §4.1's verbatim template.
		return nil, unsupportedArchiveExtension(pathExtOf(req.ArchivePath))
	}

	rc, node, err := s.Get(ctx, p, req.RepoKey, req.ArchivePath)
	if err != nil {
		return nil, err
	}
	// rc's ownership transfers to the RESULT BODY on success (the member
	// readers stream lazily off it); every failure exit below releases it
	// explicitly — there is deliberately NO defer, a deferred release would
	// fire under the live body at function exit. The nested-recursion arm
	// fires it early once a level is buffered; the flag makes that
	// idempotent.
	released := false
	release := func() {
		if !released {
			released = true
			_ = rc.Close()
		}
	}

	// The checksum suffix (§3.2): ".sha1"/".md5"/".sha256" after the final
	// member segment addresses the computed digest, not a stored member.
	entry, checksumAlgo := splitMemberChecksumSuffix(req.Entry)
	src := memberSource{
		rc:      rc,
		release: release,
		size:    node.Size,
		kind:    kind,
		name:    req.ArchivePath,
		uri:     req.RepoKey + "/" + req.ArchivePath + "!/" + req.Entry,
	}
	body, size, ctype, err := openMember(src, entry)
	if err != nil {
		release()
		return nil, err
	}
	if checksumAlgo == "" {
		return &ArchiveMemberResult{
			Body:        &memberReadCloser{inner: body, release: release},
			Size:        size,
			ContentType: ctype,
		}, nil
	}

	// Checksum mode: hash the member on the fly and answer the digest text
	// (bare hex, no trailing newline — BinFlow's sidecar-less convention;
	// the spec is silent on the exact bytes, registered).
	var h hash.Hash
	switch checksumAlgo {
	case "sha1":
		// sha1/md5 here are Artifactory COMPATIBILITY digests — the §3.2
		// member checksum suffix face answers .sha1/.md5 BY SPEC; addressing
		// and integrity decisions stay sha256-only (ADR-0003, the
		// storage/digest.go precedent).
		h = sha1.New() //nolint:gosec // G401: compatibility digest only
	case "md5":
		h = md5.New() //nolint:gosec // G401: compatibility digest only
	default:
		h = sha256.New()
	}
	_, copyErr := io.Copy(h, body)
	_ = body.Close()
	if copyErr != nil {
		release()
		return nil, memberUnreadable(src.name, copyErr)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	return &ArchiveMemberResult{
		Body:         io.NopCloser(strings.NewReader(digest)),
		Size:         int64(len(digest)),
		ContentType:  "text/plain",
		ChecksumText: true,
	}, nil
}

// memberReadCloser couples a lazily-streaming member body with the release
// of the archive source it reads through (exactly once, on Close).
type memberReadCloser struct {
	inner   io.ReadCloser
	release func()
	once    sync.Once
}

func (m *memberReadCloser) Read(p []byte) (int, error) { return m.inner.Read(p) }

func (m *memberReadCloser) Close() error {
	err := m.inner.Close()
	m.once.Do(m.release)
	return err
}

// memberSource is one opened archive: the byte source (seekable — the blob
// plane's disk readers), its size, the format kind, and the two spellings
// the §3.2 error messages carry (this level's archive name and the
// caller's full URI). release frees the top-level blob reader (idempotent;
// the nested arm may fire it early once the level is buffered).
type memberSource struct {
	rc      io.ReadSeekCloser
	release func()
	size    int64
	kind    string // "zip" | "tar" | "targz"
	name    string
	uri     string
}

// openMember resolves one entry level against a source, recursing on a
// further "!/" in the entry (§3.1: nested archives, the FIRST "!/" split
// at every level).
func openMember(src memberSource, entry string) (io.ReadCloser, int64, string, error) {
	name, tail, nested := strings.Cut(entry, "!/")
	if !nested {
		return openMemberLeaf(src, name)
	}
	// The intermediate member is itself an archive: extract and buffer it
	// (bounded) so the next level's central directory is reachable, then
	// recurse on the remainder.
	inner, size, _, err := openMemberLeaf(src, name)
	if err != nil {
		return nil, 0, "", err
	}
	buf, err := readBounded(inner, archiveMemberMaxBytes)
	_ = inner.Close()
	if err != nil {
		src.release()
		return nil, 0, "", &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("Failed to get zip resource: nested archive member '%s' exceeds the %dMB buffering limit",
				name, archiveMemberMaxBytes>>20),
			cause: fmt.Errorf("buffer nested member %s: %w", name, err),
		}
	}
	// The level is buffered: the source beneath it is no longer needed
	// (release is idempotent and owns the top-level reader).
	src.release()
	kind := archiveKindOf(name)
	if kind == "" {
		src.release()
		return nil, 0, "", unsupportedArchiveExtension(pathExtOf(name))
	}
	next := memberSource{
		rc:      nopSeekCloser{bytes.NewReader(buf)},
		release: src.release,
		size:    size,
		kind:    kind,
		name:    name,
		uri:     src.uri,
	}
	return openMember(next, tail)
}

// openMemberLeaf extracts one (non-nested) member spelling from a source.
func openMemberLeaf(src memberSource, name string) (io.ReadCloser, int64, string, error) {
	if src.kind == "zip" {
		return openZipMember(src, name)
	}
	return openTarMember(src, name)
}

// openZipMember reads the central directory and streams the matching entry.
// The verbatim name comparison is the strictArchiveDotSlash default-true
// rule (§3.1): member spellings are never dot-folded or cleaned — BinFlow's
// router never normalizes paths, so the raw client spelling arrives intact
// and a stored "./x" matches exactly "./x" and nothing else.
func openZipMember(src memberSource, name string) (io.ReadCloser, int64, string, error) {
	var ra io.ReaderAt
	size := src.size
	if seeker, ok := src.rc.(io.ReaderAt); ok {
		ra = seeker
	} else {
		// A backend without ReaderAt (a non-disk engine): buffer the whole
		// archive, bounded, so the central directory becomes readable.
		buf, err := readBounded(src.rc, archiveMemberMaxBytes)
		if err != nil {
			return nil, 0, "", memberUnreadable(src.name, err)
		}
		br := bytes.NewReader(buf)
		ra, size = br, int64(len(buf))
	}
	if size < 0 {
		return nil, 0, "", memberUnreadable(src.name, fmt.Errorf("archive size unknown"))
	}
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return nil, 0, "", memberUnreadable(src.name, err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, 0, "", memberUnreadable(src.name, err)
			}
			size := int64(-1)
			if f.UncompressedSize64 <= uint64(math.MaxInt64) {
				size = int64(f.UncompressedSize64)
			}
			return rc, size, memberContentType(name), nil
		}
	}
	return nil, 0, "", memberNotFound(name, src.uri)
}

// openTarMember scans the (optionally gzipped) tar stream for the entry.
// The returned reader is the tar stream itself: it ends with the next
// header, and needs no Close of its own (the archive source's lifetime is
// the caller's; the gzip wrapper holds no resources worth closing before
// EOF).
func openTarMember(src memberSource, name string) (io.ReadCloser, int64, string, error) {
	if _, err := src.rc.Seek(0, io.SeekStart); err != nil {
		return nil, 0, "", memberUnreadable(src.name, err)
	}
	var r io.Reader = src.rc
	if src.kind == "targz" {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return nil, 0, "", memberUnreadable(src.name, err)
		}
		r = gz
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, 0, "", memberNotFound(name, src.uri)
		}
		if err != nil {
			return nil, 0, "", memberUnreadable(src.name, err)
		}
		if hdr.Name == name {
			return io.NopCloser(tr), hdr.Size, memberContentType(name), nil
		}
	}
}

// memberNotFound is §3.2's verbatim 404.
func memberNotFound(name, uri string) error {
	return &StatusError{
		Code:    http.StatusNotFound,
		Message: fmt.Sprintf("Unable to find zip resource: '%s' using full URI '%s'", name, uri),
		cause:   fmt.Errorf("archive member %q: %w", name, ErrNodeNotFound),
	}
}

// memberUnreadable maps a stream/format failure onto §3.2's 404 wording.
func memberUnreadable(archivePath string, err error) error {
	return &StatusError{
		Code:    http.StatusNotFound,
		Message: fmt.Sprintf("Failed to get zip resource: '%s'", archivePath),
		cause:   fmt.Errorf("open archive member of %s: %w", archivePath, err),
	}
}

// unsupportedArchiveExtension is §4.1's verbatim 400 (shared by the member
// reader's §0 row 7 arm — the spec points that row's 400 at this template).
func unsupportedArchiveExtension(ext string) error {
	return &StatusError{
		Code: http.StatusBadRequest,
		Message: fmt.Sprintf("Unsupported archive extension: '%s' of possible extensions : '%s'",
			ext, archiveReadExts),
		cause: fmt.Errorf("archive extension %q: %w", ext, ErrInvalidPath),
	}
}

// memberContentType derives the response type from the member's extension
// (octet-stream fallback — the spec's mime-probe rule, stdlib table).
func memberContentType(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// splitMemberChecksumSuffix peels a trailing .sha1/.md5/.sha256 off the
// member addressing (§3.2's member checksum requests). A member genuinely
// named "x.sha1" is indistinguishable from a checksum request — Artifactory
// reads it the same way; mid confidence, registered.
func splitMemberChecksumSuffix(entry string) (string, string) {
	for _, algo := range []string{"sha256", "sha1", "md5"} {
		if strings.HasSuffix(entry, "."+algo) && len(entry) > len(algo)+1 {
			return strings.TrimSuffix(entry, "."+algo), algo
		}
	}
	return entry, ""
}

// archiveKindOf classifies a file name's extension onto the reader kinds
// ("zip" | "tar" | "targz" | "").
func archiveKindOf(name string) string {
	lower := strings.ToLower(name)
	for ext := range tarGzFamilyExts {
		if strings.HasSuffix(lower, "."+ext) {
			return "targz"
		}
	}
	for ext := range zipFamilyExts {
		if strings.HasSuffix(lower, "."+ext) {
			return "zip"
		}
	}
	for ext := range tarFamilyExts {
		if strings.HasSuffix(lower, "."+ext) {
			return "tar"
		}
	}
	return ""
}

// pathExtOf renders the extension a refusal message names: the longest
// dotted tail of the final segment ("" when there is none — a name without
// any dot reports the empty extension, the §4.1 no-extension arm).
func pathExtOf(name string) string {
	base := path.Base(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}
	return ""
}

// readBounded reads r fully into memory, refusing bodies over limit.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > limit {
		return nil, fmt.Errorf("body exceeds %d bytes", limit)
	}
	return buf, nil
}

// nopSeekCloser adapts an in-memory reader onto the member-source shape.
type nopSeekCloser struct{ *bytes.Reader }

func (nopSeekCloser) Close() error { return nil }

// ---- section 4: exploded archive upload ----

// ExplodeRequest is one exploded upload: the PUT addressing (every entry
// lands under its parent directory) and the atomicity knob.
type ExplodeRequest struct {
	RepoKey string
	Path    string
	Atomic  bool
}

// ExplodeResult reports the landing: entries deployed, entries skipped by
// the §4.2 exclusion rules (system files, maven-metadata.xml carriers),
// and entries whose deploy failed (non-atomic mode continues past them —
// the statusHolder accumulate-then-answer posture; atomic mode never
// reports failures, it rolls back).
type ExplodeResult struct {
	Files   int
	Skipped int
	Failed  int
}

// ExplodeArchive implements the §4 face. The uploaded bytes stage in a
// temp file (the to_extract_ family, §4.2), unpack in ONE streaming pass,
// and every surviving entry deploys through Put — the full landing chain
// (permission pair, quota, audit, replication hook) runs per entry. The
// archive file itself never lands (§4.2's rule). The parallel-threads knob
// is fixed at the spec's default 1 (sequential) — the >1 two-batch arm is
// an optimization BinFlow does not carry, registered. V-1 ruling: success
// is 201 (the documented code wins over the decompiled implicit 200; no
// live instance was reachable to break the tie — see the T-343 report);
// the httpapi face renders the status, this method only reports success.
func (s *service) ExplodeArchive(ctx context.Context, p *Principal, req ExplodeRequest, body io.Reader) (*ExplodeResult, error) {
	// Writes are never anonymous (ADR-0009).
	if err := requireAuthenticated(p); err != nil {
		return nil, err
	}
	// §4.1's missing-file-name 400 precedes everything but authentication.
	if req.Path == "" || isFolderNode(req.Path) {
		return nil, &StatusError{
			Code:    http.StatusBadRequest,
			Message: "Explode archive deployment failed, Missing file name.",
			cause:   fmt.Errorf("explode target %q: %w", req.Path, ErrInvalidPath),
		}
	}
	if err := validateNodePath(req.Path); err != nil {
		return nil, err
	}
	// §4.1's whitelist: the four-type closed set; everything else —
	// including no extension at all — is the verbatim 400.
	if !explodeWhitelistSet[pathExtOf(req.Path)] {
		return nil, &StatusError{
			Code: http.StatusBadRequest,
			Message: fmt.Sprintf("Unsupported archive extension: '%s' of possible extensions : '%s'",
				pathExtOf(req.Path), strings.Join(explodeWhitelist, ", ")),
			cause: fmt.Errorf("explode extension %q: %w", pathExtOf(req.Path), ErrInvalidPath),
		}
	}

	// The write plane's repository resolution (the Put contract): an
	// un-routed virtual answers its 405 here, a routed one lands in the
	// member, a remote answers RE-05.
	repoKey, _, err := s.resolveWriteRepo(ctx, req.RepoKey)
	if err != nil {
		return nil, err
	}

	// The permission pre-check (§4.2): deploy permission on the target
	// path, the verbatim 403. The trailing-slash spelling is the folder
	// path's storage form (the folder-deploy gate's precedent). Every
	// entry's own Put re-runs the pair, so a finer-granted principal still
	// cannot smuggle a file in.
	permParent := parentPrefix(req.Path)
	if !s.allow(ctx, p, repoKey, permParent, ActionWrite) {
		return nil, &StatusError{
			Code:    http.StatusForbidden,
			Message: "User is not authorized to deploy to specified repo path.",
			cause:   fmt.Errorf("write %s/%s: %w", repoKey, permParent, ErrForbidden),
		}
	}
	parent := strings.TrimSuffix(permParent, "/")

	// Stage the upload (§4.2's temp file; cleaned on every exit path).
	tmp, err := os.CreateTemp("", "to_extract_*")
	if err != nil {
		return nil, fmt.Errorf("explode staging: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck // best-effort cleanup of our own staging file
	if _, err := io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		// §4.2's IO mapping: stream faults answer the 404 wording.
		return nil, &StatusError{
			Code:    http.StatusNotFound,
			Message: "Explode archive deployment failed. View log for more details.",
			cause:   fmt.Errorf("stage upload: %w", err),
		}
	}

	res := &ExplodeResult{}
	var landed []string
	var firstFailure error
	deploy := func(e explodeEntry) error {
		if e.dir {
			return nil // directory records materialize as file ancestors
		}
		if isExplodeExcluded(e.name) {
			res.Skipped++
			slog.DebugContext(ctx, "repo: explode skipped entry", "repo", repoKey, "entry", e.name)
			return nil
		}
		// The zip-slip defense (a BinFlow security addition — the spec is
		// silent; an entry naming outside the target prefix is refused, not
		// rewritten). A shape fault aborts the whole run whatever the
		// atomicity knob: it indicts the request, not one entry.
		if !safeExplodeEntry(e.name) {
			return &StatusError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("Explode archive deployment failed: entry '%s' escapes the target path", e.name),
				cause:   fmt.Errorf("entry %q: %w", e.name, ErrInvalidPath),
			}
		}
		rc, err := e.open()
		if err != nil {
			return &StatusError{
				Code:    http.StatusNotFound,
				Message: "Explode archive deployment failed. View log for more details.",
				cause:   fmt.Errorf("open entry %s: %w", e.name, err),
			}
		}
		_, err = s.Put(ctx, p, repoKey, cmJoin(parent, e.name), rc, storage.BlobRef{}, explodeMimeOf(e.name))
		_ = rc.Close()
		if err != nil {
			// Atomic mode is all-or-nothing: compensate the already-landed
			// entries away and abort (BinFlow's best-effort rollback — Put
			// offers no cross-call transaction; registered). The default
			// mode keeps the accumulate-then-answer posture: record the
			// first failure and keep deploying the rest.
			if req.Atomic {
				s.rollbackExplode(ctx, repoKey, landed)
				return err
			}
			res.Failed++
			if firstFailure == nil {
				firstFailure = err
			}
			slog.WarnContext(ctx, "repo: explode entry deploy failed",
				"repo", repoKey, "entry", e.name, "error", err.Error())
			return nil
		}
		res.Files++
		landed = append(landed, cmJoin(parent, e.name))
		return nil
	}
	if err := eachStagedEntry(tmp, req.Path, deploy); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("explode staging close: %w", err)
	}
	if firstFailure != nil {
		// The audit row rides only completed operations; a failure leaves
		// the per-entry deploy rows (Put's own) as the record.
		return nil, firstFailure
	}

	s.audit(ctx, AuditEvent{
		Actor: p.Name, Action: AuditActionExplode, Repo: repoKey, Path: req.Path,
		Detail: fmt.Sprintf(`{"files":%d,"skipped":%d,"atomic":%t}`, res.Files, res.Skipped, req.Atomic),
	})
	return res, nil
}

// rollbackExplode compensates an atomic run's landed entries away (the
// usage-aware delete — the copy/move pipeline's own bookkeeping arm).
func (s *service) rollbackExplode(ctx context.Context, repoKey string, landed []string) {
	for i := len(landed) - 1; i >= 0; i-- {
		if err := s.md.Usage().DeleteNodeWithUsage(ctx, repoKey, landed[i], s.now()); err != nil {
			slog.WarnContext(ctx, "repo: explode rollback missed an entry",
				"repo", repoKey, "path", landed[i], "error", err.Error())
		}
	}
}

// isExplodeExcluded reports §4.2's skip set: system files (the .jfrog
// internal prefix family) and any entry whose FILE NAME carries
// maven-metadata.xml (skipped quietly — debug, never an error).
func isExplodeExcluded(name string) bool {
	if name == ".jfrog" || strings.HasPrefix(name, ".jfrog/") {
		return true
	}
	return strings.Contains(path.Base(name), "maven-metadata.xml")
}

// safeExplodeEntry rejects traversal-shaped member names: empty, absolute,
// backslash, dot segments, or an empty interior segment.
func safeExplodeEntry(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		switch seg {
		case "", ".", "..":
			return false
		}
	}
	return true
}

// explodeMimeOf types one entry's deploy by extension (octet-stream
// fallback — the generic upload plane's default).
func explodeMimeOf(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// explodeEntry is one unpacked archive record. open must be consumed
// before the iteration advances (the tar arm streams straight off the
// scan).
type explodeEntry struct {
	name string
	dir  bool
	open func() (io.ReadCloser, error)
}

// eachStagedEntry walks the staged upload's entries in archive order,
// handing each to fn. A corrupt archive answers the §0 row 8 400 family
// (the whitelist and missing-name messages are verbatim; the corrupt-
// archive wording is BinFlow's rendering of the same status, registered —
// the spec spells no bytes for it).
func eachStagedEntry(f *os.File, archivePath string, fn func(explodeEntry) error) error {
	kind := archiveKindOf(archivePath)
	switch kind {
	case "zip":
		fi, err := f.Stat()
		if err != nil {
			return fmt.Errorf("explode staging stat: %w", err)
		}
		zr, err := zip.NewReader(f, fi.Size())
		if err != nil {
			return badExplodeArchive(kind, err)
		}
		for _, fe := range zr.File {
			// Windows-born zip entries may carry backslash separators;
			// normalize to slashes so the entry addresses a node path.
			name := strings.ReplaceAll(fe.Name, "\\", "/")
			if err := fn(explodeEntry{
				name: name, dir: fe.FileInfo().IsDir(),
				open: func() (io.ReadCloser, error) { return fe.Open() },
			}); err != nil {
				return err
			}
		}
		return nil
	case "tar", "targz":
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("explode staging rewind: %w", err)
		}
		var r io.Reader = f
		if kind == "targz" {
			gz, err := gzip.NewReader(r)
			if err != nil {
				return badExplodeArchive(kind, err)
			}
			defer gz.Close() //nolint:errcheck // scan reader
			r = gz
		}
		tr := tar.NewReader(r)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return badExplodeArchive(kind, err)
			}
			// The reader is positioned on this entry's payload: hand fn a
			// bounded view of it (the tar stream advances with the next
			// header — hence the single-pass, consume-before-advance
			// contract).
			remaining := hdr.Size
			if err := fn(explodeEntry{
				name: hdr.Name, dir: hdr.Typeflag == tar.TypeDir,
				open: func() (io.ReadCloser, error) {
					return io.NopCloser(io.LimitReader(tr, remaining)), nil
				},
			}); err != nil {
				return err
			}
			// A partially-consumed entry is drained before the scan moves
			// on (fn contracts to consume, Put does; this keeps the stream
			// aligned even if an entry was skipped mid-read).
			if _, err := io.CopyN(io.Discard, tr, remaining); err != nil && !errors.Is(err, io.EOF) {
				return badExplodeArchive(kind, err)
			}
		}
	default:
		return fmt.Errorf("explode: unhandled archive kind %q", kind)
	}
}

// badExplodeArchive renders the corrupt-archive 400 (§0 row 8's "坏归档"
// arm).
func badExplodeArchive(kind string, err error) error {
	return &StatusError{
		Code:    http.StatusBadRequest,
		Message: "Explode archive deployment failed: the uploaded file is not a valid " + kind + " archive",
		cause:   fmt.Errorf("open %s archive: %w", kind, err),
	}
}

// Compile-time pin: the concrete service carries the archive family's
// capability faces.
var _ ArchiveFamilyService = (*service)(nil)

// ArchiveFamilyService is the archive trio's capability face (the
// CopyMoveService precedent: deliberately NOT part of the big Service
// interface — consumers reach it by assertion, so the hand-written adapter
// fakes that implement Service method by method never break).
type ArchiveFamilyService interface {
	ArchiveDownload(ctx context.Context, p *Principal, req ArchiveDownloadRequest) (*ArchiveDownloadResult, error)
	ArchiveMember(ctx context.Context, p *Principal, req ArchiveMemberRequest) (*ArchiveMemberResult, error)
	ExplodeArchive(ctx context.Context, p *Principal, req ExplodeRequest, body io.Reader) (*ExplodeResult, error)
}
