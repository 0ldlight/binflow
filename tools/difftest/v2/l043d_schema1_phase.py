#!/usr/bin/env python3
"""L043 deep-dive probe 2 — schema1 Accept negotiation phase dependency.

Candidate (known-divergence.yaml docker/remote-schema1-conversion):
  A = per-request deterministic negotiation (serve-time schema2->schema1
      conversion, cache-transparent);
  B = cache-phase dependent (hot row serves as-is ignoring Accept; cold row
      lets Accept participate in the upstream fetch AND caches the response).

Four-direction matrix (hot/served x Accept), plus the C-b question: does a
COLD schema1 fetch pollute B's cached representation so later schema2 clients
get prettyjws?

Sequence (tags t1 hot-normalized first, then two fresh upstream tags):
  P0  prime t1 with schema2 Accept on both sides (normalize hot row)
  H1  t1 + s1 Accept            (A prettyjws? / B schema2 = hot phase?)
  H2  t1 + s2 Accept
  H2b t1 + s2 Accept again      (A: conversion was serve-time, row unpolluted?)
  H3  t1 + no Accept
  H4  t1 + mixed Accept         (wave2: both schema2 — re-verify)
  D1  cold tag1 + s1 Accept     (A prettyjws / B prettyjws via upstream zot)
  D2  tag1 + s2 Accept          (A schema2 / B prettyjws = C-b POLLUTED?)
  D3  tag1 + no Accept
  E1  cold tag2 + s2 Accept
  E2  tag2 + s1 Accept          (A prettyjws / B schema2 = unconverted)
  E3  tag2 + no Accept

Upstream writes: two run-unique tags (manifest copied from fixture t2; blobs
shared). Upstream DELETE is 405 -> residue ledger. No A/B writes.

Ticket marker: L043-deepdive (probe 2 of 5).
"""
import importlib.util
import json
import os
import sys
import time

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

TAG1 = "d1l043d" + lib.NONCE
TAG2 = "d2l043d" + lib.NONCE


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
        print("%-4s %-14s %-8s %s -> %s ct=%s media=%s cfg=%s" % (
            label, tag[:14], out["accept"][:22], name,
            entry["status"], (entry["content_type"] or "")[:52],
            entry["shape"].get("media_type"),
            entry["shape"].get("config_media_type")))
    return out


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag1", default=None,
                    help="reuse an existing upstream tag for the cold-s1 arm")
    ap.add_argument("--tag2", default=None,
                    help="reuse an existing upstream tag for the cold-s2 arm")
    args = ap.parse_args()
    global TAG1, TAG2
    if args.tag1:
        TAG1 = args.tag1
    if args.tag2:
        TAG2 = args.tag2

    sides = lib.load_sides()
    facts = {"nonce": lib.NONCE, "tags": [TAG1, TAG2], "started": lib.epoch(),
             "steps": {}}

    # upstream fixture manifest (schema2 bytes, blobs already present)
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

    # cold tag1: s1 first, then s2 (C-b pollution check), then no Accept
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
    facts["upstream_residue_ledger"] = {
        "tags": [TAG1, TAG2],
        "note": "upstream DELETE is 405; add to manual GC list (difftest-docker/smoke)"}
    path = lib.write_evidence("p2_schema1_phase.json", facts)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
