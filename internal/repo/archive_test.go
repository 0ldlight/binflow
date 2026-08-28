package repo_test

// The archive family's service matrix (M12 T-343): folder download
// (repo-operations.md section 2), archive!/ member reads (section 3) and
// exploded upload (section 4) over the real engines. The REST-face legs
// (routing, the license gate's dual form, the wire shapes) live in
// internal/httpapi/t343_archive_test.go.

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1" // test fixture digest, G401 excluded globally
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// archRun resolves the archive capability face off the environment's
// service.
func archRun(t *testing.T, e *env) repo.ArchiveFamilyService {
	t.Helper()
	svc, ok := e.svc.(repo.ArchiveFamilyService)
	if !ok {
		t.Fatalf("service does not carry the archive family capability")
	}
	return svc
}

// archErr runs one call expecting a refusal and renders its shape: the
// StatusError's exact code/message when the family spells one, otherwise
// the sentinel mapping the HTTP face applies.
func archErr(t *testing.T, e *env, call func(repo.ArchiveFamilyService) error) (int, string) {
	t.Helper()
	err := call(archRun(t, e))
	if err == nil {
		t.Fatalf("expected a refusal, got success")
	}
	var se *repo.StatusError
	if errors.As(err, &se) {
		return se.Code, se.Message
	}
	switch {
	case errors.Is(err, repo.ErrUnauthorized):
		return http.StatusUnauthorized, err.Error()
	case errors.Is(err, repo.ErrForbidden):
		return http.StatusForbidden, err.Error()
	case errors.Is(err, repo.ErrNodeNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, repo.ErrInvalidPath):
		return http.StatusBadRequest, err.Error()
	}
	t.Fatalf("refusal carries no known shape: %v", err)
	return 0, ""
}

// enableFolders switches the folder-download configuration on for the
// environment's service (the assembly-time ConfigureFolderDownload seam).
func enableFolders(t *testing.T, e *env, cfg repo.FolderDownloadConfig) {
	t.Helper()
	repo.ConfigureFolderDownload(e.svc, cfg)
}

// enabledCfg is the minimal on-configuration (everything else default).
func enabledCfg() repo.FolderDownloadConfig {
	return repo.FolderDownloadConfig{Enabled: true}
}

// ---- section 2: folder download ----

// TestArchiveDownloadGateOrder walks the §2.2 refusal table in execution
// order: the anonymous 401 precedes the archiveType parse, the
// qualification steps precede the permission step, and the master switch
// follows them all.
func TestArchiveDownloadGateOrder(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "d/f.bin", "v")
	seedRepoRow(t, e, "rmt", repo.TypeRemote)

	tests := []struct {
		name    string
		cfg     *repo.FolderDownloadConfig
		p       *repo.Principal
		req     repo.ArchiveDownloadRequest
		want    int
		wantMsg string
	}{
		{
			name:    "anonymous gate precedes archiveType",
			p:       nil,
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Type: "bogus"},
			want:    http.StatusUnauthorized,
			wantMsg: "You must be logged in to download a folder or repository.",
		},
		{
			name:    "archiveType required",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib"},
			want:    http.StatusBadRequest,
			wantMsg: "Unsupported archive type: '' of possible types : 'zip, tar, tar.gz, tgz'",
		},
		{
			name:    "archiveType enum",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Type: "rar"},
			want:    http.StatusBadRequest,
			wantMsg: "Unsupported archive type: 'rar'",
		},
		{
			name:    "repository missing",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "nope", Type: "zip"},
			want:    http.StatusNotFound,
			wantMsg: "nope is not a repository.",
		},
		{
			name:    "remote refused",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "rmt", Type: "zip"},
			want:    http.StatusNotFound,
			wantMsg: "only available for local (or cache) repositories",
		},
		{
			name:    "path missing",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "nodir", Type: "zip"},
			want:    http.StatusNotFound,
			wantMsg: "Path 'lib/nodir' does not exist, aborting folder download",
		},
		{
			name:    "path is a file",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d/f.bin", Type: "zip"},
			want:    http.StatusBadRequest,
			wantMsg: "Path 'lib/d/f.bin' is not a folder, aborting folder download",
		},
		{
			name:    "read permission precedes the master switch",
			p:       alice(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"},
			want:    http.StatusForbidden,
			wantMsg: "You don't have the required permissions to download lib/d.",
		},
		{
			name:    "master switch default off",
			p:       admin(),
			req:     repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"},
			want:    http.StatusForbidden,
			wantMsg: "Download Folder functionality is disabled.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg != nil {
				enableFolders(t, e, *tt.cfg)
			}
			code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
				_, err := svc.ArchiveDownload(context.Background(), tt.p, tt.req)
				return err
			})
			if code != tt.want {
				t.Fatalf("code = %d, want %d (msg %s)", code, tt.want, msg)
			}
			if !strings.Contains(msg, tt.wantMsg) {
				t.Fatalf("message %q does not contain %q", msg, tt.wantMsg)
			}
		})
	}
}

