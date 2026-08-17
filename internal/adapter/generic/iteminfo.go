package generic

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// fileInfo is the ItemCreated/FileInfo body (rest-api.md 1.2 and 3). Field
// order mirrors the spec's listing; size is a STRING (PRD 5.5 calibration
// item 6) and timestamps are ISO8601 with milliseconds and zone.
type fileInfo struct {
	URI               string             `json:"uri"`
	DownloadURI       string             `json:"downloadUri"`
	Repo              string             `json:"repo"`
	Path              string             `json:"path"`
	Created           string             `json:"created"`
	CreatedBy         string             `json:"createdBy"`
	LastModified      string             `json:"lastModified,omitempty"`
	ModifiedBy        string             `json:"modifiedBy,omitempty"`
	LastUpdated       string             `json:"lastUpdated,omitempty"`
	Size              string             `json:"size"`
	MimeType          string             `json:"mimeType"`
	Checksums         checksums          `json:"checksums"`
	OriginalChecksums checksums          `json:"originalChecksums"`
	Children          []folderChildLevel `json:"children,omitempty"`
}

// checksums is the sha1/md5/sha256 triple. Fields stay omitted (not empty
// strings) when unknown: the spec's "compat (subset)" rule — never echo an
// error value.
type checksums struct {
	Sha1   string `json:"sha1,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// folderChildLevel is one FolderInfo children entry.
type folderChildLevel struct {
	URI    string `json:"uri"`
	Folder bool   `json:"folder"`
}

// itemInfo renders the FileInfo shape for node. base is scheme://host.
// declared limits originalChecksums to the algorithms the client actually
// supplied on upload (the ancillary source of that map); without upload
// context (downloads, folder GETs) originalChecksums falls back to the
// stored triple, matching Artifactory's behavior of echoing what it was
// told at deploy time.
func (h *Handler) itemInfo(base, repoKey, relPath string, node *metadata.Node, sums digestTriple, declared map[string]bool) fileInfo {
	created := isoMillis(node.CreatedAt, h.now())
	modified := isoMillis(node.UpdatedAt, h.now())
	info := fileInfo{
		URI:         base + "/" + repoKey + "/" + escapePath(relPath),
		DownloadURI: base + "/" + repoKey + "/" + escapePath(relPath),
		Repo:        repoKey,
		Path:        "/" + relPath,
		Created:     created,
		CreatedBy:   node.CreatedBy,
		Size:        strconv.FormatInt(node.Size, 10),
		MimeType:    mimeOr(node.Mime),
		Checksums: checksums{
			Sha1:   sums.sha1,
			Md5:    sums.md5,
			Sha256: sums.sha256,
		},
		OriginalChecksums: originalChecksumsOf(sums, declared),
	}
	if node.UpdatedAt != "" && node.UpdatedAt != node.CreatedAt {
		info.LastModified = modified
		info.LastUpdated = modified
		info.ModifiedBy = node.CreatedBy
	}
	return info
}

// originalChecksumsOf echoes the client-declared digests. On upload the
// declared set is authoritative (what the client sent); when re-rendering
// an item without upload context, the stored triple is the best echo
// available.
func originalChecksumsOf(sums digestTriple, declared map[string]bool) checksums {
	if len(declared) == 0 {
		return checksums{Sha1: sums.sha1, Md5: sums.md5, Sha256: sums.sha256}
	}
	out := checksums{}
	if declared["sha1"] {
		out.Sha1 = sums.sha1
	}
	if declared["md5"] {
		out.Md5 = sums.md5
	}
	if declared["sha256"] {
		out.Sha256 = sums.sha256
	}
	return out
}

// errorEnvelope is the unified non-2xx body (rest-api.md section 0):
// {"errors":[{"status":..,"message":..}]}, pretty-printed.
type errorEnvelope struct {
	Errors []errorEntry `json:"errors"`
}

type errorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// writeError emits the errors[] envelope with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Errors: []errorEntry{{Status: status, Message: message}}})
}

// writeJSON emits a 2xx JSON body (pretty-printed, Artifactory style).
func writeJSON(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
