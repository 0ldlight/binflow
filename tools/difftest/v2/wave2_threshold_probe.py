#!/usr/bin/env python3
"""wave2 Task-1 probe — precise recurrent-failure breaker counting on A (JFrog 7.161.26).

A-side-only controlled experiment (not a dual case): one-time usernames, bad
Basic credentials, >=1.2s between attempts. Produces the three facts that
decide whether internal/auth/recurrent.go converges threshold 5 -> N:
  T: exact attempt index of the first 403 "recurrent request failures" block
  R: block window behaviour (how long 403 persists; is the counter reset
     to a fresh count after the window lapses -> second trip threshold)
  C: does a CORRECT credential for the blocked username also get 403 while
     the block is armed (block keyed per-username vs per-credential)
plus two scope facts recorded along the way:
  K: is the block keyed per-username (different username unaffected)
  X: is the counter shared across faces (REST /artifactory/api/* vs docker /v2/*)

Discipline: read-only endpoints only; one-time usernames; no reference
configuration touched; credentials enter via env (A_BASE/A_USER/A_PASSWORD)
and are never printed; evidence JSON carries statuses and body heads only.
"""
from __future__ import annotations

import base64
import json
import os
import sys
import time
import urllib.error
import urllib.request

A_BASE = os.environ.get("A_BASE", "").rstrip("/")
A_USER = os.environ.get("A_USER", "")
A_PASSWORD = os.environ.get("A_PASSWORD", "")

REST_PING = "/artifactory/api/system/ping"
DOCKER_PING = "/v2/"
BLOCK_SUBSTR = "recurrent request failures"

HAMMER_INTERVAL_S = 1.2   # task floor: >=1s between attempts
POLL_INTERVAL_S = 2.0
MAX_HAMMER = 12           # wave-1 said ~2-3; 12 is generous headroom
MAX_POLL_STEPS = 20       # window claimed 4s; 40s ceiling is plenty


def basic(user, password):
    return "Basic " + base64.b64encode(
        ("%s:%s" % (user, password)).encode()).decode()


def get(path, auth_value, timeout=10.0):
    req = urllib.request.Request(A_BASE + path, method="GET")
    req.add_header("Authorization", auth_value)
    t0 = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status, headers, body = resp.status, dict(resp.headers), resp.read()
    except urllib.error.HTTPError as e:
        status, headers, body = e.code, dict(e.headers or {}), e.read()
    dt = time.monotonic() - t0
    return {
        "status": status,
        "latency_ms": int(dt * 1000),
        "www_authenticate": headers.get("WWW-Authenticate"),
        "content_type": headers.get("Content-Type"),
        "body_head": body[:300].decode("utf-8", "replace"),
        "blocked": BLOCK_SUBSTR in body.decode("utf-8", "replace"),
    }


def hammer(username, password, path, label, log):
    """Send failing attempts until the block text appears; returns the
    1-based attempt index of the first 403-blocked response (None if never)."""
    first_block = None
    for i in range(1, MAX_HAMMER + 1):
        r = get(path, basic(username, password))
        r["attempt"] = i
        log.append({"phase": label, **r})
        print("  [%s] attempt %d -> %s %s" % (
            label, i, r["status"], "(BLOCKED)" if r["blocked"] else r["body_head"][:60]))
        if r["blocked"]:
            first_block = i
            break
        time.sleep(HAMMER_INTERVAL_S)
    return first_block


def poll_until_unblocked(username, password, path, label, log):
    """Poll with bad credentials until the response is no longer the block.
    Returns (steps_until_unblocked, final_response)."""
    for step in range(1, MAX_POLL_STEPS + 1):
        r = get(path, basic(username, password))
        r["poll_step"] = step
        log.append({"phase": label, **r})
        print("  [%s] poll %d -> %s %s" % (
            label, step, r["status"], "(BLOCKED)" if r["blocked"] else r["body_head"][:60]))
        if not r["blocked"]:
            return step, r
        time.sleep(POLL_INTERVAL_S)
    return None, r


