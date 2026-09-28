"""T-533 / BIN-16 arm 2 (D-4 reverse case): POM content GAV vs deploy path
GAV mismatch — upload-side validation, dual send, register only (no fix).

L029 registered D-4: the A side answers a GAV-inconsistent pom deploy with
409. Spec anchor docs/reverse/virtual-resolution.md §5.2 upload note: a pom
deployed through a virtual into defaultDeploymentRepo goes through the
pom-coordinates vs target-path consistency check; mismatch is rejected
unless suppressPomConsistencyChecks is set (repo knob, default off). BinFlow
renders the knob in its repo config surface but no enforcement exists in
internal/ (pre-run hypothesis; the wire decides).

Legs (both through the virtual -> defaultDeploymentRepo routing):
  1. control: consistent pom PUT -> 201 (sanity: no false positive);
  2. mismatch PUT: pom CONTENT says com.diff:right-lib:1.0.0 while the
     PATH says com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom -> expect 409
     with an errors[]-shaped body; the artifact must NOT be stored (GET 404);
  3. real-client mismatch: mvn deploy:deploy-file with -DartifactId=wrong-lib
     but -DpomFile carrying right-lib content -> expect non-zero exit and
     the path to stay absent (GET 404).

Error bodies are captured to evidence (sha256 + first 240 chars, no
credentials ever appear in server error envelopes); only the status shape
is asserted so legitimate message-text differences do not mask the
divergence being measured.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-deploy-gav-mismatch",
    "title": "POM content GAV vs path GAV mismatch through virtual — "
             "409 consistency rejection (D-4 reverse case)",
    "layer": "L3",
    "domain": "maven",
    "auth": True,
    "timeout_s": 300,
}

LOCAL, VIRT = "difftest-mvn-gavl", "difftest-mvn-gavv"
GROUP = "com.diff"
OK_ART, BAD_ART, VER = "gav-lib", "wrong-lib", "1.0.0"
GDIR_OK = "/%s/%s" % (GROUP.replace(".", "/"), OK_ART)
GDIR_BAD = "/%s/%s" % (GROUP.replace(".", "/"), BAD_ART)

POM_OK = mavenlib.pom_fixture(GROUP, OK_ART, VER)
POM_MISMATCH = mavenlib.pom_fixture(GROUP, "right-lib", VER)  # content GAV
JAR = mavenlib.jar_fixture("gav-mismatch")

EXPECTED = {
    "put_consistent_status": "201",
    "put_mismatch_status": "409",
    "put_mismatch_get": "404",
    "mvn_mismatch_exit": "nonzero",
    "mvn_mismatch_path_get": "404",
}


def _put_pom(ctx, side, path, body):
    return ctx.http(side, "PUT", path, body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": __import__("hashlib").md5(body).hexdigest(),
    })


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL),
        mavenlib.maven_virtual(VIRT, [LOCAL], defaultDeploymentRepo=LOCAL),
    ])
    work = mavenlib.scratch_dir()
    try:
        # 1. control: consistent pom via virtual -> defaultDeploymentRepo
        ok_path = "/%s%s/%s/%s-%s.pom" % (VIRT, GDIR_OK, VER, OK_ART, VER)
        resp = _put_pom(ctx, side, ok_path, POM_OK)
        raw["put_consistent"] = {"path": ok_path, "status": resp["status"]}
        asserts["put_consistent_status"] = str(resp["status"])

        # 2. mismatch PUT: content GAV != path GAV
        bad_path = "/%s%s/%s/%s-%s.pom" % (VIRT, GDIR_BAD, VER, BAD_ART, VER)
        resp = _put_pom(ctx, side, bad_path, POM_MISMATCH)
        excerpt = resp["body"].decode("utf-8", "replace")[:240]
        raw["put_mismatch"] = {
            "path": bad_path, "status": resp["status"],
            "body_sha256": mavenlib.sha256_hex(resp["body"]),
            "body_excerpt": excerpt,
        }
        asserts["put_mismatch_status"] = str(resp["status"])
        gets = ctx.http(side, "GET", "/%s%s" % (LOCAL, GDIR_BAD.replace(BAD_ART, "right-lib")))
        # the REJECTED path itself must stay absent on the member:
        get_path = ctx.http(side, "GET", bad_path)
        raw["put_mismatch_get"] = {"via_virtual": get_path["status"],
                                   "member_wronglib": gets["status"]}
        asserts["put_mismatch_get"] = str(get_path["status"])

        # 3. real-client leg: mvn deploy-file with path GAV != pom GAV
        jar_p = os.path.join(work, "m.jar")
        pom_p = os.path.join(work, "m.pom")
        with open(jar_p, "wb") as fh:
            fh.write(JAR)
        with open(pom_p, "wb") as fh:
            fh.write(POM_MISMATCH)
        url = "%s/%s" % (ctx.sides[side]["base"], VIRT)
        out = mavenlib.mvn_deploy_file(
            ctx, side, url, work, GROUP, BAD_ART, VER, jar_p, pom_p)
        raw["mvn_mismatch"] = out
        asserts["mvn_mismatch_exit"] = "zero" if out["exit"] == "0" else "nonzero"
        mvn_get = ctx.http(side, "GET", bad_path)
        raw["mvn_mismatch_path_get"] = mvn_get["status"]
        asserts["mvn_mismatch_path_get"] = str(mvn_get["status"])
    finally:
        mavenlib.rm_scratch(work)
        mavenlib.cleanup_repos(ctx, side, [VIRT, LOCAL])
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

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "PUT consistent pom (control), PUT GAV-mismatch"
                                  " pom, mvn deploy-file GAV mismatch; GET paths"}]}
