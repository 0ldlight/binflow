"""L035 / T-562 stage 1 — W2: precise definition of the "max timestamp"
selection the reference performs for a plain-SNAPSHOT GET in a unique home
(FORENSICS, dual oracle, expected = live a; all candidates land as EXPLICIT
timestamped spellings so the matrix is wall-clock-deterministic).

r0 lesson baked in: paths MUST carry the version dir — A derives GAV from
the layout (group/artifact/version/file) and 409s otherwise.

Arms (fresh GA each):
  s1  filename-ts vs upload-mtime: newer filename-ts uploaded FIRST,
      older filename-ts uploaded LAST -> which wins?
  s2  same filename-ts, buildNumber 1 then 2 -> tie-break?
  s2b same as s2 but upload order reversed -> build number or upload order?
  s3  buildNumber numeric-vs-lex: -9 vs -10 (lex max "9", numeric 10)
  s4  extension scoping: newer .jar + older .pom in one dir; plain .pom
      and plain .jar each resolve within their own extension
  s5  classifier scoping: pom bn2 (newer) + sources.jar bn1 (older, the
      only sources candidate); plain .pom -> bn2 pom; plain -sources.jar
      -> bn1 sources (classifier-aware walk), NOT a miss and NOT the pom
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-walk-selection-tiebreak",
    "title": "FORENSICS (T-562 s1/W2): plain-SNAPSHOT resolve selection "
             "rule — filename-ts vs mtime, tie-break, buildNumber "
             "numeric-vs-lex, extension/classifier scoping",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 180,
}

REPO = "difftest-l035-ws"
GP = "com/diff"


def _pom(art, ver, marker):
    return mavenlib.pom_fixture("com.diff", art, ver).replace(
        b"</project>", ("<!-- %s -->\n</project>" % marker).encode())


def _put(ctx, side, rel, body):
    import hashlib
    return ctx.http(side, "PUT", "/%s/%s" % (REPO, rel), body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324


def _resolve(ctx, side, repo, rel, cand):
    g = ctx.http(side, "GET", "/%s/%s" % (repo, rel))
    if g["status"] != 200:
        return "status=%d" % g["status"]
    return cand.get(mavenlib.sha256_hex(g["body"]),
                    "unknown-sha:%s" % mavenlib.sha256_hex(g["body"])[:12])


def _leg(ctx, side, cand):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(REPO, snapshotVersionBehavior="unique")])

    def arm(art, base, files, probe):
        d = "%s/%s/%s-SNAPSHOT" % (GP, art, base)
        for name, body in files:
            rel = "%s/%s" % (d, name)
            r = _put(ctx, side, rel, body)
            raw["%s_put_%s" % (art, name)] = r["status"]
        asserts["%s_plain_get" % art] = _resolve(ctx, side, REPO, probe, cand)

    # s1: newer filename-ts uploaded FIRST
    a = "w2s1"
    arm(a, "1.0", [
        ("%s-1.0-20260601.000001-1.pom" % a, _pom(a, "1.0-SNAPSHOT", "s1-newts")),
        ("%s-1.0-20260101.000001-1.pom" % a, _pom(a, "1.0-SNAPSHOT", "s1-oldts")),
    ], "%s/%s/1.0-SNAPSHOT/%s-1.0-SNAPSHOT.pom" % (GP, a, a))

    # s2: same ts, bn 1 then 2
    a = "w2s2"
    arm(a, "1.0", [
        ("%s-1.0-20260501.000001-1.pom" % a, _pom(a, "1.0-SNAPSHOT", "s2-bn1")),
        ("%s-1.0-20260501.000001-2.pom" % a, _pom(a, "1.0-SNAPSHOT", "s2-bn2")),
    ], "%s/%s/1.0-SNAPSHOT/%s-1.0-SNAPSHOT.pom" % (GP, a, a))

    # s2b: same ts, bn 2 first then 1
    a = "w2s2b"
    arm(a, "1.0", [
        ("%s-1.0-20260501.000001-2.pom" % a, _pom(a, "1.0-SNAPSHOT", "s2b-bn2")),
        ("%s-1.0-20260501.000001-1.pom" % a, _pom(a, "1.0-SNAPSHOT", "s2b-bn1")),
    ], "%s/%s/1.0-SNAPSHOT/%s-1.0-SNAPSHOT.pom" % (GP, a, a))

    # s3: bn 9 vs 10 (lex max "9", numeric max 10)
    a = "w2s3"
    arm(a, "1.0", [
        ("%s-1.0-20260502.000001-9.pom" % a, _pom(a, "1.0-SNAPSHOT", "s3-bn9")),
        ("%s-1.0-20260502.000001-10.pom" % a, _pom(a, "1.0-SNAPSHOT", "s3-bn10")),
    ], "%s/%s/1.0-SNAPSHOT/%s-1.0-SNAPSHOT.pom" % (GP, a, a))

    # s4: extension scoping — newer jar ts + older pom ts in one dir
    a = "w2s4"
    d = "%s/%s/1.0-SNAPSHOT" % (GP, a)
    for rel, body in [
        ("%s/%s-1.0-20260601.000001-1.jar" % (d, a), b"w2s4 jar newts\n"),
        ("%s/%s-1.0-20260101.000001-1.pom" % (d, a),
         _pom(a, "1.0-SNAPSHOT", "s4-pom-oldts")),
    ]:
        r = _put(ctx, side, rel, body)
        raw["s4_put_%s" % rel.rsplit("/", 1)[-1][:34]] = r["status"]
    asserts["s4_plain_pom_get"] = _resolve(
        ctx, side, REPO, "%s/%s-1.0-SNAPSHOT.pom" % (d, a), cand)
    asserts["s4_plain_jar_get"] = _resolve(
        ctx, side, REPO, "%s/%s-1.0-SNAPSHOT.jar" % (d, a), cand)

    # s5: classifier scoping — pom bn2 (newer) + sources.jar bn1 (older)
    a = "w2s5"
    d = "%s/%s/1.0-SNAPSHOT" % (GP, a)
    for rel, body in [
        ("%s/%s-1.0-20260101.000001-1-sources.jar" % (d, a),
         b"w2s5 sources bn1\n"),
        ("%s/%s-1.0-20260707.000001-2.pom" % (d, a),
         _pom(a, "1.0-SNAPSHOT", "s5-pom-bn2")),
    ]:
        r = _put(ctx, side, rel, body)
        raw["s5_put_%s" % rel.rsplit("/", 1)[-1][:40]] = r["status"]
    asserts["s5_plain_pom_get"] = _resolve(
        ctx, side, REPO, "%s/%s-1.0-SNAPSHOT.pom" % (d, a), cand)
    asserts["s5_plain_sources_get"] = _resolve(
        ctx, side, REPO, "%s/%s-1.0-SNAPSHOT-sources.jar" % (d, a), cand)

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


DUAL_KEYS = (
    "w2s1_plain_get", "w2s2_plain_get", "w2s2b_plain_get", "w2s3_plain_get",
    "s4_plain_pom_get", "s4_plain_jar_get",
    "s5_plain_pom_get", "s5_plain_sources_get",
)


def run(ctx):
    cand = {}
    for art, marker in [
            ("w2s1", "s1-newts"), ("w2s1", "s1-oldts"),
            ("w2s2", "s2-bn1"), ("w2s2", "s2-bn2"),
            ("w2s2b", "s2b-bn1"), ("w2s2b", "s2b-bn2"),
            ("w2s3", "s3-bn9"), ("w2s3", "s3-bn10"),
            ("w2s4", "s4-pom-oldts"), ("w2s5", "s5-pom-bn2")]:
        cand[mavenlib.sha256_hex(_pom(art, "1.0-SNAPSHOT", marker))] = marker
    cand[mavenlib.sha256_hex(b"w2s4 jar newts\n")] = "s4-jar-newts"
    cand[mavenlib.sha256_hex(b"w2s5 sources bn1\n")] = "s5-sources-bn1"
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side, cand)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [REPO])

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = {k: a[k] for k in DUAL_KEYS if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "FORENSICS T-562 s1/W2 selection rule. A values are the "
                "oracle; B 404 across the board is the registered fence "
                "face (known)."})
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "explicit-ts seed matrix (6 arms) -> "
                                  "plain GET per arm"}]}
