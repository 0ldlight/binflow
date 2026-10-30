#!/usr/bin/env python3
"""wave3e probe 4 — tags/list TTL workload-dependency experiment (task 3).

Established (deep-dive 2026-10-29, B=dev-cfa5da05; single full window):
  A's remote tags/list is a stale cached view, TTL ~2.5h, lazy refresh
  (in-window pure tags reads never refresh; first post-expiry read did).

Open question (this probe): is the in-window inertness WORKLOAD-DEPENDENT —
i.e. does a manifest PULL refresh the tags view while pure tags reads do not?

Protocol (A-side observation; B recorded as live-aggregation control):
  T0        prime A's view with a tags/list read (view excludes the future tag)
  T0+30s    push run-unique nonce tag to the upstream relay (201; DELETE is 405
            -> residue ledger)
  T0+35s    B live check (tag must be immediately visible on B)
  +5/15/25min   PURE reads: tags/list only (expect absent each time)
  +40min    W1 workload arm: manifest GET t1 (hot cache hit, schema2) ->
            immediate tags read  (conductor protocol: "manifest GET once")
  +41min    W2 workload arm: manifest GET of the NONCE TAG ITSELF (cold
            pull-through) -> immediate tags read (strongest pull workload)
  +55/70min PURE reads again (persistence latch check)
  +75min    quiet window ends (gate file touched; other probes may run; every
            flip is timestamped and correlated against their step logs)
  +75..175min  3-min polls (expiry race, deep-dive protocol): first poll that
            contains the tag closes the window; persistence recheck; exit 0.
            If the tag appeared during the workload phase the window is closed
            by workload and the expiry race is recorded as moot.

Crash-safe: evidence JSON rewritten after every step/poll.

Ticket marker: L044-wave3-expansion (probe 4).
"""
import argparse
import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

EVIDENCE_NAME = "p4_ttl_workload.json"
GATE_FILE = os.path.join(HERE, "run", "wave3-expansion", ".p4-quiet-window-done")
TAG = None


def tags_read(sides, side_name):
    r = lib.http(sides[side_name], "GET",
                 "%s/v2/%s/%s/tags/list" % (sides[side_name]["docker"],
                                            lib.REPO, lib.IMAGE))
    try:
        tags = json.loads(r["body"].decode("utf-8")).get("tags")
    except Exception:
        tags = None
    return {"status": r["status"],
            "tag_count": len(tags) if isinstance(tags, list) else None,
            "has_tag": bool(tags and TAG in tags),
            "body_head": r["body"][:160].decode("utf-8", "replace")}


def manifest_read(sides, tag, accept=None):
    headers = {"Accept": accept} if accept else {}
    return lib.http(sides["a"], "GET",
                    "%s/v2/%s/%s/manifests/%s" % (sides["a"]["docker"],
                                                  lib.REPO, lib.IMAGE, tag),
                    headers=headers)


def record(facts, kind, a, b=None, extra=None):
    step = {"kind": kind, "at": lib.epoch(),
            "elapsed_s": int(time.time() - facts["t0_wall"]),
            "a": a}
    if b is not None:
        step["b_has_tag"] = b["has_tag"]
        step["b_tag_count"] = b["tag_count"]
    if extra:
        step.update(extra)
    facts["steps"].append(step)
    path = lib.write_evidence(EVIDENCE_NAME, facts)
    return step, path


