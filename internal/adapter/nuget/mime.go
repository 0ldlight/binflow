package nuget

import "github.com/lzwzzy/binflow/internal/adapter/mimetable"

// mimeForPath is the envelope mimeType authority: the shared factory
// table lookup (internal/adapter/mimetable — the single source hoisted
// from the four former lockstep copies, T-581; table content and rule:
// mimetypes.xml v17, docs/reverse/mime-ownership.md section 2). The
// bare-content PUT 201's ItemCreated envelope renders its mimeType field
// from this lookup (T-579); table misses answer application/octet-stream
// with no stdlib fallback. Do not re-grow a local copy of the table; a
// value change lands in mimetable once.
func mimeForPath(relPath string) string {
	return mimetable.ByPath(relPath)
}
