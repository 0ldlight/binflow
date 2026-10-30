"""wave3e_lib.py — shared plumbing for the L044 wave-3 expansion batch
(L043 five-candidate live re-verification on the current deployment,
bearer cross-face A-side matrix completion, tags-TTL workload-dependency
experiment; 2026-10-30; report: reports/compatibility/L044-wave3-expansion-report.md).

Topology (unchanged from wave-2/3):
  - A = JFrog Artifactory 7.161.26 @ http://192.168.120.36:32082
        docker v2 face at HOST ROOT /v2/*, REST at /artifactory/api/*
  - B = BinFlow dev-fdeabbf2 (live-verified; brief said dev-aceee772) @
        http://192.168.120.36:32083, docker v2 face at HOST ROOT /v2/*,
        REST at /binflow/api/*
  - upstream test relay (zot) @ http://192.168.120.36:32100, fixture image
    difftest-docker/smoke (schema2), remote repo diff-docker-remote on both sides.

Re-verification context: the five L043 conclusions were established on
B=dev-cfa5da05 (deep-dive, 2026-10-29); this batch measures the same arms on
the CURRENT B deployment and compares.

Credential discipline: credentials are read from the gitignored env files with
line-anchored parsing, flow into Authorization headers only, and are NEVER
written to evidence or stdout. Evidence bodies are truncated heads; token
values are recorded as length + sha256 prefix at most.
"""

import base64
import hashlib
import json
import os
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
E_REF = "/Users/lzw/vscodeProjects/binflow/.env.ref"
E_LZW = "/Users/lzw/vscodeProjects/binflow/.env.lzw"

A_DOCKER = "http://192.168.120.36:32082"
B_DOCKER = "http://192.168.120.36:32083"
UPSTREAM = "http://192.168.120.36:32100"

A_API = A_DOCKER + "/artifactory/api"
B_API = B_DOCKER + "/binflow/api"

REPO = "diff-docker-remote"
IMAGE = "difftest-docker/smoke"

ACCEPT_S2 = "application/vnd.docker.distribution.manifest.v2+json"
ACCEPT_S2_LIST = "application/vnd.docker.distribution.manifest.list.v2+json"
ACCEPT_S1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"
ACCEPT_OCI_INDEX = "application/vnd.oci.image.index.v1+json"

NONCE = format(int(time.time() * 1000), "x")

EVIDENCE_DIR = os.path.join(HERE, "run", "wave3-expansion")


def _env_value(path, key):
    """Line-anchored read of KEY=VALUE (optionally quoted) from an env file."""
    try:
        with open(path, "r", encoding="utf-8") as fh:
            for line in fh:
                if line.startswith(key + "="):
                    val = line.split("=", 1)[1].strip()
                    return val.strip('"').strip("'")
    except OSError:
        pass
    return None


def load_sides():
    """Return {side: {docker, api, user, password}}; None credentials -> anon-only."""
    a_user = _env_value(E_REF, "ARTIFACTORY_REF_USER")
    a_pass = _env_value(E_REF, "ARTIFACTORY_REF_PASSWORD")
    b_user = _env_value(E_LZW, "BINFLOW_LZW_USER")
    b_pass = _env_value(E_LZW, "BINFLOW_LZW_ADMIN_PASSWORD")
    return {
        "a": {"docker": A_DOCKER, "api": A_API, "user": a_user, "password": a_pass},
        "b": {"docker": B_DOCKER, "api": B_API, "user": b_user, "password": b_pass},
    }


def http(side, method, url, headers=None, body=None, timeout=30.0):
    """One request with side Basic credentials. Never logs credentials.
    Redirects are not followed (3xx surfaces as a normal response)."""
    req = urllib.request.Request(url, data=body, method=method.upper())
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    if side.get("user") and side.get("password"):
        cred = base64.b64encode(
            ("%s:%s" % (side["user"], side["password"])).encode()).decode()
        req.add_header("Authorization", "Basic " + cred)
    opener = urllib.request.build_opener(_NoRedirect)
    try:
        with opener.open(req, timeout=timeout) as resp:
            return {"status": resp.status, "headers": dict(resp.headers),
                    "body": resp.read()}
    except urllib.error.HTTPError as e:
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return {"status": e.code, "headers": dict(e.headers or {}), "body": raw}
    except OSError as e:
        return {"status": -1, "headers": {}, "body": str(e).encode()}


