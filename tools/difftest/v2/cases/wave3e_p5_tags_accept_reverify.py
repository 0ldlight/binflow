#!/usr/bin/env python3
"""wave3e probe 5 — tags/list Accept negotiation re-verification
(L043 candidate-face 2: A 406 rule vs B permissive; re-run on current deploys).

Deep-dive rule under test (A=7.161.26):
  406 <=> Accept header present AND contains neither application/json nor */*
  (text/html also 406 — the rule is content-type based, not media-type based)
  B answers 200 on all arms.

9 arms: none / application/json / schema2 / manifest.list / schema1 /
oci-index / */* / mixed s2+json / text/html.

Ticket marker: L044-wave3-expansion (probe 5).
"""
import importlib.util
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "..", "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

ARMS = [
    ("none", None),
    ("appjson", "application/json"),
    ("s2", lib.ACCEPT_S2),
    ("s2list", lib.ACCEPT_S2_LIST),
    ("s1", lib.ACCEPT_S1),
    ("oci-index", lib.ACCEPT_OCI_INDEX),
    ("star", "*/*"),
    ("mixed-s2-appjson", lib.ACCEPT_S2 + ", application/json"),
    ("texthtml", "text/html"),
]


def main():
    sides = lib.load_sides()
    facts = {"repo": lib.REPO, "image": lib.IMAGE,
             "started": lib.epoch(), "arms": {}}
    for arm_name, accept in ARMS:
        headers = {"Accept": accept} if accept else {}
        per_side = {}
        for side_name in ("a", "b"):
            r = lib.http(sides[side_name], "GET",
                         "%s/v2/%s/%s/tags/list" % (sides[side_name]["docker"],
                                                    lib.REPO, lib.IMAGE),
                         headers=headers)
            entry = lib.digest_resp(r, body_head=160)
            try:
                doc = json.loads(r["body"].decode("utf-8"))
                entry["body_kind"] = ("errors" if isinstance(doc, dict) and "errors" in doc
                                      else ("tags-body" if isinstance(doc, dict) and "tags" in doc
                                            else "other-json"))
            except Exception:
                entry["body_kind"] = "non-json"
            per_side[side_name] = entry
        same = per_side["a"]["status"] == per_side["b"]["status"]
        facts["arms"][arm_name] = {"accept": accept or "(none)",
                                   "sides": per_side, "status_equal": same}
        print("arm %-16s A=%s(%s) B=%s(%s) %s" % (
            arm_name, per_side["a"]["status"], per_side["a"]["body_kind"],
            per_side["b"]["status"], per_side["b"]["body_kind"],
            "SAME" if same else "DIVERGE"))

    # derived rule check for A
    rule_hits, rule_misses = [], []
    for arm_name, accept in ARMS:
        a = facts["arms"][arm_name]["sides"]["a"]
        expect_406 = accept is not None and "application/json" not in accept and "*/*" not in accept
        observed = (a["status"] == 406)
        (rule_hits if expect_406 == observed else rule_misses).append(arm_name)
    facts["a_rule_406"] = {
        "statement": "406 <=> Accept present and contains neither application/json nor */*",
        "hits": rule_hits, "misses": rule_misses,
        "holds": not rule_misses}
    print("A 406 rule: holds=%s misses=%s" % (facts["a_rule_406"]["holds"], rule_misses))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p5_tags_accept_matrix.json", facts)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
