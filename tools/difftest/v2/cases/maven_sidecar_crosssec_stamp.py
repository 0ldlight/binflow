"""L034 arm 4 (T-558 contract maven/derived-sidecar-lm-materialized-stamp /
T-559 fix verification): derived-sidecar (.sha1/.md5/.sha256) Last-Modified
must be a MATERIALIZED STABLE stamp — a cross-second revisit carrying the
sidecar's own previously observed Last-Modified must answer 304, not
200+forwarded-LM (the pre-fix per-request stamp regression, L033 forensics:
LM 00:44:09 -> 00:44:10 on revisit).

Faces:
  - member java-agent STRIP face (the divergence face proper): x3 digest
    sidecars, cross-second IMS each;
  - virtual MERGE face (contract observable): capable UA through the
    virtual (merged body) + java-agent strip face through the virtual;
  - content guards: each sidecar = digest of the STRIPPED served body.

Construction = real mvn deploy:deploy-file SNAPSHOT per side (A through the
transparent forwarder — JVM proxy quirk, see _mavenlib; forensic GETs stay
direct). The mvn-deploy construction materializes the version-level
metadata on A (client uploads it), so unlike the L033 wire-PUT probe the
A-side legs here ARE directly reachable — the contract's "A 侧跨秒直接腿
构造不可达" caveat was scoped to wire-PUT; this batch direct-tests it.
"""
import importlib.util
import json
import os
import time

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-sidecar-crosssec-stamp",
    "title": "derived-sidecar materialized-stamp: cross-second IMS -> 304, "
             "x3 digests (member strip face) + virtual merge face, real "
             "mvn deploy construction",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 300,
}

LOCAL = "difftest-l034-sd"
VIRT = "difftest-l034-sv"
GROUP, ART, SNAP = "com.diff", "sdc-lib", "1.0.0-SNAPSHOT"
GP = GROUP.replace(".", "/")
META_REL = "%s/%s/%s/maven-metadata.xml" % (GP, ART, SNAP)
UA_JAVA = "Java/1.8.0_391"
UA_CAPABLE = "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)"
CROSS_SEC = 1.3  # cross-second revisit gap (L033 forensics used the same)


def _hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def _digest(name, body):
    import hashlib
    if name == "sha1":
        return hashlib.sha1(body).hexdigest()  # noqa: S324 - wire convention
    if name == "md5":
        return hashlib.md5(body).hexdigest()  # noqa: S324 - wire convention
    return mavenlib.sha256_hex(body)


