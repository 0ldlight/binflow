package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// The M4 repository governance plane (T-95, FR-24-AC4/W12a + FR-31/GE-05,
// ADR-0015): per-repository includesPattern/excludesPattern path gates and
// the repo-level quotaBytes ceiling, both carried in the repository's config
// JSON and both enforced inside repo.Service's write/read chain — the single
// choke point every protocol adapter already funnels through (architecture
// sections 4.6/11.17), so no adapter changes and no bypass seam exists.
//
// DEFAULTS ARE FREE: a repository configured without any governance field
// parses to the zero governance (includes "**/*", excludes none, quota 0) and
// every gate below short-circuits to "allow" BEFORE any additional store
// read — the M1~M3 behavior contract (W12a: "默认仓行为与 M1~M3 逐字节一致")
// holds by construction, not by coincidence.

// defaultIncludesPattern is the product default of includesPattern
// (repo-semantics section 6, high confidence) and matches every path.
const defaultIncludesPattern = "**/*"

// governance is the parsed governance view of one repository config blob.
// Parsing is a tolerant single JSON probe: fields absent or the blob not
// JSON at all (a hand-mangled row) degrade to the defaults, never fail the
// request — governance tightens a repository, it must not be able to break
// reads and writes that worked before the fields existed.
type governance struct {
	// includes holds the comma-separated includesPattern entries; nil/empty
	// means the default "**/*" (include everything).
	includes []string
	// excludes holds the comma-separated excludesPattern entries; empty means
	// no exclusions.
	excludes []string
	// includesRaw/excludesRaw are the stored spellings, for the 409 message
	// (the refusal names the configured patterns, W12a "message 含 pattern")
	// and for nothing else.
	includesRaw string
	excludesRaw string
	// quotaBytes is the configured ceiling; 0 = unlimited (the default and
	// the M1~M3 behavior).
	quotaBytes int64
}

