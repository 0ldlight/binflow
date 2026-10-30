#!/usr/bin/env python3
"""wave3e summarizer — five-candidate verdict table from evidence JSONs.

Applies the deep-dive (2026-10-29) expectations to this wave's evidence and
prints REPRODUCED / CHANGED per candidate + per-arm rows.

Deep-dive expectations (A=7.161.26 / B=dev-cfa5da05):
  cat: A root empty-body zero-CT invariant x4 topologies; B remote-member JSON;
       anon root A 401 / B 200; anon local tags A 401 / B 404
  sch: A per-request negotiation (s1/no-Accept -> prettyjws, cache transparent);
       B cold-negotiate-once (hot rows ignore Accept); C-b pollution (D2 B=prettyjws)
  brc: A all-credentialed Basic+classified; B F1 Basic+classified,
       F2/F3 Bearer+generic, F4 Basic BinFlow Realm; anon A Bearer / B F1 Bearer
       F2-F3 200
  ttl: workload-dependency experiment (new evidence, no deep-dive expectation)
  406: A 406 <=> Accept present without application/json and */*; B 200 all

Ticket marker: L044-wave3-expansion.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
EV = os.path.join(HERE, "run", "wave3-expansion")

S1_PRETTYJWS_CT = "application/vnd.docker.distribution.manifest.v1+prettyjws"
S2_CT = "application/vnd.docker.distribution.manifest.v2+json"


def load(name):
    path = os.path.join(EV, name)
    if not os.path.exists(path):
        return None
    with open(path) as fh:
        return json.load(fh)


def manifest_repr(entry):
    """'s2' | 's1-prettyjws' | 'other' from CT + shape."""
    ct = (entry.get("content_type") or "").split(";")[0].strip()
    if ct == S1_PRETTYJWS_CT:
        return "s1-prettyjws"
    if ct == S2_CT:
        return "s2"
    shape = entry.get("shape") or {}
    mt = shape.get("media_type")
    if mt == 1 or mt == "1":
        return "s1-prettyjws"
    if isinstance(mt, str) and "manifest.v2" in mt:
        return "s2"
    if isinstance(mt, str) and "manifest.v1" in mt:
        return "s1-prettyjws"
    return "other:%s/%s" % (ct or "?", mt)


def summarize_catalog(f):
    if not f:
        return ["catalog: EVIDENCE MISSING"], {}
    arms = f["arms"]
    rows, checks = [], {}
    a_kind = arms["T1a-baseline"]["a"]["body_kind"]
    b_kind = arms["T1a-baseline"]["b"]["body_kind"]
    checks["A_root_empty_invariant"] = (
        a_kind == "empty-body"
        and arms["T1b-paginated-n1000"]["a"]["body_kind"] == "empty-body"
        and arms["T3c-root-with-local"]["a"]["body_kind"] == "empty-body")
    checks["B_root_remote_members"] = (b_kind == "repositories-json")
    checks["B_local_not_listed"] = arms["T3c-root-with-local"]["b"]["has_new_local_repo"] is False
    checks["repo_scoped_members_identical"] = arms["T2-members-identical"]
    checks["anon_root_A401_B200"] = (
        arms["T4-anonymous-root"]["a"]["status"] == 401
        and arms["T4-anonymous-root"]["b"]["status"] == 200)
    checks["anon_local_tags_A401_B404"] = (
        arms["T5-anon-local-tags"]["a"]["status"] == 401
        and arms["T5-anon-local-tags"]["b"]["status"] == 404)
    rows.append("catalog root: A=%s/%s B=%s/%s n=%s" % (
        arms["T1a-baseline"]["a"]["status"], a_kind,
        arms["T1a-baseline"]["b"]["status"], b_kind,
        arms["T1a-baseline"]["b"]["member_count"]))
    rows.append("anon root: A=%s B=%s | anon local tags: A=%s B=%s" % (
        arms["T4-anonymous-root"]["a"]["status"], arms["T4-anonymous-root"]["b"]["status"],
        arms["T5-anon-local-tags"]["a"]["status"], arms["T5-anon-local-tags"]["b"]["status"]))
    return rows, checks


SCH_EXPECT = {
    "P0-prime-s2": ("s2", "s2"),
    "H1-t1-s1": ("s1-prettyjws", "s2"),
    "H2-t1-s2": ("s2", "s2"),
    "H2b-t1-s2-again": ("s2", "s2"),
    "H3-t1-noacc": ("s1-prettyjws", "s2"),
    "H4-t1-mixed": ("s2", "s2"),
    "D1-cold-s1": ("s1-prettyjws", "s1-prettyjws"),
    "D2-tag1-s2": ("s2", "s1-prettyjws"),
    "D3-tag1-noacc": ("s1-prettyjws", "s1-prettyjws"),
    "E1-cold-s2": ("s2", "s2"),
    "E2-tag2-s1": ("s1-prettyjws", "s2"),
    "E3-tag2-noacc": ("s1-prettyjws", "s2"),
}


def summarize_schema1(f):
    if not f:
        return ["schema1: EVIDENCE MISSING"], {}
    rows, checks = [], {}
    for name, (ea, eb) in SCH_EXPECT.items():
        st = f["steps"].get(name)
        if not st:
            checks[name] = "MISSING"
            continue
        ra, rb = manifest_repr(st["a"]), manifest_repr(st["b"])
        ok = (ra == ea and rb == eb)
        checks[name] = "OK" if ok else "CHANGED(a=%s exp=%s b=%s exp=%s)" % (ra, ea, rb, eb)
        rows.append("%-16s A=%-12s B=%-12s %s" % (
            name, ra, rb, "rep" if ok else "CHANGED"))
    return rows, checks


def summarize_bearer(f):
    if not f:
        return ["bearer: EVIDENCE MISSING"], {}
    rows, checks = [], {}
    m = f["matrix"]
    cred_arms = ["bad-basic", "garbage-bearer", "algnone", "truncated-real", "empty-bearer"]

    def cell(side, face, arm):
        return m[face].get("%s:%s" % (side, arm), {})

    def fam(side, face, arm):
        return cell(side, face, arm).get("challenge_family") or "none"

    checks["A_credentialed_basic_all_faces"] = all(
        fam("a", face, arm) == "basic"
        for face in m for arm in cred_arms
        if cell("a", face, arm).get("status") == 401)
    checks["B_F1_credentialed_basic"] = all(
        fam("b", "F1-ping", arm) == "basic" for arm in cred_arms
        if cell("b", "F1-ping", arm).get("status") == 401)
    checks["B_F2F3_credentialed_bearer"] = all(
        fam("b", face, arm) == "bearer"
        for face in ("F2-manifest", "F3-tags") for arm in cred_arms
        if cell("b", face, arm).get("status") == 401)
    checks["B_F4_realm_BinFlow"] = all(
        "BinFlow Realm" in (cell("b", "F4-rest-ping", arm).get("www_authenticate") or "")
        for arm in cred_arms if cell("b", "F4-rest-ping", arm).get("status") == 401)
    checks["A_Artifactory_Realm"] = all(
        "Artifactory Realm" in (cell("a", "F1-ping", arm).get("www_authenticate") or "")
        for arm in cred_arms if cell("a", "F1-ping", arm).get("status") == 401)
    checks["anon_A_bearer_F123"] = all(
        fam("a", face, "anon") == "bearer" for face in ("F1-ping", "F2-manifest", "F3-tags"))
    checks["anon_B_F1_bearer_F2F3_200"] = (
        fam("b", "F1-ping", "anon") == "bearer"
        and cell("b", "F2-manifest", "anon").get("status") == 200
        and cell("b", "F3-tags", "anon").get("status") == 200)
    for face in m:
        for side in ("a", "b"):
            anon = cell(side, face, "anon")
            rows.append("%s %-13s anon -> %s %-6s %-28s" % (
                side, face, anon.get("status"), anon.get("challenge_family") or "-",
                anon.get("classification")))
    return rows, checks


def summarize_accept406(f):
    if not f:
        return ["accept406: EVIDENCE MISSING"], {}
    rows, checks = [], {}
    rule = f.get("a_rule_406") or {}
    checks["A_406_rule_holds"] = bool(rule.get("holds"))
    checks["B_all_200"] = all(arm["sides"]["b"]["status"] == 200
                              for arm in f["arms"].values())
    for name, arm in f["arms"].items():
        rows.append("%-16s A=%s(%s) B=%s(%s)" % (
            name, arm["sides"]["a"]["status"], arm["sides"]["a"]["body_kind"],
            arm["sides"]["b"]["status"], arm["sides"]["b"]["body_kind"]))
    return rows, checks


def summarize_ttl(f):
    if not f:
        return ["ttl-workload: EVIDENCE MISSING"], {}
    rows = []
    state = f.get("state")
    rows.append("state=%s tag=%s" % (state, f.get("tag")))
    rows.append("B live immediate=%s upstream push=%s" % (
        f.get("b_live_immediate"), f.get("upstream_push_status")))
    flip = None
    for st in f.get("steps", []):
        if st.get("a", {}).get("has_tag"):
            flip = st
            break
    if flip:
        rows.append("FIRST FLIP at +%ds by %s (pull=%s) -> workload_dependent_refresh=%s" % (
            flip["elapsed_s"], flip["kind"], flip.get("pull"),
            f.get("workload_dependent_refresh")))
    else:
        rows.append("no in-window flip: pure reads and manifest workload arms all stale")
    for st in f["steps"]:
        if st["kind"].startswith("workload") or st["kind"].startswith("pure"):
            rows.append("  +%4ds %-30s A n=%s has_tag=%s %s" % (
                st["elapsed_s"], st["kind"], st["a"].get("tag_count"),
                st["a"].get("has_tag"),
                ("pull=%s->%s" % (st.get("pull"), st.get("pull_resp", {}).get("status"))
                 if st.get("pull") else "")))
    rows.append("first_seen=+%s ttl_bound=%s" % (
        f.get("first_seen_elapsed_s"), f.get("ttl_bound_seconds")))
    return rows, {"b_live_aggregation": bool(f.get("b_live_immediate")),
                  "window_closed": state in ("window-closed",
                                             "window-closed-earlier-by-workload",
                                             "flipped-in-window-workload")}


def main():
    print("== wave3e five-candidate verdict summary ==")
    out = {}

    name, f = "catalog", load("p1_catalog_topology.json")
    rows, checks = summarize_catalog(f)
    out[name] = {"rows": rows, "checks": checks}
    print("\n-- candidate 3 catalog --")
    for r in rows:
        print(" ", r)
    for k, v in checks.items():
        print("  check %-32s %s" % (k, v))

    name, f = "schema1", load("p2_schema1_phase.json")
    rows, checks = summarize_schema1(f)
    out[name] = {"rows": rows, "checks": checks}
    print("\n-- candidate 2 schema1 --")
    for r in rows:
        print(" ", r)

    name, f = "bearer", load("p3_bearer_matrix.json")
    rows, checks = summarize_bearer(f)
    out[name] = {"rows": rows, "checks": checks}
    print("\n-- candidate 1 bearer cross-face --")
    for r in rows:
        print(" ", r)
    for k, v in checks.items():
        print("  check %-32s %s" % (k, v))

    name, f = "accept406", load("p5_tags_accept_matrix.json")
    rows, checks = summarize_accept406(f)
    out[name] = {"rows": rows, "checks": checks}
    print("\n-- candidate face 2 Accept 406 --")
    for r in rows:
        print(" ", r)
    for k, v in checks.items():
        print("  check %-32s %s" % (k, v))

    name, f = "ttl-workload", load("p4_ttl_workload.json")
    rows, checks = summarize_ttl(f)
    out[name] = {"rows": rows, "checks": checks}
    print("\n-- candidate face 1 TTL workload dependence --")
    for r in rows:
        print(" ", r)

    path = os.path.join(EV, "summary.json")
    with open(path, "w") as fh:
        json.dump(out, fh, ensure_ascii=False, indent=2)
    print("\nsummary: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
