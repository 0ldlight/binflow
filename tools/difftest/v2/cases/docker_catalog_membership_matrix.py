"""wave5 arm 1 — L043 candidate ③ continuation: catalog membership matrix.

wave3 (dcr-catalog-bare-root-topology) established: A's bare-root /v2/_catalog
is an invariant decoupled face (empty body, zero CT) across
remote-present / empty-local / post-cleanup topologies, while B aggregates
(remote upstream entries enter, empty local repos do not). This case adds the
missing topologies and states:

  P0 context   : authed catalog + per-side REST repo counts and docker
                 topology keys (A carries ~93 repos incl. docker local x2,
                 remote x2, virtual x1 — B carries the fixture remote only)
  T tri-state  : catalog under anon / one-shot bad-basic / valid Basic
                 (wave3 recorded the anon leg only)
  M populated  : temp LOCAL docker repo created on both sides AND populated
                 with the deterministic smoke image (blob upload + manifest
                 PUT) — does a populated local repo enter the catalog?
  C cleanup    : temp repo DELETEd, membership-gone re-read

Verdict-driving: A-side empty-body-zero-CT invariant across P0/M/C (now
including a POPULATED local repo — the strongest topology yet); B-side JSON
carrying repositories; B membership delta (temp entry appears at M, gone at
C); A/B status intercompare per phase. Cleanup deletes the temp repo on both
sides before return.
"""
import base64
import hashlib
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

REPO_KEY = "diff-cat5-" + dl.NONCE
ONESHOT = "w5c" + dl.NONCE
CONTEXT_API = {"a": "/artifactory/api", "b": "/binflow/api"}
ACCEPT_S2 = dl.ACCEPT_MANIFEST

# deterministic smoke fixture bytes (byte-identical to docker_wave_setup.sh)
LAYER_BODY = b"A" * 64
LAYER_SHA = hashlib.sha256(LAYER_BODY).hexdigest()
CONFIG_BODY = (
    '{"architecture":"amd64","os":"linux","created":"2026-10-28T00:00:00Z",'
    '"config":{},"rootfs":{"type":"layers","diff_ids":["sha256:%s"]},'
    '"history":[{"created":"2026-10-28T00:00:00Z","created_by":"difftest wave"}]}'
    % LAYER_SHA).encode()
CFG_SHA = hashlib.sha256(CONFIG_BODY).hexdigest()
MANIFEST_BODY = (
    '{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",'
    '"config":{"mediaType":"application/vnd.docker.container.image.v1+json",'
    '"size":%d,"digest":"sha256:%s"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip",'
    '"size":%d,"digest":"sha256:%s"}]}'
    % (len(CONFIG_BODY), CFG_SHA, len(LAYER_BODY), LAYER_SHA)).encode()
MANIFEST_SHA = hashlib.sha256(MANIFEST_BODY).hexdigest()

