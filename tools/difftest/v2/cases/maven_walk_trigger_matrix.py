"""L035 / T-562 stage 1 — W1: which request shapes trigger the unique-home
plain-SNAPSHOT -> timestamped resolve on the reference (FORENSICS, dual
oracle, expected = live a; B-side values recorded as-is — the resolve legs
are the registered maven/plain-snapshot-path-resolve-404 fence face).

Arms (all on a fresh unique home seeded by one plain pom PUT + one plain
jar PUT, both rewritten by the server):
  t1  GET  plain .pom                (control, F2 re-probe)
  t2  HEAD plain .pom                (does HEAD walk too?)
  t3  GET  plain .jar
  t4  GET  plain .pom  Range: bytes=0-15
  t5  GET  plain .pom.sha1           (checksum sidecar walk — which digest?)
  t6  GET  plain .pom.md5
  t7  GET  version-level maven-metadata.xml via the plain dir (F5 control)
  t8  GET  NON-SNAPSHOT plain spelling in a SNAPSHOT dir holding ts
      candidates (art-3.0.pom in 3.0-SNAPSHOT/) — is the walk gated on the
      -SNAPSHOT token in the requested filename?
  t9  GET  plain .pom in a dir whose only candidates are .jar (extension
      scoping of the walk)
  t10 HEAD plain .pom through a virtual repo (F6 family)
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
    "id": "maven-walk-trigger-matrix",
    "title": "FORENSICS (T-562 s1/W1): unique-home plain-SNAPSHOT resolve "
             "trigger matrix — GET/HEAD/Range/jar/checksum-sidecar/gates",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 180,
}

REPO = "difftest-l035-wt"
VIRT = "difftest-l035-wtv"
GROUP, ART = "com.diff", "wtart"
SNAP = "1.0-SNAPSHOT"
GP = GROUP.replace(".", "/")

POM_PLAIN = mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
    b"</project>", b"<!-- w1pom -->\n</project>")
JAR_PLAIN = b"difftest w1 jar payload\n"

CAND = {}  # sha256 -> label (run())


def _put(ctx, side, repo, rel, body):
    import hashlib
    return ctx.http(side, "PUT", "/%s/%s" % (repo, rel), body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324


def _hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def _label(body):
    import hashlib
    return CAND.get(mavenlib.sha256_hex(body),
                    "unknown-sha:%s" % mavenlib.sha256_hex(body)[:12])


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(REPO, snapshotVersionBehavior="unique")])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(VIRT, [REPO]))

    # seed: one plain pom PUT + one plain jar PUT (unique home rewrites both)
    pom_dir = "%s/%s/%s" % (GP, ART, SNAP)
    jar_ga = "com.diff.wj"
    jar_dir = "com/diff/wj/2.0-SNAPSHOT"
    r = _put(ctx, side, REPO, "%s/%s-%s.pom" % (pom_dir, ART, SNAP), POM_PLAIN)
    raw["seed_pom_put"] = r["status"]
    raw["seed_pom_location"] = _hdr(r, "Location") or ""
    r = _put(ctx, side, REPO, "%s/wj-2.0-SNAPSHOT.jar" % jar_dir, JAR_PLAIN)
    raw["seed_jar_put"] = r["status"]
    raw["seed_jar_location"] = _hdr(r, "Location") or ""
    lst = ctx.http(side, "GET",
                   "/api/storage/%s/%s?list&deep=1&listFolders=1"
                   % (REPO, pom_dir))
    raw["pom_dir_listing"] = (lst["body"].decode("utf-8", "replace")[:1500]
                              if lst["status"] == 200
                              else "status=%d" % lst["status"])

    plain_pom = "/%s/%s/%s-%s.pom" % (REPO, pom_dir, ART, SNAP)

    # t1 GET plain pom (control)
    g = ctx.http(side, "GET", plain_pom)
    raw["t1_status"] = g["status"]
    asserts["t1_get_plain_pom"] = (
        _label(g["body"]) if g["status"] == 200 else "status=%d" % g["status"])

    # t2 HEAD plain pom
    h = ctx.http(side, "HEAD", plain_pom)
    raw["t2_head_status"] = h["status"]
    raw["t2_head_content_length"] = _hdr(h, "Content-Length")
    raw["t2_head_etag"] = _hdr(h, "ETag")
    asserts["t2_head_plain_pom"] = (
        "len=%s" % _hdr(h, "Content-Length") if h["status"] == 200
        else "status=%d" % h["status"])

    # t3 GET plain jar
    g = ctx.http(side, "GET", "/%s/%s/wj-2.0-SNAPSHOT.jar" % (REPO, jar_dir))
    raw["t3_status"] = g["status"]
    asserts["t3_get_plain_jar"] = (
        _label(g["body"]) if g["status"] == 200 else "status=%d" % g["status"])

    # t4 Range on plain pom
    g = ctx.http(side, "GET", plain_pom, headers={"Range": "bytes=0-15"})
    raw["t4_status"] = g["status"]
    raw["t4_body_head_hex"] = g["body"][:16].hex()
    asserts["t4_range_plain_pom"] = "status=%d;len=%d" % (g["status"],
                                                          len(g["body"]))

    # t5/t6 checksum sidecars of the plain spelling
    for ext, dim in (("sha1", "t5"), ("md5", "t6")):
        g = ctx.http(side, "GET", plain_pom + "." + ext)
        raw[dim + "_status"] = g["status"]
        body = g["body"].decode("utf-8", "replace").strip()
        raw[dim + "_body"] = body[:80]
        import hashlib
        if ext == "sha1":
            of_pom = mavenlib.sha1_hex(POM_PLAIN)
        else:
            of_pom = hashlib.md5(POM_PLAIN).hexdigest()  # noqa: S324
        asserts[dim + "_sidecar_plain_pom"] = (
            "200=sha_of_pom" if (g["status"] == 200 and body == of_pom)
            else ("200=other:%s" % body[:20] if g["status"] == 200
                  else "status=%d" % g["status"]))

    # t7 version-level metadata via plain dir (F5 control)
    st, body = mavenlib.poll_until(
        ctx, side, "/%s/%s/maven-metadata.xml" % (REPO, pom_dir),
        lambda s, b: s == 200, budget_s=10.0)
    raw["t7_meta_status"] = st
    asserts["t7_version_meta_get"] = "200" if st == 200 else "status=%d" % st

    # t8 non-SNAPSHOT plain spelling in a SNAPSHOT dir with ts candidates
    g8_dir = "%s/gt8/3.0-SNAPSHOT" % GP
    t8_ts = "%s/gt8-3.0-20260601.000001-1.pom" % g8_dir
    r = _put(ctx, side, REPO, t8_ts,
             mavenlib.pom_fixture("com.diff", "gt8", "3.0-SNAPSHOT").replace(
                 b"</project>", b"<!-- gt8ts -->\n</project>"))
    raw["t8_seed_put_status"] = r["status"]
    g = ctx.http(side, "GET", "/%s/%s/gt8-3.0.pom" % (REPO, g8_dir))
    raw["t8_status"] = g["status"]
    raw["t8_body_head"] = g["body"].decode("utf-8", "replace")[:220]
    asserts["t8_nonsnap_plain_in_snap_dir"] = "status=%d" % g["status"]

    # t9 plain .pom where only .jar candidates exist (extension scoping)
    t9_dir = "%s/gt9/4.0-SNAPSHOT" % GP
    t9_ts_jar = "%s/gt9-4.0-20260601.000001-1.jar" % t9_dir
    r = _put(ctx, side, REPO, t9_ts_jar, b"difftest t9 jar payload\n")
    raw["t9_seed_put_status"] = r["status"]
    g = ctx.http(side, "GET", "/%s/%s/gt9-4.0-SNAPSHOT.pom" % (REPO, t9_dir))
    raw["t9_status"] = g["status"]
    asserts["t9_plain_pom_only_jar_cands"] = "status=%d" % g["status"]

    # t10 HEAD plain pom through virtual
    h = ctx.http(side, "HEAD",
                 "/%s/%s/%s-%s.pom" % (VIRT, pom_dir, ART, SNAP))
    raw["t10_head_virt_status"] = h["status"]
    raw["t10_head_virt_len"] = _hdr(h, "Content-Length")
    asserts["t10_head_plain_pom_virtual"] = (
        "len=%s" % _hdr(h, "Content-Length") if h["status"] == 200
        else "status=%d" % h["status"])

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


DUAL_KEYS = (
    "t1_get_plain_pom", "t2_head_plain_pom", "t3_get_plain_jar",
    "t4_range_plain_pom", "t5_sidecar_plain_pom", "t6_sidecar_plain_pom",
    "t7_version_meta_get", "t8_nonsnap_plain_in_snap_dir",
    "t9_plain_pom_only_jar_cands", "t10_head_plain_pom_virtual",
)


def run(ctx):
    import hashlib
    global CAND
    CAND = {
        mavenlib.sha256_hex(POM_PLAIN): "w1pom",
        mavenlib.sha256_hex(JAR_PLAIN): "wj-jar",
    }
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, REPO])

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = {k: a[k] for k in DUAL_KEYS if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "FORENSICS T-562 s1/W1 trigger matrix, dual oracle "
                "(expected = live a). t1-t5/t10 a!=b is the registered "
                "walk-family fence face (known, feeds the spec), not a new "
                "finding; t8/t9 are gate-scoping probes."})
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "unique home seed (plain pom+jar PUT) -> "
                                  "GET/HEAD/Range/sidecar/metadata/gate "
                                  "probes x10"}]}
