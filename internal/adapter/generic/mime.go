package generic

import "github.com/lzwzzy/binflow/internal/adapter/mimetable"

// mimeByPath is the storage-plane mime authority: the shared factory
// table lookup (internal/adapter/mimetable — the single source hoisted
// from the four former lockstep copies, T-581; table content and rule:
// mimetypes.xml v17, docs/reverse/mime-ownership.md section 2). Per the
// BIN-53 / T-571 ownership ruling the table is the ONLY mime authority
// on the storage faces: PUT stores the table value for the deployed path
// (the request's declared Content-Type is ignored), and GET/FileInfo
// look the path up here again at render time. Table misses answer
// application/octet-stream; there is no stdlib fallback (the OS mime
// database answers per host — a drift family the reference does not
// have). Do not re-grow a local copy of the table here; a value change
// lands in mimetable once.
func mimeByPath(relPath string) string {
	return mimetable.ByPath(relPath)
}
