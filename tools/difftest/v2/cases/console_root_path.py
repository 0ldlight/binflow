"""Wave-1 case #0 (root-path -> console homepage), domain console.

Reference behavior (Artifactory 7.161.26 live, 2026-10-28):
  - GET /            -> 200 text/html (UI homepage served at the bare root)
  - GET /artifactory/ -> 302 with ABSOLUTE Location .../ui/
  - GET /ui/          -> 200
BinFlow live: bare / -> 404 (no-root-mirror JSON); /binflow/ -> 301 with
RELATIVE Location /binflow/ui/; /binflow/ui/ -> 200.
Deltas recorded per check; the bare-root homepage absence is the headline
divergence (ticket T-630).
Paths here are context-explicit (this batch runs with bare-root bases, so
/artifactory and /binflow spell out each side's context root).
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "console-root-path",
    "title": "root path -> console homepage redirect semantics",
    "layer": "L2",
    "domain": "console-root",
    "auth": False,
}

CONTEXT = {"a": "/artifactory", "b": "/binflow"}


def run(ctx):
    checks = dl.Checks()
    facts = {}
    for side in ("a", "b"):
        ctxp = CONTEXT[side]

        root = dl.anon_http(ctx, side, "GET", "/")
        ct = dl.hdr(root, "Content-Type") or ""
        facts[side] = {"root_status": root["status"], "root_content_type": ct}
        checks.add(side, "bare-root-200-homepage", root["status"] == 200,
                   "got %s body=%s" % (root["status"],
                                       root["body"][:120].decode("utf-8", "replace")))
        checks.add(side, "bare-root-html-content-type", ct.startswith("text/html"), ct)

        cr = dl.anon_http(ctx, side, "GET", ctxp + "/")
        loc = dl.hdr(cr, "Location") or ""
        facts[side]["ctx_status"] = cr["status"]
        facts[side]["ctx_location"] = loc
        checks.add(side, "context-root-redirects-to-ui", 300 <= cr["status"] < 400,
                   "got %s" % cr["status"])
        checks.add(side, "redirect-status-302", cr["status"] == 302,
                   "got %s" % cr["status"])
        checks.add(side, "location-absolute", loc.startswith("http"), loc)
        checks.add(side, "location-targets-ui", loc.endswith("/ui/"), loc)

        ui_path = "/ui/" if side == "a" else "/binflow/ui/"
        ui = dl.anon_http(ctx, side, "GET", ui_path)
        checks.add(side, "ui-page-200", ui["status"] == 200, "got %s" % ui["status"])
        facts[side]["ui_status"] = ui["status"]

    checks.add("ab", "root-behavior-intercompare",
               facts.get("a", {}).get("root_status") == facts.get("b", {}).get("root_status"),
               "a=%s b=%s" % (facts.get("a", {}).get("root_status"),
                              facts.get("b", {}).get("root_status")))
    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
