#!/usr/bin/env python3
"""L043 deep-dive probe 4 — remote repo tags/list freshness cache-TTL window.

Candidate (known-divergence.yaml docker/remote-tags-list-freshness-and-406,
face 1): A's remote tags/list is a NON-LIVE cached view — an upstream push is
invisible until the view's staleness TTL expires and a later request triggers
the refresh; B aggregates live (push visible on the next request).

Protocol (anchors the window to a known refresh moment):
  T0   prime A's view (GET tags/list) — refresh happens here with a view that
       EXCLUDES the not-yet-pushed tag; also records B's live baseline.
  T0+30s  push a run-unique tag to the upstream relay (manifest copied from
       fixture t2). Upstream DELETE is 405 -> residue ledger.
  T0+35s  B live check (tag must already be visible on B).
  poll every --interval seconds: GET A tags/list, record presence. First poll
       that contains the tag closes the window: TTL_bound = [prev_poll - T0,
       first_seen - T0] seconds (corrected below if T0 was a cache hit).
  on found: immediate re-request (persistence latch), then exit 0.
  on deadline: record "window not closed within deadline" and exit 2.

Bound correction: if A's view had been refreshed by earlier tags/list traffic
(probe 5) shortly BEFORE T0, the actual window anchor is that earlier refresh
moment; the probe records the p5 finish timestamp when present so the report
can correct first_seen - T0 accordingly.

Run in background; state is rewritten to evidence JSON after EVERY poll
(crash-safe). No A/B writes; one upstream residue tag per run.

Ticket marker: L043-deepdive (probe 4 of 5).
"""
import argparse
import importlib.util
import json
import os
import sys
import time

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

EVIDENCE_NAME = "p4_tags_freshness_ttl.json"


def tags_list(sides, side_name):
    r = lib.http(sides[side_name], "GET",
                 "%s/v2/%s/%s/tags/list" % (sides[side_name]["docker"],
                                            lib.REPO, lib.IMAGE))
    try:
        tags = json.loads(r["body"].decode("utf-8")).get("tags")
    except Exception:
        tags = None
    return {"status": r["status"], "tags": tags,
            "body_head": r["body"][:200].decode("utf-8", "replace")}


def save(facts):
    path = lib.write_evidence(EVIDENCE_NAME, facts)
    return path


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--interval", type=int, default=180,
                    help="poll interval seconds (default 180)")
    ap.add_argument("--deadline-min", type=int, default=150,
                    help="abandon after N minutes (default 150)")
    args = ap.parse_args()

    sides = lib.load_sides()
    nonce = lib.NONCE
    tag = "tf" + nonce
    facts = {"tag": tag, "repo": lib.REPO, "image": lib.IMAGE,
             "interval_s": args.interval, "deadline_min": args.deadline_min,
             "started": lib.epoch(), "polls": [], "state": "init"}

    # bound-correction reference: when did tags/list traffic last touch A?
    # p0 = fixture refixture+warm (a full refresh moment); p5 = Accept matrix
    for ref_name, ref_file in (("refixture_warm", "p0_refixture.json"),
                               ("p5_tags_matrix", "p5_tags_accept_matrix.json")):
        ref_path = os.path.join(lib.EVIDENCE_DIR, ref_file)
        if os.path.exists(ref_path):
            try:
                with open(ref_path) as fh:
                    ref = json.load(fh)
                facts.setdefault("last_pre_window_tags_traffic", {})[ref_name] = \
                    ref.get("finished")
            except Exception:
                pass

    # T0 prime: refresh A's view BEFORE the upstream push
    t0_wall = time.time()
    facts["t0"] = lib.epoch()
    a0 = tags_list(sides, "a")
    b0 = tags_list(sides, "b")
    facts["t0_view"] = {"a": a0, "b": b0}
    facts["state"] = "primed"
    print("T0 prime done. A tags=%s" % (a0["tags"],))
    print("T0 B tags=%s" % (b0["tags"],))
    if a0["status"] != 200 or b0["status"] != 200:
        facts["state"] = "blocked-prime-non-200"
        save(facts)
        return 1
    if a0["tags"] and tag in a0["tags"]:
        facts["state"] = "blocked-tag-collision"
        save(facts)
        return 1
    save(facts)

    time.sleep(30)
    st, src = lib.fetch_upstream_fixture("t2")
    if st != 200:
        facts["state"] = "blocked-fixture-fetch"
        facts["upstream_push_status"] = st
        save(facts)
        return 1
    st, body = lib.push_upstream_tag(src, tag)
    facts["upstream_push_status"] = st
    print("T0+30s upstream push %s -> %s" % (tag, st))
    if st not in (201, 202):
        facts["state"] = "blocked-push-failed"
        save(facts)
        return 1

    time.sleep(5)
    b1 = tags_list(sides, "b")
    facts["b_live_check"] = b1
    facts["b_live_immediate"] = bool(b1["tags"] and tag in b1["tags"])
    print("B live check: tag visible=%s" % facts["b_live_immediate"])
    facts["state"] = "polling"
    save(facts)

    deadline = t0_wall + args.deadline_min * 60
    n = 0
    while time.time() < deadline:
        n += 1
        time.sleep(args.interval)
        wall = time.time()
        a = tags_list(sides, "a")
        b = tags_list(sides, "b")
        seen = bool(a["tags"] and tag in a["tags"])
        poll = {"n": n, "at": lib.epoch(), "elapsed_s": int(wall - t0_wall),
                "a_status": a["status"],
                "a_tag_count": len(a["tags"]) if isinstance(a["tags"], list) else None,
                "a_has_tag": seen,
                "b_has_tag": bool(b["tags"] and tag in b["tags"])}
        facts["polls"].append(poll)
        print("poll %2d (+%ds): A has_tag=%s (status=%s, n=%s) B has_tag=%s" % (
            n, poll["elapsed_s"], seen, a["status"], poll["a_tag_count"],
            poll["b_has_tag"]))
        if seen:
            facts["first_seen_elapsed_s"] = poll["elapsed_s"]
            again = tags_list(sides, "a")
            facts["persistence_recheck"] = {
                "has_tag": bool(again["tags"] and tag in again["tags"]),
                "at": lib.epoch()}
            facts["state"] = "window-closed"
            facts["finished"] = lib.epoch()
            facts["ttl_bound_seconds"] = [
                (poll["elapsed_s"] - args.interval) if n > 1 else None,
                poll["elapsed_s"]]
            save(facts)
            print("WINDOW CLOSED: tag first seen at +%ds (bound %s). evidence saved." % (
                poll["elapsed_s"], facts["ttl_bound_seconds"]))
            return 0
        save(facts)

    facts["state"] = "deadline-window-not-closed"
    facts["finished"] = lib.epoch()
    save(facts)
    print("DEADLINE: window not closed within %d min" % args.deadline_min)
    return 2


if __name__ == "__main__":
    sys.exit(main())
