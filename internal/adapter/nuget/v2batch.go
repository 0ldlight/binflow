package nuget

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The OData $batch face (nuget.md section 2 #13; the wire shape
// live-captured against nuget.org's own $batch, August 2026): a
// multipart/mixed request of application/http parts, each carrying one
// embedded "GET <resource> HTTP/1.1" request line; the answer is 202
// Accepted with a multipart/mixed body of application/http response parts
// under the batchresponse_<guid> boundary. The sub-request set is the
// search family (Search / Packages / FindPackagesById / GetUpdates, each
// with the ±$count variant); ANY other shape fails the WHOLE batch with
// the 400 "Unsupported batch entry.". A part's query string REPLACES the
// handler's own (the part is the request; there is no outer query to
// merge with on this endpoint).

// v2BatchBodyLimit bounds one batch request (the metadata-class cap).
const v2BatchBodyLimit = 64 << 20

// captureWriter is the minimal http.ResponseWriter the internal dispatch
// renders into (one part's response).
type captureWriter struct {
	hdr    http.Header
	status int
	body   bytes.Buffer
}

func newCaptureWriter() *captureWriter { return &captureWriter{hdr: http.Header{}} }

func (c *captureWriter) Header() http.Header         { return c.hdr }
func (c *captureWriter) WriteHeader(n int)           { c.status = n }
func (c *captureWriter) Write(b []byte) (int, error) { return c.body.Write(b) }

// v2BatchPart is one part's captured answer.
type v2BatchPart struct {
	status int
	ctype  string
	body   []byte
}

// serveV2Batch renders POST $batch.
func (h *Handler) serveV2Batch(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" || params["boundary"] == "" {
		writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
		return
	}
	body, rerr := io.ReadAll(io.LimitReader(r.Body, v2BatchBodyLimit+1))
	if rerr != nil || len(body) > v2BatchBodyLimit {
		writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
		return
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])

	var results []v2BatchPart
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
			return
		}
		raw, rerr := io.ReadAll(io.LimitReader(part, v2BatchBodyLimit))
		_ = part.Close() //nolint:errcheck // read-only part fd
		if rerr != nil {
			writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
			return
		}
		entry, ok := parseV2BatchEntry(raw)
		if !ok {
			writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
			return
		}
		results = append(results, h.runV2BatchEntry(ctx, p, repoKey, class, entry))
	}
	if len(results) == 0 {
		writePlain(w, http.StatusBadRequest, "Unsupported batch entry.")
		return
	}

	guid := make([]byte, 16)
	if _, err := rand.Read(guid); err != nil {
		writePlain(w, http.StatusInternalServerError, fmt.Sprintf("batch guid: %v", err))
		return
	}
	boundary := "batchresponse_" + hex.EncodeToString(guid)
	var out bytes.Buffer
	for _, res := range results {
		out.WriteString("--" + boundary + "\r\n")
		out.WriteString("Content-Type: application/http\r\n")
		out.WriteString("Content-Transfer-Encoding: binary\r\n\r\n")
		fmt.Fprintf(&out, "HTTP/1.1 %d %s\r\n", res.status, http.StatusText(res.status))
		if res.ctype != "" {
			out.WriteString("Content-Type: " + res.ctype + "\r\n")
		}
		fmt.Fprintf(&out, "Content-Length: %d\r\n\r\n", len(res.body))
		out.Write(res.body)
		out.WriteString("\r\n")
	}
	out.WriteString("--" + boundary + "--\r\n")

	hdr := w.Header()
	hdr.Set("Content-Type", "multipart/mixed; boundary="+boundary)
	hdr.Set("DataServiceVersion", v2DataServiceVersion)
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", fmt.Sprint(out.Len()))
	w.WriteHeader(http.StatusAccepted)
	if r.Method != http.MethodHead {
		_, _ = w.Write(out.Bytes()) //nolint:gosec // G705: server-rendered batch envelope over escaped fields
	}
}

// v2BatchEntry is one parsed sub-request: the route plus the part's own
// query string (the override).
type v2BatchEntry struct {
	route  route
	action *http.Request
}

// parseV2BatchEntry parses one part's embedded request: the first line is
// "GET <uri> HTTP/1.x"; the uri carries the resource path and its own
// query string (the override). Absolute paths are accepted in every
// spelling clients emit — the live capture's "/api/v2/<resource>",
// BinFlow's "/binflow/api/nuget/v2/<repo>/<resource>", the bare
// "<resource>" — by scanning for the first resource-literal segment.
func parseV2BatchEntry(raw []byte) (*v2BatchEntry, bool) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	line := text
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		line = text[:i]
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 3 || !strings.EqualFold(fields[0], http.MethodGet) || !strings.HasPrefix(fields[2], "HTTP/") {
		return nil, false
	}
	uri := fields[1]
	rawQuery := ""
	if parsed, err := url.Parse(uri); err == nil {
		rawQuery = parsed.RawQuery
		uri = parsed.EscapedPath()
	} else if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri, rawQuery = uri[:i], uri[i+1:]
	}
	uri, _ = url.PathUnescape(uri)
	resource, ok := v2ResourceSegment(strings.Split(strings.Trim(uri, "/"), "/"))
	if !ok {
		return nil, false
	}
	rt, ok := parseV2(resource, true)
	if !ok {
		return nil, false
	}
	switch rt.kind {
	case kindV2Search, kindV2FindPackages, kindV2GetUpdates, kindV2Packages:
	default:
		return nil, false
	}
	req, err := http.NewRequest(http.MethodGet, "/"+resource, http.NoBody) //nolint:gosec // G704: the resource is parse-validated against the closed route set; this URL is an internal dispatch, never an outbound hop
	if err != nil {
		return nil, false
	}
	req.URL.RawQuery = rawQuery
	return &v2BatchEntry{route: rt, action: req}, true
}

// v2ResourceSegment finds the first resource-literal segment of a split
// path and returns the path from there (any mount/repo prefix in front of
// it is dropped).
func v2ResourceSegment(segments []string) (string, bool) {
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		name := strings.TrimSuffix(seg, "()")
		if name == seg && strings.HasSuffix(seg, ")") && strings.HasPrefix(seg, v2Packages+"(") {
			name = v2Packages // the argument-bearing Packages(...) form
		}
		switch name {
		case v2Search, v2GetUpdates, findPackagesByID, v2Packages:
			return strings.Join(segments[i:], "/"), true
		}
	}
	return "", false
}

// runV2BatchEntry executes one sub-request through the ordinary collection
// dispatcher and captures its response.
func (h *Handler) runV2BatchEntry(ctx context.Context, p *repo.Principal, repoKey, class string, entry *v2BatchEntry) v2BatchPart {
	cw := newCaptureWriter()
	req := entry.action.WithContext(ctx)
	h.serveV2Collection(ctx, cw, req, p, repoKey, class, entry.route)
	return v2BatchPart{status: cw.status, ctype: cw.hdr.Get("Content-Type"), body: cw.body.Bytes()}
}
