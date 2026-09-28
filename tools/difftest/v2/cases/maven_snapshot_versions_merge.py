"""T-533 / BIN-16 arm 1: virtual merge of <snapshotVersions> — conditional
(A-side claim) vs unconditional (BinFlow mergeSnapshotVersioning).

Construction (real client): mvn deploy:deploy-file of a SNAPSHOT GAV
directly into two local members of a virtual — once into L1 (buildNumber 1)
and twice into L2 (buildNumber 2, strictly newer) — then GET the virtual's
version-level maven-metadata.xml under two client User-Agents:

  - M3-capable UA "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)":
    spec docs/reverse/virtual-resolution.md §5.1 row 4 — merge is allowed
    (system flag mvn.metadata.version3.enabled defaults on + snapshot-level
    path + client supports M3 markers) -> snapshotVersions merged per
    (classifier x extension) taking the newer (L2, bn 2 > 1); the
    <snapshot> block takes the larger buildNumber (also L2).
  - M3-incapable UA "Java/1.8.0_391" (full-match of Artifactory's
    [Jj]ava/(.+) java-agent pattern, RequestResponseHelper.
    clientSupportsM3SnapshotVersions == false): §5.1 — "否则首个基底的
    snapshotVersions 被剥除" -> the merged document carries NO
    <snapshotVersions> while the <snapshot> block survives.

Member-level control leg: GET L1's own version-level metadata with the
incapable UA — the same M3 predicate also filters snapshotVersions on
plain local-repo metadata (decompile anchor: DbStoringRepoMixin.
resolveMavenMetadataForCompatibility; behavior-form restatement only).

Assertion values are deliberately side-RELATIVE (bn digits, winner member
id, extension sets) so cross-side timestamp differences cannot fake an
A/B divergence.
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
    "id": "maven-snapshot-versions-merge",
    "title": "virtual version-level metadata <snapshotVersions> merge — "
             "M3-capable vs java-agent User-Agent (conditional per §5.1)",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 600,
}

L1, L2, VIRT = "difftest-mvn-svl1", "difftest-mvn-svl2", "difftest-mvn-svv"
GROUP, ART = "com.diff", "sv-merge-lib"
SNAP = "1.0.0-SNAPSHOT"
GDIR = "/%s/%s" % (GROUP.replace(".", "/"), ART)
VMETA = "/%s%s/%s/maven-metadata.xml" % (VIRT, GDIR, SNAP)
L1META = "/%s%s/%s/maven-metadata.xml" % (L1, GDIR, SNAP)
L2META = "/%s%s/%s/maven-metadata.xml" % (L2, GDIR, SNAP)

UA_CAPABLE = "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)"
UA_INCAPABLE = "Java/1.8.0_391"

EXPECTED = {
    "deploy_l1_exit": "0",
    "deploy_l2a_exit": "0",
    "deploy_l2b_exit": "0",
    "member_l1_bn": "1",
    "member_l2_bn": "2",
    "virt_m3cap_sv": "present",
    "virt_m3cap_exts": "jar+pom",  # sorted() slug: jar < pom
    "virt_m3cap_winner": "l2",
    "virt_m3cap_snapshot_bn": "2",
    "virt_m3no_sv": "stripped",
    "virt_m3no_snapshot_bn": "2",
    "member_m3no_sv": "stripped",
}


def _get_meta(ctx, side, path, ua):
    resp = ctx.http(side, "GET", path, headers={"User-Agent": ua})
    if resp["status"] != 200:
        return {"status": resp["status"], "md": None}
    return {"status": 200, "md": mavenlib.parse_metadata(resp["body"])}


def _sv_facts(md):
    """(present, exts-slug, values-by-ext, snapshot buildNumber)."""
    svs = md["snapshot_versions"]
    exts = sorted({sv.get("extension") for sv in svs})
    vals = {sv.get("extension"): sv.get("value") for sv in svs}
    bn = (md["snapshot"].get("buildNumber") or "?") if md["snapshot"] else "none"
    return ("present" if svs else "stripped",
            "+".join(e for e in exts if e) or "none", vals, bn)


def _winner(vals, l1_vals, l2_vals):
    hits = set()
    for ext, v in vals.items():
        if v is not None and v == l1_vals.get(ext):
            hits.add("l1")
        if v is not None and v == l2_vals.get(ext):
            hits.add("l2")
    if not vals:
        return "none"
    if hits == {"l2"}:
        return "l2"
    if hits == {"l1"}:
        return "l1"
    return "mixed:%s" % ",".join(sorted(hits)) if hits else "unknown"


def _poll_member_bn(ctx, side, path, want_bn):
    def _ok(status, body):
        if status != 200:
            return False
        try:
            md = mavenlib.parse_metadata(body)
            return (md["snapshot"].get("buildNumber", "").isdigit()
                    and int(md["snapshot"]["buildNumber"]) >= want_bn)
        except Exception:  # noqa: BLE001 - poll predicate must not raise
            return False
    status, body = mavenlib.poll_until(ctx, side, path, _ok, budget_s=30.0)
    return status, body


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(L1, snapshotVersionBehavior="unique"),
        mavenlib.maven_local(L2, snapshotVersionBehavior="unique"),
        mavenlib.maven_virtual(VIRT, [L1, L2]),  # declaration order: L1 base
    ])
    work = mavenlib.scratch_dir()
    try:
        base = ctx.sides[side]["base"]

        def _deploy(tag):
            return mavenlib.mvn_deploy_file(
                ctx, side, "%s/%s" % (base, L2 if tag.startswith("l2") else L1),
                work, GROUP, ART, SNAP,
                os.path.join(work, "%s.jar" % tag),
                os.path.join(work, "%s.pom" % tag))

        pom = mavenlib.pom_fixture(GROUP, ART, SNAP)
        for tag, payload in (("l1", mavenlib.jar_fixture("sv-merge-l1")),
                             ("l2a", mavenlib.jar_fixture("sv-merge-l2a")),
                             ("l2b", mavenlib.jar_fixture("sv-merge-l2b"))):
            with open(os.path.join(work, "%s.jar" % tag), "wb") as fh:
                fh.write(payload)
            with open(os.path.join(work, "%s.pom" % tag), "wb") as fh:
                fh.write(pom)

        # sequence (per side): L1 once -> bn 1; L2 twice -> bn 2 (strictly
        # newer buildNumber AND later timestamp; deploys take seconds).
        for tag, key in (("l1", "deploy_l1_exit"), ("l2a", "deploy_l2a_exit"),
                         ("l2b", "deploy_l2b_exit")):
            out = _deploy(tag)
            raw.setdefault("mvn", {})[tag] = out
            asserts[key] = out["exit"]

        status, _ = _poll_member_bn(ctx, side, L1META, 1)
        l1 = _get_meta(ctx, side, L1META, UA_CAPABLE)
        raw["member_l1"] = {"status": status, "md": l1["md"]}
        asserts["member_l1_bn"] = (
            l1["md"]["snapshot"].get("buildNumber") if l1["md"] else "status=%d" % l1["status"])
        status, _ = _poll_member_bn(ctx, side, L2META, 2)
        l2 = _get_meta(ctx, side, L2META, UA_CAPABLE)
        raw["member_l2"] = {"status": status, "md": l2["md"]}
        asserts["member_l2_bn"] = (
            l2["md"]["snapshot"].get("buildNumber") if l2["md"] else "status=%d" % l2["status"])

        l1_pres, _, l1_vals, _ = _sv_facts(l1["md"] or mavenlib.parse_metadata(b"<metadata/>"))
        l2_pres, _, l2_vals, _ = _sv_facts(l2["md"] or mavenlib.parse_metadata(b"<metadata/>"))
        raw["member_sv_values"] = {"l1": l1_vals, "l2": l2_vals,
                                   "l1_present": l1_pres, "l2_present": l2_pres}

        # virtual, M3-capable UA
        cap = _get_meta(ctx, side, VMETA, UA_CAPABLE)
        raw["virt_m3cap"] = {"status": cap["status"], "md": cap["md"]}
        if cap["md"] is None:
            for k in ("virt_m3cap_sv", "virt_m3cap_exts", "virt_m3cap_winner",
                      "virt_m3cap_snapshot_bn"):
                asserts[k] = "status=%d" % cap["status"]
        else:
            pres, exts, vals, bn = _sv_facts(cap["md"])
            asserts["virt_m3cap_sv"] = pres
            asserts["virt_m3cap_exts"] = exts
            asserts["virt_m3cap_winner"] = _winner(vals, l1_vals, l2_vals)
            asserts["virt_m3cap_snapshot_bn"] = bn

        # virtual, M3-incapable (java-agent) UA — the decisive leg
        noc = _get_meta(ctx, side, VMETA, UA_INCAPABLE)
        raw["virt_m3no"] = {"status": noc["status"], "md": noc["md"]}
        if noc["md"] is None:
            asserts["virt_m3no_sv"] = "status=%d" % noc["status"]
            asserts["virt_m3no_snapshot_bn"] = "status=%d" % noc["status"]
        else:
            pres, _, _, bn = _sv_facts(noc["md"])
            asserts["virt_m3no_sv"] = pres
            asserts["virt_m3no_snapshot_bn"] = bn

        # member-level control with the incapable UA (L1's own metadata)
        mem = _get_meta(ctx, side, L1META, UA_INCAPABLE)
        raw["member_m3no"] = {"status": mem["status"], "md": mem["md"]}
        if mem["md"] is None:
            asserts["member_m3no_sv"] = "status=%d" % mem["status"]
        else:
            asserts["member_m3no_sv"] = _sv_facts(mem["md"])[0]
    finally:
        mavenlib.rm_scratch(work)
        mavenlib.cleanup_repos(ctx, side, [VIRT, L2, L1])
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

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "assertion values are side-relative (member id / bn / ext set);"
                " absolute timestamps live in raw"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "mvn-deploy x3 (L1 once, L2 twice), poll member"
                                  " metadata, GET virtual metadata under"
                                  " M3-capable + java-agent UA, GET member"
                                  " metadata under java-agent UA"}]}