// TestArchiveDownloadLimits pins the §2.2 step-7 statistics: the file-count
// and size ceilings, both messages verbatim (two-decimal MB).
func TestArchiveDownloadLimits(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "d/a.bin", strings.Repeat("a", 600*1024))
	put(t, e, admin(), "lib", "d/b.bin", strings.Repeat("b", 600*1024))

	t.Run("file count", func(t *testing.T) {
		enableFolders(t, e, repo.FolderDownloadConfig{Enabled: true, MaxFiles: 1})
		code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ArchiveDownload(context.Background(), admin(),
				repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"})
			return err
		})
		if code != http.StatusBadRequest || !strings.Contains(msg,
			"Number of files under the path 'lib/d' (2) exceeds the max allowed file count for folder download (1).") {
			t.Fatalf("file-count refusal = %d %q", code, msg)
		}
	})

	t.Run("size", func(t *testing.T) {
		enableFolders(t, e, repo.FolderDownloadConfig{Enabled: true, MaxDownloadSizeMb: 1})
		code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ArchiveDownload(context.Background(), admin(),
				repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"})
			return err
		})
		if code != http.StatusBadRequest || !strings.Contains(msg,
			"Size of path 'lib/d' (1.17MB) exceeds the max allowed folder download size (1MB).") {
			t.Fatalf("size refusal = %d %q", code, msg)
		}
	})
}

