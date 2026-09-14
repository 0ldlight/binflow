package pypi

// Distribution-metadata extraction for the simple index (L016 D1/D7).
// BinFlow keeps no metadata sidecars (ADR-0006), so the index derives
// every per-file fact from the artifact's OWN bytes — the wheel's
// dist-info/METADATA and the sdist's PKG-INFO (maven-npm-pypi.md section
// 4.5's reconstruction-from-facts posture). Two consumers:
//
//   - D1: the Requires-Python header feeds the anchor's
//     data-requires-python attribute (HTML-escaped at render time) and
//     the PEP 691 JSON requires-python field.
//   - D7: a .whl whose metadata cannot be located (not a zip archive, or
//     no top-level *.dist-info/METADATA member) is STORED but never
//     INDEXED — the reference's index-integrity posture (L016 three-state
//     matrix: only the bad-metadata wheel is refused; bad-name and
//     bad-version wheels carry self-consistent METADATA and stay indexed,
//     both ends alike). A bad sdist (no PKG-INFO / not a gzip stream) is
//     NOT evidenced on either side and keeps its index entry, attribute
//     absent — the policy is bounded to exactly what the wire proved.

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"path"
	"strings"
)

// Parse bounds: a metadata header block is far below 1 MiB in every real
// distribution; the sdist scan caps member count and decompressed volume so
// a hostile archive cannot turn an index render into an unbounded read.
const (
	maxDistMetadataBytes = 1 << 20
	maxSdistScanMembers  = 64
	maxSdistScanBytes    = 8 << 20
	// maxWheelBufferBytes bounds the fallback whole-file buffer for engines
	// whose blob reader is not io.ReaderAt (none today — the DiskEngine
	// returns *os.File). A bigger wheel keeps its entry without the
	// attribute rather than being evicted on an engine limitation.
	maxWheelBufferBytes = 64 << 20
)

// distMetaFacts is what the index derives from one distribution file.
// indexable is false ONLY for the D7 wheel verdict.
type distMetaFacts struct {
	requiresPython string // raw header value (no HTML escaping here)
	indexable      bool
}

// entryFacts derives one entry's facts from its blob. Non-distribution
// spellings are admitted untouched; a blob that cannot be opened (store
// fault) keeps its entry with the attribute absent: admission is a verdict
// about the BYTES (the D7 matrix), not about engine health.
func (h *Handler) entryFacts(ctx context.Context, e indexEntry) distMetaFacts {
	var wheel bool
	switch {
	case strings.HasSuffix(e.filename, ".whl"):
		wheel = true
	case strings.HasSuffix(e.filename, ".tar.gz"):
	default:
		return distMetaFacts{indexable: true}
	}
	facts := distMetaFacts{indexable: true}
	if rc, ref, err := h.st.Open(ctx, e.sha256); err == nil {
		if wheel {
			facts = wheelFacts(rc, ref.Size)
		} else {
			facts = sdistFacts(rc)
		}
		_ = rc.Close()
	}
	return facts
}

// enrichIndexEntries derives every entry's metadata facts and applies the
// D7 admission policy. Blobs are opened through the storage engine by the
// entry's sha256 — the no-stats seam: repo.Service.Get marks a download per
// read, which an index render must never do.
//
// ponytail: cost is linear in the project's file count per render (a zip
// central-directory read per wheel, a head-of-archive scan per sdist); if a
// huge-project page ever matters, persist pypi.requires.python as node
// properties at upload time via the NodePropStore seam instead.
func (h *Handler) enrichIndexEntries(ctx context.Context, entries []indexEntry) []indexEntry {
	out := entries[:0]
	for _, e := range entries {
		facts := h.entryFacts(ctx, e)
		if !facts.indexable {
			continue // D7: stored, never indexed
		}
		e.requiresPython = facts.requiresPython
		out = append(out, e)
	}
	return out
}

