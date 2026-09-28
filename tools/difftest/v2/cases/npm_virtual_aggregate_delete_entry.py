"""T-550 arm 5 (known-divergence rest/virtual-aggregate-delete-entry-arm):
the WITH-ENTRY arm of DELETE /{virtual}/{path}.

A-side construction forensics (live 7.161.26, this batch): the <virt>-cache
aggregate repo does NOT materialize through any npm wire flow probed —
  (1) local-only virtual packument GET,
  (2) remote+local mixed merge GET,
  (3) explicit virtualRetrievalCachePeriodSecs=600,
  (4) remote-sourced tarball GET through the virtual,
  (5) local-sourced tarball GET through the virtual;
/api/storage/<virt>-cache/** answers 404 "Unable to find item" while the
remote's <K>-cache/.npm listing serves 200. The A with-entry arm therefore
stays UNMEASURED (construction unattained from the wire) — recorded here
as evidence, not papered over.

B-side reachable half (drift rows ARE the own storage,
internal/repo/service.go deleteVirtualOwnStorage): a raw-seeded node row
under the virtual key drops with 204 via DELETE /{virt}/{path}, the
re-DELETE answers the idempotent 404, members survive. Seeding goes
through the scratch instance's SQLite (env DIFFTEST_B_DB; the runner
process is the difftest harness operating its own scratch keyspace) —
generic package type so no protocol layout gate shadows the service
branch (maven layout validation rejects non-GAV paths first, probe
2026-09-28).
"""
import importlib.util
import json
import os
import subprocess

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "npm-virtual-aggregate-delete-entry",
    "title": "virtual own-storage DELETE with an entry — A construction "
             "forensics (unattained) + B drift-row arm",
    "layer": "L7",
    "domain": "npm",
    "auth": True,
    "timeout_s": 300,
}

TAG = "r4t550g"
NLOC, NVIRT = "difftest-%s-loc" % TAG, "difftest-%s-virt" % TAG
GEN_LOC, GEN_VIRT = "difftest-%s-gloc" % TAG, "difftest-%s-gvirt" % TAG
DRIFT = "drift/agg-entry.bin"
MEMBER_PATH = "com/diff/dr/1.0.0/dr-1.0.0.pom"

EXPECT_A = {
    "a_rem_cache_npm_listing": "status=200",
    "a_virt_cache_listing": "status=404",
    "a_delete_no_entry": "status=405",  # live 7.161.26: plain DELETE on the
    # npm API plane is 405 (unpublish requires the -rev form); the §7.5
    # storage-plane no-entry 404 posture is pinned by the existing
    # maven-virtual-delete-passthrough case
    "a_member_survives": "status=200",
}
EXPECT_B = {
    "b_seed_visible_to_get": "status=404",  # probe 2026-09-28: virtual GET
    # does not serve own-storage rows (DELETE-only visibility)
    "b_delete_entry": "status=204",
    "b_redelete": "404-family",
    "b_member_survives": "status=200",
}


def _sql(db, stmt):
    p = subprocess.run(["sqlite3", db, stmt], capture_output=True, text=True,
                       timeout=15)
    return p.returncode, p.stdout.strip(), p.stderr.strip()


