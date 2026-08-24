package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// Repository quota usage observability (GE-06/W26b, PRD FR-31, T-95):
//
//	GET /binflow/api/v1/storage/usage/{repo} -> {"repo","usedBytes","quotaBytes"}
//
// usedBytes is the same logical-byte total the quota gate enforces against
// (repo_usage, maintained same-transaction with node writes); quotaBytes is
// the configured ceiling, 0 meaning unlimited. Access is the family-7 OR
// formula (architecture section 7.1, K11; T-217 added the second arm):
// CanManageRepo(read) ∨ Can(r) — admin and readonly_admin pass through the
// role's global read, a plain user passes with a read grant OR the manage
// bit on the repository (a repo admin who may set quotaBytes may not be
// blind to the usage). The route demands authentication; the service use
// case owns the OR decision so the denial renders 403 (not a 401 challenge)
// for an authenticated non-reader.

// usageBody is the GE-06 response shape; the field spellings are the PRD's
// ({repo, usedBytes, quotaBytes}).
type usageBody struct {
	Repo       string `json:"repo"`
	UsedBytes  int64  `json:"usedBytes"`
	QuotaBytes int64  `json:"quotaBytes"`
}

// handleStorageUsage serves GET /api/v1/storage/usage/{repo}. Unknown keys
// answer the E-01 envelope 404 (through the storage plane's mapper, the same
// wording as every other repository-addressed endpoint); a principal without
// the read grant answers 403.
func (s *Server) handleStorageUsage(w http.ResponseWriter, r *http.Request, repoKey string) {
	if strings.Contains(repoKey, "/") { // defense: the router already splits one segment
		notImplemented(w, "/binflow/api/v1/storage/usage")
		return
	}
	u, err := s.deps.ReposSvc.Usage(r.Context(), principalFrom(r.Context()), repoKey)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, usageBody{
		Repo:       u.RepoKey,
		UsedBytes:  u.UsedBytes,
		QuotaBytes: u.QuotaBytes,
	})
}

// Batch usage observability (M9 E1, ADR-0030 / architecture section 14.1,
// FR-79.1 — the fan-out collapse of the endpoint above):
//
//	GET /binflow/api/v1/storage/usage            -> [{"repo","usedBytes","quotaBytes"}, ...]
//	GET ...?repos=a,b,c                          -> the point-named subset
//	GET ...?include=counts                       -> rows add {"nodeCount","updatedAt"}
//
// The body is a BARE array (K20: the /api/v1 list-family convention —
// permissions, replications), one row per repository in the caller's
// visibility set, ordered by repo key; an empty set answers "[]" — a set
// endpoint owes a filtered view to any authenticated caller, never a 403.
// Visibility is the family-7 OR formula evaluated per repository in the use
// case, so a batch row can never appear where the single-repo endpoint
// above would answer 403, and an invisible repository leaks nothing: a
// point-named key that is unknown OR unreadable is simply absent from the
// array, the two indistinguishable by design. updatedAt is the repository
// CONFIG change moment (repositories.updated_at), not the newest artifact
// time — the semantic is pinned in section 14.1 and in the user docs.

// usageBatchItem is the E1 base row, field-for-field the usageBody shape
// (the console shares one type across both endpoints, section 14.1).
type usageBatchItem struct {
	Repo       string `json:"repo"`
	UsedBytes  int64  `json:"usedBytes"`
	QuotaBytes int64  `json:"quotaBytes"`
}

// usageBatchCountsItem is the ?include=counts row: the base fields plus
// nodeCount/updatedAt ALWAYS rendered — an empty repository's nodeCount 0 is
// information, not absence, so omitempty has no place here (the default row
// shape stays exactly the base trio instead).
type usageBatchCountsItem struct {
	usageBatchItem
	NodeCount int64  `json:"nodeCount"`
	UpdatedAt string `json:"updatedAt"`
}

// handleStorageUsageBatch serves GET /api/v1/storage/usage. Query knobs:
// ?repos= comma-separated point names (absent = the full visibility set; a
// present-but-empty value point-names nothing and answers []), and
// ?include=counts. An include value outside {counts} answers the E-01 400
// envelope — the governance family's explicit-rejection convention (unknown
// filter values are refused, not silently ignored; section 14.1 E6's rule,
// applied to this sibling parameter).
func (s *Server) handleStorageUsageBatch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	includeCounts := false
	for _, v := range q["include"] {
		switch strings.TrimSpace(v) {
		case "":
			// An empty value carries no ask (?include=&...): tolerated,
			// like every other optional parameter's empty spelling.
		case "counts":
			includeCounts = true
		default:
			writeError(w, http.StatusBadRequest,
				`include must be "counts" (unknown include value: `+strconv.Quote(strings.TrimSpace(v))+")")
			return
		}
	}

	var repos []string
	if q.Has("repos") {
		// The parameter is present: the selection starts EMPTY (not nil),
		// so a present-but-empty value point-names nothing and answers []
		// — the literal intersection reading, never a silent "all".
		repos = []string{}
		for _, raw := range q["repos"] {
			for _, key := range strings.Split(raw, ",") {
				if key = strings.TrimSpace(key); key != "" {
					repos = append(repos, key)
				}
			}
		}
	}

	rows, err := s.deps.ReposSvc.UsageBatch(r.Context(), principalFrom(r.Context()),
		repo.UsageBatchQuery{Repos: repos, IncludeCounts: includeCounts})
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	if !includeCounts {
		items := make([]usageBatchItem, 0, len(rows)) // never nil: empty set renders []
		for _, row := range rows {
			items = append(items, usageBatchItem{
				Repo:       row.RepoKey,
				UsedBytes:  row.UsedBytes,
				QuotaBytes: row.QuotaBytes,
			})
		}
		writeJSONBody(w, http.StatusOK, items)
		return
	}
	items := make([]usageBatchCountsItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, usageBatchCountsItem{
			usageBatchItem: usageBatchItem{
				Repo:       row.RepoKey,
				UsedBytes:  row.UsedBytes,
				QuotaBytes: row.QuotaBytes,
			},
			NodeCount: row.NodeCount,
			UpdatedAt: row.UpdatedAt,
		})
	}
	writeJSONBody(w, http.StatusOK, items)
}