def main():
    if not (A_BASE and A_USER and A_PASSWORD):
        print("missing env: A_BASE / A_USER / A_PASSWORD", file=sys.stderr)
        return 2
    out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run", "wave2")
    os.makedirs(out_dir, exist_ok=True)
    log = []
    facts = {}

    BAD = "difftest-wrongpass-20261028"
    U1 = "threshold-count-20261028"
    U2 = "threshold-count-b-%d" % int(time.time())

    print("== Phase T: threshold count (user %s, REST face %s) ==" % (U1, REST_PING))
    n1 = hammer(U1, BAD, REST_PING, "T-threshold-rest", log)
    facts["threshold_rest_face"] = n1

    print("== Phase K1: different username while U1 blocked ==")
    r = get(REST_PING, basic(U2, BAD))
    log.append({"phase": "K1-other-username", **r})
    facts["other_username_while_blocked_status"] = r["status"]
    print("  [%s] -> %s %s" % ("K1", r["status"], r["body_head"][:60]))

    print("== Phase K2: admin CORRECT credential while U1 blocked ==")
    r = get(REST_PING, basic(A_USER, A_PASSWORD))
    log.append({"phase": "K2-admin-correct", **r})
    facts["admin_correct_while_u1_blocked_status"] = r["status"]
    print("  [%s] -> %s" % ("K2", r["status"]))

    print("== Phase X: same blocked username on the docker face ==")
    r = get(DOCKER_PING, basic(U1, BAD))
    log.append({"phase": "X-docker-face", **r})
    facts["docker_face_same_username_while_blocked_status"] = r["status"]
    facts["docker_face_same_username_blocked_text"] = r["blocked"]
    print("  [%s] -> %s blocked=%s" % ("X", r["status"], r["blocked"]))

    print("== Phase R1: window lapse poll (bad creds, U1) ==")
    steps, r = poll_until_unblocked(U1, BAD, REST_PING, "R1-window-poll", log)
    facts["window_poll_steps_to_unblock"] = steps
    facts["post_window_status"] = r["status"]

    print("== Phase R2: re-trip after window (fresh count?) ==")
    n2 = hammer(U1, BAD, REST_PING, "R2-retrip", log)
    facts["threshold_rest_face_second_trip"] = n2

    print("== Phase R3: window lapse poll again ==")
    steps2, r = poll_until_unblocked(U1, BAD, REST_PING, "R3-window-poll", log)
    facts["window_poll_steps_to_unblock_2nd"] = steps2

    print("== Phase C: admin bad-cred hammer, then CORRECT cred during block ==")
    nc = hammer(A_USER, BAD, REST_PING, "C-admin-threshold", log)
    facts["threshold_admin_username"] = nc
    if nc is not None:
        r = get(REST_PING, basic(A_USER, A_PASSWORD))
        log.append({"phase": "C-admin-correct-during-block", **r})
        facts["admin_correct_during_own_block_status"] = r["status"]
        print("  [C] admin correct cred during block -> %s %s" % (
            r["status"], r["body_head"][:60]))
        print("== Phase C-recovery: admin correct-cred poll ==")
        for step in range(1, MAX_POLL_STEPS + 1):
            r = get(REST_PING, basic(A_USER, A_PASSWORD))
            log.append({"phase": "C-admin-recovery", "poll_step": step, **r})
            print("  [C-recovery] poll %d -> %s" % (step, r["status"]))
            if r["status"] == 200:
                facts["admin_recovery_poll_steps"] = step
                break
            time.sleep(POLL_INTERVAL_S)
    else:
        facts["admin_correct_during_own_block_status"] = None

    facts["A_BASE"] = A_BASE
    facts["one_time_users"] = sorted({U1, U2})
    facts["started_at"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    path = os.path.join(out_dir, "threshold_experiment.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"facts": facts, "log": log}, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("---- derived facts ----")
    print(json.dumps(facts, indent=1, ensure_ascii=False))
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
