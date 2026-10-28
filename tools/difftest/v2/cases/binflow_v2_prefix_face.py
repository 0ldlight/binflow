"""wave2 arm 4 — /binflow/v2/ prefix mount topology (L042 unknown, B-only face).

Wave-1 live mapping: B serves the docker v2 face at the BARE HOST ROOT
(/v2/*) and answers /binflow/v2/ with 404, while A has no /binflow context
at all (its REST context is /artifactory). This case pins the B-side
topology assertion (docker face is NOT mounted under the /binflow prefix):
  - GET /binflow/v2/                 -> 404 (not a 401 docker challenge)
  - anonymous GET /binflow/v2/       -> 404 (no WWW-Authenticate challenge)
  - GET /binflow/v2/<repo>/<image>/manifests/t2 -> 404 (real manifest NOT served)
A-side same paths are recorded verbatim as observations (A has no such
face; asserting A's incidental 404 shape is out of scope) and do not gate
the verdict. PASS iff all three B assertions hold.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "binflow-v2-prefix-face",
    "title": "B topology: docker v2 face NOT mounted under /binflow/v2/",
    "layer": "L1",
    "domain": "mount-topology",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    facts = {}

    # B assertions (the contract face of this case)
    r = ctx.http("b", "GET", "/binflow/v2/")
    facts["b_prefixed_ping"] = dl.resp_summary(r)
    checks.add("b", "prefixed-ping-404", r["status"] == 404,
               "got %s body=%s" % (r["status"], r["body"][:120].decode("utf-8", "replace")))
    checks.add("b", "prefixed-ping-no-docker-challenge",
               dl.hdr(r, "WWW-Authenticate") is None,
               dl.hdr(r, "WWW-Authenticate"))

    anon = dl.anon_http(ctx, "b", "GET", "/binflow/v2/")
    facts["b_prefixed_ping_anon"] = dl.resp_summary(anon)
    checks.add("b", "prefixed-ping-anon-404", anon["status"] == 404,
               "got %s" % anon["status"])
    checks.add("b", "prefixed-ping-anon-no-challenge",
               dl.hdr(anon, "WWW-Authenticate") is None,
               dl.hdr(anon, "WWW-Authenticate"))

    r = ctx.http("b", "GET", "/binflow/v2/%s/%s/manifests/%s" % (
        dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
        headers={"Accept": dl.ACCEPT_MANIFEST})
    facts["b_prefixed_manifest"] = dl.resp_summary(r)
    checks.add("b", "prefixed-manifest-404-not-served", r["status"] == 404,
               "got %s" % r["status"])

    # A-side observations (record-only, non-gating)
    for name, path in (("a_prefixed_ping", "/binflow/v2/"),):
        r = ctx.http("a", "GET", path)
        facts[name] = dl.resp_summary(r)

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