def main():
    global TAG
    ap = argparse.ArgumentParser()
    ap.add_argument("--poll-interval", type=int, default=180)
    ap.add_argument("--race-start-min", type=int, default=75,
                    help="start 3-min race polls at N minutes (default 75)")
    ap.add_argument("--deadline-min", type=int, default=175)
    args = ap.parse_args()

    sides = lib.load_sides()
    nonce = lib.NONCE
    TAG = "tw3e" + nonce
    facts = {"tag": TAG, "repo": lib.REPO, "image": lib.IMAGE,
             "probe": "ttl-workload-dependency",
             "interval_s": args.poll_interval,
             "race_start_min": args.race_start_min,
             "deadline_min": args.deadline_min,
             "started": lib.epoch(), "steps": [], "state": "init"}

    # pre-window anchor bookkeeping (T0 may be a cache hit; deep-dive bound correction)
    lib.write_evidence(EVIDENCE_NAME, facts)  # ensure dir exists
    pf_path = os.path.join(lib.EVIDENCE_DIR, "p0_preflight.json")
    if os.path.exists(pf_path):
        try:
            with open(pf_path) as fh:
                pf = json.load(fh)
            facts["pre_window_anchors"] = {
                "preflight_tags_a": pf["checks"]["tags_baseline_a"]["at"],
                "preflight_tags_b": pf["checks"]["tags_baseline_b"]["at"]}
        except Exception:
            pass

    # T0 prime
    t0_wall = time.time()
    facts["t0"] = lib.epoch()
    facts["t0_wall"] = t0_wall
    a0 = tags_read(sides, "a")
    b0 = tags_read(sides, "b")
    facts["t0_view"] = {"a": a0, "b": b0}
    facts["state"] = "primed"
    print("T0 prime: A n=%s has_tag=%s | B n=%s" % (
        a0["tag_count"], a0["has_tag"], b0["tag_count"]))
    if a0["status"] != 200 or b0["status"] != 200:
        facts["state"] = "blocked-prime-non-200"
        lib.write_evidence(EVIDENCE_NAME, facts)
        return 1
    if a0["has_tag"]:
        facts["state"] = "blocked-tag-collision"
        lib.write_evidence(EVIDENCE_NAME, facts)
        return 1
    record(facts, "T0-prime", a0, b0)

    time.sleep(30)
    st, src = lib.fetch_upstream_fixture("t2")
    if st != 200:
        facts["state"] = "blocked-fixture-fetch"
        facts["upstream_push_status"] = st
        lib.write_evidence(EVIDENCE_NAME, facts)
        return 1
    st, body = lib.push_upstream_tag(src, TAG)
    facts["upstream_push_status"] = st
    facts["upstream_residue_ledger"] = {
        "tags": [TAG],
        "note": "upstream DELETE is 405; add to manual GC list (difftest-docker/smoke)"}
    print("T0+30s upstream push %s -> %s" % (TAG, st))
    if st not in (201, 202):
        facts["state"] = "blocked-push-failed"
        lib.write_evidence(EVIDENCE_NAME, facts)
        return 1

    time.sleep(5)
    b1 = tags_read(sides, "b")
    facts["b_live_immediate"] = b1["has_tag"]
    print("T0+35s B live check: has_tag=%s" % b1["has_tag"])
    record(facts, "B-live-check", {"status": None, "tag_count": None,
                                   "has_tag": None}, b1)
    facts["state"] = "in-window"
    lib.write_evidence(EVIDENCE_NAME, facts)

    # schedule: (delay_min, kind)
    schedule = [
        (5, "pure-read"),
        (15, "pure-read"),
        (25, "pure-read"),
        (40, "workload-W1-hot-t1-pull"),
        (41, "workload-W2-cold-nonce-pull"),
        (55, "pure-read"),
        (70, "pure-read"),
    ]
    for delay_min, kind in schedule:
        target = t0_wall + delay_min * 60
        wait = target - time.time()
        if wait > 0:
            time.sleep(wait)
        if kind == "pure-read":
            a = tags_read(sides, "a")
            b = tags_read(sides, "b")
            step, _ = record(facts, kind, a, b)
            print("+%4ds %-28s A n=%s has_tag=%s" % (
                step["elapsed_s"], kind, a["tag_count"], a["has_tag"]))
            if a["has_tag"]:
                facts["flipped_during_inwindow"] = True
                facts["flip_step"] = step
                facts["state"] = "flipped-in-window-pure-read"
                print("UNEXPECTED: tag visible after PURE read — inertness refuted")
                lib.write_evidence(EVIDENCE_NAME, facts)
        else:
            pull_tag = "t1" if kind.endswith("t1-pull") else TAG
            m = manifest_read(sides, pull_tag, accept=lib.ACCEPT_S2)
            md = {"status": m["status"],
                  "content_type": lib.hdr(m, "content-type"),
                  "docker_digest": lib.hdr(m, "docker-content-digest"),
                  "body_len": len(m["body"])}
            a = tags_read(sides, "a")
            b = tags_read(sides, "b")
            step, _ = record(facts, kind, a, b, extra={"pull": pull_tag,
                                                       "pull_resp": md})
            print("+%4ds %-28s pull=%s -> %s | A n=%s has_tag=%s" % (
                step["elapsed_s"], kind, pull_tag, md["status"],
                a["tag_count"], a["has_tag"]))
            if a["has_tag"]:
                facts["flipped_during_inwindow"] = True
                facts["flip_step"] = step
                facts["state"] = "flipped-in-window-workload"
                facts["workload_dependent_refresh"] = True
                print("WORKLOAD-DEPENDENT REFRESH FOUND: manifest pull refreshed the view")
                lib.write_evidence(EVIDENCE_NAME, facts)

    # quiet window ends here: touch the gate so the batch runner may proceed
    try:
        with open(GATE_FILE, "w") as fh:
            fh.write("quiet window ended %s\n" % lib.epoch())
    except OSError:
        pass
    facts["state"] = "racing" if not facts.get("flipped_during_inwindow") \
        else "racing-post-flip"
    lib.write_evidence(EVIDENCE_NAME, facts)

    # race phase: 3-min polls until tag appears or deadline
    deadline = t0_wall + args.deadline_min * 60
    n = 0
    while time.time() < deadline:
        time.sleep(args.poll_interval)
        n += 1
        a = tags_read(sides, "a")
        b = tags_read(sides, "b")
        step, _ = record(facts, "race-poll-%d" % n, a, b)
        print("race +%4ds poll %2d: A has_tag=%s (n=%s) B=%s" % (
            step["elapsed_s"], n, a["has_tag"], a["tag_count"], b["has_tag"]))
        if a["has_tag"]:
            again = tags_read(sides, "a")
            facts["persistence_recheck"] = {"has_tag": again["has_tag"],
                                            "at": lib.epoch()}
            facts["first_seen_elapsed_s"] = step["elapsed_s"]
            if facts.get("flipped_during_inwindow"):
                facts["state"] = "window-closed-earlier-by-workload"
                facts["ttl_bound_seconds"] = None
            else:
                facts["state"] = "window-closed"
                facts["ttl_bound_seconds"] = [
                    (step["elapsed_s"] - args.poll_interval) if n > 1 else None,
                    step["elapsed_s"]]
            facts["finished"] = lib.epoch()
            lib.write_evidence(EVIDENCE_NAME, facts)
            print("WINDOW CLOSED: first_seen=+%ds state=%s" % (
                step["elapsed_s"], facts["state"]))
            return 0

    facts["state"] = "deadline-window-not-closed"
    facts["finished"] = lib.epoch()
    lib.write_evidence(EVIDENCE_NAME, facts)
    print("DEADLINE: window not closed within %d min" % args.deadline_min)
    return 2


if __name__ == "__main__":
    sys.exit(main())
