"""wave5 arm 4 — tags/list non-live view on the remote repo (wave1-rerun13b).

wave1-rerun13b exposed: A's tags/list on a REMOTE repo serves a stale cached
view (upstream had 5 tags, A kept answering 3 for >=54 min across requests)
while B aggregates live. This case instruments the window with a controlled
cadence:

  W  warm        : tags/list BEFORE the push — deterministically fills A's
                   view with the pre-push tag set
  U  upstream    : copy the fixture manifest onto run-unique tag TNEW
  t0 immediate   : tags/list + manifest GET (Accept schema2) on both sides
  t60/t120/t180  : the same four reads every 60s

Per sample per side: tags_has_new (TNEW membership), tags_count,
manifest_status. The grid distinguishes the combined non-live picture:
A tags/list stale AND manifest negative/positive cache state vs B live
aggregate + manifest state. Verdict-driving checks: warm 200 both;
upstream push 201; t0 manifest fetch 200 both (pull-through liveness);
B t0 tags membership (B live-aggregate assertion); t0 and t180 A/B tags
membership intercompare (expected DIVERGENT — that IS the candidate).
Upstream residue: one run-unique tag (DELETE 405 — GC ledger).
"""
import importlib.util
import json
import os
import time
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

UPSTREAM = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")
TAG_NEW = "w5t" + dl.NONCE

CASE = {
    "id": "dcr-tags-list-liveness",
    "title": "remote tags/list non-live view: warm->push->t0/60/120/180 cadence",
    "layer": "L9",
    "domain": "docker-remote",
    "auth": True,
    "timeout_s": 60.0,
}


def upstream_request(method, path, body=None, content_type=None, timeout=15.0):
    req = urllib.request.Request(UPSTREAM + path, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    else:
        req.add_header("Accept", dl.ACCEPT_MANIFEST)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def sample(ctx, side):
    tags = ctx.http(side, "GET",
                    "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE))
    man = ctx.http(side, "GET",
                   "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, TAG_NEW),
                   headers={"Accept": dl.ACCEPT_MANIFEST})
    view = {"manifest_status": man["status"],
            "manifest_ct": dl.hdr(man, "Content-Type"), "tags_status": tags["status"],
            "tags_has_new": None, "tags_count": None}
    try:
        doc = dl.jbody(tags)
        tlist = doc.get("tags") or []
        view["tags_has_new"] = TAG_NEW in tlist
        view["tags_count"] = len(tlist)
    except Exception:
        pass
    return view


def run(ctx):
    if not UPSTREAM:
        return {"status": "BLOCKED",
                "reason": "missing env DIFFTEST_UPSTREAM_BASE (fixture upstream)"}
    checks = dl.Checks()
    facts = {"tag_new": TAG_NEW, "upstream": dl.hostport({"base": UPSTREAM})}

    # ---- W: warm the tags/list view with the pre-push set
    warm = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE))
        try:
            warm[side] = {"status": r["status"], "tags": dl.jbody(r).get("tags")}
        except Exception:
            warm[side] = {"status": r["status"], "tags": None}
        checks.add(side, "warm-tags-200", r["status"] == 200, "got %s" % r["status"])
    facts["warm"] = {s: {"status": warm[s]["status"],
                         "count": len(warm[s]["tags"] or [])} for s in ("a", "b")}

    # ---- U: push the new tag upstream (copy fixture manifest bytes)
    status, src = upstream_request(
        "GET", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], dl.POSITIVE_TAG))
    checks.add("ab", "upstream-fixture-fetch-200", status == 200, "got %s" % status)
    if status != 200:
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out
    put_status, put_body = upstream_request(
        "PUT", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], TAG_NEW),
        body=src, content_type=dl.ACCEPT_MANIFEST)
    checks.add("ab", "upstream-push-new-tag-2xx", put_status in (201, 202),
               "got %s body=%s" % (put_status, put_body[:120].decode("utf-8", "replace")))
    if checks.result()["status"] != "PASS":
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # ---- t0 / t60 / t120 / t180 grid
    grid = {}
    for label, delay in (("t0", 0), ("t60", 60), ("t120", 60), ("t180", 60)):
        if delay:
            time.sleep(delay)
        grid[label] = {side: sample(ctx, side) for side in ("a", "b")}
        facts[label] = grid[label]
        for side in ("a", "b"):
            checks.add(side, "%s-manifest-200" % label,
                       grid[label][side]["manifest_status"] == 200,
                       "got %s (pull-through liveness leg)" % grid[label][side]["manifest_status"])
        checks.add("b", "%s-b-tags-live" % label,
                   grid[label]["b"]["tags_has_new"] is True,
                   "b tags_has_new=%s count=%s" % (
                       grid[label]["b"]["tags_has_new"], grid[label]["b"]["tags_count"]))
        checks.add("ab", "%s-tags-membership-intercompare" % label,
                   grid[label]["a"]["tags_has_new"] == grid[label]["b"]["tags_has_new"],
                   "a has_new=%s b has_new=%s (a count=%s b count=%s)" % (
                       grid[label]["a"]["tags_has_new"], grid[label]["b"]["tags_has_new"],
                       grid[label]["a"]["tags_count"], grid[label]["b"]["tags_count"]))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
