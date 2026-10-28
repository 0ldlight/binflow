"""Replays contract entry docker/remote-token-issue (C01b).

expect (contract): GET /v2/token with valid Basic -> 200; body keys_exact
[token, expires_in]; expires_in == 9000; token is JWT-shaped (eyJ prefix);
Docker-Distribution-Api-Version header absent; scope not validated here.
Known contract status: DIVERGENT (BinFlow historically answers with a
superset {token, access_token, expires_in: 2592000, issued_at, scope} and an
opaque hex token) — this replay reproduces the live divergence; token values
themselves are never recorded, only shape facts.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-token-issue",
    "title": "GET /v2/token valid Basic -> JWT + expires_in 9000",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    shape = {}
    for side in ("a", "b"):
        r = dl.issue_token(ctx, side)
        checks.add(side, "status-200", r["status"] == 200, "got %s" % r["status"])
        try:
            doc = dl.jbody(r)
        except Exception as e:
            checks.add(side, "body-json", False, repr(e))
            continue
        token = doc.get("token", "")
        shape[side] = {
            "keys": sorted(doc.keys()),
            "expires_in": doc.get("expires_in"),
            "token_is_jwt": token.startswith("eyJ"),
            "token_len": len(token),
        }
        checks.add(side, "keys-exact-token-expires-in",
                   sorted(doc.keys()) == ["expires_in", "token"], sorted(doc.keys()))
        checks.add(side, "expires-in-9000", doc.get("expires_in") == 9000,
                   doc.get("expires_in"))
        checks.add(side, "token-jwt-shaped", token.startswith("eyJ"),
                   "prefix=%r len=%d" % (token[:3], len(token)))
        checks.add(side, "no-api-version-header",
                   dl.hdr(r, "Docker-Distribution-Api-Version") is None,
                   dl.hdr(r, "Docker-Distribution-Api-Version"))
        checks.add(side, "content-type-present", dl.hdr(r, "Content-Type") is not None,
                   dl.hdr(r, "Content-Type"))
    inter = (shape.get("a", {}).get("keys") == shape.get("b", {}).get("keys")
             and shape.get("a", {}).get("expires_in") == shape.get("b", {}).get("expires_in"))
    checks.add("ab", "token-shape-intercompare", inter, "a=%s b=%s" % (
        shape.get("a"), shape.get("b")))
    ev = ctx.write_evidence("token-shape.json", {"shape": shape,  # no token values
                                                 "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
