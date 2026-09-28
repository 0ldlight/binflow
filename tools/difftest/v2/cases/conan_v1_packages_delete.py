"""T-550 arm 4 (known-divergence conan/v1-packages-delete-virtual-plain-404):
POST conan v1 conans/<ref>/packages/delete against a VIRTUAL repository.

A-side live (7.161.26, fresh this batch): 400 + errors[] envelope
"Unsupported Conan v1 repository request for '<key>'" — NOT a 404 and NOT
a plain body. This settles the ledger's open question ("A 臂实形未知"):
the proposed plain->envelope-404 alignment would still diverge (status AND
carrier AND wording family).

B-side: a stock BinFlow binary stays on the community license floor and
gates the conan package type at repo creation (live captured: 400
"package type 'conan' is not available (license tier 'community' < 'pro')"),
so the v1 virtual plane is unreachable on this B instance. The licensed
posture (serveVirtual default -> writePlain 404 "not found",
internal/adapter/conan/virtual.go default branch + handler.go writePlain)
is cited from the tree, not live-measured; adjudication input lives in the
batch report.

Plane note (harness adaptation, not a product divergence): A serves conan
v1 under /api/conan/<repo>/v1/... while B mounts it on the content plane
/<repo>/v1/... (apiProtocolMounts has no conan slot) — each leg addresses
its side's own plane spelling.
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "conan-v1-pkgdel-virtual",
    "title": "conan v1 POST conans/<ref>/packages/delete on a virtual — "
             "A 400 'Unsupported Conan v1 repository request' envelope vs "
             "B stock license gate / licensed plain-404",
    "layer": "L5",
    "domain": "conan",
    "auth": True,
    "timeout_s": 45,
}

TAG = "r4t550c"
CLOC, CVIRT = "difftest-%s-loc" % TAG, "difftest-%s-virt" % TAG
REF = "hello/1.0/difftest/stable"
A_MSG = "Unsupported Conan v1 repository request"
B_GATE = "license tier 'community' < 'pro'"


def _plane_path(side, repo):
    if side == "a":
        return "/api/conan/%s/v1/conans/%s/packages/delete" % (repo, REF)
    return "/%s/v1/conans/%s/packages/delete" % (repo, REF)


def _leg(ctx, side):
    asserts, raw = {}, {}
    rl = ctx.http(side, "PUT", "/api/repositories/" + CLOC,
                  body=json.dumps({"rclass": "local", "packageType": "conan"}),
                  headers={"Content-Type": "application/json"})
    raw["create_local_status"] = rl["status"]
    raw["create_local_body"] = rl["body"][:300].decode("utf-8", "replace")
    rv = ctx.http(side, "PUT", "/api/repositories/" + CVIRT,
                  body=json.dumps({"rclass": "virtual", "packageType": "conan",
                                   "repositories": [CLOC]}),
                  headers={"Content-Type": "application/json"})
    raw["create_virt_status"] = rv["status"]
    raw["create_virt_body"] = rv["body"][:300].decode("utf-8", "replace")
    conan_live = 200 <= rv["status"] < 300

    pd = ctx.http(side, "POST", _plane_path(side, CVIRT),
                  body=json.dumps({"package_ids": ["5ab84d6ac45f007deef0"]}),
                  headers={"Content-Type": "application/json"})
    raw["pkgdel_status"] = pd["status"]
    raw["pkgdel_body"] = pd["body"][:400].decode("utf-8", "replace")
    raw["pkgdel_ct"] = pd["headers"].get("Content-Type") or \
        pd["headers"].get("content-type")

    if side == "a":
        asserts["a_v1pkgdel"] = "400+%s" % A_MSG if (
            pd["status"] == 400 and A_MSG in raw["pkgdel_body"]
        ) else "status=%d" % pd["status"]
        asserts["a_v1pkgdel_carrier"] = "errors-envelope" if (
            '"errors"' in raw["pkgdel_body"]) else "other"
    else:
        if conan_live:
            asserts["b_v1pkgdel"] = "status=%d" % pd["status"]
            asserts["b_v1pkgdel_carrier"] = "plain" if (
                raw["pkgdel_body"].strip() == "not found") else "other"
        else:
            asserts["b_conan_gate"] = "400+license-gate" if (
                rv["status"] == 400 and B_GATE in raw["create_virt_body"]
            ) else "status=%d" % rv["status"]
            asserts["b_v1pkgdel"] = "plane-unreachable(license-gate)"
    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts, conan_live


def run(ctx):
    per_side, live = {}, {}
    try:
        for side in ("a", "b"):
            per_side[side], live[side] = _leg(ctx, side)
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [CVIRT, CLOC])

    if live.get("b"):
        expected = {"a_v1pkgdel": per_side["a"].get("a_v1pkgdel"),
                    "a_v1pkgdel_carrier": "errors-envelope",
                    "b_v1pkgdel": per_side["a"].get("a_v1pkgdel"),
                    "b_v1pkgdel_carrier": "errors-envelope"}
        note = "licensed B live: dual compare against the A envelope"
    else:
        expected = {"a_v1pkgdel": "400+%s" % A_MSG,
                    "a_v1pkgdel_carrier": "errors-envelope",
                    "b_conan_gate": "400+license-gate",
                    "b_v1pkgdel": "plane-unreachable(license-gate)"}
        note = ("stock B community floor: the v1 virtual plane is "
                "unreachable live; the licensed posture (plain 404) stays "
                "code-cited — dual adjudication lives in the batch report")

    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism, "note": note})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "create conan local+virt; POST v1 "
                                  "packages/delete on the virtual"}]}
