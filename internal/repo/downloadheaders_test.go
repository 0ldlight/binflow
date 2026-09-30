package repo_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The disposition pair and the 416 bare set are the download-face header
// single source (T-608 / BIN-90): these tests pin the live wire forms
// T-601's dual-round probe recorded on the reference 7.161.26 — the
// dual-parameter and filename*-only Content-Disposition spellings, the
// URL-encoded X-Artifactory-Filename echo, and the 416 strip of every
// body-descriptive header.

func TestSetDownloadDispositionForms(t *testing.T) {
	tests := []struct {
		name       string
		relPath    string
		wantCD     string
		wantFilenm string
	}{
		{
			name:       "ascii name takes the dual-parameter form",
			relPath:    "t601/plain-ascii.bin",
			wantCD:     `attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`,
			wantFilenm: "plain-ascii.bin",
		},
		{
			name:       "root-level ascii name",
			relPath:    "plain-ascii.bin",
			wantCD:     `attachment; filename="plain-ascii.bin"; filename*=UTF-8''plain-ascii.bin`,
			wantFilenm: "plain-ascii.bin",
		},
		{
			name:       "gav artifact name",
			relPath:    "com/diff/t601/art/1.0.0/art-1.0.0.jar",
			wantCD:     `attachment; filename="art-1.0.0.jar"; filename*=UTF-8''art-1.0.0.jar`,
			wantFilenm: "art-1.0.0.jar",
		},
		{
			name:    "non-ascii name takes filename* only, pct-encoded",
			relPath: "t601/文件-ünïcode.bin",
			wantCD:  "attachment; filename*=UTF-8''%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin",
			// The live wire (T-601 leg gl-get-utf8): uppercase percent-hex
			// UTF-8, '-' literal — exactly url.PathEscape's spelling.
			wantFilenm: "%E6%96%87%E4%BB%B6-%C3%BCn%C3%AFcode.bin",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hdr := http.Header{}
			repo.SetDownloadDisposition(hdr, tt.relPath)
			if got := hdr.Get("Content-Disposition"); got != tt.wantCD {
				t.Fatalf("Content-Disposition = %q, want %q", got, tt.wantCD)
			}
			if got := hdr.Get("X-Artifactory-Filename"); got != tt.wantFilenm {
				t.Fatalf("X-Artifactory-Filename = %q, want %q", got, tt.wantFilenm)
			}
		})
	}
}

func TestSetDownloadDispositionDegeneratePaths(t *testing.T) {
	for _, rel := range []string{"", ".", "/"} {
		hdr := http.Header{}
		repo.SetDownloadDisposition(hdr, rel)
		if got := hdr.Get("Content-Disposition"); got != "" {
			t.Fatalf("relPath %q: Content-Disposition = %q, want unset", rel, got)
		}
		if got := hdr.Get("X-Artifactory-Filename"); got != "" {
			t.Fatalf("relPath %q: X-Artifactory-Filename = %q, want unset", rel, got)
		}
	}
}

// TestWriteRangeNotSatisfiableBareSet pins the 416 shape: the strip list
// removes the body-descriptive family the caller already assembled, the
// response answers exactly Content-Range bytes */<total> + Content-Length 0.
func TestWriteRangeNotSatisfiableBareSet(t *testing.T) {
	rec := httptest.NewRecorder()
	hdr := rec.Header()
	for _, k := range []string{
		"Content-Type", "ETag", "Last-Modified", "Accept-Ranges",
		"Content-Disposition", "X-Artifactory-Filename",
		"X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256",
	} {
		hdr.Set(k, "assembled")
	}
	// F4's INTENTIONAL-keep family survives the strip (T-601 §四).
	hdr.Set("X-BinFlow-Resolved-From", "member-local")
	repo.WriteRangeNotSatisfiable(rec, 62)
	resp := rec.Result()
	defer resp.Body.Close() //nolint:errcheck // probe
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes */62" {
		t.Fatalf("Content-Range = %q, want bytes */62", got)
	}
	if got := resp.Header.Get("Content-Length"); got != "0" {
		t.Fatalf("Content-Length = %q, want 0", got)
	}
	for _, k := range []string{
		"Content-Type", "Etag", "Last-Modified", "Accept-Ranges",
		"Content-Disposition", "X-Artifactory-Filename",
		"X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256",
	} {
		if got := resp.Header.Get(k); got != "" {
			t.Fatalf("%s = %q after the 416 strip, want absent", k, got)
		}
	}
	if got := resp.Header.Get("X-BinFlow-Resolved-From"); got != "member-local" {
		t.Fatalf("X-BinFlow-Resolved-From = %q, want kept through the 416", got)
	}
}
