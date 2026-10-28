"""Replays contract entry docker/remote-v2-ping-bad-credentials — P1/P2 arms.

P1: GET /v2/ + Basic <bad> -> 401, WWW-Authenticate 'Basic realm="Artifactory
Realm"', charset Content-Type, pretty-json errors[0] = {status: 401,
message: "Bad Credentials"} with code/detail absent (pretty JSON dialect
difference Jackson vs Go Encoder is accepted fingerprint N3 — compared
structurally here).
P2: GET /v2/ + Bearer <unknown> -> 401, errors[0].message = "Props
Authentication Token not found" (credential-class typing).
P3/P4b (expired/revoked bearer) need reference-side token state mutation —
not covered this wave, see the wave report.
"""
import base64
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-ping-reject-arms",
    "title": "GET /v2/ bad Basic (P1) + unknown Bearer (P2) rejection shape",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}


def errors0_shape_ok(body_bytes, want_message, checks, side, arm):
    try:
        doc = dl.jbody({"body": body_bytes})
    except Exception as e:
        checks.add(side, arm + " body-json", False, repr(e))
        return
    checks.add(side, arm + " keys-exact-errors", list(doc.keys()) == ["errors"], list(doc.keys()))
    e0 = doc.get("errors", [{}])[0] if doc.get("errors") else {}
    checks.add(side, arm + " e0-status", e0.get("status") == 401, e0.get("status"))
    checks.add(side, arm + " e0-message", e0.get("message") == want_message,
               e0.get("message"))
    extra = sorted(set(e0.keys()) - {"status", "message"})
    checks.add(side, arm + " e0-no-code-detail", extra == [], extra)


def run(ctx):
    checks = dl.Checks()
    # Run-unique fake identity: Artifactory trips recurrent-failure blocking
    # ("blocked due to recurrent request failures") on repeated bad Basic for
    # the same username across runs — 401 arms must not accumulate strikes.
    bad_basic = base64.b64encode(
        ("difftest-bad-user-%s:difftest-bad-pass-%s" % (dl.NONCE, dl.NONCE)).encode()).decode()
    unknown_bearer = dl.NONCE + "0" * 32
    arms = [
        ("p1-basic-bad", "Basic " + bad_basic, "Bad Credentials"),
        ("p2-bearer-unknown", "Bearer " + unknown_bearer, "Props Authentication Token not found"),
    ]
    for side in ("a", "b"):
        for arm, auth_value, want_msg in arms:
            r = ctx.http(side, "GET", "/v2/", headers={"Authorization": auth_value})
            checks.add(side, arm + " status-401", r["status"] == 401, "got %s" % r["status"])
            www = dl.hdr(r, "WWW-Authenticate") or ""
            if arm == "p1-basic-bad":
                checks.add(side, arm + " basic-realm", www == 'Basic realm="Artifactory Realm"', www)
            checks.add(side, arm + " content-type-charset",
                       dl.hdr(r, "Content-Type") == "application/json;charset=ISO-8859-1",
                       dl.hdr(r, "Content-Type"))
            errors0_shape_ok(r["body"], want_msg, checks, side, arm)
    ev = ctx.write_evidence("checks.json", {"arms": [a[0] for a in arms],
                                            "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