def _a_leg(ctx):
    """Construction forensics on A: fake source + remote + virtual, merges
    and tarball pulls, then the <virt>-cache enumeration and the no-entry
    DELETE posture. Uses the L031 helpers (import-only; no modification)."""
    npmlib_spec = importlib.util.spec_from_file_location(
        "difftest_npmlib",
        os.path.join(os.path.dirname(os.path.abspath(__file__)), "_npmlib.py"))
    npmlib = importlib.util.module_from_spec(npmlib_spec)
    npmlib_spec.loader.exec_module(npmlib)

    asserts, raw = {}, {}
    src = "difftest-%s-src" % TAG
    rem = "difftest-%s-rem" % TAG
    scratch = npmlib.scratch_dir()
    try:
        fx = npmlib.build_fixtures()
        rmt = fx["rmt"]["name"]
        # fake upstream on A (published with the real npm client)
        npmlib.repo_put(ctx, "a", src, {"rclass": "local", "packageType": "npm"})
        cache = os.path.join(scratch, "npmc")
        os.makedirs(cache, exist_ok=True)
        reg = npmlib.registry_url(ctx, "a", src)
        paths = npmlib.materialize(fx, scratch)
        for i in range(len(fx["rmt"]["versions"])):
            r = npmlib.run_npm(ctx, "a", ["publish", paths[("rmt", i)]], reg, cache)
            if r["exit"] != "0":
                raise mavenlib.SetupError("seed rmt[%d]: %s" % (i, r["out"][-160:]))
        npmlib.repo_put(ctx, "a", NLOC, {"rclass": "local", "packageType": "npm"})
        lcache = os.path.join(scratch, "npmc-loc")
        os.makedirs(lcache, exist_ok=True)
        lreg = npmlib.registry_url(ctx, "a", NLOC)
        r = npmlib.run_npm(ctx, "a", ["publish", paths["lonly"]], lreg, lcache)
        if r["exit"] != "0":
            raise mavenlib.SetupError("seed lonly: %s" % r["out"][-160:])
        npmlib.repo_put(ctx, "a", rem, {
            "rclass": "remote", "packageType": "npm",
            "url": npmlib.a_direct_base(ctx.sides["a"]["base"]) + "/api/npm/" + src,
            "username": ctx.sides["a"]["user"],
            "password": ctx.sides["a"]["password"]})
        npmlib.repo_put(ctx, "a", NVIRT, {
            "rclass": "virtual", "packageType": "npm",
            "repositories": [rem, NLOC]})

        # constructions 1-5: merges + tarball pulls through the virtual
        for pkg in (rmt, fx["shadow_loc"]["name"], fx["lonly"]["name"]):
            ctx.http("a", "GET", "/api/npm/%s/%s" % (NVIRT, pkg))
        ctx.http("a", "GET", "/api/npm/%s/%s/-/difftest-npm-rmt-2.0.0.tgz" % (NVIRT, rmt))
        ctx.http("a", "GET", "/api/npm/%s/difftest-npm-lonly/-/difftest-npm-lonly-1.0.0.tgz" % NVIRT)

        rc = ctx.http("a", "GET", "/api/storage/%s-cache/.npm" % rem)
        raw["a_rem_cache_listing_status"] = rc["status"]
        asserts["a_rem_cache_npm_listing"] = "status=%d" % rc["status"]
        vc = ctx.http("a", "GET", "/api/storage/%s-cache/" % NVIRT)
        vc2 = ctx.http("a", "GET", "/api/storage/%s-cache/.npm/%s" % (NVIRT, rmt))
        raw["a_virt_cache_root"] = vc["status"]
        raw["a_virt_cache_npm"] = vc2["status"]
        raw["a_virt_cache_body"] = vc["body"][:200].decode("utf-8", "replace")
        asserts["a_virt_cache_listing"] = "status=%d" % vc["status"]

        # no-entry DELETE posture through the virtual (packument wire path)
        d = ctx.http("a", "DELETE", "/api/npm/%s/%s" % (NVIRT, rmt))
        raw["a_delete_no_entry_status"] = d["status"]
        body = d["body"][:300].decode("utf-8", "replace")
        asserts["a_delete_no_entry"] = "status=%d" % d["status"]
        raw["a_delete_no_entry_body"] = body

        m = ctx.http("a", "GET", "/api/npm/%s/%s" % (rem, rmt))
        asserts["a_member_survives"] = "status=%d" % m["status"]
    finally:
        mavenlib.cleanup_repos(ctx, "a", [NVIRT, rem, NLOC, src,
                                          NVIRT + "-cache", rem + "-cache"])
        mavenlib.rm_scratch(scratch)
    ctx.write_evidence("a-leg.json", {"asserts": asserts, "raw": raw})
    return asserts


