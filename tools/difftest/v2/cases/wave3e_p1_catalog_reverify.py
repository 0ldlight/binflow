#!/usr/bin/env python3
"""wave3e probe 1 — catalog bare-root: four-topology + anonymous re-verification.

Re-verifies L043 candidate 3 conclusion (established on B=dev-cfa5da05,
deep-dive 2026-10-29) on the CURRENT deployment (B=dev-fdeabbf2):
  A root /v2/_catalog = 200 EMPTY body, zero Content-Type, invariant across
  topologies; B root = 200 JSON remote-member aggregation; new local repos
  enter NEITHER side's root face; anonymous root: A 401 vs B 200 (C-a).

Arms (writes only to a run-unique local repo, deleted + verified at the end):
  T1 baseline auth root catalog (fixture remote present) + ?n=1000 pagination
  T2 repo-scoped /v2/{remote}/_catalog (membership parity re-check)
  T3 create run-unique empty local docker repo both sides (settle 3s)
     -> auth root catalog with-local (membership + shape)
  T4 anonymous root catalog
  T5 anonymous tags/list on the new local repo face
  cleanup DELETE + verify gone

Ticket marker: L044-wave3-expansion (probe 1).
"""
import importlib.util
import json
import os
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

REPO_KEY = "diff-w3e-" + lib.NONCE
EMPTY_IMAGE = "difftest-empty-" + lib.NONCE


def catalog_members(r):
    try:
        doc = json.loads(r["body"].decode("utf-8"))
        if isinstance(doc, dict) and isinstance(doc.get("repositories"), list):
            return doc["repositories"]
    except Exception:
        pass
    return None


def catalog_cell(sides, side_name, url):
    r = lib.http(sides[side_name], "GET", url)
    entry = lib.digest_resp(r, body_head=200)
    members = catalog_members(r)
    entry["members"] = members
    entry["member_count"] = len(members) if isinstance(members, list) else None
    entry["body_kind"] = ("empty-body" if entry["body_len"] == 0
                          else ("repositories-json" if isinstance(members, list)
                                else "other-json"))
    return entry


def main():
    sides = lib.load_sides()
    facts = {"repo_key": REPO_KEY, "started": lib.epoch(), "arms": {}}

    def root(side_name, suffix=""):
        return "%s/v2/_catalog%s" % (sides[side_name]["docker"], suffix)

    # T1: baseline auth root catalog + pagination arm
    for label, suffix in (("T1a-baseline", ""), ("T1b-paginated-n1000", "?n=1000")):
        per = {}
        for side_name in ("a", "b"):
            per[side_name] = catalog_cell(sides, side_name, root(side_name, suffix))
        facts["arms"][label] = per
        print("%s A=%s kind=%s n=%s | B=%s kind=%s n=%s" % (
            label, per["a"]["status"], per["a"]["body_kind"], per["a"]["member_count"],
            per["b"]["status"], per["b"]["body_kind"], per["b"]["member_count"]))

    # T2: repo-scoped catalog on the remote repo face
    per = {}
    for side_name in ("a", "b"):
        per[side_name] = catalog_cell(
            sides, side_name, "%s/v2/%s/_catalog" % (sides[side_name]["docker"], lib.REPO))
        per[side_name]["members_sorted"] = sorted(per[side_name]["members"]) \
            if isinstance(per[side_name]["members"], list) else None
    same_members = (per["a"]["members_sorted"] is not None
                    and per["a"]["members_sorted"] == per["b"]["members_sorted"])
    facts["arms"]["T2-repo-scoped-remote"] = per
    facts["arms"]["T2-members-identical"] = same_members
    print("T2 repo-scoped A=%s n=%s | B=%s n=%s | members identical=%s" % (
        per["a"]["status"], per["a"]["member_count"],
        per["b"]["status"], per["b"]["member_count"], same_members))

    # T3: create empty local docker repos both sides
    created = {}
    for side_name in ("a", "b"):
        body = json.dumps({"key": REPO_KEY, "rclass": "local",
                           "packageType": "docker"}).encode()
        r = lib.http(sides[side_name], "PUT",
                     "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY),
                     body=body, headers={"Content-Type": "application/json"})
        created[side_name] = r["status"]
    facts["arms"]["T3-create-local-status"] = created
    print("T3 create %s A=%s B=%s" % (REPO_KEY, created["a"], created["b"]))
    if not all(200 <= s < 300 for s in created.values()):
        cleanup = {}
        for side_name in ("a", "b"):
            dr = lib.http(sides[side_name], "DELETE",
                          "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY))
            cleanup[side_name] = dr["status"]
        facts["arms"]["cleanup-on-failure"] = cleanup
        lib.write_evidence("p1_catalog_topology.json", facts)
        print("create failed on a side; cleanup attempted")
        return 1
    time.sleep(3)

    per = {}
    for side_name in ("a", "b"):
        per[side_name] = catalog_cell(sides, side_name, root(side_name))
        members = per[side_name]["members"]
        per[side_name]["has_new_local_repo"] = bool(members and REPO_KEY in members)
    facts["arms"]["T3c-root-with-local"] = per
    print("T3c with-local A=%s kind=%s has_new=%s | B=%s kind=%s has_new=%s n=%s" % (
        per["a"]["status"], per["a"]["body_kind"], per["a"]["has_new_local_repo"],
        per["b"]["status"], per["b"]["body_kind"], per["b"]["has_new_local_repo"],
        per["b"]["member_count"]))

    # T4: anonymous root catalog
    per = {}
    for side_name in ("a", "b"):
        r = lib.anon_http("GET", root(side_name))
        entry = lib.digest_resp(r, body_head=200)
        entry["members"] = catalog_members(r)
        entry["member_count"] = len(entry["members"]) if isinstance(entry["members"], list) else None
        per[side_name] = entry
    facts["arms"]["T4-anonymous-root"] = per
    print("T4 anon root A=%s ct=%s len=%s | B=%s len=%s n=%s" % (
        per["a"]["status"], per["a"]["content_type"], per["a"]["body_len"],
        per["b"]["status"], per["b"]["body_len"], per["b"]["member_count"]))

    # T5: anonymous tags/list on the new local repo face
    per = {}
    for side_name in ("a", "b"):
        r = lib.anon_http("GET", "%s/v2/%s/%s/tags/list" % (
            sides[side_name]["docker"], REPO_KEY, EMPTY_IMAGE))
        per[side_name] = lib.digest_resp(r, body_head=200)
    facts["arms"]["T5-anon-local-tags"] = per
    print("T5 anon local tags A=%s | B=%s" % (per["a"]["status"], per["b"]["status"]))

    # cleanup + verify
    cleanup, verify = {}, {}
    for side_name in ("a", "b"):
        dr = lib.http(sides[side_name], "DELETE",
                      "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY))
        cleanup[side_name] = dr["status"]
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET",
                     "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY))
        verify[side_name] = r["status"]
    facts["arms"]["cleanup"] = cleanup
    facts["arms"]["cleanup_verify_404"] = verify
    print("cleanup DELETE=%s verify(expect 404)=%s" % (cleanup, verify))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p1_catalog_topology.json", facts)
    print("evidence: %s" % path)
    return 0 if all(200 <= s < 300 for s in cleanup.values()) \
        and all(s == 404 for s in verify.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
