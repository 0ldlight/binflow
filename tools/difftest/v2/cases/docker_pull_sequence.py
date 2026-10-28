"""Replays contract entry docker/remote-pull-sequence (#23) — REST-face
subset (real docker daemon legs are environment-blocked this wave; the
protocol chain itself is fully exercised).

Sequence per side: anonymous ping 401 challenge -> token 200 -> manifest by
tag 200 (Accept negotiation) -> config blob 200 sha256 == manifest digest ->
layer blob 200 sha256 == manifest digest. PASS requires the full chain on
both sides plus byte-identical manifests across sides (digest parity with
the upstream fixture).
"""
import importlib.util
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_dockerlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_dockerlib.py"))
dl = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(dl)

CASE = {
    "id": "dcr-pull-sequence",
    "title": "REST pull sequence ping->token->manifest->config->layer",
    "layer": "L6",
    "domain": "docker-remote",
    "auth": True,
}


def run(ctx):
    checks = dl.Checks()
    manifests = {}
    for side in ("a", "b"):
        ping = dl.anon_http(ctx, side, "GET", "/v2/")
        checks.add(side, "step1 ping-401-challenge", ping["status"] == 401,
                   "got %s" % ping["status"])
        checks.add(side, "step1 bearer-challenge",
                   (dl.hdr(ping, "WWW-Authenticate") or "").startswith("Bearer realm="),
                   dl.hdr(ping, "WWW-Authenticate"))

        tr = dl.issue_token(ctx, side)
        checks.add(side, "step2 token-200", tr["status"] == 200, "got %s" % tr["status"])
        if tr["status"] != 200:
            continue
        tok = dl.jbody(tr).get("token")
        checks.add(side, "step2 token-nonempty", bool(tok), "len=%d" % len(tok or ""))

        mf = dl.get_manifest(ctx, side, dl.POSITIVE_TAG, token=tok)
        checks.add(side, "step3 manifest-200", mf["status"] == 200, "got %s" % mf["status"])
        if mf["status"] != 200:
            continue
        manifests[side] = mf["body"]
        dcd = dl.hdr(mf, "Docker-Content-Digest")
        checks.add(side, "step3 dcd-equals-sha256",
                   dcd == "sha256:" + dl.sha256_hex(mf["body"]), dcd)
        doc = dl.jbody(mf)
        cfg = doc["config"]["digest"].split(":", 1)[1]
        layer = doc["layers"][0]["digest"].split(":", 1)[1]

        rb = dl.get_blob(ctx, side, cfg, token=tok)
        checks.add(side, "step4 config-blob-200", rb["status"] == 200, "got %s" % rb["status"])
        checks.add(side, "step4 config-sha256", dl.sha256_hex(rb["body"]) == cfg,
                   "sha=%s" % dl.sha256_hex(rb["body"])[:16])

        rl = dl.get_blob(ctx, side, layer, token=tok)
        checks.add(side, "step5 layer-blob-200", rl["status"] == 200, "got %s" % rl["status"])
        checks.add(side, "step5 layer-sha256", dl.sha256_hex(rl["body"]) == layer,
                   "sha=%s" % dl.sha256_hex(rl["body"])[:16])

    checks.add("ab", "manifest-digest-parity",
               manifests.get("a") is not None and manifests.get("a") == manifests.get("b"),
               "a-sha=%s b-sha=%s" % (
                   dl.sha256_hex(manifests["a"])[:16] if manifests.get("a") else None,
                   dl.sha256_hex(manifests["b"])[:16] if manifests.get("b") else None))
    ev = ctx.write_evidence("checks.json", {"checks": checks.items})
    out = checks.result()
    out["evidence"] = [ev]
    return out
