package nuget

import (
	"crypto/md5"  //nolint:gosec // G501: MD5 is a wire-compat digest echo, not a security primitive
	"crypto/sha1" //nolint:gosec // G401: same — SHA-1 echoes the reference's triple
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The bare-content PUT's 201 and its checksum-policy 409, rendered to the
// A face's byte shape (T-579 / BIN-61; live raw = /tmp/r10/a-raw.json,
// Artifactory 7.161.26, revision 86126900):
//
//   - 201: CT application/vnd.org.jfrog.artifactory.storage.ItemCreated
//     +json;charset=UTF-8 (no space before the charset parameter) plus the
//     Jackson-pretty-printed ItemCreated envelope — 2-space indent, " : "
//     separators, field order repo, path, created, createdBy, downloadUri,
//     mimeType, size, checksums, originalChecksums, uri (uri LAST), and no
//     trailing newline.
//   - 409 (a well-formed declared digest that disagrees): the errors[]
//     JSON envelope under CT application/json;charset=ISO-8859-1, its
//     message the CLIENT-policy rejection family with the ChecksumsInfo
//     dump. B's former wording leaked the storage session id — this render
//     is built ONLY from adapter-local facts (the declared headers and the
//     body's measured digests), so no internal identifier can surface.

// contentTypeItemCreated is the bare-content PUT 201 body's Content-Type
// (the A raw's exact spelling — no space before charset; maven's local
// copy of this constant carries a space, each pinned on its own evidence).
const contentTypeItemCreated = "application/vnd.org.jfrog.artifactory.storage.ItemCreated+json;charset=UTF-8"

// contentTypeErrorJSON is the 409 errors-envelope Content-Type (the A
// raw's exact spelling).
const contentTypeErrorJSON = "application/json;charset=ISO-8859-1"

// jsonStr renders one JSON string literal (quoted, escaped).
func jsonStr(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// Strings only — Marshal cannot fail on them; keep the renderer
		// total rather than threading an unreachable error out.
		return `""`
	}
	return string(b)
}

// itemCreatedBody renders the ItemCreated envelope (the A raw's byte
// shape). sums is the server-measured triple.
func itemCreatedBody(self, repoKey, relPath string, node *metadata.Node, sums digestTriple) []byte {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("{")
	line("  %s : %s,", jsonStr("repo"), jsonStr(repoKey))
	line("  %s : %s,", jsonStr("path"), jsonStr("/"+relPath))
	line("  %s : %s,", jsonStr("created"), jsonStr(node.CreatedAt))
	line("  %s : %s,", jsonStr("createdBy"), jsonStr(node.CreatedBy))
	line("  %s : %s,", jsonStr("downloadUri"), jsonStr(self))
	line("  %s : %s,", jsonStr("mimeType"), jsonStr(mimeForPath(relPath)))
	line("  %s : %s,", jsonStr("size"), jsonStr(strconv.FormatInt(node.Size, 10)))
	// checksums: the measured triple, unknown members omitted (never an
	// error value); sha1, md5, sha256 in the raw's order.
	if cs := memberLines(sums.sha1, sums.md5, sums.sha256); len(cs) > 0 {
		line("  %s : {", jsonStr("checksums"))
		line(strings.Join(cs, ",\n"))
		line("  },")
	}
	// originalChecksums: the single ADR-0052 assembly point (see
	// originalChecksumsLines).
	if oc := originalChecksumsLines(node, sums); len(oc) > 0 {
		line("  %s : {", jsonStr("originalChecksums"))
		line(strings.Join(oc, ",\n"))
		line("  },")
	}
	// uri LAST, no trailing newline after the closing brace.
	fmt.Fprintf(&b, "  %s : %s\n}", jsonStr("uri"), jsonStr(self))
	return []byte(b.String())
}

// memberLines renders the checksum object members present in the raw's
// order (sha1, md5, sha256), skipping the empty ones.
func memberLines(sha1v, md5v, sha256v string) []string {
	var out []string
	if sha1v != "" {
		out = append(out, fmt.Sprintf("    %s : %s", jsonStr("sha1"), jsonStr(sha1v)))
	}
	if md5v != "" {
		out = append(out, fmt.Sprintf("    %s : %s", jsonStr("md5"), jsonStr(md5v)))
	}
	if sha256v != "" {
		out = append(out, fmt.Sprintf("    %s : %s", jsonStr("sha256"), jsonStr(sha256v)))
	}
	return out
}

