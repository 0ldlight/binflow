"""T-550 arm 2 (known-divergence rest/kcache-deploy-404-wording): the deploy
reject family when the target is not a deployable local repository.

Spec anchors (docs/reverse/repo-semantics.md §8.2 + remote-cache-projection
§2.1, high confidence):
  - PUT /<K>-cache/<path> (the auto-derived remote cache projection) and
    PUT /<K>/<path> (a remote itself) both answer 404 "Could not find a
    local repository named <key> to deploy to." — A-side live 7.161.26
    captured verbatim this batch.
  - PUT /<virtual>/<path> without defaultDeploymentRepo answers 405 +
    Allow: GET + "No local repository was configured as local deployment
    repository for the (<key>) virtual repository." — A/B identical live.

B-side live: the <K>-cache leg keeps the generic repo-lookup wording
("Failed to find the repository '<K>-cache' specified in the request.")
— status equal, wording family different (the registered BUG); the remote
leg answers 405 read-only-proxy instead of the 404 family (newly evidenced
this batch, adjudication in the report).
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
    "id": "rest-kcache-deploy-404-wording",
    "title": "PUT deploy reject family: /<K>-cache + /<K> remote (404 "
             "wording) and /<virtual> no-default (405)",
    "layer": "L5",
    "domain": "rest",
    "auth": True,
    "timeout_s": 60,
}

TAG = "r4t550w"
LOCAL, VIRT, REMOTE = ("difftest-%s-loc" % TAG, "difftest-%s-virt" % TAG,
                       "difftest-%s-rem" % TAG)
GROUP, ART, VER = "com.diff", "wd", "1.0.0"
POM_PATH = "/%s/%s/%s/%s-%s.pom" % (GROUP.replace(".", "/"), ART, VER, ART, VER)
POM = mavenlib.pom_fixture(GROUP, ART, VER)  # GAV-consistent with the path

SPEC_404 = "Could not find a local repository named"
SPEC_405 = "No local repository was configured as local deployment repository"

EXPECTED = {
    "put_kcache_status": "status=404",
    "put_kcache_wording": SPEC_404,
    "put_remote_status": "status=404",
    "put_remote_wording": SPEC_404,
    "put_virt_status": "status=405",
    "put_virt_wording": SPEC_405,
    "put_virt_allow": "GET",
}


def _wording(body: bytes, *needles) -> str:
    text = body.decode("utf-8", "replace")
    for n in needles:
        if n in text:
            return n
    return "other"


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL),
        mavenlib.maven_virtual(VIRT, [LOCAL]),
    ])
    # remote K: never fetched on a rejected deploy; url is pro-forma
    mavenlib.repo_put(ctx, side, REMOTE, {
        "rclass": "remote", "packageType": "maven",
        "url": "https://repo1.maven.org/maven2/",
        "allowPrivateUpstream": True,
    })
    hdrs = {"Content-Type": "application/xml"}

    pk = ctx.http(side, "PUT", "/%s-cache%s" % (REMOTE, POM_PATH), body=POM,
                  headers=hdrs)
    raw["put_kcache_body"] = pk["body"][:400].decode("utf-8", "replace")
    asserts["put_kcache_status"] = "status=%d" % pk["status"]
    asserts["put_kcache_wording"] = _wording(pk["body"], SPEC_404)

    pr = ctx.http(side, "PUT", "/%s%s" % (REMOTE, POM_PATH), body=POM,
                  headers=hdrs)
    raw["put_remote_body"] = pr["body"][:400].decode("utf-8", "replace")
    asserts["put_remote_status"] = "status=%d" % pr["status"]
    asserts["put_remote_wording"] = _wording(pr["body"], SPEC_404)

    pv = ctx.http(side, "PUT", "/%s%s" % (VIRT, POM_PATH), body=POM,
                  headers=hdrs)
    raw["put_virt_body"] = pv["body"][:400].decode("utf-8", "replace")
    asserts["put_virt_status"] = "status=%d" % pv["status"]
    asserts["put_virt_wording"] = _wording(pv["body"], SPEC_405)
    allow = pv["headers"].get("Allow") or pv["headers"].get("allow") or "absent"
    raw["put_virt_allow"] = allow
    asserts["put_virt_allow"] = allow

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    keys = [VIRT, LOCAL, REMOTE]
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, keys)
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, keys + [REMOTE + "-cache"])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "FAIL is the expected outcome while the registered BUG "
                "(kcache wording) and the remote-put leg diverge; the 405 "
                "virtual leg is expected PASS on both sides"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "create local+virt+remote; PUT gav-ok pom "
                                  "to /<K>-cache, /<K>, /<virt>"}]}
