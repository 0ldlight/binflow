"""L035 / T-562 stage 1 — side probe: version/module-level maven-metadata.xml
auto-materialization on wire PUT (known-divergence
maven/deploy-put-version-metadata-auto-materialize, UNKNOWN / contract ⑨
unobserved arms). FORENSICS + ruling material (dual oracle, expected = live
a; conductor rules, this case only produces the evidence matrix).

Arms (fresh GA, default-behavior maven local; effective repo config
recorded per side):
  m0  release wire PUT pom 1.0.0 -> version-level metadata (…/1.0.0/
      maven-metadata.xml): appears? within what bound? full body captured.
  m1  module-level metadata (…/<art>/maven-metadata.xml): appears?
      versions list content.
  m2  second release wire PUT 1.1.0 -> module metadata merge: versions
      ordering / latest / release fields.
  m3  version-level metadata for 1.1.0.
  m4  DELETE the 1.0.0 pom -> is metadata reclaimed (version list drops
      1.0.0? version-level 1.0.0 metadata 404?).
  m5  checksum sidecar of the materialized version metadata (.sha1).
  m6  SNAPSHOT wire PUT (plain spelling, default home) -> version-level
      metadata within a 60s bound (L033 saw none within 30s) + dir
      listing (rewrite spelling evidence).

Poll budgets are fixed and never auto-widened (rounds must agree).
"""
import importlib.util
import os
import time

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-auto-materialize-probe",
    "title": "FORENSICS (T-562 s1 side probe): wire-PUT auto-materialized "
             "version/module maven-metadata.xml — appear, merge, reclaim, "
             "sidecar, SNAPSHOT timing bound",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 300,
}

REPO = "difftest-l035-mat"
GROUP, ART = "com.diff.mata", "mata"
GP = GROUP.replace(".", "/")
BASE_DIR = "%s/%s" % (GP, ART)

V1, V2, VSNAP = "1.0.0", "1.1.0", "3.0-SNAPSHOT"


def _pom(ver, marker):
    return mavenlib.pom_fixture(GROUP, ART, ver).replace(
        b"</project>", ("<!-- %s -->\n</project>" % marker).encode())


