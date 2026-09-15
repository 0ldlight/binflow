package repo

import (
	"context"
	"crypto/md5"  //nolint:gosec // G501: Artifactory-compatibility digest, never a security primitive (storage/digest.go precedent)
	"crypto/sha1" //nolint:gosec // G505: same, ancillary protocol digest
	"crypto/sha256"
	"fmt"
	"hash"
	"io"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The D03-R13 checksum-audit kernel (L024-3A, aql.md §16.5-R13): GET
// /api/search/badChecksum's hit set. A "bad checksum" row is a file node
// whose REGISTERED digest (the blobs ledger — the value the upload itself
// recorded) differs from the ACTUAL digest of the stored content: the
// corruption the storage plane's own Stat probe detects, reported per the
// requested digest type. The wire arms (400 copies, the admin gate) live in
// httpapi; this kernel only computes hits.
//
// ponytail: the scan streams every candidate blob once (O(total bytes)) —
// an on-demand admin face, the same cost class Artifactory's own
// recalculation pays; a cached/integrity-map accelerator can slot behind
// this seam if a differential ever flags the latency.

// ChecksumAuditService is the badChecksum capability face of the concrete
// Service implementation (the LegacySearchService precedent).
type ChecksumAuditService interface {
	// AuditChecksums returns the file nodes whose registered typ digest
	// differs from the digest of the stored content, (repo_key, path)
	// ordered, capped at limit. typ is md5, sha1 or sha256.
	AuditChecksums(ctx context.Context, p *Principal, typ string, limit int, repos []string) ([]ChecksumMismatch, error)
}

// ChecksumMismatch is one bad-checksum hit: the node, its registered digest
// (the blobs ledger value) and the actual digest of the content on disk.
type ChecksumMismatch struct {
	RepoKey    string
	Path       string
	Registered string
	Actual     string
}

// AuditChecksums implements ChecksumAuditService. The admin verdict is the
// endpoint's (this kernel still runs the read gate so a wired-but-exposed
// face cannot read past the ACL); blob-open failures are skipped as repair
// cases, not checksum verdicts — a missing file has no actual digest to
// report.
func (s *service) AuditChecksums(ctx context.Context, p *Principal, typ string, limit int, repos []string) ([]ChecksumMismatch, error) {
	if typ != "md5" && typ != "sha1" && typ != "sha256" {
		return nil, fmt.Errorf("%w: unknown checksum type %q", ErrInvalidSearchQuery, typ)
	}
	if err := validateLegacyLimit(limit); err != nil {
		return nil, err
	}
	if err := validateReposFilter(repos); err != nil {
		return nil, err
	}
	if err := s.searchGate(ctx, p); err != nil {
		return nil, err
	}
	ns, err := s.searcher()
	if err != nil {
		return nil, err
	}
	nodes, err := ns.SearchByPath(ctx, metadata.PathFilter{}, limit, repos)
	if err != nil {
		return nil, fmt.Errorf("badChecksum scan: %w", err)
	}
	nodes = s.filterVisible(ctx, p, nodes)
	var out []ChecksumMismatch
	for _, n := range nodes {
		hit, err := s.auditNodeChecksum(ctx, n, typ)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue // repair case (blob gone/unreadable): no verdict
		}
		if hit != nil {
			out = append(out, *hit)
		}
	}
	return out, nil
}

// auditNodeChecksum compares one node's registered typ digest against the
// actual content digest. A nil hit is a clean row; an error means no verdict
// was computable.
func (s *service) auditNodeChecksum(ctx context.Context, n *metadata.Node, typ string) (*ChecksumMismatch, error) {
	registered := n.Sha256
	if typ != "sha256" {
		blob, err := s.md.Blobs().Get(ctx, n.Sha256)
		if err != nil {
			return nil, fmt.Errorf("badChecksum blob row %s: %w", n.Sha256, err)
		}
		if typ == "sha1" {
			registered = blob.Sha1
		} else {
			registered = blob.Md5
		}
	}
	rc, _, err := s.st.Open(ctx, n.Sha256)
	if err != nil {
		return nil, fmt.Errorf("badChecksum open %s: %w", n.Sha256, err)
	}
	defer rc.Close() //nolint:errcheck // read-only stream
	var h hash.Hash
	switch typ {
	case "md5":
		h = md5.New() //nolint:gosec // G401: compatibility digest, not a security primitive
	case "sha1":
		h = sha1.New() //nolint:gosec // G401: compatibility digest, not a security primitive
	default:
		h = sha256.New()
	}
	if _, err := io.Copy(h, rc); err != nil {
		return nil, fmt.Errorf("badChecksum read %s: %w", n.Sha256, err)
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual == registered {
		return nil, nil
	}
	return &ChecksumMismatch{RepoKey: n.RepoKey, Path: n.Path, Registered: registered, Actual: actual}, nil
}
