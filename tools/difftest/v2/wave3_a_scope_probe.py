#!/usr/bin/env python3
"""wave3 Task-1 — A-side (JFrog 7.161.26) breaker-ledger SCOPE semantics.

BinFlow-side established fact (T-632/T-635 design input): the failure ledger
is keyed by username and SHARED across routes — alternating bad credentials
over two routes trips the block on the combined count (budget effectively
multiplies by route count). This probe measures whether the reference is
isomorphic:

  Phase A (alternating trip): one-time username, bad Basic credentials,
    alternating GET /artifactory/api/system/ping and
    GET /artifactory/api/repositories, 4 rounds (8 requests), >=1.1s apart.
      - shared ledger  -> first 403 at request 3 (2 accumulated failures)
      - per-route ledger -> first 403 at request 5 (ping route: 1,2,403)
  Phase B (blocked-scope confirm): one bad request on EACH route + the docker
    root /v2/ — all should render the block message with a shared countdown.
  Phase C (window self-heal): sleep ~4.6s (past the measured 4s window),
    single bad request -> expect 401 (healed); immediate next bad request ->
    403 re-arm if the counter is sticky (L043 measured sticky; recorded here
    as the cross-check leg).

No users are created; the one-time username does not exist on A and leaves no
residue. Evidence carries statuses/countdowns/body heads only — never the
credentials. Verdict words (shared / per-route) are recorded as `scope_hint`;
classification authority stays with compatibility-engineer.
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
A_PASSWORD = os.environ.get("A_PASSWORD", "")
USER = os.environ.get("SCOPE_USER") or ("semantics-a-%s" % format(int(time.time()), "x"))
ROUTES = [
    ("rest-ping", "/artifactory/api/system/ping"),
    ("rest-repositories", "/artifactory/api/repositories"),
]
GAP_S = 1.15
BLOCK_RE = re.compile(r"please try again in (\d+) second\(s\)")
RECURRENT_RE = re.compile(r"recurrent", re.IGNORECASE)


def get(path, auth_value, timeout=10.0):
    req = urllib.request.Request(A_BASE + path, method="GET")
    if auth_value:
        req.add_header("Authorization", auth_value)
    t = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            status, body = resp.status, resp.read()
    except urllib.error.HTTPError as e:
        status, body = e.code, e.read()
    return status, body, int((time.monotonic() - t) * 1000)


def bad_basic():
    token = base64.b64encode(("%s:difftest-wrongpass-w3" % USER).encode()).decode()
    return "Basic " + token


def attempt(route_path):
    status, body, latency_ms = get(route_path, bad_basic())
    text = body.decode("utf-8", "replace")
    m = BLOCK_RE.search(text)
    return {"status": status, "countdown": int(m.group(1)) if m else None,
            "recurrent_wording": bool(RECURRENT_RE.search(text)),
            "body_head": text[:200], "latency_ms": latency_ms}


def main():
    if not (A_BASE and A_USER and A_PASSWORD):
        print("missing env: A_BASE / A_USER / A_PASSWORD", file=sys.stderr)
        return 2
    here = os.path.dirname(os.path.abspath(__file__))
    out_dir = os.path.join(here, "run", "wave3")
    os.makedirs(out_dir, exist_ok=True)
    log, facts = [], {"user": USER, "routes": [r[0] for r in ROUTES]}

    print("== A scope: alternating trip (user %s) ==" % USER)
    first_403 = None
    seq = 0
    for rnd in range(1, 5):
        for name, path in ROUTES:
            seq += 1
            r = attempt(path)
            r.update({"seq": seq, "round": rnd, "route": name})
            log.append({"phase": "A-alternating", **r})
            print("  req %02d round %d %-18s -> %s cd=%s" % (
                seq, rnd, name, r["status"], r["countdown"]))
            if r["status"] == 403 and first_403 is None:
                first_403 = r
                break
            time.sleep(GAP_S)
        if first_403 is not None:
            break

    facts["first_403_seq"] = first_403["seq"] if first_403 else None
    facts["first_403_route"] = first_403["route"] if first_403 else None
    fails_before = sum(1 for e in log
                       if e["phase"] == "A-alternating" and e["status"] == 401)
    facts["failures_before_403"] = fails_before
    if first_403:
        if first_403["seq"] == 3:
            facts["scope_hint"] = ("shared: trip at combined request 3 "
                                   "(2 accumulated cross-route failures)")
        elif first_403["seq"] == 5:
            facts["scope_hint"] = ("per-route: trip at request 5 (route-local "
                                   "count reached 2 on ping before any 403)")
        else:
            facts["scope_hint"] = "other: first 403 at seq %s" % first_403["seq"]
        facts["trip_countdown"] = first_403["countdown"]
        facts["trip_recurrent_wording"] = first_403["recurrent_wording"]
        facts["trip_body_head"] = first_403["body_head"]
    else:
        facts["scope_hint"] = "no 403 within 8 alternating requests"
    print("  scope_hint: %s" % facts["scope_hint"])

    if first_403:
        print("== blocked-scope confirm (bad creds, one per route) ==")
        for name, path in ROUTES + [("docker-root", "/v2/")]:
            r = attempt(path)
            r.update({"route": name})
            log.append({"phase": "B-blocked-confirm", **r})
            print("  %-18s -> %s cd=%s" % (name, r["status"], r["countdown"]))
        facts["blocked_confirm"] = [
            {"route": e["route"], "status": e["status"], "countdown": e["countdown"]}
            for e in log if e["phase"] == "B-blocked-confirm"]

        print("== window self-heal: sleep 4.6s, single shot, then re-arm check ==")
        time.sleep(4.6)
        r = attempt(ROUTES[0][1])
        log.append({"phase": "C-post-window-single", **r})
        facts["post_window_status"] = r["status"]
        facts["post_window_countdown"] = r["countdown"]
        print("  post-window single -> %s cd=%s" % (r["status"], r["countdown"]))
        time.sleep(GAP_S)
        r2 = attempt(ROUTES[0][1])
        log.append({"phase": "C-rearm-check", **r2})
        facts["rearm_status"] = r2["status"]
        facts["rearm_countdown"] = r2["countdown"]
        print("  re-arm check       -> %s cd=%s" % (r2["status"], r2["countdown"]))

    facts["A_BASE"] = A_BASE
    facts["started_at"] = time.strftime("%Y-%m-%dT%H:%M:%S%z")
    path = os.path.join(out_dir, "scope_experiment_a.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump({"facts": facts, "log": log}, fh, ensure_ascii=False, indent=1)
        fh.write("\n")
    print("---- facts ----")
    print(json.dumps(facts, indent=1, ensure_ascii=False))
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
