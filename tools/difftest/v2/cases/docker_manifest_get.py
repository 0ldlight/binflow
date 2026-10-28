"""Replays contract entries docker/remote-manifest-get-tag (#6, VERIFIED) and
docker/remote-manifest-head-digest (#7, VERIFIED).

GET by tag: 200; body bytes identical across sides; sha256(body) ==
Docker-Content-Digest; header set Accept-Ranges / Content-Disposition
(attachment; filename="manifest.json") / Docker-Distribution-Api-Version
registry/2.0 / X-Checksum-{Md5,Sha1,Sha256} / X-Artifactory-Filename /
X-Artifactory-Origin-Remote-Path; Etag == X-Checksum-Sha1 (derived rule
etag_is_sha1); Last-Modified present (value = fetch time, volatile — not
value-compared). X-Binflow-Cache / X-Request-Id supersets tolerated.
HEAD by digest: 200 + Docker-Content-Digest + Last-Modified +
X-Artifactory-Docker-Registry == repoKey (reference narrow set; GET full-set
supersets tolerated per contract #7 note).
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
    "id": "dcr-manifest-get",
    "title": "manifest GET by tag + HEAD by digest header/body faces",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    bodies = {}
    digests = {}
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        checks.add(side, "token-issued", tok is not None, "token status %s" % tr["status"])

        r = dl.get_manifest(ctx, side, dl.POSITIVE_TAG, token=tok)
        checks.add(side, "get status-200", r["status"] == 200, "got %s" % r["status"])
        if r["status"] != 200:
            checks.add(side, "get body", False, r["body"][:200].decode("utf-8", "replace"))
            continue
        bodies[side] = r["body"]
        dcd = dl.hdr(r, "Docker-Content-Digest")
        digests[side] = dcd
        checks.add(side, "dcd-equals-body-sha256", dcd == "sha256:" + dl.sha256_hex(r["body"]), dcd)
        checks.add(side, "api-version", dl.hdr(r, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(r, "Docker-Distribution-Api-Version"))
        checks.add(side, "content-disposition-literal",
                   dl.hdr(r, "Content-Disposition") == 'attachment; filename="manifest.json"',
                   dl.hdr(r, "Content-Disposition"))
        checks.add(side, "accept-ranges-bytes", dl.hdr(r, "Accept-Ranges") == "bytes",
                   dl.hdr(r, "Accept-Ranges"))
        checks.add(side, "x-filename-manifest", dl.hdr(r, "X-Artifactory-Filename") == "manifest.json",
                   dl.hdr(r, "X-Artifactory-Filename"))
        sha1 = dl.hdr(r, "X-Checksum-Sha1")
        sha256 = dl.hdr(r, "X-Checksum-Sha256")
        checks.add(side, "checksum-sha256-present", sha256 == dl.sha256_hex(r["body"]), sha256)
        checks.add(side, "etag-is-sha1-derived", dl.hdr(r, "Etag") == sha1,
                   "etag=%s sha1=%s" % (dl.hdr(r, "Etag"), sha1))
        checks.add(side, "last-modified-present", dl.hdr(r, "Last-Modified") is not None,
                   dl.hdr(r, "Last-Modified"))
        orp = dl.hdr(r, "X-Artifactory-Origin-Remote-Path") or ""
        checks.add(side, "origin-remote-path-shape",
                   bool(re.fullmatch(r"https?://[^\" ]+/v2/%s/manifests/%s" % (dl.IMAGE, dl.POSITIVE_TAG), orp)),
                   orp)
        checks.add(side, "x-checksum-md5-present", dl.hdr(r, "X-Checksum-Md5") is not None,
                   dl.hdr(r, "X-Checksum-Md5"))

        hd = dl.head_manifest(ctx, side, dcd, token=tok)
        checks.add(side, "head-by-digest status-200", hd["status"] == 200, "got %s" % hd["status"])
        checks.add(side, "head registry-header",
                   dl.hdr(hd, "X-Artifactory-Docker-Registry") == dl.REPO,
                   dl.hdr(hd, "X-Artifactory-Docker-Registry"))
        checks.add(side, "head dcd", dl.hdr(hd, "Docker-Content-Digest") == dcd,
                   dl.hdr(hd, "Docker-Content-Digest"))
        checks.add(side, "head last-modified-present", dl.hdr(hd, "Last-Modified") is not None,
                   dl.hdr(hd, "Last-Modified"))

    checks.add("ab", "manifest-body-intercompare",
               bodies.get("a") is not None and bodies.get("a") == bodies.get("b"),
               "sha a=%s b=%s" % (dl.sha256_hex(bodies["a"]) if bodies.get("a") else None,
                                  dl.sha256_hex(bodies["b"]) if bodies.get("b") else None))
    checks.add("ab", "dcd-intercompare", digests.get("a") == digests.get("b"),
               "a=%s b=%s" % (digests.get("a"), digests.get("b")))
    ev = ctx.write_evidence("checks.json", {"checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
