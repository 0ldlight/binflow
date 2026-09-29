"""L035 / T-562 stage 1 — W3/W4: same-face evidence for the timestamp
spelling arm + real-client (mvn deploy) construction + virtual multi-member
walk (FORENSICS, dual oracle, expected = live a).

Arms:
  c1  byte identity: plain PUT -> Location carries the rewritten spelling
      T1; GET plain vs GET T1 must be byte-identical (A), and a second
      plain PUT (T2) must flip the plain GET to T2's bytes — the plain GET
      is a live resolve, not a frozen alias.
  c2  HEAD plain vs HEAD resolved-timestamp spelling: same Content-Length
      and ETag? (does the plain GET echo the target's validators?)
  c3  real mvn deploy:deploy-file SNAPSHOT into the unique home (A via the
      memory-only forward proxy, B direct), then: dir listing (what
      spellings/metadata landed), plain GET member + plain GET virtual.
      This constructs the "timestamp spelling arm" (mvn unique deploy) and
      tests whether its plain resolve is the SAME walk face.
  c4  virtual multi-member global walk: two unique locals m1(ts=20260601)
      / m2(ts=20260707), virtual [m1, m2]; plain GET via the virtual —
      global-max across members or member-order precedence?
"""
import importlib.util
import os
import urllib.parse

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-walk-sameface-mvn",
    "title": "FORENSICS (T-562 s1/W3+W4): plain GET byte-identity with the "
             "rewritten spelling, real mvn deploy construction, virtual "
             "multi-member walk",
    "layer": "L5",
    "domain": "maven",
    "auth": True,
    "timeout_s": 240,
}

REPO = "difftest-l035-wm"
VIRT = "difftest-l035-wmv"
M1 = "difftest-l035-wm1"
M2 = "difftest-l035-wm2"
VIRT2 = "difftest-l035-wmv2"
GROUP, ART = "com.diff", "wmart"
SNAP = "2.0-SNAPSHOT"
MVN_GA = ("com.diff.walkmvn", "walkmvn", "1.0-SNAPSHOT")
GP = GROUP.replace(".", "/")
DIR_REL = "%s/%s/%s" % (GP, ART, SNAP)
PLAIN_REL = "%s/%s-%s.pom" % (DIR_REL, ART, SNAP)

P1 = mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
    b"</project>", b"<!-- wm-p1 -->\n</project>")
P2 = mavenlib.pom_fixture(GROUP, ART, SNAP).replace(
    b"</project>", b"<!-- wm-p2 -->\n</project>")


def _hdr(resp, name):
    for k, v in resp["headers"].items():
        if k.lower() == name.lower():
            return v
    return None


def _put(ctx, side, repo, rel, body):
    import hashlib
    return ctx.http(side, "PUT", "/%s/%s" % (repo, rel), body=body, headers={
        "X-Checksum-Sha1": mavenlib.sha1_hex(body),
        "X-Checksum-Md5": hashlib.md5(body).hexdigest()})  # noqa: S324


