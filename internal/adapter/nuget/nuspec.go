package nuget

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// The .nupkg validation chain and the nuspec fact model.
//
// Push lands the package through a temp-file spool: the body streams to
// disk while an inline SHA-512 is computed (the .sha512 sidecar and the
// registration packageHash are the BASE64 of exactly this hash — the
// official flatcontainer contract), then the landed file opens as a zip
// for the nuspec extraction. Nothing package-sized is ever held in memory
// whole (the metadata-class 64MB cap is not this path's law; NuGet
// packages reach hundreds of MB).

// nupkgBodyLimit is the spool ceiling (nuget.org's documented package-size
// ceiling, a generous bound; a larger body refuses before the disk fills).
const nupkgBodyLimit = 512 << 20 // 512 MiB

// nuspecSizeLimit bounds one extracted nuspec (real nuspecs are < 1MB).
const nuspecSizeLimit = 4 << 20 // 4 MiB

// dependency is one nuspec <dependency> row.
type dependency struct {
	id        string
	rangeSpec string // the nuspec version-range spelling, "" when absent
}

// dependencyGroup is one nuspec <group> (or the implicit flat list).
type dependencyGroup struct {
	targetFramework string
	deps            []dependency
}

// nuspecInfo is the parsed fact set the generated documents render from.
type nuspecInfo struct {
	id                    string
	version               string // the nuspec's spelling, verbatim
	title                 string
	summary               string
	description           string
	authors               []string
	owners                []string
	licenseURL            string
	projectURL            string
	iconURL               string
	requireLicenseAccept  bool
	tags                  []string
	language              string
	minClientVersion      string
	dependencyGroups      []dependencyGroup
	developmentDependency bool
}

// errInvalidPackage marks every push-validation refusal (mapped to 400).
var errInvalidPackage = errors.New("invalid package")

// spooledPackage is one landed push body plus its measured digest. The
// SHA-256 measurement of the T-337 era is gone since D-10's ruling: its
// only consumer was the v2 publish's declared-digest arm (the
// idempotent-retransmit short-circuit T-378 removed); the sidecar the
// official protocol spells is SHA-512.
type spooledPackage struct {
	file   *os.File
	size   int64
	sha512 []byte // raw digest; base64 at render time
}

