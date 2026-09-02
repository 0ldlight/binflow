package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// Per-node download counting (M16 T-438, FR-146.2 / ADR-0044 K69,
// architecture 25.5). The nodes table's four statistics columns are the
// download plane's SINGLE counting channel — the data source of the ?stats
// wire face, the batch-3 field family and the FR-148 usage domain. There is
// no second counter, no derived aggregate and no audit-side tally: wherever
// a download audit row lands, the count lands with it (same site, same
// best-effort posture — the markDownload helper below is the one seam), and
// nowhere else. Adding a counting site means adding an audit download site;
// splitting the pair is a review reject.
//
// The three-arm criterion (K69 decision 2):
//
//   - direct — a GET on the repository that stores the node;
//   - via virtual — a virtual resolution hit; the count lands on the
//     MEMBER's row (a virtual owns no node rows);
//   - remote-serving — a download a remote repository's cache served
//     (fresh, landed-this-call or stale; including a fetch-then-serve).
//
// Every arm bumps download_count and refreshes last_downloaded_at/by on
// the serving row. A serving row that lives in a REMOTE repository
// additionally bumps remote_download_count — the column's own semantics,
// "downloads this remote repository served", which covers both the direct
// remote GET (arm 3) and a virtual resolution onto a remote member (the
// remote cache served that download too). Local rows keep the column at
// its structural zero. This is deliberately NOT Artifactory's smart-remote
// pull-back remote_downloads: that family counts downloads BY downstream
// proxying instances, has no source in BinFlow and stays unsourced
// (docs/reverse/aql.md section 14.1 — the AQL wire face's mapping is
// T-440's call, registered there).
//
// Folder rows stay at zero by construction: the store-side UPDATE excludes
// the shared folder marker, so the folder-download archive face (whose
// audit row addresses the folder root) is a structural no-op. HEAD requests
// and cache probes never reach this seam — they never record an audit
// download row either.

// The origin dimension every download audit row's detail carries (K69
// decision 6: the three-arm audit trail; nodes grows no column for it).
const (
	downloadOriginDirect = "direct"
	downloadOriginRemote = "remote"
)

// downloadOriginVirtual spells the via-virtual origin: "virtual:<vkey>"
// names the resolution surface the caller addressed.
func downloadOriginVirtual(virtualKey string) string { return "virtual:" + virtualKey }

// downloadDetail renders the download audit row's detail object: the
// origin dimension always first, then any site-specific fragments the
// caller pre-rendered as `"key":value` (resolvedFrom, aggregate, the
// archive facts) — the existing detail spellings ride along unchanged
// instead of being rebuilt around the new key. Empty fragments (an absent
// statsSync marker) drop out rather than leaving a hole in the object.
func downloadDetail(origin string, extra ...string) string {
	var kept []string
	for _, e := range extra {
		if e != "" {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return fmt.Sprintf(`{"origin":%q}`, origin)
	}
	return fmt.Sprintf(`{"origin":%q,%s}`, origin, strings.Join(kept, ","))
}

// downloadMark addresses one download landing. auditRepo/auditPath carry
// the row the audit log sees (the VIRTUAL key on virtual arms — the
// surface the caller addressed); countRepo/countPath carry the node row
// that takes the count (the member on virtual arms, the addressed
// repository everywhere else). remoteServed marks a serving row in a
// remote repository (the extra column); extra carries pre-rendered detail
// fragments.
type downloadMark struct {
	auditRepo    string
	auditPath    string
	countRepo    string
	countPath    string
	origin       string
	extra        []string
	remoteServed bool
}

// markDownload is the download plane's single bookkeeping pair: the audit
// row (ActionDownload, origin dimension folded into the detail) and the
// per-node count UPDATE, at one site, in that order, both best-effort — a
// counting failure is logged and never fails a served download (the audit
// append's own posture, technical debt #4).
func (s *service) markDownload(ctx context.Context, p *Principal, m downloadMark) {
	who := actor(p)
	s.audit(ctx, AuditEvent{
		Actor:  who,
		Action: AuditActionDownload,
		Repo:   m.auditRepo,
		Path:   m.auditPath,
		Detail: downloadDetail(m.origin, m.extra...),
	})
	s.countDownload(ctx, m.countRepo, m.countPath, who, m.remoteServed)
}

// countDownload lands one increment through the busy budget (T-423): the
// download faces already sit at one audit-write-per-GET magnitude, and one
// single-statement UPDATE beside it is the same class of write — zero
// second transaction, the NFR-P71 structural guarantee.
func (s *service) countDownload(ctx context.Context, repoKey, path, by string, remoteServed bool) {
	if repoKey == "" || path == "" {
		return // the archive root spelling: no row to count
	}
	at := s.now()
	err := s.cacheBusyRetry(ctx, "node download count", func(ctx context.Context) error {
		return s.md.Nodes().CountDownload(ctx, repoKey, path, by, at, remoteServed)
	})
	if err != nil {
		slog.WarnContext(ctx, "repo: download count failed",
			"repo", repoKey, "path", path, "error", err.Error())
	}
}

// statsSyncDetail returns the remote-serving arm's contentSynchronisation
// eligibility fragment: enabled && statisticsEnabled adds
// `"statsSync":true` to the audit detail (K69 decision 6). Counting itself
// is unconditional — local statistics are local facts, never gated on a
// synchronisation key — the marker only records that this download WOULD
// be reported upstream once a transport exists (the notification body is
// out of scope and registered as architecture 11.48's gap).
func (s *service) statsSyncDetail(ctx context.Context, remoteRepo string) string {
	if !s.statsSyncEligible(ctx, remoteRepo) {
		return ""
	}
	return `"statsSync":true`
}

// statsSyncEligible resolves a remote repository's contentSynchronisation
// policy row (the same canonical-config read remoteContentTTL performs).
// Every failure path answers false: the marker is telemetry, never a gate.
func (s *service) statsSyncEligible(ctx context.Context, remoteRepo string) bool {
	row, err := s.md.Repos().Get(ctx, remoteRepo)
	if err != nil || row == nil || row.Config == "" {
		return false
	}
	pol := remoteConfig{}
	if err := json.Unmarshal([]byte(row.Config), &pol); err != nil {
		return false
	}
	return pol.ContentSynchronisation.Enabled && pol.ContentSynchronisation.StatisticsEnabled
}
