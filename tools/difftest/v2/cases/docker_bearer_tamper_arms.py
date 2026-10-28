"""wave2 arm 6 — forged/tampered bearer token arms (L042 P3/P4b REST-ized subset).

True expiry/revocation needs reference-side token-state mutation (still out
of reach, noted in the wave report); the REST-izable forgery family is:
  t1 tampered-suffix : valid token, last 4 chars replaced
  t2 truncated-half  : valid token cut in half
  t3 alg-none-jwt    : JWT-shaped header {"alg":"none"} + forged signature
  t4 empty-bearer    : "Bearer " with empty credential
  t5 wrong-action    : token scoped ...:push only, presented on a pull
Each arm: fresh per-side token issue (200 asserted once), dual-send, then
intercompare status AND the JSON errors[0] shape (message string included —
a split message is a shape divergence candidate, not silently tolerated).
PASS iff every arm intercompares equal. Token values flow into request
headers only; evidence carries statuses/shapes, never the tokens.
"""
import base64
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

MANIFEST_PATH = "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG)
ALG_NONE_TOKEN = ("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0."
                  "eyJzdWIiOiJkaWZmdGVzdCJ9."
                  "Zm9yZ2VkLXNpZ25hdHVyZQ")

CASE = {
    "id": "dcr-bearer-tamper-arms",
    "title": "forged/tampered bearer arms: 401/403 shape intercomparison",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}


def bearer_manifest(ctx, side, token):
    return ctx.http(side, "GET", MANIFEST_PATH,
                    headers={"Authorization": "Bearer " + token,
                             "Accept": dl.ACCEPT_MANIFEST})


def errors0_shape(resp):
    try:
        doc = dl.jbody(resp)
    except Exception:
        return {"json": False}
    e0 = (doc.get("errors") or [{}])[0] if isinstance(doc, dict) else {}
    return {"keys": sorted(e0.keys()), "status": e0.get("status"),
            "message": e0.get("message")}


def run(ctx):
    checks = dl.Checks()
    facts = {}

    tokens = {}
    for side in ("a", "b"):
        r = dl.issue_token(ctx, side)
        checks.add(side, "valid-token-issue-200", r["status"] == 200,
                   "got %s" % r["status"])
        try:
            tokens[side] = dl.jbody(r)["token"]
        except Exception as e:
            checks.add(side, "valid-token-parse", False, repr(e))
    if checks.result()["status"] != "PASS":
        ev = ctx.write_evidence("checks.json", {"checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    push_scoped = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET",
                     "/v2/token?service=%s&scope=repository:%s/%s:push&account=%s" % (
                         dl.hostport(ctx.sides[side]), dl.REPO, dl.IMAGE,
                         ctx.sides[side]["user"]))
        checks.add(side, "push-scope-token-issue-200", r["status"] == 200,
                   "got %s" % r["status"])
        try:
            push_scoped[side] = dl.jbody(r)["token"]
        except Exception as e:
            checks.add(side, "push-scope-token-parse", False, repr(e))

    arms = [
        ("t1-tampered-suffix", tokens["a"][:-4] + "0000", tokens["b"][:-4] + "0000"),
        ("t2-truncated-half", tokens["a"][:len(tokens["a"]) // 2],
         tokens["b"][:len(tokens["b"]) // 2]),
        ("t3-alg-none-jwt", ALG_NONE_TOKEN, ALG_NONE_TOKEN),
        ("t4-empty-bearer", "", ""),
    ]
    if all(push_scoped.get(s) for s in ("a", "b")):
        arms.append(("t5-push-scope-on-pull", push_scoped["a"], push_scoped["b"]))

    for name, tok_a, tok_b in arms:
        per = {}
        for side, tok in (("a", tok_a), ("b", tok_b)):
            r = bearer_manifest(ctx, side, tok)
            per[side] = {"status": r["status"], "shape": errors0_shape(r),
                         "www_authenticate": dl.hdr(r, "WWW-Authenticate")}
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
        checks.add("ab", "%s-shape-intercompare" % name,
                   per["a"]["shape"] == per["b"]["shape"],
                   "a=%s b=%s" % (per["a"]["shape"], per["b"]["shape"]))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
