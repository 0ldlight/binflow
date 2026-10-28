"""Replays contract entry docker/remote-v2-ping-anonymous (L4 auth).

expect: GET /v2/ anonymous -> 401; Docker-Distribution-Api-Version
registry/2.0; Content-Type application/json;charset=ISO-8859-1; compact
UNAUTHORIZED body; WWW-Authenticate 'Bearer realm="<own-root>/v2/token",
service="<host:port>"' with service = request Host echo
(derived rule service_equals_request_host).
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
    "id": "dcr-ping-anonymous",
    "title": "GET /v2/ anonymous -> 401 challenge shape",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}

BODY_LITERAL = '{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}'


def run(ctx):
    checks = dl.Checks()
    for side in ("a", "b"):
        r = dl.anon_http(ctx, side, "GET", "/v2/")
        checks.add(side, "status-401", r["status"] == 401, "got %s" % r["status"])
        checks.add(side, "api-version-registry-2.0",
                   dl.hdr(r, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(r, "Docker-Distribution-Api-Version"))
        checks.add(side, "content-type-charset",
                   dl.hdr(r, "Content-Type") == "application/json;charset=ISO-8859-1",
                   dl.hdr(r, "Content-Type"))
        www = dl.hdr(r, "WWW-Authenticate") or ""
        m = re.fullmatch(r'Bearer realm="([^"]+)/v2/token",service="([^"]+)"', www)
        checks.add(side, "www-authenticate-bearer-shape", bool(m), www)
        if m:
            checks.add(side, "realm-own-root",
                       m.group(1) == ctx.sides[side]["base"], m.group(1))
            checks.add(side, "service-host-echo",
                       m.group(2) == dl.hostport(ctx.sides[side]), m.group(2))
        body = r["body"].decode("utf-8", "replace")
        checks.add(side, "compact-unauthorized-body", body.strip() == BODY_LITERAL,
                   "raw_len=%d head=%s" % (len(body), body[:80]))
        checks.add(side, "trailing-newline-note", True,
                   "endswith_newline=%s" % body.endswith("\n"))
    ev = ctx.write_evidence("checks.json", {"arm": "GET /v2/ anonymous",
                                            "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
