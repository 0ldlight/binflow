"""T-554 Arm C lineage -> L034 arms 2/3 (T-558 contracts / T-559 fix
verification): handle* seat via LOCAL members, now with BYTE-EXACT message
comparison and the long-path truncation leg that settles t559's
"300-char ceiling" extrapolation.

History: L033 r3-r6 pinned the divergence (9 dims); T-558/BIN-40 ruled the
three faces BUG; T-559/BIN-41 implemented the aligned wording family
(put.go ME-08 replacement + handler.go read-path class gate). This case now
judges:
  - the four message dims as FULL parsed strings (dual oracle: byte-exact
    a vs b — L033's harness-side 300-char body capture hid the tail, the
    extrapolated "no server-side truncation, closing quote + envelope"
    gets direct-tested here via the long-path leg);
  - the member GET class gate (409 not 404) on both unlanded legs;
  - the pre-existing plainsnap control legs (expected to stay red until
    T-562/BIN-44 — the walk-family resolve face, OUT of T-559 scope);
  - module/version metadata merge + resolution legs (L033/T-556 lineage).
"""
import importlib.util
import json
import os

_spec = importlib.util.spec_from_file_location(
    "difftest_mavenlib",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "_mavenlib.py"))
mavenlib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(mavenlib)

CASE = {
    "id": "maven-local-handle-walk-skip",
    "title": "handle* via LOCAL members — 409 wording family byte-exact, "
             "member GET class gate, long-path truncation ruling, "
             "walk/merge legs (plainsnap controls expected divergent)",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 150,
}

TAG = "l034"
HR = "difftest-%s-hr" % TAG    # local maven, handleReleases=false
HS = "difftest-%s-hs" % TAG    # local maven, handleSnapshots=false
CTL = "difftest-%s-ctl" % TAG  # local maven, defaults
V = "difftest-%s-v" % TAG      # virtual [hr, hs, ctl]
GROUP = "com.diff"
GP = GROUP.replace(".", "/")

SNAP = "1.0.0-SNAPSHOT"

# long group path: 16 segments x 12 chars = 207 chars of group, pushing the
# '<K>:<path>' artifact reference (and the whole message) far past any
# 300-char ceiling — the truncation ruling leg.
GP_LONG = "/".join(["g" * 12] * 16)
LONG_PATH = "/%s/%s/lp/1.0.0/lp-1.0.0.pom" % (HR, GP_LONG)

DUAL_KEYS = (
    "seat_hr_echo", "seat_hs_echo",
    "put_rel_to_hr_status", "put_rel_to_hr_msg",
    "put_snap_to_hs_status", "put_snap_to_hs_msg",
    "member_hr_modulemeta_versions",
    "direct_get_relpath_hr_status", "direct_get_relpath_hr_msg",
    "direct_get_snappath_hs_status", "direct_get_snappath_hs_msg",
    "longpath_get_status", "longpath_get_msg", "longpath_msg_len",
    "longpath_fill_identity",
    "virt_resolve_snap_from_hr", "virt_resolve_rel_from_ctl",
    "virt_snapmeta_from_hr_status", "virt_snapmeta_from_hr_sv",
    "virt_modmeta_hwm_status", "virt_modmeta_hwm_versions",
    "virt_modmeta_hwc_status", "virt_modmeta_hwc_versions",
    # confound controls (L033 r1 finding): B 404s plain-SNAPSHOT pom GETs
    # even from a DEFAULT member — expected divergent until T-562/BIN-44.
    "control_direct_get_plainsnap",
    "control_virt_get_plainsnap",
    "control_virt_modmeta_plainsnap",
)

A_TEMPLATE_MARK = "due to conflict in the snapshot release handling policy."


def _parse_message(body_text):
    """Extract errors[0].message from an errors[] envelope, full string."""
    try:
        doc = json.loads(body_text)
        return doc["errors"][0]["message"]
    except Exception:  # noqa: BLE001 - forensic, never raise
        return "(unparseable)%s" % body_text[:200]


