#!/usr/bin/env python3
"""wave1-arms — negative-cache TTL +6min re-probe (arm-1 dispatch leg).

The runner case dcr-negative-cache-ttl plants a negative-cache row with a
run-unique tag (404 -> upstream gains the tag -> immediate re-GET). This
probe completes the dispatch's "+6min 后再发是否仍 404" observation:
after the batch, re-GET the SAME tag on both sides and record whether the
negative cache still holds (404) or has expired into a 200 (fetch-through).

Reads the planted tag from run/wave1-arms/evidence/dcr-negative-cache-ttl/
checks.json (facts.tag). Standalone probe (wave convention), NOT a runner
case: a 360s sleep would stall the batch.

Usage: python3 wave1_arms_ttl6m_probe.py [--tag TAG] [--wait-s N] [--out PATH]
Env contract identical to runner cases (A_BASE/B_BASE/A_USER/A_PASSWORD/
B_USER/B_PASSWORD). Evidence: statuses + Content-Type + sha256 + body head,
never Authorization values. Output: one-line JSON verdict to stdout.
"""
import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "cases"))
import _dockerlib as dl  # noqa: E402

REASON_PHASE = {
    200: "expired (fetch-through answered 200)",
    404: "negative cache still holds",
}


def basic_auth_header(user, password):
    import base64
    raw = ("%s:%s" % (user, password)).encode()
    return "Basic " + base64.b64encode(raw).decode()


def get_manifest(base, user, password, repo, image, tag):
    req = urllib.request.Request(
        "%s/v2/%s/%s/manifests/%s" % (base, repo, image, tag), method="GET")
    req.add_header("Accept", dl.ACCEPT_MANIFEST)
    req.add_header("Authorization", basic_auth_header(user, password))
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, dict(resp.headers), resp.read()
    except urllib.error.HTTPError as e:
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return e.code, dict(e.headers or {}), raw


