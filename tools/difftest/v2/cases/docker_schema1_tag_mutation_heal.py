"""wave5 arm 6 — C-b boundary: does an upstream tag mutation heal the cache?

L044 open question: "C-b 缓存污染的恢复窗口（TTL 届满是否自愈）". TTL expiry
sampling lives in the standalone wave5_schema1_recovery_probe.py; this case
probes the OTHER possible heal trigger — the upstream manifest under the SAME
tag being replaced (tag mutation), which registries may treat as a cache
invalidation signal independent of TTL:

  U1 upstream push v1 onto run-unique tag TMUT (deterministic fixture bytes)
  P1 A cold+s1  -> prettyjws (serve-time conversion)
  P2 B cold+s1  -> pollutes B's cache with the s1 representation (C-b setup)
  U2 upstream MUTATE: push v2 (different config -> different manifest digest)
     onto the SAME tag
  M1 hot+s2        both sides — which manifest digest comes back?
  M2 hot+negotiate both sides — breadth of the answer post-mutation
  M3 hot+s1        both sides — representation after mutation

Per read: status + CT family + config digest (identifies v1 vs v2) + schema1
shape. Verdict = A/B intercompare per read; which side (if either) observes
the mutation is derived into facts. Upstream residue: one run-unique tag
(DELETE 405 — GC ledger).
"""
import hashlib
import importlib.util
import os
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

UPSTREAM = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")
SCHEMA1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"
TAG_MUT = "w5m2" + dl.NONCE

LAYER_BODY = b"A" * 64
LAYER_SHA = hashlib.sha256(LAYER_BODY).hexdigest()


def config_body(created):
    return ('{"architecture":"amd64","os":"linux","created":"%s",'
            '"config":{},"rootfs":{"type":"layers","diff_ids":["sha256:%s"]},'
            '"history":[{"created":"%s","created_by":"difftest wave"}]}'
            % (created, LAYER_SHA, created)).encode()


def manifest_body(cfg):
    cfg_sha = hashlib.sha256(cfg).hexdigest()
    return ('{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",'
            '"config":{"mediaType":"application/vnd.docker.container.image.v1+json",'
            '"size":%d,"digest":"sha256:%s"},"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip",'
            '"size":%d,"digest":"sha256:%s"}]}'
            % (len(cfg), cfg_sha, len(LAYER_BODY), LAYER_SHA)).encode()


V1_CONFIG = config_body("2026-10-28T00:00:00Z")
V2_CONFIG = config_body("2026-10-29T00:00:00Z")
V1 = manifest_body(V1_CONFIG)
V2 = manifest_body(V2_CONFIG)

CASE = {
    "id": "dcr-schema1-tag-mutation-heal",
    "title": "upstream same-tag mutation vs poisoned/negotiated cache states",
    "layer": "L9",
    "domain": "docker-remote",
    "auth": True,
}


def upstream_request(method, path, body=None, content_type=None, timeout=15.0,
                     extra_headers=None):
    req = urllib.request.Request(UPSTREAM + path, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    else:
        req.add_header("Accept", dl.ACCEPT_MANIFEST)
    for k, v in (extra_headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read(), dict(resp.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers or {})


def upstream_upload_blob(body, sha):
    """Full upload dance against the relay: POST -> Location -> PUT?digest=."""
    st, _, hdrs = upstream_request(
        "POST", "/v2/%s/smoke/blobs/uploads/" % dl.IMAGE.rsplit("/", 1)[0])
    if st not in (200, 201, 202):
        return st, "post"
    loc = None
    for k, v in hdrs.items():
        if k.lower() == "location":
            loc = v
            break
    if not loc:
        return st, "no-location"
    url = loc if loc.startswith("http") else UPSTREAM + (loc if loc.startswith("/") else "/" + loc)
    sep = "&" if "?" in url else "?"
    st, err, _ = upstream_request(
        "PUT", url[len(UPSTREAM):] + "%sdigest=sha256:%s" % (sep, sha),
        body=body, content_type="application/octet-stream")
    return st, err[:120].decode("utf-8", "replace")


def push_upstream_tag(body, tag):
    st, _, _ = upstream_request(
        "PUT", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], tag),
        body=body, content_type=dl.ACCEPT_MANIFEST)
    return st


