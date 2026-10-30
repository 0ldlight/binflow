#!/usr/bin/env python3
"""wave3e probe 2 — schema1 Accept negotiation phase dependency re-verification
(L043 candidate 2, established on B=dev-cfa5da05; this run on B=dev-fdeabbf2).

Deep-dive conclusions under test:
  A = per-request deterministic negotiation, cache transparent
      ({schema1, no Accept} -> prettyjws served; {schema2, mixed} -> schema2)
  B = cold-fetch negotiation stored once; hot rows serve as-is ignoring Accept
  C-b = TRUE: a COLD schema1 fetch pollutes B's cached representation —
      later schema2 clients receive prettyjws (D2/D3); reverse direction does
      not convert (E2 hot s1 -> schema2)

Matrix: P0 prime t1 (schema2) -> hot arms H1..H4 -> two fresh upstream nonce
tags -> cold arms D1..D3 (s1 first) and E1..E3 (s2 first).

Upstream writes: two run-unique tags (manifest copied from fixture t2; blobs
shared). Upstream DELETE is 405 -> residue ledger. No A/B writes.

Ticket marker: L044-wave3-expansion (probe 2).
"""
import argparse
import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

TAG1 = "d1w3e" + lib.NONCE
TAG2 = "d2w3e" + lib.NONCE


def get_manifest(side, tag, accept):
    headers = {"Accept": accept} if accept else {}
    return lib.http(side, "GET",
                    "%s/v2/%s/%s/manifests/%s" % (side["docker"], lib.REPO,
                                                  lib.IMAGE, tag),
                    headers=headers)


def step(label, sides, tag, accept):
    out = {"accept": accept or "(none)"}
    for name in ("a", "b"):
        r = get_manifest(sides[name], tag, accept)
        entry = lib.digest_resp(r, body_head=120)
        entry["shape"] = lib.manifest_shape(r)
        out[name] = entry
        print("%-4s %-16s %-20s %s -> %s ct=%s media=%s cfg=%s" % (
            label, tag[:14], out["accept"][:20], name,
            entry["status"], (entry["content_type"] or "")[:52],
            entry["shape"].get("media_type"),
            entry["shape"].get("config_media_type")))
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag1", default=None)
    ap.add_argument("--tag2", default=None)
    args = ap.parse_args()
    global TAG1, TAG2
    if args.tag1:
        TAG1 = args.tag1
    if args.tag2:
        TAG2 = args.tag2

    sides = lib.load_sides()
    facts = {"nonce": lib.NONCE, "tags": [TAG1, TAG2], "started": lib.epoch(),
             "steps": {}}

    status, src = lib.fetch_upstream_fixture("t2")
    print("upstream fixture t2 fetch -> %s (%d bytes)" % (status, len(src)))
    if status != 200:
        lib.write_evidence("p2_schema1_phase.json", facts)
        return 1
    pushes = {}
    if not args.tag1:
        st, body = lib.push_upstream_tag(src, TAG1)
        pushes[TAG1] = st
        print("upstream push %s -> %s" % (TAG1, st))
    if not args.tag2:
        st, body = lib.push_upstream_tag(src, TAG2)
        pushes[TAG2] = st
        print("upstream push %s -> %s" % (TAG2, st))
    facts["upstream_push_status"] = pushes or "reused-existing-tags"
    facts["upstream_residue_ledger"] = {
        "tags": list(pushes) if pushes else [TAG1, TAG2],
        "note": "upstream DELETE is 405; add to manual GC list (difftest-docker/smoke)"}
    if pushes and not all(s in (201, 202) for s in pushes.values()):
        lib.write_evidence("p2_schema1_phase.json", facts)
        return 1

    # P0 prime + hot matrix on t1
    facts["steps"]["P0-prime-s2"] = step("P0", sides, "t1", lib.ACCEPT_S2)
    time.sleep(0.4)
    facts["steps"]["H1-t1-s1"] = step("H1", sides, "t1", lib.ACCEPT_S1)
    time.sleep(0.4)
    facts["steps"]["H2-t1-s2"] = step("H2", sides, "t1", lib.ACCEPT_S2)
    time.sleep(0.4)
    facts["steps"]["H2b-t1-s2-again"] = step("H2b", sides, "t1", lib.ACCEPT_S2)
    time.sleep(0.4)
    facts["steps"]["H3-t1-noacc"] = step("H3", sides, "t1", None)
    time.sleep(0.4)
    facts["steps"]["H4-t1-mixed"] = step(
        "H4", sides, "t1", lib.ACCEPT_S1 + ", " + lib.ACCEPT_S2)
    time.sleep(0.4)

    # cold tag1: s1 first (C-b pollution check), then s2, then no Accept
    facts["steps"]["D1-cold-s1"] = step("D1", sides, TAG1, lib.ACCEPT_S1)
    time.sleep(0.4)
    facts["steps"]["D2-tag1-s2"] = step("D2", sides, TAG1, lib.ACCEPT_S2)
    time.sleep(0.4)
    facts["steps"]["D3-tag1-noacc"] = step("D3", sides, TAG1, None)
    time.sleep(0.4)

    # cold tag2: s2 first, then s1, then no Accept
    facts["steps"]["E1-cold-s2"] = step("E1", sides, TAG2, lib.ACCEPT_S2)
    time.sleep(0.4)
    facts["steps"]["E2-tag2-s1"] = step("E2", sides, TAG2, lib.ACCEPT_S1)
    time.sleep(0.4)
    facts["steps"]["E3-tag2-noacc"] = step("E3", sides, TAG2, None)

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p2_schema1_phase.json", facts)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
