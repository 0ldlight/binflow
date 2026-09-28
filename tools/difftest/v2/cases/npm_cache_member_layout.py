"""L031 case 2 (T-545): the explicit cache-member layout — is the auto-derived
<remote>-cache repo accepted as a virtual member, and does the packument merge
stay correct (cache projection filtered / no double-count)?

This is the A-side differential arm of the T-538 FacetCache question: BinFlow
expands every remote member into FacetCache + FacetPlain steps and filters all
cache steps whenever a remote body step is present (docs/design/virtual-
four-bucket.md §5.3; spec twin: virtual-resolution.md §6 filterCache-
RepositoriesDuplication "只要有序列含任一 remote 本体，就把所有 cache 仓从序
列滤掉"). The wire question for the reference: with <rem>-cache declared as an
EXPLICIT member next to its remote body, what does A do?

Behavior asserted:
  - If the explicit-cache virtual is accepted (PUT 2xx), its merged packument
    must equal the plain virtual's merge (versions union, dist-tags, shadow
    shasum local-first) and tarball resolution must stay byte-faithful —
    duplicate cache membership must not double-count or reorder precedence.
  - A repeat GET of the plain virtual packument stays semantically stable
    (aggregation-cache window, spec §6 TTL 600s).
  - The SLIM packument variant (Accept: application/vnd.npm.install-v1+json,
    maven-npm-pypi.md §2 table) serves the same version union.

The accept/reject status of the explicit cache member itself is recorded raw
(no spec pin for either outcome; adjudicated in the batch report).
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_npmlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_npmlib.py"))
npmlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(npmlib)

TAG = "npm2"
FX = npmlib.build_fixtures()
RMT = FX["rmt"]["name"]
SHADOW = FX["shadow_loc"]["name"]

CASE = {
    "id": "npm-cache-member-layout",
    "title": "explicit <remote>-cache virtual member — accept status, merge "
             "equivalence, repeat stability, SLIM variant",
    "layer": "L7",
    "domain": "npm",
    "auth": True,
    "timeout_s": 480,
}

EXPECTED = {
    "virt_versions_union": "|".join(v["version"] for v in FX["rmt"]["versions"]),
    # round-1 A observation: base member dist-tags preserved (no excludes)
    "virt_latest_tag": "1.0.0",
    "virt_repeat_stable": "same",
    "slim_status": "status=200",
    "slim_versions_union": "|".join(v["version"] for v in FX["rmt"]["versions"]),
    # conditional asserts (only when the explicit cache-member PUT is 2xx)
    "virt2_versions_union": "|".join(v["version"] for v in FX["rmt"]["versions"]),
    "virt2_latest_tag": "1.0.0",
    "virt2_stable_tag_union": "2.0.0",
    "virt2_shadow_shasum_local_first": FX["shadow_loc"]["sha1"],
    "virt2_tarball_shadow_local_bytes": "200+sha256-ok",
}


def _leg(ctx, side):
    keys = npmlib.layout_keys(TAG)
    virt, virt2 = keys["virt"], keys["virt"] + "2"
    asserts, raw = {}, {}
    scratch = npmlib.scratch_dir()
    proxy = None
    try:
        npmlib.cleanup_layout(ctx, side, TAG, extra=[virt2, virt2 + "-cache"])
        if side == "b":
            src_reg = (npmlib.a_direct_base(ctx.sides["a"]["base"])
                       + "/api/npm/" + keys["src"])
            proxy = npmlib.start_layout_proxy(
                src_reg, ctx.sides["a"]["user"], ctx.sides["a"]["password"])
        npmlib.build_layout(ctx, side, scratch, FX, TAG, proxy)

        # baseline merge from the plain virtual
        resp = npmlib.get_packument(ctx, side, virt, RMT)
        sem1 = npmlib.semantic_packument(resp["body"])
        raw["virt_status"] = resp["status"]
        if "error" in sem1:
            asserts["virt_versions_union"] = "status=%d" % resp["status"]
            asserts["virt_latest_tag"] = "status=%d" % resp["status"]
        else:
            asserts["virt_versions_union"] = "|".join(sem1["versions"])
            asserts["virt_latest_tag"] = sem1["dist_tags"].get("latest") or "absent"

        # repeat read: semantic stability within the aggregation-cache window
        resp2 = npmlib.get_packument(ctx, side, virt, RMT)
        sem2 = npmlib.semantic_packument(resp2["body"])
        stable_keys = ("versions", "dist_tags") + tuple(
            "shasum_%s" % v["version"] for v in FX["rmt"]["versions"])
        asserts["virt_repeat_stable"] = "same" if all(
            sem1.get(k) == sem2.get(k) for k in stable_keys
        ) and "error" not in sem1 and "error" not in sem2 else "changed"

        # SLIM packument variant (npm ci posture)
        slim = npmlib.get_packument(ctx, side, virt, RMT,
                                    accept="application/vnd.npm.install-v1+json")
        semslim = npmlib.semantic_packument(slim["body"])
        raw["slim_content_type"] = slim["headers"].get("Content-Type") or \
            slim["headers"].get("content-type")
        asserts["slim_status"] = "status=%d" % slim["status"]
        asserts["slim_versions_union"] = (
            "|".join(semslim["versions"]) if "error" not in semslim
            else "parse-error")

        # explicit cache-member virtual: [loc, rem, <rem>-cache]
        put = ctx.http(side, "PUT", "/api/repositories/" + virt2,
                       body=json.dumps({
                           "rclass": "virtual", "packageType": "npm",
                           "repositories": [keys["loc"], keys["rem"],
                                            keys["rem"] + "-cache"]}),
                       headers={"Content-Type": "application/json"})
        raw["virt2_put_status"] = put["status"]
        raw["virt2_put_body"] = put["body"][:300].decode("utf-8", "replace")
        if 200 <= put["status"] < 300:
            resp3 = npmlib.get_packument(ctx, side, virt2, RMT)
            sem3 = npmlib.semantic_packument(resp3["body"])
            raw["virt2_status"] = resp3["status"]
            if "error" in sem3:
                asserts["virt2_versions_union"] = "status=%d" % resp3["status"]
                asserts["virt2_latest_tag"] = "status=%d" % resp3["status"]
                asserts["virt2_stable_tag_union"] = "status=%d" % resp3["status"]
                asserts["virt2_shadow_shasum_local_first"] = "status=%d" % resp3["status"]
            else:
                asserts["virt2_versions_union"] = "|".join(sem3["versions"])
                asserts["virt2_latest_tag"] = sem3["dist_tags"].get("latest") or "absent"
                asserts["virt2_stable_tag_union"] = sem3["dist_tags"].get("stable") or "absent"
            sh = npmlib.get_packument(ctx, side, virt2, SHADOW)
            semsh = npmlib.semantic_packument(sh["body"])
            asserts["virt2_shadow_shasum_local_first"] = (
                semsh.get("shasum_1.0.0") or "absent"
                if "error" not in semsh else "status=%d" % sh["status"])
            asserts["virt2_tarball_shadow_local_bytes"] = npmlib.tarball_verdict(
                ctx, side, virt2, SHADOW, FX["shadow_loc"]["filename"],
                FX["shadow_loc"]["tgz_sha256"])
        else:
            for k in ("virt2_versions_union", "virt2_latest_tag",
                      "virt2_stable_tag_union", "virt2_shadow_shasum_local_first"):
                asserts[k] = "rejected-put=%d" % put["status"]
            asserts["virt2_tarball_shadow_local_bytes"] = "rejected-put=%d" % put["status"]
    finally:
        npmlib.cleanup_layout(ctx, side, TAG, extra=[virt2, virt2 + "-cache"])
        if proxy:
            proxy[0].shutdown()
            proxy[0].server_close()
        npmlib.rm_scratch(scratch)
    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    scratch = npmlib.scratch_dir()
    extra = [npmlib.layout_keys(TAG)["virt"] + "2", npmlib.layout_keys(TAG)["virt"] + "2-cache"]
    try:
        # shared fake upstream on A spans both legs (round-1 lesson)
        npmlib.ensure_source_on_a(ctx, scratch, FX, TAG)
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except npmlib.SetupError as e:
        for s in ("a", "b"):
            npmlib.cleanup_layout(ctx, s, TAG, extra=extra)
        npmlib.drop_source_on_a(ctx, TAG)
        npmlib.rm_scratch(scratch)
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            npmlib.cleanup_layout(ctx, s, TAG, extra=extra)
        npmlib.drop_source_on_a(ctx, TAG)
        npmlib.rm_scratch(scratch)

    # judge with the conditional block: if either side rejected the explicit
    # cache member, the equivalence asserts collapse to the reject marker and
    # are only comparable when BOTH sides rejected.
    a, b = per_side.get("a", {}), per_side.get("b", {})
    if any(str(v).startswith("rejected-put") for v in a.values()) or \
       any(str(v).startswith("rejected-put") for v in b.values()):
        expected = dict(EXPECTED)
        if a.get("virt2_versions_union", "").startswith("rejected") and \
           b.get("virt2_versions_union", "").startswith("rejected") and \
           a["virt2_versions_union"] == b["virt2_versions_union"]:
            marker = a["virt2_versions_union"]
            for k in list(expected):
                if k.startswith("virt2_"):
                    expected[k] = marker
        else:
            expected = {k: v for k, v in EXPECTED.items()
                        if not k.startswith("virt2_")}
    else:
        expected = EXPECTED

    status, reason, mism = npmlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "seed; GET virt packument x2 (stability); "
                                  "SLIM accept; PUT virt2 [loc,rem,rem-cache]; "
                                  "GET virt2 packument+tarball"}]}
