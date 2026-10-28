#!/usr/bin/env python3
"""wave2 Task-1 companion — B-side (BinFlow dev-cfa5da05) breaker spot-check.

Dual-facing companion to the A-side probes: the same three facts measured on
A (threshold N, window behaviour, correct-credential-during-block) recorded
live on B's freshly wired T-632/T-633 face, for the convergence question
(recurrent.go threshold 5 vs A's measured 3). One-time username; B's
in-process ledger self-heals; correct-credential recovery verified at the end.
"""
from __future__ import annotations

import base64
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

B_BASE = os.environ.get("B_BASE", "").rstrip("/")
B_USER = os.environ.get("B_USER", "")
B_PASSWORD = os.environ.get("B_PASSWORD", "")
PING = "/binflow/api/system/ping"
BLOCK_RE = re.compile(r"please try again in (\d+) second\(s\)")
GAP = 1.1


def get(path, auth_value, timeout=10.0):
    req = urllib.request.Request(B_BASE + path, method="GET")
    req.add_header("Authorization", auth_value)
    t = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status, body = resp.status, resp.read()
    except urllib.error.HTTPError as e:
        status, body = e.code, e.read()
    return status, body, int((time.monotonic() - t) * 1000)


def attempt(user, pw):
    status, body, latency_ms = get(PING, "Basic " + base64.b64encode(
        ("%s:%s" % (user, pw)).encode()).decode())
    text = body.decode("utf-8", "replace")
    m = BLOCK_RE.search(text)
    return {"status": status, "countdown": int(m.group(1)) if m else None,
            "body_head": text[:160], "latency_ms": latency_ms}


def main():
    if not (B_BASE and B_USER and B_PASSWORD):
        print("missing env: B_BASE / B_USER / B_PASSWORD", file=sys.stderr)
        return 2
    out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run", "wave2")
    os.makedirs(out_dir, exist_ok=True)
    log, facts = [], {}
    bad_pw = "difftest-wrongpass-w2b"
    u = "threshold-count-b-%d" % int(time.time())

    print("== B threshold (user %s) ==" % u)
    first_block = None
    for i in range(1, 9):
        r = attempt(u, bad_pw)
        r["attempt"] = i
        log.append({"phase": "T-threshold", **r})
        print("  attempt %d -> %s cd=%s" % (i, r["status"], r["countdown"]))
        if r["countdown"] is not None:
            first_block = i
            break
        time.sleep(GAP)
    facts["threshold"] = first_block
    facts["trip_countdown"] = r["countdown"]
    facts["trip_body"] = r["body_head"]

    print("== B correct credential during block ==")
    status, body, _ = get(PING, "Basic " + base64.b64encode(
        ("%s:%s" % (B_USER, B_PASSWORD)).encode()).decode())
    log.append({"phase": "C-correct-during-block", "status": status})
    facts["correct_during_block_status"] = status
    print("  correct creds -> %s" % status)

    print("== B window: poll until unblocked (bad creds) ==")
    for step in range(1, 15):
        r = attempt(u, bad_pw)
        r["poll_step"] = step
        log.append({"phase": "R-window-poll", **r})
        print("  poll %d -> %s cd=%s" % (step, r["status"], r["countdown"]))
        if r["countdown"] is None:
            break
        time.sleep(1.0)
    facts["window_poll_steps"] = step
    facts["post_window_status"] = r["status"]

    print("== B recovery: correct creds ==")
    status, body, _ = get(PING, "Basic " + base64.b64encode(
        ("%s:%s" % (B_USER, B_PASSWORD)).encode()).decode())
    log.append({"phase": "C-recovery", "status": status})
    facts["recovery_status"] = status
    print("  correct creds -> %s" % status)

    facts["B_BASE"] = B_BASE
    facts["started_at"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    path = os.path.join(out_dir, "threshold_experiment_bside.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"facts": facts, "log": log}, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("---- facts ----")
    print(json.dumps(facts, indent=1, ensure_ascii=False))
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
