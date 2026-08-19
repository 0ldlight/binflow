package pypi

// The virtual-repository simple-index collection (T-72, FR-21-AC5;
// maven-npm-pypi.md section 3.6, high confidence): a project page read
// through a VIRTUAL repository is the union of every member's entries —
// LOCAL members contribute their node facts (the same reconstruction the
// local page runs), REMOTE members their upstream index pages pulled
// through the FR-20 chain and PARSED (HTML or PEP 691 JSON, whichever the
// upstream serves). The merged page is computed PER REQUEST; nothing is
// cached beyond what the members' own FR-20 caches already hold (a remote
// member's miss keeps its negative-cache row — the PRD's "复用 FR-20 负缓存
// 参数" line; that is the designed quieting of upstream flapping, not
// pollution).
//
// Format fallback (the spec's rule): a JSON-requesting client gets JSON
// only when EVERY contributing member can serve it — local facts can render
// either way, a remote page is whatever the upstream sent, so one
// HTML-only member flips the WHOLE response to HTML (pip lists both media
// types in Accept, so the fallback is transparent to it).
//
// Failure policy (the PRD's "任一成员失败不阻塞其它"): an unfound member
// contributes nothing; a classified member failure (the remote engine's
// SSRF 400, hardFail 502) is skipped while other members still answer —
// but a collection where NOTHING was collected surfaces the remembered
// failure instead of masking it as a plain 404.
//
// A BARE REMOTE repository answers the same collection over itself (the
// single-member degenerate case): the upstream page is fetched through the
// ordinary gated GET of the addressed repository and its entries remapped
// onto this repository's packages/ mount — the M46/M47 face T-70 left at
// its transitional refusal.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// simplePageReadLimit bounds one upstream index page (real project pages
// run to a few hundred KB; beyond it the member is treated as unreadable
// and skipped).
const simplePageReadLimit = 16 << 20

// anchorRegExp matches one simple-index HTML anchor with its href; the
// greedy-free body capture takes the anchor text (the file name).
var anchorRegExp = regexp.MustCompile(`(?is)<a\s[^>]*href=(?:"([^"]*)"|'([^']*)')[^>]*>(.*?)</a>`)

// collectedProject is one merged project page in the making.
type collectedProject struct {
	entries []indexEntry
	seen    map[string]bool // (filename, sha256) — the same distribution in two members is one entry
	// htmlOnly records that at least one contributing member page cannot
	// render JSON (an upstream HTML page); it forces the whole-response
	// fallback.
	htmlOnly bool
}

// note folds one member's entries in, first-wins on duplicates: the
// earlier member's path is the one resolution would serve, so pointing the
// href there keeps the index consistent with the download plane.
func (c *collectedProject) note(entries []indexEntry, memberHTML bool) {
	for _, e := range entries {
		key := e.filename + "\x00" + e.sha256
		if c.seen[key] {
			continue
		}
		c.seen[key] = true
		c.entries = append(c.entries, e)
	}
	if memberHTML {
		c.htmlOnly = true
	}
}

// serveVirtualProjectPage renders the merged project page of a virtual
// repository.
func (h *Handler) serveVirtualProjectPage(w http.ResponseWriter, r *http.Request, repoKey, name string) {
	entries, htmlOnly, err := h.collectVirtualProject(r.Context(), repoKey, name)
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, segSimple+"/"+name+"/")
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("project '%s' not found in repository '%s'", normalizePackageName(name), repoKey))
		return
	}
	sortIndexEntries(entries)
	h.writeProjectPage(w, r, name, entries, htmlOnly)
}

