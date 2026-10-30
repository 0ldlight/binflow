#!/usr/bin/env python3
"""L043 probe rerun preflight — decide verify-only vs full re-probe.

Checks: A/B REST ping, B version string, A docker bare-root catalog shape,
B docker bare-root catalog shape, upstream relay health. No credentials in
output; statuses and body-shape fingerprints only.
"""
import base64
import json
import os
import sys
import urllib.request
import urllib.error

A_BASE = os.environ["A_BASE"]
B_BASE = os.environ["B_BASE"]
UP_BASE = os.environ.get("DIFFTEST_UPSTREAM_BASE", "http://192.168.120.36:32100")
A_USER, A_PASSWORD = os.environ["A_USER"], os.environ["A_PASSWORD"]
B_USER, B_PASSWORD = os.environ["B_USER"], os.environ["B_PASSWORD"]


def basic(u, p):
    return "Basic " + base64.b64encode(f"{u}:{p}".encode()).decode()


def req(method, url, headers=None, timeout=15):
    r = urllib.request.Request(url, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            body = resp.read()
            return resp.status, dict(resp.headers), body
    except urllib.error.HTTPError as e:
        body = e.read()
        return e.code, dict(e.headers), body
    except Exception as e:  # noqa: BLE001
        return None, {}, str(e).encode()


out = {}

# 1. A REST ping
s, h, b = req("GET", A_BASE + "/artifactory/api/system/ping",
              {"Authorization": basic(A_USER, A_PASSWORD)})
out["a_ping"] = {"status": s, "body": b[:120].decode("utf-8", "replace").strip()}

# 2. B REST ping
s, h, b = req("GET", B_BASE + "/binflow/api/system/ping",
              {"Authorization": basic(B_USER, B_PASSWORD)})
out["b_ping"] = {"status": s, "body": b[:120].decode("utf-8", "replace").strip()}

# 3. B version
s, h, b = req("GET", B_BASE + "/binflow/api/system/version",
              {"Authorization": basic(B_USER, B_PASSWORD)})
try:
    v = json.loads(b)
    out["b_version"] = {"status": s, "version": v.get("version"), "revision": v.get("revision")}
except Exception:  # noqa: BLE001
    out["b_version"] = {"status": s, "body": b[:200].decode("utf-8", "replace")}

# 4. A bare-root catalog (docker face, authed)
s, h, b = req("GET", A_BASE + "/v2/_catalog",
              {"Authorization": basic(A_USER, A_PASSWORD)})
out["a_catalog"] = {"status": s, "ct": h.get("Content-Type", ""),
                    "body_len": len(b), "body_head": b[:80].decode("utf-8", "replace")}

# 5. B bare-root catalog (docker face, authed)
s, h, b = req("GET", B_BASE + "/v2/_catalog",
              {"Authorization": basic(B_USER, B_PASSWORD)})
out["b_catalog"] = {"status": s, "ct": h.get("Content-Type", ""), "body": b[:200].decode("utf-8", "replace")}

# 6. upstream relay
s, h, b = req("GET", UP_BASE + "/v2/", timeout=10)
out["upstream"] = {"status": s}

# 7. A tags/list accept-strictness spot check (probe 5 core): schema2-only Accept on remote repo
s, h, b = req("GET", A_BASE + "/v2/diff-docker-remote/difftest-docker/smoke/tags/list",
              {"Authorization": basic(A_USER, A_PASSWORD), "Accept": "application/vnd.docker.distribution.manifest.v2+json"})
out["a_tags_schema2accept"] = {"status": s, "body_head": b[:80].decode("utf-8", "replace")}
s, h, b = req("GET", B_BASE + "/v2/diff-docker-remote/difftest-docker/smoke/tags/list",
              {"Authorization": basic(B_USER, B_PASSWORD), "Accept": "application/vnd.docker.distribution.manifest.v2+json"})
out["b_tags_schema2accept"] = {"status": s, "body_head": b[:80].decode("utf-8", "replace")}

print(json.dumps(out, indent=2, ensure_ascii=False))
