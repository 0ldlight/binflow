"""L034 arms 6/8 (ledger maven/deploy-created-envelope-uri-missing-context-
prefix, T-561 fix verification + Location-header forensics): maven wire
deploy 201 created-envelope uri/downloadUri must carry the instance context
prefix (/artifactory on A, /binflow on B) — uri == <side base>/<repo>/<path>
with base INCLUDING the context root — and the envelope uri must be
resolvable (GET -> 200, deployed bytes for artifact arms).

Arms: pom / jar / module-level maven-metadata.xml (client-uploaded) /
checksum-deploy (zero-transfer, artifact pre-seeded by the pom arm — if a
side refuses the construction the dims record the refusal verbatim,
dual-oracle).

Location header (arm 8): recorded raw per arm on both sides; forensic only
(ledger has no record for it — no judgement, feeds a possible new entry).
"""
import hashlib
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-deploy-created-envelope",
    "title": "maven wire deploy 201 created-envelope uri/downloadUri context "
             "prefix + uri resolvability (pom/jar/metadata/checksum-deploy) "
             "+ Location header forensics",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 90,
}

REPO = "difftest-l034-env"
REPO2 = "difftest-l034-env2"
GROUP, ART, VER = "com.diff", "envp", "1.0.0"
GP = GROUP.replace(".", "/")
POM_REL = "%s/%s/%s/%s-%s.pom" % (GP, ART, VER, ART, VER)
JAR_REL = "%s/%s/%s/%s-%s.jar" % (GP, ART, VER, ART, VER)
META_REL = "%s/%s/maven-metadata.xml" % (GP, ART)

META_BODY = (
    '<metadata>\n'
    '  <groupId>%s</groupId>\n'
    '  <artifactId>%s</artifactId>\n'
    '  <versioning><versions><version>%s</version></versions>'
    '<latest>%s</latest><release>%s</release></versioning>\n'
    '</metadata>\n' % (GROUP, ART, VER, VER, VER)).encode()


def _hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def _envelope_leg(ctx, side, tag, put_rel, body, extra_headers=None):
    """PUT -> grade envelope dims; returns (asserts, raw)."""
    asserts, raw = {}, {}
    r = ctx.http(side, "PUT", "/%s/%s" % (REPO, put_rel), body=body,
                 headers=extra_headers or {"Content-Type": "application/xml"})
    raw["status"] = r["status"]
    raw["location"] = _hdr(r, "Location") or ""
    raw["body_full"] = r["body"].decode("utf-8", "replace")
    asserts["%s_status" % tag] = "%d" % r["status"]
    if r["status"] != 201:
        return asserts, raw
    try:
        doc = json.loads(raw["body_full"])
    except ValueError:
        doc = {}
    want = "%s/%s/%s" % (ctx.sides[side]["base"], REPO, put_rel)
    raw["want_uri"] = want
    asserts["%s_uri_form" % tag] = "base+prefix+path" if doc.get("uri") == want \
        else "other:%s" % doc.get("uri")
    asserts["%s_dluri_form" % tag] = "base+prefix+path" if doc.get("downloadUri") == want \
        else "other:%s" % doc.get("downloadUri")
    # resolvability: GET the envelope uri verbatim (ctx.http prepends the
    # side base, so hand it the uri's path beyond the base)
    uri = doc.get("uri") or ""
    base = ctx.sides[side]["base"]
    rel = uri[len(base):] if uri.startswith(base) else "/__unroutable__"
    g = ctx.http(side, "GET", rel)
    raw["uri_get_status"] = g["status"]
    if tag in ("pom", "jar"):
        asserts["%s_uri_resolvable" % tag] = (
            "200+sha-ok" if g["status"] == 200 and
            mavenlib.sha256_hex(g["body"]) == mavenlib.sha256_hex(body)
            else "status=%d" % g["status"])
    else:
        # metadata documents may be recalculated server-side: resolvability
        # only (200), bytes not pinned (T-561 unit-test convention)
        asserts["%s_uri_resolvable" % tag] = "%d" % g["status"]
    return asserts, raw


def _leg(ctx, side):
    out, rawall = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(REPO), mavenlib.maven_local(REPO2)])

    pom = mavenlib.pom_fixture(GROUP, ART, VER)
    jar = mavenlib.jar_fixture("envelope-l034")

    a, raw = _envelope_leg(ctx, side, "pom", POM_REL, pom)
    out.update(a)
    rawall["pom"] = raw
    a, raw = _envelope_leg(ctx, side, "jar", JAR_REL, jar)
    out.update(a)
    rawall["jar"] = raw
    a, raw = _envelope_leg(ctx, side, "meta", META_REL, META_BODY)
    out.update(a)
    rawall["meta"] = raw

    # checksum-deploy: same pom GAV into REPO2, zero transfer — the pom
    # above seeds the instance store. X-Checksum-Deploy + checksums, no body.
    r = ctx.http(side, "PUT", "/%s/%s" % (REPO2, POM_REL), body=None, headers={
        "X-Checksum-Deploy": "true",
        "X-Checksum-Sha1": mavenlib.sha1_hex(pom),
        "X-Checksum-Md5": hashlib.md5(pom).hexdigest()})  # noqa: S324
    rawall["cksum"] = {
        "status": r["status"],
        "location": _hdr(r, "Location") or "",
        "body_full": r["body"].decode("utf-8", "replace")}
    out["cksum_status"] = "%d" % r["status"]
    if r["status"] == 201:
        try:
            doc = json.loads(rawall["cksum"]["body_full"])
        except ValueError:
            doc = {}
        want = "%s/%s/%s" % (ctx.sides[side]["base"], REPO2, POM_REL)
        rawall["cksum"]["want_uri"] = want
        out["cksum_uri_form"] = ("base+prefix+path" if doc.get("uri") == want
                                 else "other:%s" % doc.get("uri"))
        uri = doc.get("uri") or ""
        base = ctx.sides[side]["base"]
        rel = uri[len(base):] if uri.startswith(base) else "/__unroutable__"
        g = ctx.http(side, "GET", rel)
        rawall["cksum"]["uri_get_status"] = g["status"]
        out["cksum_uri_resolvable"] = (
            "200+sha-ok" if g["status"] == 200 and
            mavenlib.sha256_hex(g["body"]) == mavenlib.sha256_hex(pom)
            else "status=%d" % g["status"])

    ctx.write_evidence("%s-leg.json" % side, {"asserts": out, "raw": rawall})
    return out


EXPECTED = {
    "pom_status": "201", "pom_uri_form": "base+prefix+path",
    "pom_dluri_form": "base+prefix+path", "pom_uri_resolvable": "200+sha-ok",
    "jar_status": "201", "jar_uri_form": "base+prefix+path",
    "jar_dluri_form": "base+prefix+path", "jar_uri_resolvable": "200+sha-ok",
    "meta_status": "201", "meta_uri_form": "base+prefix+path",
    "meta_dluri_form": "base+prefix+path", "meta_uri_resolvable": "200",
    "cksum_status": "201", "cksum_uri_form": "base+prefix+path",
    "cksum_uri_resolvable": "200+sha-ok",
}


def run(ctx):
    per_side = {}
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [REPO, REPO2])

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "registered face: 201 envelope uri/downloadUri == base "
                "(incl. context root) + repo + path, and the envelope uri "
                "GETs back the deployed bytes. Location headers are RAW "
                "forensics (arm 8, no ledger record -> not judged)"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "wire PUT pom/jar/maven-metadata.xml -> "
                                  "201 envelope + uri GET; checksum-deploy "
                                  "to second repo (pre-seeded pom)"}]}
