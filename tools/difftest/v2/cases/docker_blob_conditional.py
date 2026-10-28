"""Replays contract entries docker/remote-blob-conditional-get (#12, arms
B1/B4/B6), docker/remote-blob-unknown-digest-404 (#14) and
docker/remote-blob-empty-digest-synthetic (#15).

B1 GET + INM quoted match -> 304 with the FULL artifact face (Etag,
Last-Modified, X-Checksum triple, X-Artifactory-Filename,
Docker-Content-Digest, Accept-Ranges) and no Content-Type/Content-Length.
B4 INM non-match -> 200 full body. B6 HEAD + INM -> 200 (HEAD never 304).
Unknown digest (run-scoped): 404 compact BLOB_UNKNOWN with detail.blobSum
echo, api-version header, local fast answer.
Empty-layer digest: synthetic 200, 32 bytes, body sha256 == digest,
Docker-Content-Digest echo.
"""
import hashlib
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-blob-conditional",
    "title": "blob conditional B1/B4/B6 + unknown-digest 404 + empty synthetic",
    "layer": "L6",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    unknown_digest = hashlib.sha256(("difftest-unknown-%s" % dl.NONCE).encode()).hexdigest()
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        mf = dl.get_manifest(ctx, side, dl.POSITIVE_TAG, token=tok)
        if mf["status"] != 200:
            checks.add(side, "manifest-200", False, "status %s" % mf["status"])
            continue
        layer_dig = dl.jbody(mf)["layers"][0]["digest"].split(":", 1)[1]
        base = dl.get_blob(ctx, side, layer_dig, token=tok)
        if base["status"] != 200:
            checks.add(side, "blob-200", False, "status %s" % base["status"])
            continue
        etag = (dl.hdr(base, "Etag") or "").strip('"')
        quoted = '"%s"' % etag
        path = "/v2/%s/%s/blobs/sha256:%s" % (dl.REPO, dl.IMAGE, layer_dig)

        b1 = ctx.http(side, "GET", path,
                      headers={"Authorization": "Bearer " + tok, "If-None-Match": quoted})
        checks.add(side, "b1-quoted-inm status-304", b1["status"] == 304, "got %s" % b1["status"])
        for h in ("Etag", "Last-Modified", "X-Checksum-Md5", "X-Checksum-Sha1",
                  "X-Checksum-Sha256", "X-Artifactory-Filename",
                  "Docker-Content-Digest", "Accept-Ranges"):
            checks.add(side, "b1 has-" + h, dl.hdr(b1, h) is not None, dl.hdr(b1, h))
        checks.add(side, "b1 no-content-type", dl.hdr(b1, "Content-Type") is None,
                   dl.hdr(b1, "Content-Type"))
        checks.add(side, "b1 no-content-length", dl.hdr(b1, "Content-Length") is None,
                   dl.hdr(b1, "Content-Length"))

        b4 = ctx.http(side, "GET", path,
                      headers={"Authorization": "Bearer " + tok,
                               "If-None-Match": '"difftest-nonmatch-%s"' % dl.NONCE})
        checks.add(side, "b4-nonmatch-inm status-200", b4["status"] == 200, "got %s" % b4["status"])
        checks.add(side, "b4 full-body", dl.sha256_hex(b4["body"]) == layer_dig,
                   "sha=%s" % dl.sha256_hex(b4["body"])[:16])

        b6 = ctx.http(side, "HEAD", path,
                      headers={"Authorization": "Bearer " + tok, "If-None-Match": quoted})
        checks.add(side, "b6-head-inm status-200", b6["status"] == 200, "got %s" % b6["status"])

        unk = dl.get_blob(ctx, side, unknown_digest, token=tok)
        checks.add(side, "unknown-digest status-404", unk["status"] == 404, "got %s" % unk["status"])
        checks.add(side, "unknown-digest api-version",
                   dl.hdr(unk, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(unk, "Docker-Distribution-Api-Version"))
        try:
            e0 = (dl.jbody(unk).get("errors") or [{}])[0]
            detail = e0.get("detail") or {}
            checks.add(side, "unknown-digest code", e0.get("code") == "BLOB_UNKNOWN",
                       e0.get("code"))
            checks.add(side, "unknown-digest message",
                       e0.get("message") == "blob unknown to registry", e0.get("message"))
            checks.add(side, "unknown-digest blobSum-echo",
                       detail.get("blobSum") == "sha256:" + unknown_digest, detail)
        except Exception as e:
            checks.add(side, "unknown-digest body-json", False, repr(e))

        emp = dl.get_blob(ctx, side, dl.EMPTY_LAYER_DIGEST, token=tok)
        checks.add(side, "empty-synthetic status-200", emp["status"] == 200,
                   "got %s" % emp["status"])
        checks.add(side, "empty-synthetic body-32-bytes", len(emp["body"]) == 32,
                   "len=%d" % len(emp["body"]))
        checks.add(side, "empty-synthetic body-sha256",
                   dl.sha256_hex(emp["body"]) == dl.EMPTY_LAYER_DIGEST,
                   dl.sha256_hex(emp["body"]))
        checks.add(side, "empty-synthetic dcd-echo",
                   dl.hdr(emp, "Docker-Content-Digest") == "sha256:" + dl.EMPTY_LAYER_DIGEST,
                   dl.hdr(emp, "Docker-Content-Digest"))
    ev = ctx.write_evidence("checks.json", {"unknown_digest": unknown_digest,
                                            "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
