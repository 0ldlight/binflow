"""wave2 arm 3 — star If-None-Match legs (L042 unknown: "star INM 腿").

RFC 7232 semantics under test on the docker face: If-None-Match: * on
GET/HEAD matches whenever the resource exists -> 304; on a missing resource
the condition is false -> normal 200/404. Arms (each dual-send, intercompared):
  - GET  manifest t2 (exists)          with INM: *  -> expect 304-family
  - HEAD manifest t2 (exists)          with INM: *  -> expect 304-family
  - GET  manifest <run-unique unknown> with INM: *  -> expect non-304
  - GET  blob <config digest>          with INM: *  -> expect 304-family
The blob digest is discovered live from the manifest (no hardcoded digest).
Statuses and 304 header faces (Etag/CT absence on 304) recorded verbatim;
PASS iff every arm intercompares equal on status. Absolute values are
recorded for the contract author — no expect block pins them yet.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

UNKNOWN_TAG = "star-unknown-" + dl.NONCE

CASE = {
    "id": "dcr-star-inm",
    "title": "If-None-Match: * on manifest/blob GET+HEAD (existing vs missing)",
    "layer": "L3",
    "domain": "docker-remote",
    "auth": True,
}


def inm_get(ctx, side, method, path):
    return ctx.http(side, method, path,
                    headers={"If-None-Match": "*",
                             "Accept": dl.ACCEPT_NEGOTIATE})


def run(ctx):
    checks = dl.Checks()
    facts = {"unknown_tag": UNKNOWN_TAG}

    # discover a live blob digest from the fixture manifest (robust, no literals)
    m = ctx.http("a", "GET", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                 headers={"Accept": dl.ACCEPT_MANIFEST})
    blob_digest = None
    try:
        blob_digest = dl.jbody(m)["config"]["digest"]
    except Exception as e:
        checks.add("a", "fixture-manifest-for-blob-digest", False, repr(e))
    facts["blob_digest"] = blob_digest

    arms = [
        ("get-manifest-exists", "GET",
         "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG)),
        ("head-manifest-exists", "HEAD",
         "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG)),
        ("get-manifest-missing", "GET",
         "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, UNKNOWN_TAG)),
    ]
    if blob_digest:
        arms.append(("get-blob-exists", "GET",
                     "/v2/%s/%s/blobs/%s" % (dl.REPO, dl.IMAGE, blob_digest)))

    for name, method, path in arms:
        per = {}
        for side in ("a", "b"):
            r = inm_get(ctx, side, method, path)
            per[side] = {"status": r["status"],
                         "etag": dl.hdr(r, "Etag"),
                         "content_type": dl.hdr(r, "Content-Type"),
                         "body_len": len(r["body"])}
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
        checks.add("ab", "%s-304-header-face-intercompare" % name,
                   (per["a"]["status"] == 304) == (per["b"]["status"] == 304),
                   "a304=%s b304=%s" % (per["a"]["status"] == 304,
                                        per["b"]["status"] == 304))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
