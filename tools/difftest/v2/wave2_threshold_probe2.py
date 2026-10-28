#!/usr/bin/env python3
"""wave2 Task-1 probe #2 — window semantics under silence + re-arm law.

Follow-up to threshold_experiment.json (probe #1). Three legs, one-time
usernames each, bad Basic on A REST ping, >=1.05s inside-burst spacing:

  Leg1 silence profile (5 users): trip (3 failures) -> quiet D in {1..5}s
      -> single probe. Gives the true first-window W1 (no re-arm confound)
      and 5 fresh samples of the trip countdown value.
  Leg2 fresh-vs-sticky (1 user): trip -> quiet 5s (> W1) -> failure ->
      failure -> failure. Fresh-count predicts 401/401/403(countdown~3);
      sticky-counter predicts 401/403(countdown~9, escalated).
  Leg3 escalation law (1 user, contained): trip -> quiet 4s -> failure ->
      failure (expect re-arm window W2 ~9s) -> quiet 11s (> W2) -> failure
      -> failure -> capture third-window countdown. Stops after capture;
      the one-time user self-heals.

Same discipline as probe #1: read-only endpoint, credentials via env,
nothing printed but statuses/countdowns, full log to evidence JSON.
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

A_BASE = os.environ.get("A_BASE", "").rstrip("/")
A_USER = os.environ.get("A_USER", "")
REST_PING = "/artifactory/api/system/ping"
BLOCK_RE = re.compile(r"please try again in (\d+) second\(s\)")
BURST_GAP_S = 1.05


def get(path, auth_value, timeout=10.0):
    req = urllib.request.Request(A_BASE + path, method="GET")
    req.add_header("Authorization", auth_value)
    t = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status, body = resp.status, resp.read()
    except urllib.error.HTTPError as e:
        status, body = e.code, e.read()
    text = body.decode("utf-8", "replace")
    m = BLOCK_RE.search(text)
    return {"status": status, "countdown": int(m.group(1)) if m else None,
            "latency_ms": int((time.monotonic() - t) * 1000)}


def burst(username, n, log, label):
    """n failing attempts >=1.05s apart; returns list of results."""
    out = []
    for i in range(1, n + 1):
        r = get(REST_PING, "Basic " + base64.b64encode(
            ("%s:wrongpass-w2" % username).encode()).decode())
        r["attempt"] = i
        log.append({"phase": label, **r})
        out.append(r)
        print("  [%s] #%d -> %s cd=%s" % (label, i, r["status"], r["countdown"]))
        if i < n:
            time.sleep(BURST_GAP_S)
    return out


def main():
    if not (A_BASE and A_USER):
        print("missing env: A_BASE / A_USER", file=sys.stderr)
        return 2
    out_dir = os.path.join(os.path.dirname(os.path.abspath(__file__)), "run", "wave2")
    os.makedirs(out_dir, exist_ok=True)
    log = []
    facts = {}
    base_ts = int(time.time())

    print("== Leg1: silence profile, trip countdown + unblock delay ==")
    for k, d in enumerate((1, 2, 3, 4, 5), start=1):
        u = "w2-silence-%d-%d" % (base_ts, k)
        b = burst(u, 3, log, "L1-u%d-trip" % k)
        trip_cd = b[-1]["countdown"]
        time.sleep(d)
        p = get(REST_PING, "Basic " + base64.b64encode(
            ("%s:wrongpass-w2" % u).encode()).decode())
        log.append({"phase": "L1-u%d-probe@%ds" % (k, d), **p})
        print("  [L1-u%d] quiet %ds -> %s cd=%s (trip cd=%s)" % (
            k, d, p["status"], p["countdown"], trip_cd))
        facts.setdefault("silence_profile", {})[str(d)] = {
            "trip_countdown": trip_cd, "probe_status": p["status"],
            "probe_countdown": p["countdown"]}

    print("== Leg2: fresh-count vs sticky-counter after quiet > W1 ==")
    u = "w2-sticky-%d" % base_ts
    burst(u, 3, log, "L2-trip")
    time.sleep(5)
    b = burst(u, 3, log, "L2-after-quiet5s")
    facts["leg2_after_quiet"] = [{"status": r["status"], "countdown": r["countdown"]}
                                 for r in b]

    print("== Leg3: escalation law, third window (contained) ==")
    u = "w2-escal-%d" % base_ts
    burst(u, 3, log, "L3-trip")
    time.sleep(4)
    b2 = burst(u, 2, log, "L3-second-window")
    facts["leg3_second_window"] = [{"status": r["status"], "countdown": r["countdown"]}
                                   for r in b2]
    time.sleep(11)
    b3 = burst(u, 2, log, "L3-third-window")
    facts["leg3_third_window"] = [{"status": r["status"], "countdown": r["countdown"]}
                                  for r in b3]
    facts["note"] = ("leg3 user left to self-heal; no further probing by design")

    facts["A_BASE"] = A_BASE
    facts["started_at"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    path = os.path.join(out_dir, "threshold_experiment2.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"facts": facts, "log": log}, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("---- derived facts ----")
    print(json.dumps(facts, indent=1, ensure_ascii=False))
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
