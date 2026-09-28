"""Shared helpers for difftest v2 npm cases (L031 / T-545).

Not a case: the runner's discovery skips files starting with "_". Generic
pieces (repo CRUD, judge, digests, scratch dirs) are reused from the sibling
_mavenlib.py via an explicit path load — they are batch infrastructure, not
Maven-specific.

Spec anchors for the assertions (docs/reverse/):
  - virtual-resolution.md §1 (locals always precede remotes in the expanded
    member sequence regardless of declaration order), §2 (four-bucket order),
    §6 (npm packument merge: member sequence = four-bucket order with cache
    repos filtered whenever any remote body is present; merged result written
    to <virtualKey>-cache, TTL virtualRetrievalCachePeriodSecs default 600s).
  - maven-npm-pypi.md §2.6 (field-level merge: first packument-producing
    member is the base, later members' versions putIfAbsent, dist-tags/time
    unioned, common fields first-wins, latest recomputed after excludes),
    §2.2 (.npm/{name}/package.json storage layout), §2 table (SLIM packument
    variant on Accept: application/vnd.npm.install-v1+json).

Harness topology (controlled fake npm source, difftest- namespace only):
  - The fake upstream is a local npm repository hosted ON the A instance
    (key difftest-<tag>-src), seeded with deterministic prebuilt tarballs via
    the real npm client. Both sides' remotes consume the exact same physical
    upstream bytes.
  - A cannot reach the difftest host (probe 2026-09-28: VPN utun4 has no
    inbound route), so the source cannot live on the runner machine.
  - A's remote must fetch the source through A's DIRECT port (8081): the
    router port (8082) self-reference fails to fetch (404, probe 2026-09-28).
  - BinFlow's npm remote requests the packument at <name>/packument.json
    (storage-layout identity mapping, known wire drift, T-538 drift A) while
    Artifactory serves /<name> only: a layout-translating proxy bridges the
    B leg (translate packument.json -> /<name>, everything else passthrough).
    The proxy injects A-side basic auth from env-injected values at runtime;
    no credential ever lands in a file, argv or evidence payload.

Determinism: fixture tarballs are built as tar (mtime=0) + gzip(mtime=0);
plain tarfile mode "w:gz" embeds a wall-clock gzip header and is NOT
reproducible (verified 2026-09-28), so digests double as r2==r3 anchors.
"""
from __future__ import annotations

import base64
import gzip
import hashlib
import importlib.util
import io
import json
import os
import subprocess
import tarfile
import threading
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

sha1_hex = mavenlib.sha1_hex
sha256_hex = mavenlib.sha256_hex
judge = mavenlib.judge
SetupError = mavenlib.SetupError
repo_put = mavenlib.repo_put
repo_delete = mavenlib.repo_delete
cleanup_repos = mavenlib.cleanup_repos
scratch_dir = mavenlib.scratch_dir
rm_scratch = mavenlib.rm_scratch

# Harness adaptation, not a product divergence: Artifactory's router port
# refuses to proxy a remote URL that references itself; the direct Artifactory
# port serves the same instance and works (probe 2026-09-28).
A_ROUTER_PORT = "8082"
A_DIRECT_PORT = "8081"


def a_direct_base(a_base: str) -> str:
    out = a_base.replace(":" + A_ROUTER_PORT, ":" + A_DIRECT_PORT, 1)
    return out if out != a_base else a_base + "___direct-port-not-derived"


# ------------------------------------------------------------------ fixtures

def _fixture_tgz(name: str, version: str, description: str,
                 index_content: str) -> bytes:
    """Deterministic npm tarball: tar entries mtime=0, gzip header mtime=0."""
    manifest = {"name": name, "version": version, "description": description}
    raw = io.BytesIO()
    with tarfile.open(fileobj=raw, mode="w") as tf:
        def add(path: str, data: bytes):
            ti = tarfile.TarInfo(path)
            ti.size = len(data)
            ti.mtime = 0
            ti.mode = 0o644
            tf.addfile(ti, io.BytesIO(data))
        add("package/package.json",
            json.dumps(manifest, indent=2).encode())
        add("package/index.js", index_content.encode())
    return gzip.compress(raw.getvalue(), compresslevel=9, mtime=0)


def _info(name: str, version: str, description: str, index_content: str,
          tarball: bytes) -> dict:
    return {
        "name": name, "version": version, "description": description,
        "index_content": index_content,
        "index_sha256": sha256_hex(index_content.encode()),
        "tgz": tarball,
        "tgz_sha256": sha256_hex(tarball),
        "sha1": sha1_hex(tarball),
        "filename": "%s-%s.tgz" % (name, version),
    }