def _loc_rel(location):
    """Location URL -> repo-relative storage path (strip scheme+base)."""
    p = urllib.parse.urlsplit(location or "").path
    for pref in ("/artifactory/", "/binflow/"):
        if p.startswith(pref):
            return p[len(pref):]
    return p.lstrip("/")


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(REPO, snapshotVersionBehavior="unique")])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(VIRT, [REPO]))

    # c1: plain PUT -> T1; byte identity plain GET vs T1 GET; second PUT
    r1 = _put(ctx, side, REPO, PLAIN_REL, P1)
    raw["c1_put1_status"] = r1["status"]
    raw["c1_put1_location"] = _hdr(r1, "Location") or ""
    t1_rel = _loc_rel(raw["c1_put1_location"])
    raw["c1_t1_rel"] = t1_rel
    g_plain = ctx.http(side, "GET", "/%s/%s" % (REPO, PLAIN_REL))
    g_ts1 = ctx.http(side, "GET", "/" + t1_rel)
    raw["c1_plain_status"], raw["c1_ts1_status"] = g_plain["status"], g_ts1["status"]
    if g_plain["status"] == 200 and g_ts1["status"] == 200:
        asserts["c1_plain_get_eq_ts1_bytes"] = (
            "byte-identical" if g_plain["body"] == g_ts1["body"] else "DIFFER")
        asserts["c1_plain_get_is_p1"] = (
            "yes" if g_plain["body"] == P1 else "no")
    else:
        asserts["c1_plain_get_eq_ts1_bytes"] = "plain=%d,ts1=%d" % (
            g_plain["status"], g_ts1["status"])
        asserts["c1_plain_get_is_p1"] = "plain_status=%d" % g_plain["status"]
    r2 = _put(ctx, side, REPO, PLAIN_REL, P2)
    raw["c1_put2_status"] = r2["status"]
    raw["c1_put2_location"] = _hdr(r2, "Location") or ""
    t2_rel = _loc_rel(raw["c1_put2_location"])
    raw["c1_t2_rel"] = t2_rel
    g_plain = ctx.http(side, "GET", "/%s/%s" % (REPO, PLAIN_REL))
    g_ts2 = ctx.http(side, "GET", "/" + t2_rel)
    if g_plain["status"] == 200 and g_ts2["status"] == 200:
        asserts["c1_after_put2_plain_eq_ts2"] = (
            "byte-identical" if g_plain["body"] == g_ts2["body"] else "DIFFER")
    else:
        asserts["c1_after_put2_plain_eq_ts2"] = "plain=%d,ts2=%d" % (
            g_plain["status"], g_ts2["status"])

    # c2: HEAD plain vs HEAD T2 — validators
    h_plain = ctx.http(side, "HEAD", "/%s/%s" % (REPO, PLAIN_REL))
    h_ts2 = ctx.http(side, "HEAD", "/" + t2_rel)
    raw["c2_head_plain"] = {"status": h_plain["status"],
                            "len": _hdr(h_plain, "Content-Length"),
                            "etag": _hdr(h_plain, "ETag")}
    raw["c2_head_ts2"] = {"status": h_ts2["status"],
                          "len": _hdr(h_ts2, "Content-Length"),
                          "etag": _hdr(h_ts2, "ETag")}
    asserts["c2_head_plain_eq_ts2"] = "%s/%s/%s" % (
        h_plain["status"], _hdr(h_plain, "Content-Length"),
        "etag_eq" if (_hdr(h_plain, "ETag") and _hdr(h_plain, "ETag") ==
                      _hdr(h_ts2, "ETag")) else "etag_ne_or_absent")

    ctx.write_evidence("%s-leg-stage1.json" % side,
                       {"asserts": asserts, "raw": raw})
    return asserts, raw


