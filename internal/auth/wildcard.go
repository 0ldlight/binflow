package auth

import (
	"context"
	"slices"
)

// The preset wildcard buckets of the permission plane (T-491, FR-156.2 /
// M16 B-2.16; ADR-0046 point 3 and Errata ④ E8 for the distribution
// channel). A permission target's repos[] may name one repository (the
// exact form every pre-M17 row uses) or one of the three PRESET BUCKETS
// that name a repository population:
//
//	ANY LOCAL        every repository whose class is local
//	ANY REMOTE       every repository whose class is remote
//	ANY DISTRIBUTION the bundle-domain pseudo-key (see below)
//
// The spellings are the reference product's internal constants
// (docs/reverse/release-bundle.md §0⑧ and §5: `ANY LOCAL`, `ANY REMOTE`,
// `ANY DISTRIBUTION`, the family PermissionTarget.ANY_DISTRIBUTION_REPO
// anchors; the UI renders them "Any Local" / "Any Remote" /
// "Any Distribution"). A bucket literal can never collide with a real
// repository key: BinFlow keys match [a-z][a-z0-9-]{1,62} and carry no
// spaces.
//
// Semantics (the T-491 in-ticket ruling, spec basis high unless noted):
//
//   - Class coverage. For a repository of class local, a target whose
//     repos[] lists ANY LOCAL covers it — the exact-key listing and the
//     bucket listing are one disjunction, and everything else about the
//     target (include/exclude patterns, per-principal verb columns,
//     union-across-targets combination) is UNCHANGED. A repository that
//     does not exist yet is covered the moment it is created: the class
//     is resolved per evaluation, so a wildcard grant needs no re-grant
//     (the "new repo auto-included" probe).
//   - ANY REMOTE mirrors this for remote repositories. Virtual
//     repositories are covered by NEITHER bucket: the reference's ANY
//     family is class-keyed (local/remote/distribution) and no preset
//     names the virtual class — a virtual repository stays exact-listed
//     only (fail-closed; logged as a spec-gap note in the ticket).
//   - ANY DISTRIBUTION is the pseudo-key channel ADR-0046 point 3 pins:
//     bundle-domain callers pass the bucket literal itself in Can's
//     repoKey position (path carries the bundle name so includes/
//     excludes apply by name). Evaluation of a bucket literal is
//     EXACT-match: a target grants the pseudo-key only by naming it —
//     the ANY family never IMPLICITLY covers the bundle domain (E8: the
//     reference excludes release-bundle repositories from the ANY-family
//     fallback; authorization must name the wildcard bucket).
//   - The fourth family member, ANY ("Anything"), is deliberately NOT
//     implemented: BinFlow's console preset face is the three buckets
//     (console-ui.md §3.8) and the reference chain's ANY row is
//     registered spec-pending (ticket log). The wire therefore accepts
//     only the three spellings above — a literal "ANY" is the ordinary
//     unknown-repository 400, so no stored row can silently activate
//     when/if the fourth bucket is ever ruled in.
//   - Zero verb expansion (AC2): the buckets widen ONLY the repos-list
//     match. r/w/d/m/a stay five independent columns — rowAllows is
//     untouched — so a read grant through ANY LOCAL grants read, never
//     write/delete/manage/annotate (the per-verb probe matrix).
//
// A service built without a repository-class source (auth.New with fakes,
// WithRepoClass(nil)) keeps exact-only semantics for every key: an
// unanswered class question denies the bucket, never assumes it.

// The three preset wildcard bucket spellings (repos[] values).
const (
	BucketAnyLocal        = "ANY LOCAL"
	BucketAnyRemote       = "ANY REMOTE"
	BucketAnyDistribution = "ANY DISTRIBUTION"
)

// WildcardBuckets is the closed bucket set, for wire validation and QA
// matrix enumeration.
func WildcardBuckets() []string {
	return []string{BucketAnyLocal, BucketAnyRemote, BucketAnyDistribution}
}

// IsWildcardBucket reports whether repoKey is one of the three preset
// bucket spellings. Case-sensitive: the stored form is the constant, and
// a lowercased spelling is not a second spelling but an unknown key
// (fail-closed, like every other exact compare in the plane).
func IsWildcardBucket(repoKey string) bool {
	switch repoKey {
	case BucketAnyLocal, BucketAnyRemote, BucketAnyDistribution:
		return true
	}
	return false
}

// RepoClass is the repository-class vocabulary the buckets key on (the
// repositories table's type column, adapted by repoClassAdapter).
type RepoClass string

// The two classes a preset bucket names. Virtual (and any future class)
// maps to no bucket by construction — BucketForClass's "" arm.
const (
	RepoClassLocal  RepoClass = "local"
	RepoClassRemote RepoClass = "remote"
)

// BucketForClass maps one repository class onto the bucket that covers
// it, "" when no bucket does (virtual, unknown classes). The single
// translation table of the feature: Can and the ?permissions view both
// route through it, so the decision and the view cannot disagree.
func BucketForClass(class RepoClass) string {
	switch class {
	case RepoClassLocal:
		return BucketAnyLocal
	case RepoClassRemote:
		return BucketAnyRemote
	default:
		return ""
	}
}

// repoClassSource is the consumer-side seam Can needs to classify one
// repository key: the auth package cannot import the repository domain
// (it sits below it), so the class answer is injected — NewFromStore
// wires the metadata-backed adapter (deps.go), tests may wire fakes or
// nothing. nil (the zero service) means wildcard semantics are inert:
// every Can evaluation degrades to exact-key matching, which is the
// pre-T-491 behavior, fail-closed.
type repoClassSource interface {
	RepoClass(ctx context.Context, repoKey string) (RepoClass, bool)
}

// WithRepoClass returns a copy of svc whose repository-class source is
// replaced. src == nil switches wildcard semantics off (exact-only).
func (s *Service) WithRepoClass(src repoClassSource) *Service {
	clone := *s
	clone.repoClass = src
	return &clone
}

// RepoClassSource is the exported alias of the seam (the PermissionSource
// precedent): callers building their own class answers — tests, future
// consumers like the search-scope walk — can name and implement it.
type RepoClassSource = repoClassSource

// bucketFor resolves the one bucket literal that covers repoKey in this
// evaluation, "" when none does: no class source wired, the key is not a
// repository, its class carries no bucket (virtual), or the key IS a
// bucket literal — the pseudo-key channel evaluates exactly, never
// recursively (the ANY family does not implicitly cover the bundle
// domain, E8). At most one bucket can apply: a repository has exactly
// one class.
func (s *Service) bucketFor(ctx context.Context, repoKey string) string {
	if s.repoClass == nil || repoKey == "" || IsWildcardBucket(repoKey) {
		return ""
	}
	class, ok := s.repoClass.RepoClass(ctx, repoKey)
	if !ok {
		return ""
	}
	return BucketForClass(class)
}

// repoListed reports whether one target's decoded repos list names the
// repository key directly or through the applicable wildcard bucket
// ("" disables the second arm). The shared repos-membership predicate of
// targetCovers (the path plane) and targetListsRepo (the m plane), so
// all five verbs see one and the same coverage rule.
func repoListed(repos []string, repoKey, bucket string) bool {
	if slices.Contains(repos, repoKey) {
		return true
	}
	return bucket != "" && slices.Contains(repos, bucket)
}