def anon_http(method, url, headers=None, body=None, timeout=30.0):
    """One anonymous request, redirects not followed."""
    req = urllib.request.Request(url, data=body, method=method.upper())
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    opener = urllib.request.build_opener(_NoRedirect)
    try:
        with opener.open(req, timeout=timeout) as resp:
            return {"status": resp.status, "headers": dict(resp.headers),
                    "body": resp.read()}
    except urllib.error.HTTPError as e:
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return {"status": e.code, "headers": dict(e.headers or {}), "body": raw}
    except OSError as e:
        return {"status": -1, "headers": {}, "body": str(e).encode()}


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, hdrs, newurl):
        return None


def hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def digest_resp(resp, body_head=400):
    """Evidence-safe projection: status + selected headers + body head only."""
    return {
        "status": resp["status"],
        "content_type": hdr(resp, "content-type"),
        "www_authenticate": hdr(resp, "www-authenticate"),
        "docker_digest": hdr(resp, "docker-content-digest"),
        "body_len": len(resp["body"]),
        "body_head": resp["body"][:body_head].decode("utf-8", "replace"),
    }


def manifest_shape(resp):
    """(media_type, config_media_type) from a manifest body, for the schema1
    conversion face; None when body is not JSON."""
    try:
        doc = json.loads(resp["body"].decode("utf-8"))
    except Exception:
        return {"json": False}
    out = {"json": True}
    if isinstance(doc, dict):
        out["media_type"] = doc.get("mediaType") or doc.get("schemaVersion")
        cfg = doc.get("config") or {}
        out["config_media_type"] = cfg.get("mediaType") if isinstance(cfg, dict) else None
        out["has_layers"] = isinstance(doc.get("layers"), list)
    return out


def token_fingerprint(token):
    """Recordable trace of a token without its value: length + sha256 prefix."""
    return "len=%d sha256=%s" % (len(token), hashlib.sha256(token.encode()).hexdigest()[:16])


def upstream_request(method, path, body=None, content_type=None, timeout=15.0):
    """Anonymous request against the test relay (zot). Manifest fetches send
    Accept: schema2 unless overridden — the relay negotiates strictly."""
    req = urllib.request.Request(UPSTREAM + path, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    if "/manifests/" in path:
        req.add_header("Accept", ACCEPT_S2)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def push_upstream_tag(src_manifest_bytes, tag):
    """Copy a schema2 manifest to a NEW upstream tag (blobs already exist).
    Returns (status, body). Upstream DELETE is 405 -> residue ledger entry."""
    return upstream_request(
        "PUT", "/v2/%s/manifests/%s" % (IMAGE, tag),
        body=src_manifest_bytes, content_type=ACCEPT_S2)


def fetch_upstream_fixture(src_tag):
    return upstream_request("GET", "/v2/%s/manifests/%s" % (IMAGE, src_tag))


def write_evidence(name, obj):
    os.makedirs(EVIDENCE_DIR, exist_ok=True)
    root, ext = os.path.splitext(name)
    suffix = os.environ.get("W3E_EVIDENCE_SUFFIX", "")
    path = os.path.abspath(os.path.join(EVIDENCE_DIR, root + suffix + ext))
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(obj, fh, ensure_ascii=False, indent=2)
    return path


def epoch():
    return time.strftime("%Y-%m-%dT%H:%M:%S%z") + " (%d)" % int(time.time())


def challenge_family(resp):
    """'basic' | 'bearer' | None from WWW-Authenticate."""
    wa = hdr(resp, "www-authenticate") or ""
    low = wa.lower()
    if low.startswith("basic"):
        return "basic"
    if low.startswith("bearer"):
        return "bearer"
    return None
