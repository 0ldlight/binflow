"""Replays contract entries docker/remote-token-bad-credentials (C02, SAME)
and docker/remote-token-anonymous (C03, DIVERGENT).

Bad Basic on /v2/token -> 401 pretty-json errors[0] = {status: 401,
message: "Bad Credentials"} (both sides expected).
Anonymous /v2/token -> reference 401 "Authentication is required"; BinFlow
historically issues an anonymous token 200 — replay records the live value.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-token-reject-arms",
    "title": "/v2/token bad Basic 401 + anonymous arm",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    anon_shape = {}
    for side in ("a", "b"):
        bad = base64_bad()
        r = ctx.http(side, "GET", token_query(ctx, side),
                     headers={"Authorization": "Basic " + bad})
        checks.add(side, "bad-basic status-401", r["status"] == 401, "got %s" % r["status"])
        try:
            doc = dl.jbody(r)
            e0 = (doc.get("errors") or [{}])[0]
            checks.add(side, "bad-basic message", e0.get("message") == "Bad Credentials",
                       e0.get("message"))
            checks.add(side, "bad-basic e0-keys",
                       sorted(e0.keys()) == ["message", "status"], sorted(e0.keys()))
        except Exception as e:
            checks.add(side, "bad-basic body-json", False, repr(e))

        anon = dl.anon_http(ctx, side, "GET", token_query(ctx, side))
        try:
            doc = dl.jbody(anon)
            e0 = (doc.get("errors") or [{}])[0]
            msg = e0.get("message")
        except Exception:
            doc, e0, msg = None, {}, None
        anon_shape[side] = {"status": anon["status"], "message": msg,
                            "keys": sorted(doc.keys()) if isinstance(doc, dict) else None}
        checks.add(side, "anonymous status-401", anon["status"] == 401,
                   "got %s" % anon["status"])
        checks.add(side, "anonymous message-auth-required",
                   msg == "Authentication is required", msg)
    checks.add("ab", "anonymous-shape-intercompare",
               anon_shape.get("a") == anon_shape.get("b"),
               "a=%s b=%s" % (anon_shape.get("a"), anon_shape.get("b")))
    ev = ctx.write_evidence("checks.json", {"anonymous_shape": anon_shape,
                                            "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out


def token_query(ctx, side):
    return "/v2/token?service=%s&scope=repository:%s/%s:pull&account=%s" % (
        dl.hostport(ctx.sides[side]), dl.REPO, dl.IMAGE, ctx.sides[side]["user"])


def base64_bad():
    import base64
    return base64.b64encode(
        ("difftest-bad-user-%s:difftest-bad-pass-%s" % (dl.NONCE, dl.NONCE)).encode()).decode()
