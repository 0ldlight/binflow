#!/usr/bin/env python3
"""wave5 standalone — schema1 cache-poisoning recovery sampler (C-b boundary).

L044 open question: does B's polluted cache (a cold schema1 pull cached the
prettyjws representation) self-heal at TTL expiry? This sampler:

  U   push run-unique tag TPOI upstream (deterministic fixture manifest bytes)
  P   B cold pull with schema1-only Accept -> pollutes B's cache (C-b setup);
      A cold pull recorded too (canonical schema2, serve-time conversion)
  c0  immediate follow-up schema2 read on both sides (pollution proof at t0)
  R1..R30  every 60s: schema2-accept read on both sides; record representation
  stop when B's read returns the schema2 representation again (healed) or
  after 30 samples (~30 min).

Caveat recorded in the JSON: if hot reads REFRESH the cache TTL, the poison
never expires under sampling and the conclusion is "not healed under 60s-read
cadence within the observed span" — inconclusive on absolute TTL.

Evidence: run/l043-probe/schema1_recovery.json. Exit code always 0.
"""
import base64
import hashlib
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
S1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"
TAG_POI = "w5rec" + format(int(time.time() * 1000), "x")
SAMPLES = 30
INTERVAL = 60

LAYER = b"A" * 64
LAYER_SHA = hashlib.sha256(LAYER).hexdigest()
CONFIG = ('{"architecture":"amd64","os":"linux","created":"2026-10-28T00:00:00Z",'
          '"config":{},"rootfs":{"type":"layers","diff_ids":["sha256:%s"]},'
          '"history":[{"created":"2026-10-28T00:00:00Z","created_by":"difftest wave"}]}'
          % LAYER_SHA).encode()
CFG_SHA = hashlib.sha256(CONFIG).hexdigest()
MANIFEST = ('{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",'
            '"config":{"mediaType":"application/vnd.docker.container.image.v1+json",'
            '"size":%d,"digest":"sha256:%s"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip",'
            '"size":%d,"digest":"sha256:%s"}]}'
            % (len(CONFIG), CFG_SHA, len(LAYER), LAYER_SHA)).encode()
CONFIG_DIGEST = "sha256:" + CFG_SHA


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


def read_view_full(base, auth, accept):
    """Manifest read capturing status + Content-Type header + parsed shape."""
    req = urllib.request.Request(
        "%s/v2/%s/%s/manifests/%s" % (base, REPO, IMAGE, TAG_POI))
    req.add_header("Accept", accept)
    if auth:
        req.add_header("Authorization", "Basic " + auth)
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            return _view(r.status, dict(r.headers), r.read())
    except urllib.error.HTTPError as e:
        return _view(e.code, dict(e.headers or {}), e.read())


def _view(st, hdrs, body):
    ct = None
    for k, v in hdrs.items():
        if k.lower() == "content-type":
            ct = v
    try:
        doc = json.loads(body.decode("utf-8"))
        mt = doc.get("mediaType") or ""
        cfg = (doc.get("config") or {}).get("digest")
        s1 = ("prettyjws" in (ct or "")) or ("prettyjws" in mt)
    except Exception:
        mt, cfg, s1 = "", None, None
    return {"status": st, "ct": ct, "media_type": mt,
            "config": cfg, "schema1_shape": s1}


def main():
    out = {"tag": TAG_POI, "upstream_push": None, "pollute": {}, "c0": {},
           "samples": [], "caveat": ("hot reads during sampling may refresh the "
                                     "cache TTL; a non-heal under this cadence is "
                                     "inconclusive on absolute TTL")}
    st, src = get("%s/v2/%s/manifests/%s" % (UPSTREAM, IMAGE, "t2"), accept=S2)
    if st == 200:
        push, _ = get("%s/v2/%s/manifests/%s" % (UPSTREAM, IMAGE, TAG_POI),
                      body=src, ct=S2, method="PUT")
        out["upstream_push"] = push
    else:
        out["upstream_push"] = "fixture-fetch-%s" % st
    print("push=%s tag=%s" % (out["upstream_push"], TAG_POI), flush=True)
    if out["upstream_push"] not in (201, 202):
        out["aborted"] = "upstream push failed"
        _write(out)
        return
    # P: pollute (cold schema1 on both sides; B is the target)
    for name, base, auth in (("a", A_BASE, A_AUTH), ("b", B_BASE, B_AUTH)):
        out["pollute"][name] = read_view_full(base, auth, S1)
    # c0: immediate schema2 follow-up
    for name, base, auth in (("a", A_BASE, A_AUTH), ("b", B_BASE, B_AUTH)):
        out["c0"][name] = read_view_full(base, auth, S2)
    print("pollute a(s1=%s) b(s1=%s) | c0 a(s1=%s) b(s1=%s)" % (
        out["pollute"]["a"]["schema1_shape"], out["pollute"]["b"]["schema1_shape"],
        out["c0"]["a"]["schema1_shape"], out["c0"]["b"]["schema1_shape"]), flush=True)
    healed = None
    for i in range(1, SAMPLES + 1):
        time.sleep(INTERVAL)
        row = {"i": i, "a": read_view_full(A_BASE, A_AUTH, S2),
               "b": read_view_full(B_BASE, B_AUTH, S2)}
        out["samples"].append(row)
        print("sample %2d a(s1=%s) b(s1=%s cfg=%s)" % (
            i, row["a"]["schema1_shape"], row["b"]["schema1_shape"],
            (row["b"]["config"] or "")[:20]), flush=True)
        if row["b"]["schema1_shape"] is False:
            healed = i * INTERVAL
            break
    out["healed_at_s"] = healed
    out["conclusion"] = ("B cache healed ~%ss after pollution" % healed
                         if healed is not None else
                         "B still serving polluted representation after ~%ds of "
                         "60s-cadence reads" % (SAMPLES * INTERVAL))
    print(out["conclusion"], flush=True)
    _write(out)


def _write(out):
    here = os.path.dirname(os.path.abspath(__file__))
    d = os.path.join(here, "run", "l043-probe")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "schema1_recovery.json"), "w", encoding="utf-8") as fh:
        json.dump(out, fh, ensure_ascii=False, indent=1)


if __name__ == "__main__":
    main()
