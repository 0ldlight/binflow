package repo

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// The absolute-URL dependency pull-through's SERVICE half (M13 T-367,
// FR-117; helm.md section 6/S10's landing posture). The helm adapter's
// _external face used to stream third-party URLs straight through its own
// guarded client with no landing — the pre-T-367 posture registered in the
// T-342 report's D-3 evaluation, whose remedy this file is: the engine grew
// FetchAbsolute (the URL-override seam; internal/remote no longer assumes
// the upstream base IS the fetch base), and these methods wrap it with the
// same read gate, error mapping, GC-hold release and audit posture
// getRemote gives an ordinary path-joined fetch — so a cached dependency
// and a cached chart can never be served by different rules.

// Compile-time pin: the service implements the adapter-facing capability.
var _ RemoteExternalPlane = (*service)(nil)

// FetchExternal implements RemoteExternalPlane: one read-gated absolute-URL
// dependency fetch into the remote repository's cache at the folded proxy
// path. The engine's *FetchError maps verbatim onto a *StatusError (the
// unfound family wraps ErrNodeNotFound so the face keeps its 404 wording
// branch), and the MISS arm releases the landing session's GC hold and
// emits the artifact/cached webhook exactly like getRemote.
func (s *service) FetchExternal(ctx context.Context, p *Principal, repoKey, path, target string) (io.ReadSeekCloser, *metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, nil, err
	}
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return nil, nil, err
	}
	if row.Type != TypeRemote {
		return nil, nil, fmt.Errorf("%w: %s repositories have no external dependency pull-through", ErrRepoTypeNotSupported, row.Type)
	}
	if !s.allow(ctx, p, repoKey, path, ActionRead) {
		if p == nil {
			return nil, nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrUnauthorized)
		}
		return nil, nil, fmt.Errorf("read %s/%s: %w", repoKey, path, ErrForbidden)
	}
	return s.fetchExternalCore(ctx, p, repoKey, path, target, repoKey, "")
}

// FetchVirtualExternal implements RemoteExternalPlane: the virtual face's
// member twin. The read gate has already run on the VIRTUAL key (the
// adapter's route); the guard here is membership — the member must
// currently sit in the virtual's two-bucket order AND be a remote
// repository, the ReadVirtualMember posture (a local member has no egress
// face; the walk skips it before ever calling in). The landing goes into
// the MEMBER's cache namespace; the audit row is addressed to the virtual
// with the resolvedFrom detail, mirroring the aggregation face.
func (s *service) FetchVirtualExternal(ctx context.Context, p *Principal, virtualKey, member, path, target string) (io.ReadSeekCloser, *metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, nil, err
	}
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, nil, err
	}
	found := false
	for i := range order {
		if order[i].key == member {
			found = order[i].typ == TypeRemote
			break
		}
	}
	if !found {
		return nil, nil, fmt.Errorf("fetch member %s of virtual %s: %w: not a current remote member",
			member, virtualKey, ErrRepoNotFound)
	}
	frag := fmt.Sprintf(`"resolvedFrom":%q`, member)
	return s.fetchExternalCore(ctx, p, member, path, target, virtualKey, frag)
}

// fetchExternalCore is the ungated engine hop both faces share: the
// FetchAbsolute call, the error mapping and the MISS-side bookkeeping
// (getRemote's twin — one place so the two cache-serving faces cannot
// drift). auditRepo shapes the ONE download audit row's repository — the
// addressed repository key on the direct face, the VIRTUAL key on the
// member face — and auditFrag the member face's resolvedFrom detail
// fragment (the getVirtual + engine double-row posture T-365 registered,
// kept to a single row per call).
func (s *service) fetchExternalCore(ctx context.Context, p *Principal, repoKey, path, target, auditRepo, auditFrag string) (io.ReadSeekCloser, *metadata.Node, error) {
	if s.remoteEng == nil {
		return nil, nil, fmt.Errorf("%w: remote repositories have no engine wired", ErrRepoTypeNotSupported)
	}
	res, err := s.remoteEng.FetchAbsolute(ctx, repoKey, path, target)
	if err != nil {
		var fe *remote.FetchError
		if errors.As(err, &fe) {
			var cause error
			if fe.Unfound {
				cause = fmt.Errorf("node %s/%s: %w", repoKey, path, ErrNodeNotFound)
			}
			return nil, nil, &StatusError{Code: fe.Status, Message: fe.Message, cause: cause}
		}
		return nil, nil, fmt.Errorf("remote external fetch %s/%s: %w", repoKey, path, err)
	}
	// The [M9] ADR-0031 posture of getRemote: a MISS is the only state that
	// landed a blob in THIS call, so the hold's job is done; HIT/STALE
	// served an already-referenced copy.
	if res.CacheState == remote.CacheMiss && res.Node != nil {
		s.releaseGCHold(ctx, res.Node.Sha256)
		s.emitHook(ctx, webhook.Event{
			Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactCached,
			Repo: repoKey, Path: path, Sha256: res.Node.Sha256, Size: res.Node.Size,
			Actor: hookActorOf(p),
		})
	}
	// One download bookkeeping pair, shaped by the addressed surface: the
	// direct face is the remote-serving arm (both columns, origin remote);
	// the virtual member face counts the member's row (K69 arm 2) — the
	// member's cache served it, so the remote column moves there too.
	var extra []string
	if auditFrag != "" {
		extra = append(extra, auditFrag)
	}
	origin := downloadOriginVirtual(auditRepo)
	if auditRepo == repoKey {
		origin = downloadOriginRemote
		extra = append(extra, s.statsSyncDetail(ctx, repoKey))
	}
	s.markDownload(ctx, p, downloadMark{
		auditRepo: auditRepo, auditPath: path,
		countRepo: repoKey, countPath: path,
		origin:       origin,
		extra:        extra,
		remoteServed: true,
	})
	return res.Body, res.Node, nil
}
