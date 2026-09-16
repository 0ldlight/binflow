package repo

import (
	"context"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The D03-R13 checksum-audit kernel (L024-5, diff L4 — the V-z reading is
// voided): GET /api/search/badChecksum's hit set is the DB-side
// client-vs-server comparison the reference runs. The SERVER value of the
// requested type is the blobs ledger's registered digest; the CLIENT value
// is the digest the deploying client DECLARED (persisted on the node row at
// write time). A row is bad when the declaration is MISSING or differs —
// the stored bytes are never rescanned (the reference's own posture: a
// physically corrupted blob whose declaration matches its registered digest
// does not flag, the differential's four-point live proof).

// ChecksumAuditService is the badChecksum capability face of the concrete
// Service implementation (the LegacySearchService precedent).
type ChecksumAuditService interface {
	// AuditChecksums returns the file nodes whose CLIENT-declared typ
	// digest is missing or differs from the registered (server) digest,
	// (repo_key, path) ordered, capped at limit. typ is md5, sha1 or
	// sha256.
	AuditChecksums(ctx context.Context, p *Principal, typ string, limit int, repos []string) ([]ChecksumMismatch, error)
}

// ChecksumMismatch is one bad-checksum hit: the node, the registered
// (server) digest and the client-declared digest of the requested type.
type ChecksumMismatch struct {
	RepoKey string
	Path    string
	Server  string
	Client  string
}

// AuditChecksums implements ChecksumAuditService.
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
	nodes, err := ns.SearchByPath(ctx, metadata.PathFilter{}, limit+1, repos)
	if err != nil {
		return nil, fmt.Errorf("badChecksum scan: %w", err)
	}
	nodes = s.filterVisible(ctx, p, nodes)
	var out []ChecksumMismatch
	for _, n := range nodes {
		server := n.Sha256
		client := n.ClientSha256
		if typ != "sha256" {
			blob, err := s.md.Blobs().Get(ctx, n.Sha256)
			if err != nil {
				continue // no blob row: a repair case, not a checksum verdict
			}
			if typ == "sha1" {
				server, client = blob.Sha1, n.ClientSha1
			} else {
				server, client = blob.Md5, n.ClientMd5
			}
		}
		if client != "" && client == server {
			continue // the declared matches the registered: not bad
		}
		out = append(out, ChecksumMismatch{RepoKey: n.RepoKey, Path: n.Path, Server: server, Client: client})
	}
	return out, nil
}
