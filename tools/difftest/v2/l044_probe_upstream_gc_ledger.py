#!/usr/bin/env python3
"""Enumerate upstream difftest-docker/smoke tags (manual-GC ledger check).

Upstream DELETE is 405 (zot relay, L043 standing note) so batch-pushed probe
tags accumulate; this lists current tags for the carried GC ledger.
"""
import base64
import json
import os
import sys
import urllib.request

UP = os.environ.get("DIFFTEST_UPSTREAM_BASE", "http://192.168.120.36:32100")
USER = os.environ.get("A_USER", "")
PASSWORD = os.environ.get("A_PASSWORD", "")


def get(url):
    r = urllib.request.Request(url)
    if USER:
        r.add_header("Authorization", "Basic " + base64.b64encode(f"{USER}:{PASSWORD}".encode()).decode())
    r.add_header("Accept", "application/json")
    with urllib.request.urlopen(r, timeout=15) as resp:
        return resp.status, resp.read()


s, b = get(UP + "/v2/difftest-docker/smoke/tags/list")
data = json.loads(b)
tags = data.get("tags") or []
wave5 = [t for t in tags if t.startswith("w5")]
print(json.dumps({"status": s, "total_tags": len(tags),
                  "wave5_tags": sorted(wave5)}, indent=1))
