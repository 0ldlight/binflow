#!/usr/bin/env python3
"""wave5 re-run gate — compare run/l043-probe vs run/l043-probe-2.

PASS requires, for every executed case (status != NOT_RUN):
  - identical status on both runs;
  - identical first-failure reason after nonce normalization
    (run-unique repo/tag/username tokens w5* are replaced by <NZ>);
  - identical check ledgers after the same normalization (name, side, ok).

Prints one JSON verdict line and exits 0 either way (state travels in JSON).
"""
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
NZ_PATTERNS = [
    (re.compile(r"w5[a-z0-9]*[0-9a-f]{4,}"), "<NZ>"),   # w5-prefixed nonce tokens
    (re.compile(r"diff-cat5-[0-9a-f]+"), "<NZ>"),
    (re.compile(r"count=\d+"), "count=<N>"),            # upstream residue growth
]


def norm(obj):
    text = json.dumps(obj, sort_keys=True, ensure_ascii=False)
    for pat, repl in NZ_PATTERNS:
        text = pat.sub(repl, text)
    return json.loads(text)


def load(run_dir):
    data = json.load(open(os.path.join(HERE, run_dir, "results.json")))
    cases = {}
    for c in data["cases"]:
        if c["status"] == "NOT_RUN":
            continue
        checks = []
        ev_rel = (c.get("evidence") or [None])[0]
        if ev_rel:
            p = os.path.join(HERE, run_dir, ev_rel)
            if os.path.isfile(p):
                d = json.load(open(p))
                checks = [{"side": x["side"], "name": x["name"], "ok": x["ok"]}
                          for x in d.get("checks", [])]
        cases[c["id"]] = {"status": c["status"],
                          "reason": norm(c.get("reason")),
                          "checks": norm(checks)}
    return cases


def main():
    a = load(sys.argv[1] if len(sys.argv) > 1 else "run/l043-probe")
    b = load(sys.argv[2] if len(sys.argv) > 2 else "run/l043-probe-2")
    problems = []
    for cid in sorted(set(a) | set(b)):
        if cid not in a or cid not in b:
            problems.append({"case": cid, "issue": "missing in one run"})
            continue
        if a[cid]["status"] != b[cid]["status"]:
            problems.append({"case": cid, "issue": "status %s vs %s" % (
                a[cid]["status"], b[cid]["status"])})
            continue
        if a[cid]["reason"] != b[cid]["reason"]:
            problems.append({"case": cid, "issue": "reason differs",
                             "r1": a[cid]["reason"], "r2": b[cid]["reason"]})
        if a[cid]["checks"] != b[cid]["checks"]:
            ca, cb = a[cid]["checks"], b[cid]["checks"]
            delta = [{"check": x, "ok1": x in ca and ca[ca.index(x)]["ok"]}
                     for x in cb if x not in ca] + \
                    [{"check": x, "ok2": x in cb and cb[cb.index(x)]["ok"]}
                     for x in ca if x not in cb]
            ok_mismatch = []
            for x in ca:
                match = [y for y in cb if y["side"] == x["side"] and y["name"] == x["name"]]
                if match and match[0]["ok"] != x["ok"]:
                    ok_mismatch.append(x["name"])
            problems.append({"case": cid, "issue": "check ledger differs",
                             "only_run1": len([x for x in ca if x not in cb]),
                             "only_run2": len([x for x in cb if x not in ca]),
                             "ok_mismatch": ok_mismatch[:6]})
    print(json.dumps({"rerun_gate": "PASS" if not problems else "FAIL",
                      "cases_compared": sorted(set(a) & set(b)),
                      "problems": problems}, ensure_ascii=False, indent=1))


if __name__ == "__main__":
    main()
