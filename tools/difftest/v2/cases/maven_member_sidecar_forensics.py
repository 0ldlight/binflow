"""T-550 L031 member-sidecar A-face forensics (T-542 Next / R3 Review B'
non-blocking 1): under the java-agent UA strip face, what does the MEMBER
repo's SNAPSHOT version-level maven-metadata.xml serve for .sha1/.md5/
.sha256/.sha512 sidecars, Last-Modified, and the If-Modified-Since /
If-None-Match conditionals — live A (7.161.26) versus BinFlow's derived
contract (.sha1/.md5/.sha256 = digest of the STRIPPED served body, ETag =
digest, no sidecar Last-Modified, If-None-Match only; sha512 = 404).

Construction: one real mvn deploy:deploy-file (SNAPSHOT) per side, poll
until the version-level metadata carries snapshotVersions (capable UA),
then the forensics matrix. Verdict formula: spec-anchored dimensions are
pinned statically (sv strip, §5.1); the A-unknown dimensions are judged
against the LIVE A response (the forensics oracle) — any b != a lands in
ab_divergence and feeds the ruling, never papered over.
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
    "id": "maven-member-snapshot-sidecar-304",
    "title": "member SNAPSHOT metadata sidecar + conditional GETs under "
             "the java-agent strip face — A live forensics vs B",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 180,
}

LOCAL = "difftest-r4t550-sd"
GROUP, ART, SNAP = "com.diff", "sd-lib", "1.0.0-SNAPSHOT"
GROUP_PATH = GROUP.replace(".", "/")
META = "/%s/%s/%s/%s/maven-metadata.xml" % (LOCAL, GROUP_PATH, ART, SNAP)
UA_JAVA = "Java/1.8.0_391"
UA_CAPABLE = "Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)"

# spec-anchored (virtual-resolution.md §5.1, high confidence)
STATIC_EXPECTED = {
    "body_javaagent_sv": "0",
    "body_javaagent_snapshot": "present",
    "body_capable_sv": "present",
}

# A-live oracle dimensions: judged as b == a per dimension
DUAL_KEYS = (
    "sidecar_sha1_matches_served",
    "sidecar_md5_matches_served",
    "sidecar_sha256_matches_served",
    "sidecar_sha512_status",
    "sidecar_sha1_lm_present",
    "sidecar_sha1_ims_304",
    "sidecar_sha1_inm_304",
    "body_lm_present",
    "body_ims_304",
    "body_ims_old_200",
    "body_inm_304",
    "body_etag_present",
    "body_checksum_hdr_family",
    "sidecar_sha1_capable_matches_served",
)


def _digests(body):
    import hashlib
    return {"sha1": hashlib.sha1(body).hexdigest(),
            "md5": hashlib.md5(body).hexdigest(),  # noqa: S324 - wire convention
            "sha256": mavenlib.sha256_hex(body)}


def _sv_count(body):
    try:
        md = mavenlib.parse_metadata(body)
    except Exception:  # noqa: BLE001 - forensic, never raise
        return "parse-error", "parse-error"
    n = len(md["snapshot_versions"])
    snap = "present" if md["snapshot"].get("buildNumber") else "absent"
    return "%d" % n, snap


def _get(ctx, side, path, ua, extra_headers=None):
    headers = {"User-Agent": ua}
    headers.update(extra_headers or {})
    return ctx.http(side, "GET", path, headers=headers)


def _hdr(resp, *names):
    for n in names:
        for k, v in resp["headers"].items():
            if k.lower() == n.lower():
                return v
    return None


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(LOCAL, snapshotVersionBehavior="unique")])
    work = mavenlib.scratch_dir()
    proxy = None
    try:
        pom = mavenlib.pom_fixture(GROUP, ART, SNAP)
        jar = mavenlib.jar_fixture("sidecar-forensics")
        for name, payload in (("f.jar", jar), ("f.pom", pom)):
            with open(os.path.join(work, name), "wb") as fh:
                fh.write(payload)
        # Harness adaptation (T-550): side a's JVM cannot reach A directly
        # (system-proxy interception, see mavenlib.start_forward_proxy) —
        # the mvn deploy goes through the local transparent forwarder. The
        # bytes on the wire are identical (verbatim relay + preemptive
        # Basic); all forensic GETs below stay DIRECT against each side.
        deploy_base = ctx.sides[side]["base"]
        if side == "a":
            proxy, port = mavenlib.start_forward_proxy(
                ctx.sides["a"]["base"], ctx.sides["a"]["user"],
                ctx.sides["a"]["password"])
            deploy_base = "http://127.0.0.1:%d" % port
        out = mavenlib.mvn_deploy_file(
            ctx, side, "%s/%s" % (deploy_base, LOCAL),
            work, GROUP, ART, SNAP,
            os.path.join(work, "f.jar"), os.path.join(work, "f.pom"))
        raw["mvn_deploy_via"] = "forward-proxy" if proxy else "direct"
        raw["mvn_deploy"] = out
        if out["exit"] != "0":
            raise mavenlib.SetupError("mvn deploy exit=%s: %s"
                                      % (out["exit"], out["tail"][-200:]))

        def _ready(status, body):
            if status != 200:
                return False
            try:
                md = mavenlib.parse_metadata(body)
                return len(md["snapshot_versions"]) >= 1
            except Exception:  # noqa: BLE001
                return False
        mavenlib.poll_until(ctx, side, META, _ready, budget_s=30.0)

        # ---- body faces
        bj = _get(ctx, side, META, UA_JAVA)
        raw["body_javaagent_status"] = bj["status"]
        n, snap = _sv_count(bj["body"]) if bj["status"] == 200 else ("?", "?")
        asserts["body_javaagent_sv"] = n
        asserts["body_javaagent_snapshot"] = snap
        dig_j = _digests(bj["body"]) if bj["status"] == 200 else {}
        lm = _hdr(bj, "Last-Modified")
        etag = _hdr(bj, "ETag")
        raw["body_javaagent_lm"] = lm
        raw["body_javaagent_etag"] = etag
        asserts["body_lm_present"] = "present" if lm else "absent"
        asserts["body_etag_present"] = "present" if etag else "absent"
        fam = []
        for h in ("X-Checksum-Sha1", "X-Checksum-Sha256", "X-Checksum-Md5"):
            if _hdr(bj, h):
                fam.append(h.split("-")[-1].lower())
        asserts["body_checksum_hdr_family"] = "+".join(sorted(fam)) or "none"

        bc = _get(ctx, side, META, UA_CAPABLE)
        nc, _ = _sv_count(bc["body"]) if bc["status"] == 200 else ("?", "?")
        asserts["body_capable_sv"] = "present" if (nc not in ("0", "?")) else nc
        dig_c = _digests(bc["body"]) if bc["status"] == 200 else {}

        # ---- sidecars under the java-agent UA
        def _sidecar(algo):
            return _get(ctx, side, "%s.%s" % (META, algo), UA_JAVA)

        for algo in ("sha1", "md5", "sha256"):
            r = _sidecar(algo)
            raw["sidecar_%s_status" % algo] = r["status"]
            raw["sidecar_%s_body" % algo] = \
                r["body"][:120].decode("utf-8", "replace").strip()
            if r["status"] == 200 and dig_j:
                got = raw["sidecar_%s_body" % algo]
                asserts["sidecar_%s_matches_served" % algo] = (
                    "match" if got == dig_j[algo] else "mismatch")
            else:
                asserts["sidecar_%s_matches_served" % algo] = (
                    "status=%d" % r["status"])
        r512 = _sidecar("sha512")
        raw["sidecar_sha512_status"] = r512["status"]
        asserts["sidecar_sha512_status"] = "status=%d" % r512["status"]

        # ---- sidecar capable control
        r1c = _get(ctx, side, "%s.sha1" % META, UA_CAPABLE)
        raw["sidecar_sha1_capable_body"] = \
            r1c["body"][:120].decode("utf-8", "replace").strip()
        if r1c["status"] == 200 and dig_c:
            asserts["sidecar_sha1_capable_matches_served"] = (
                "match" if raw["sidecar_sha1_capable_body"] == dig_c["sha1"]
                else "mismatch")
        else:
            asserts["sidecar_sha1_capable_matches_served"] = (
                "status=%d" % r1c["status"])

        # ---- conditionals (java-agent face)
        if lm:
            ims = _get(ctx, side, META, UA_JAVA, {"If-Modified-Since": lm})
            raw["body_ims_status"] = ims["status"]
            asserts["body_ims_304"] = "304" if ims["status"] == 304 else \
                "status=%d" % ims["status"]
            ims_old = _get(ctx, side, META, UA_JAVA,
                           {"If-Modified-Since": "Thu, 01 Jan 1970 00:00:00 GMT"})
            asserts["body_ims_old_200"] = "200" if ims_old["status"] == 200 \
                else "status=%d" % ims_old["status"]
        else:
            asserts["body_ims_304"] = "no-lm"
            asserts["body_ims_old_200"] = "no-lm"
        if etag:
            inm = _get(ctx, side, META, UA_JAVA, {"If-None-Match": etag})
            raw["body_inm_status"] = inm["status"]
            asserts["body_inm_304"] = "304" if inm["status"] == 304 else \
                "status=%d" % inm["status"]
        else:
            asserts["body_inm_304"] = "no-etag"

        s1 = _sidecar("sha1")
        s1_lm = _hdr(s1, "Last-Modified")
        s1_etag = _hdr(s1, "ETag")
        raw["sidecar_sha1_lm"] = s1_lm
        asserts["sidecar_sha1_lm_present"] = "present" if s1_lm else "absent"
        if s1_lm:
            sims = _get(ctx, side, "%s.sha1" % META, UA_JAVA,
                        {"If-Modified-Since": s1_lm})
            asserts["sidecar_sha1_ims_304"] = "304" if sims["status"] == 304 \
                else "status=%d" % sims["status"]
        else:
            # the body's LM is the only candidate anchor for the probe
            if lm:
                sims = _get(ctx, side, "%s.sha1" % META, UA_JAVA,
                            {"If-Modified-Since": lm})
                asserts["sidecar_sha1_ims_304"] = \
                    "304" if sims["status"] == 304 else "status=%d" % sims["status"]
            else:
                asserts["sidecar_sha1_ims_304"] = "no-lm"
        if s1_etag:
            sinm = _get(ctx, side, "%s.sha1" % META, UA_JAVA,
                        {"If-None-Match": s1_etag})
            asserts["sidecar_sha1_inm_304"] = "304" if sinm["status"] == 304 \
                else "status=%d" % sinm["status"]
        else:
            asserts["sidecar_sha1_inm_304"] = "no-etag"
    finally:
        if proxy is not None:
            proxy.shutdown()
            proxy.server_close()
        mavenlib.rm_scratch(work)
        mavenlib.cleanup_repos(ctx, side, [LOCAL])
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

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = dict(STATIC_EXPECTED)
    for k in DUAL_KEYS:
        if k in a:
            expected[k] = a[k]  # live A is the forensics oracle
    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "STATIC keys pinned by §5.1; DUAL keys expect b == live-a "
                "(the forensics question); ab_divergence feeds the ruling"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "mvn deploy SNAPSHOT; poll metadata; body "
                                  "GET x2 UA; sidecars sha1/md5/sha256/"
                                  "sha512 x2 UA; IMS/INM conditionals"}]}