// TestArchiveDownloadZipRoundTrip: the real-archive reconciliation — the
// streamed zip opens as a zip, every file's sha256 matches its uploaded
// content, generated checksum companions match the ledger, and empty
// directories ride only when the knob allows.
func TestArchiveDownloadZipRoundTrip(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "d/sub/a.txt", "alpha-bytes")
	put(t, e, admin(), "lib", "d/sub/deep/b.bin", "beta-bytes")
	put(t, e, admin(), "lib", "d/top.txt", "top-bytes")

	download := func(t *testing.T, cfg repo.FolderDownloadConfig, include bool) *zip.Reader {
		t.Helper()
		enableFolders(t, e, cfg)
		res, err := archRun(t, e).ArchiveDownload(context.Background(), admin(),
			repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip", IncludeChecksumFiles: include})
		if err != nil {
			t.Fatalf("ArchiveDownload: %v", err)
		}
		defer res.Body.Close() //nolint:errcheck // test read
		if res.Files != 3 || res.Bytes != int64(len("alpha-bytes")+len("beta-bytes")+len("top-bytes")) {
			t.Fatalf("stats = %d files / %d bytes", res.Files, res.Bytes)
		}
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read archive: %v", err)
		}
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatalf("stream is not a zip: %v", err)
		}
		return zr
	}

	want := map[string]string{
		"d/sub/a.txt":      "alpha-bytes",
		"d/sub/deep/b.bin": "beta-bytes",
		"d/top.txt":        "top-bytes",
	}

	t.Run("plain", func(t *testing.T) {
		zr := download(t, enabledCfg(), false)
		if len(zr.File) != len(want) {
			t.Fatalf("entries = %d, want %d", len(zr.File), len(want))
		}
		for _, f := range zr.File {
			body, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", f.Name, err)
			}
			got, err := io.ReadAll(body)
			_ = body.Close()
			if err != nil {
				t.Fatalf("read %s: %v", f.Name, err)
			}
			exp, ok := want[f.Name]
			if !ok {
				t.Fatalf("unexpected entry %s", f.Name)
			}
			if string(got) != exp {
				t.Fatalf("entry %s = %q, want %q", f.Name, got, exp)
			}
			sum := sha256.Sum256(got)
			if hex.EncodeToString(sum[:]) != mustNode(t, e, "lib", f.Name).Sha256 {
				t.Fatalf("entry %s sha256 mismatch against the node row", f.Name)
			}
		}
	})

	t.Run("checksum companions", func(t *testing.T) {
		zr := download(t, enabledCfg(), true)
		seen := map[string]string{}
		for _, f := range zr.File {
			body, _ := f.Open()
			got, _ := io.ReadAll(body)
			_ = body.Close()
			seen[f.Name] = string(got)
		}
		n := mustNode(t, e, "lib", "d/top.txt")
		if seen["d/top.txt.sha256"] != n.Sha256 {
			t.Fatalf("sha256 companion = %q, want %q", seen["d/top.txt.sha256"], n.Sha256)
		}
		ledger, err := e.md.Blobs().Get(context.Background(), n.Sha256)
		if err != nil {
			t.Fatalf("ledger: %v", err)
		}
		if seen["d/top.txt.sha1"] != ledger.Sha1 || seen["d/top.txt.md5"] != ledger.Md5 {
			t.Fatalf("sha1/md5 companions = %q/%q", seen["d/top.txt.sha1"], seen["d/top.txt.md5"])
		}
	})

	t.Run("empty directories", func(t *testing.T) {
		// Seed an empty folder row directly (uploads materialize ancestors
		// only when they carry files).
		if err := e.md.Nodes().Put(context.Background(), &metadata.Node{
			RepoKey: "lib", Path: "d/hollow/", Sha256: metadata.FolderMarkerSHA,
		}); err != nil {
			t.Fatalf("seed empty folder: %v", err)
		}
		zr := download(t, repo.FolderDownloadConfig{Enabled: true}, false)
		for _, f := range zr.File {
			if f.Name == "d/hollow/" {
				t.Fatalf("empty directory present without the knob")
			}
		}
		zr = download(t, repo.FolderDownloadConfig{Enabled: true, EnabledEmptyDirectories: true}, false)
		found := false
		for _, f := range zr.File {
			if f.Name == "d/hollow/" {
				found = true
			}
		}
		if !found {
			t.Fatalf("empty directory missing with the knob on")
		}
	})
}

// TestArchiveDownloadTarFlavors: the tar and tar.gz/tgz round trips.
func TestArchiveDownloadTarFlavors(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "d/a.txt", "alpha")
	put(t, e, admin(), "lib", "d/b.txt", "bravo")

	for _, tt := range []struct{ kind, wantCT string }{
		{"tar", "application/x-tar"},
		{"tar.gz", "application/gzip"},
		{"tgz", "application/gzip"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			enableFolders(t, e, enabledCfg())
			res, err := archRun(t, e).ArchiveDownload(context.Background(), admin(),
				repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: tt.kind})
			if err != nil {
				t.Fatalf("ArchiveDownload(%s): %v", tt.kind, err)
			}
			defer res.Body.Close() //nolint:errcheck // test read
			if res.ContentType != tt.wantCT {
				t.Fatalf("content type = %q, want %q", res.ContentType, tt.wantCT)
			}
			raw, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var r io.Reader = bytes.NewReader(raw)
			if tt.kind != "tar" {
				gz, err := gzip.NewReader(r)
				if err != nil {
					t.Fatalf("not gzip: %v", err)
				}
				defer gz.Close() //nolint:errcheck // test read
				r = gz
			}
			tr := tar.NewReader(r)
			seen := map[string]string{}
			for {
				hdr, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("tar walk: %v", err)
				}
				got, _ := io.ReadAll(tr)
				seen[hdr.Name] = string(got)
			}
			if seen["d/a.txt"] != "alpha" || seen["d/b.txt"] != "bravo" {
				t.Fatalf("tar contents = %v", seen)
			}
		})
	}
}