def _b_leg(ctx):
    asserts, raw = {}, {}
    db = os.environ.get("DIFFTEST_B_DB", "")
    if not db or not os.path.exists(db):
        return None, "DIFFTEST_B_DB not set or missing: %r" % db
    mavenlib.ensure_repos(ctx, "b", [
        (GEN_LOC, {"rclass": "local", "packageType": "generic"}),
        (GEN_VIRT, {"rclass": "virtual", "packageType": "generic",
                    "repositories": [GEN_LOC]}),
    ])
    pom = mavenlib.pom_fixture("com.diff", "dr", "1.0.0")
    put = ctx.http("b", "PUT", "/%s/%s" % (GEN_LOC, MEMBER_PATH), body=pom,
                   headers={"Content-Type": "application/xml"})
    raw["member_put"] = put["status"]

    rc, out, err = _sql(db, "SELECT sha256, size FROM nodes WHERE repo_key='%s' "
                            "AND path='%s';" % (GEN_LOC, MEMBER_PATH))
    if rc != 0 or "|" not in out:
        mavenlib.cleanup_repos(ctx, "b", [GEN_VIRT, GEN_LOC])
        return None, "member row lookup failed: %s" % (err or out)
    sha, size = out.split("|", 1)
    stmt = ("INSERT INTO nodes (repo_key, path, sha256, size, mime, "
            "created_by, created_at, updated_at) VALUES ('%s', '%s', '%s', "
            "%s, 'application/octet-stream', 'difftest', "
            "'2026-09-28T12:00:00Z', '2026-09-28T12:00:00Z');" %
            (GEN_VIRT, DRIFT, sha, size or "0"))
    rc, out, err = _sql(db, stmt)
    raw["seed_rc"] = rc
    raw["seed_err"] = err[:200]
    if rc != 0:
        mavenlib.cleanup_repos(ctx, "b", [GEN_VIRT, GEN_LOC])
        return None, "seed failed: %s" % err[:200]
    rc, out, _ = _sql(db, "SELECT count(*) FROM nodes WHERE repo_key='%s' "
                          "AND path='%s';" % (GEN_VIRT, DRIFT))
    raw["seed_count"] = out

    try:
        g = ctx.http("b", "GET", "/%s/%s" % (GEN_VIRT, DRIFT))
        raw["get_drift_status"] = g["status"]
        asserts["b_seed_visible_to_get"] = "status=%d" % g["status"]

        d = ctx.http("b", "DELETE", "/%s/%s" % (GEN_VIRT, DRIFT))
        raw["delete_status"] = d["status"]
        raw["delete_body"] = d["body"][:200].decode("utf-8", "replace")
        asserts["b_delete_entry"] = "status=%d" % d["status"]

        d2 = ctx.http("b", "DELETE", "/%s/%s" % (GEN_VIRT, DRIFT))
        raw["redelete_status"] = d2["status"]
        body2 = d2["body"][:300].decode("utf-8", "replace")
        asserts["b_redelete"] = (
            "404-family" if d2["status"] == 404 else "status=%d" % d2["status"])
        raw["redelete_body"] = body2

        m = ctx.http("b", "GET", "/%s/%s" % (GEN_LOC, MEMBER_PATH))
        asserts["b_member_survives"] = "status=%d" % m["status"]
        rc, out, _ = _sql(db, "SELECT count(*) FROM nodes WHERE repo_key='%s' "
                              "AND path='%s';" % (GEN_VIRT, DRIFT))
        raw["rows_after"] = out
    finally:
        mavenlib.cleanup_repos(ctx, "b", [GEN_VIRT, GEN_LOC])
    ctx.write_evidence("b-leg.json", {"asserts": asserts, "raw": raw})
    return asserts, None


def run(ctx):
    per_side = {}
    try:
        per_side["a"] = _a_leg(ctx)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "a-leg setup: %s" % e}
    b_asserts, b_err = _b_leg(ctx)
    if b_err:
        return {"status": "BLOCKED", "reason": "b-leg: %s" % b_err}
    per_side["b"] = b_asserts

    expected = dict(EXPECT_A)
    expected.update(EXPECT_B)
    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "the dual WITH-ENTRY compare is UNMEASURED: A's construction "
                "leg is unattained from the wire (five flows enumerated); "
                "PASS pins each side's reachable own posture"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "A: source+rem+virt npm layout, merges/"
                                  "tarballs, virt-cache enum, no-entry "
                                  "DELETE; B: generic drift seed, DELETE, "
                                  "re-DELETE, member check"}]}
