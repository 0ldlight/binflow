"""Demo case: GET /api/system/ping on both sides (layer L0, HTTP primitive).

Programmatic format (2026-10-29 rewrite): the ping face moved with the
platform context prefix (JFrog 7.161.26 serves /artifactory/api/*, BinFlow
/binflow/api/*; both root aliases 404), so the request rides the shared
ctx-prefix probe (mavenlib.patch_ctx) instead of a hardcoded declarative
path. Compare dimensions unchanged: status + Content-Type + body literal.
Endpoints and credentials come exclusively from the six diff env vars.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "demo-ping",
    "title": "GET /api/system/ping — status + Content-Type + body literal",
    "layer": "L0",
    "domain": None,        # no normalize domain -> raw comparison by design
    "auth": True,          # needs all six env vars -> exercises full env contract
    "timeout_s": 10,
}


def _ct(resp):
    return next((v for k, v in resp["headers"].items()
                 if k.lower() == "content-type"), "")


def run(ctx):
    mavenlib.patch_ctx(ctx)
    per = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "/api/system/ping")
        per[side] = {"status": r["status"], "content_type": _ct(r),
                     "body": r["body"].decode("utf-8", "replace")}
    a, b = per["a"], per["b"]
    diff = {k: {"a": a[k], "b": b[k]} for k in a if a[k] != b[k]}
    ev = ctx.write_evidence("ping.json", {"a": a, "b": b, "diff": diff})
    if diff:
        return {"status": "FAIL",
                "reason": "; ".join("%s a=%r b=%r" % (k, v["a"], v["b"])
                                    for k, v in sorted(diff.items())),
                "evidence": [ev],
                "requests": [{"method": "GET", "path": "/api/system/ping"}]}
    return {"status": "PASS", "reason": None,
            "evidence": [ev],
            "requests": [{"method": "GET", "path": "/api/system/ping",
                          "a": a["status"], "b": b["status"]}]}