def build_fixtures() -> dict:
    """The L031 fixture set (pure, in-memory; byte-stable across builds).

    - rmt: upstream-only package, 3 versions; upstream dist-tags deliberately
      stale (latest=1.0.0 while 2.0.0 exists) to stress the latest-recompute
      rule (maven-npm-pypi.md §2.6), plus a stable tag that must survive the
      dist-tags union untouched.
    - shadow: same name+version upstream and local, DIFFERENT bytes and
      description — pins locals-precede-remotes + putIfAbsent (virtual-
      resolution.md §1).
    - lonly: local-member-only package.
    """
    fx = {"rmt": {"name": "difftest-npm-rmt", "versions": []}}
    for ver, idx in (("1.0.0", "rmt upstream one"),
                     ("1.0.1", "rmt upstream one-patch"),
                     ("2.0.0", "rmt upstream two")):
        desc = "rmt-multi upstream " + ver
        fx["rmt"]["versions"].append(_info(
            "difftest-npm-rmt", ver, desc, idx,
            _fixture_tgz("difftest-npm-rmt", ver, desc, idx)))
    fx["shadow_up"] = _info("difftest-npm-shadow", "1.0.0",
                            "shadow upstream description",
                            "module.exports = 'shadow UPSTREAM bytes';",
                            _fixture_tgz("difftest-npm-shadow", "1.0.0",
                                         "shadow upstream description",
                                         "module.exports = 'shadow UPSTREAM bytes';"))
    fx["shadow_loc"] = _info("difftest-npm-shadow", "1.0.0",
                             "shadow LOCAL override description",
                             "module.exports = 'shadow LOCAL bytes';",
                             _fixture_tgz("difftest-npm-shadow", "1.0.0",
                                          "shadow LOCAL override description",
                                          "module.exports = 'shadow LOCAL bytes';"))
    fx["lonly"] = _info("difftest-npm-lonly", "1.0.0",
                        "local-only package",
                        "module.exports = 'local only';",
                        _fixture_tgz("difftest-npm-lonly", "1.0.0",
                                     "local-only package",
                                     "module.exports = 'local only';"))
    return fx


def materialize(fx: dict, directory: str):
    """Write the fixture tarballs to disk for npm publish/pack legs."""
    paths = {}
    for key, entry in fx.items():
        if key == "rmt":
            for i, vinfo in enumerate(entry["versions"]):
                p = os.path.join(directory, vinfo["filename"])
                with open(p, "wb") as fh:
                    fh.write(vinfo["tgz"])
                paths[("rmt", i)] = p
        else:
            p = os.path.join(directory, entry["filename"])
            with open(p, "wb") as fh:
                fh.write(entry["tgz"])
            paths[key] = p
    return paths


# ----------------------------------------------------------------- npm client

def _auth_env_value(ctx, side: str) -> str:
    s = ctx.sides[side]
    return base64.b64encode(("%s:%s" % (s["user"], s["password"])).encode()).decode()


def _registry_auth_key(registry: str) -> str:
    """npmrc per-registry auth key: //host[:port]/path/:_auth (trailing /)."""
    rest = registry.split("://", 1)[-1]
    if not rest.endswith("/"):
        rest += "/"
    return "//" + rest + ":_auth"


def registry_url(ctx, side: str, repo: str) -> str:
    return "%s/api/npm/%s/" % (ctx.sides[side]["base"], repo)


def run_npm(ctx, side: str, args, registry: str, cache_dir: str,
            cwd=None, timeout_s=None) -> dict:
    """Real npm client leg. The userconfig carries no secret: _auth comes
    from ${NPM_AUTH} expanded by npm from the subprocess environment only."""
    cfg = os.path.join(cache_dir, "npmrc-%s" % side)
    with open(cfg, "w", encoding="utf-8") as fh:
        fh.write("registry=%s\nalways-auth=true\n%s=${NPM_AUTH}\n"
                 % (registry, _registry_auth_key(registry)))
    env = dict(os.environ)
    env["NPM_AUTH"] = _auth_env_value(ctx, side)
    cmd = ["npm", "--userconfig", cfg, "--cache", cache_dir,
           "--prefer-online", "--fetch-retries", "1", "--no-audit",
           "--no-fund", "--loglevel", "error"] + args
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True,
                              timeout=timeout_s or ctx.timeout_s,
                              env=env, cwd=cwd)
        out = (proc.stdout + "\n" + proc.stderr).strip()
        return {"exit": str(proc.returncode), "out": out[-1500:]}
    except FileNotFoundError:
        raise SetupError("npm binary not found on PATH") from None
    except subprocess.TimeoutExpired:
        return {"exit": "timeout", "out": ""}