def pull_view(ctx, side, tag, accept):
    r = ctx.http(side, "GET",
                 "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, tag),
                 headers={"Accept": accept})
    v = {"status": r["status"], "content_type": dl.hdr(r, "Content-Type")}
    if r["status"] == 200:
        try:
            doc = dl.jbody(r)
            v["config_digest"] = (doc.get("config") or {}).get("digest")
            v["media_type"] = doc.get("mediaType")
        except Exception as e:
            v["parse_error"] = repr(e)
    return v


def run(ctx):
    if not UPSTREAM:
        return {"status": "BLOCKED",
                "reason": "missing env DIFFTEST_UPSTREAM_BASE (fixture upstream)"}
    checks = dl.Checks()
    facts = {"tag_mut": TAG_MUT, "upstream": dl.hostport({"base": UPSTREAM}),
             "v1_config_digest": "sha256:" + hashlib.sha256(V1_CONFIG).hexdigest(),
             "v2_config_digest": "sha256:" + hashlib.sha256(V2_CONFIG).hexdigest()}

    # blob HEADs on the upstream first — v2's config blob must exist for the
    # manifest PUT to be accepted (layer is shared; config differs).
    for name, cfg in (("v1", V1_CONFIG), ("v2", V2_CONFIG)):
        sha = hashlib.sha256(cfg).hexdigest()
        hst, _, _ = upstream_request(
            "GET", "/v2/%s/smoke/blobs/sha256:%s" % (
                dl.IMAGE.rsplit("/", 1)[0], sha))
        if hst != 200:
            ust, detail = upstream_upload_blob(cfg, sha)
            checks.add("ab", "upstream-blob-%s-upload-2xx" % name,
                       ust in (200, 201), "got %s detail=%s" % (ust, detail))
            if ust not in (200, 201):
                ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
                out = checks.result()
                out["evidence"] = [ev]
                return out
        facts["upstream_blob_%s_head" % name] = hst

    # ---- U1: push v1, P1/P2 cold schema1 pulls
    st = push_upstream_tag(V1, TAG_MUT)
    checks.add("ab", "upstream-push-v1-2xx", st in (201, 202), "got %s" % st)
    if st not in (201, 202):
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out
    p = {side: pull_view(ctx, side, TAG_MUT, SCHEMA1) for side in ("a", "b")}
    facts["cold-s1"] = p
    for side in ("a", "b"):
        checks.add(side, "cold-s1-200", p[side]["status"] == 200, "got %s" % p[side]["status"])

    # ---- U2: mutate the same tag to v2
    st = push_upstream_tag(V2, TAG_MUT)
    checks.add("ab", "upstream-mutate-v2-2xx", st in (201, 202), "got %s" % st)

    # ---- M1/M2/M3 post-mutation reads
    for name, accept in (("mut-hot-s2", dl.ACCEPT_MANIFEST),
                         ("mut-hot-negotiate", dl.ACCEPT_NEGOTIATE),
                         ("mut-hot-s1", SCHEMA1)):
        per = {side: pull_view(ctx, side, TAG_MUT, accept) for side in ("a", "b")}
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
        if per["a"]["status"] == 200 and per["b"]["status"] == 200:
            checks.add("ab", "%s-config-digest-intercompare" % name,
                       per["a"].get("config_digest") == per["b"].get("config_digest"),
                       "a=%s b=%s" % (per["a"].get("config_digest"),
                                      per["b"].get("config_digest")))

    # side-local facts: did the mutation surface (v2 digest) or stale v1?
    v2_digest = facts["v2_config_digest"]
    for side in ("a", "b"):
        facts.setdefault("mutation_observed", {})[side] = {
            name: per[side].get("config_digest") == v2_digest
            for name, per in facts.items()
            if isinstance(per, dict) and side in per and "config_digest" in per[side]}

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