// TestArchiveDownloadWholeRepo: the empty-path form (section 0 row 6)
// packages the repository root and its stats count everything.
func TestArchiveDownloadWholeRepo(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "a.txt", "one")
	put(t, e, admin(), "lib", "s/b.txt", "two")
	enableFolders(t, e, enabledCfg())

	res, err := archRun(t, e).ArchiveDownload(context.Background(), admin(),
		repo.ArchiveDownloadRequest{RepoKey: "lib", Type: "zip"})
	if err != nil {
		t.Fatalf("ArchiveDownload: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // test read
	raw, _ := io.ReadAll(res.Body)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	if len(zr.File) != 2 || zr.File[0].Name != "a.txt" || zr.File[1].Name != "s/b.txt" {
		t.Fatalf("whole-repo entries wrong: %v", zr.File)
	}
}

// TestArchiveDownloadConcurrency: a full slot answers the §2.2 step-8 400
// and closing the first body releases the slot.
func TestArchiveDownloadConcurrency(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "d/a.txt", "v")
	enableFolders(t, e, repo.FolderDownloadConfig{Enabled: true, MaxConcurrentRequests: 1})
	svc := archRun(t, e)

	first, err := svc.ArchiveDownload(context.Background(), admin(),
		repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"})
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	// The slot is held until the body closes.
	code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
		_, err := svc.ArchiveDownload(context.Background(), admin(),
			repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"})
		return err
	})
	if code != http.StatusBadRequest || !strings.Contains(msg,
		"There are too many folder download requests currently running. Try again later.") {
		t.Fatalf("second download = %d %q", code, msg)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}
	if _, err := svc.ArchiveDownload(context.Background(), admin(),
		repo.ArchiveDownloadRequest{RepoKey: "lib", Path: "d", Type: "zip"}); err != nil {
		t.Fatalf("download after release: %v", err)
	}
}

// ---- section 3: archive!/ member reads ----

// buildZip builds an in-memory zip fixture.
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(entries) {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(entries[name])); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// buildTarGz builds an in-memory tar.gz fixture.
func buildTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	for _, name := range sortedKeys(entries) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(entries[name]))}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(entries[name])); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return raw.Bytes()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// memberGet reads one member and returns its bytes.
func memberGet(t *testing.T, e *env, p *repo.Principal, repoKey, archive, entry string) ([]byte, *repo.ArchiveMemberResult) {
	t.Helper()
	res, err := archRun(t, e).ArchiveMember(context.Background(), p,
		repo.ArchiveMemberRequest{RepoKey: repoKey, ArchivePath: archive, Entry: entry})
	if err != nil {
		t.Fatalf("ArchiveMember(%s!/%s): %v", archive, entry, err)
	}
	defer res.Body.Close() //nolint:errcheck // test read
	got, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read member: %v", err)
	}
	return got, res
}

