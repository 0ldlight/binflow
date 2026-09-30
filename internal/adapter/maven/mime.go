package maven

import "github.com/lzwzzy/binflow/internal/adapter/mimetable"

// mimeForPath is the storage-plane mime authority: the shared factory
// table lookup (internal/adapter/mimetable — the single source hoisted
// from the four former lockstep copies, T-581; table content and rule:
// mimetypes.xml v17, docs/reverse/mime-ownership.md section 2). Per the
// BIN-53 / T-571 ownership ruling the table is the ONLY mime authority
// on the storage faces: PUT stores the table value for the deployed path
// (the request's declared Content-Type is ignored), and GET/FileInfo
// look the path up here again at render time; table misses answer
// application/octet-stream with no stdlib fallback. Factory quirk kept
// here: NO .sha512 entry (an undeclared .sha512 path falls to
// octet-stream; the sidecar GET face keeps its own x-checksum protocol
// constant, ruled separately in L032). Do not re-grow a local copy of
// the table; a value change lands in mimetable once.
func mimeForPath(relPath string) string {
	return mimetable.ByPath(relPath)
}
