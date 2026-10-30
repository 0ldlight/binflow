"""wave5 arm 2 — L043 candidate ② re-qualification + four-direction boundary.

wave3 (dcr-schema1-cache-phase) fixed the semantics: A converts at serve time
(phase-independent, cache stays canonical schema2) while B negotiates once at
cold fill and then serves the cached representation regardless of Accept —
including the C-b negative consequence that a cold schema1 pull poisons B's
cache with the prettyjws representation. This case re-qualifies that matrix on
the live B build and extends the boundary:

  F1 warm-cycle tag (pushed fresh):
    w1 cold+negotiate : first pull, 3-element negotiation set
    w2 hot +s1-only   : cached phase vs schema1 Accept (candidate ② core)
    w3 hot +s1-mixed  : mixed Accept
    w4 hot +negotiate : stability leg
  F2 cold-schema1 tag (pushed fresh):
    c1 cold+s1-only   : pass-through/conversion shape
    c2 follow-up+s2   : pollution proof (C-b)
    c3 follow-up2+s2  : pollution PERSISTENCE (new — 3rd read)
    c4 follow-up+negotiate : pollution breadth under negotiation (new)
    rec90 (+90s, record-only) : short-window recovery sample
  F3 control tag (pushed fresh):
    n1 cold+s2-only   : single-Accept schema2 cold fill (NOT the 3-set)
    n2 hot +s1-only   : does the s2-only cold fill avoid the hot-s1 split?

Upstream residue: three run-unique tags per run (upstream DELETE is 405 — GC
ledger). rec90 is a recorded fact, never a verdict (TTL unknown).
"""
import importlib.util
import os
import time
import urllib.error
import urllib.request

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

UPSTREAM = os.environ.get("DIFFTEST_UPSTREAM_BASE", "").rstrip("/")
SCHEMA1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"
TAG_WARM = "w5w" + dl.NONCE
TAG_COLD = "w5c1" + dl.NONCE
TAG_CTRL = "w5n" + dl.NONCE

CASE = {
    "id": "dcr-schema1-phase-boundary",
    "title": "schema1 Accept x cache phase: re-qualify + persistence/control boundary",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
    "timeout_s": 60.0,
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
    facts = {"tag_warm": TAG_WARM, "tag_cold": TAG_COLD, "tag_ctrl": TAG_CTRL,
             "upstream": dl.hostport({"base": UPSTREAM})}

    # ---- upstream fixture: copy the schema2 manifest onto three fresh tags
    status, src = upstream_request(
        "GET", "/v2/%s/smoke/manifests/%s" % (
            dl.IMAGE.rsplit("/", 1)[0], dl.POSITIVE_TAG))
    checks.add("ab", "upstream-fixture-fetch-200", status == 200, "got %s" % status)
    if status != 200:
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out
    for tag in (TAG_WARM, TAG_COLD, TAG_CTRL):
        put_status, put_body = upstream_request(
            "PUT", "/v2/%s/smoke/manifests/%s" % (
                dl.IMAGE.rsplit("/", 1)[0], tag),
            body=src, content_type=dl.ACCEPT_MANIFEST)
        checks.add("ab", "upstream-push-%s-2xx" % tag[:8], put_status in (201, 202),
                   "got %s body=%s" % (put_status, put_body[:120].decode("utf-8", "replace")))
    if checks.result()["status"] != "PASS":
        ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
        out = checks.result()
        out["evidence"] = [ev]
        return out

    # ---- phase table: (name, tag, Accept)
    phases = (
        ("w1-cold-negotiate", TAG_WARM, dl.ACCEPT_NEGOTIATE),
        ("w2-hot-s1-only", TAG_WARM, SCHEMA1),
        ("w3-hot-s1-mixed", TAG_WARM, ", ".join([SCHEMA1, dl.ACCEPT_MANIFEST])),
        ("w4-hot-negotiate", TAG_WARM, dl.ACCEPT_NEGOTIATE),
        ("c1-cold-s1-only", TAG_COLD, SCHEMA1),
        ("c2-followup-s2", TAG_COLD, dl.ACCEPT_MANIFEST),
        ("c3-followup2-s2", TAG_COLD, dl.ACCEPT_MANIFEST),
        ("c4-followup-negotiate", TAG_COLD, dl.ACCEPT_NEGOTIATE),
        ("n1-cold-s2-only", TAG_CTRL, dl.ACCEPT_MANIFEST),
        ("n2-hot-s1-only", TAG_CTRL, SCHEMA1),
    )
    views = {}
    for name, tag, accept in phases:
        per = {side: manifest_view(pull(ctx, side, tag, accept)) for side in ("a", "b")}
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

    # ---- recovery sample at +90s (record-only; TTL unknown -> never a verdict)
    time.sleep(90)
    rec = {side: manifest_view(pull(ctx, side, TAG_COLD, dl.ACCEPT_MANIFEST))
           for side in ("a", "b")}
    facts["rec90-followup-s2"] = rec

    # ---- side-local semantic derivations (facts, not verdicts)
    def is_schema1(v):
        ct = v.get("content_type") or ""
        mt = v.get("media_type") or ""
        return "prettyjws" in ct or "v1+prettyjws" in mt

    for side in ("a", "b"):
        warm, hot_s1 = views["w1-cold-negotiate"][side], views["w2-hot-s1-only"][side]
        same_warm = all(warm.get(k) == hot_s1.get(k)
                        for k in ("status", "content_type", "media_type"))
        facts.setdefault("semantics", {})[side] = {
            "hot_s1_equals_cached_warm": same_warm,
            "c2_schema1_shape": bool(is_schema1(views["c2-followup-s2"][side])),
            "c3_schema1_shape": bool(is_schema1(views["c3-followup2-s2"][side])),
            "c4_schema1_shape": bool(is_schema1(views["c4-followup-negotiate"][side])),
            "n2_schema1_shape": bool(is_schema1(views["n2-hot-s1-only"][side])),
            "rec90_schema1_shape": bool(is_schema1(rec[side])),
            "rec90_status": rec[side]["status"],
            "reading": ("cache-wins: hot read returns the cached representation "
                        "ignoring Accept" if same_warm else
                        "serve-time-conversion-or-revalidate"),
        }

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
