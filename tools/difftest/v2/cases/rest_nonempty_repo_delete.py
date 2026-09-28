"""T-550 arm 1 (known-divergence rest/nonempty-repo-delete-cascade-guard,
L028 D-3): DELETE /api/repositories/{key} on a NON-EMPTY repo.

A-side live (7.161.26, captured fresh this batch): 200 + body
{"repoKey","statusMsg":"Repository '<key>' and all its content have been
removed successfully.","deletedArtifactsCount":N,"success":true} — silent
cascade, no confirmation semantics. B-side: 400 "repository is not empty …
retry with deleteContent=true"; the explicit flag then cascades (200).

The A/B divergence is the registered pending-ruling UNKNOWN; this case pins
both postures stably (r==r') and the post-conditions (repo gone on both).
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
    "id": "rest-nonempty-repo-delete-cascade",
    "title": "DELETE /api/repositories/{key} on a non-empty repo — "
             "A silent 200 cascade vs B 400 + deleteContent=true gate",
    "layer": "L2",
    "domain": "rest",
    "auth": True,
    "timeout_s": 60,
}

LOCAL = "difftest-r4t550-a1"
GROUP, ART, VER = "com.diff", "casc", "1.0.0"
EXPECT_A = {
    "a_delete_nonempty": "200+cascade",
    "a_repo_gone": "status=400",  # live 7.161.26: repo GET for a missing key
    # answers 400 {"errors":[{"status":400,"message":"Bad Request"}]} — B
    # mirrors this exactly (probe 2026-09-28, deleted AND never-existed keys)
}
EXPECT_B = {
    "b_delete_nonempty": "400+deleteContent-hint",
    "b_delete_forced": "status=200",
    "b_repo_gone": "status=400",
}


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [mavenlib.maven_local(LOCAL)])
    put = mavenlib.put_pom(ctx, side, LOCAL, GROUP, ART, VER)
    raw["put_pom"] = put["status"]

    d = ctx.http(side, "DELETE", "/api/repositories/" + LOCAL)
    raw["delete_status"] = d["status"]
    raw["delete_body"] = d["body"][:400].decode("utf-8", "replace")
    if side == "a":
        body = raw["delete_body"]
        ok_cascade = ("removed successfully" in body) or ("success" in body)
        asserts["a_delete_nonempty"] = "200+cascade" if (
            d["status"] == 200 and ok_cascade) else "status=%d" % d["status"]
    else:
        hint = "deleteContent" in raw["delete_body"]
        asserts["b_delete_nonempty"] = "400+deleteContent-hint" if (
            d["status"] == 400 and hint) else "status=%d" % d["status"]
        if d["status"] != 200:
            d2 = ctx.http(side, "DELETE",
                          "/api/repositories/%s?deleteContent=true" % LOCAL)
            raw["delete_forced_status"] = d2["status"]
            asserts["b_delete_forced"] = "status=%d" % d2["status"]

    g = ctx.http(side, "GET", "/api/repositories/" + LOCAL)
    raw["repo_get_after"] = g["status"]
    key = "a_repo_gone" if side == "a" else "b_repo_gone"
    asserts[key] = "status=%d" % g["status"]

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [LOCAL])
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [LOCAL])

    expected = dict(EXPECT_A)
    expected.update(EXPECT_B)
    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "the a-vs-b posture gap IS the registered pending-ruling "
                "divergence; PASS here means each side stably meets its own "
                "observed posture and both clean up to repo-gone"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "create local; PUT pom; DELETE repo; "
                                  "(b) DELETE ?deleteContent=true; GET repo"}]}
