package replication

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The source-side metadata seam of the protocol-aware push planes (T-195).
//
// The engine's task rows carry (sha256, node_path) — enough for the generic
// REST plane, but the docker/npm/pypi planes also need facts only the SOURCE
// instance's metadata holds: which package type the source repository serves
// (plane selection), a node's stored mime (the docker manifest's wire
// Content-Type), a path's current blob digest (the npm packument lookup —
// the tarball task does not know the packument's sha), and the docker tag
// pointers of one manifest digest (tags live in the docker index, not in
// node rows, and a push that omits them leaves the target un-pull-able by
// tag). The seam is consumer-side (architecture section 3) and read-only;
// StoreMetaSource below implements it over metadata.Store, so the engine
// never grows a principal or a service dependency of its own.

// ErrMetaNotFound marks a missing source-side fact: a repository or node the
// task row still references but the metadata no longer serves. Planes map it
// onto ErrNotRetryable — a vanished fact cannot be retried into existence.
var ErrMetaNotFound = errors.New("replication: source metadata fact not found")

// NodeMeta is the per-node slice of the metadata the planes consume.
type NodeMeta struct {
	// Mime is the node row's stored mime. For docker manifest nodes this is
	// the manifest's wire Content-Type (FR-7-AC4); empty means "unknown" and
	// the caller decides whether it can proceed without it.
	Mime string
	// Sha256 is the node row's current blob digest — the engine's way from a
	// node PATH to the bytes when the task only names a related path (the npm
	// plane resolving a package's packument from a tarball task).
	Sha256 string
}

// MetaSource is the engine's read-only view of the source instance's
// metadata. Implementations must be safe for concurrent use.
type MetaSource interface {
	// PackageType returns the source repository's package_type
	// ("generic", "docker", "npm", "pypi", ...). ErrMetaNotFound when the
	// repository row is gone.
	PackageType(ctx context.Context, repoKey string) (string, error)
	// Node returns the stored facts of one node path. ErrMetaNotFound when
	// no node row holds the path.
	Node(ctx context.Context, repoKey, path string) (*NodeMeta, error)
	// DockerTags returns the tag names currently pointing at the manifest
	// digest hex within one image (empty when none do — a digest-only push).
	DockerTags(ctx context.Context, repoKey, image, digestHex string) ([]string, error)
	// NodeProps returns every property of one node (the M10 property
	// system; T-317, FR-101.2). A node without properties answers an empty
	// map — the push plane reads this at PUSH time, so properties attached
	// between landing and the drain ride the same task.
	NodeProps(ctx context.Context, repoKey, path string) (map[string][]string, error)
}

// StoreMetaSource implements MetaSource over an opened metadata store. It is
// the production wiring (cmd hands the same store the rest of the process
// uses); tests inject fakes over the same interface.
type StoreMetaSource struct {
	md metadata.Store
}

// NewStoreMetaSource wires the metadata-backed implementation.
func NewStoreMetaSource(md metadata.Store) *StoreMetaSource {
	return &StoreMetaSource{md: md}
}

// Compile-time pin.
var _ MetaSource = (*StoreMetaSource)(nil)

// PackageType implements MetaSource.
func (s *StoreMetaSource) PackageType(ctx context.Context, repoKey string) (string, error) {
	row, err := s.md.Repos().Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return "", notRetryable(fmt.Errorf("source repo %s: %w: %w", repoKey, ErrMetaNotFound, err))
		}
		return "", fmt.Errorf("source repo %s lookup: %w", repoKey, err)
	}
	return row.PackageType, nil
}

// Node implements MetaSource.
func (s *StoreMetaSource) Node(ctx context.Context, repoKey, path string) (*NodeMeta, error) {
	n, err := s.md.Nodes().Get(ctx, repoKey, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, notRetryable(fmt.Errorf("node %s/%s: %w", repoKey, path, ErrMetaNotFound))
		}
		return nil, fmt.Errorf("node %s/%s lookup: %w", repoKey, path, err)
	}
	return &NodeMeta{Mime: n.Mime, Sha256: n.Sha256}, nil
}

// DockerTags implements MetaSource.
func (s *StoreMetaSource) DockerTags(ctx context.Context, repoKey, image, digestHex string) ([]string, error) {
	tags, err := s.md.Docker().ListTagsByImage(ctx, repoKey, image)
	if err != nil {
		return nil, fmt.Errorf("docker tags %s/%s: %w", repoKey, image, err)
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t != nil && t.Digest == digestHex {
			out = append(out, t.Tag)
		}
	}
	return out, nil
}

// NodeProps implements MetaSource (T-317, FR-101.2): the node's property
// map off the M10 node_props store. The store answers an empty map for a
// node without properties; a store fault surfaces as a transient error so
// the task's retry schedule owns it.
func (s *StoreMetaSource) NodeProps(ctx context.Context, repoKey, path string) (map[string][]string, error) {
	props, err := s.md.NodeProps().List(ctx, repoKey, path)
	if err != nil {
		return nil, fmt.Errorf("node properties %s/%s: %w", repoKey, path, err)
	}
	return props, nil
}
