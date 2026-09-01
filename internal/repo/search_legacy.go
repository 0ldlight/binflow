package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The FR-134 legacy search use cases (T-417, aql.md section 8): gavc, prop
// and pattern — the old-search family's first three doors beyond the T-92
// pair. Like SR-01/SR-02 the SQL mechanics live in metadata.NodeSearcher and
// this layer owns what the store cannot judge: query validation, the
// anonymous-channel gate and the ACL visibility filter (the very
// searchGate/filterVisible pair above, so the whole legacy family answers
// one permission posture — the same allow() a download runs).
//
// The K63 row ceiling arrives as a limit PARAMETER, not a constant here:
// search.ResultCap lives in internal/search (which imports this package), so
// the endpoint — the single place allowed to source it — passes cap+1 down
// and reads the truncation off the extra row.

// LegacySearchService is the old-search capability face of the concrete
// Service implementation (FR-134.1-.3). It is deliberately NOT part of the
// big Service interface, exactly like CopyMoveService: consumers reach it by
// assertion (httpapi), so the interface addition cannot break the
// hand-written adapter test fakes that implement Service method by method.
type LegacySearchService interface {
	// SearchGavc returns every file node matching the given maven coordinate
	// subset (a blank field is unconstrained; at least one field required).
	// limit must be positive; repos narrows like the T-92 pair's filter.
	SearchGavc(ctx context.Context, p *Principal, q GavcQuery, limit int, repos []string) ([]*metadata.Node, error)
	// SearchProps returns every file node carrying ALL the given property
	// constraints (a blank Value is the key's bare existence). conds must be
	// non-empty and within MaxPropSearchConds; limit must be positive.
	SearchProps(ctx context.Context, p *Principal, conds []PropCond, limit int, repos []string) ([]*metadata.Node, error)
	// SearchPattern returns every file node whose repository key and path
	// match the two PRE-TRANSLATED SQL LIKE patterns (internal/search's
	// single wildcard kernel produced them — this package cannot import it,
	// the search package imports this one; the endpoint therefore owns the
	// translation). Either pattern may be empty (unconstrained); limit must
	// be positive.
	SearchPattern(ctx context.Context, p *Principal, repoLike, pathLike string, limit int) ([]*metadata.Node, error)
}

// GavcQuery is the FR-134.1 coordinate set: groupId, artifactId, version and
// classifier, every field optional (the official "at least one" rule is the
// use case's to enforce). The semantics are a compatible SUBSET of the M3
// maven-2-default layout model (adapter/maven's parser — which this package
// cannot import, repo never imports the adapters): each present field
// translates onto literal path pieces of the [org]/[module]/[version]/
// [module]-[version](-[classifier]).[ext] template.
type GavcQuery struct {
	Group      string
	Artifact   string
	Version    string
	Classifier string
}

// PropCond is one FR-134.2 property constraint. The key grammar is the M10
// write face's own (metadata.ValidatePropKey/Value): a search key that could
// never be written is refused, never silently matched-nothing.
type PropCond struct {
	Key   string
	Value string // "" = any value of the key
}

// MaxPropSearchConds bounds how many property constraints one prop search
// may carry: every constraint is one EXISTS probe, and the any-parameter
// form (aql.md section 8.2 — every unknown query parameter names a property
// key) would otherwise let one URL stack unbounded probes.
const MaxPropSearchConds = 16

// maxLegacyValueLen bounds one gavc coordinate: no legitimate groupId or
// version is anywhere near this long, and the pieces ride LIKE arguments
// whose total statement budget stays bounded.
const maxLegacyValueLen = 512

