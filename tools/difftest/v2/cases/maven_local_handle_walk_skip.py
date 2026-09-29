"""T-554 Arm C (ledger maven/virtual-metadata-modulereleases-skip + T-541
walk-layer handle*): the handle* seat is unreachable on the REMOTE face
(BinFlow's remote canonical does not persist handle* — the seat probe's
b=true), but the LOCAL face persists the seat on BOTH sides, and A's live
behavior there is a live oracle, not a guess.

Live A finding that shaped this case (manual probe, 2026-09-29, deleted
afterwards): a local maven repo with handleReleases=false REFUSES a release
pom PUT with 409 — and even a direct GET of a release path answers 409
(path-class vs handle-policy is a read+write gate on the member face in A).
So "a release artifact homed in a handleReleases=false member" is NOT
constructable on either side (BinFlow put.go ME-08 refuses the same PUT);
that sub-face stays NOT_RUN by construction, recorded honestly.

What IS constructable: a handleReleases=false local member holding a
SNAPSHOT (its handleSnapshots stays true) and a handleSnapshots=false local
member holding a release. Then, through a virtual [hr, hs, ctl]:
  - the walk-layer release/snapshot family skips (internal/repo/virtual.go,
    T-541) and the module-level merge skip (virtual_metadata.go
    filterMetadataSteps — the §3.4 drift candidate: does A still list the
    handleReleases=false member's snapshot version in the merged module
    maven-metadata.xml? A behavior UNKNOWN = the ledger item's open
    authority) — every dimension is judged b == live-a (dual oracle);
    any a != b lands in ab_divergence and feeds the ruling, never guessed.
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
    "title": "handle* walk/merge skips via LOCAL members (seat persisted) — "
             "release/snapshot family resolution + module-level listing, "
             "live A oracle",
    "layer": "L7",
    "domain": "maven",
    "auth": True,
    "timeout_s": 150,
}

TAG = "r5t554"
HR = "difftest-%s-hr" % TAG    # local maven, handleReleases=false
HS = "difftest-%s-hs" % TAG    # local maven, handleSnapshots=false
CTL = "difftest-%s-ctl" % TAG  # local maven, defaults
V = "difftest-%s-v" % TAG      # virtual [hr, hs, ctl]
GROUP = "com.diff"
GP = GROUP.replace(".", "/")

SNAP = "1.0.0-SNAPSHOT"

# every dimension is dual-oracle (expected = live a); no static pins.
DUAL_KEYS = (
    "seat_hr_echo", "seat_hs_echo",
    "put_rel_to_hr_status", "put_rel_to_hr_family",
    "put_snap_to_hs_status", "put_snap_to_hs_family",
    "member_hr_modulemeta_versions",
    "direct_get_relpath_hr_status", "direct_get_relpath_hr_family",
    "direct_get_snappath_hs_status", "direct_get_snappath_hs_family",
    "virt_resolve_snap_from_hr", "virt_resolve_rel_from_ctl",
    "virt_snapmeta_from_hr_status", "virt_snapmeta_from_hr_sv",
    "virt_modmeta_hwm_status", "virt_modmeta_hwm_versions",
    "virt_modmeta_hwc_status", "virt_modmeta_hwc_versions",
    # confound controls (r1 finding): B 404s plain-SNAPSHOT pom GETs even
    # from a DEFAULT member — virt_resolve_snap_from_hr's divergence is
    # handle-unattributable unless these controls PASS (b == a).
    "control_direct_get_plainsnap",
    "control_virt_get_plainsnap",
    "control_virt_modmeta_plainsnap",
)

FAMILY_REFUSAL = "handling of"  # BinFlow ME-08 family; A wording unknown


def _families(status, body):
    if status == 409:
        return ("refusal-409:" +
                (FAMILY_REFUSAL if FAMILY_REFUSAL in body else "other-409"))
    return "status=%d" % status


def _versions(body):
    try:
        md = mavenlib.parse_metadata(body)
        return ",".join(md["versions"]) or "(empty)"
    except Exception:  # noqa: BLE001 - forensic, never raise
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
    # C1: release pom into handleReleases=false member — the face that must
    # NOT land on either side (A live: 409; B ME-08: 409). Status recorded;
    # a landed 201 poisons this leg only (ab_divergence tells the truth).
    st, _ = _put_pom_status(ctx, side, HR, "hwr", "1.0.0")
    raw["put_rel_to_hr_status"] = st
    asserts["put_rel_to_hr_status"] = "status=%d" % st
    # re-GET the exact body for the family judgement (put_pom drops it)
    pr = ctx.http(side, "PUT", "/%s/%s/hwr/1.0.0/hwr-1.0.0.pom" % (HR, GP),
                  body=mavenlib.pom_fixture(GROUP, "hwr", "1.0.0"),
                  headers={"Content-Type": "application/xml"})
    raw["put_rel_to_hr_body"] = pr["body"][:300].decode("utf-8", "replace")
    asserts["put_rel_to_hr_family"] = _families(
        pr["status"], raw["put_rel_to_hr_body"])
    # C2: snapshot pom into handleSnapshots=false member — same expectation.
    st, _ = _put_pom_status(ctx, side, HS, "hws", SNAP)
    raw["put_snap_to_hs_status"] = st
    asserts["put_snap_to_hs_status"] = "status=%d" % st
    ps = ctx.http(side, "PUT", "/%s/%s/hws/%s/hws-%s.pom" % (HS, GP, SNAP, SNAP),
                  body=mavenlib.pom_fixture(GROUP, "hws", SNAP),
                  headers={"Content-Type": "application/xml"})
    raw["put_snap_to_hs_body"] = ps["body"][:300].decode("utf-8", "replace")
    asserts["put_snap_to_hs_family"] = _families(
        ps["status"], raw["put_snap_to_hs_body"])

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
    # DEFAULT member (r1: B 404s this spelling everywhere; the walk-face
    # snapshot leg above is handle-attributable only if these agree).
    st, _ = _put_pom_status(ctx, side, CTL, "plainsnap", SNAP)
    raw["put_plainsnap_to_ctl_status"] = st
    if st not in (200, 201):
        raise mavenlib.SetupError("plainsnap control PUT failed side %r: %d"
                                  % (side, st))

    # ---- settle member-level metadata calc (async, both sides) before the
    # virtual probes: member hwm module listing carries 1.0.0-SNAPSHOT, and
    # the control member hwc listing carries 1.0.0.
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

    # ---- member-face class-policy GET legs (no artifact needed: the class
    # gate is the question, not the bytes)
    g1 = _get(ctx, side, "/%s/%s/hwr/1.0.0/hwr-1.0.0.pom" % (HR, GP))
    raw["direct_get_relpath_hr_status"] = g1["status"]
    raw["direct_get_relpath_hr_body"] = g1["body"][:300].decode("utf-8", "replace")
    asserts["direct_get_relpath_hr_status"] = "status=%d" % g1["status"]
    asserts["direct_get_relpath_hr_family"] = _families(
        g1["status"], raw["direct_get_relpath_hr_body"])
    g2 = _get(ctx, side, "/%s/%s/hws/%s/hws-%s.pom" % (HS, GP, SNAP, SNAP))
    raw["direct_get_snappath_hs_status"] = g2["status"]
    raw["direct_get_snappath_hs_body"] = g2["body"][:300].decode("utf-8", "replace")
    asserts["direct_get_snappath_hs_status"] = "status=%d" % g2["status"]
    asserts["direct_get_snappath_hs_family"] = _families(
        g2["status"], raw["direct_get_snappath_hs_body"])

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
    # version-level (snapshot) metadata from the hr member: §5.1 skip keys
    # on handleSnapshots — hr keeps snapshots, so the member contributes.
    v3 = _get(ctx, side, "/%s/%s/hwm/%s/maven-metadata.xml" % (V, GP, SNAP))
    raw["virt_snapmeta_from_hr_status"] = v3["status"]
    asserts["virt_snapmeta_from_hr_status"] = "status=%d" % v3["status"]
    asserts["virt_snapmeta_from_hr_sv"] = (
        _sv_count(v3["body"]) if v3["status"] == 200 else "status=%d" % v3["status"])
    # module-level listing from the hr member (THE drift question): does the
    # handleReleases=false member's snapshot version survive the merge?
    v4 = _get(ctx, side, "/%s/%s/hwm/maven-metadata.xml" % (V, GP))
    raw["virt_modmeta_hwm_status"] = v4["status"]
    asserts["virt_modmeta_hwm_status"] = "status=%d" % v4["status"]
    asserts["virt_modmeta_hwm_versions"] = (
        _versions(v4["body"]) if v4["status"] == 200 else "status=%d" % v4["status"])
    # module-level listing for the control member's artifact (sanity).
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
        "note": "dual oracle: expected = live a per dimension; every "
                "a != b lands in ab_divergence and feeds the ruling "
                "(module-level handleReleases=false member skip = the "
                "ledger drift question; release-in-hr=false sub-face is "
                "NOT constructable — deploy-time 409 refusal on both "
                "sides, statuses recorded in put_rel_to_hr_* / "
                "put_snap_to_hs_*). Attribution rule (r1 finding): the "
                "walk-face snapshot leg virt_resolve_snap_from_hr is "
                "handle-attributable ONLY if control_virt_get_plainsnap "
                "agrees (b == a); B 404s the plain-SNAPSHOT spelling from "
                "a default member too — that spelling divergence is a "
                "separate, pre-existing face"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "create local hr=false/hs=false/ctl + "
                                  "virtual; wire-PUT 4 constructions; "
                                  "member/virtual GET resolution + "
                                  "module/version metadata probes"}]}
