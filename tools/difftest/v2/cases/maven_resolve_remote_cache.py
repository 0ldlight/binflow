"""T-523 case 2: resolve a remote artifact through a virtual (first pull
populates the <remote>-cache projection; second pull is cache-served).

Behavior asserted (spec: docs/reverse/):
  - virtual-resolution.md §2: virtual with a single remote member resolves
    the path via that member (cache bucket precedes remote body).
  - remote-cache-projection.md §3: first GET with empty cache pulls upstream
    and writes the bytes into <K>-cache before streaming back.
  - remote-cache-projection.md §2.1 L56: GET /<K>-cache/<path> serves cached
    content directly -> 200 once populated.
  - remote-cache-projection.md §4: within retrievalCachePeriodSecs (default
    7200s) the entry is fresh, so the second pull is served from cache. The
    cache-HIT itself is not wire-observable without instance logs, so the
    asserted surface is: upstream-exact bytes on both pulls + populated
    projection (deterministic); durations and headers are recorded as
    evidence only.

Upstream fixture (immutable on Maven Central, fetched & hashed 2026-09-25):
  javax.annotation:javax.annotation-api:1.3.2 pom.
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
    "id": "maven-resolve-remote-cache",
    "title": "resolve remote artifact via virtual — first pull writes "
             "<remote>-cache, second pull consistent",
    "layer": "L6",
    "domain": "maven",
    "auth": True,
    "timeout_s": 90,
}

REMOTE, VIRT = "difftest-mvn-remote", "difftest-mvn-virt2"
CENTRAL = "https://repo1.maven.org/maven2"
GAV = "javax/annotation/javax.annotation-api/1.3.2/javax.annotation-api-1.3.2.pom"
UPSTREAM_SHA256 = ("46a4a251ca406e78e4853d7a2bae83282844a4992851439"
                   "ee9a1f23716f06b97")

EXPECTED = {
    "first_resolve": "200+sha256-ok",
    "cache_projection_artifact": "200",
    "second_resolve": "200+sha256-ok",
}


def _timed_get(ctx, side, path):
    t0 = time.monotonic()
    resp = ctx.http(side, "GET", path)
    return resp, int((time.monotonic() - t0) * 1000)


def _leg(ctx, side):
    asserts, raw = {}, {}
    # fresh cache each run: delete-first (deleting a remote wipes its cache,
    # remote-cache-projection.md §1.4), then create remote + virtual.
    mavenlib.cleanup_repos(ctx, side, [VIRT, REMOTE])
    mavenlib.ensure_repos(ctx, side, [
        (REMOTE, {"rclass": "remote", "packageType": "maven", "url": CENTRAL}),
        mavenlib.maven_virtual(VIRT, [REMOTE]),
    ])

    first, ms1 = _timed_get(ctx, side, "/%s/%s" % (VIRT, GAV))
    raw["first"] = {"status": first["status"],
                    "sha256": mavenlib.sha256_hex(first["body"]),
                    "duration_ms": ms1,
                    "headers": {k: v for k, v in first["headers"].items()
                                if k.lower() in ("etag", "last-modified",
                                                 "content-type",
                                                 "x-artifactory-origin-remote-path",
                                                 "x-binflow-cache")}}
    asserts["first_resolve"] = (
        "200+sha256-ok" if first["status"] == 200
        and mavenlib.sha256_hex(first["body"]) == UPSTREAM_SHA256
        else "status=%d sha=%s" % (first["status"],
                                   mavenlib.sha256_hex(first["body"])[:16]))

    cache_probe = ctx.http(side, "GET", "/%s-cache/%s" % (REMOTE, GAV))
    raw["cache_probe_status"] = cache_probe["status"]
    raw["cache_probe_api_storage"] = ctx.http(
        side, "GET", "/api/storage/%s-cache/%s" % (REMOTE, GAV))["status"]
    asserts["cache_projection_artifact"] = "status=%d" % cache_probe["status"]

    second, ms2 = _timed_get(ctx, side, "/%s/%s" % (VIRT, GAV))
    raw["second"] = {"status": second["status"],
                     "sha256": mavenlib.sha256_hex(second["body"]),
                     "duration_ms": ms2}
    asserts["second_resolve"] = (
        "200+sha256-ok" if second["status"] == 200
        and mavenlib.sha256_hex(second["body"]) == UPSTREAM_SHA256
        else "status=%d sha=%s" % (second["status"],
                                   mavenlib.sha256_hex(second["body"])[:16]))
    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, REMOTE])
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT, REMOTE])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "cache HIT judged by populated projection + byte-exact double "
                "resolve; wire-level hit proof needs instance logs (out of "
                "scope for v2 first batch)"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "virt-resolve-first, cache-probe, "
                                  "virt-resolve-second"}]}