// TestArchiveMemberZip: member bytes, the verbatim miss 404, checksum
// suffixes, and the strictArchiveDotSlash verbatim matching (V-6's
// companion: no toggle is consulted anywhere on this path — the read works
// on a plain, unconfigured local repository).
func TestArchiveMemberZip(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildZip(t, map[string]string{
		"dir/hello.txt": "hello-bytes",
		"./strict.txt":  "strict-bytes",
	})
	put(t, e, admin(), "lib", "pkg/a.zip", string(body))

	t.Run("member bytes", func(t *testing.T) {
		got, res := memberGet(t, e, admin(), "lib", "pkg/a.zip", "dir/hello.txt")
		if string(got) != "hello-bytes" {
			t.Fatalf("member = %q", got)
		}
		if res.Size != int64(len("hello-bytes")) {
			t.Fatalf("size = %d", res.Size)
		}
	})

	t.Run("missing member", func(t *testing.T) {
		code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ArchiveMember(context.Background(), admin(),
				repo.ArchiveMemberRequest{RepoKey: "lib", ArchivePath: "pkg/a.zip", Entry: "nope.txt"})
			return err
		})
		if code != http.StatusNotFound || !strings.Contains(msg,
			"Unable to find zip resource: 'nope.txt' using full URI 'lib/pkg/a.zip!/nope.txt'") {
			t.Fatalf("miss = %d %q", code, msg)
		}
	})

	t.Run("checksum suffix", func(t *testing.T) {
		got, res := memberGet(t, e, admin(), "lib", "pkg/a.zip", "dir/hello.txt.sha1")
		sum := sha1.Sum([]byte("hello-bytes")) //nolint:gosec // fixture digest
		if string(got) != hex.EncodeToString(sum[:]) {
			t.Fatalf("sha1 = %q", got)
		}
		if !res.ChecksumText || res.ContentType != "text/plain" {
			t.Fatalf("checksum mode not flagged: %+v", res)
		}
	})

	t.Run("strict dot-slash verbatim", func(t *testing.T) {
		// strictArchiveDotSlash default true: "./strict.txt" is addressed
		// by its exact spelling; the folded form is NOT the same member.
		if got, _ := memberGet(t, e, admin(), "lib", "pkg/a.zip", "./strict.txt"); string(got) != "strict-bytes" {
			t.Fatalf("verbatim member = %q", got)
		}
		code, _ := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ArchiveMember(context.Background(), admin(),
				repo.ArchiveMemberRequest{RepoKey: "lib", ArchivePath: "pkg/a.zip", Entry: "strict.txt"})
			return err
		})
		if code != http.StatusNotFound {
			t.Fatalf("folded spelling = %d, want 404", code)
		}
	})
}

// TestArchiveMemberNested: the first "!/" split at every level — a zip
// inside a zip is reached by its own "!/" segment.
func TestArchiveMemberNested(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	inner := buildZip(t, map[string]string{"f.txt": "inner-bytes"})
	outer := buildZip(t, map[string]string{
		"dir/inner.zip": string(inner),
		"top.txt":       "top-bytes",
	})
	put(t, e, admin(), "lib", "pkg/outer.zip", string(outer))

	got, _ := memberGet(t, e, admin(), "lib", "pkg/outer.zip", "dir/inner.zip!/f.txt")
	if string(got) != "inner-bytes" {
		t.Fatalf("nested member = %q", got)
	}
}

// TestArchiveMemberTarGz: the tar.gz flavor streams off the scan.
func TestArchiveMemberTarGz(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "pkg/b.tgz", string(buildTarGz(t, map[string]string{
		"x/y.txt": "tar-member",
	})))
	got, res := memberGet(t, e, admin(), "lib", "pkg/b.tgz", "x/y.txt")
	if string(got) != "tar-member" {
		t.Fatalf("member = %q", got)
	}
	if res.Size != int64(len("tar-member")) {
		t.Fatalf("size = %d", res.Size)
	}
}

// TestArchiveMemberNonArchive: the §0 row 7 400 family borrows §4.1's
// template for a non-archive extension.
func TestArchiveMemberNonArchive(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "pkg/plain.bin", "v")
	code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
		_, err := svc.ArchiveMember(context.Background(), admin(),
			repo.ArchiveMemberRequest{RepoKey: "lib", ArchivePath: "pkg/plain.bin", Entry: "x"})
		return err
	})
	if code != http.StatusBadRequest || !strings.HasPrefix(msg,
		"Unsupported archive extension: 'bin' of possible extensions : '") {
		t.Fatalf("non-archive = %d %q", code, msg)
	}
}

// TestArchiveMemberPermission: the member read carries the ARCHIVE path's
// read permission (the Get gate), nothing finer.
func TestArchiveMemberPermission(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "pkg/a.zip", string(buildZip(t, map[string]string{"f.txt": "v"})))

	code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
		_, err := svc.ArchiveMember(context.Background(), alice(),
			repo.ArchiveMemberRequest{RepoKey: "lib", ArchivePath: "pkg/a.zip", Entry: "f.txt"})
		return err
	})
	if code != http.StatusForbidden {
		t.Fatalf("ungranted member read = %d %q, want 403", code, msg)
	}
	e.az.add("alice", repo.ActionRead, "pkg/")
	memberGet(t, e, &repo.Principal{Name: "alice"}, "lib", "pkg/a.zip", "f.txt")
}

