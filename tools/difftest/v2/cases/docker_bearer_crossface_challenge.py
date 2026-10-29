"""wave3 arm 3 — L043 candidate ① completion: challenge cross-face matrix.

L043 noted a B-internal inconsistency: on the /v2/ ping face B answers with
the Props-token wording while the manifest face answers with the generic
"authentication required", and B's 401s carry a Bearer challenge where A
carries Basic. This case completes that picture as a full matrix (2 faces x
4 auth shapes, each cell dual-send):

  faces : GET /v2/  (registry base ping face)
          GET /v2/<repo>/<image>/manifests/t2  (Accept: schema2)
  shapes: anon          — no Authorization header at all
          basic-bad     — Basic of a one-time NONEXISTENT username
                          (per-cell fresh name: single 401, no breaker trip;
                          the ledger is username-keyed and cross-route shared
                          — wave3 task-1 — so admin is never touched)
          bearer-garbage— alg:none JWT with forged signature (wave2 t3 arm)
          bearer-empty  — "Bearer " with an empty credential

Per cell: status + WWW-Authenticate challenge family (Basic/Bearer/none) +
errors[0] JSON shape (keys/status/message). Verdict = per-cell A/B
intercomparison of status and challenge family; per-side cross-face
consistency (ping vs manifest per shape) is derived into facts — the split
is candidate ①'s completion evidence, classification stays upstream.

Ledger safety: no valid credential is ever sent with a wrong shape; bad-basic
cells use one-shot usernames (1 failure each, below the measured threshold);
bearer cells are single sends per face and any 403 recurrent-block answer is
recorded as a fact, never retried.
"""
import base64
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

ALG_NONE_TOKEN = ("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0."
                  "eyJzdWIiOiJkaWZmdGVzdCJ9."
                  "Zm9yZ2VkLXNpZ25hdHVyZQ")
ONESHOT = "w3x" + dl.NONCE


CASE = {
    "id": "dcr-bearer-crossface-challenge",
    "title": "401 challenge family across ping/manifest faces x 4 auth shapes",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
}


def cell_request(ctx, side, face_path, auth_header):
    headers = {"Accept": dl.ACCEPT_MANIFEST} if "manifests/" in face_path else None
    if auth_header is not None:
        headers = dict(headers or {})
        headers["Authorization"] = auth_header
    if auth_header is None:
        return dl.anon_http(ctx, side, "GET", face_path, headers=headers)
    return ctx.http(side, "GET", face_path, headers=headers)


def challenge_family(resp):
    wa = dl.hdr(resp, "WWW-Authenticate") or ""
    if wa.lower().startswith("basic"):
        return "basic"
    if wa.lower().startswith("bearer"):
        return "bearer"
    return "none" if not wa else "other"


def errors0_shape(resp):
    try:
        doc = dl.jbody(resp)
    except Exception:
        return {"json": False}
    e0 = (doc.get("errors") or [{}])[0] if isinstance(doc, dict) else {}
    return {"keys": sorted(e0.keys()), "status": e0.get("status"),
            "message": e0.get("message")}


def run(ctx):
    checks = dl.Checks()
    facts = {"oneshot_prefix": ONESHOT}
    faces = [
        ("ping", "/v2/"),
        ("manifest", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG)),
    ]
    shapes = [
        ("anon", None),
        ("basic-bad", "Basic " + base64.b64encode(
            ("%s:difftest-wrongpass-w3" % (ONESHOT + "-<face>")).encode()).decode()),
        ("bearer-garbage", "Bearer " + ALG_NONE_TOKEN),
        ("bearer-empty", "Bearer "),
    ]

    matrix = {}
    for face_name, face_path in faces:
        for shape_name, template in shapes:
            cell = "%s/%s" % (face_name, shape_name)
            per = {}
            for side in ("a", "b"):
                auth = None
                if template is not None:
                    auth = template.replace("-<face>", "-%s-%s" % (face_name[:3], side))
                r = cell_request(ctx, side, face_path, auth)
                text = r["body"].decode("utf-8", "replace")
                per[side] = {
                    "status": r["status"],
                    "challenge": challenge_family(r),
                    "www_authenticate": dl.hdr(r, "WWW-Authenticate"),
                    "shape": errors0_shape(r),
                    "recurrent_wording": "recurrent" in text.lower(),
                    "body_head": text[:160],
                }
            matrix[cell] = per
            facts.setdefault("cells", {})[cell] = per
            checks.add("ab", "%s-status-intercompare" % cell.replace("/", "-"),
                       per["a"]["status"] == per["b"]["status"],
                       "a=%s b=%s" % (per["a"]["status"], per["b"]["status"]))
            checks.add("ab", "%s-challenge-intercompare" % cell.replace("/", "-"),
                       per["a"]["challenge"] == per["b"]["challenge"],
                       "a=%s b=%s (a wa=%r b wa=%r)" % (
                           per["a"]["challenge"], per["b"]["challenge"],
                           per["a"]["www_authenticate"], per["b"]["www_authenticate"]))

    # per-side cross-face consistency (the candidate-① completion evidence)
    for side in ("a", "b"):
        diffs = []
        for shape_name, _ in shapes:
            pa, pm = matrix["ping/%s" % shape_name][side], matrix["manifest/%s" % shape_name][side]
            if pa["status"] != pm["status"] or pa["shape"] != pm["shape"] or \
                    pa["challenge"] != pm["challenge"]:
                diffs.append(shape_name)
        facts.setdefault("crossface_inconsistent_shapes", {})[side] = diffs

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
