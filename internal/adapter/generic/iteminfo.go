package generic

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// fileInfo is the ItemCreated/FileInfo body (rest-api.md 1.2 and 3). Field
// order mirrors the spec's listing; size is a STRING (PRD 5.5 calibration
// item 6) and timestamps are ISO8601 with milliseconds and zone.
type fileInfo struct {
	URI               string              `json:"uri"`
	DownloadURI       string              `json:"downloadUri"`
	Repo              string              `json:"repo"`
	Path              string              `json:"path"`
	Created           string              `json:"created"`
	CreatedBy         string              `json:"createdBy"`
	LastModified      string              `json:"lastModified,omitempty"`
	ModifiedBy        string              `json:"modifiedBy,omitempty"`
	LastUpdated       string              `json:"lastUpdated,omitempty"`
	Size              string              `json:"size"`
	MimeType          string              `json:"mimeType"`
	Checksums         *checksums          `json:"checksums,omitempty"`
	OriginalChecksums *checksums          `json:"originalChecksums,omitempty"`
	Children          []folderChildLevel1 `json:"children,omitempty"`
}

// checksums is the sha1/md5/sha256 triple. Fields stay omitted (not empty
// strings) when unknown: the spec's "compat (subset)" rule — never echo an
// error value. Both objects are pointers so a zero-declaration upload still
// renders `"originalChecksums": {"sha256": …}` (the A keyset's permanent
// sha256 member) while folder nodes omit them entirely (no checksums exist
// for a directory, T-13 review m4).
type checksums struct {
	Sha1   string `json:"sha1,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// folderChildLevel1 is one FolderInfo children entry. (The trailing digit
// keeps the type name clear of the checksums pointer swap above.)
type folderChildLevel1 struct {
	URI    string `json:"uri"`
	Folder bool   `json:"folder"`
}

// itemInfo renders the FileInfo shape for node. base is scheme://host.
// originalChecksums follows the single-source A keyset off the LANDED node
// (repo.OriginalChecksums, BIN-71 / T-589): the client-registered
// algorithms ∪ {sha256} — the deploy chain already registered the declared
// set into the node's Client columns, so the envelope reads the node, not
// a second hand-rolled copy of the request.
//
// Folder nodes carry no checksums at all: the storage layer's shared
// empty-content sentinel is an internal marker and must not surface as a
// bogus digest value (T-13 review m4).
func (h *Handler) itemInfo(base, repoKey, relPath string, node *metadata.Node, sums digestTriple) fileInfo {
	created := isoMillis(node.CreatedAt, h.now())
	modified := isoMillis(node.UpdatedAt, h.now())
	self := base + productPrefix + "/" + repoKey + "/" + escapePath(relPath)
	info := fileInfo{
		URI:         self,
		DownloadURI: self,
		Repo:        repoKey,
		Path:        "/" + relPath,
		Created:     created,
		CreatedBy:   node.CreatedBy,
		Size:        strconv.FormatInt(node.Size, 10),
		MimeType:    mimeByPath(relPath),
	}
	if !isFolderNode(node) {
		info.Checksums = &checksums{
			Sha1:   sums.sha1,
			Md5:    sums.md5,
			Sha256: sums.sha256,
		}
		o256, o1, o5 := repo.OriginalChecksums(node, sums.sha256)
		info.OriginalChecksums = &checksums{Sha256: o256, Sha1: o1, Md5: o5}
	}
	if node.UpdatedAt != "" && node.UpdatedAt != node.CreatedAt {
		info.LastModified = modified
		info.LastUpdated = modified
		info.ModifiedBy = node.CreatedBy
	}
	return info
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
