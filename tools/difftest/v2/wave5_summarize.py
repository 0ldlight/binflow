#!/usr/bin/env python3
"""wave5 evidence summarizer — per-case verdicts + key facts from a results.json
+ evidence tree. Read-only; no credentials in any payload."""
import json
import os
import sys

run_dir = sys.argv[1] if len(sys.argv) > 1 else "run/l043-probe"
here = os.path.dirname(os.path.abspath(__file__))
results = json.load(open(os.path.join(here, run_dir, "results.json")))

for case in results["cases"]:
    if case["status"] == "NOT_RUN":
        continue
    print("=====", case["id"], "->", case["status"],
          (case.get("reason") or "")[:200])
    ev_path = os.path.join(here, run_dir, "evidence", case["id"], "checks.json")
    if not os.path.isfile(ev_path):
        print("  (no checks.json)")
        continue
    d = json.load(open(ev_path))
    failed = [c for c in d["checks"] if not c["ok"]]
    print("  checks: %d total, %d failed" % (len(d["checks"]), len(failed)))
    for c in failed[:8]:
        print("   FAIL %s/%s: %s" % (c["side"], c["name"], c["detail"][:170]))
    facts = d.get("facts", {})
    for key in ("p0_fixture_baseline", "topology_context", "tri_state",
                "m_membership", "c_membership", "semantics",
                "crossface_clusters", "strictness", "warm", "t0", "t180",
                "cold-s1", "mut-hot-s2", "mut-hot-negotiate", "mut-hot-s1",
                "mutation_observed"):
        if key in facts:
            print("  facts.%s: %s" % (key, json.dumps(
                facts[key], ensure_ascii=False)[:900]))
    if "cells" in facts:
        for cell, per in facts["cells"].items():
            for side in ("a", "b"):
                p = per[side]
                print("  %-22s %s -> %s chal=%-6s keys=%s msg=%r rec=%s" % (
                    cell, side, p["status"], p["challenge"],
                    p["shape"].get("keys"), (p["shape"].get("message") or "")[:60],
                    p["recurrent_wording"]))
