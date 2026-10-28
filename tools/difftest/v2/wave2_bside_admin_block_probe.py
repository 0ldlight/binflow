#!/usr/bin/env python3
"""wave2 Task-1 companion #2 — B-side admin-self-block leg.

The A-side phase-C fact is "correct credentials for the BLOCKED username
itself during the block -> 200". The first B-side spot-check used a
one-time username for the block and admin for the correct-credential probe
(different principals — trivially 200, not comparable). This leg repeats
A's exact protocol on B: hammer the ADMIN username with bad passwords to
arm the block, then present the CORRECT admin credential while admin's own
block is armed, then verify recovery.
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


def get(auth_value, timeout=10.0):
    req = urllib.request.Request(B_BASE + PING, method="GET")
    req.add_header("Authorization", auth_value)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def attempt(pw):
    status, body = get("Basic " + base64.b64encode(
        ("%s:%s" % (B_USER, pw)).encode()).decode())
    text = body.decode("utf-8", "replace")
    m = BLOCK_RE.search(text)
    return {"status": status, "countdown": int(m.group(1)) if m else None,
            "body_head": text[:160]}


def main():
    if not (B_BASE and B_USER and B_PASSWORD):
        print("missing env", file=sys.stderr)
        return 2
    out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run", "wave2")
    os.makedirs(out_dir, exist_ok=True)
    log, facts = [], {}
    bad_pw = "difftest-wrongpass-admin-w2b"

    print("== B admin self-block threshold ==")
    first_block = None
    for i in range(1, 10):
        r = attempt(bad_pw)
        r["attempt"] = i
        log.append({"phase": "admin-threshold", **r})
        print("  attempt %d -> %s cd=%s" % (i, r["status"], r["countdown"]))
        if r["countdown"] is not None:
            first_block = i
            break
        time.sleep(GAP)
    facts["admin_threshold"] = first_block

    if first_block is not None:
        print("== B correct ADMIN credential during ADMIN's own block ==")
        status, body = get("Basic " + base64.b64encode(
            ("%s:%s" % (B_USER, B_PASSWORD)).encode()).decode())
        text = body.decode("utf-8", "replace")
        m = BLOCK_RE.search(text)
        log.append({"phase": "admin-correct-during-block",
                    "status": status, "countdown": m.group(1) if m else None})
        facts["admin_correct_during_own_block_status"] = status
        print("  correct admin creds -> %s (cd=%s)" % (status, m.group(1) if m else None))

    print("== B recovery: correct admin creds poll ==")
    for step in range(1, 15):
        status, body = get("Basic " + base64.b64encode(
            ("%s:%s" % (B_USER, B_PASSWORD)).encode()).decode())
        log.append({"phase": "admin-recovery", "poll_step": step, "status": status})
        print("  poll %d -> %s" % (step, status))
        if status == 200:
            facts["admin_recovery_poll_steps"] = step
            break
        time.sleep(1.0)
    facts["recovery_status"] = status

    facts["B_BASE"] = B_BASE
    facts["started_at"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    path = os.path.join(out_dir, "threshold_experiment_bside_admin.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"facts": facts, "log": log}, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("---- facts ----")
    print(json.dumps(facts, indent=1, ensure_ascii=False))
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
