"""L034 arm 7 — T-562 / BIN-44 evidence leg (FORENSICS, not a flip
candidate): after a UNIQUE home rewrites a plain-SNAPSHOT wire PUT to the
timestamped spelling, what does a plain-SNAPSHOT GET resolve to?

Probes the full picture for the walk-family face:
  F1  trigger: plain wire PUT into a unique home — rewrite or store-as-is
      (Location spelling) on each side;
  F2  plain GET right after one rewritten version exists;
  F3  second plain PUT (two rewritten versions coexist) — plain GET picks
      which bytes;
  F4  explicit timestamped spellings landed as-is (old 20260101.000001-3,
      newer 20260102.000002-4) coexisting with the rewrites — plain GET
      selection rule among FOUR candidates (latest ts? highest build
      number? latest stored?);
  F5  version-level maven-metadata.xml on the plain dir after the wire
      PUTs (A's materialization face is UNKNOWN/pending — recorded as-is;
      poll bounded, never auto-widened);
  F6  virt resolve of the plain spelling;
  F7  non-unique contrast lives in maven-plainsnap-nonunique-resolve.

Dual-oracle dims (b == live a). The resolve legs are the REGISTERED
divergence (maven/plain-snapshot-path-resolve-404 fence face — B 404 until
T-562): case FAIL is EXPECTED and feeds the T-562 spec, it is not a batch
regression. served-bytes dims name the candidate whose sha256 was served.
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
    "id": "maven-unique-plain-resolve-forensics",
    "title": "FORENSICS (T-562/BIN-44): unique-home plain-SNAPSHOT GET "
             "resolve family — trigger, coexistence selection rule, "
             "metadata face, virtual face",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 120,
}

UQ = "difftest-l034-uf"
UQV = "difftest-l034-ufv"
GROUP, ART = "com.diff", "ffwalk"
BASE_VER = "1.0"                 # snapshot base rev; SNAP = 1.0-SNAPSHOT
SNAP = BASE_VER + "-SNAPSHOT"
GP = GROUP.replace(".", "/")
DIR_REL = "%s/%s/%s" % (GP, ART, SNAP)
PLAIN_REL = "%s/%s-%s.pom" % (DIR_REL, ART, SNAP)
TS3_REL = "%s/%s-%s-20260101.000001-3.pom" % (DIR_REL, ART, BASE_VER)
TS4_REL = "%s/%s-%s-20260102.000002-4.pom" % (DIR_REL, ART, BASE_VER)

CANDIDATES = {}  # sha256 -> label, filled in run()


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


def _served_label(ctx, side, repo, rel):
    g = ctx.http(side, "GET", "/%s/%s" % (repo, rel))
    if g["status"] != 200:
        return "status=%d" % g["status"], g["status"]
    sha = mavenlib.sha256_hex(g["body"])
    return CANDIDATES.get(sha, "unknown-sha:%s" % sha[:12]), g["status"]


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(UQ, snapshotVersionBehavior="unique")])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(UQV, [UQ]))

    bodies = {
        "plain1": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
            b"</project>", b"<!-- plain1 -->\n</project>"),
        "plain2": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
            b"</project>", b"<!-- plain2 -->\n</project>"),
        "ts3": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
            b"</project>", b"<!-- ts3 -->\n</project>"),
        "ts4": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
            b"</project>", b"<!-- ts4 -->\n</project>"),
    }

    # F1: trigger — plain PUT into the unique home
    r = _put(ctx, side, UQ, PLAIN_REL, bodies["plain1"])
    raw["f1_put_status"] = r["status"]
    raw["f1_put_location"] = _hdr(r, "Location") or ""
    asserts["f1_put_status"] = "%d" % r["status"]
    asserts["f1_location_spelling"] = (
        "plain" if "%s-%s.pom" % (ART, SNAP) in raw["f1_put_location"]
        else "timestamped" if "20" in raw["f1_put_location"]
        else "absent-or-other")
    # which physical spelling now exists? probe the plain path on disk via
    # GET (F2) plus the sidecar dir listing through /api/storage list
    lst = ctx.http(side, "GET",
                   "/api/storage/%s/%s?list&deep=1&listFolders=1" % (UQ, DIR_REL))
    raw["f1_dir_list"] = lst["body"].decode("utf-8", "replace")[:1200] \
        if lst["status"] == 200 else "status=%d" % lst["status"]

    # F2: plain GET after ONE rewritten version
    label, st = _served_label(ctx, side, UQ, PLAIN_REL)
    raw["f2_get_status"] = st
    asserts["f2_plain_get"] = label

    # F3: second plain PUT (two rewrites coexist) — selection
    r = _put(ctx, side, UQ, PLAIN_REL, bodies["plain2"])
    raw["f3_put2_status"] = r["status"]
    raw["f3_put2_location"] = _hdr(r, "Location") or ""
    label, st = _served_label(ctx, side, UQ, PLAIN_REL)
    raw["f3_get_status"] = st
    asserts["f3_plain_get_two_cands"] = label

    # F4: explicit timestamped spellings (stored as-is) — FOUR candidates
    r3 = _put(ctx, side, UQ, TS3_REL, bodies["ts3"])
    r4 = _put(ctx, side, UQ, TS4_REL, bodies["ts4"])
    raw["f4_put_ts3_status"] = r3["status"]
    raw["f4_put_ts4_status"] = r4["status"]
    raw["f4_put_ts3_location"] = _hdr(r3, "Location") or ""
    raw["f4_put_ts4_location"] = _hdr(r4, "Location") or ""
    label, st = _served_label(ctx, side, UQ, PLAIN_REL)
    raw["f4_get_status"] = st
    asserts["f4_plain_get_four_cands"] = label

    # F5: version-level metadata on the plain dir (bounded poll; A's
    # materialization face is the pending auto-materialize ledger item)
    def _ready(status, body):
        return status == 200
    st, body = mavenlib.poll_until(
        ctx, side, "/%s/%s/maven-metadata.xml" % (UQ, DIR_REL),
        _ready, budget_s=10.0)
    raw["f5_meta_status"] = st
    if st == 200:
        md = mavenlib.parse_metadata(body)
        raw["f5_snapshot"] = md["snapshot"]
        raw["f5_snapshot_versions"] = md["snapshot_versions"]
        asserts["f5_meta_sv_count"] = "%d" % len(md["snapshot_versions"])
    else:
        asserts["f5_meta_sv_count"] = "status=%d" % st

    # F6: virtual resolve of the plain spelling
    label, st = _served_label(ctx, side, UQV, PLAIN_REL)
    raw["f6_get_status"] = st
    asserts["f6_virt_plain_get"] = label

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


DUAL_KEYS = (
    "f1_put_status", "f1_location_spelling",
    "f2_plain_get",
    "f3_plain_get_two_cands",
    "f4_plain_get_four_cands",
    "f5_meta_sv_count",
    "f6_virt_plain_get",
)


def run(ctx):
    global CANDIDATES
    CANDIDATES = {
        mavenlib.sha256_hex(v): k for k, v in {
            "plain1": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
                b"</project>", b"<!-- plain1 -->\n</project>"),
            "plain2": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
                b"</project>", b"<!-- plain2 -->\n</project>"),
            "ts3": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
                b"</project>", b"<!-- ts3 -->\n</project>"),
            "ts4": mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
                b"</project>", b"<!-- ts4 -->\n</project>"),
        }.items()}
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [UQV, UQ])

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = {k: a[k] for k in DUAL_KEYS if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "FORENSICS for T-562/BIN-44: dual oracle, expected = live a. "
                "Resolve dims (f2/f3/f4/f6) are the registered divergence "
                "fence face — a != b here is the KNOWN gap feeding the "
                "walk-family spec, not a new finding; f1/f5 dims are "
                "evidence for the spec's trigger and metadata inputs"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "unique home: plain PUT x2 + explicit "
                                  "timestamped PUT x2; plain GET after each "
                                  "coexistence stage; version metadata; "
                                  "virt resolve"}]}
