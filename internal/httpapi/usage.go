package httpapi

import (
	"net/http"
	"strings"
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
