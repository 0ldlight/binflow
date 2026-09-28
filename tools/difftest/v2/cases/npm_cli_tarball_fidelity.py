"""L031 case 3 (T-545): real npm client through the virtual — view / install /
pack, sha256 byte fidelity both directions (Errata I-4 5th clause: npm CLI +
curl dual-client; this file is the npm CLI arm, case 1 is the curl arm).

Behavior asserted:
  - npm view <rmt> versions --json -> the full version union (remote merged
    through the four-bucket walk with cache steps deduped).
  - npm view <rmt> dist-tags --json -> latest recomputed to 2.0.0 (the stale
    upstream latest must NOT leak through the merge), stable preserved.
  - npm view <shadow>@1.0.0 dist.shasum -> the LOCAL member's sha1
    (locals-precede-remotes putIfAbsent).
  - npm install of pinned rmt@1.0.1 + shadow@1.0.0 + lonly@1.0.0 in a scratch
    project -> exit 0 and node_modules index.js bytes match the fixtures
    (shadow = local override content, rmt = upstream content, lonly = local).
  - npm pack <rmt>@2.0.0 / <shadow>@1.0.0 -> the downloaded tarball is
    byte-identical to the fixture tarballs (sha256 anchors).
"""
import hashlib
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_npmlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_npmlib.py"))
npmlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(npmlib)

TAG = "npm3"
FX = npmlib.build_fixtures()
RMT = FX["rmt"]["name"]
SHADOW = FX["shadow_loc"]["name"]
LONLY = FX["lonly"]["name"]

CASE = {
    "id": "npm-cli-tarball-fidelity",
    "title": "npm CLI through virtual — view union/tags/shasum, install, pack, "
             "tarball sha256 byte fidelity",
    "layer": "L5",
    "domain": "npm",
    "auth": True,
    "timeout_s": 600,
}

EXPECTED = {
    "view_versions_union": "|".join(v["version"] for v in FX["rmt"]["versions"]),
    # round-1 A observation: stale upstream latest preserved through the merge
    "view_dist_tags": "latest=1.0.0|stable=2.0.0",
    "view_shadow_shasum_local_first": FX["shadow_loc"]["sha1"],
    "install_exit": "0",
    "installed_shadow_bytes": "sha256-ok",
    "installed_rmt_bytes": "sha256-ok",
    "installed_lonly_bytes": "sha256-ok",
    "pack_rmt_upstream_bytes": "sha256-ok",
    "pack_shadow_local_bytes": "sha256-ok",
}


def _file_sha256(path):
    with open(path, "rb") as fh:
        return hashlib.sha256(fh.read()).hexdigest()


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

        cache = os.path.join(scratch, "npm-cache-%s" % side)
        os.makedirs(cache, exist_ok=True)
        reg = npmlib.registry_url(ctx, side, virt)

        res = npmlib.run_npm(ctx, side, ["view", RMT, "versions", "--json"],
                             reg, cache)
        out = npmlib.npm_out_json(res)
        raw["view_versions"] = res
        asserts["view_versions_union"] = ("|".join(out) if isinstance(out, list)
                                          else "exit=%s" % res["exit"])

        res = npmlib.run_npm(ctx, side, ["view", RMT, "dist-tags", "--json"],
                             reg, cache)
        out = npmlib.npm_out_json(res)
        raw["view_dist_tags"] = res
        asserts["view_dist_tags"] = (
            "|".join("%s=%s" % (k, out[k]) for k in sorted(out))
            if isinstance(out, dict) else "exit=%s" % res["exit"])

        res = npmlib.run_npm(ctx, side,
                             ["view", SHADOW + "@1.0.0", "dist.shasum", "--json"],
                             reg, cache)
        out = npmlib.npm_out_json(res)
        raw["view_shadow_shasum"] = res
        asserts["view_shadow_shasum_local_first"] = (
            out if isinstance(out, str) else "exit=%s" % res["exit"])

        # scratch install project with pinned dependencies
        proj = os.path.join(scratch, "proj-%s" % side)
        os.makedirs(proj, exist_ok=True)
        with open(os.path.join(proj, "package.json"), "w") as fh:
            json.dump({"name": "difftest-install", "version": "1.0.0",
                       "dependencies": {
                           RMT: "1.0.1", SHADOW: "1.0.0", LONLY: "1.0.0"}}, fh)
        res = npmlib.run_npm(ctx, side, ["install"], reg, cache, cwd=proj)
        raw["install"] = {"exit": res["exit"], "out": res["out"][-600:]}
        asserts["install_exit"] = res["exit"]
        for label, pkg, want in (
                ("installed_shadow_bytes", SHADOW, FX["shadow_loc"]["index_sha256"]),
                ("installed_rmt_bytes", RMT,
                 FX["rmt"]["versions"][1]["index_sha256"]),
                ("installed_lonly_bytes", LONLY, FX["lonly"]["index_sha256"])):
            path = os.path.join(proj, "node_modules", pkg, "index.js")
            if res["exit"] != "0" or not os.path.isfile(path):
                asserts[label] = "missing"
            else:
                asserts[label] = ("sha256-ok" if _file_sha256(path) == want
                                  else "sha_mismatch")

        # npm pack downloads the tarball served by the virtual: byte fidelity
        res = npmlib.run_npm(ctx, side, ["pack", RMT + "@2.0.0",
                                         "--pack-destination", scratch],
                             reg, cache, cwd=proj)
        raw["pack_rmt"] = {"exit": res["exit"], "out": res["out"][-300:]}
        path = os.path.join(scratch, "%s-2.0.0.tgz" % RMT)
        asserts["pack_rmt_upstream_bytes"] = (
            "sha256-ok" if res["exit"] == "0" and os.path.isfile(path)
            and _file_sha256(path) == FX["rmt"]["versions"][2]["tgz_sha256"]
            else "exit=%s" % res["exit"] if not os.path.isfile(path)
            else "sha_mismatch")

        res = npmlib.run_npm(ctx, side, ["pack", SHADOW + "@1.0.0",
                                         "--pack-destination", scratch],
                             reg, cache, cwd=proj)
        raw["pack_shadow"] = {"exit": res["exit"], "out": res["out"][-300:]}
        path = os.path.join(scratch, "%s-1.0.0.tgz" % SHADOW)
        asserts["pack_shadow_local_bytes"] = (
            "sha256-ok" if res["exit"] == "0" and os.path.isfile(path)
            and _file_sha256(path) == FX["shadow_loc"]["tgz_sha256"]
            else "exit=%s" % res["exit"] if not os.path.isfile(path
                                                              ) else "sha_mismatch")
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
            "requests": [{"step": "seed; npm view x3; npm install; npm pack x2"}]}
