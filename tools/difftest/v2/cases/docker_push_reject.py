"""Replays contract entries docker/remote-push-blobs-uploads-reject (#20),
docker/remote-push-manifest-reject (#21), docker/remote-delete-manifest-reject
(#22) — remote repositories reject all three write verbs with 400 and the
verbatim Artifactory wordings, pretty-json errors[0] = {status, message}
without code/detail.
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-push-reject",
    "title": "remote repo rejects POST uploads / PUT manifest / DELETE manifest",
    "layer": "L5",
    "domain": "docker-remote",
    "auth": True,
}

WANT = {
    "post-uploads": "Unable to upload blobs to a remote repository.",
    "put-manifest": "Unable to upload a manifest to a remote repository.",
    "delete-manifest": "Unable to delete a manifest from a remote repository.",
}


def check_reject(checks, side, arm, r):
    checks.add(side, arm + " status-400", r["status"] == 400, "got %s" % r["status"])
    checks.add(side, arm + " content-type-present", dl.hdr(r, "Content-Type") is not None,
               dl.hdr(r, "Content-Type"))
    try:
        doc = dl.jbody(r)
        e0 = (doc.get("errors") or [{}])[0]
        checks.add(side, arm + " keys-exact-errors", list(doc.keys()) == ["errors"],
                   list(doc.keys()))
        checks.add(side, arm + " message-verbatim", e0.get("message") == WANT[arm],
                   e0.get("message"))
        checks.add(side, arm + " e0-status-400", e0.get("status") == 400, e0.get("status"))
        extra = sorted(set(e0.keys()) - {"status", "message"})
        checks.add(side, arm + " e0-no-code-detail", extra == [], extra)
    except Exception as e:
        checks.add(side, arm + " body-json", False, repr(e))


def run(ctx):
    checks = dl.Checks()
    push_tag = "push-reject-%s" % dl.NONCE
    for side in ("a", "b"):
        tok = None
        tr = dl.issue_token(ctx, side, actions="pull,push")
        if tr["status"] == 200:
            tok = dl.jbody(tr).get("token")
        checks.add(side, "push-scope-token-issued", tok is not None,
                   "token status %s" % tr["status"])
        auth = {"Authorization": "Bearer " + tok} if tok else {}

        r1 = ctx.http(side, "POST", "/v2/%s/%s/blobs/uploads/" % (dl.REPO, dl.IMAGE),
                      headers=auth)
        check_reject(checks, side, "post-uploads", r1)

        r2 = ctx.http(side, "PUT",
                      "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, push_tag),
                      body='{"schemaVersion":2}',
                      headers=dict(auth, **{"Content-Type": dl.ACCEPT_MANIFEST}))
        check_reject(checks, side, "put-manifest", r2)

        r3 = ctx.http(side, "DELETE",
                      "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
                      headers=auth)
        check_reject(checks, side, "delete-manifest", r3)
    ev = ctx.write_evidence("checks.json", {"push_tag": push_tag, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
