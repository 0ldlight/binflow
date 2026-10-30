#!/usr/bin/env python3
"""wave3e rerun gate — compare run 1 vs run 2 conclusions per arm.

Two batch runs (evidence suffix "" and "-run2") must agree on every arm's
CONCLUSION (status, body kind, challenge family, classification — nonce- and
count-varying fields excluded). Disagreement = flake, per the wave practice:
a rerun that differs goes to serial re-check before any conclusion is drawn.

Ticket marker: L044-wave3-expansion.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
EV = os.path.join(HERE, "run", "wave3-expansion")

FILES = ["p1_catalog_topology.json", "p3_bearer_matrix.json",
         "p5_tags_accept_matrix.json", "p2_schema1_phase.json"]


def load(suffix):
    out = {}
    for f in FILES:
        root, ext = os.path.splitext(f)
        path = os.path.join(EV, root + suffix + ext)
        if os.path.exists(path):
            with open(path) as fh:
                out[f] = json.load(fh)
    return out


def p1_conclusions(d):
    arms = d["arms"]
    return {
        "T1a_A_kind": arms["T1a-baseline"]["a"]["body_kind"],
        "T1a_B_kind": arms["T1a-baseline"]["b"]["body_kind"],
        "T1b_A_kind": arms["T1b-paginated-n1000"]["a"]["body_kind"],
        "T2_members_identical": arms["T2-members-identical"],
        "T3c_A_has_new": arms["T3c-root-with-local"]["a"]["has_new_local_repo"],
        "T3c_B_has_new": arms["T3c-root-with-local"]["b"]["has_new_local_repo"],
        "T4_A_status": arms["T4-anonymous-root"]["a"]["status"],
        "T4_B_status": arms["T4-anonymous-root"]["b"]["status"],
        "T5_A_status": arms["T5-anon-local-tags"]["a"]["status"],
        "T5_B_status": arms["T5-anon-local-tags"]["b"]["status"],
        "cleanup_ok": all(200 <= s < 300 for s in arms["cleanup"].values())
        and all(s == 404 for s in arms["cleanup_verify_404"].values()),
    }


def p3_conclusions(d):
    out = {}
    for face, cells in d["matrix"].items():
        for key, e in cells.items():
            side, arm = key.split(":", 1)
            out["%s/%s/%s" % (face, side, arm)] = (
                e["status"], e.get("challenge_family"), e.get("classification"))
    return out


def p5_conclusions(d):
    return {name: (arm["sides"]["a"]["status"], arm["sides"]["b"]["status"])
            for name, arm in d["arms"].items()}


def p2_conclusions(d):
    out = {}
    for name, st in d["steps"].items():
        for side in ("a", "b"):
            e = st[side]
            out["%s/%s" % (name, side)] = (
                e["status"], e["content_type"], e["shape"].get("media_type"),
                e["shape"].get("config_media_type"))
    return out


CONCL = {"p1_catalog_topology.json": p1_conclusions,
         "p3_bearer_matrix.json": p3_conclusions,
         "p5_tags_accept_matrix.json": p5_conclusions,
         "p2_schema1_phase.json": p2_conclusions}


def main():
    r1, r2 = load(""), load("-run2")
    facts = {"files": FILES, "compared": {}, "all_match": True}
    for f in FILES:
        if f not in r1 or f not in r2:
            facts["compared"][f] = "MISSING (one run lacks the file)"
            facts["all_match"] = False
            print("%-28s MISSING" % f)
            continue
        c1, c2 = CONCL[f](r1[f]), CONCL[f](r2[f])
        diffs = {k: (c1.get(k), c2.get(k)) for k in set(c1) | set(c2)
                 if c1.get(k) != c2.get(k)}
        facts["compared"][f] = {"cells": len(c1), "diffs": diffs,
                                "match": not diffs}
        if diffs:
            facts["all_match"] = False
        print("%-28s cells=%-3d %s" % (f, len(c1), "MATCH" if not diffs
                                       else "DIFF: %s" % list(diffs)[:8]))
    path = os.path.join(EV, "rerun_gate.json")
    with open(path, "w") as fh:
        json.dump(facts, fh, ensure_ascii=False, indent=2)
    print("RERUN GATE: %s (evidence: %s)" % (
        "PASS" if facts["all_match"] else "FAIL", path))
    return 0 if facts["all_match"] else 1


if __name__ == "__main__":
    sys.exit(main())
