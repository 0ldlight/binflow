package maven

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// fileInfo is the ItemCreated body of a successful deploy (rest-api.md
// section 1.2): size is a STRING (the Java field spelling's habit), and
// the timestamp fields carry the store's RFC3339 values verbatim.
type fileInfo struct {
	URI               string     `json:"uri"`
	DownloadURI       string     `json:"downloadUri"`
	Repo              string     `json:"repo"`
	Path              string     `json:"path"`
	Created           string     `json:"created"`
	CreatedBy         string     `json:"createdBy"`
	LastModified      string     `json:"lastModified,omitempty"`
	ModifiedBy        string     `json:"modifiedBy,omitempty"`
	LastUpdated       string     `json:"lastUpdated,omitempty"`
	Size              string     `json:"size"`
	MimeType          string     `json:"mimeType"`
	Checksums         *checksums `json:"checksums,omitempty"`
	OriginalChecksums *checksums `json:"originalChecksums,omitempty"`
}

// checksums is the sha1/md5/sha256 triple; fields stay omitted when
// unknown (never echo an error value).
type checksums struct {
	Sha1   string `json:"sha1,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// errorEnvelope is the unified non-2xx body the three M3 protocol
// adapters share (maven-npm-pypi.md section 0, high confidence):
// {"errors":[{"status":..,"message":..}]}.
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

// sha256Hex hashes b into lowercase hex.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