# ------------------------------------------------------- layout proxy (B leg)

def start_layout_proxy(a_src_registry: str, a_user: str, a_password: str):
    """Threaded translating proxy for the B leg: BinFlow's npm remote fetches
    <name>/packument.json, Artifactory serves <name>; everything else passes
    through. Returns (server, port); caller must shutdown()+server_close()."""

    upstream = a_src_registry.rstrip("/")
    token = base64.b64encode(("%s:%s" % (a_user, a_password)).encode()).decode()

    class Handler(BaseHTTPRequestHandler):
        def _relay(self, with_body: bool):
            path = self.path
            if path.endswith("/packument.json"):
                path = path[: -len("/packument.json")]
            req = urllib.request.Request(upstream + path, method="GET")
            req.add_header("Authorization", "Basic " + token)
            try:
                with urllib.request.urlopen(req, timeout=40) as r:
                    body = r.read()
                    self.send_response(r.status)
                    for k, v in r.headers.items():
                        if k.lower() not in ("connection", "transfer-encoding",
                                             "content-length"):
                            self.send_header(k, v)
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    if with_body:
                        self.wfile.write(body)
            except urllib.error.HTTPError as e:
                body = e.read()
                self.send_response(e.code)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                if with_body:
                    self.wfile.write(body)
            except Exception:
                self.send_response(502)
                self.send_header("Content-Length", "0")
                self.end_headers()

        def do_GET(self):
            self._relay(True)

        def do_HEAD(self):
            self._relay(False)

        def log_message(self, fmt, *a):  # keep evidence/output clean
            pass

    srv = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True,
                     name="difftest-layout-proxy").start()
    return srv, port


# ------------------------------------------------------------ repo payloads

def npm_local(key: str) -> tuple:
    return key, {"rclass": "local", "packageType": "npm"}


def npm_virtual(key: str, members) -> tuple:
    return key, {"rclass": "virtual", "packageType": "npm",
                 "repositories": list(members)}


def layout_keys(tag: str):
    return {
        "src": "difftest-%s-src" % tag,
        "loc": "difftest-%s-loc" % tag,
        "rem": "difftest-%s-rem" % tag,
        "virt": "difftest-%s-virt" % tag,
    }


def cleanup_layout(ctx, side: str, tag: str, extra=(), include_src=False):
    keys = layout_keys(tag)
    order = [keys["virt"], keys["rem"], keys["loc"]]
    if side == "a" and include_src:
        order.append(keys["src"])
    order += list(extra)
    # the derived cache repos go too (best effort; absent keys are fine)
    order += [keys["virt"] + "-cache", keys["rem"] + "-cache"]
    cleanup_repos(ctx, side, order)


def ensure_source_on_a(ctx, scratch: str, fx: dict, tag: str):
    """(Re)create the shared fake upstream on A. The source's lifecycle spans
    BOTH legs of a case run: it is seeded once before leg a and deleted only
    after leg b finishes (round-1 lesson: per-leg cleanup starved the b leg)."""
    key = layout_keys(tag)["src"]
    cleanup_repos(ctx, "a", [key])
    seed_source_on_a(ctx, scratch, fx, tag)
    return key


def drop_source_on_a(ctx, tag: str):
    cleanup_repos(ctx, "a", [layout_keys(tag)["src"]])


def seed_source_on_a(ctx, scratch: str, fx: dict, tag: str):
    """Create the fake upstream on A and publish via the real npm client.
    Publish order 1.0.0 -> 1.0.1 -> 2.0.0 (npm sets latest=2.0.0), then a
    deliberate dist-tag rewind latest->1.0.0 + stable->2.0.0."""
    key = layout_keys(tag)["src"]
    repo_put(ctx, "a", key, {"rclass": "local", "packageType": "npm"})
    cache = os.path.join(scratch, "npm-cache-a-src")
    os.makedirs(cache, exist_ok=True)
    reg = registry_url(ctx, "a", key)
    paths = materialize(fx, scratch)
    for i in range(len(fx["rmt"]["versions"])):
        res = run_npm(ctx, "a", ["publish", paths[("rmt", i)]], reg, cache)
        if res["exit"] != "0":
            raise SetupError("seed src publish rmt[%d] -> %s: %s"
                             % (i, res["exit"], res["out"][-200:]))
    res = run_npm(ctx, "a", ["publish", paths["shadow_up"]], reg, cache)
    if res["exit"] != "0":
        raise SetupError("seed src publish shadow_up -> %s: %s"
                         % (res["exit"], res["out"][-200:]))
    for tagname, ver in (("latest", "1.0.0"), ("stable", "2.0.0")):
        res = run_npm(ctx, "a", ["dist-tag", "add",
                                 "difftest-npm-rmt@" + ver, tagname], reg, cache)
        if res["exit"] != "0":
            raise SetupError("seed src dist-tag %s -> %s: %s"
                             % (tagname, res["exit"], res["out"][-200:]))


