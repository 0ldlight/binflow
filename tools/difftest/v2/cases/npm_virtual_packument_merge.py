"""L031 case 1 (T-545): virtual packument merge semantics on the A reference
vs BinFlow, under the remote+cache member layout, with a controlled fake
upstream — curl client.

Behavior asserted (spec: docs/reverse/virtual-resolution.md §1/§2/§6 +
maven-npm-pypi.md §2.6):
  - Member declaration order [remote, local] still resolves locals first
    (§1: the expanded sequence has locals before remotes regardless of
    declaration order), so the local shadow copy wins putIfAbsent for the
    same name+version (shasum, tarball bytes and description all local).
  - Version union for the upstream-only package (3 versions, exactly once).
  - dist-tags union with latest RECOMPUTED to the newest version (upstream
    latest deliberately rewound to 1.0.0 while 2.0.0 exists) and the stable
    tag surviving untouched.
  - Local-member-only package appears through the virtual.
  - Tarball byte fidelity through the virtual: shadow = local bytes,
    upstream package = upstream bytes (sha256 anchors from deterministic
    fixtures).
  - Unknown package -> 404.

Cache-repo observatory (recorded raw, judged in the batch report):
  - <rem>-cache derivation (GET /api/repositories/<rem>-cache) and cached
    packument storage probe (/<rem>-cache/.npm/<pkg>/package.json).
  - <virt>-cache derivation (npm aggregation cache, spec §6) and its .npm
    children via /api/storage.
  - ETag / X-Binflow-* headers on the served packument/tarball.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_npmlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_npmlib.py"))
npmlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(npmlib)

TAG = "npm1"
FX = npmlib.build_fixtures()
RMT = FX["rmt"]["name"]
SHADOW = FX["shadow_loc"]["name"]
LONLY = FX["lonly"]["name"]

CASE = {
    "id": "npm-virtual-packument-merge",
    "title": "virtual npm packument merge — locals-first putIfAbsent, version "
             "union, dist-tags latest recompute, tarball byte fidelity",
    "layer": "L7",
    "domain": "npm",
    "auth": True,
    "timeout_s": 480,
}

EXPECTED = {
    "rmt_versions_union": "|".join(v["version"] for v in FX["rmt"]["versions"]),
    # Round-1 A observation (2026-09-28): with NO exclusion patterns the
    # reference PRESERVES the base member's dist-tags verbatim — the §2.6
    # latest recompute applies to the exclusion-filtered set, not an
    # unconditional max(). Upstream latest is deliberately stale (1.0.0) to
    # pin this: expected = preserved base tag. A BinFlow max-version
    # recompute here is the divergence this case exists to surface.
    "rmt_latest_tag": "1.0.0",
    "rmt_stable_tag_union": "2.0.0",
    "shadow_version": "1.0.0",
    "shadow_shasum_local_first": FX["shadow_loc"]["sha1"],
    "shadow_description_base_member": FX["shadow_loc"]["description"],
    "lonly_versions": "1.0.0",
    "tarball_shadow_local_bytes": "200+sha256-ok",
    "tarball_rmt_upstream_bytes": "200+sha256-ok",
    "missing_pkg_status": "status=404",
}


def _packument_asserts(resp, asserts, prefix, fx_entry=None):
    sem = npmlib.semantic_packument(resp["body"])
    if "error" in sem:
        asserts[prefix + "_versions_union"] = "status=%d %s" % (
            resp["status"], sem["error"])
        return sem
    asserts[prefix + "_versions_union"] = "|".join(sem["versions"])
    return sem


def _leg(ctx, side):
    keys = npmlib.layout_keys(TAG)
    virt = keys["virt"]
    asserts, raw = {}, {}
    scratch = npmlib.scratch_dir()
    proxy = None
    try:
        npmlib.cleanup_layout(ctx, side, TAG)
        if side == "b":
            src_reg = (npmlib.a_direct_base(ctx.sides["a"]["base"])
                       + "/api/npm/" + keys["src"])
            proxy = npmlib.start_layout_proxy(
                src_reg, ctx.sides["a"]["user"], ctx.sides["a"]["password"])
        npmlib.build_layout(ctx, side, scratch, FX, TAG, proxy)

        # --- upstream-only package: union + dist-tags recompute shape
        resp = npmlib.get_packument(ctx, side, virt, RMT)
        sem = _packument_asserts(resp, asserts, "rmt")
        raw["rmt_status"] = resp["status"]
        raw["rmt_semantic"] = sem
        raw["rmt_etag"] = resp["headers"].get("ETag") or resp["headers"].get("etag")
        if "error" not in sem:
            asserts["rmt_latest_tag"] = sem["dist_tags"].get("latest") or "absent"
            asserts["rmt_stable_tag_union"] = sem["dist_tags"].get("stable") or "absent"

        # --- shadow: locals-first putIfAbsent
        resp = npmlib.get_packument(ctx, side, virt, SHADOW)
        sem = npmlib.semantic_packument(resp["body"])
        raw["shadow_status"] = resp["status"]
        raw["shadow_semantic"] = sem
        if "error" in sem:
            asserts["shadow_version"] = "status=%d %s" % (resp["status"], sem["error"])
            asserts["shadow_shasum_local_first"] = "status=%d" % resp["status"]
            asserts["shadow_description_base_member"] = "status=%d" % resp["status"]
        else:
            asserts["shadow_version"] = "|".join(sem["versions"]) or "absent"
            asserts["shadow_shasum_local_first"] = sem.get("shasum_1.0.0") or "absent"
            asserts["shadow_description_base_member"] = sem.get("description") or "absent"

        # --- local-member-only package
        resp = npmlib.get_packument(ctx, side, virt, LONLY)
        sem = npmlib.semantic_packument(resp["body"])
        raw["lonly_status"] = resp["status"]
        asserts["lonly_versions"] = ("|".join(sem["versions"]) or "absent"
                                     ) if "error" not in sem else "status=%d" % resp["status"]

        # --- tarball byte fidelity
        asserts["tarball_shadow_local_bytes"] = npmlib.tarball_verdict(
            ctx, side, virt, SHADOW, FX["shadow_loc"]["filename"],
            FX["shadow_loc"]["tgz_sha256"])
        rmt101 = FX["rmt"]["versions"][1]
        tb = npmlib.tarball_verdict(ctx, side, virt, RMT, rmt101["filename"],
                                    rmt101["tgz_sha256"])
        asserts["tarball_rmt_upstream_bytes"] = tb
        if side == "b":
            probe = ctx.http(side, "GET", "/api/npm/%s/%s/-/%s" % (
                virt, RMT, rmt101["filename"]))
            raw["b_tarball_headers"] = {
                k: v for k, v in probe["headers"].items()
                if k.lower().startswith("x-binflow")}

        # --- miss
        miss = ctx.http(side, "GET", "/api/npm/%s/difftest-npm-nosuchpkg" % virt)
        asserts["missing_pkg_status"] = "status=%d" % miss["status"]

        # --- cache-repo observatory (raw only; adjudicated in the report)
        for label, path in (
                ("rem_cache_repo", "/api/repositories/%s-cache" % keys["rem"]),
                ("rem_cache_packument_storage",
                 "/%s-cache/.npm/%s/package.json" % (keys["rem"], RMT)),
                ("virt_cache_repo", "/api/repositories/%s-cache" % virt),
                ("virt_cache_npm_dir", "/api/storage/%s-cache/.npm" % virt)):
            try:
                r = ctx.http(side, "GET", path)
                raw[label] = {"status": r["status"],
                              "body_head": r["body"][:220].decode("utf-8", "replace")}
            except Exception as e:  # noqa: BLE001 - observatory must not fail the case
                raw[label] = {"error": repr(e)}
    finally:
        npmlib.cleanup_layout(ctx, side, TAG)
        if proxy:
            proxy[0].shutdown()
            proxy[0].server_close()
        npmlib.rm_scratch(scratch)
    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    scratch = npmlib.scratch_dir()
    try:
        # shared fake upstream on A spans both legs (round-1 lesson)
        npmlib.ensure_source_on_a(ctx, scratch, FX, TAG)
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except npmlib.SetupError as e:
        for s in ("a", "b"):
            npmlib.cleanup_layout(ctx, s, TAG)
        npmlib.drop_source_on_a(ctx, TAG)
        npmlib.rm_scratch(scratch)
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            npmlib.cleanup_layout(ctx, s, TAG)
        npmlib.drop_source_on_a(ctx, TAG)
        npmlib.rm_scratch(scratch)

    status, reason, mism = npmlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "seed src(a)/loc, rem, virt[rem,loc]; GET "
                                  "packuments x3; tarballs x2; miss; cache "
                                  "observatory"}]}
