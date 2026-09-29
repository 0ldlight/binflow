"""T-563 fix verification (BIN-45; L034-R6 Arm 8 forensic -> flip leg):
every maven deploy 201 Location header must render THROUGH the context
root — byte-equal to the envelope uri (byte-deploy + checksum-deploy) or
to the prefixed TARGET path (checksum-file registration face, rest-api.md
§1.5: Location = TARGET artifact, no body) — and the Location must GET 200.

Arms (dual-oracle + static form, L030 two-round criterion):
  1-3  byte-deploy: pom / jar / maven-metadata.xml -> 201, Location ==
       envelope uri == base(incl. context root)+/<repo>/<path>, GET 200;
  4    X-Checksum-Deploy zero-transfer (pre-seeded pom) -> same equality;
  5-6  checksum-file PUT (.sha1/.md5 against the deployed pom) -> 201,
       Location == prefixed TARGET (the .pom path), empty body, TARGET GET
       200 + sha-ok.

A-face reference shape: L034-R6 Arm 8 (Location == uri through /artifactory
on all four envelope arms; bare-root unresolvable on pre-fix B).
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
    "id": "maven-deploy-201-location-header",
    "title": "maven deploy 201 Location header renders through the context "
             "root: byte-deploy x3 + checksum-deploy + checksum-file x2 "
             "(Location == uri / == prefixed TARGET, GET 200)",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 90,
}

REPO = "difftest-l034-loc"
REPO2 = "difftest-l034-loc2"
GROUP, ART, VER = "com.diff", "locp", "1.0.0"
GP = GROUP.replace(".", "/")
POM_REL = "%s/%s/%s/%s-%s.pom" % (GP, ART, VER, ART, VER)
JAR_REL = "%s/%s/%s/%s-%s.jar" % (GP, ART, VER, ART, VER)
META_REL = "%s/%s/maven-metadata.xml" % (GP, ART)

META_BODY = (
    '<metadata>\n  <groupId>%s</groupId>\n  <artifactId>%s</artifactId>\n'
    '  <versioning><versions><version>%s</version></versions></versioning>\n'
    '</metadata>\n' % (GROUP, ART, VER)).encode()


def _hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def _loc_get(ctx, side, loc, base, body):
    """GET the Location verbatim (path beyond base); returns dim slug."""
    rel = loc[len(base):] if loc.startswith(base) else "/__unroutable__"
    g = ctx.http(side, "GET", rel)
    if body is None:  # resolvability only (metadata face: server recalc)
        return "%d" % g["status"]
    return ("200+sha-ok" if g["status"] == 200 and
            mavenlib.sha256_hex(g["body"]) == mavenlib.sha256_hex(body)
            else "status=%d" % g["status"])


def _deploy_leg(ctx, side, tag, repo, rel, body, extra=None, pin_bytes=True):
    """PUT -> 201: Location == envelope uri == base+prefix+path; GET 200.

    pin_bytes=False (metadata face): the served bytes may be recalculated
    server-side (T-561 convention) — resolvability only, bytes not pinned.
    """
    asserts, raw = {}, {}
    r = ctx.http(side, "PUT", "/%s/%s" % (repo, rel), body=body,
                 headers=extra or {"Content-Type": "application/xml"})
    loc = _hdr(r, "Location") or ""
    raw["status"], raw["location"], raw["body_full"] = (
        r["status"], loc, r["body"].decode("utf-8", "replace"))
    asserts["%s_status" % tag] = "%d" % r["status"]
    want = "%s/%s/%s" % (ctx.sides[side]["base"], repo, rel)
    raw["want"] = want
    asserts["%s_loc_form" % tag] = ("base+prefix+path" if loc == want
                                    else "other:%s" % loc)
    if r["status"] == 201:
        try:
            uri = json.loads(raw["body_full"]).get("uri", "")
        except ValueError:
            uri = ""
        asserts["%s_loc_eq_uri" % tag] = "equal" if loc and loc == uri else "other"
        asserts["%s_loc_get" % tag] = _loc_get(
            ctx, side, loc, ctx.sides[side]["base"],
            body if pin_bytes else None)
    return asserts, raw


def _checksumfile_leg(ctx, side, algo, pom_rel, pom_body):
    """PUT <pom>.<algo> (digest body) -> 201: Location == prefixed TARGET,
    empty body, TARGET GET 200+sha-ok."""
    asserts, raw = {}, {}
    digest = (mavenlib.sha1_hex(pom_body) if algo == "sha1"
              else hashlib.md5(pom_body).hexdigest())  # noqa: S324
    r = ctx.http(side, "PUT", "/%s/%s.%s" % (REPO, pom_rel, algo),
                 body=digest.encode(), headers={"Content-Type": "text/plain"})
    loc = _hdr(r, "Location") or ""
    raw["status"], raw["location"], raw["body_len"] = (
        r["status"], loc, len(r["body"]))
    asserts["cf_%s_status" % algo] = "%d" % r["status"]
    want = "%s/%s/%s" % (ctx.sides[side]["base"], REPO, pom_rel)
    raw["want"] = want
    asserts["cf_%s_loc_form" % algo] = ("base+prefix+target" if loc == want
                                        else "other:%s" % loc)
    asserts["cf_%s_body" % algo] = "empty" if not r["body"] else "nonempty"
    if loc == want:
        asserts["cf_%s_target_get" % algo] = _loc_get(
            ctx, side, loc, ctx.sides[side]["base"], pom_body)
    return asserts, raw


def _leg(ctx, side):
    out, rawall = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(REPO), mavenlib.maven_local(REPO2)])
    pom = mavenlib.pom_fixture(GROUP, ART, VER)
    jar = mavenlib.jar_fixture("location-minidiff-l034")

    for tag, rel, body, extra, pin in (
            ("pom", POM_REL, pom, None, True),
            ("jar", JAR_REL, jar, None, True),
            ("meta", META_REL, META_BODY, None, False)):
        a, raw = _deploy_leg(ctx, side, tag, REPO, rel, body, extra,
                             pin_bytes=pin)
        out.update(a)
        rawall[tag] = raw

    # checksum-deploy zero-transfer: pre-seeded pom, second repo
    a, raw = _deploy_leg(ctx, side, "cksum", REPO2, POM_REL, pom, {
        "X-Checksum-Deploy": "true",
        "X-Checksum-Sha1": mavenlib.sha1_hex(pom),
        "X-Checksum-Md5": hashlib.md5(pom).hexdigest()})  # noqa: S324
    out.update(a)
    rawall["cksum"] = raw

    # checksum-file registration face x2 (target = the deployed pom)
    for algo in ("sha1", "md5"):
        a, raw = _checksumfile_leg(ctx, side, algo, POM_REL, pom)
        out.update(a)
        rawall["cf_" + algo] = raw

    ctx.write_evidence("%s-leg.json" % side, {"asserts": out, "raw": rawall})
    return out


EXPECTED = {
    "pom_status": "201", "pom_loc_form": "base+prefix+path",
    "pom_loc_eq_uri": "equal", "pom_loc_get": "200+sha-ok",
    "jar_status": "201", "jar_loc_form": "base+prefix+path",
    "jar_loc_eq_uri": "equal", "jar_loc_get": "200+sha-ok",
    "meta_status": "201", "meta_loc_form": "base+prefix+path",
    "meta_loc_eq_uri": "equal", "meta_loc_get": "200",
    "cksum_status": "201", "cksum_loc_form": "base+prefix+path",
    "cksum_loc_eq_uri": "equal", "cksum_loc_get": "200+sha-ok",
    "cf_sha1_status": "201", "cf_sha1_loc_form": "base+prefix+target",
    "cf_sha1_body": "empty", "cf_sha1_target_get": "200+sha-ok",
    "cf_md5_status": "201", "cf_md5_loc_form": "base+prefix+target",
    "cf_md5_body": "empty", "cf_md5_target_get": "200+sha-ok",
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
        "note": "T-563 face: 201 Location renders through the context root "
                "on every maven deploy chain — byte-equal to the envelope "
                "uri (byte-deploy + checksum-deploy) or the prefixed TARGET "
                "(checksum-file registration: Location=target, empty body, "
                "rest-api.md §1.5); the Location must GET 200"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "wire PUT pom/jar/maven-metadata.xml + "
                                  "checksum-deploy (pre-seeded) + checksum-"
                                  "file .sha1/.md5; Location form/equality/"
                                  "GET per arm"}]}
