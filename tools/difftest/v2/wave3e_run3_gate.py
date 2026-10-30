#!/usr/bin/env python3
"""wave3e run-3 gate — third-run stability confirmation vs run 1.

Run 3 covers the write-free probes only (p1 catalog topology, p3 bearer
matrix, p5 tags Accept 406): probe 2's cold-phase arms push new unique tags
to the upstream relay (DELETE 405, ledger-carried residue) and its run1/run2
agreement is already gated, so run 3 skips it to conserve upstream residue.

Same per-arm conclusion extraction as wave3e_rerun_gate.py (imported, not
duplicated). Evidence: run/wave3-expansion/<probe>-run3.json vs "".

Ticket marker: L044-wave3-expansion (second dispatch addendum).
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

import wave3e_rerun_gate as gate  # noqa: E402

FILES = ["p1_catalog_topology.json", "p3_bearer_matrix.json",
         "p5_tags_accept_matrix.json"]
RUN3_SUFFIX = "-run3"


def load(suffix):
    out = {}
    for f in FILES:
        root, ext = os.path.splitext(f)
        path = os.path.join(gate.EV, root + suffix + ext)
        if os.path.exists(path):
            with open(path) as fh:
                out[f] = json.load(fh)
    return out


def main():
    r1, r3 = load(""), load(RUN3_SUFFIX)
    facts = {"files": FILES, "run3_suffix": RUN3_SUFFIX,
             "skipped": ["p2_schema1_phase.json (upstream residue conservation;"
                         " run1/run2 gated)", "p4_ttl_workload (single-window"
                         " experiment, not repeated)"],
             "compared": {}, "all_match": True}
    for f in FILES:
        if f not in r1 or f not in r3:
            facts["compared"][f] = "MISSING (one run lacks the file)"
            facts["all_match"] = False
            print("%-28s MISSING" % f)
            continue
        c1 = gate.CONCL[f](r1[f])
        c3 = gate.CONCL[f](r3[f])
        diffs = {k: (c1.get(k), c3.get(k)) for k in set(c1) | set(c3)
                 if c1.get(k) != c3.get(k)}
        facts["compared"][f] = {"cells": len(c1), "diffs": diffs,
                                "match": not diffs}
        if diffs:
            facts["all_match"] = False
        print("%-28s cells=%-3d %s" % (f, len(c1), "MATCH" if not diffs
                                       else "DIFF: %s" % list(diffs)[:8]))
    path = os.path.join(gate.EV, "rerun_gate_run3.json")
    with open(path, "w") as fh:
        json.dump(facts, fh, ensure_ascii=False, indent=2)
    print("RUN-3 GATE: %s (evidence: %s)" % (
        "PASS" if facts["all_match"] else "FAIL", path))
    return 0 if facts["all_match"] else 1


if __name__ == "__main__":
    sys.exit(main())
