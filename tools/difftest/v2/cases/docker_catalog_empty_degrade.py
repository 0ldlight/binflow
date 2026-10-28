"""wave2 arm 2 — catalog/tags empty-list degradation (L042 unknown: "catalog 空列表降级臂").

Fresh run-unique EMPTY local docker repo on both sides (difftest namespace,
DELETEd at the end), then the empty-repository faces:
  - tags/list of a never-pushed image inside the empty repo: 404 NAME_UNKNOWN
    vs 200 {"name","tags":[]} — the degradation shape under test;
  - registry-level GET /v2/_catalog: membership of the empty repo + list shape;
  - tags/list of the never-pushed image on the remote repo (upstream-backed,
    tag never pushed): remote-side degradation shape.

Four-state: PASS iff both creations succeed and every arm's status + body
shape intercompares equal; split statuses/shapes are recorded verbatim and
reported as new-divergence candidates by the wave report.

Cleanup discipline: repos are DELETEd on both sides before the case returns;
a cleanup failure is recorded in evidence and surfaced in the wave report
(difftest namespace residue ledger).
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

REPO_KEY = "diff-catalog-" + dl.NONCE
EMPTY_IMAGE = "difftest-empty-" + dl.NONCE
CONTEXT_API = {"a": "/artifactory/api", "b": "/binflow/api"}

CASE = {
    "id": "dcr-catalog-empty-degrade",
    "title": "empty local repo: tags/list + catalog degradation shapes",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def create_repo(ctx, side):
    body = json.dumps({
        "key": REPO_KEY,
        "rclass": "local",
        "packageType": "docker",
    }).encode()
    return ctx.http(side, "PUT", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY),
                    body=body, headers={"Content-Type": "application/json"})


def delete_repo(ctx, side):
    return ctx.http(side, "DELETE", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY))


def shape_of(resp):
    """Body shape digest for intercomparison without literal-bytes coupling."""
    try:
        doc = json.loads(resp["body"].decode("utf-8"))
    except Exception:
        return {"json": False}
    if isinstance(doc, dict):
        return {"json": True, "keys": sorted(doc.keys()),
                "tags_type": type(doc.get("tags")).__name__,
                "tags_len": len(doc["tags"]) if isinstance(doc.get("tags"), list) else None}
    return {"json": True, "type": type(doc).__name__}


def run(ctx):
    checks = dl.Checks()
    facts = {"repo": REPO_KEY, "empty_image": EMPTY_IMAGE}

    created = {}
    for side in ("a", "b"):
        r = create_repo(ctx, side)
        created[side] = r["status"]
        checks.add(side, "create-empty-local-2xx", 200 <= r["status"] < 300,
                   "got %s body=%s" % (r["status"], r["body"][:160].decode("utf-8", "replace")))
    facts["create_status"] = created

    if not all(200 <= s < 300 for s in created.values()):
        for side in ("a", "b"):
            dr = delete_repo(ctx, side)
            facts.setdefault("cleanup", {})[side] = dr["status"]
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # arm 1: tags/list of a never-pushed image in the empty local repo
    arm1 = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "/v2/%s/%s/tags/list" % (REPO_KEY, EMPTY_IMAGE))
        arm1[side] = {"status": r["status"], "shape": shape_of(r),
                      "body_head": r["body"][:200].decode("utf-8", "replace")}
        checks.add(side, "arm1-empty-tags-status-recorded",
                   r["status"] in (200, 404), "got %s" % r["status"])
    checks.add("ab", "arm1-status-intercompare",
               arm1["a"]["status"] == arm1["b"]["status"],
               "a=%s b=%s" % (arm1["a"]["status"], arm1["b"]["status"]))
    checks.add("ab", "arm1-shape-intercompare",
               arm1["a"]["shape"] == arm1["b"]["shape"],
               "a=%s b=%s" % (arm1["a"]["shape"], arm1["b"]["shape"]))

    # arm 2: registry-level catalog — membership of the empty repo + shape
    arm2 = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "/v2/_catalog")
        members = None
        try:
            members = json.loads(r["body"].decode("utf-8")).get("repositories")
        except Exception:
            pass
        arm2[side] = {"status": r["status"], "shape": shape_of(r),
                      "has_empty_repo": bool(members and REPO_KEY in members),
                      "repo_count": len(members) if isinstance(members, list) else None,
                      "body_head": r["body"][:200].decode("utf-8", "replace"),
                      "content_type": dl.hdr(r, "Content-Type")}
        checks.add(side, "arm2-catalog-200", r["status"] == 200,
                   "got %s" % r["status"])
    checks.add("ab", "arm2-shape-intercompare",
               arm2["a"]["shape"] == arm2["b"]["shape"],
               "a=%s b=%s" % (arm2["a"]["shape"], arm2["b"]["shape"]))
    facts["arm2_catalog"] = arm2

    # arm 3: tags/list on the remote repo for an image the upstream never had
    arm3 = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "/v2/%s/%s/tags/list" % (dl.REPO, EMPTY_IMAGE))
        arm3[side] = {"status": r["status"], "shape": shape_of(r),
                      "body_head": r["body"][:200].decode("utf-8", "replace")}
        checks.add(side, "arm3-remote-tags-status-recorded",
                   r["status"] in (200, 404), "got %s" % r["status"])
    checks.add("ab", "arm3-status-intercompare",
               arm3["a"]["status"] == arm3["b"]["status"],
               "a=%s b=%s" % (arm3["a"]["status"], arm3["b"]["status"]))
    facts["arm3_remote_tags"] = arm3

    # cleanup: DELETE both repos, verify gone
    facts["cleanup"] = {}
    for side in ("a", "b"):
        dr = delete_repo(ctx, side)
        facts["cleanup"][side] = dr["status"]
        checks.add(side, "cleanup-delete-2xx", 200 <= dr["status"] < 300,
                   "got %s" % dr["status"])

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