// ---- section 4: exploded upload ----

// explode runs one exploded upload expecting success.
func explode(t *testing.T, e *env, p *repo.Principal, req repo.ExplodeRequest, body []byte) *repo.ExplodeResult {
	t.Helper()
	res, err := archRun(t, e).ExplodeArchive(context.Background(), p, req, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ExplodeArchive(%s): %v", req.Path, err)
	}
	return res
}

// TestExplodeZip: the entries land under the PUT path's parent, the
// archive itself never lands, and the exclusion rules skip quietly.
func TestExplodeZip(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildZip(t, map[string]string{
		"a.txt":              "alpha",
		"sub/b.txt":          "bravo",
		"maven-metadata.xml": "meta",
		".jfrog/inner.txt":   "system",
	})
	res := explode(t, e, admin(), repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, body)

	if res.Files != 2 || res.Skipped != 2 {
		t.Fatalf("result = %+v, want 2 files / 2 skipped", res)
	}
	if got := mustGet(t, e, "lib", "rel/a.txt"); got.Size != int64(len("alpha")) {
		t.Fatalf("entry a.txt = %+v", got)
	}
	if n, err := e.md.Nodes().Get(context.Background(), "lib", "rel/sub/b.txt"); err != nil || n == nil {
		t.Fatalf("entry sub/b.txt missing: %v", err)
	}
	assertGone(t, e, "lib", "rel/pkg.zip")
	assertGone(t, e, "lib", "rel/maven-metadata.xml")
	assertGone(t, e, "lib", "rel/.jfrog/inner.txt")
}

// TestExplodeTarGz: the tar.gz flavor on the same chain.
func TestExplodeTarGz(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildTarGz(t, map[string]string{"deep/c.txt": "charlie"})
	res := explode(t, e, admin(), repo.ExplodeRequest{RepoKey: "lib", Path: "bundle.tgz"}, body)
	if res.Files != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := mustGet(t, e, "lib", "deep/c.txt"); got.Size != int64(len("charlie")) {
		t.Fatalf("entry = %+v", got)
	}
	assertGone(t, e, "lib", "bundle.tgz")
}

// TestExplodeRefusals: the §4.1 verbatim table — the whitelist, the
// missing file name — plus the anonymous door and the deploy-permission
// 403.
func TestExplodeRefusals(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	valid := buildZip(t, map[string]string{"a.txt": "v"})

	tests := []struct {
		name    string
		p       *repo.Principal
		req     repo.ExplodeRequest
		want    int
		wantMsg string
	}{
		{
			name: "anonymous",
			p:    nil,
			req:  repo.ExplodeRequest{RepoKey: "lib", Path: "x.zip"},
			want: http.StatusUnauthorized,
		},
		{
			name:    "outside the whitelist",
			p:       admin(),
			req:     repo.ExplodeRequest{RepoKey: "lib", Path: "x.7z"},
			want:    http.StatusBadRequest,
			wantMsg: "Unsupported archive extension: '7z' of possible extensions : 'zip, tar, tar.gz, tgz'",
		},
		{
			name:    "no extension",
			p:       admin(),
			req:     repo.ExplodeRequest{RepoKey: "lib", Path: "bundle"},
			want:    http.StatusBadRequest,
			wantMsg: "Unsupported archive extension: ''",
		},
		{
			name:    "missing file name",
			p:       admin(),
			req:     repo.ExplodeRequest{RepoKey: "lib", Path: "rel/"},
			want:    http.StatusBadRequest,
			wantMsg: "Explode archive deployment failed, Missing file name.",
		},
		{
			name:    "deploy permission",
			p:       alice(),
			req:     repo.ExplodeRequest{RepoKey: "lib", Path: "rel/x.zip"},
			want:    http.StatusForbidden,
			wantMsg: "User is not authorized to deploy to specified repo path.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
				_, err := svc.ExplodeArchive(context.Background(), tt.p, tt.req, bytes.NewReader(valid))
				return err
			})
			if code != tt.want {
				t.Fatalf("code = %d, want %d (msg %s)", code, tt.want, msg)
			}
			if tt.wantMsg != "" && !strings.Contains(msg, tt.wantMsg) {
				t.Fatalf("message %q does not contain %q", msg, tt.wantMsg)
			}
		})
	}

	// A write grant on the parent opens the door (the per-entry Put gates
	// still apply downstream).
	e.az.add("alice", repo.ActionWrite, "rel/")
	explode(t, e, &repo.Principal{Name: "alice"}, repo.ExplodeRequest{RepoKey: "lib", Path: "rel/x.zip"}, valid)
}