def _versions(body):
    try:
        md = mavenlib.parse_metadata(body)
        return ",".join(md["versions"]) or "(empty)"
    except Exception:  # noqa: BLE001
        return "parse-error"


def _sv_count(body):
    try:
        md = mavenlib.parse_metadata(body)
        return "%d" % len(md["snapshot_versions"])
    except Exception:  # noqa: BLE001
        return "parse-error"


def _put_pom_status(ctx, side, repo, art, ver):
    r = mavenlib.put_pom(ctx, side, repo, GROUP, art, ver)
    return r["status"], r["path"]


def _get(ctx, side, path):
    return ctx.http(side, "GET", path)


def _seat(ctx, side, key, field):
    cfg = ctx.http(side, "GET", "/api/repositories/" + key)
    try:
        doc = json.loads(cfg["body"].decode("utf-8", "replace"))
    except ValueError:
        doc = {}
    v = doc.get(field)
    return ("false" if v is False else "true" if v is True else "absent")


def _leg(ctx, side):
    asserts, raw = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        mavenlib.maven_local(HR, handleReleases=False),
        mavenlib.maven_local(HS, handleSnapshots=False),
        mavenlib.maven_local(CTL),
        mavenlib.maven_virtual(V, [HR, HS, CTL]),
    ])

    # ---- P0 seats (precondition: both sides persist the local seat)
    asserts["seat_hr_echo"] = _seat(ctx, side, HR, "handleReleases")
    asserts["seat_hs_echo"] = _seat(ctx, side, HS, "handleSnapshots")

    # ---- construction legs
    st, _ = _put_pom_status(ctx, side, HR, "hwr", "1.0.0")
    raw["put_rel_to_hr_status"] = st
    asserts["put_rel_to_hr_status"] = "status=%d" % st
    pr = ctx.http(side, "PUT", "/%s/%s/hwr/1.0.0/hwr-1.0.0.pom" % (HR, GP),
                  body=mavenlib.pom_fixture(GROUP, "hwr", "1.0.0"),
                  headers={"Content-Type": "application/xml"})
    raw["put_rel_to_hr_body_full"] = pr["body"].decode("utf-8", "replace")
    asserts["put_rel_to_hr_msg"] = _parse_message(raw["put_rel_to_hr_body_full"])
    st, _ = _put_pom_status(ctx, side, HS, "hws", SNAP)
    raw["put_snap_to_hs_status"] = st
    asserts["put_snap_to_hs_status"] = "status=%d" % st
    ps = ctx.http(side, "PUT", "/%s/%s/hws/%s/hws-%s.pom" % (HS, GP, SNAP, SNAP),
                  body=mavenlib.pom_fixture(GROUP, "hws", SNAP),
                  headers={"Content-Type": "application/xml"})
    raw["put_snap_to_hs_body_full"] = ps["body"].decode("utf-8", "replace")
    asserts["put_snap_to_hs_msg"] = _parse_message(raw["put_snap_to_hs_body_full"])

    # C3: SNAPSHOT into the handleReleases=false member (constructable face)
    st, _ = _put_pom_status(ctx, side, HR, "hwm", SNAP)
    raw["put_snap_to_hr_status"] = st
    if st not in (200, 201):
        raise mavenlib.SetupError(
            "snapshot PUT into handleReleases=false member refused on side "
            "%r (status=%d) — the constructable face collapsed; legs below "
            "would be vacuous" % (side, st))
    # C4: control release into the default member
    st, _ = _put_pom_status(ctx, side, CTL, "hwc", "1.0.0")
    raw["put_rel_to_ctl_status"] = st
    if st not in (200, 201):
        raise mavenlib.SetupError("control release PUT failed side %r: %d"
                                  % (side, st))
    # C5: confound control — the SAME plain-SNAPSHOT spelling homed in the
    # DEFAULT member (expected divergent until T-562/BIN-44).
    st, _ = _put_pom_status(ctx, side, CTL, "plainsnap", SNAP)
    raw["put_plainsnap_to_ctl_status"] = st
    if st not in (200, 201):
        raise mavenlib.SetupError("plainsnap control PUT failed side %r: %d"
                                  % (side, st))

    # ---- settle member-level metadata calc (async, both sides)
    def _ready_hwm(status, body):
        if status != 200:
            return False
        try:
            return "1.0.0-SNAPSHOT" in mavenlib.parse_metadata(body)["versions"]
        except Exception:  # noqa: BLE001
            return False
    st, body = mavenlib.poll_until(
        ctx, side, "/%s/%s/hwm/maven-metadata.xml" % (HR, GP),
        _ready_hwm, budget_s=30.0)
    asserts["member_hr_modulemeta_versions"] = (
        _versions(body) if st == 200 else "status=%d" % st)
    raw["member_hr_modulemeta_status"] = st

    def _ready_hwc(status, body):
        if status != 200:
            return False
        try:
            return "1.0.0" in mavenlib.parse_metadata(body)["versions"]
        except Exception:  # noqa: BLE001
            return False
    mavenlib.poll_until(ctx, side, "/%s/%s/hwc/maven-metadata.xml" % (CTL, GP),
                        _ready_hwc, budget_s=30.0)
    mavenlib.poll_until(
        ctx, side, "/%s/%s/hwm/%s/maven-metadata.xml" % (HR, GP, SNAP),
        lambda s, b: s == 200, budget_s=30.0)
    mavenlib.poll_until(
        ctx, side, "/%s/%s/plainsnap/maven-metadata.xml" % (CTL, GP),
        _ready_hwm, budget_s=30.0)

    # ---- member-face class-policy GET legs (full message capture)
    g1 = _get(ctx, side, "/%s/%s/hwr/1.0.0/hwr-1.0.0.pom" % (HR, GP))
    raw["direct_get_relpath_hr_status"] = g1["status"]
    raw["direct_get_relpath_hr_body_full"] = g1["body"].decode("utf-8", "replace")
    asserts["direct_get_relpath_hr_status"] = "status=%d" % g1["status"]
    asserts["direct_get_relpath_hr_msg"] = _parse_message(
        raw["direct_get_relpath_hr_body_full"])
    g2 = _get(ctx, side, "/%s/%s/hws/%s/hws-%s.pom" % (HS, GP, SNAP, SNAP))
    raw["direct_get_snappath_hs_status"] = g2["status"]
    raw["direct_get_snappath_hs_body_full"] = g2["body"].decode("utf-8", "replace")
    asserts["direct_get_snappath_hs_status"] = "status=%d" % g2["status"]
    asserts["direct_get_snappath_hs_msg"] = _parse_message(
        raw["direct_get_snappath_hs_body_full"])

    # ---- long-path truncation ruling leg (t559 extrapolation -> direct
    # test): unlanded release path, '<K>:<path>' pushes the message far
    # past 300 chars. Judge: full message, its length, and whether the
    # "; Path:" fill is byte-identical to the inner artifact reference.
    g3 = _get(ctx, side, LONG_PATH)
    raw["longpath_get_status"] = g3["status"]
    raw["longpath_get_body_full"] = g3["body"].decode("utf-8", "replace")
    msg = _parse_message(raw["longpath_get_body_full"])
    asserts["longpath_get_status"] = "status=%d" % g3["status"]
    asserts["longpath_get_msg"] = msg
    asserts["longpath_msg_len"] = "%d" % len(msg)
    ref = "%s:%s" % (HR, LONG_PATH[len("/%s/" % HR):])
    fill = "; Path: '%s'" % ref
    cut = msg.find("; Path: ")
    asserts["longpath_fill_identity"] = (
        "identical" if cut > 0 and msg.endswith(fill) and
        ("'%s'" % ref) in msg[:cut] else "other")

    # ---- virtual resolution legs
    v1 = _get(ctx, side, "/%s/%s/hwm/%s/hwm-%s.pom" % (V, GP, SNAP, SNAP))
    raw["virt_resolve_snap_from_hr"] = v1["status"]
    asserts["virt_resolve_snap_from_hr"] = "status=%d" % v1["status"]
    v2 = _get(ctx, side, "/%s/%s/hwc/1.0.0/hwc-1.0.0.pom" % (V, GP))
    raw["virt_resolve_rel_from_ctl"] = v2["status"]
    asserts["virt_resolve_rel_from_ctl"] = "status=%d" % v2["status"]

    # ---- confound control legs: same plain-SNAPSHOT spelling, DEFAULT home
    c1 = _get(ctx, side, "/%s/%s/plainsnap/%s/plainsnap-%s.pom" % (CTL, GP, SNAP, SNAP))
    raw["control_direct_get_plainsnap"] = c1["status"]
    asserts["control_direct_get_plainsnap"] = "status=%d" % c1["status"]
    c2 = _get(ctx, side, "/%s/%s/plainsnap/%s/plainsnap-%s.pom" % (V, GP, SNAP, SNAP))
    raw["control_virt_get_plainsnap"] = c2["status"]
    asserts["control_virt_get_plainsnap"] = "status=%d" % c2["status"]
    c3 = _get(ctx, side, "/%s/%s/plainsnap/maven-metadata.xml" % (V, GP))
    raw["control_virt_modmeta_plainsnap_status"] = c3["status"]
    asserts["control_virt_modmeta_plainsnap"] = (
        _versions(c3["body"]) if c3["status"] == 200 else "status=%d" % c3["status"])

    # ---- virtual metadata legs
    v3 = _get(ctx, side, "/%s/%s/hwm/%s/maven-metadata.xml" % (V, GP, SNAP))
    raw["virt_snapmeta_from_hr_status"] = v3["status"]
    asserts["virt_snapmeta_from_hr_status"] = "status=%d" % v3["status"]
    asserts["virt_snapmeta_from_hr_sv"] = (
        _sv_count(v3["body"]) if v3["status"] == 200 else "status=%d" % v3["status"])
    v4 = _get(ctx, side, "/%s/%s/hwm/maven-metadata.xml" % (V, GP))
    raw["virt_modmeta_hwm_status"] = v4["status"]
    asserts["virt_modmeta_hwm_status"] = "status=%d" % v4["status"]
    asserts["virt_modmeta_hwm_versions"] = (
        _versions(v4["body"]) if v4["status"] == 200 else "status=%d" % v4["status"])
    v5 = _get(ctx, side, "/%s/%s/hwc/maven-metadata.xml" % (V, GP))
    raw["virt_modmeta_hwc_status"] = v5["status"]
    asserts["virt_modmeta_hwc_status"] = "status=%d" % v5["status"]
    asserts["virt_modmeta_hwc_versions"] = (
        _versions(v5["body"]) if v5["status"] == 200 else "status=%d" % v5["status"])

    ctx.write_evidence("%s-leg.json" % side, {"asserts": asserts, "raw": raw})
    return asserts


def run(ctx):
    per_side = {}
    keys = [V, HR, HS, CTL]
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, keys)

    a, b = per_side.get("a", {}), per_side.get("b", {})
    expected = {k: a[k] for k in DUAL_KEYS if k in a}
    status, reason, mism = mavenlib.judge(per_side, expected)
    ev = ctx.write_evidence("summary.json", {
        "expected": expected, "a": a, "b": b, "mismatches": mism,
        "note": "dual oracle: expected = live a per dimension. Message dims "
                "are FULL parsed strings (L033's 300-char harness capture "
                "hidden the tail — longpath leg rules on server-side "
                "truncation directly). plainsnap control dims are the "
                "registered walk-family face (T-562/BIN-44) and are "
                "EXPECTED divergent; release-in-hr sub-face NOT construct-"
                "able (deploy-time 409 on both sides)"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "create local hr=false/hs=false/ctl + "
                                  "virtual; wire-PUT constructions; member/"
                                  "virtual GET resolution + module/version "
                                  "metadata probes + long-path 409 leg"}]}
