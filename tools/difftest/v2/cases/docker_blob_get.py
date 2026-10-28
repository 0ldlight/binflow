"""Replays contract entries docker/remote-blob-get (#10, VERIFIED) and
docker/remote-blob-get-range (#11, VERIFIED).

Blob GET by digest (config + layer from the manifest chain): 200;
sha256(body) == digest; Docker-Content-Digest == digest; header set
X-Checksum-{Md5,Sha1,Sha256} (sha256 == digest), Accept-Ranges bytes,
Last-Modified present, Content-Disposition / X-Artifactory-Filename with
sha256__<64hex> naming, Etag == X-Checksum-Sha1, Origin-Remote-Path blob
shape. Range bytes=0-63 on the 64-byte layer: 206, Content-Range
bytes 0-63/<total>, 64-byte body, api-version header, checksums + Etag +
Accept-Ranges present.
"""
import importlib.util
import os
import re

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-blob-get",
    "title": "blob GET (config/layer) + Range 206 header/body faces",
    "layer": "L6",
    "domain": "docker-remote",
    "auth": True,
}

SHA256__PATTERN = re.compile(r'^attachment; filename="sha256__[0-9a-f]{64}"$')
FILENAME_PATTERN = re.compile(r"^sha256__[0-9a-f]{64}$")


def blob_face(checks, side, arm, r, digest, total=None):
    checks.add(side, arm + " status", r["status"] == (206 if total else 200),
               "got %s" % r["status"])
    dcd = dl.hdr(r, "Docker-Content-Digest")
    checks.add(side, arm + " dcd-equals-digest", dcd == "sha256:" + digest, dcd)
    checks.add(side, arm + " body-sha256", dl.sha256_hex(r["body"]) == digest,
               "sha=%s len=%d" % (dl.sha256_hex(r["body"])[:16], len(r["body"])))
    sha1 = dl.hdr(r, "X-Checksum-Sha1")
    checks.add(side, arm + " etag-is-sha1", dl.hdr(r, "Etag") == sha1,
               "etag=%s sha1=%s" % (dl.hdr(r, "Etag"), sha1))
    checks.add(side, arm + " checksum-sha256", dl.hdr(r, "X-Checksum-Sha256") == digest,
               dl.hdr(r, "X-Checksum-Sha256"))
    checks.add(side, arm + " checksum-md5-present", dl.hdr(r, "X-Checksum-Md5") is not None,
               dl.hdr(r, "X-Checksum-Md5"))
    checks.add(side, arm + " accept-ranges-bytes", dl.hdr(r, "Accept-Ranges") == "bytes",
               dl.hdr(r, "Accept-Ranges"))
    checks.add(side, arm + " last-modified-present", dl.hdr(r, "Last-Modified") is not None,
               dl.hdr(r, "Last-Modified"))
    cd = dl.hdr(r, "Content-Disposition") or ""
    checks.add(side, arm + " content-disposition-sha256__", bool(SHA256__PATTERN.match(cd)), cd)
    fn = dl.hdr(r, "X-Artifactory-Filename") or ""
    checks.add(side, arm + " x-filename-sha256__", bool(FILENAME_PATTERN.match(fn)), fn)
    orp = dl.hdr(r, "X-Artifactory-Origin-Remote-Path") or ""
    checks.add(side, arm + " origin-remote-path-shape",
               bool(re.fullmatch(r"https?://[^\" ]+/v2/%s/blobs/sha256:%s" % (dl.IMAGE, digest), orp)), orp)


def run(ctx):
    checks = dl.Checks()
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        mf = dl.get_manifest(ctx, side, dl.POSITIVE_TAG, token=tok)
        if mf["status"] != 200:
            checks.add(side, "manifest-200", False, "status %s" % mf["status"])
            continue
        doc = dl.jbody(mf)
        cfg_dig = doc["config"]["digest"].split(":", 1)[1]
        layer_dig = doc["layers"][0]["digest"].split(":", 1)[1]
        layer_size = doc["layers"][0]["size"]

        r_cfg = dl.get_blob(ctx, side, cfg_dig, token=tok)
        blob_face(checks, side, "config", r_cfg, cfg_dig)

        r_layer = dl.get_blob(ctx, side, layer_dig, token=tok)
        blob_face(checks, side, "layer", r_layer, layer_dig)

        rr = dl.get_blob(ctx, side, layer_dig, token=tok,
                         extra_headers={"Range": "bytes=0-63"})
        checks.add(side, "range status-206", rr["status"] == 206, "got %s" % rr["status"])
        checks.add(side, "range content-range",
                   dl.hdr(rr, "Content-Range") == "bytes 0-63/%d" % layer_size,
                   dl.hdr(rr, "Content-Range"))
        checks.add(side, "range body-64-bytes", len(rr["body"]) == 64, "len=%d" % len(rr["body"]))
        checks.add(side, "range api-version",
                   dl.hdr(rr, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(rr, "Docker-Distribution-Api-Version"))
        checks.add(side, "range etag-present", dl.hdr(rr, "Etag") is not None, dl.hdr(rr, "Etag"))
        checks.add(side, "range dcd",
                   dl.hdr(rr, "Docker-Content-Digest") == "sha256:" + layer_dig,
                   dl.hdr(rr, "Docker-Content-Digest"))
    ev = ctx.write_evidence("checks.json", {"checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
