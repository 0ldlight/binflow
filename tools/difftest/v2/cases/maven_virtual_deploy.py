"""T-523 case 1: mvn deploy (RELEASE + SNAPSHOT) through a virtual whose
defaultDeploymentRepo points at a local member.

Behavior asserted (spec: docs/reverse/):
  - repo-semantics.md §8.2 L217: PUT via virtual with defaultDeploymentRepo
    routes to that local member -> mvn deploy:deploy-file exits 0.
  - maven-npm-pypi.md §1.1: PUT deploy -> 201; GET -> 200.
  - maven-npm-pypi.md §1.3: local repo snapshotVersionBehavior=unique ->
    snapshot lands as <baseRev>-<yyyyMMdd.HHmmss>-<N>.<ext> under the
    <baseRev>-SNAPSHOT folder.
  - maven-npm-pypi.md §1.4/§1.5: version-dir + group-dir maven-metadata.xml
    are server-calculated (release jar async, unique snapshot sync) with
    versions/latest/release per Maven comparator; GET <file>.sha1 on a local
    repo returns the server-computed value.
  - virtual-resolution.md §5.2: fixture pom has no <repositories>, so the
    PomInterceptor (default discard_active_reference) returns it verbatim.
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
    "id": "maven-virtual-deploy",
    "title": "mvn deploy RELEASE+SNAPSHOT via virtual (defaultDeploymentRepo) "
             "— mvn exit, checksum, landed path, maven-metadata.xml",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 300,
}

GROUP, ART = "com.diff", "difftest-deploy"
REL, SNAP = "1.0.0", "1.0.1-SNAPSHOT"
LOCAL, VIRT = "difftest-mvn-local", "difftest-mvn-virt"

POM_REL = mavenlib.pom_fixture(GROUP, ART, REL)
POM_SNAP = mavenlib.pom_fixture(GROUP, ART, SNAP)
JAR_REL = mavenlib.jar_fixture("deploy-release-1.0.0")
JAR_SNAP = mavenlib.jar_fixture("deploy-snapshot-1.0.1")
GDIR = "/%s/%s" % (GROUP.replace(".", "/"), ART)

EXPECTED = {
    "deploy_release_mvn_exit": "0",
    "deploy_snapshot_mvn_exit": "0",
    "release_pom_via_virtual": "200+sha256-ok",
    "release_jar_on_local": "200+sha256-ok",
    "release_jar_sha1_endpoint": "200+match",
    "snapshot_version_metadata": "200 fields_ok",
    "snapshot_versions_v3": "pom+jar",
    "snapshot_landed_form": "timestamped",
    "snapshot_jar_content": "200+sha256-ok",
    "group_metadata": "versions+latest+release_ok",
}


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL, snapshotVersionBehavior="unique"),
        mavenlib.maven_virtual(VIRT, [LOCAL], defaultDeploymentRepo=LOCAL),
    ])

    work = mavenlib.scratch_dir()
    try:
        def _wf(name, data):
            p = os.path.join(work, name)
            with open(p, "wb") as fh:
                fh.write(data)
            return p

        base = ctx.sides[side]["base"]
        url = "%s/%s" % (base, VIRT)
        rel = mavenlib.mvn_deploy_file(
            ctx, side, url, work, GROUP, ART, REL,
            _wf("rel.jar", JAR_REL), _wf("rel.pom", POM_REL))
        snap = mavenlib.mvn_deploy_file(
            ctx, side, url, work, GROUP, ART, SNAP,
            _wf("snap.jar", JAR_SNAP), _wf("snap.pom", POM_SNAP))
        raw["mvn"] = {"release": rel, "snapshot": snap}
        asserts["deploy_release_mvn_exit"] = rel["exit"]
        asserts["deploy_snapshot_mvn_exit"] = snap["exit"]

        # --- release leg: exact bytes served back through the virtual/member
        asserts["release_pom_via_virtual"] = mavenlib.get_sha_verdict(
            ctx, side, "%s/%s/%s-%s.pom" % (GDIR, REL, ART, REL),
            mavenlib.sha256_hex(POM_REL))
        jar_rel_path = "%s/%s/%s-%s.jar" % (GDIR, REL, ART, REL)
        asserts["release_jar_on_local"] = mavenlib.get_sha_verdict(
            ctx, side, "/%s%s" % (LOCAL, jar_rel_path),
            mavenlib.sha256_hex(JAR_REL))
        sha1_resp = ctx.http(side, "GET", "/%s%s.sha1" % (LOCAL, jar_rel_path))
        if sha1_resp["status"] != 200:
            asserts["release_jar_sha1_endpoint"] = "status=%d" % sha1_resp["status"]
        else:
            want = mavenlib.sha1_hex(JAR_REL)
            asserts["release_jar_sha1_endpoint"] = (
                "200+match" if sha1_resp["body"].decode().strip() == want
                else "200+mismatch")

        # --- snapshot leg: version-dir metadata (poll: async calc, §1.4)
        def _snap_meta_ok(status, body):
            if status != 200:
                return False
            try:
                return bool(mavenlib.parse_metadata(body)["snapshot_versions"])
            except Exception:  # noqa: BLE001 - poll predicate must not raise
                return False

        status, body = mavenlib.poll_until(
            ctx, side, "%s/%s/maven-metadata.xml" % (GDIR, SNAP), _snap_meta_ok)
        raw["snapshot_metadata_status"] = status
        if status != 200:
            asserts["snapshot_version_metadata"] = "status=%d" % status
            asserts["snapshot_versions_v3"] = "absent"
            asserts["snapshot_landed_form"] = "unknown"
            asserts["snapshot_jar_content"] = "unknown"
        else:
            md = mavenlib.parse_metadata(body)
            raw["snapshot_metadata"] = md
            fields_ok = (md["group_id"] == GROUP and md["artifact_id"] == ART
                         and md["version"] == SNAP
                         and md["snapshot"].get("buildNumber", "").isdigit()
                         and int(md["snapshot"]["buildNumber"]) >= 1)
            asserts["snapshot_version_metadata"] = (
                "200 fields_ok" if fields_ok else "200 fields_bad:%s" % json.dumps(
                    {"g": md["group_id"], "a": md["artifact_id"],
                     "v": md["version"], "snap": md["snapshot"]}, sort_keys=True))
            exts = {sv.get("extension") for sv in md["snapshot_versions"]}
            asserts["snapshot_versions_v3"] = (
                "pom+jar" if {"pom", "jar"} <= exts
                else "missing:%s" % ",".join(sorted({"pom", "jar"} - exts)))
            jar_value = next((sv["value"] for sv in md["snapshot_versions"]
                              if sv.get("extension") == "jar"), None)
            if jar_value is None:
                asserts["snapshot_landed_form"] = "no_jar_entry"
                asserts["snapshot_jar_content"] = "no_jar_entry"
            else:
                asserts["snapshot_landed_form"] = mavenlib.snapshot_form(jar_value)
                asserts["snapshot_jar_content"] = mavenlib.get_sha_verdict(
                    ctx, side, "/%s%s/%s" % (LOCAL, GDIR, SNAP) + "/" + jar_value,
                    mavenlib.sha256_hex(JAR_SNAP))

        # --- group-level metadata (poll: pom upload -> async, §1.4)
        def _group_ok(status, body):
            if status != 200:
                return False
            try:
                return {REL, SNAP} <= set(mavenlib.parse_metadata(body)["versions"])
            except Exception:  # noqa: BLE001
                return False

        status, body = mavenlib.poll_until(
            ctx, side, "%s/maven-metadata.xml" % GDIR, _group_ok)
        raw["group_metadata_status"] = status
        if status != 200:
            asserts["group_metadata"] = "status=%d" % status
        else:
            md = mavenlib.parse_metadata(body)
            raw["group_metadata"] = md
            want = [REL, SNAP]  # Maven comparator order: 1.0.0 < 1.0.1-SNAPSHOT
            problems = []
            if md["versions"] != want:
                problems.append("versions=%s" % md["versions"])
            if md["latest"] != SNAP:
                problems.append("latest=%r" % md["latest"])
            if md["release"] != REL:
                problems.append("release=%r" % md["release"])
            asserts["group_metadata"] = (
                "versions+latest+release_ok" if not problems else ";".join(problems))
    finally:
        mavenlib.rm_scratch(work)
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
            "requests": [{"step": "mvn-deploy-release+snapshot-via-virtual"}]}
