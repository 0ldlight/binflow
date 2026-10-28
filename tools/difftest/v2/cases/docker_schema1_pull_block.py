"""wave2 arm 5 — schema1 pull Accept faces (L042 unknown: "schema1 拉取阻断").

The docker v2 pull family restricted to the legacy schema1 media type:
  - arm s1-only:  Accept: application/vnd.docker.distribution.manifest.v1+prettyjws
  - arm s1-mixed: Accept: schema1 + schema2 (negotiation: does schema1 in the
    list downgrade the answer?)
Both arms dual-send on manifest t2 and intercompare status + Content-Type +
(if 200) the manifest's mediaType/config digest. The upstream relay carries
docker schema2 (+OCI) manifests only, so a 200 schema1 answer would prove an
on-the-fly conversion on that side; a 404/other refusal shape is equally
recorded. PASS iff both arms intercompare equal on all asserted dimensions;
split shapes are new-divergence candidates for the wave report.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

SCHEMA1 = "application/vnd.docker.distribution.manifest.v1+prettyjws"

CASE = {
    "id": "dcr-schema1-pull-block",
    "title": "schema1 Accept on manifest GET: refusal/conversion shape",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def manifest_view(resp):
    view = {"status": resp["status"], "content_type": dl.hdr(resp, "Content-Type")}
    if resp["status"] == 200:
        try:
            doc = dl.jbody(resp)
            view["media_type"] = doc.get("mediaType")
            view["config_digest"] = (doc.get("config") or {}).get("digest")
        except Exception as e:
            view["parse_error"] = repr(e)
    return view


def run(ctx):
    checks = dl.Checks()
    facts = {}
    arms = (
        ("s1-only", SCHEMA1),
        ("s1-mixed", ", ".join([SCHEMA1, dl.ACCEPT_MANIFEST])),
    )
    for name, accept in arms:
        per = {}
        for side in ("a", "b"):
            r = ctx.http(side, "GET",
                         "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                         headers={"Accept": accept})
            per[side] = manifest_view(r)
        facts[name] = per
        checks.add("ab", "%s-status-intercompare" % name,
                   per["a"]["status"] == per["b"]["status"],
                   "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
        checks.add("ab", "%s-content-type-intercompare" % name,
                   (per["a"]["content_type"] or "").split(";")[0] ==
                   (per["b"]["content_type"] or "").split(";")[0],
                   "a=%s b=%s" % (per["a"]["content_type"], per["b"]["content_type"]))
        if per["a"]["status"] == 200 and per["b"]["status"] == 200:
            checks.add("ab", "%s-media-type-intercompare" % name,
                       per["a"]["media_type"] == per["b"]["media_type"],
                       "a=%s b=%s" % (per["a"]["media_type"], per["b"]["media_type"]))
            checks.add("ab", "%s-config-digest-intercompare" % name,
                       per["a"]["config_digest"] == per["b"]["config_digest"],
                       "a=%s b=%s" % (per["a"]["config_digest"], per["b"]["config_digest"]))

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
