"""T-523 case 3: two local members hold different versions of the same GAV;
GET the virtual's group-level maven-metadata.xml and assert the merge.

Behavior asserted (spec: docs/reverse/virtual-resolution.md §5.1):
  - versions = union of members, re-sorted with the Maven version comparator;
  - latest recomputed = sorted last (SNAPSHOT counts);
  - release recomputed = last non-SNAPSHOT;
  - merge result is computed per request and never written to <virtual>-cache
    (contrast with npm §6) -> GET <virtual>-cache/<path> stays 404.
Member metadata itself is server-calculated after pom PUT
(maven-npm-pypi.md §1.4: pom upload -> group-dir metadata async) so each
member's metadata is polled before reading the virtual view.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-virtual-metadata-merge",
    "title": "virtual maven-metadata.xml merge — versions union, "
             "latest/release recompute, no virtual-cache write",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 120,
}

L1, L2, VIRT = "difftest-mvn-local1", "difftest-mvn-local2", "difftest-mvn-virt3"
GROUP, ART = "com.diff", "merge-lib"
V_L1 = ["1.0.0", "1.2.0"]
V_L2 = ["1.1.0", "2.0.0", "3.0.0-SNAPSHOT"]
GDIR = "/%s/%s" % (GROUP.replace(".", "/"), ART)
EXPECTED_ORDER = ["1.0.0", "1.1.0", "1.2.0", "2.0.0", "3.0.0-SNAPSHOT"]

EXPECTED = {
    "versions_union_order": "|".join(EXPECTED_ORDER),
    "latest_recomputed": "3.0.0-SNAPSHOT",
    "release_recomputed": "2.0.0",
    "virtual_cache_not_merged": "404",
}


def _member_versions_ok(status, body, want):
    if status != 200:
        return False
    try:
        return set(want) <= set(mavenlib.parse_metadata(body)["versions"])
    except Exception:  # noqa: BLE001 - poll predicate must not raise
        return False


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(L1),
        mavenlib.maven_local(L2),
        mavenlib.maven_virtual(VIRT, [L1, L2]),  # declaration order: L1 base
    ])
    for repo, versions in ((L1, V_L1), (L2, V_L2)):
        for v in versions:
            put = mavenlib.put_pom(ctx, side, repo, GROUP, ART, v)
            raw.setdefault("puts", []).append(
                {"repo": repo, "version": v, "status": put["status"]})

    for repo, want in ((L1, V_L1), (L2, V_L2)):
        status, body = mavenlib.poll_until(
            ctx, side, "/%s%s/maven-metadata.xml" % (repo, GDIR),
            lambda s, b, w=want: _member_versions_ok(s, b, w))
        raw.setdefault("member_metadata_status", {})[repo] = status

    status, body = mavenlib.poll_until(
        ctx, side, "/%s%s/maven-metadata.xml" % (VIRT, GDIR),
        lambda s, b: _member_versions_ok(s, b, EXPECTED_ORDER))
    raw["virtual_metadata_status"] = status
    if status != 200:
        asserts["versions_union_order"] = "status=%d" % status
        asserts["latest_recomputed"] = "status=%d" % status
        asserts["release_recomputed"] = "status=%d" % status
    else:
        md = mavenlib.parse_metadata(body)
        raw["virtual_metadata"] = md
        asserts["versions_union_order"] = "|".join(md["versions"])
        asserts["latest_recomputed"] = md["latest"] or "absent"
        asserts["release_recomputed"] = md["release"] or "absent"

    cache = ctx.http(side, "GET", "/%s-cache%s/maven-metadata.xml" % (VIRT, GDIR))
    raw["virtual_cache_probe_status"] = cache["status"]
    asserts["virtual_cache_not_merged"] = "status=%d" % cache["status"]

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, L2, L1])
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, L2, L1])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "put-poms-2-members, poll member metadata, "
                                  "GET virtual metadata, probe virtual-cache"}]}