def upstream_request(method, path, body=None, content_type=None, timeout=15.0):
    upstream = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")
    if not upstream:
        raise RuntimeError("DIFFTEST_UPSTREAM_BASE not set")
    req = urllib.request.Request(upstream + path, data=body, method=method)
    req.add_header("Accept", dl.ACCEPT_MANIFEST)
    if content_type:
        req.add_header("Content-Type", content_type)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tag", default=None,
                    help="planted negative-cache tag; default: read from "
                         "run/wave1-arms evidence")
    ap.add_argument("--wait-s", type=int, default=360,
                    help="seconds to wait before the probe (default 360 = +6min)")
    ap.add_argument("--plant", action="store_true",
                    help="plant fresh negative rows first: GET the tag on both "
                         "sides (expect 404) before waiting; requires --tag")
    ap.add_argument("--push-upstream", action="store_true",
                    help="after planting, copy the fixture manifest to the "
                         "tag on the upstream relay (DIFFTEST_UPSTREAM_BASE). "
                         "REQUIRED for a discriminating A-side probe: without "
                         "it an expired row refetches a 404 and A's status "
                         "cannot distinguish row-held from row-expired.")
    ap.add_argument("--out", default=os.path.join(
        HERE, "run", "wave1-arms", "ttl6m_probe.json"))
    args = ap.parse_args()

    env = {k: os.environ.get(k, "") for k in
           ("A_BASE", "B_BASE", "A_USER", "A_PASSWORD", "B_USER", "B_PASSWORD")}
    missing = [k for k, v in env.items() if not v]
    if missing:
        print(json.dumps({"status": "BLOCKED", "missing_env": missing}))
        return 2

    tag = args.tag
    if not tag:
        ev_path = os.path.join(HERE, "run", "wave1-arms", "evidence",
                               "dcr-negative-cache-ttl", "checks.json")
        try:
            with open(ev_path) as f:
                tag = json.load(f)["facts"]["tag"]
        except Exception as e:
            print(json.dumps({"status": "BLOCKED",
                              "reason": "cannot read planted tag: %r" % e,
                              "evidence_path": ev_path}))
            return 2

    probe = {"tag": tag, "wait_s": args.wait_s}
    if args.plant:
        plant = {}
        for side, base, user, password in (
                ("a", env["A_BASE"], env["A_USER"], env["A_PASSWORD"]),
                ("b", env["B_BASE"], env["B_USER"], env["B_PASSWORD"])):
            status, _, _ = get_manifest(base, user, password, dl.REPO, dl.IMAGE, tag)
            plant[side] = status
        probe["plant_status"] = plant
        if not all(s == 404 for s in plant.values()):
            probe["verdict"] = "invalid-plant (negative row not established)"
            os.makedirs(os.path.dirname(args.out), exist_ok=True)
            with open(args.out, "w") as f:
                json.dump(probe, f, indent=2, sort_keys=True)
            print(json.dumps({k: probe[k] for k in ("tag", "verdict",
                                                    "plant_status")}))
            return 1
        if args.push_upstream:
            fixture = "%s/smoke" % dl.IMAGE.rsplit("/", 1)[0]
            status, src = upstream_request(
                "GET", "/v2/%s/manifests/%s" % (fixture, dl.POSITIVE_TAG))
            if status != 200:
                probe["verdict"] = "invalid-plant (fixture fetch %s)" % status
                os.makedirs(os.path.dirname(args.out), exist_ok=True)
                with open(args.out, "w") as f:
                    json.dump(probe, f, indent=2, sort_keys=True)
                print(json.dumps({"tag": tag, "verdict": probe["verdict"]}))
                return 1
            put_status, _ = upstream_request(
                "PUT", "/v2/%s/manifests/%s" % (fixture, tag),
                body=src, content_type=dl.ACCEPT_MANIFEST)
            probe["upstream_push_status"] = put_status
            if put_status not in (201, 202):
                probe["verdict"] = "invalid-plant (upstream push %s)" % put_status
                os.makedirs(os.path.dirname(args.out), exist_ok=True)
                with open(args.out, "w") as f:
                    json.dump(probe, f, indent=2, sort_keys=True)
                print(json.dumps({"tag": tag, "verdict": probe["verdict"],
                                  "upstream_push_status": put_status}))
                return 1
    # the +Nmin interval sits BETWEEN row establishment and the re-GET
    if args.wait_s > 0:
        time.sleep(args.wait_s)

    probe["probed_at_epoch"] = int(time.time())
    bodies = {}
    for side, base_u, base_p in (
            ("a", env["A_BASE"], (env["A_USER"], env["A_PASSWORD"])),
            ("b", env["B_BASE"], (env["B_USER"], env["B_PASSWORD"]))):
        status, headers, body = get_manifest(
            base_u, base_p[0], base_p[1], dl.REPO, dl.IMAGE, tag)
        bodies[side] = body
        probe[side] = {
            "status": status,
            "phase": REASON_PHASE.get(status, "other (%s)" % status),
            "content_type": dl.hdr({"headers": headers, "body": b""},
                                   "Content-Type"),
            "body_sha256": dl.sha256_hex(body),
            "body_head": body[:200].decode("utf-8", "replace"),
        }
    probe["ab_status_equal"] = (probe["a"]["status"] == probe["b"]["status"])
    probe["ab_body_verbatim_equal"] = (bodies["a"] == bodies["b"])
    probe["both_still_404"] = (probe["a"]["status"] == 404
                               and probe["b"]["status"] == 404)
    if probe["both_still_404"]:
        probe["verdict"] = "both-still-404 (windows >= 6min on both sides)"
    elif not probe["ab_status_equal"]:
        probe["verdict"] = "SPLIT (new-divergence candidate)"
    else:
        probe["verdict"] = "both-%s" % probe["a"]["status"]

    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    with open(args.out, "w") as f:
        json.dump(probe, f, indent=2, sort_keys=True)
    print(json.dumps({k: probe[k] for k in
                      ("tag", "verdict", "ab_status_equal",
                       "ab_body_verbatim_equal", "both_still_404")}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
