package pypi

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// metadataProvider is the PyPI MetadataProvider (architecture section
// 5.4): the pure path semantics the remote cache (T-66) and the virtual
// aggregation (T-72) consume. Every method is a pure function of the
// repository-relative path; it must stay safe for concurrent use.
type metadataProvider struct{}

// Protocol implements adapter.MetadataProvider; the literal is the same
// constant the Handler answers (kept that way by Register, which is the
// only assembly door — T-63 review N2).
func (metadataProvider) Protocol() string { return Protocol }

// Classify implements adapter.MetadataProvider. The PyPI layout keeps no
// stored metadata documents (no hidden .pypi/ index files by design), but
// the ROUTES address metadata: a simple/ path is a regenerable index page
// (short TTL, conditional revalidation for the remote cache), everything
// else — packages/ downloads, bare <name>/<version>/<filename> storage
// paths, and unknown shapes — is immutable content (long TTL, the safe
// default the SPI prescribes).
func (metadataProvider) Classify(relPath string) adapter.MetadataKind {
	trimmed := strings.TrimPrefix(relPath, "/")
	if trimmed == segSimple || strings.HasPrefix(trimmed, segSimple+"/") {
		return adapter.KindMetadata
	}
	return adapter.KindContent
}

// PackageName implements adapter.MetadataProvider: the PEP 503 normalized
// project name a path belongs to, for every spelling the layout accepts —
// the storage shape (<name>/<version>/<filename>), the download mount
// (packages/<name>/<version>/<filename>) and the index page
// (simple/<name>). ok is false for anything else (the legacy JSON mount,
// bare protocol segments, deeper index paths), which the T-72 aggregation
// skips.
func (metadataProvider) PackageName(relPath string) (string, bool) {
	trimmed := strings.TrimPrefix(relPath, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return "", false
	}
	name := ""
	switch {
	case strings.HasPrefix(trimmed, segPackages+"/"):
		name, _, _ = strings.Cut(strings.TrimPrefix(trimmed, segPackages+"/"), "/")
	case strings.HasPrefix(trimmed, segSimple+"/"):
		var extra string
		name, extra, _ = strings.Cut(strings.TrimPrefix(trimmed, segSimple+"/"), "/")
		if extra != "" {
			return "", false // /simple/<name>/<version>: reserved, no package page
		}
	default:
		var extra string
		name, extra, _ = strings.Cut(trimmed, "/")
		if extra == "" {
			return "", false // a bare repository root or single segment
		}
		if name == segLegacyJSON {
			return "", false
		}
	}
	if name == "" {
		return "", false
	}
	return normalizePackageName(name), true
}

// Versions implements adapter.MetadataProvider: nil — BinFlow M3 defines no
// PEP 440 ordering (the virtual best-version seam is M4; T-72's simple
// aggregation merges entries without ordering). Callers fall back to
// first-hit resolution, the SPI-documented posture.
func (metadataProvider) Versions() adapter.VersionComparator { return nil }

// Register is the single-call assembly hook (T-63 review N2): it builds the
// handler AND enters both process-wide registries under one literal —
// adapter.Register for the HTTP dispatch key and adapter.RegisterMetadata
// for the SPI consumers — so the handler's protocol and the provider's can
// never drift apart. Duplicate registration panics at startup (the
// registry's own assembly-bug contract).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, st storage.Engine) *Handler {
	h := New(svc, repos, blobs, st)
	adapter.Register(h)
	adapter.RegisterMetadata(metadataProvider{})
	return h
}
