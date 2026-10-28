"""wave2 arm 1 — negative-cache TTL window (404 then re-verify after upstream gains the tag).

L042 unknown: "negative-cache TTL 窗口臂（#9/#16 上游计数判定）" and the
revalidation-serve arm. REST-ized here via an observable upstream transition:
  1. GET manifest <run-unique tag> on both sides -> 404 (upstream has no such
     tag; this plants a negative-cache row, Artifactory missedTTL ~1800s).
  2. The tag IS pushed to the upstream relay (anonymous PUT, byte-identical
     schema2 manifest copied from the fixture tag).
  3. GET the same tag again immediately (well inside any TTL) -> the reference
     (negative cache) must still answer 404 without consulting the upstream;
     a side without negative caching (or revalidating per request) answers 200.

Four-state: PASS iff step-1 404 on both sides, upstream push 201, and step-3
statuses are EQUAL on both sides; the actual step-3 pair (404/404 or
404/200) is recorded verbatim in evidence either way — a split pair is a
new-divergence candidate, classification is not this case's job.

Upstream base enters via env DIFFTEST_UPSTREAM_BASE (fixture topology, not an
A/B endpoint; BLOCKED when absent). Upstream residue: one run-unique manifest
tag per run (upstream DELETE is 405 — listed in the wave cleanup ledger).
"""
import importlib.util
import os
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

UPSTREAM = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")
TAG = "nc" + dl.NONCE

CASE = {
    "id": "dcr-negative-cache-ttl",
    "title": "negative-cache window: 404 stays cached after upstream gains the tag",
    "layer": "L9",
    "domain": "docker-remote",
    "auth": True,
}


def upstream_request(method, path, body=None, content_type=None, timeout=15.0):
    req = urllib.request.Request(UPSTREAM + path, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    else:
        # the relay negotiates strictly; a schema2 fetch must ask for schema2
        # or the answer's representation won't match the PUT's Content-Type
        req.add_header("Accept", dl.ACCEPT_MANIFEST)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def fetch_manifest(ctx, side, tag):
    return ctx.http(side, "GET",
                    "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, tag),
                    headers={"Accept": dl.ACCEPT_MANIFEST})


def run(ctx):
    if not UPSTREAM:
        return {"status": "BLOCKED",
                "reason": "missing env DIFFTEST_UPSTREAM_BASE (fixture upstream)"}
    checks = dl.Checks()
    facts = {"tag": TAG, "upstream": dl.hostport({"base": UPSTREAM})}

    # step 1: plant the negative-cache row (tag unknown everywhere)
    first = {}
    for side in ("a", "b"):
        r = fetch_manifest(ctx, side, TAG)
        first[side] = r
        checks.add(side, "step1-unknown-tag-404", r["status"] == 404,
                   "got %s body=%s" % (r["status"], r["body"][:120].decode("utf-8", "replace")))
    checks.add("ab", "step1-status-intercompare",
               first["a"]["status"] == first["b"]["status"],
               "a=%s b=%s" % (first["a"]["status"], first["b"]["status"]))

    # step 2: upstream gains the tag (copy the fixture manifest verbatim)
    status, src = upstream_request(
        "GET", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], dl.POSITIVE_TAG))
    checks.add("ab", "step2-upstream-fixture-fetch-200", status == 200,
               "got %s" % status)
    if status == 200:
        put_status, put_body = upstream_request(
            "PUT", "/v2/%s/smoke/manifests/%s" % (
                dl.IMAGE.rsplit("/", 1)[0], TAG),
            body=src, content_type=dl.ACCEPT_MANIFEST)
        checks.add("ab", "step2-upstream-tag-push-201", put_status in (201, 202),
                   "got %s body=%s" % (put_status, put_body[:120].decode("utf-8", "replace")))
        facts["upstream_push_status"] = put_status
    if checks.result()["status"] != "PASS":
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # step 3: immediate re-request — negative cache must hold (A) vs B
    second = {}
    for side in ("a", "b"):
        r = fetch_manifest(ctx, side, TAG)
        second[side] = {"status": r["status"],
                        "body_head": r["body"][:200].decode("utf-8", "replace"),
                        "content_type": dl.hdr(r, "Content-Type")}
    facts["second_get"] = second
    checks.add("ab", "step3-status-intercompare",
               second["a"]["status"] == second["b"]["status"],
               "a=%s b=%s (a body=%r b body=%r)" % (
                   second["a"]["status"], second["b"]["status"],
                   second["a"]["body_head"][:80], second["b"]["body_head"][:80]))
    # side facts recorded regardless of verdict (classification upstream)
    checks.add("a", "step3-a-record-negative-cache-held",
               second["a"]["status"] in (200, 404), second["a"]["status"])
    checks.add("b", "step3-b-record", second["b"]["status"] in (200, 404),
               second["b"]["status"])
    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
