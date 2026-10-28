"""Replays contract entry docker/remote-manifest-conditional-get (#8) — arm
subset M1/M3/M4/M7 within the fresh window (retrievalCachePeriodSecs default
21600s on both fixtures).

M1 GET + If-None-Match "<etag>" quoted match -> 304, bare headers
(api-version present, empty body).
M3 GET + If-Modified-Since <Last-Modified> -> 304.
M4 GET + If-None-Match "<non-matching>" -> 200 full body.
M7 HEAD + If-None-Match "<etag>" quoted match -> 200 (HEAD never 304).
Unquoted-INM arms are not replayed here (D1 history); the wave report
records the subset scope.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-manifest-conditional",
    "title": "manifest conditional arms M1/M3/M4/M7 (fresh window)",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    base_sha = {}
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        base = dl.get_manifest(ctx, side, dl.POSITIVE_TAG, token=tok)
        if base["status"] != 200:
            checks.add(side, "baseline-200", False, "status %s" % base["status"])
            continue
        etag = dl.hdr(base, "Etag")
        lm = dl.hdr(base, "Last-Modified")
        base_sha[side] = dl.sha256_hex(base["body"])
        checks.add(side, "baseline-etag-present", bool(etag), etag)
        quoted = '"%s"' % etag.strip('"')
        bad_etag = '"difftest-nonexistent-%s"' % dl.NONCE

        m1 = ctx.http(side, "GET", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                      headers={"Authorization": "Bearer " + tok, "Accept": dl.ACCEPT_MANIFEST,
                               "If-None-Match": quoted})
        checks.add(side, "m1-quoted-inm status-304", m1["status"] == 304, "got %s" % m1["status"])
        checks.add(side, "m1 api-version", dl.hdr(m1, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(m1, "Docker-Distribution-Api-Version"))
        checks.add(side, "m1 empty-body", len(m1["body"]) == 0, "len=%d" % len(m1["body"]))

        m3 = ctx.http(side, "GET", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                      headers={"Authorization": "Bearer " + tok, "Accept": dl.ACCEPT_MANIFEST,
                               "If-Modified-Since": lm or ""})
        checks.add(side, "m3-ims status-304", m3["status"] == 304, "got %s" % m3["status"])

        m4 = ctx.http(side, "GET", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                      headers={"Authorization": "Bearer " + tok, "Accept": dl.ACCEPT_MANIFEST,
                               "If-None-Match": bad_etag})
        checks.add(side, "m4-nonmatch-inm status-200", m4["status"] == 200, "got %s" % m4["status"])
        checks.add(side, "m4 full-body", dl.sha256_hex(m4["body"]) == base_sha[side],
                   "sha=%s" % dl.sha256_hex(m4["body"])[:16])

        m7 = ctx.http(side, "HEAD", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                      headers={"Authorization": "Bearer " + tok, "Accept": dl.ACCEPT_MANIFEST,
                               "If-None-Match": quoted})
        checks.add(side, "m7-head-inm status-200", m7["status"] == 200, "got %s" % m7["status"])

    checks.add("ab", "baseline-sha-intercompare",
               base_sha.get("a") is not None and base_sha.get("a") == base_sha.get("b"),
               "a=%s b=%s" % (base_sha.get("a"), base_sha.get("b")))
    ev = ctx.write_evidence("checks.json", {"checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
