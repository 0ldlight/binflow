"""L035 / T-562 stage 1 — W4b: the VIRTUAL multi-member plain-SNAPSHOT
walk's CROSS-MEMBER selection key (FORENSICS, dual oracle, expected = live
a). Born from manual dissection: the c4 single scenario could not separate
"global max filename-ts" / "last-declared member" / "latest storage mtime".
Three scenarios, two unique members each, upload order crossed against
declaration order and filename-ts:

  v1  declared [old-ts, new-ts], upload old->new  (c4 replay)
  v2  declared [new-ts, old-ts], upload new->old
  v3  declared [new-ts, old-ts], upload old->new  (the discriminator)

Model fit (A live, 2026-09-29, fresh members per scenario): the served
body is always the LAST-UPLOADED member's candidate (latest storage
mtime) — 13/13 clean observations incl. manual dissection runs. Global
max filename-ts across members is REFUTED (v2: old-ts body uploaded last
wins over a newer-ts first member). Member declaration order is
irrelevant (v3). Contrast: the LOCAL member walk itself selects by max
FILENAME ts (W2 s1) — the virtual cross-member pick uses a different
key (storage mtime).
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-virtual-walk-selection",
    "title": "FORENSICS (T-562 s1/W4b): virtual multi-member plain-"
             "SNAPSHOT walk cross-member selection key (mtime vs "
             "filename-ts vs member order)",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 120,
}

M1 = "difftest-l035-vw1"
M2 = "difftest-l035-vw2"
V = "difftest-l035-vwv"
P = "com/diff/vww/vww/3.0-SNAPSHOT"


def _pom(marker):
    return mavenlib.pom_fixture("com.diff.vww", "vww", "3.0-SNAPSHOT").replace(
        b"</project>", ("<!-- %s -->\n</project>" % marker).encode())


def _put(ctx, side, repo, ts, body):
    import hashlib
    return ctx.http(side, "PUT", "/%s/%s/vww-3.0-%s-1.pom" % (repo, P, ts),
                    body=body, headers={
                        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
                        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324,E501


def _leg(ctx, side):
    asserts, raw = {}, {}
    b_new, b_old = _pom("vNEW"), _pom("vOLD")
    # FRESH repos per scenario (r4/r5 lesson: reusing the members across
    # scenarios contaminates the dirs with earlier candidates and the
    # served BODY stops identifying the winner — those rounds are void).
    scenarios = [
        # (name, upload sequence as (member_slot, ts, body)) — slot 1 is
        # the FIRST-declared virtual member
        ("v1_decl_old_new_upload_old_new",
         ((1, "20260101.000001", b_old), (2, "20260808.000001", b_new))),
        ("v2_decl_new_old_upload_new_old",
         ((1, "20260808.000001", b_new), (2, "20260101.000001", b_old))),
        ("v3_decl_new_old_upload_old_new",
         ((2, "20260101.000001", b_old), (1, "20260808.000001", b_new))),
    ]
    import time
    for name, uploads in scenarios:
        m1 = M1 + name[-2:]
        m2 = M2 + name[-2:]
        v = V + name[-2:]
        mavenlib.ensure_repos(ctx, side, [
            mavenlib.maven_local(m1, snapshotVersionBehavior="unique"),
            mavenlib.maven_local(m2, snapshotVersionBehavior="unique")])
        mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(v, [m1, m2]))
        for slot, ts, body in uploads:
            repo = m1 if slot == 1 else m2
            r = _put(ctx, side, repo, ts, body)
            raw["%s_put_m%d_%s" % (name, slot, ts[:8])] = r["status"]
            time.sleep(1.1)  # distinct storage mtimes between members
        g = ctx.http(side, "GET", "/%s/%s/vww-3.0-SNAPSHOT.pom" % (v, P))
        asserts[name] = (
            "vNEW" if g["status"] == 200 and g["body"] == b_new else
            "vOLD" if g["status"] == 200 and g["body"] == b_old else
            "other-200" if g["status"] == 200 else "status=%d" % g["status"])
        mavenlib.cleanup_repos(ctx, side, [v, m1, m2])

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


DUAL_KEYS = ("v1_decl_old_new_upload_old_new", "v2_decl_new_old_upload_new_old",
             "v3_decl_new_old_upload_old_new")


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [V, M1, M2])

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = {k: a[k] for k in DUAL_KEYS if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "FORENSICS T-562 s1/W4b. Fresh members per scenario "
                "(r4/r5 were void: dir contamination). A live: served body "
                "= last-uploaded member's candidate in every scenario; "
                "global filename-ts and declaration order refuted."})
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "two unique members + virtual; 3 "
                                  "declaration/upload-order scenarios; "
                                  "plain GET via virtual"}]}