def run(ctx):
    per_side, raw_side = {}, {}
    fwd = None
    try:
        for side in ("a", "b"):
            per_side[side], raw_side[side] = _leg(ctx, side)

        # c3: real mvn deploy:deploy-file SNAPSHOT (both sides; A via the
        # in-memory forward proxy). Fresh GA so the c1 state cannot leak in.
        group, art, ver = MVN_GA
        wd = mavenlib.scratch_dir()
        jar = os.path.join(wd, "wmvn.jar")
        pom = os.path.join(wd, "wmvn.pom")
        with open(jar, "wb") as fh:
            fh.write(b"walk-mvn jar payload\n")
        with open(pom, "wb") as fh:
            fh.write(mavenlib.pom_fixture(group, art, ver).replace(
                b"</project>", b"<!-- wmvn -->\n</project>"))
        mvn_dir = "%s/%s/%s" % (group.replace(".", "/"), art, ver)
        try:
            for side in ("a", "b"):
                if side == "a":
                    fwd, fport = mavenlib.start_forward_proxy(
                        ctx.sides["a"]["base"],
                        ctx.sides["a"]["user"], ctx.sides["a"]["password"])
                    # forwarder targets upstream=<base incl. /artifactory>
                    # and relays the path verbatim — no context suffix here
                    # (the L034 r1 lesson)
                    repo_url = "http://127.0.0.1:%d/%s" % (fport, REPO)
                else:
                    repo_url = "%s/%s" % (ctx.sides["b"]["base"], REPO)
                out = mavenlib.mvn_deploy_file(
                    ctx, side, repo_url, wd, group, art, ver, jar, pom)
                raw_side[side]["c3_mvn"] = {"exit": out["exit"],
                                            "tail": out["tail"][-400:]}
                lst = ctx.http(side, "GET",
                               "/api/storage/%s/%s?list&deep=1&listFolders=1"
                               % (REPO, mvn_dir))
                raw_side[side]["c3_dir_listing"] = (
                    lst["body"].decode("utf-8", "replace")[:1600]
                    if lst["status"] == 200 else "status=%d" % lst["status"])
                for name, repo in (("member", REPO), ("virtual", VIRT)):
                    g = ctx.http(side, "GET", "/%s/%s/%s-%s.pom"
                                 % (repo, mvn_dir, art, ver))
                    hit = ("mvn-pom" if g["status"] == 200 and
                           b"<!-- wmvn -->" in g["body"]
                           else ("other-200" if g["status"] == 200
                                 else "status=%d" % g["status"]))
                    per_side[side]["c3_plain_pom_%s" % name] = hit
        finally:
            if fwd:
                fwd.shutdown()
                fwd.server_close()
            mavenlib.rm_scratch(wd)

        # c4: virtual multi-member global walk. Path carries the full
        # layout (group path + ARTIFACT DIR + version dir — r0 lesson: A
        # derives GAV from layout and 409s otherwise). PUT statuses and
        # per-member GETs recorded so an empty member is self-diagnosing.
        for side in ("a", "b"):
            mavenlib.ensure_repos(ctx, side, [
                mavenlib.maven_local(M1, snapshotVersionBehavior="unique"),
                mavenlib.maven_local(M2, snapshotVersionBehavior="unique")])
            mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(
                VIRT2, [M1, M2]))
            gp4 = "com/diff/wmglobal/wmglobal/3.0-SNAPSHOT"
            b1 = mavenlib.pom_fixture("com.diff.wmglobal", "wmglobal",
                                      "3.0-SNAPSHOT").replace(
                b"</project>", b"<!-- c4-m1-old -->\n</project>")
            b2 = mavenlib.pom_fixture("com.diff.wmglobal", "wmglobal",
                                      "3.0-SNAPSHOT").replace(
                b"</project>", b"<!-- c4-m2-new -->\n</project>")
            r1 = _put(ctx, side, M1, "%s/wmglobal-3.0-20260601.000001-1.pom"
                      % gp4, b1)
            r2 = _put(ctx, side, M2, "%s/wmglobal-3.0-20260707.000001-1.pom"
                      % gp4, b2)
            raw_side[side]["c4_puts"] = {"m1": r1["status"], "m2": r2["status"]}
            for name, repo, body in (("m1_ts", M1, b1), ("m2_ts", M2, b2)):
                g = ctx.http(side, "GET", "/%s/%s/wmglobal-3.0-%s.pom"
                             % (repo, gp4,
                                "20260601.000001-1" if repo == M1
                                else "20260707.000001-1"))
                raw_side[side]["c4_member_get_%s" % name] = "%s%s" % (
                    g["status"], "/bytes-ok" if g["status"] == 200 and
                    g["body"] == body else "/bytes-mismatch")
            for name, repo, body in (("m1_plain", M1, b1),
                                     ("m2_plain", M2, b2)):
                g = ctx.http(side, "GET", "/%s/%s/wmglobal-3.0-SNAPSHOT.pom"
                             % (repo, gp4))
                raw_side[side]["c4_member_get_%s" % name] = (
                    "%d/bytes-ok" % g["status"]
                    if g["status"] == 200 and g["body"] == body
                    else "%d" % g["status"])
            g = ctx.http(side, "GET", "/%s/%s/wmglobal-3.0-SNAPSHOT.pom"
                         % (VIRT2, gp4))
            per_side[side]["c4_virt2_plain_get"] = (
                "c4-m1-old" if g["status"] == 200 and g["body"] == b1 else
                "c4-m2-new" if g["status"] == 200 and g["body"] == b2 else
                "other-200" if g["status"] == 200 else
                "status=%d" % g["status"])
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, [VIRT2, M1, M2, VIRT, REPO])
        if fwd:
            try:
                fwd.shutdown()
            except Exception:  # noqa: BLE001
                pass

    a, b = per_side.get("a", {}), per_side.get("b", {})
    dual = ("c1_plain_get_eq_ts1_bytes", "c1_plain_get_is_p1",
            "c1_after_put2_plain_eq_ts2", "c2_head_plain_eq_ts2",
            "c3_plain_pom_member", "c3_plain_pom_virtual",
            "c4_virt2_plain_get")
    expected = {k: a[k] for k in dual if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    for s in ("a", "b"):
        ctx.write_evidence("%s-leg.json" % s,
                           {"asserts": per_side[s], "raw": raw_side[s]})
    ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "FORENSICS T-562 s1/W3+W4. c1/c2 byte-identity + validator "
                "echo; c3 real mvn deploy construction (same-face test for "
                "the timestamp spelling arm); c4 virtual global-max walk. "
                "a!=b on resolve dims is the registered fence face."})
    return {"status": status, "reason": reason,
            "evidence": ["evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"],
                         "evidence/%s/summary.json" % CASE["id"]],
            "requests": [{"step": "wire PUT x2 + byte-identity probes; mvn "
                                  "deploy:deploy-file SNAPSHOT; two-member "
                                  "virtual plain GET"}]}