// parseGovernance probes one repository config blob for the governance
// fields. It never returns an error (see the type comment); CREATE/UPDATE
// time validation is validateLocalConfig's job (config.go), this read-side
// probe only degrades.
func parseGovernance(config string) governance {
	var probe struct {
		IncludesPattern string `json:"includesPattern"`
		ExcludesPattern string `json:"excludesPattern"`
		QuotaBytes      *int64 `json:"quotaBytes"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return governance{}
	}
	g := governance{
		includes:    splitPatterns(probe.IncludesPattern),
		excludes:    splitPatterns(probe.ExcludesPattern),
		includesRaw: probe.IncludesPattern,
		excludesRaw: probe.ExcludesPattern,
	}
	if probe.QuotaBytes != nil && *probe.QuotaBytes > 0 {
		g.quotaBytes = *probe.QuotaBytes
	}
	return g
}

// splitPatterns splits one comma-separated pattern field into trimmed
// entries (Artifactory's list spelling; repo-semantics section 6). Empty
// entries drop; an all-empty result is nil (= the field's default).
func splitPatterns(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// default reports whether the governance is the no-op configuration (every
// gate then short-circuits, which is what keeps unconfigured repositories on
// the M1~M3 path byte-for-byte).
func (g governance) defaulted() bool {
	return len(g.excludes) == 0 && (len(g.includes) == 0 || matchesAllPattern(g.includes)) && g.quotaBytes == 0
}

// matchesAllPattern reports whether the pattern list is exactly the
// everything-pattern in any of its equivalent spellings.
func matchesAllPattern(patterns []string) bool {
	if len(patterns) != 1 {
		return false
	}
	switch patterns[0] {
	case "", "**", "**/*":
		return true
	}
	return false
}

// allowsPath reports whether the path passes the include/exclude pair:
// excludes run FIRST and win (repo-semantics section 6 / auth-model section
// 4's shared "exclude 优先" rule), then the path must match some include.
func (g governance) allowsPath(path string) bool {
	for _, p := range g.excludes {
		if (govMatcher{pattern: p}).match(path) {
			return false
		}
	}
	if len(g.includes) == 0 {
		return true // no includes configured: the "**/*" default
	}
	for _, p := range g.includes {
		if (govMatcher{pattern: p}).match(path) {
			return true
		}
	}
	return false
}

// rejectPut is the write-plane pattern refusal (W12a): the 409 whose
// message names the configured patterns. The spec's exact IncludeExclude
// wording is still uncaptured (repo-semantics section 9 item 2 — 动态捕获
// pending), so BinFlow's own message carries the two governing values; the
// STATUS is the converged dual-value ruling (download 404 / upload 409).
func (g governance) rejectPut(repoKey, path string) error {
	includes := g.includesRaw
	if strings.TrimSpace(includes) == "" {
		includes = defaultIncludesPattern
	}
	excludes := g.excludesRaw
	if strings.TrimSpace(excludes) == "" {
		excludes = "(none)"
	}
	for _, p := range g.excludes {
		if (govMatcher{pattern: p}).match(path) {
			return &StatusError{
				Code: http.StatusConflict,
				Message: fmt.Sprintf(
					"Repository '%s' rejected deployment of '%s': the path matches excludesPattern '%s' (includesPattern '%s').",
					repoKey, path, excludes, includes),
				cause: fmt.Errorf("%w: %s/%s hit excludesPattern %q",
					ErrPatternRejected, repoKey, path, excludes),
			}
		}
	}
	return &StatusError{
		Code: http.StatusConflict,
		Message: fmt.Sprintf(
			"Repository '%s' rejected deployment of '%s': the path does not match includesPattern '%s' (excludesPattern '%s').",
			repoKey, path, includes, excludes),
		cause: fmt.Errorf("%w: %s/%s misses includesPattern %q",
			ErrPatternRejected, repoKey, path, includes),
	}
}

// ---- the pattern matcher ----
//
// Ant-style two-level wildcard matching over repo-relative paths, the same
// semantics internal/auth/pathmatch.go implements for permission targets
// (auth-model section 4, the shared o.a.a.util.PathMatcher lineage): '*'
// spans one segment, '**' spans any run of segments, a trailing '/**' covers
// the directory and everything under it, and a directory-name pattern
// covers deeper paths only through the folder form (matchStart under the
// isFolder gate). The implementation is duplicated here rather than shared
// because auth's matcher is deliberately unexported (a shared
// internal/pathutil extraction would be a cross-package refactor outside
// this ticket's area; flagged for review).
type govMatcher struct {
	pattern string
}

// match reports whether path (repo-relative, no leading '/') matches the
// pattern. A trailing '/' on path marks a folder and is the only form that
// can match through the directory-prefix rule.
func (m govMatcher) match(path string) bool {
	pattern := m.pattern
	if pattern == "" || pattern == "**" || pattern == "**/*" {
		return true
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	isFolder := strings.HasSuffix(path, "/")
	return govSegsMatch(splitGovSegments(pattern), splitGovSegments(path), isFolder)
}

// govSegsMatch matches pattern segments against path segments with
// backtracking on '**'. After the pattern is consumed, leftover path
// segments are covered only when the path is a folder AND the last pattern
// segment was a plain directory name (the matchStart rule).
func govSegsMatch(pSegs, tSegs []string, isFolder bool) bool {
	pi, ti := 0, 0
	lastPlain := false
	for pi < len(pSegs) {
		seg := pSegs[pi]
		switch {
		case seg == "**":
			rest := pSegs[pi+1:]
			for skip := ti; skip <= len(tSegs); skip++ {
				if govSegsMatch(rest, tSegs[skip:], isFolder) {
					return true
				}
			}
			return false
		case ti >= len(tSegs):
			return false
		case !govSegmentMatch(seg, tSegs[ti]):
			return false
		default:
			lastPlain = !strings.Contains(seg, "*")
			pi++
			ti++
		}
	}
	if ti == len(tSegs) {
		return true
	}
	return isFolder && lastPlain && pi > 0
}

// govSegmentMatch matches one segment: '*' spans anything except '/'
// (guaranteed by segmentation), everything else compares literally.
func govSegmentMatch(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == segment
	}
	return govWildcardEqual(pattern, segment)
}

// govWildcardEqual compares a '*' bearing segment with a concrete segment.
func govWildcardEqual(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	if !strings.HasSuffix(s, last) {
		return false
	}
	s = s[:len(s)-len(last)]
	for _, mid := range parts[1 : len(parts)-1] {
		if mid == "" {
			continue
		}
		idx := strings.Index(s, mid)
		if idx < 0 {
			return false
		}
		s = s[idx+len(mid):]
	}
	return true
}

// splitGovSegments splits a pattern/path on '/', trimming whitespace per
// segment (Ant token trimming) and dropping empties.
func splitGovSegments(s string) []string {
	raw := strings.Split(s, "/")
	segs := make([]string, 0, len(raw))
	for _, seg := range raw {
		if seg = strings.TrimSpace(seg); seg != "" {
			segs = append(segs, seg)
		}
	}
	return segs
}

// ---- the quota gate (GE-05/W26) ----

// quotaRefusal is the 413 (E-01 envelope at the REST plane, spec body at
// the protocol planes through the StatusError verbatim contract): the
// message names the ceiling and both numbers — "quota exceeded" plus the
// used/quota pair the AC greps for.
func quotaRefusal(repoKey, path string, used, quota, incoming int64) *StatusError {
	return &StatusError{
		Code: http.StatusRequestEntityTooLarge,
		Message: fmt.Sprintf(
			"Repository '%s' quota exceeded: used %d of %d bytes; the write to '%s' needs %d more bytes.",
			repoKey, used, quota, path, incoming),
		cause: fmt.Errorf("%w: repository %s holds %d of %d bytes", ErrQuotaExceeded, repoKey, used, quota),
	}
}

// checkQuota is the quota pre-check every content-landing use case runs once
// the incoming size is known (GE-05): incoming is the size about to land,
// replaced the size of the node row this write REPLACES, whatever the write
// shape — 0 only when the path is genuinely new. A same-content retransmit
// (declared checksum, X-Checksum-Deploy re-announcement, docker's
// digest-keyed re-finalize) carries replaced == incoming — same sha256 means
// identical bytes means identical size — so its delta is 0 and the arm below
// exempts it WITHOUT a usage read: re-announcing bytes the repository
// already holds must not 413 (review B1; repo-semantics section 3's
// retransmit contract). An overwrite with different content carries the OLD
// row's size and is charged the difference. quotaBytes 0 (the default)
// short-circuits before any store read.
//
// The check is a pre-check, not a reservation: between the read here and the
// metered write another concurrent upload may land (SQLite serializes the
// writes themselves, not this read). The final write can therefore exceed
// the ceiling by the size of one racing upload — enforcement stays exact for
// the sequential contract the AC specifies (W26's PUT sequence) and the
// counter itself never lies (the metered write is same-transaction).
func (s *service) checkQuota(ctx context.Context, p *Principal, g governance, repoKey, path string, incoming, replaced int64) error {
	if g.quotaBytes <= 0 {
		return nil // unlimited: the M1~M3 default, zero extra I/O
	}
	delta := incoming - replaced
	if delta <= 0 {
		return nil // shrinks or no-ops can never cross a ceiling
	}
	u, err := s.md.Usage().Get(ctx, repoKey)
	if err != nil {
		return fmt.Errorf("quota read %s: %w", repoKey, err)
	}
	if u.LogicalBytes+delta <= g.quotaBytes {
		return nil
	}
	used, quota := u.LogicalBytes, g.quotaBytes
	refusal := quotaRefusal(repoKey, path, used, quota, delta)
	// GE-05/W26: the refusal leaves an audit record (detail carries the five
	// facts) and a WARN-level structured log line.
	s.audit(ctx, AuditEvent{
		Actor: actor(p), Action: AuditActionQuotaExceeded, Repo: repoKey, Path: path,
		Detail: fmt.Sprintf(`{"actor":%q,"repo":%q,"path":%q,"used":%d,"quota":%d}`,
			actor(p), repoKey, path, used, quota),
	})
	slog.WarnContext(ctx, "repo: write refused by repository quota",
		"repo", repoKey, "path", path, "used", used, "quota", quota, "incoming", incoming,
		"actor", actor(p))
	return refusal
}