// wheelFacts reads one wheel's dist-info/METADATA. Every failure maps to a
// verdict rather than an error: an unopenable zip or a missing METADATA
// member is the evidenced not-indexed state (L015 P4 / L016 b2); a METADATA
// member that exists but will not read leaves the entry indexed without the
// attribute (the file IS metadata-bearing; its header block is unusable).
func wheelFacts(rc io.ReadCloser, size int64) distMetaFacts {
	zr, ok := openWheelZip(rc, size)
	if !ok {
		return distMetaFacts{indexable: false}
	}
	for _, f := range zr.File {
		// Wheel spec: a single top-level {distribution}-{version}.dist-info/
		// directory holding METADATA. The member name is not cross-checked
		// against the filename — the bad-name matrix arm proved the
		// reference indexes self-consistent metadata under either spelling.
		if !strings.HasSuffix(f.Name, ".dist-info/METADATA") || strings.Count(f.Name, "/") != 1 {
			continue
		}
		mr, err := f.Open()
		if err != nil {
			return distMetaFacts{indexable: true}
		}
		defer mr.Close() //nolint:errcheck // read-only member stream
		return distMetaFacts{
			indexable:      true,
			requiresPython: scanRequiresPython(io.LimitReader(mr, maxDistMetadataBytes)),
		}
	}
	return distMetaFacts{indexable: false}
}

// openWheelZip answers the zip reader over the blob, preferring ranged
// reads (io.ReaderAt: only the central directory and the METADATA member
// are read) and falling back to a bounded whole-file buffer.
func openWheelZip(rc io.ReadCloser, size int64) (*zip.Reader, bool) {
	if ra, ok := rc.(io.ReaderAt); ok {
		zr, err := zip.NewReader(ra, size)
		return zr, err == nil
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxWheelBufferBytes+1))
	if err != nil || int64(len(raw)) > maxWheelBufferBytes {
		return nil, false
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	return zr, err == nil
}

// sdistFacts scans one sdist's head for PKG-INFO (conventionally the first
// member, at {name}-{version}/PKG-INFO; a root-level PKG-INFO is accepted
// too). Any failure — not a gzip stream, unreadable tar, no PKG-INFO within
// the scan bounds — keeps the entry indexed with the attribute absent
// (un-evidenced face, deliberately not folded into the D7 wheel policy).
func sdistFacts(rc io.ReadCloser) distMetaFacts {
	gz, err := gzip.NewReader(rc)
	if err != nil {
		return distMetaFacts{indexable: true}
	}
	defer gz.Close() //nolint:errcheck // read-only stream
	tr := tar.NewReader(io.LimitReader(gz, maxSdistScanBytes))
	for i := 0; i < maxSdistScanMembers; i++ {
		hd, err := tr.Next()
		if err != nil {
			return distMetaFacts{indexable: true} // EOF or malformed before PKG-INFO
		}
		if hd.Typeflag != tar.TypeReg {
			continue
		}
		dir, base := path.Split(hd.Name)
		if base != "PKG-INFO" || strings.Count(strings.Trim(dir, "/"), "/") > 0 {
			continue // depth <= 2 only (root or {name}-{version}/)
		}
		return distMetaFacts{
			indexable:      true,
			requiresPython: scanRequiresPython(io.LimitReader(tr, maxDistMetadataBytes)),
		}
	}
	return distMetaFacts{indexable: true}
}

// scanRequiresPython extracts the Requires-Python value from a metadata
// header block (the RFC 822 form METADATA and PKG-INFO share):
// case-insensitive name, first occurrence wins, folded continuation lines
// joined with one space; "" when absent. The 64 KiB scanner line cap makes
// a hostile single-line header abort the scan rather than buffer it.
func scanRequiresPython(r io.Reader) string {
	sc := bufio.NewScanner(r)
	val := ""
	inRequires := false
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break // blank line ends the header block
		}
		if line[0] == ' ' || line[0] == '\t' {
			if inRequires {
				val += " " + strings.TrimSpace(line)
			}
			continue
		}
		inRequires = false
		if name, v, ok := strings.Cut(line, ":"); ok && val == "" &&
			strings.EqualFold(name, "Requires-Python") {
			val = strings.TrimSpace(v)
			inRequires = true
		}
	}
	return val
}
