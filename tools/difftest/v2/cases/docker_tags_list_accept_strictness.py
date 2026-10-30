"""wave5 arm 5 — tags/list Accept-negotiation strictness (wave1-rerun13b).

Companion observation to the non-live view: with a manifest media-type Accept
header A answers tags/list with 406 while B answers 200. This case maps the
strictness rule on both sides across Accept variants:

  none        : no Accept header at all
  any         : Accept: */*
  app-json    : Accept: application/json
  s2-only     : schema2 manifest media type only
  list-only   : manifest LIST (fat manifest) media type only
  oci-index   : OCI image index media type only
  s1-only     : schema1 prettyjws media type only
  negotiate   : the 3-element negotiation set (wave1 get_manifest default)
  s2+any      : schema2 + */* mixed

Per variant per side: status + Content-Type + JSON tags-shape. Verdict-driving:
baseline (none/any/app-json) 200 + JSON both sides; per-variant status
intercompare (expected DIVERGENT on manifest-media-type variants — the
candidate). A-side strictness rule (which Accepts 406) and B-side tolerance
(200 always?) derived into facts.
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

SCHEMA1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"
OCI_INDEX = "application/vnd.oci.image.index.v1+json"
LIST_MT = "application/vnd.docker.distribution.manifest.list.v2+json"

VARIANTS = (
    ("none", None),
    ("any", "*/*"),
    ("app-json", "application/json"),
    ("s2-only", dl.ACCEPT_MANIFEST),
    ("list-only", LIST_MT),
    ("oci-index", OCI_INDEX),
    ("s1-only", SCHEMA1),
    ("negotiate", dl.ACCEPT_NEGOTIATE),
    ("s2-plus-any", ", ".join([dl.ACCEPT_MANIFEST, "*/*"])),
)

CASE = {
    "id": "dcr-tags-list-accept-strictness",
    "title": "tags/list Accept-negotiation strictness: 9 Accept variants dual-send",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def tags_view(resp):
    view = {"status": resp["status"], "content_type": dl.hdr(resp, "Content-Type")}
    try:
        doc = json.loads(resp["body"].decode("utf-8"))
        view["json"] = isinstance(doc, dict) and "name" in doc and "tags" in doc
        view["tags_count"] = len(doc.get("tags") or []) if isinstance(doc, dict) else None
    except Exception:
        view["json"] = False
        view["tags_count"] = None
    view["body_head"] = resp["body"][:160].decode("utf-8", "replace")
    return view


def run(ctx):
    checks = dl.Checks()
    facts = {"variants": {}}
    path = "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE)

    for name, accept in VARIANTS:
        per = {}
        for side in ("a", "b"):
            headers = {"Accept": accept} if accept else None
            # ctx.http adds only Basic auth — passing no headers sends no Accept
            r = ctx.http(side, "GET", path, headers=headers)
            per[side] = tags_view(r)
        facts["variants"][name] = per
        for side in ("a", "b"):
            if name in ("none", "any", "app-json"):
                checks.add(side, "baseline-%s-200-json" % name,
                           per[side]["status"] == 200 and per[side]["json"],
                           "got %s json=%s" % (per[side]["status"], per[side]["json"]))
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))

    # side-local strictness rule derivation (facts)
    for side in ("a", "b"):
        rule = {name: per[side]["status"] for name, _ in VARIANTS
                for per in (facts["variants"][name],)}
        facts.setdefault("strictness", {})[side] = {
            "status_by_variant": rule,
            "not_acceptable_variants": [n for n, s in rule.items() if s == 406],
        }

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
