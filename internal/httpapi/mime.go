package httpapi

import (
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter/mimetable"
)

// mimeByPath is the table lookup: the shared factory table
// (internal/adapter/mimetable — the single source hoisted from the four
// former lockstep copies, T-581; table content and rule: mimetypes.xml
// v17, docs/reverse/mime-ownership.md section 2), application/octet-
// stream for every miss. Do not re-grow a local copy of the table; a
// value change lands in mimetable once.
func mimeByPath(relPath string) string {
	return mimetable.ByPath(relPath)
}

// ociMediaTreePrefixes names the OCI content-negotiation vendor trees the
// docker and helmoci adapters store verbatim on manifest nodes: that
// mediaType is protocol payload (T-32 R3's pass-through ruling), excluded
// from the path-table ownership flip (BIN-53), so it keeps rendering from
// the stored column. The extension table never produces these spellings,
// so the prefix match cannot shadow a table value.
var ociMediaTreePrefixes = [...]string{
	"application/vnd.docker.",
	"application/vnd.oci.",
	"application/vnd.cncf.",
}

// mimeForNode renders one node's FileInfo mimeType: the OCI mediaType
// carve-out first (see ociMediaTreePrefixes), the extension table for the
// path everywhere else — including rows whose stored column disagrees
// with the table (old-model declared values, other adapters' storage
// constants): those re-render per the table exactly like fresh deploys
// (BIN-53 / T-571 render-time ownership).
func mimeForNode(relPath, stored string) string {
	for _, p := range ociMediaTreePrefixes {
		if strings.HasPrefix(stored, p) {
			return stored
		}
	}
	return mimeByPath(relPath)
}