// originalChecksumsLines assembles the envelope's originalChecksums
// members off the shared A keyset (the ledger's
// httpapi/original-checksums-key-model, BIN-71 / T-589): sha256 is ALWAYS
// a member (the raw-pinned fill rule — the declared value when one passed
// validation, the measured one otherwise), sha1/md5 only when the node
// carries a client registration. ADR-0052 decision 4: the whole rule is
// single-sourced through repo.OriginalChecksums off the landed node — on
// this 201 path the node's Client columns hold exactly the validated
// declared set, so the members equal the declared echo of the raw.
func originalChecksumsLines(node *metadata.Node, sums digestTriple) []string {
	o256, oSha1, oMd5 := repo.OriginalChecksums(node, sums.sha256)
	return memberLines(oSha1, oMd5, o256)
}

// writeBareCreated renders the bare-content PUT 201: Location (the
// absolute context-prefixed address, T-567), X-Checksum-Sha256 (the
// effective sha256, T-575) and the ItemCreated envelope body (T-579). The
// body's uri is the Location's value byte-for-byte (the A raw renders
// them equal).
func writeBareCreated(w http.ResponseWriter, r *http.Request, repoKey, rel string, node *metadata.Node, sums digestTriple) {
	self := requestBase(r) + productPrefix + "/" + repoKey + "/" + escapePath(rel)
	hdr := w.Header()
	if node != nil && node.Sha256 != "" {
		hdr.Set(hdrChecksumSha256, node.Sha256)
	}
	hdr.Set("Location", self)
	hdr.Set("Content-Type", contentTypeItemCreated)
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(itemCreatedBody(self, repoKey, rel, node, sums)) //nolint:gosec // G705: server-rendered envelope
}

// writeChecksumPolicy409 renders the declared-digest disagreement as the
// A face's CLIENT-policy rejection: the errors[] JSON envelope (Jackson
// spacing) whose message carries the ChecksumsInfo dump in the raw's
// member order (SHA-1, MD5, SHA-256; original='null' for the undeclared).
// declared are the client's X-Checksum-* headers; actual is the streamed
// body's measured triple — the storage error itself (session ids, the
// internal wrap chain) is deliberately NOT consulted, so no internal
// detail can leak (the information-exposure requirement this render
// exists for).
func writeChecksumPolicy409(w http.ResponseWriter, repoKey, rel string, declared storage.BlobRef, actual digestTriple) {
	mk := func(typ, orig, act string) string {
		if orig == "" {
			orig = "null"
		}
		return fmt.Sprintf("ChecksumInfo{type=%s, original='%s', actual='%s'}", typ, orig, act)
	}
	msg := fmt.Sprintf("Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact '%s:%s'. "+
		"Checksums info: ChecksumsInfo{checksums={SHA-1=%s, MD5=%s, SHA-256=%s}}",
		repoKey, rel,
		mk("SHA-1", declared.Sha1, actual.sha1),
		mk("MD5", declared.Md5, actual.md5),
		mk("SHA-256", declared.Sha256, actual.sha256))
	w.Header().Set("Content-Type", contentTypeErrorJSON)
	w.WriteHeader(http.StatusConflict)
	_, _ = io.WriteString(w, //nolint:gosec // G705: server-rendered error envelope
		"{\n  "+jsonStr("errors")+" : [ {\n    "+jsonStr("status")+" : 409,\n    "+
			jsonStr("message")+" : "+jsonStr(msg)+"\n  } ]\n}")
}

// measuredBody wraps the PUT body so the digests the 409's ChecksumsInfo
// needs are measured as the bytes stream through to the service (the
// mismatch compare runs at commit, i.e. after the last byte — the sums are
// complete whenever the mismatch error can fire).
type measuredBody struct {
	r              io.Reader
	h256, h1, hmd5 hash.Hash
}

// newMeasuredBody wires the hashing tee.
func newMeasuredBody(r io.Reader) *measuredBody {
	//nolint:gosec // G401/G501: the wire-compat digest triple's measurement, not a security primitive
	return &measuredBody{r: r, h256: sha256.New(), h1: sha1.New(), hmd5: md5.New()}
}

// Read streams through the hash tee.
func (m *measuredBody) Read(p []byte) (int, error) {
	n, err := m.r.Read(p)
	if n > 0 {
		_, _ = m.h256.Write(p[:n]) //nolint:errcheck // hash.Write never errors
		_, _ = m.h1.Write(p[:n])   //nolint:errcheck // hash.Write never errors
		_, _ = m.hmd5.Write(p[:n]) //nolint:errcheck // hash.Write never errors
	}
	return n, err
}

// sums renders the measured triple.
func (m *measuredBody) sums() digestTriple {
	return digestTriple{
		sha256: hex.EncodeToString(m.h256.Sum(nil)),
		sha1:   hex.EncodeToString(m.h1.Sum(nil)),
		md5:    hex.EncodeToString(m.hmd5.Sum(nil)),
	}
}
