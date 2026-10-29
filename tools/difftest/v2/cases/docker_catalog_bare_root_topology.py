"""wave3 arm 1 — L043 candidate ③ deep dive: registry-level /v2/_catalog topology triple.

L043 finding to complete: A answers the bare-root catalog 200 with an EMPTY
body and ZERO Content-Type while B returns JSON membership. Open question:
is A's empty face membership-dependent (remote-only? local-only?) or an
invariant decoupled face? Three authed topologies + one anonymous state,
each dual-send:

  P0 fixture  : baseline — the persistent remote repo diff-docker-remote is
                the only difftest-namespace docker repo (authed)
  P1 +local   : a run-unique EMPTY local docker repo created (authed);
                membership deltas recorded on both sides
  P2 anon     : no credentials at all (record-only — policy may differ)
  P3 -local   : temp repo DELETEd; membership gone re-recorded

Verdict-driving checks: authed catalog 200 on both sides in P0/P1/P3;
A-side tri-state stability (empty body, zero CT in every authed phase —
the reference invariant under test); B-side tri-state stability (JSON with
a repositories key); A/B status intercompare. The shape intercompare is
expected to split (that split IS candidate ③, now evidenced across all
three topologies). Cleanup: temp repo DELETEd on both sides before return.
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

REPO_KEY = "diff-cat3-" + dl.NONCE
CONTEXT_API = {"a": "/artifactory/api", "b": "/binflow/api"}


CASE = {
    "id": "dcr-catalog-bare-root-topology",
    "title": "/v2/_catalog bare-root face across remote/local/anonymous topologies",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def catalog_view(resp):
    view = {"status": resp["status"],
            "content_type": dl.hdr(resp, "Content-Type"),
            "body_len": len(resp["body"]),
            "body_head": resp["body"][:200].decode("utf-8", "replace")}
    try:
        doc = json.loads(resp["body"].decode("utf-8"))
        view["json"] = True
        view["repositories"] = doc.get("repositories") if isinstance(doc, dict) else None
    except Exception:
        view["json"] = False
        view["repositories"] = None
    return view


def run(ctx):
    checks = dl.Checks()
    facts = {"temp_repo": REPO_KEY}

    def authed_catalog(side):
        return ctx.http(side, "GET", "/v2/_catalog")

    # ---- P0: baseline with the fixture remote repo present
    p0 = {s: catalog_view(authed_catalog(s)) for s in ("a", "b")}
    facts["p0_fixture_baseline"] = p0

    # ---- P1: create the temp EMPTY local docker repo, re-read the catalog
    created = {}
    for side in ("a", "b"):
        body = json.dumps({"key": REPO_KEY, "rclass": "local",
                           "packageType": "docker"}).encode()
        r = ctx.http(side, "PUT", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY),
                     body=body, headers={"Content-Type": "application/json"})
        created[side] = r["status"]
        checks.add(side, "p1-create-local-2xx", 200 <= r["status"] < 300,
                   "got %s body=%s" % (r["status"], r["body"][:120].decode("utf-8", "replace")))
    facts["p1_create_status"] = created
    p1 = {}
    if all(200 <= s < 300 for s in created.values()):
        p1 = {s: catalog_view(authed_catalog(s)) for s in ("a", "b")}
        facts["p1_local_present"] = p1
        for side in ("a", "b"):
            members = p1[side]["repositories"] or []
            facts.setdefault("p1_membership", {})[side] = {
                "has_temp_repo": REPO_KEY in members,
                "has_remote_fixture": any("diff-docker-remote" in m for m in members),
                "repo_count": len(members),
            }
    else:
        for side in ("a", "b"):
            dr = ctx.http(side, "DELETE", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY))
            facts.setdefault("cleanup", {})[side] = dr["status"]
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # ---- P2: anonymous (record-only)
    p2 = {s: catalog_view(dl.anon_http(ctx, s, "GET", "/v2/_catalog")) for s in ("a", "b")}
    facts["p2_anonymous"] = p2

    # ---- P3: cleanup then re-read (membership-gone re-record)
    facts["cleanup"] = {}
    for side in ("a", "b"):
        dr = ctx.http(side, "DELETE", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY))
        facts["cleanup"][side] = dr["status"]
        checks.add(side, "p3-cleanup-delete-2xx", 200 <= dr["status"] < 300,
                   "got %s" % dr["status"])
    p3 = {s: catalog_view(authed_catalog(s)) for s in ("a", "b")}
    facts["p3_post_cleanup"] = p3
    for side in ("a", "b"):
        members = p3[side]["repositories"] or []
        facts.setdefault("p3_membership", {})[side] = {
            "has_temp_repo": REPO_KEY in members, "repo_count": len(members)}

    # ---- verdict-driving checks
    for name, view in (("p0", p0), ("p1", p1), ("p3", p3)):
        for side in ("a", "b"):
            checks.add(side, "%s-authed-catalog-200" % name, view[side]["status"] == 200,
                       "got %s" % view[side]["status"])
        checks.add("ab", "%s-status-intercompare" % name,
                   view["a"]["status"] == view["b"]["status"],
                   "a=%s b=%s" % (view["a"]["status"], view["b"]["status"]))
        checks.add("ab", "%s-body-kind-intercompare" % name,
                   view["a"]["json"] == view["b"]["json"],
                   "a json=%s (len %s) b json=%s (len %s)" % (
                       view["a"]["json"], view["a"]["body_len"],
                       view["b"]["json"], view["b"]["body_len"]))

    # A-side tri-state invariant: empty body + zero CT in every authed phase
    for name, view in (("p0", p0), ("p1", p1), ("p3", p3)):
        checks.add("a", "%s-a-empty-body-zero-ct" % name,
                   view["a"]["body_len"] == 0 and view["a"]["content_type"] is None,
                   "len=%s ct=%s" % (view["a"]["body_len"], view["a"]["content_type"]))
    # B-side tri-state invariant: JSON object carrying a repositories key
    for name, view in (("p0", p0), ("p1", p1), ("p3", p3)):
        checks.add("b", "%s-b-json-repositories-key" % name,
                   view["b"]["json"] and isinstance(view["b"]["repositories"], list),
                   "json=%s repositories_type=%s" % (
                       view["b"]["json"], type(view["b"]["repositories"]).__name__))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
