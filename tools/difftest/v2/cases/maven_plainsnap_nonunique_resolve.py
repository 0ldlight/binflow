"""L034 arm 5 (contract maven/plain-snapshot-path-resolve, T-559 anchor):
plain-SNAPSHOT spelling homed in a NON-UNIQUE snapshot repo (snapshot-
VersionBehavior=non-unique) is stored AS-IS (docs/reverse/maven-npm-pypi.md
§1.3 non-unique row) — member direct GET and virtual resolve must both
answer 200 with the exact stored bytes on BOTH sides.

The UNIQUE-home ctl legs (expected 404 on B until T-562/BIN-44) live in
maven-local-handle-walk-skip's control dims; this case is the GREEN half of
the contract face (the fix's stored-path plane). Dual-oracle on the sha
dims; static 200 expectations from the contract.
"""
import hashlib
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-plainsnap-nonunique-resolve",
    "title": "plain-SNAPSHOT spelling in a non-unique home: member direct + "
             "virtual resolve GET 200 with exact bytes",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 90,
}

NU = "difftest-l034-nu"
NUV = "difftest-l034-nuv"
GROUP, ART, SNAP = "com.diff", "psnu-lib", "1.0.0-SNAPSHOT"
GP = GROUP.replace(".", "/")
POM_REL = "%s/%s/%s/%s-%s.pom" % (GP, ART, SNAP, ART, SNAP)


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(NU, snapshotVersionBehavior="non-unique")])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(NUV, [NU]))

    body = mavenlib.pom_fixture(GROUP, ART, SNAP)
    sha = mavenlib.sha256_hex(body)
    put = ctx.http(side, "PUT", "/%s/%s" % (NU, POM_REL), body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324
    raw["put_status"] = put["status"]
    raw["put_location"] = next(
        (v for k, v in put["headers"].items() if k.lower() == "location"), "")
    if put["status"] not in (200, 201):
        raise mavenlib.SetupError("plain PUT into non-unique refused side %r: %d"
                                  % (side, put["status"]))
    asserts["put_status"] = "%d" % put["status"]
    # storage-as-is guard: the Location must carry the PLAIN spelling (no
    # timestamp rewrite in a non-unique home)
    asserts["put_location_spelling"] = ("plain" if SNAP in raw["put_location"]
                                        else "rewritten")

    g1 = ctx.http(side, "GET", "/%s/%s" % (NU, POM_REL))
    raw["direct_status"] = g1["status"]
    asserts["direct_status"] = "%d" % g1["status"]
    asserts["direct_sha"] = ("sha-ok" if g1["status"] == 200 and
                             mavenlib.sha256_hex(g1["body"]) == sha
                             else "mismatch-or-%d" % g1["status"])
    g2 = ctx.http(side, "GET", "/%s/%s" % (NUV, POM_REL))
    raw["virt_status"] = g2["status"]
    asserts["virt_status"] = "%d" % g2["status"]
    asserts["virt_sha"] = ("sha-ok" if g2["status"] == 200 and
                           mavenlib.sha256_hex(g2["body"]) == sha
                           else "mismatch-or-%d" % g2["status"])
    # sidecar faces through both planes (contract unobserved arms — dual
    # oracle, honest either way)
    for tag, base in (("direct", NU), ("virt", NUV)):
        sc = ctx.http(side, "GET", "/%s/%s.sha1" % (base, POM_REL))
        raw["sidecar_%s_status" % tag] = sc["status"]
        ok = (sc["status"] == 200 and
              sc["body"].decode("utf-8", "replace").strip()
              == mavenlib.sha1_hex(body))
        asserts["sidecar_%s_sha1" % tag] = "match" if ok else (
            "status=%d" % sc["status"] if sc["status"] != 200 else "mismatch")

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


EXPECTED = {
    "put_status": "201",  # A answers 201 Created on first deploy; B aligned
    "put_location_spelling": "plain",
    "direct_status": "200",
    "direct_sha": "sha-ok",
    "virt_status": "200",
    "virt_sha": "sha-ok",
    "sidecar_direct_sha1": "match",
    "sidecar_virt_sha1": "match",
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
            mavenlib.cleanup_repos(ctx, s, [NUV, NU])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "contract face: plain spelling stored as-is in non-unique "
                "home (§1.3), both planes serve 200 exact bytes; sidecar "
                "dims are the contract's unobserved arms graded dual-oracle"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "non-unique local + virtual; wire-PUT "
                                  "plain-SNAPSHOT pom; direct/virt GET + "
                                  ".sha1 sidecars"}]}
