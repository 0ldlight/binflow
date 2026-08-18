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
// renders `"originalChecksums": {}` while folder nodes omit them entirely
// (no checksums exist for a directory, T-13 review m4).
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
// up (when set) limits originalChecksums to the algorithms the client
// actually declared on the upload that triggered this render — zero
// declarations renders an empty object, never a fallback echo (T-13 review
// m1). A nil up means "no upload context" (downloads, storage-info
// renders), where the stored triple is the best echo available.
//
// Folder nodes carry no checksums at all: the storage layer's shared
// empty-content sentinel is an internal marker and must not surface as a
// bogus digest value (T-13 review m4).
func (h *Handler) itemInfo(base, repoKey, relPath string, node *metadata.Node, sums digestTriple, up uploadContext) fileInfo {
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
		MimeType:    mimeForNode(relPath, node.Mime),
	}
	if !isFolderNode(node) {
		info.Checksums = &checksums{
			Sha1:   sums.sha1,
			Md5:    sums.md5,
			Sha256: sums.sha256,
		}
		info.OriginalChecksums = &checksums{}
		*info.OriginalChecksums = originalChecksumsOf(sums, up)
	}
	if node.UpdatedAt != "" && node.UpdatedAt != node.CreatedAt {
		info.LastModified = modified
		info.LastUpdated = modified
		info.ModifiedBy = node.CreatedBy
	}
	return info
}

// originalChecksumsOf echoes the client-declared digests. With upload
// context the declared set is authoritative (zero declarations = empty
// object); without upload context the stored triple is the best echo
// available.
func originalChecksumsOf(sums digestTriple, up uploadContext) checksums {
	if up.declared == nil {
		return checksums{Sha1: sums.sha1, Md5: sums.md5, Sha256: sums.sha256}
	}
	out := checksums{}
	if up.declared["sha1"] {
		out.Sha1 = sums.sha1
	}
	if up.declared["md5"] {
		out.Md5 = sums.md5
	}
	if up.declared["sha256"] {
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
