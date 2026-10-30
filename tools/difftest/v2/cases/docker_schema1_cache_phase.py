"""wave3 arm 2 — L043 candidate ② deep dive: schema1 pull cache-phase dependency.

L043 established that A performs on-the-fly schema2->schema1 conversion for a
schema1-only Accept on the (hot) fixture tag while B serves the cached
schema2 verbatim. The open boundary: does the cached phase change the answer
(cache-wins: hot reads ignore Accept; conversion only at cold fill) or is the
conversion serve-time and phase-independent? Four directed directions, each
dual-send on run-unique tags freshly pushed to the upstream relay (genuinely
cold on both sides every run):

  F1 (warm-cycle tag, pushed fresh):
    w1 cold+negotiate : first pull, Accept = OCI/schema2 negotiation set
                        (warms the cache with the schema2 representation)
    w2 hot +s1-only   : same tag, Accept = schema1 prettyjws only
    w3 hot +s1-mixed  : same tag, Accept = schema1 + schema2
    w4 hot +negotiate : stability leg — cached representation unchanged?
  F2 (cold-schema1 tag, pushed fresh):
    c1 cold+s1-only   : first pull of F2 — pass-through/conversion shape
    c2 follow-up +s2  : what did the cold-s1 leg cache, if anything?

Per phase the intercomparison covers status, Content-Type family and (on 200)
the manifest mediaType + config digest. Side-local semantic facts (cache-wins
vs conversion at serve time) are derived into `facts` for the wave report;
the verdict itself stays a pure A/B differential. Upstream residue: two
run-unique manifest tags per run (upstream DELETE is 405 — GC ledger).
"""
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
TAG_WARM = "w3w" + dl.NONCE
TAG_COLD = "w3c" + dl.NONCE

CASE = {
    "id": "dcr-schema1-cache-phase",
    "title": "schema1 Accept x cache phase (hot/cold) four-direction probes",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def upstream_request(method, path, body=None, content_type=None, timeout=15.0):
    req = urllib.request.Request(UPSTREAM + path, data=body, method=method)
    if content_type:
        req.add_header("Content-Type", content_type)
    else:
        req.add_header("Accept", dl.ACCEPT_MANIFEST)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def manifest_view(resp):
    view = {"status": resp["status"], "content_type": dl.hdr(resp, "Content-Type")}
    if resp["status"] == 200:
        try:
            doc = dl.jbody(resp)
            view["media_type"] = doc.get("mediaType")
            view["config_digest"] = (doc.get("config") or {}).get("digest")
            view["schema_version"] = doc.get("schemaVersion")
        except Exception as e:
            view["parse_error"] = repr(e)
    return view


def pull(ctx, side, tag, accept):
    return ctx.http(side, "GET",
                    "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, tag),
                    headers={"Accept": accept})


def run(ctx):
    if not UPSTREAM:
        return {"status": "BLOCKED",
                "reason": "missing env DIFFTEST_UPSTREAM_BASE (fixture upstream)"}
    checks = dl.Checks()
    facts = {"tag_warm": TAG_WARM, "tag_cold": TAG_COLD,
             "upstream": dl.hostport({"base": UPSTREAM})}

    # ---- upstream fixture: copy the schema2 manifest onto two fresh tags
    status, src = upstream_request(
        "GET", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], dl.POSITIVE_TAG))
    checks.add("ab", "upstream-fixture-fetch-200", status == 200, "got %s" % status)
    if status != 200:
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out
    for tag in (TAG_WARM, TAG_COLD):
        put_status, put_body = upstream_request(
            "PUT", "/v2/%s/smoke/manifests/%s" % (
                dl.IMAGE.rsplit("/", 1)[0], tag),
            body=src, content_type=dl.ACCEPT_MANIFEST)
        checks.add("ab", "upstream-push-%s-2xx" % tag, put_status in (201, 202),
                   "got %s body=%s" % (put_status, put_body[:120].decode("utf-8", "replace")))
    if checks.result()["status"] != "PASS":
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # ---- four directed directions + follow-up, dual-send per phase
    phases = (
        ("w1-cold-negotiate", TAG_WARM, dl.ACCEPT_NEGOTIATE),
        ("w2-hot-s1-only", TAG_WARM, SCHEMA1),
        ("w3-hot-s1-mixed", TAG_WARM, ", ".join([SCHEMA1, dl.ACCEPT_MANIFEST])),
        ("w4-hot-negotiate", TAG_WARM, dl.ACCEPT_NEGOTIATE),
        ("c1-cold-s1-only", TAG_COLD, SCHEMA1),
        ("c2-cold-followup-s2", TAG_COLD, dl.ACCEPT_MANIFEST),
    )
    views = {}
    for name, tag, accept in phases:
        per = {}
        for side in ("a", "b"):
            per[side] = manifest_view(pull(ctx, side, tag, accept))
        views[name] = per
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
        checks.add("ab", "%s-ct-family-intercompare" % name,
                   (per["a"]["content_type"] or "").split(";")[0] ==
                   (per["b"]["content_type"] or "").split(";")[0],
                   "a=%s b=%s" % (per["a"]["content_type"], per["b"]["content_type"]))
        if per["a"]["status"] == 200 and per["b"]["status"] == 200:
            checks.add("ab", "%s-media-type-intercompare" % name,
                       per["a"].get("media_type") == per["b"].get("media_type"),
                       "a=%s b=%s" % (per["a"].get("media_type"), per["b"].get("media_type")))
            checks.add("ab", "%s-config-digest-intercompare" % name,
                       per["a"].get("config_digest") == per["b"].get("config_digest"),
                       "a=%s b=%s" % (per["a"].get("config_digest"),
                                      per["b"].get("config_digest")))

    # ---- side-local semantic derivations (facts, not verdicts)
    def is_schema1(v):
        ct = v.get("content_type") or ""
        mt = v.get("media_type") or ""
        return "prettyjws" in ct or "v1+prettyjws" in mt

    for side in ("a", "b"):
        warm, hot_s1, cold_s1, cold_s2 = (
            views["w1-cold-negotiate"][side], views["w2-hot-s1-only"][side],
            views["c1-cold-s1-only"][side], views["c2-cold-followup-s2"][side])
        same_warm = all(warm.get(k) == hot_s1.get(k)
                        for k in ("status", "content_type", "media_type"))
        facts.setdefault("semantics", {})[side] = {
            "hot_s1_equals_cached": same_warm,
            "hot_s1_schema1_shape": bool(is_schema1(hot_s1)),
            "cold_s1_schema1_shape": bool(is_schema1(cold_s1)),
            "cold_s1_status": cold_s1["status"],
            "cold_followup_status": cold_s2["status"],
            "cold_followup_media_type": cold_s2.get("media_type"),
            "reading": ("cache-wins: hot read returned the cached representation "
                        "ignoring Accept" if same_warm else
                        "serve-time-conversion-or-revalidate: hot schema1 read "
                        "differed from the cached representation"),
        }

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
