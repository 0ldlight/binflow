"""wave5 arm 3 — L043 candidate ① completion: full cross-face challenge matrix.

wave3 (dcr-bearer-crossface-challenge) covered 2 faces x 4 shapes and located
the B-internal split at the manifest/content face. This case completes the
matrix across the SIX docker-v2 faces and intercompares against A:

  faces : ping      GET /v2/
          manifest  GET /v2/<repo>/<image>/manifests/t2   (Accept: schema2)
          blob      GET /v2/<repo>/<image>/blobs/<empty-layer digest>
          tags      GET /v2/<repo>/<image>/tags/list      (no Accept)
          catalog   GET /v2/_catalog
          token     GET /v2/token?service=...&scope=...&account=...
  shapes: anon           — no Authorization header
          basic-bad      — Basic of a one-shot NONEXISTENT username
                           (per-CELL fresh name: exactly one 401 each, below
                           any measured threshold — admin is never touched)
          bearer-garbage — alg:none JWT with forged signature (wave2 t3 arm)
          bearer-empty   — "Bearer " with an empty credential

Per cell: status + WWW-Authenticate challenge family + errors[0] JSON shape +
recurrent-wording flag (must stay False — a trip would contaminate the cell).
Verdict = per-cell A/B intercompare of status and challenge family.
Per-side cross-face consistency across all six faces is derived into facts —
the candidate-① completion evidence (which faces cluster together per side).
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
ONESHOT = "w5b" + dl.NONCE
EMPTY_LAYER = dl.EMPTY_LAYER_DIGEST

CASE = {
    "id": "dcr-bearer-face-full-matrix",
    "title": "401 challenge family across 6 docker-v2 faces x 4 auth shapes",
    "layer": "L4",
    "domain": "docker-remote",
    "auth": True,
    "timeout_s": 60.0,
}


def face_paths(ctx, side):
    hostport = dl.hostport(ctx.sides[side])
    return [
        ("ping", "/v2/", None),
        ("manifest", "/v2/%s/%s/manifests/%s" % (dl.REPO, dl.IMAGE, dl.POSITIVE_TAG),
         {"Accept": dl.ACCEPT_MANIFEST}),
        ("blob", "/v2/%s/%s/blobs/sha256:%s" % (dl.REPO, dl.IMAGE, EMPTY_LAYER), None),
        ("tags", "/v2/%s/%s/tags/list" % (dl.REPO, dl.IMAGE), None),
        ("catalog", "/v2/_catalog", None),
        ("token", "/v2/token?service=%s&scope=repository:%s/%s:pull&account=%s"
                  % (hostport, dl.REPO, dl.IMAGE, ONESHOT), None),
    ]


def cell_request(ctx, side, path, extra_headers, auth_header):
    headers = dict(extra_headers or {})
    if auth_header is not None:
        headers["Authorization"] = auth_header
    if auth_header is None:
        return dl.anon_http(ctx, side, "GET", path, headers=headers)
    return ctx.http(side, "GET", path, headers=headers)


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
    shapes = [
        ("anon", None),
        ("basic-bad", "basic"),
        ("bearer-garbage", "Bearer " + ALG_NONE_TOKEN),
        ("bearer-empty", "Bearer "),
    ]

    matrix = {}
    for side in ("a", "b"):
        for face_name, face_path, extra in face_paths(ctx, side):
            for shape_name, template in shapes:
                auth = None
                if template == "basic":
                    auth = "Basic " + base64.b64encode(
                        ("%s-%s-%s:difftest-wrongpass-w5"
                         % (ONESHOT, face_name[:3], side)).encode()).decode()
                elif template is not None:
                    auth = template
                cell = "%s/%s" % (face_name, shape_name)
                r = cell_request(ctx, side, face_path, extra, auth)
                text = r["body"].decode("utf-8", "replace")
                per = {
                    "status": r["status"],
                    "challenge": challenge_family(r),
                    "www_authenticate": dl.hdr(r, "WWW-Authenticate"),
                    "shape": errors0_shape(r),
                    "recurrent_wording": "recurrent" in text.lower(),
                    "body_head": text[:160],
                }
                matrix.setdefault(cell, {})[side] = per
                facts.setdefault("cells", {})[cell] = matrix[cell]
                if side == "b":
                    pa, pb = matrix[cell]["a"], matrix[cell]["b"]
                    slug = cell.replace("/", "-")
                    checks.add("ab", "%s-status-intercompare" % slug,
                               pa["status"] == pb["status"],
                               "a=%s b=%s" % (pa["status"], pb["status"]))
                    checks.add("ab", "%s-challenge-intercompare" % slug,
                               pa["challenge"] == pb["challenge"],
                               "a=%s b=%s (a wa=%r b wa=%r)" % (
                                   pa["challenge"], pb["challenge"],
                                   pa["www_authenticate"], pb["www_authenticate"]))
                    for s in ("a", "b"):
                        checks.add(s, "%s-no-recurrent-trip" % slug,
                                   not matrix[cell][s]["recurrent_wording"],
                                   "breaker wording present in cell")

    # per-side cross-face consistency across all six faces (per shape)
    face_order = ["ping", "manifest", "blob", "tags", "catalog", "token"]
    for side in ("a", "b"):
        clusters = {}
        for shape_name, _ in shapes:
            sig = []
            for face in face_order:
                p = matrix["%s/%s" % (face, shape_name)][side]
                sig.append((p["status"], p["challenge"], p["shape"].get("keys")))
            # cluster faces by identical (status, challenge, keys) signature
            face_clusters = {}
            for face, s in zip(face_order, sig):
                face_clusters.setdefault(repr(s), []).append(face)
            clusters[shape_name] = {
                "distinct_profiles": len(face_clusters),
                "profiles": {k[:80]: v for k, v in face_clusters.items()},
            }
        facts.setdefault("crossface_clusters", {})[side] = clusters

    ev = ctx.write_evidence("checks.json", {"facts": facts, "checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
