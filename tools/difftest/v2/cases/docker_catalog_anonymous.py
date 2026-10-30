"""wave1-arms supplement — anonymous catalog/tags faces (arm-2 "对匿名" leg).

L042 wave-1-arms dispatch splits the catalog degradation arm into three
states: remote-repo view, local-repo view, and ANONYMOUS access. The
committed dcr-catalog-empty-degrade case covers the first two (authed);
this case pins the anonymous leg on the remote-repo fixture:
  - anonymous GET /v2/_catalog                    (registry level)
  - anonymous GET /v2/<remote-repo>/<image>/tags/list (repo level)
No credentials travel on these legs (anon_http; 401 challenge is a legal
answer and participates in intercomparison like any status). Bodies and
WWW-Authenticate shapes are recorded verbatim; PASS iff statuses
intercompare equal per leg. A split is a new-divergence candidate for the
wave report — classification is not this case's job.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-catalog-anonymous",
    "title": "anonymous /v2/_catalog + remote tags/list: challenge vs open",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def shape_of(resp):
    try:
        doc = dl.jbody(resp)
    except Exception:
        return {"json": False}
    if isinstance(doc, dict):
        return {"json": True, "keys": sorted(doc.keys())}
    return {"json": True, "type": type(doc).__name__}


def run(ctx):
    checks = dl.Checks()
    facts = {}

    legs = [
        ("anon-catalog", "GET", "/v2/_catalog", None),
        ("anon-remote-tags", "GET",
         "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE), dl.ACCEPT_MANIFEST),
    ]
    for name, method, path, accept in legs:
        headers = {"Accept": accept} if accept else None
        per = {}
        for side in ("a", "b"):
            r = dl.anon_http(ctx, side, method, path, headers=headers)
            per[side] = {
                "status": r["status"],
                "shape": shape_of(r),
                "www_authenticate": dl.hdr(r, "WWW-Authenticate"),
                "content_type": dl.hdr(r, "Content-Type"),
                "body_head": r["body"][:200].decode("utf-8", "replace"),
            }
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s (a body=%r b body=%r)" % (
                       per["a"]["status"], per["b"]["status"],
                       per["a"]["body_head"][:80], per["b"]["body_head"][:80]))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
