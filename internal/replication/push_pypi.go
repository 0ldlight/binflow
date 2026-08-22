package replication

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

// The pypi push plane (T-195, closing the D4 chain end to end: twine-style
// uploads land through repo.Service.PutLandedBlob, whose new replication
// hook enqueues the task; this plane then drives the TARGET's warehouse
// upload face — the same multipart POST twine speaks).
//
// The pypi node layout is <name>/<version>/<filename> and the simple index
// is DERIVED from the node listing at serve time (adapter/pypi simple.go),
// so landing the distribution file is the whole replication: the target's
// /simple/ index and the packages/ download face light up from the node
// alone. Raw content-path writes are read-only on the pypi face (PE-03's
// second entrance), so the multipart upload at the repository root is the
// one writable surface — protocol-faithful, credentials-checked, and it
// re-runs the target's own filename/no-overwrite governance.

// pushPypi is the pypi plane entry.
func (e *Engine) pushPypi(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath string) error {
	name, version, filename, err := splitPypiNodePath(nodePath)
	if err != nil {
		return notRetryable(err)
	}

	// Step 1: idempotence probe on the bare content entrance (read-only
	// GET/HEAD face, same checksum header family as the generic plane).
	u, err := e.planeURL(cfg, nodePath, append([]string{cfg.TargetRepo}, strings.Split(nodePath, "/")...)...)
	if err != nil {
		return err
	}
	resp, err := e.do(ctx, http.MethodHead, u, cfg, nil, 0, nil)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		_ = resp.Body.Close()
		if sum := resp.Header.Get("X-Checksum-Sha256"); sum != "" {
			if strings.EqualFold(sum, sha256) {
				return nil // the target already holds these exact bytes
			}
			return notRetryable(fmt.Errorf(
				"conflict: target holds sha256 %s at the path, source sha256 is %s; target left untouched (Q7 interim)",
				strings.ToLower(sum), sha256))
		}
	case http.StatusNotFound:
		_ = resp.Body.Close()
	default:
		return classifyPypi(resp, "probe")
	}

	// Step 2: the warehouse upload. The multipart body streams the blob
	// through a pipe (no full-body buffering; the storage session on the
	// target side streams too).
	rc, ref, err := e.blobs.Open(ctx, sha256)
	if err != nil {
		return notRetryable(fmt.Errorf("source blob %s: %w", sha256, err))
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd

	mime := "application/octet-stream"
	if e.cfg.meta != nil {
		if n, merr := e.cfg.meta.Node(ctx, cfg.SourceRepo, nodePath); merr == nil && n.Mime != "" {
			mime = n.Mime
		}
	}

	uploadURL, err := e.planeURL(cfg, nodePath, cfg.TargetRepo)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		werr := func() error {
			for _, kv := range [][2]string{
				{":action", "file_upload"},
				{"name", name},
				{"version", version},
				{"sha256_digest", sha256},
			} {
				if werr := writer.WriteField(kv[0], kv[1]); werr != nil {
					return werr
				}
			}
			if ref.Md5 != "" {
				if werr := writer.WriteField("md5_digest", ref.Md5); werr != nil {
					return werr
				}
			}
			hdr := textproto.MIMEHeader{}
			hdr.Set("Content-Disposition", fmt.Sprintf(
				`form-data; name="content"; filename=%q`, sanitizeFormFilename(filename)))
			hdr.Set("Content-Type", mime)
			part, werr := writer.CreatePart(hdr)
			if werr != nil {
				return werr
			}
			if _, werr := io.Copy(part, rc); werr != nil {
				return werr
			}
			return writer.Close()
		}()
		if werr != nil {
			_ = pw.CloseWithError(werr)
		} else {
			_ = pw.Close()
		}
	}()

	hdr := http.Header{}
	hdr.Set("Content-Type", writer.FormDataContentType())
	resp, err = e.do(ctx, http.MethodPost, uploadURL, cfg, pr, 0, hdr)
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		_ = resp.Body.Close()
		return nil
	default:
		return classifyPypi(resp, "upload "+name+"-"+version)
	}
}

// splitPypiNodePath parses <name>/<version>/<filename> (the upload face's
// own path composition). Extra segments beyond three are refused: the
// warehouse layout never nests deeper, and a task row claiming otherwise
// is a mangled write.
func splitPypiNodePath(nodePath string) (name, version, filename string, err error) {
	segs := strings.Split(nodePath, "/")
	if len(segs) < 3 {
		return "", "", "", fmt.Errorf("pypi node path %q: not the <name>/<version>/<filename> layout", nodePath)
	}
	name, version, filename = segs[0], segs[1], segs[len(segs)-1]
	if name == "" || version == "" || filename == "" {
		return "", "", "", fmt.Errorf("pypi node path %q: empty segment", nodePath)
	}
	return name, version, filename, nil
}

// sanitizeFormFilename neutralizes the characters the Content-Disposition
// quoting could otherwise smuggle (the source's own upload validator bounds
// the charset; this is the defensive belt).
func sanitizeFormFilename(name string) string {
	r := strings.NewReplacer(`\`, `_`, `"`, `_`, "\r", "_", "\n", "_")
	return r.Replace(name)
}

// classifyPypi maps a non-2xx pypi-face response onto a task error. The
// upload face's deterministic refusals — 400 (malformed form, or the
// warehouse "file already exists" no-overwrite rule: the initial HEAD probe
// already handled the idempotent case, so a 400 here means a same-path
// different-content race or a rejected form) — are terminal; 401/403/404/5xx
// stay retryable (an admin creating the target repo or fixing credentials
// revives them, D1).
func classifyPypi(resp *http.Response, what string) error {
	err := fmt.Errorf("%s: target answered %d %s", what, resp.StatusCode, strings.TrimSpace(readSnippet(resp)))
	if resp.StatusCode == http.StatusBadRequest {
		return notRetryable(err)
	}
	return err
}