// collectVirtualProject walks the two-bucket order collecting every
// member's entries for one project.
func (h *Handler) collectVirtualProject(ctx context.Context, virtualKey, name string) ([]indexEntry, bool, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, false, err
	}
	want := normalizePackageName(name)
	collected := &collectedProject{seen: map[string]bool{}}
	var failure *repo.StatusError
	for _, m := range order {
		var (
			entries []indexEntry
			html    bool
		)
		switch m.Type {
		case repo.TypeLocal:
			nodes, lerr := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, "")
			if lerr != nil {
				return nil, false, lerr
			}
			entries = entriesFromNodes(nodes, want)
		case repo.TypeRemote:
			page, perr := h.readMemberSimplePage(ctx, virtualKey, m.Key, want)
			if perr != nil {
				if errors.Is(perr, repo.ErrNodeNotFound) {
					continue // the member (its upstream) has no such project
				}
				var se *repo.StatusError
				if errors.As(perr, &se) {
					if failure == nil {
						failure = se
					}
					slog.WarnContext(ctx, "pypi: virtual member index failed — skipped",
						"virtual", virtualKey, "member", m.Key, "error", se.Message)
					continue // one member's fault must not block the rest
				}
				return nil, false, perr
			}
			entries, html = page.entries, page.htmlOnly
		}
		collected.note(entries, html)
	}
	if len(collected.entries) == 0 && failure != nil {
		return nil, false, failure
	}
	return collected.entries, collected.htmlOnly, nil
}

// memberSimplePage is one remote member's parsed upstream page.
type memberSimplePage struct {
	entries  []indexEntry
	htmlOnly bool
}

// readMemberSimplePage pulls one remote member's project page through the
// ungated aggregation seam and parses it.
func (h *Handler) readMemberSimplePage(ctx context.Context, virtualKey, member, norm string) (*memberSimplePage, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, simplePagePath(norm))
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, simplePageReadLimit))
	if rerr != nil {
		return nil, fmt.Errorf("read simple page %s/%s: %w", member, norm, rerr)
	}
	entries, isHTML := parseSimplePage(raw)
	return &memberSimplePage{entries: entries, htmlOnly: isHTML}, nil
}

// simplePagePath is the PEP 503 project-page path of a normalized name
// (the canonical trailing-slash form — upstreams answer it without a
// redirect).
func simplePagePath(norm string) string { return segSimple + "/" + norm + "/" }

// serveRemoteProjectPage renders a BARE remote repository's project page:
// the upstream page through the ordinary gated GET of the addressed
// repository, parsed and remapped onto this repository's packages/ mount.
func (h *Handler) serveRemoteProjectPage(w http.ResponseWriter, r *http.Request, repoKey, name string) {
	norm := normalizePackageName(name)
	rc, _, err := h.svc.Get(r.Context(), adapter.PrincipalFrom(r.Context()), repoKey, simplePagePath(norm))
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, segSimple+"/"+name+"/")
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, simplePageReadLimit))
	if rerr != nil {
		writeError(w, http.StatusInternalServerError, "reading the upstream index page: "+rerr.Error())
		return
	}
	entries, _ := parseSimplePage(raw)
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("project '%s' not found in repository '%s'", norm, repoKey))
		return
	}
	sortIndexEntries(entries)
	h.writeProjectPage(w, r, name, entries, false)
}

// writeProjectPage renders the collected entries in the negotiated form,
// with the JSON fallback: an HTML-only contribution forces HTML even for a
// JSON-requesting client (the whole-response rule).
func (h *Handler) writeProjectPage(w http.ResponseWriter, r *http.Request, name string, entries []indexEntry, htmlOnly bool) {
	w.Header().Set("Vary", "Accept")
	if wantsSimpleJSON(r) && !htmlOnly {
		writeSimpleJSON(w, r, name, entries)
		return
	}
	var b strings.Builder
	b.WriteString(indexHead)
	for _, e := range entries {
		href := "../../" + segPackages + "/" + escapePath(e.path) + "#sha256=" + e.sha256
		b.WriteString(`<a href="` + htmlEscape(href) + `">` + htmlEscape(e.filename) + "</a>\n")
	}
	b.WriteString(indexFoot)
	writeIndex(w, r, []byte(b.String()), simpleHTMLMediaType)
}