// SearchGavc implements LegacySearchService. The matching subset, per field
// (all pieces literal, case-sensitive — maven coordinates are):
//
//   - g: the path's leading directories (dots become slashes: com.acme ->
//     com/acme/…, a directory boundary the trailing slash pins);
//   - a: the module directory — directly after g when g is present, any
//     depth otherwise (/demo-app/) — plus the template's file-name anchor
//     (/demo-app-): an artifact of the module opens with "<module>-", which
//     keeps module-level maven-metadata.xml documents out of the results;
//   - v: the version directory — directly after a when the chain is
//     unbroken, any depth otherwise (/1.0.0/);
//   - c: the classifier token inside the file name (-sources.).
//
// Repositories other than maven ones are NOT excluded: the gavc arm matches
// path SHAPE across every repository (the repos filter narrows), the
// registered simplification — Artifactory consults each repository's layout
// descriptor; BinFlow has exactly one maven layout, spelled by this subset.
func (s *service) SearchGavc(ctx context.Context, p *Principal, q GavcQuery, limit int, repos []string) ([]*metadata.Node, error) {
	f, err := gavcPathFilter(q)
	if err != nil {
		return nil, err
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
	nodes, err := ns.SearchByPath(ctx, f, limit, repos)
	if err != nil {
		return nil, fmt.Errorf("search gavc %+v: %w", q, err)
	}
	return s.filterVisible(ctx, p, nodes), nil
}

// SearchProps implements LegacySearchService.
func (s *service) SearchProps(ctx context.Context, p *Principal, conds []PropCond, limit int, repos []string) ([]*metadata.Node, error) {
	if err := validatePropConds(conds); err != nil {
		return nil, err
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
	filters := make([]metadata.PropFilter, 0, len(conds))
	for _, c := range conds {
		filters = append(filters, metadata.PropFilter{Key: c.Key, Value: c.Value})
	}
	nodes, err := ns.SearchByProps(ctx, filters, limit, repos)
	if err != nil {
		return nil, fmt.Errorf("search props %v: %w", conds, err)
	}
	return s.filterVisible(ctx, p, nodes), nil
}

// SearchPattern implements LegacySearchService.
func (s *service) SearchPattern(ctx context.Context, p *Principal, repoLike, pathLike string, limit int) ([]*metadata.Node, error) {
	if strings.TrimSpace(repoLike) == "" && strings.TrimSpace(pathLike) == "" {
		return nil, fmt.Errorf("%w: pattern search requires at least one of repo or path pattern", ErrInvalidSearchQuery)
	}
	if err := validateLegacyLimit(limit); err != nil {
		return nil, err
	}
	if err := s.searchGate(ctx, p); err != nil {
		return nil, err
	}
	ns, err := s.searcher()
	if err != nil {
		return nil, err
	}
	nodes, err := ns.SearchByPattern(ctx, repoLike, pathLike, limit)
	if err != nil {
		return nil, fmt.Errorf("search pattern %q:%q: %w", repoLike, pathLike, err)
	}
	return s.filterVisible(ctx, p, nodes), nil
}

// gavcPathFilter validates the coordinate set and composes the literal path
// pieces. The chain rule: g, a and v are CONTIGUOUS template tokens, so a
// field chains onto the leading prefix only while every earlier field is
// present; a field that cannot chain (a without g, v without a) degrades to
// an infix constraint at any depth. The classifier is always an infix of
// the file name.
func gavcPathFilter(q GavcQuery) (metadata.PathFilter, error) {
	q.Group = strings.TrimSpace(q.Group)
	q.Artifact = strings.TrimSpace(q.Artifact)
	q.Version = strings.TrimSpace(q.Version)
	q.Classifier = strings.TrimSpace(q.Classifier)
	if q.Group == "" && q.Artifact == "" && q.Version == "" && q.Classifier == "" {
		return metadata.PathFilter{}, fmt.Errorf(
			"%w: gavc search requires at least one of g, a, v or c", ErrInvalidSearchQuery)
	}
	if q.Group != "" {
		if err := validateGavcGroup(q.Group); err != nil {
			return metadata.PathFilter{}, err
		}
	}
	for _, field := range []struct{ name, value string }{
		{"a", q.Artifact}, {"v", q.Version}, {"c", q.Classifier},
	} {
		if field.value == "" {
			continue
		}
		if err := validateGavcValue(field.name, field.value); err != nil {
			return metadata.PathFilter{}, err
		}
	}

	var f metadata.PathFilter
	switch {
	case q.Group != "":
		parts := []string{strings.ReplaceAll(q.Group, ".", "/")}
		if q.Artifact != "" {
			parts = append(parts, q.Artifact)
			if q.Version != "" {
				parts = append(parts, q.Version)
			}
		} else if q.Version != "" {
			// v cannot chain without a: any-depth directory infix.
			f.Infixes = append(f.Infixes, "/"+q.Version+"/")
		}
		// The trailing slash is the directory boundary: without it the
		// prefix "com/acme/demo-app" would also swallow a sibling named
		// com/acme/demo-app-extra/.
		f.Prefix = strings.Join(parts, "/") + "/"
	case q.Artifact != "":
		if q.Version != "" {
			f.Infixes = append(f.Infixes, "/"+q.Artifact+"/"+q.Version+"/")
		} else {
			f.Infixes = append(f.Infixes, "/"+q.Artifact+"/")
		}
	case q.Version != "":
		f.Infixes = append(f.Infixes, "/"+q.Version+"/")
	}
	if q.Artifact != "" {
		// The template's load-bearing file-name rule (adapter/maven's own
		// words): an artifact file opens with "<module>-". The anchor keeps
		// module-level maven-metadata.xml documents (and their checksum
		// sidecars) out of a coordinate search — they live in the module
		// directory but are not artifacts of the GAV.
		f.Infixes = append(f.Infixes, "/"+q.Artifact+"-")
	}
	if q.Classifier != "" {
		f.Infixes = append(f.Infixes, "-"+q.Classifier+".")
	}
	return f, nil
}

// validateGavcGroup checks the groupId: no slashes, no control bytes,
// bounded length, and every dot-separated segment non-empty and neither "."
// nor ".." — the shared content-path segment rules the maven layout itself
// assumes (adapter/maven defers to them; this is the compatible subset).
func validateGavcGroup(g string) error {
	if err := validateGavcValue("g", g); err != nil {
		return err
	}
	for _, seg := range strings.Split(g, ".") {
		if seg == "" {
			return fmt.Errorf("%w: group %q has an empty segment", ErrInvalidSearchQuery, g)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: group %q has a dot-only segment", ErrInvalidSearchQuery, g)
		}
	}
	return nil
}

// validateGavcValue checks one coordinate value: no path separator, no NUL
// or control bytes, bounded length.
func validateGavcValue(field, v string) error {
	if strings.ContainsAny(v, "/\x00") {
		return fmt.Errorf("%w: gavc %s %q must not contain a path separator", ErrInvalidSearchQuery, field, v)
	}
	if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("%w: gavc %s %q contains control characters", ErrInvalidSearchQuery, field, v)
	}
	if len(v) > maxLegacyValueLen {
		return fmt.Errorf("%w: gavc %s is longer than %d characters", ErrInvalidSearchQuery, field, maxLegacyValueLen)
	}
	return nil
}

// validatePropConds checks the constraint list: non-empty, within
// MaxPropSearchConds, and every key/value through the M10 write grammar so
// the search face and the write face accept one spelling of a property.
func validatePropConds(conds []PropCond) error {
	if len(conds) == 0 {
		return fmt.Errorf("%w: property search requires at least one property constraint", ErrInvalidSearchQuery)
	}
	if len(conds) > MaxPropSearchConds {
		return fmt.Errorf("%w: property search carries %d constraints, at most %d are accepted",
			ErrInvalidSearchQuery, len(conds), MaxPropSearchConds)
	}
	for _, c := range conds {
		if err := metadata.ValidatePropKey(c.Key); err != nil {
			return fmt.Errorf("property search key: %w: %w", ErrInvalidSearchQuery, err)
		}
		if c.Value != "" {
			if err := metadata.ValidatePropValue(c.Key, c.Value); err != nil {
				return fmt.Errorf("property search value: %w: %w", ErrInvalidSearchQuery, err)
			}
		}
	}
	return nil
}

// validateLegacyLimit keeps the K63 ceiling meaningful: the endpoint passes
// cap+1 and no other caller exists, but a zero or negative limit would
// silently unbound the row materialization.
func validateLegacyLimit(limit int) error {
	if limit <= 0 {
		return fmt.Errorf("%w: legacy search requires a positive result limit", ErrInvalidSearchQuery)
	}
	return nil
}
