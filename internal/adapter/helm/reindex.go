package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The reindex face (helm.md section 5.2 / S6) — the management plane's
// two endpoints call in here (httpapi/helm.go owns the routes, the gates
// and the async/sync split):
//
//   - ReindexRepository: the whole-repo rebuild — clear, re-scan every
//     stored .tgz, rewrite. The route wraps this in a goroutine (the
//     async posture JFrog documents).
//   - ReindexPath: the partial, synchronous rebuild — a .tgz path re-adds
//     that one package; a directory path first drops every entry whose
//     urls carry the prefix, then re-scans the subtree (removal matches
//     on the entry urls prefix — relative mode's <path> form, exactly the
//     rule the index's urls spelling guarantees).
//
// Both run under the repository's index lock, so a concurrent upload's
// read-modify-write can never interleave with a rebuild.

// ReindexRepository rebuilds the whole repo-root index.yaml from storage.
func (h *Handler) ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error {
	return h.withIndexLock(repoKey, func() error {
		added, err := h.reindexScanLocked(ctx, p, repoKey, "")
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, "helm: repository reindex complete",
			slog.String("repo", repoKey), slog.Int("charts", added))
		return nil
	})
}

// ReindexPath rebuilds one path's slice of the index synchronously.
func (h *Handler) ReindexPath(ctx context.Context, p *repo.Principal, repoKey, path string) error {
	target := strings.TrimSuffix(path, "/")
	if target == "" {
		target = "/"
	}
	return h.withIndexLock(repoKey, func() error {
		doc, err := h.readStoredIndex(ctx, p, repoKey)
		if err != nil {
			return err
		}
		if isChartPath(target) {
			// The single-package arm: a missing node is the honest 404 —
			// there is nothing at the path to re-add.
			if _, _, err := h.svc.Get(ctx, p, repoKey, target); err != nil {
				if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
					return fmt.Errorf("%w: %s", repo.ErrNodeNotFound, target)
				}
				return err
			}
		} else {
			doc.removePrefix(target)
		}
		added, err := h.reindexScanInto(ctx, p, repoKey, target, doc)
		if err != nil {
			return err
		}
		if err := h.writeStoredIndex(ctx, p, repoKey, doc.render(h.now())); err != nil {
			return err
		}
		slog.InfoContext(ctx, "helm: path reindex complete",
			slog.String("repo", repoKey), slog.String("path", target), slog.Int("charts", added))
		return nil
	})
}

// reindexScanLocked is the full-repo arm: a FRESH document (the clear of
// section 5.2), then the whole-tree scan.
func (h *Handler) reindexScanLocked(ctx context.Context, p *repo.Principal, repoKey, prefix string) (int, error) {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	added, err := h.reindexScanInto(ctx, p, repoKey, prefix, doc)
	if err != nil {
		return 0, err
	}
	if err := h.writeStoredIndex(ctx, p, repoKey, doc.render(h.now())); err != nil {
		return 0, err
	}
	return added, nil
}

// reindexScanInto walks the stored .tgz nodes under prefix, re-parsing
// each archive for its Chart.yaml facts, and upserts the entries into
// doc. Unparsable packages skip with a WARN (the same tolerance as the
// upload chain); the created timestamp is one shared moment per run so
// the rewrite stays deterministic.
func (h *Handler) reindexScanInto(ctx context.Context, p *repo.Principal, repoKey, prefix string, doc *indexDoc) (int, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return 0, fmt.Errorf("list %s under %q: %w", repoKey, prefix, err)
	}
	now := h.now()
	absoluteBase := ""
	if h.opts.AbsoluteURLs {
		absoluteBase = h.opts.BaseURL + "/binflow/" + repoKey
	}
	added := 0
	for _, n := range nodes {
		if !isChartPath(n.Path) {
			continue
		}
		arc, err := h.readChartArchive(ctx, p, repoKey, n.Path)
		if err != nil {
			slog.WarnContext(ctx, "helm: reindex skipped an unparsable chart",
				slog.String("repo", repoKey), slog.String("path", n.Path), slog.String("error", err.Error()))
			continue
		}
		name, _, ok := arc.identity()
		if !ok {
			continue
		}
		entry, ok := buildEntry(arc, n.Path, n.Sha256, now, absoluteBase)
		if !ok {
			slog.WarnContext(ctx, "helm: reindex entry failed the roundtrip check; skipped",
				slog.String("repo", repoKey), slog.String("path", n.Path))
			continue
		}
		doc.upsertEntry(name, entry)
		added++
	}
	return added, nil
}

// now resolves the handler clock (Options.Now, defaulting to time.Now —
// tests inject deterministic timestamps).
func (h *Handler) now() time.Time {
	if h.opts.Now != nil {
		return h.opts.Now()
	}
	return time.Now()
}