CASE = {
    "id": "dcr-catalog-membership-matrix",
    "title": "/v2/_catalog membership across populated-local topology x auth tri-state",
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


def push_smoke_image(ctx, side, repo_key):
    """Upload layer+config blobs and the schema2 manifest into a LOCAL repo.
    Returns a dict of step statuses; no secrets in any value."""
    base = ctx.sides[side]["base"]
    out = {}
    for name, body, sha in (("layer", LAYER_BODY, LAYER_SHA),
                            ("config", CONFIG_BODY, CFG_SHA)):
        head = ctx.http(side, "HEAD",
                        "/v2/%s/%s/blobs/sha256:%s" % (repo_key, dl.IMAGE, sha))
        if head["status"] == 200:
            out["blob-%s" % name] = "skip-200"
            continue
        post = ctx.http(side, "POST",
                        "/v2/%s/%s/blobs/uploads/" % (repo_key, dl.IMAGE))
        out["blob-%s-post" % name] = post["status"]
        loc = dl.hdr(post, "Location")
        if not loc:
            out["blob-%s" % name] = "no-location"
            continue
        url = loc if loc.startswith("http") else base + (loc if loc.startswith("/") else "/" + loc)
        sep = "&" if "?" in url else "?"
        # route the final PUT through the runner so Basic auth is attached
        path = url[len(base):] if url.startswith(base) else None
        if path is None:
            out["blob-%s" % name] = "external-location"
            continue
        put = ctx.http(side, "PUT", path + "%sdigest=sha256:%s" % (sep, sha),
                       body=body, headers={"Content-Type": "application/octet-stream"})
        out["blob-%s" % name] = put["status"]
    put = ctx.http(side, "PUT",
                   "/v2/%s/%s/manifests/w5m" % (repo_key, dl.IMAGE),
                   body=MANIFEST_BODY, headers={"Content-Type": ACCEPT_S2})
    out["manifest"] = put["status"]
    head = ctx.http(side, "HEAD",
                    "/v2/%s/%s/manifests/w5m" % (repo_key, dl.IMAGE),
                    headers={"Accept": ACCEPT_S2})
    out["manifest-head"] = head["status"]
    return out


def run(ctx):
    checks = dl.Checks()
    facts = {"temp_repo": REPO_KEY, "fixture_digests": {
        "layer": LAYER_SHA, "config": CFG_SHA, "manifest": MANIFEST_SHA}}

    # ---- P0: context + baseline catalog
    p0 = {s: catalog_view(ctx.http(s, "GET", "/v2/_catalog")) for s in ("a", "b")}
    facts["p0_fixture_baseline"] = p0
    facts["topology_context"] = {}
    for side in ("a", "b"):
        r = ctx.http(side, "GET", "%s/repositories" % CONTEXT_API[side])
        try:
            repos = dl.jbody(r)
            docker = [{"key": x.get("key"), "type": x.get("type")}
                      for x in repos if str(x.get("packageType", "")).lower() == "docker"]
            facts["topology_context"][side] = {
                "repo_count_total": len(repos), "docker_repos": docker}
        except Exception as e:
            facts["topology_context"][side] = {"rest_status": r["status"], "error": repr(e)}

    # ---- T: auth tri-state on the bare-root catalog
    tri = {}
    tri["valid"] = {s: catalog_view(ctx.http(s, "GET", "/v2/_catalog")) for s in ("a", "b")}
    tri["anon"] = {s: catalog_view(dl.anon_http(ctx, s, "GET", "/v2/_catalog"))
                   for s in ("a", "b")}
    tri["basic-bad"] = {}
    for s in ("a", "b"):
        bad = "Basic " + base64.b64encode(
            ("%s-difftest:difftest-wrongpass-w5" % ONESHOT).encode()).decode()
        r = ctx.http(s, "GET", "/v2/_catalog", headers={"Authorization": bad})
        tri["basic-bad"][s] = {"status": r["status"],
                               "body_head": r["body"][:160].decode("utf-8", "replace")}
    facts["tri_state"] = tri

    # ---- M: create + POPULATE the temp local repo on both sides
    created, pushes = {}, {}
    for side in ("a", "b"):
        body = json.dumps({"key": REPO_KEY, "rclass": "local",
                           "packageType": "docker"}).encode()
        r = ctx.http(side, "PUT", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY),
                     body=body, headers={"Content-Type": "application/json"})
        created[side] = r["status"]
        checks.add(side, "m-create-local-2xx", 200 <= r["status"] < 300,
                   "got %s body=%s" % (r["status"], r["body"][:120].decode("utf-8", "replace")))
    facts["m_create_status"] = created
    if all(200 <= s < 300 for s in created.values()):
        for side in ("a", "b"):
            pushes[side] = push_smoke_image(ctx, side, REPO_KEY)
            for step, st in pushes[side].items():
                checks.add(side, "m-push-%s-2xx" % step,
                           str(st).startswith("skip") or (isinstance(st, int) and 200 <= st < 300),
                           "got %s" % st)
        facts["m_push_steps"] = pushes
    else:
        facts["m_push_steps"] = "skipped-create-failed"

    pm = {}
    if all(200 <= s < 300 for s in created.values()):
        pm = {s: catalog_view(ctx.http(s, "GET", "/v2/_catalog")) for s in ("a", "b")}
        facts["m_populated_local"] = pm
        for side in ("a", "b"):
            members = pm[side]["repositories"] or []
            facts.setdefault("m_membership", {})[side] = {
                "has_temp_repo_entry": any(m.split("/", 1)[0] == REPO_KEY for m in members),
                "temp_entries": [m for m in members if m.split("/", 1)[0] == REPO_KEY],
                "repo_count": len(members),
            }

    # ---- C: cleanup then membership-gone re-read
    facts["cleanup"] = {}
    for side in ("a", "b"):
        dr = ctx.http(side, "DELETE", "%s/repositories/%s" % (CONTEXT_API[side], REPO_KEY))
        facts["cleanup"][side] = dr["status"]
        checks.add(side, "c-cleanup-delete-2xx", 200 <= dr["status"] < 300, "got %s" % dr["status"])
    p3 = {s: catalog_view(ctx.http(s, "GET", "/v2/_catalog")) for s in ("a", "b")}
    facts["c_post_cleanup"] = p3
    for side in ("a", "b"):
        members = p3[side]["repositories"] or []
        facts.setdefault("c_membership", {})[side] = {
            "has_temp_repo_entry": any(m.split("/", 1)[0] == REPO_KEY for m in members),
            "repo_count": len(members)}

    # ---- verdict-driving checks
    for name, view in (("p0", p0), ("m", pm), ("c", p3)):
        if not view:
            continue
        for side in ("a", "b"):
            checks.add(side, "%s-authed-catalog-200" % name, view[side]["status"] == 200,
                       "got %s" % view[side]["status"])
        checks.add("ab", "%s-status-intercompare" % name,
                   view["a"]["status"] == view["b"]["status"],
                   "a=%s b=%s" % (view["a"]["status"], view["b"]["status"]))
        checks.add("ab", "%s-body-kind-intercompare" % name,
                   view["a"]["json"] == view["b"]["json"],
                   "a json=%s(len %s) b json=%s(len %s)" % (
                       view["a"]["json"], view["a"]["body_len"],
                       view["b"]["json"], view["b"]["body_len"]))
    # A invariant: empty body + zero CT in every authed phase (incl. populated local)
    for name, view in (("p0", p0), ("m", pm), ("c", p3)):
        if not view:
            continue
        checks.add("a", "%s-a-empty-body-zero-ct" % name,
                   view["a"]["body_len"] == 0 and view["a"]["content_type"] is None,
                   "len=%s ct=%s" % (view["a"]["body_len"], view["a"]["content_type"]))
    # B invariant: JSON object with a repositories list
    for name, view in (("p0", p0), ("m", pm), ("c", p3)):
        if not view:
            continue
        checks.add("b", "%s-b-json-repositories-list" % name,
                   view["b"]["json"] and isinstance(view["b"]["repositories"], list),
                   "json=%s type=%s" % (view["b"]["json"],
                                        type(view["b"]["repositories"]).__name__))

    # tri-state: valid must be 200 both; anon/basic-bad status intercompared
    for state in ("valid", "anon", "basic-bad"):
        checks.add("ab", "tri-%s-status-intercompare" % state,
                   tri[state]["a"]["status"] == tri[state]["b"]["status"],
                   "a=%s b=%s" % (tri[state]["a"]["status"], tri[state]["b"]["status"]))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
