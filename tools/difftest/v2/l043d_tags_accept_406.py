#!/usr/bin/env python3
"""L043 deep-dive probe 5 — tags/list Accept negotiation matrix.

Candidate: GET /v2/{remote-repo}/{image}/tags/list with a manifest Accept
header -> A 406 {"errors":[{"status":406,"message":"Not Acceptable"}]} vs
B 200 normal tags body (known-divergence.yaml
docker/remote-tags-list-freshness-and-406, face 2).

This probe pins the negotiation RULE, not just the single arm:
  - which Accept values trigger the strict rejection on A
  - whether the rejection is media-type-class based (any manifest type) or
    content-type based (any non-application/json type)
  - no-Accept baseline and */* wildcards

Read-only, run before the tags-freshness TTL window (its A-side tags/list
traffic refreshes A's cached view; the TTL probe anchors on that).

Ticket marker: L043-deepdive (probe 5 of 5).
"""
import importlib.util
import json
import os
import sys

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

TAG = "t1"
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
    facts = {"repo": lib.REPO, "image": lib.IMAGE, "tag": TAG,
             "started": lib.epoch(), "arms": {}}
    for arm_name, accept in ARMS:
        headers = {"Accept": accept} if accept else {}
        per_side = {}
        for side_name in ("a", "b"):
            r = lib.http(sides[side_name], "GET",
                         "%s/v2/%s/%s/tags/list" % (sides[side_name]["docker"],
                                                    lib.REPO, lib.IMAGE),
                         headers=headers)
            entry = lib.digest_resp(r, body_head=200)
            try:
                doc = json.loads(r["body"].decode("utf-8"))
                entry["body_kind"] = ("errors" if isinstance(doc, dict) and "errors" in doc
                                      else ("tags-body" if isinstance(doc, dict) and "tags" in doc
                                            else "other-json"))
            except Exception:
                entry["body_kind"] = "non-json"
            per_side[side_name] = entry
        same = per_side["a"]["status"] == per_side["b"]["status"]
        facts["arms"][arm_name] = {"sides": per_side, "status_equal": same}
        print("arm %-16s A=%s B=%s %s" % (
            arm_name, per_side["a"]["status"], per_side["b"]["status"],
            "SAME" if same else "DIVERGE"))
        print("   A: %s" % json.dumps(per_side["a"], ensure_ascii=False)[:260])
        print("   B: %s" % json.dumps(per_side["b"], ensure_ascii=False)[:260])

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p5_tags_accept_matrix.json", facts)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