// TestExplodeZipSlip: a traversal-shaped entry refuses the whole upload
// and lands nothing (the BinFlow security addition).
func TestExplodeZipSlip(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	// Hand-build the zip: the fixture builder only takes sane names.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.Create("../evil.txt")
	_, _ = fw.Write([]byte("evil"))
	fw2, _ := zw.Create("ok.txt")
	_, _ = fw2.Write([]byte("ok"))
	_ = zw.Close()

	code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
		_, err := svc.ExplodeArchive(context.Background(), admin(),
			repo.ExplodeRequest{RepoKey: "lib", Path: "rel/x.zip"}, bytes.NewReader(buf.Bytes()))
		return err
	})
	if code != http.StatusBadRequest || !strings.Contains(msg, "escapes the target path") {
		t.Fatalf("zip-slip = %d %q", code, msg)
	}
	assertGone(t, e, "lib", "ok.txt")
	assertGone(t, e, "lib", "rel/ok.txt")
}

// TestExplodeAtomicRollback: atomic mode is all-or-nothing — an entry the
// repository's governance refuses rolls the whole run back; the default
// mode keeps what landed.
func TestExplodeAtomicRollback(t *testing.T) {
	newRepo := func(t *testing.T, key string) *env {
		e := newEnv(t)
		if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
			Config: `{"excludesPattern":"**/blocked.txt"}`,
		}); err != nil {
			t.Fatalf("create governed repo: %v", err)
		}
		return e
	}
	body := buildZip(t, map[string]string{
		"ok.txt":      "ok-bytes",
		"blocked.txt": "blocked-bytes",
	})

	t.Run("atomic rolls back", func(t *testing.T) {
		e := newRepo(t, "gov")
		code, msg := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ExplodeArchive(context.Background(), admin(),
				repo.ExplodeRequest{RepoKey: "gov", Path: "rel/x.zip", Atomic: true}, bytes.NewReader(body))
			return err
		})
		if code != http.StatusConflict {
			t.Fatalf("atomic explode = %d %q, want the governance 409", code, msg)
		}
		assertGone(t, e, "gov", "rel/ok.txt")
		assertGone(t, e, "gov", "rel/x.zip")
	})

	t.Run("default keeps the partial landing", func(t *testing.T) {
		e := newRepo(t, "gov")
		code, _ := archErr(t, e, func(svc repo.ArchiveFamilyService) error {
			_, err := svc.ExplodeArchive(context.Background(), admin(),
				repo.ExplodeRequest{RepoKey: "gov", Path: "rel/x.zip"}, bytes.NewReader(body))
			return err
		})
		if code != http.StatusConflict {
			t.Fatalf("plain explode = %d, want the governance 409", code)
		}
		if got := mustGet(t, e, "gov", "rel/ok.txt"); got.Size != int64(len("ok-bytes")) {
			t.Fatalf("partial landing missing: %+v", got)
		}
	})
}

// seedRepoRow plants a repository row of any class directly (bypassing
// CreateRepo's per-class validation — the qualification arms only read the
// row).
func seedRepoRow(t *testing.T, e *env, key, rclass string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("seed repo row %s: %v", key, err)
	}
}

// mustNode fetches a node row for checksum reconciliation.
func mustNode(t *testing.T, e *env, repoKey, path string) *metadata.Node {
	t.Helper()
	n, err := e.md.Nodes().Get(context.Background(), repoKey, path)
	if err != nil {
		t.Fatalf("node %s/%s: %v", repoKey, path, err)
	}
	return n
}
