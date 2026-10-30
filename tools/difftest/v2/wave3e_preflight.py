#!/usr/bin/env python3
"""wave3e preflight — instance health, fixture liveness, deployment versions.

Gate for the wave-3 expansion batch: both sides must answer REST ping 200,
the fixture remote repo must serve manifest t1 (schema2 Accept) on both sides,
the upstream relay must serve t2, and B's version must be recorded
(re-verification target: L043 conclusions were pinned on dev-cfa5da05).

Ticket marker: L044-wave3-expansion.
"""
import importlib.util
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)


def main():
    sides = lib.load_sides()
    facts = {"started": lib.epoch(), "checks": {}}

    # REST ping both sides
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET", "%s/system/ping" % sides[name]["api"])
        facts["checks"]["rest_ping_" + name] = {
            "status": r["status"], "body_head": r["body"][:120].decode("utf-8", "replace")}
        print("rest ping %s -> %s" % (name, r["status"]))

    # B version (deployment identity for the re-verification)
    r = lib.http(sides["b"], "GET", "%s/system/version" % sides["b"]["api"])
    facts["checks"]["b_version"] = {
        "status": r["status"], "body_head": r["body"][:200].decode("utf-8", "replace")}
    print("B version -> %s %s" % (r["status"], r["body"][:120].decode("utf-8", "replace")))

    # fixture liveness: manifest t1 through both remotes (schema2)
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET",
                     "%s/v2/%s/%s/manifests/t1" % (sides[name]["docker"],
                                                   lib.REPO, lib.IMAGE),
                     headers={"Accept": lib.ACCEPT_S2})
        entry = lib.digest_resp(r, body_head=80)
        entry["shape"] = lib.manifest_shape(r)
        facts["checks"]["fixture_manifest_t1_" + name] = entry
        print("fixture t1 %s -> %s ct=%s len=%s" % (
            name, r["status"], entry["content_type"], entry["body_len"]))

    # upstream relay t2
    st, body = lib.fetch_upstream_fixture("t2")
    facts["checks"]["upstream_t2"] = {"status": st, "len": len(body)}
    print("upstream t2 -> %s (%d bytes)" % (st, len(body)))

    # tags baseline both sides (pre-window traffic bookkeeping for the TTL probe)
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET",
                     "%s/v2/%s/%s/tags/list" % (sides[name]["docker"],
                                                lib.REPO, lib.IMAGE))
        try:
            tags = json.loads(r["body"].decode("utf-8")).get("tags")
        except Exception:
            tags = None
        facts["checks"]["tags_baseline_" + name] = {
            "status": r["status"], "tag_count": len(tags) if isinstance(tags, list) else None,
            "at": lib.epoch()}
        print("tags baseline %s -> %s n=%s" % (name, r["status"],
                                               facts["checks"]["tags_baseline_" + name]["tag_count"]))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p0_preflight.json", facts)
    print("evidence: %s" % path)
    ok = (facts["checks"]["rest_ping_a"]["status"] == 200
          and facts["checks"]["rest_ping_b"]["status"] == 200
          and facts["checks"]["fixture_manifest_t1_a"]["status"] == 200
          and facts["checks"]["fixture_manifest_t1_b"]["status"] == 200
          and facts["checks"]["upstream_t2"]["status"] == 200)
    print("PREFLIGHT: %s" % ("PASS" if ok else "FAIL"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