// spoolNupkg streams the request body into a temp file, hashing SHA-512
// and counting bytes. The caller owns the file (close removes it).
func spoolNupkg(body io.Reader) (*spooledPackage, error) {
	f, err := os.CreateTemp("", "binflow-nuget-*.nupkg")
	if err != nil {
		return nil, fmt.Errorf("spool upload: %w", err)
	}
	sp := &spooledPackage{file: f}
	h := sha512.New()
	buf := make([]byte, 64<<10)
	var n int64
	for {
		r, rerr := body.Read(buf)
		if r > 0 {
			n += int64(r)
			if n > nupkgBodyLimit {
				return nil, fmt.Errorf("%w: package exceeds the %d MiB ceiling", errInvalidPackage, nupkgBodyLimit>>20)
			}
			if _, werr := h.Write(buf[:r]); werr != nil {
				return nil, fmt.Errorf("hash upload: %w", werr)
			}
			if _, werr := f.Write(buf[:r]); werr != nil {
				return nil, fmt.Errorf("spool upload: %w", werr)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("read upload: %w", rerr)
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("%w: empty package body", errInvalidPackage)
	}
	sp.size = n
	sp.sha512 = h.Sum(nil)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind spool: %w", err)
	}
	return sp, nil
}

// close removes the spool (the push path's only temp artifact). The file
// name is THIS function's own CreateTemp return — never a client string —
// so the removal cannot traverse.
func (sp *spooledPackage) close() {
	_ = sp.file.Close()           //nolint:errcheck // read-only fd close on cleanup
	_ = os.Remove(sp.file.Name()) //nolint:gosec // G703: name is os.CreateTemp's own, not client input //nolint:errcheck // best-effort temp cleanup
}

// identityFromNuspec derives the push target wholly from the package: the
// single root-level nuspec's id/version, normalized into the storage keys
// (the DIRECT push shape's authority — no URL identity exists to check).
func (sp *spooledPackage) identityFromNuspec() ([]byte, pkgRef, error) {
	body, err := sp.extractNuspec("")
	if err != nil {
		return nil, pkgRef{}, err
	}
	info, err := parseNuspec(body)
	if err != nil {
		return nil, pkgRef{}, err
	}
	if !validPackageID(info.id) {
		return nil, pkgRef{}, fmt.Errorf("%w: nuspec id %q is not a legal package id", errInvalidPackage, info.id)
	}
	version, ok := normalizeNuGetVersion(info.version)
	if !ok {
		return nil, pkgRef{}, fmt.Errorf("%w: nuspec version %q is not a valid version", errInvalidPackage, info.version)
	}
	return body, pkgRef{id: lowerASCII(info.id), version: version}, nil
}

// extractNuspec opens the spooled body as a zip and reads the package's
// nuspec: the entry named <id>.nuspec (case-insensitive — the client's id
// spelling is the authority for the file name), falling back to the single
// root-level *.nuspec when the id does not match or no id was given (the
// gallery-tolerant form). The returned bytes are bounded by
// nuspecSizeLimit.
func (sp *spooledPackage) extractNuspec(id string) ([]byte, error) {
	zr, err := zip.NewReader(sp.file, sp.size)
	if err != nil {
		return nil, fmt.Errorf("%w: not a valid package (zip): %w", errInvalidPackage, err)
	}
	var fallback *zip.File
	for _, f := range zr.File {
		base := f.Name
		if strings.Contains(base, "/") {
			continue // root-level entries only
		}
		if !strings.HasSuffix(lowerASCII(base), suffixNuspec) {
			continue
		}
		if id != "" && lowerASCII(strings.TrimSuffix(base, suffixNuspec)) == lowerASCII(id) {
			return readZipEntry(f)
		}
		if fallback == nil {
			fallback = f
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("%w: no nuspec found in the package", errInvalidPackage)
	}
	return readZipEntry(fallback)
}

// readZipEntry reads one bounded entry.
func readZipEntry(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > nuspecSizeLimit {
		return nil, fmt.Errorf("%w: nuspec exceeds the %d MiB ceiling", errInvalidPackage, nuspecSizeLimit>>20)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open nuspec entry: %w", err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, nuspecSizeLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read nuspec entry: %w", err)
	}
	if int64(len(body)) > nuspecSizeLimit {
		return nil, fmt.Errorf("%w: nuspec exceeds the %d MiB ceiling", errInvalidPackage, nuspecSizeLimit>>20)
	}
	return body, nil
}

// nuspecXML is the subset of the nuspec schema the generated documents
// render from (the official .nuspec reference's metadata element).
type nuspecXML struct {
	Metadata struct {
		ID           string `xml:"id"`
		Version      string `xml:"version"`
		Title        string `xml:"title"`
		Authors      string `xml:"authors"`
		Owners       string `xml:"owners"`
		LicenseURL   string `xml:"licenseUrl"`
		ProjectURL   string `xml:"projectUrl"`
		IconURL      string `xml:"iconUrl"`
		Summary      string `xml:"summary"`
		Description  string `xml:"description"`
		Language     string `xml:"language"`
		Tags         string `xml:"tags"`
		Require      *bool  `xml:"requireLicenseAcceptance"`
		DevDep       *bool  `xml:"developmentDependency"`
		MinClient    string `xml:"minClientVersion"`
		Dependencies struct {
			Dependency []struct {
				ID      string `xml:"id,attr"`
				Version string `xml:"version,attr"`
			} `xml:"dependency"`
			Group []struct {
				TargetFramework string `xml:"targetFramework,attr"`
				Dependency      []struct {
					ID      string `xml:"id,attr"`
					Version string `xml:"version,attr"`
				} `xml:"dependency"`
			} `xml:"group"`
		} `xml:"dependencies"`
	} `xml:"metadata"`
}

// parseNuspec parses nuspec bytes into the fact model. The decoder is the
// standard library's own XML parser with NO DTD/entity resolution wired
// (encoding/xml declines external entities by default) — the decoded
// tree is inert data, never code.
func parseNuspec(body []byte) (*nuspecInfo, error) {
	var doc nuspecXML
	dec := xml.NewDecoder(bytes.NewReader(body)) //nolint:gosec // G709: inert field decode, no entity expansion
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: nuspec is not valid XML: %w", errInvalidPackage, err)
	}
	m := doc.Metadata
	info := &nuspecInfo{
		id:                    strings.TrimSpace(m.ID),
		version:               strings.TrimSpace(m.Version),
		title:                 m.Title,
		summary:               m.Summary,
		description:           m.Description,
		authors:               splitList(m.Authors),
		owners:                splitList(m.Owners),
		licenseURL:            m.LicenseURL,
		projectURL:            m.ProjectURL,
		iconURL:               m.IconURL,
		language:              m.Language,
		minClientVersion:      m.MinClient,
		tags:                  splitTags(m.Tags),
		requireLicenseAccept:  m.Require != nil && *m.Require,
		developmentDependency: m.DevDep != nil && *m.DevDep,
	}
	if m.Dependencies.Group != nil {
		for _, g := range m.Dependencies.Group {
			grp := dependencyGroup{targetFramework: strings.TrimSpace(g.TargetFramework)}
			for _, d := range g.Dependency {
				grp.deps = append(grp.deps, dependency{id: d.ID, rangeSpec: d.Version})
			}
			info.dependencyGroups = append(info.dependencyGroups, grp)
		}
	} else {
		var flat []dependency
		for _, d := range m.Dependencies.Dependency {
			flat = append(flat, dependency{id: d.ID, rangeSpec: d.Version})
		}
		if flat != nil {
			info.dependencyGroups = []dependencyGroup{{deps: flat}}
		}
	}
	return info, nil
}

// splitList splits a comma-separated nuspec author/owner list.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitTags splits a whitespace-separated tag list.
func splitTags(s string) []string {
	return strings.Fields(s)
}

// validatePush checks the package against the push target (the official
// rule: the embedded nuspec's id/version are the package's identity and
// must match the addressed id/version). The nuspec bytes are extracted and
// returned — the push stores them as the sidecar.
func (sp *spooledPackage) validatePush(target pkgRef) ([]byte, *nuspecInfo, error) {
	body, err := sp.extractNuspec(target.id)
	if err != nil {
		return nil, nil, err
	}
	info, err := parseNuspec(body)
	if err != nil {
		return nil, nil, err
	}
	if lowerASCII(info.id) != target.id {
		return nil, nil, fmt.Errorf("%w: nuspec id %q does not match the addressed id %q",
			errInvalidPackage, info.id, target.id)
	}
	nuspecNorm, ok := normalizeNuGetVersion(info.version)
	if !ok {
		return nil, nil, fmt.Errorf("%w: nuspec version %q is not a valid version", errInvalidPackage, info.version)
	}
	if nuspecNorm != target.version {
		return nil, nil, fmt.Errorf("%w: nuspec version %q does not match the addressed version %q",
			errInvalidPackage, info.version, target.version)
	}
	return body, info, nil
}

// sha512Base64 renders the measured digest in the official sidecar
// spelling (standard base64, trailing '=' kept, trailing newline none).
func (sp *spooledPackage) sha512Base64() string {
	return base64.StdEncoding.EncodeToString(sp.sha512[:])
}
