package maven

import (
	"context"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Protocol is the package-type identifier this adapter serves — it equals
// the repositories.package_type value httpapi dispatches on.
const Protocol = "maven"

// BlobLedger is the read-only digest ledger the adapter consults for
// sha1/md5 (the ancillary digests of a node's blob; ADR-0006 keeps no
// sidecar files). Same consumer-side convention as the generic and docker
// adapters; satisfied by metadata.Store.Blobs().
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// NodeReader is the read-only facts seam the maven-metadata calculator
// needs (T-68): computing maven-metadata.xml is a server-side derivation
// over STORAGE FACTS, not a client read — the deploy that triggered the
// recalculation was already authorized, so the node listing must not be
// re-gated on the trigger principal's read grant (a write-without-read
// deployer would otherwise lose metadata maintenance on every deploy,
// and svc.List's read gate cannot express that distinction). A narrow
// read-only seam in the ClassReader tradition (T-63); satisfied
// structurally by metadata.NodeStore.
type NodeReader interface {
	// ListByPrefix returns the nodes under repoKey/prefix ordered by path
	// (folder rows included; the calculator filters them).
	ListByPrefix(ctx context.Context, repoKey, prefix string) ([]*metadata.Node, error)
}

// Handler is the Maven 2 content-path adapter (architecture section 5.4.1:
// maven mounts the shared content namespace with zero httpapi changes —
// dispatch keys on the repository row's package type).
type Handler struct {
	// svc is the repo.Service use-case face: content Get/Put/Delete for
	// the transfer plane, GetRepo for the repository configuration the
	// maven policy chain reads (writes are always authenticated, so the
	// management-plane read is legal on every PUT path).
	svc repo.Service
	// class resolves a repository's CLASS without a principal — the
	// adapter SPI's ClassReader seam (T-63): the remote sidecar 404
	// (RE/ME-03) must trigger on anonymous content reads too, before any
	// authenticated management-plane lookup could run. Only row.Type
	// crosses that seam by contract.
	class repo.ClassReader
	// ledger serves sha1/md5 for download headers and computed sidecar
	// GETs; nil degrades to sha256-only.
	ledger BlobLedger
	// nodes lists storage facts for the maven-metadata calculator (T-68);
	// nil disables server-side recalculation (the transfer plane keeps
	// working, metadata documents stay whatever clients stored).
	nodes NodeReader
	// calc is the maven-metadata.xml calculator (FR-17); nil together with
	// nodes.
	calc *calculator
}

// New wires the handler. svc is required; class is required (the remote
// sidecar pass-through has no fallback); ledger may be nil (sha256-only
// degradation); nodes may be nil (calculator disabled, T-67 behavior).
func New(svc repo.Service, class repo.ClassReader, ledger BlobLedger, nodes NodeReader) *Handler {
	h := &Handler{svc: svc, class: class, ledger: ledger, nodes: nodes}
	if nodes != nil {
		h.calc = newCalculator(svc, nodes, nil)
	}
	return h
}

// Compile-time SPI conformance pins.
var (
	_ adapter.Handler = (*Handler)(nil)
	// metadata's Repos sub-store satisfies the class seam without an
	// adapter type (the same convention repo's own declaration pins).
	_ repo.ClassReader = metadata.RepoStore(nil)
	// metadata's Nodes sub-store satisfies the calculator's facts seam the
	// same way.
	_ NodeReader = metadata.NodeStore(nil)
)
