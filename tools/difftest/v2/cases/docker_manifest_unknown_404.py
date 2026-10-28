"""Replays contract entry docker/remote-manifest-unknown-404 (#13, VERIFIED).

GET manifest on a run-scoped never-queried tag -> 404;
Docker-Distribution-Api-Version registry/2.0; Content-Type present;
Artifactory-Manifest-Handler-Error absent; compact-json errors[0] =
{code: MANIFEST_UNKNOWN, message: "The named manifest is not known to the
registry.", detail: {manifest: <image-normalized>}} — upstream here is the
in-cluster relay registry (no docker.io library/ normalization applies).
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-manifest-unknown-404",
    "title": "manifest GET nonexistent tag -> MANIFEST_UNKNOWN shape",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    miss_tag = "miss-%s" % dl.NONCE
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side)
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        r = dl.get_manifest(ctx, side, miss_tag, token=tok)
        checks.add(side, "status-404", r["status"] == 404, "got %s" % r["status"])
        checks.add(side, "api-version", dl.hdr(r, "Docker-Distribution-Api-Version") == "registry/2.0",
                   dl.hdr(r, "Docker-Distribution-Api-Version"))
        checks.add(side, "content-type-present", dl.hdr(r, "Content-Type") is not None,
                   dl.hdr(r, "Content-Type"))
        checks.add(side, "no-handler-error-header",
                   dl.hdr(r, "Artifactory-Manifest-Handler-Error") is None,
                   dl.hdr(r, "Artifactory-Manifest-Handler-Error"))
        try:
            doc = dl.jbody(r)
            e0 = (doc.get("errors") or [{}])[0]
            detail = e0.get("detail") or {}
            checks.add(side, "keys-exact-errors", list(doc.keys()) == ["errors"], list(doc.keys()))
            checks.add(side, "code-manifest-unknown", e0.get("code") == "MANIFEST_UNKNOWN",
                       e0.get("code"))
            checks.add(side, "message-literal",
                       e0.get("message") == "The named manifest is not known to the registry.",
                       e0.get("message"))
            checks.add(side, "detail-manifest-image-name",
                       detail.get("manifest") == dl.IMAGE, detail)
            checks.add(side, "detail-excludes-tag",
                       miss_tag not in str(detail), detail)
        except Exception as e:
            checks.add(side, "body-json", False, repr(e))
    ev = ctx.write_evidence("checks.json", {"miss_tag": miss_tag, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