// entriesFromNodes filters a member's node facts down to one project's
// distribution entries (the local page's own reconstruction, member-side).
func entriesFromNodes(nodes []*metadata.Node, want string) []indexEntry {
	entries := make([]indexEntry, 0, 4)
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue // folder marker rows carry no file
		}
		segs := strings.Split(n.Path, "/")
		if len(segs) != 3 || segs[0] == "" || segs[1] == "" || segs[2] == "" {
			continue // not a <name>/<version>/<filename> storage shape
		}
		if normalizePackageName(segs[0]) != want {
			continue
		}
		entries = append(entries, indexEntry{filename: segs[2], path: n.Path, sha256: n.Sha256, size: n.Size})
	}
	return entries
}

// sortIndexEntries orders entries by filename, ties by path — the
// deterministic order the spec pins (and the ETag's stability condition).
func sortIndexEntries(entries []indexEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].filename != entries[j].filename {
			return entries[i].filename < entries[j].filename
		}
		return entries[i].path < entries[j].path
	})
}

// parseSimplePage parses one upstream simple page, HTML (PEP 503) or JSON
// (PEP 691), into index entries whose PATH is the BinFlow-side fetch path:
// the tail after the last "/packages/" segment of the href when the
// upstream uses the conventional mount, the href's own path otherwise
// (mock and warehouse-like layouts both resolve — the engine fetches the
// member upstream at exactly that path). The second return reports an HTML
// page (the format-fallback input).
func parseSimplePage(raw []byte) ([]indexEntry, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") {
		return parseSimpleJSON([]byte(trimmed))
	}
	return parseSimpleHTML([]byte(trimmed)), true
}

// parseSimpleJSON parses the PEP 691 JSON form.
func parseSimpleJSON(raw []byte) ([]indexEntry, bool) {
	var doc struct {
		Files []struct {
			Filename string            `json:"filename"`
			URL      string            `json:"url"`
			Hashes   map[string]string `json:"hashes"`
			Size     int64             `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false
	}
	entries := make([]indexEntry, 0, len(doc.Files))
	for _, f := range doc.Files {
		if f.Filename == "" {
			continue
		}
		entries = append(entries, indexEntry{
			filename: f.Filename,
			path:     fetchPathOfHref(f.URL),
			sha256:   f.Hashes["sha256"],
			size:     f.Size,
		})
	}
	return entries, false
}

// parseSimpleHTML parses the PEP 503 HTML form.
func parseSimpleHTML(raw []byte) []indexEntry {
	matches := anchorRegExp.FindAllSubmatch(raw, -1)
	entries := make([]indexEntry, 0, len(matches))
	for _, m := range matches {
		href := string(m[1])
		if href == "" {
			href = string(m[2])
		}
		if href == "" {
			continue
		}
		filename := strings.TrimSpace(html.UnescapeString(string(m[3])))
		if filename == "" {
			continue
		}
		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		sha := ""
		if v, ok := strings.CutPrefix(u.Fragment, "sha256="); ok {
			sha = v
		}
		entries = append(entries, indexEntry{
			filename: filename,
			path:     fetchPathOfHref(href),
			sha256:   sha,
		})
	}
	return entries
}

// fetchPathOfHref reduces an upstream href to the repository-relative path
// BinFlow must fetch — and therefore the path the packages/ href rebuilds
// around: the href's own path with leading slashes and parent hops
// stripped, percent-decoded. The upstream's own "packages/" mount prefix
// STAYS in the path (an upstream file the page addresses as
// ../../packages/<name>/<v>/<file> is fetched — and cached — at exactly
// that path), which is why a remote member's entry hrefs read one
// "packages/" deeper than a local member's. Foreign HOSTS are deliberately
// ignored: the engine only ever contacts the member's configured upstream,
// so the path, not the host, is the useful part of the URL.
func fetchPathOfHref(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	p := u.Path
	if p == "" {
		p = href
	}
	for strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") {
		p = strings.TrimPrefix(strings.TrimPrefix(p, "/"), "../")
	}
	if dec, derr := url.PathUnescape(p); derr == nil {
		return dec
	}
	return p
}
