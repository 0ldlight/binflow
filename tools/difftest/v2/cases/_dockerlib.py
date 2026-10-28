"""_dockerlib.py — shared helpers for the docker-remote contract replay wave
(wave 1 on the lzw k8s instances, 2026-10-28).

Environment adaptation notes (this wave):
  - The docker registry face lives at the SIDE HOST ROOT on both products
    (A :32082/v2/*, B :32083/v2/* — A's router and BinFlow's root v2 mount),
    so this batch is launched with A_BASE/B_BASE pointing at the bare host
    roots, without the /artifactory or /binflow context prefix. API-face
    batches keep the prefixed bases.
  - Fixture provisioned by ../docker_wave_setup.sh BEFORE the batch: remote
    repo diff-docker-remote on both sides (upstream = in-cluster relay
    registry, private HTTP -> B side needs allowPrivateUpstream=true) and
    schema2 image difftest-docker/smoke tags t1/t2 on that upstream.
  - docker.io is unreachable from the instance pods; the upstream
    substitution is recorded in the wave report.

Verdict style (README "Maven 首批用例" precedent): per-side assertions
derived from the contract expect blocks in
docs/compatibility/contracts/docker-remote.yaml, plus A/B intercomparison
where the contract pins equality. PASS iff every check passes on both sides.

Credential discipline: token values only ever flow into request headers;
evidence payloads carry status/selected-headers/bodies, never Authorization
values.
"""

import hashlib
import json
import time

REPO = "diff-docker-remote"
IMAGE = "difftest-docker/smoke"
POSITIVE_TAG = "t2"
# Contract constant: synthetic empty-layer digest (GET must return the
# 32-byte body whose sha256 equals the requested digest).
EMPTY_LAYER_DIGEST = "a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"

ACCEPT_MANIFEST = "application/vnd.docker.distribution.manifest.v2+json"
ACCEPT_NEGOTIATE = ", ".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])

# 404-family fixtures use run-scoped names: a previous run's negative-cache
# row (missedRetrievalCachePeriodSecs default 1800s) would otherwise answer
# before the upstream is touched.
NONCE = format(int(time.time() * 1000), "x")


def hostport(side):
    return side["base"].split("//", 1)[1]


def hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def jbody(resp):
    return json.loads(resp["body"].decode("utf-8"))


def sha256_hex(data):
    return hashlib.sha256(data).hexdigest()


def issue_token(ctx, side_name, repo=REPO, image=IMAGE, actions="pull"):
    """GET /v2/token with Basic (ctx.http default). Token value must never be
    logged; it flows into request headers only."""
    side = ctx.sides[side_name]
    query = "service=%s&scope=repository:%s/%s:%s&account=%s" % (
        hostport(side), repo, image, actions, side["user"])
    return ctx.http(side_name, "GET", "/v2/token?" + query)


def anon_http(ctx, side_name, method, path, headers=None, body=None):
    """Anonymous leg: ctx.http always fills Basic when credentials exist, so
    contract arms that must travel without credentials go through here.
    Redirects are NOT followed (urllib's default handler would silently land
    on the 200 destination and hide the 3xx face the console contract is
    about): a 3xx comes back as a normal response via the HTTPError branch.
    Transport failures raise OSError and surface as case BLOCKED via the
    runner's generic handler."""
    import urllib.error
    import urllib.request

    class _NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, hdrs, newurl):
            return None

    side = ctx.sides[side_name]
    req = urllib.request.Request(side["base"] + path, data=body, method=method.upper())
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    opener = urllib.request.build_opener(_NoRedirect)
    try:
        with opener.open(req, timeout=ctx.timeout_s) as resp:
            return {"status": resp.status, "headers": dict(resp.headers),
                    "body": resp.read()}
    except urllib.error.HTTPError as e:
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return {"status": e.code, "headers": dict(e.headers or {}), "body": raw}


def get_manifest(ctx, side_name, ref, image=IMAGE, accept=ACCEPT_NEGOTIATE, token=None):
    headers = {"Accept": accept}
    if token:
        headers["Authorization"] = "Bearer " + token
    return ctx.http(side_name, "GET",
                    "/v2/%s/%s/manifests/%s" % (REPO, image, ref), headers=headers)


def head_manifest(ctx, side_name, ref, image=IMAGE, accept=ACCEPT_MANIFEST, token=None):
    headers = {"Accept": accept}
    if token:
        headers["Authorization"] = "Bearer " + token
    return ctx.http(side_name, "HEAD",
                    "/v2/%s/%s/manifests/%s" % (REPO, image, ref), headers=headers)


def get_blob(ctx, side_name, digest, image=IMAGE, token=None, extra_headers=None):
    spec = digest if ":" in digest else "sha256:" + digest
    headers = dict(extra_headers or {})
    if token:
        headers["Authorization"] = "Bearer " + token
    return ctx.http(side_name, "GET",
                    "/v2/%s/%s/blobs/%s" % (REPO, image, spec), headers=headers)


class Checks:
    """Named assertion ledger; result() reports FAIL with the first failure."""

    def __init__(self):
        self.items = []

    def add(self, side, name, ok, detail=""):
        self.items.append({"side": side, "name": name, "ok": bool(ok),
                           "detail": str(detail)[:400]})
        return ok

    def result(self):
        bad = [c for c in self.items if not c["ok"]]
        if bad:
            first = bad[0]
            return {"status": "FAIL",
                    "reason": "%s/%s: %s" % (first["side"], first["name"], first["detail"]),
                    "checks_total": len(self.items),
                    "checks_failed": len(bad)}
        return {"status": "PASS", "reason": None,
                "checks_total": len(self.items), "checks_failed": 0}


def resp_summary(resp):
    """Evidence-safe projection: no request Authorization, no raw body dumps
    over 2 KiB."""
    return {
        "status": resp["status"],
        "content_type": hdr(resp, "content-type"),
        "body_len": len(resp["body"]),
        "body_head": resp["body"][:512].decode("utf-8", "replace"),
    }