def _crosssec_ims(ctx, side, base, rel, ua):
    """GET rel (record LM) -> sleep past the second -> GET with IMS=LM.

    Returns dim slug: 304 / 200+lm-moved / 200+lm-same / status=N / nolm.
    """
    r1 = ctx.http(side, "GET", base + rel, headers={"User-Agent": ua})
    lm = _hdr(r1, "Last-Modified")
    if r1["status"] != 200 or not lm:
        return "status=%d%s" % (r1["status"], "" if lm else "+nolm"), None
    time.sleep(CROSS_SEC)
    r2 = ctx.http(side, "GET", base + rel, headers={
        "User-Agent": ua, "If-Modified-Since": lm})
    if r2["status"] == 304:
        return "304", {"lm": lm}
    lm2 = _hdr(r2, "Last-Modified")
    return ("200+lm-moved" if lm2 and lm2 != lm
            else "200+lm-same" if lm2 else "200+nolm"), {"lm": lm, "lm2": lm2}


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL, snapshotVersionBehavior="unique")])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(VIRT, [LOCAL]))
    work = mavenlib.scratch_dir()
    proxy = None
    try:
        pom = mavenlib.pom_fixture(GROUP, ART, SNAP)
        jar = mavenlib.jar_fixture("crosssec-stamp-l034")
        for name, payload in (("f.jar", jar), ("f.pom", pom)):
            with open(os.path.join(work, name), "wb") as fh:
                fh.write(payload)
        deploy_base = ctx.sides[side]["base"]
        if side == "a":
            proxy, port = mavenlib.start_forward_proxy(
                ctx.sides["a"]["base"], ctx.sides["a"]["user"],
                ctx.sides["a"]["password"])
            # the forwarder already targets upstream=<base incl. /artifactory>
            # and relays the request path verbatim — no context suffix here
            deploy_base = "http://127.0.0.1:%d" % port
        out = mavenlib.mvn_deploy_file(
            ctx, side, deploy_base + "/" + LOCAL, work,
            GROUP, ART, SNAP,
            os.path.join(work, "f.jar"), os.path.join(work, "f.pom"))
        raw["mvn_exit"] = out["exit"]
        if out["exit"] != "0":
            raise mavenlib.SetupError("mvn deploy failed side %r: %s" % (
                side, out["tail"][-200:]))

        # settle: version-level metadata served (capable UA, merged sv)
        def _ready(status, body):
            if status != 200:
                return False
            try:
                return len(mavenlib.parse_metadata(body)["snapshot_versions"]) > 0
            except Exception:  # noqa: BLE001
                return False
        st, body = mavenlib.poll_until(
            ctx, side, "/" + LOCAL + "/" + META_REL, _ready, budget_s=45.0)
        raw["member_meta_ready_status"] = st
        if st != 200:
            raise mavenlib.SetupError(
                "member version metadata not ready side %r: %d" % (side, st))

        # ---- member java-agent strip face: stripped body + x3 sidecars
        stripped = ctx.http(side, "GET", "/" + LOCAL + "/" + META_REL,
                            headers={"User-Agent": UA_JAVA})
        raw["member_stripped_status"] = stripped["status"]
        raw["member_stripped_sv"] = len(
            mavenlib.parse_metadata(stripped["body"])["snapshot_versions"]) \
            if stripped["status"] == 200 else "n/a"
        body_lm = _hdr(stripped, "Last-Modified")
        raw["member_body_lm"] = body_lm
        for i, (name, ext) in enumerate(
                (("sha1", ".sha1"), ("md5", ".md5"), ("sha256", ".sha256"))):
            sc = ctx.http(side, "GET", "/%s/%s%s" % (LOCAL, META_REL, ext),
                          headers={"User-Agent": UA_JAVA})
            raw["member_%s_status" % name] = sc["status"]
            ok = (sc["status"] == 200 and
                  sc["body"].decode("utf-8", "replace").strip()
                  == _digest(name, stripped["body"]))
            asserts["member_%s_digest_ok" % name] = "match" if ok else "mismatch"
            lm = _hdr(sc, "Last-Modified")
            raw["member_%s_lm" % name] = lm
            raw["member_%s_lm_vs_body" % name] = (
                "equal" if lm and lm == body_lm
                else "later" if lm and body_lm else "n/a")
            slug, detail = _crosssec_ims(
                ctx, side, "", "/%s/%s%s" % (LOCAL, META_REL, ext), UA_JAVA)
            asserts["member_%s_crosssec_ims" % name] = slug
            if detail:
                raw["member_%s_ims" % name] = detail

        # ---- virtual merge face (capable UA: merged body incl. sv)
        merged = ctx.http(side, "GET", "/" + VIRT + "/" + META_REL,
                          headers={"User-Agent": UA_CAPABLE})
        raw["virt_merged_status"] = merged["status"]
        raw["virt_merged_sv"] = len(
            mavenlib.parse_metadata(merged["body"])["snapshot_versions"]) \
            if merged["status"] == 200 else "n/a"
        slug, detail = _crosssec_ims(
            ctx, side, "", "/%s/%s.sha1" % (VIRT, META_REL), UA_CAPABLE)
        asserts["virt_capable_sha1_crosssec_ims"] = slug
        if detail:
            raw["virt_capable_sha1_ims"] = detail
        # ---- virtual strip face (java-agent through the virtual)
        slug, detail = _crosssec_ims(
            ctx, side, "", "/%s/%s.sha1" % (VIRT, META_REL), UA_JAVA)
        asserts["virt_java_sha1_crosssec_ims"] = slug
        if detail:
            raw["virt_java_sha1_ims"] = detail
    finally:
        if proxy:
            proxy.shutdown()
            proxy.server_close()
        mavenlib.rm_scratch(work)

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


EXPECTED = {
    "member_sha1_digest_ok": "match",
    "member_md5_digest_ok": "match",
    "member_sha256_digest_ok": "match",
    # contract assertion face: stable stamp -> cross-second IMS = 304
    "member_sha1_crosssec_ims": "304",
    "member_md5_crosssec_ims": "304",
    "member_sha256_crosssec_ims": "304",
    "virt_capable_sha1_crosssec_ims": "304",
    "virt_java_sha1_crosssec_ims": "304",
}


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, LOCAL])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "static contract expectations graded on BOTH sides "
                "(materialized-stamp face: cross-second IMS -> 304 on the "
                "strip face x3 digests + both virtual faces). LM value-level "
                "relation to the body LM is recorded raw-only (contract "
                "unobserved: A's sidecar stamp may trail the body stamp by "
                "seconds — its own materialization time — while the B fix "
                "reuses the derived input's stamp; not an assertion face)"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "mvn deploy SNAPSHOT (A via forwarder); "
                                  "poll metadata; java-agent strip body + "
                                  "x3 sidecar cross-second IMS; virtual "
                                  "merge/strip sidecar cross-second IMS"}]}
