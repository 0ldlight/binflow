#!/usr/bin/env python3
"""Standalone reproduction of the aggregate case's a-leg seed (2026-10-29).
Mirrors npm_virtual_aggregate_delete_entry._a_leg: fresh npmlib instance by
path, patched ctx, repo_put src, run_npm publish. Prints the raw run_npm
dict; no secrets printed."""
import importlib.util
import os
import subprocess

HERE = os.path.dirname(os.path.abspath(__file__))
CASES = os.path.join(HERE, "cases")


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    m = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    return m


def cred(env_path, pattern):
    out = subprocess.run(
        ["bash", "-c",
         "grep -E '^%s=' %s | head -1 | cut -d= -f2- | "
         "sed -e 's/^\"//' -e 's/\"$//'" % (pattern, env_path)],
        capture_output=True, text=True).stdout.strip()
    return out


mavenlib = load("ml_repro", os.path.join(CASES, "_mavenlib.py"))
npmlib = load("nl_repro", os.path.join(CASES, "_npmlib.py"))


class Ctx:
    pass


ctx = Ctx()
ctx.sides = {
    "a": {"name": "a", "base": "http://192.168.120.36:32082",
          "user": cred("/Users/lzw/vscodeProjects/binflow/.env.ref",
                       "ARTIFACTORY_REF_USER"),
          "password": cred("/Users/lzw/vscodeProjects/binflow/.env.ref",
                           "ARTIFACTORY_REF_PASSWORD")},
    "b": {"name": "b", "base": "http://192.168.120.36:32083",
          "user": cred("/Users/lzw/vscodeProjects/binflow/.env.lzw",
                       "BINFLOW_LZW_USER"),
          "password": cred("/Users/lzw/vscodeProjects/binflow/.env.lzw",
                           "BINFLOW_LZW_ADMIN_PASSWORD")},
}
ctx.timeout_s = 60


def _raw(side, method, path, body=None, headers=None):
    import base64
    import urllib.error
    import urllib.request
    s = ctx.sides[side]
    data = body.encode() if isinstance(body, str) else body
    req = urllib.request.Request(s["base"] + path, data=data,
                                 method=method.upper())
    ch = {k.lower() for k in (headers or {})}
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    if s["user"] and s["password"] and "authorization" not in ch:
        tok = base64.b64encode(
            ("%s:%s" % (s["user"], s["password"])).encode()).decode()
        req.add_header("Authorization", "Basic " + tok)
    try:
        with urllib.request.urlopen(req, timeout=ctx.timeout_s) as r:
            return {"status": r.status, "headers": dict(r.headers),
                    "body": r.read()}
    except urllib.error.HTTPError as e:
        try:
            raw = e.read()
        except Exception:
            raw = b""
        return {"status": e.code, "headers": dict(e.headers or {}),
                "body": raw}


ctx.http = _raw
mavenlib.patch_ctx(ctx)

TAG = "r4t550g"
SRC = "difftest-%s-src" % TAG
fx = npmlib.build_fixtures()
scratch = npmlib.scratch_dir()
try:
    mavenlib.repo_put(ctx, "a", SRC,
                      {"rclass": "local", "packageType": "npm"})
    reg = npmlib.registry_url(ctx, "a", SRC)
    print("registry url:", reg)
    print("ml cache:", mavenlib._CTX_PREFIX_CACHE)
    print("nl-mavenlib cache:",
          npmlib.mavenlib._CTX_PREFIX_CACHE if hasattr(npmlib, "mavenlib")
          else "no attr")
    print("registry url:", reg)
    os.makedirs(os.path.join(scratch, "npmc"), exist_ok=True)
    paths = npmlib.materialize(fx, scratch)
    res = npmlib.run_npm(ctx, "a", ["publish", paths[("rmt", 0)]], reg,
                         os.path.join(scratch, "npmc"))
    print("publish exit:", res["exit"])
    print("publish out:", res["out"][-600:])
finally:
    mavenlib.cleanup_repos(ctx, "a", [SRC])
    npmlib.rm_scratch(scratch)