def seed_local_member(ctx, side: str, scratch: str, fx: dict, tag: str):
    key = layout_keys(tag)["loc"]
    repo_put(ctx, side, key, {"rclass": "local", "packageType": "npm"})
    cache = os.path.join(scratch, "npm-cache-%s-loc" % side)
    os.makedirs(cache, exist_ok=True)
    reg = registry_url(ctx, side, key)
    paths = materialize(fx, scratch)
    for fkey in ("shadow_loc", "lonly"):
        res = run_npm(ctx, side, ["publish", paths[fkey]], reg, cache)
        if res["exit"] != "0":
            raise SetupError("seed loc publish %s -> %s: %s"
                             % (fkey, res["exit"], res["out"][-200:]))


def build_layout(ctx, side: str, scratch: str, fx: dict, tag: str, proxy):
    """Provision loc + rem + virt for one side (the shared fake upstream on A
    is managed by ensure_source_on_a/drop_source_on_a at run scope). `proxy`
    is the running layout proxy tuple (server, port) for side b; None for
    side a. Virtual member declaration order: remote FIRST (locals still
    resolve first per spec §1 — the declaration-order stress from the T-538
    case material)."""
    keys = layout_keys(tag)
    if side == "a":
        src_url = a_direct_base(ctx.sides["a"]["base"]) + "/api/npm/" + keys["src"]
        remote_payload = {"rclass": "remote", "packageType": "npm",
                          "url": src_url,
                          "username": ctx.sides["a"]["user"],
                          "password": ctx.sides["a"]["password"]}
    else:
        _, port = proxy
        remote_payload = {"rclass": "remote", "packageType": "npm",
                          "url": "http://127.0.0.1:%d" % port,
                          "allowPrivateUpstream": True}
    seed_local_member(ctx, side, scratch, fx, tag)
    repo_put(ctx, side, keys["rem"], remote_payload)
    repo_put(ctx, side, keys["virt"],
             npm_virtual(keys["virt"], [keys["rem"], keys["loc"]])[1])


# ------------------------------------------------------------- packument io

def get_packument(ctx, side, repo, name, accept=None):
    headers = {"Accept": accept} if accept else {}
    return ctx.http(side, "GET", "/api/npm/%s/%s" % (repo, name),
                    headers=headers)


def semantic_packument(body: bytes) -> dict:
    """Semantic view of a packument for judging/evidence: versions (sorted),
    per-version dist.shasum, dist-tags, description. Raw bodies differ across
    instances by design (tarball URL hosts, _rev, time) and are compared only
    through this semantic projection (assertion-based judging, not capture)."""
    try:
        d = json.loads(body.decode("utf-8", "replace"))
    except Exception:  # noqa: BLE001 - value extraction must not raise
        return {"error": "not-json"}
    out = {
        "versions": sorted(d.get("versions", {})),
        "dist_tags": dict(d.get("dist-tags", {})),
        "description": d.get("description"),
    }
    for ver, entry in d.get("versions", {}).items():
        out["shasum_%s" % ver] = (entry.get("dist", {}) or {}).get("shasum")
        out["tarball_%s" % ver] = (entry.get("dist", {}) or {}).get("tarball")
    return out


def tarball_verdict(ctx, side, repo, name, filename, want_sha256: str) -> str:
    resp = ctx.http(side, "GET", "/api/npm/%s/%s/-/%s" % (repo, name, filename))
    if resp["status"] != 200:
        return "status=%d" % resp["status"]
    got = sha256_hex(resp["body"])
    return "200+sha256-ok" if got == want_sha256 else "200+sha_mismatch"


def npm_out_json(res: dict):
    """Parse the JSON payload of a successful npm --json command."""
    if res["exit"] != "0":
        return None
    try:
        return json.loads(res["out"])
    except Exception:  # noqa: BLE001
        return None
