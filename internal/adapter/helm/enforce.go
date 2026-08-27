package helm

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The Enforce Layout policy (helm.md section 4.3 / S5 — the JFrog wording
// is the authority, carried verbatim where the spec quotes it): two
// repository-level switches in the local repository's config blob, both
// default OFF, both judged on the .tgz upload hook BEFORE any byte lands.
// A non-.tgz path never meets the question.

// enforcePolicy is the parsed switch pair.
type enforcePolicy struct {
	forceMetadataNameVersion bool
	forceNonDuplicateChart   bool
}

// parseEnforcePolicy reads the two switches off the repository row's
// config JSON (the Artifactory field spellings; unknown keys and a
// malformed blob both mean "off" — the config plane's own validation owns
// the hard failures).
func parseEnforcePolicy(config string) enforcePolicy {
	var probe struct {
		ForceMetadataNameVersion bool `json:"forceMetadataNameVersion"`
		ForceNonDuplicateChart   bool `json:"forceNonDuplicateChart"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return enforcePolicy{}
	}
	return enforcePolicy{
		forceMetadataNameVersion: probe.ForceMetadataNameVersion,
		forceNonDuplicateChart:   probe.ForceNonDuplicateChart,
	}
}

// The 403 wordings (section 4.3's verbatim carries; the spec's ellipses
// interpolate the package identity the JFrog messages name).
const (
	msgEnforceMalformed = "This action is prevented due to the Enforce Layout Policy, the metadata of the package %s/%s could not be read or is malformed."
	msgEnforceDuplicate = "This action is prevented due to the Enforce Layout Policy, a package with the same name and version %s-%s already exists in the repository."
)

// enforceLayoutError is the two 403 refusals' typed shape: Error() is the
// VERBATIM policy wording (the wire body carries it letter for letter —
// the spec's 逐字 requirement), and the type itself is the errors.As
// match the upload chain keys its status mapping on.
type enforceLayoutError struct{ message string }

// Error implements error: the policy wording, nothing else.
func (e enforceLayoutError) Error() string { return e.message }

// enforceError renders one policy refusal.
func enforceError(format string, args ...any) error {
	return enforceLayoutError{message: fmt.Sprintf(format, args...)}
}

// policyFor loads the repository row and reads its switches.
func (h *Handler) policyFor(ctx context.Context, repoKey string) (enforcePolicy, error) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		return enforcePolicy{}, fmt.Errorf("load repository %s: %w", repoKey, err)
	}
	return parseEnforcePolicy(row.Config), nil
}

// enforceUpload judges one .tgz upload against the switches. arc is nil
// when the archive could not be parsed at all — the malformed-metadata
// arm. A nil props seam degrades the duplicate search to the filename arm
// (the honest bound of a bare stack).
func (h *Handler) enforceUpload(ctx context.Context, p *repo.Principal, repoKey, relPath string, arc *chartArchive) error {
	policy, err := h.policyFor(ctx, repoKey)
	if err != nil {
		return err
	}
	if !policy.forceMetadataNameVersion && !policy.forceNonDuplicateChart {
		return nil
	}
	name, version, ok := arc.identity()

	// Switch one: the filename must be <name>-<version>.tgz with a SemVer2
	// version and a readable name/version pair.
	if policy.forceMetadataNameVersion {
		if !ok {
			return enforceError(msgEnforceMalformed, repoKey, path.Base(relPath))
		}
		if _, err := parseSemver(version); err != nil {
			return enforceError(msgEnforceMalformed, repoKey, path.Base(relPath))
		}
		if want := name + "-" + version + ".tgz"; path.Base(relPath) != want {
			return enforceError(msgEnforceMalformed, repoKey, path.Base(relPath))
		}
	}

	// Switch two: no second path for the same name+version. With switch one
	// also on, the filename IS the identity (section 4.3's cheaper search).
	if policy.forceNonDuplicateChart {
		dup, err := h.findDuplicate(ctx, p, repoKey, relPath, name, version, policy.forceMetadataNameVersion)
		if err != nil {
			return err
		}
		if dup != "" {
			return enforceError(msgEnforceDuplicate, name, version)
		}
	}
	return nil
}

// findDuplicate scans the stored nodes for the same chart identity at a
// different path. byFilename selects the cheap arm; otherwise the chart.*
// property search (the spec's default).
func (h *Handler) findDuplicate(ctx context.Context, p *repo.Principal, repoKey, relPath, name, version string, byFilename bool) (string, error) {
	if name == "" || version == "" {
		return "", nil
	}
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		return "", fmt.Errorf("list %s for the duplicate check: %w", repoKey, err)
	}
	wantFile := name + "-" + version + ".tgz"
	for _, n := range nodes {
		if n.Path == relPath || !isChartPath(n.Path) {
			continue
		}
		if byFilename {
			if chartBaseName(n.Path) == wantFile {
				return n.Path, nil
			}
			continue
		}
		if h.props == nil {
			continue
		}
		props, err := h.props.List(ctx, repoKey, n.Path)
		if err != nil {
			return "", fmt.Errorf("read properties %s/%s: %w", repoKey, n.Path, err)
		}
		if hasPropValue(props, propName, name) && hasPropValue(props, propVersion, version) {
			return n.Path, nil
		}
	}
	return "", nil
}

// hasPropValue reports one exact (key, value) property hit.
func hasPropValue(props map[string][]string, key, value string) bool {
	for _, v := range props[key] {
		if v == value {
			return true
		}
	}
	return false
}

// nodeChartIdentity reads one node's chart.name/chart.version pair (the
// delete event's removal key; missing properties answer not-ok).
func nodeChartIdentity(ctx context.Context, props NodeProps, repoKey, nodePath string) (string, string, bool) {
	if props == nil {
		return "", "", false
	}
	m, err := props.List(ctx, repoKey, nodePath)
	if err != nil {
		return "", "", false
	}
	names := m[propName]
	versions := m[propVersion]
	if len(names) == 0 || len(versions) == 0 {
		return "", "", false
	}
	return names[0], versions[0], true
}
