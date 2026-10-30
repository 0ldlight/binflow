#!/usr/bin/env python3
"""wave3e probe 2b — full-body capture of B's cold-s1 MANIFEST_UNKNOWN envelope.

Run-1 finding (p2 D1): on dev-fdeabbf2 a COLD schema1-only manifest request
through B returns 404 MANIFEST_UNKNOWN "not available in an accepted media
type..." (run-1 evidence truncated the body at 120 chars). This one-off
captures the FULL envelope with a fresh cold tag, plus the A-side parity arm,
then closes the tag's cache row with a schema2 GET (200 expected).

Upstream writes: one run-unique tag (DELETE is 405 -> residue ledger).

Ticket marker: L044-wave3-expansion (probe 2b).
"""
import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "..", "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

TAG = "d1bw3e" + lib.NONCE


def main():
    sides = lib.load_sides()
    facts = {"tag": TAG, "started": lib.epoch()}

    st, src = lib.fetch_upstream_fixture("t2")
    print("upstream fixture t2 -> %s" % st)
    if st != 200:
        return 1
    st, _ = lib.push_upstream_tag(src, TAG)
    facts["upstream_push_status"] = st
    print("upstream push %s -> %s" % (TAG, st))
    if st not in (201, 202):
        lib.write_evidence("p2b_d1_envelope.json", facts)
        return 1
    facts["upstream_residue_ledger"] = {
        "tags": [TAG],
        "note": "upstream DELETE is 405; add to manual GC list (difftest-docker/smoke)"}

    # cold schema1-only request, FULL body captured (evidence-safe: no secrets)
    out = {}
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET",
                     "%s/v2/%s/%s/manifests/%s" % (sides[name]["docker"],
                                                   lib.REPO, lib.IMAGE, TAG),
                     headers={"Accept": lib.ACCEPT_S1})
        out[name] = {
            "status": r["status"],
            "content_type": lib.hdr(r, "content-type"),
            "www_authenticate": lib.hdr(r, "www-authenticate"),
            "body_len": len(r["body"]),
            "body_full": r["body"].decode("utf-8", "replace")[:2000],
            "shape": lib.manifest_shape(r)}
        print("%s cold-s1 -> %s ct=%s len=%d" % (
            name, r["status"], lib.hdr(r, "content-type"), len(r["body"])))
    facts["cold_s1"] = out

    # close the cache row: schema2 GET (B canonical store)
    r = lib.http(sides["b"], "GET",
                 "%s/v2/%s/%s/manifests/%s" % (sides["b"]["docker"],
                                               lib.REPO, lib.IMAGE, TAG),
                 headers={"Accept": lib.ACCEPT_S2})
    facts["b_close_s2"] = {"status": r["status"],
                           "content_type": lib.hdr(r, "content-type")}
    print("B close s2 -> %s %s" % (r["status"], lib.hdr(r, "content-type")))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p2b_d1_envelope.json", facts)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
