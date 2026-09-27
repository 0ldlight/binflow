"""T-523 case 4: DELETE through a virtual must NOT touch member artifacts.

Behavior asserted (spec: docs/reverse/repo-semantics.md §8.2 L220 +
virtual-resolution.md §7.5, high confidence, erratum of older §8.2 text):
  - DELETE /{virtual}/{path} only acts on the virtual's own aggregate cache
    storage; with no such entry it returns 404 (ITEM_NOT_FOUND) and the
    member's physical artifact survives;
  - contrast: DELETE on the local member itself returns 204 and afterwards
    the virtual no longer resolves the path (404).
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-virtual-delete-passthrough",
    "title": "DELETE via virtual -> 404 (member artifact survives); direct "
             "member DELETE -> 204; virtual then 404",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 45,
}

LOCAL, VIRT = "difftest-mvn-local4", "difftest-mvn-virt4"
GROUP, ART, VER = "com.diff", "del-lib", "1.0.0"
POM = mavenlib.pom_fixture(GROUP, ART, VER)
POM_PATH = "/%s/%s/%s/%s-%s.pom" % (GROUP.replace(".", "/"), ART, VER, ART, VER)

EXPECTED = {
    "put_pom_on_member": "201",
    "virt_resolve_before": "200+sha256-ok",
    "delete_via_virtual": "404",
    "member_artifact_survives": "200",
    "direct_member_delete": "204",
    "virt_resolve_after": "404",
}


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL),
        mavenlib.maven_virtual(VIRT, [LOCAL]),
    ])

    put = mavenlib.put_pom(ctx, side, LOCAL, GROUP, ART, VER)
    raw["put_status"] = put["status"]
    asserts["put_pom_on_member"] = "status=%d" % put["status"]

    asserts["virt_resolve_before"] = mavenlib.get_sha_verdict(
        ctx, side, "/%s%s" % (VIRT, POM_PATH), mavenlib.sha256_hex(POM))

    del_virt = ctx.http(side, "DELETE", "/%s%s" % (VIRT, POM_PATH))
    raw["delete_via_virtual_status"] = del_virt["status"]
    asserts["delete_via_virtual"] = "status=%d" % del_virt["status"]

    member = ctx.http(side, "GET", "/%s%s" % (LOCAL, POM_PATH))
    raw["member_after_virt_delete_status"] = member["status"]
    asserts["member_artifact_survives"] = "status=%d" % member["status"]

    del_local = ctx.http(side, "DELETE", "/%s%s" % (LOCAL, POM_PATH))
    raw["direct_member_delete_status"] = del_local["status"]
    asserts["direct_member_delete"] = "status=%d" % del_local["status"]

    after = ctx.http(side, "GET", "/%s%s" % (VIRT, POM_PATH))
    raw["virt_after_member_delete_status"] = after["status"]
    asserts["virt_resolve_after"] = "status=%d" % after["status"]

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, LOCAL])
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, LOCAL])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "put-pom, virt-resolve, DELETE-via-virt, "
                                  "member-check, DELETE-member, virt-recheck"}]}