def _put(ctx, side, rel, body):
    import hashlib
    return ctx.http(side, "PUT", "/%s/%s" % (REPO, rel), body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324


def _poll_meta(ctx, side, path, predicate, budget_s):
    t0 = time.monotonic()
    st, body = mavenlib.poll_until(ctx, side, path, predicate,
                                   budget_s=budget_s, interval_s=2.0)
    return st, body, round(time.monotonic() - t0, 1)


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [mavenlib.maven_local(REPO)])
    cfg = ctx.http(side, "GET", "/api/repositories/" + REPO)
    raw["repo_config"] = (cfg["body"].decode("utf-8", "replace")[:600]
                          if cfg["status"] == 200 else "status=%d" % cfg["status"])

    # m0: release PUT 1.0.0 -> version-level metadata
    r = _put(ctx, side, "%s/%s/%s-%s.pom" % (BASE_DIR, V1, ART, V1),
             _pom(V1, "mat-v1"))
    raw["m0_put_status"] = r["status"]
    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR, V1),
        lambda s, b: s == 200, 30.0)
    raw["m0_version_meta"] = {"status": st, "took_s": took,
                              "body": body.decode("utf-8", "replace")[:800]}
    asserts["m0_v1_version_meta"] = "200@%.0fs" % took if st == 200 \
        else "status=%d@30s" % st

    # m5: sidecar of the materialized version metadata
    if st == 200:
        sc = ctx.http(side, "GET", "/%s/%s/%s/maven-metadata.xml.sha1"
                      % (REPO, BASE_DIR, V1))
        sc_body = sc["body"].decode("utf-8", "replace").strip()
        raw["m5_sidecar"] = {"status": sc["status"], "body": sc_body[:60]}
        asserts["m5_version_meta_sha1"] = (
            "200=digest-of-meta" if sc["status"] == 200 and
            sc_body == mavenlib.sha1_hex(body)
            else "200=other" if sc["status"] == 200
            else "status=%d" % sc["status"])

    # m1: module-level metadata
    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR),
        lambda s, b: s == 200, 30.0)
    md = mavenlib.parse_metadata(body) if st == 200 else {}
    raw["m1_module_meta"] = {"status": st, "took_s": took,
                             "versions": md.get("versions"),
                             "latest": md.get("latest"),
                             "release": md.get("release"),
                             "body": body.decode("utf-8", "replace")[:800]}
    asserts["m1_module_meta_versions"] = (
        ",".join(md.get("versions") or []) if st == 200
        else "status=%d" % st)

    # m2: second release version 1.1.0 -> module merge
    r = _put(ctx, side, "%s/%s/%s-%s.pom" % (BASE_DIR, V2, ART, V2),
             _pom(V2, "mat-v2"))
    raw["m2_put_status"] = r["status"]

    def _both(s, b):
        if s != 200:
            return False
        try:
            vs = mavenlib.parse_metadata(b).get("versions") or []
        except Exception:  # noqa: BLE001
            return False
        return V1 in vs and V2 in vs

    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR),
        _both, 30.0)
    md = mavenlib.parse_metadata(body) if st == 200 else {}
    raw["m2_module_meta"] = {"status": st, "took_s": took,
                             "versions": md.get("versions"),
                             "latest": md.get("latest"),
                             "release": md.get("release")}
    asserts["m2_module_versions_after_v2"] = (
        ",".join(md.get("versions") or []) if st == 200
        else "status=%d" % st)
    asserts["m2_latest_release"] = "%s|%s" % (md.get("latest"),
                                              md.get("release"))

    # m3: version-level metadata for 1.1.0
    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR, V2),
        lambda s, b: s == 200, 30.0)
    raw["m3_v2_version_meta"] = {"status": st, "took_s": took,
                                 "body": body.decode("utf-8", "replace")[:400]}
    asserts["m3_v2_version_meta"] = "200@%.0fs" % took if st == 200 \
        else "status=%d" % st

    # m4: DELETE the 1.0.0 pom -> reclaim?
    d = ctx.http(side, "DELETE", "/%s/%s/%s/%s-%s.pom"
                 % (REPO, BASE_DIR, V1, ART, V1))
    raw["m4_delete_status"] = d["status"]

    def _dropped(s, b):
        if s != 200:
            return False
        try:
            vs = mavenlib.parse_metadata(b).get("versions") or []
        except Exception:  # noqa: BLE001
            return False
        return V1 not in vs

    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR),
        _dropped, 20.0)
    md = mavenlib.parse_metadata(body) if st == 200 else {}
    raw["m4_module_meta"] = {"status": st, "took_s": took,
                             "versions": md.get("versions")}
    asserts["m4_module_versions_after_delete"] = (
        ",".join(md.get("versions") or []) if st == 200
        else "status=%d" % st)
    vmeta = ctx.http(side, "GET", "/%s/%s/%s/maven-metadata.xml"
                     % (REPO, BASE_DIR, V1))
    asserts["m4_v1_version_meta_after_delete"] = "status=%d" % vmeta["status"]

    # m6: SNAPSHOT plain wire PUT -> version-level metadata within 60s.
    # Listing FIRST (before any metadata GET) so write-path vs read-
    # triggered materialization stays distinguishable; listing again after
    # the poll to see whether the GET materialized it.
    r = _put(ctx, side, "%s/%s/%s-%s.pom" % (BASE_DIR, VSNAP, ART, VSNAP),
             _pom(VSNAP, "mat-snap"))
    raw["m6_put_status"] = r["status"]
    for k, v in r["headers"].items():
        if k.lower() == "location":
            raw["m6_put_location"] = v
    lst0 = ctx.http(side, "GET",
                    "/api/storage/%s/%s/%s?list&deep=1&listFolders=1"
                    % (REPO, BASE_DIR, VSNAP))
    raw["m6_dir_listing_before_meta_get"] = (
        lst0["body"].decode("utf-8", "replace")[:900]
        if lst0["status"] == 200 else "status=%d" % lst0["status"])
    st, body, took = _poll_meta(
        ctx, side, "/%s/%s/%s/maven-metadata.xml" % (REPO, BASE_DIR, VSNAP),
        lambda s, b: s == 200, 60.0)
    raw["m6_snap_version_meta"] = {"status": st, "took_s": took,
                                   "body": body.decode("utf-8", "replace")[:800]}
    asserts["m6_snap_version_meta_60s"] = "200@%.0fs" % took if st == 200 \
        else "status=%d@60s" % st
    lst = ctx.http(side, "GET", "/api/storage/%s/%s/%s?list&deep=1&listFolders=1"
                   % (REPO, BASE_DIR, VSNAP))
    raw["m6_dir_listing"] = (lst["body"].decode("utf-8", "replace")[:900]
                             if lst["status"] == 200
                             else "status=%d" % lst["status"])

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


DUAL_KEYS = (
    "m0_v1_version_meta", "m1_module_meta_versions",
    "m2_module_versions_after_v2", "m2_latest_release",
    "m3_v2_version_meta", "m4_module_versions_after_delete",
    "m4_v1_version_meta_after_delete", "m6_snap_version_meta_60s",
)


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
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
        "note": "FORENSICS T-562 s1 side probe (auto-materialize ledger "
                "item, UNKNOWN — ruling is the conductor's). Poll budgets "
                "fixed (30/30/30/20/60s), never widened; took_s values are "
                "round-stability inputs, timing-bound conclusions need "
                "consistent rounds."})
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "release PUT x2 + module/version metadata "
                                  "polls + DELETE reclaim + SNAPSHOT PUT 60s "
                                  "bound + sidecar"}]}
