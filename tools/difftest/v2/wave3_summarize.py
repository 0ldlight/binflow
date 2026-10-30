#!/usr/bin/env python3
"""wave3 evidence summarizer — prints per-case verdicts and key facts from a
results.json + evidence tree. Read-only; no credentials in any payload."""
import json
import os
import sys

run_dir = sys.argv[1] if len(sys.argv) > 1 else "run/wave3"
here = os.path.dirname(os.path.abspath(__file__))
results = json.load(open(os.path.join(here, run_dir, "results.json")))

for case in results["cases"]:
    if case["status"] == "NOT_RUN":
        continue
    print("=====", case["id"], "->", case["status"],
          (case.get("reason") or "")[:160])
    ev_path = os.path.join(here, run_dir, "evidence", case["id"], "checks.json")
    if not os.path.isfile(ev_path):
        print("  (no checks.json)")
        continue
    d = json.load(open(ev_path))
    failed = [c for c in d["checks"] if not c["ok"]]
    print("  checks: %d total, %d failed" % (len(d["checks"]), len(failed)))
    for c in failed[:6]:
        print("   FAIL %s/%s: %s" % (c["side"], c["name"], c["detail"][:170]))
    for key in ("p0_fixture_baseline", "p1_membership", "p3_membership",
                "p2_anonymous", "semantics", "crossface_inconsistent_shapes"):
        if key in d.get("facts", {}):
            print("  facts.%s: %s" % (key, json.dumps(
                d["facts"][key], ensure_ascii=False)[:700]))
    if "cells" in d.get("facts", {}):
        for cell, per in d["facts"]["cells"].items():
            for side in ("a", "b"):
                p = per[side]
                print("  %-22s %s -> %s chal=%-6s keys=%s msg=%r rec=%s" % (
                    cell, side, p["status"], p["challenge"],
                    p["shape"].get("keys"), (p["shape"].get("message") or "")[:60],
                    p["recurrent_wording"]))
