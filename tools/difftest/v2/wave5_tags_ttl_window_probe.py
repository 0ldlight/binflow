#!/usr/bin/env python3
"""wave5 standalone — remote tags/list cache-window sampler (probe 4 extension).

wave1-rerun13b observed A serving a stale tags/list view for >=54 min after an
upstream push. The batch case (dcr-tags-list-liveness) bounds the first 3
minutes; THIS sampler extends the observation toward the actual window:

  W   warm A/B tags/list (fills A's view with the pre-push set)
  U   push run-unique tag TNEW upstream
  S1..S30  every 60s: tags/list on A and B (authed) + upstream truth
  stop when A's view contains TNEW (window closed) or after 30 samples.

Evidence: run/l043-probe/tags_ttl_window.json (statuses/counts/flags only,
no credentials). Exit code always 0 — conclusions live in the JSON + report.
"""
import base64
import json
import os
import time
import urllib.error
import urllib.request

A_BASE = os.environ["A_BASE"].rstrip("/")
B_BASE = os.environ["B_BASE"].rstrip("/")
A_AUTH = base64.b64encode(("%s:%s" % (os.environ["A_USER"],
                                      os.environ["A_PASSWORD"])).encode()).decode()
B_AUTH = base64.b64encode(("%s:%s" % (os.environ["B_USER"],
                                      os.environ["B_PASSWORD"])).encode()).decode()
UPSTREAM = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")

REPO = "diff-docker-remote"
IMAGE = "difftest-docker/smoke"
S2 = "application/vnd.docker.distribution.manifest.v2+json"
TAG_NEW = "w5win" + format(int(time.time() * 1000), "x")
SAMPLES = 30
INTERVAL = 60


def get(url, accept=None, auth=None, method="GET", body=None, ct=None):
    req = urllib.request.Request(url, data=body, method=method)
    if accept:
        req.add_header("Accept", accept)
    if ct:
        req.add_header("Content-Type", ct)
    if auth:
        req.add_header("Authorization", "Basic " + auth)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def tags_snapshot(base, auth):
    st, body = get("%s/v2/%s/%s/tags/list" % (base, REPO, IMAGE), auth=auth)
    try:
        tags = json.loads(body.decode("utf-8")).get("tags") or []
    except Exception:
        tags = None
    return {"status": st, "count": len(tags) if tags is not None else None,
            "has_new": bool(tags) and TAG_NEW in tags}


def main():
    out = {"tag_new": TAG_NEW, "samples": [], "warm": {}, "upstream_push": None}
    # warm: fill A's cached view with the pre-push set
    for name, base, auth in (("a", A_BASE, A_AUTH), ("b", B_BASE, B_AUTH)):
        out["warm"][name] = tags_snapshot(base, auth)
    # push
    st, src = get("%s/v2/%s/manifests/%s" % (UPSTREAM, IMAGE, "t2"), accept=S2)
    push = None
    if st == 200:
        push, _ = get("%s/v2/%s/manifests/%s" % (UPSTREAM, IMAGE, TAG_NEW),
                      body=src, ct=S2, method="PUT")
    out["upstream_push"] = push
    print("warm=%s push=%s tag=%s" % (
        {k: v["status"] for k, v in out["warm"].items()}, push, TAG_NEW), flush=True)
    if push not in (201, 202):
        out["aborted"] = "upstream push failed"
        _write(out)
        return
    closed = None
    for i in range(1, SAMPLES + 1):
        t0 = time.time()
        row = {"i": i, "offset_s": None, "a": tags_snapshot(A_BASE, A_AUTH),
               "b": tags_snapshot(B_BASE, B_AUTH)}
        row["offset_s"] = int(time.time() - t0) + (i - 1) * INTERVAL
        out["samples"].append(row)
        print("sample %2d offset~%3ds a(has_new=%s n=%s) b(has_new=%s n=%s)" % (
            i, row["offset_s"], row["a"]["has_new"], row["a"]["count"],
            row["b"]["has_new"], row["b"]["count"]), flush=True)
        if row["a"]["has_new"]:
            closed = row["offset_s"]
            break
        if i < SAMPLES:
            time.sleep(INTERVAL)
    out["window_closed_at_s"] = closed
    out["conclusion"] = ("A picked up the tag ~%ss after push" % closed
                         if closed is not None else
                         "A still stale after %d samples (~%ds)" % (
                             SAMPLES, SAMPLES * INTERVAL))
    print(out["conclusion"], flush=True)
    _write(out)


def _write(out):
    here = os.path.dirname(os.path.abspath(__file__))
    d = os.path.join(here, "run", "l043-probe")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "tags_ttl_window.json"), "w", encoding="utf-8") as fh:
        json.dump(out, fh, ensure_ascii=False, indent=1)


if __name__ == "__main__":
    main()
