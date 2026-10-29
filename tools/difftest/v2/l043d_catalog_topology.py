#!/usr/bin/env python3
"""L043 deep-dive probe 1 — registry-level /v2/_catalog multi-topology + anonymous.

Candidate (known-divergence.yaml docker/registry-catalog-bare-root):
  A = 200 EMPTY body zero Content-Type vs B = 200 JSON {"repositories":[...]}.
  review_gate asks for A-side multi-topology evidence before adjudication:
    - local docker repo present: does A's root catalog list it?
    - anonymous stance on the root face
    - repo-scoped catalog on the remote repo face (is the emptiness a
      remote-filtering rule or a degenerate root face?)

Arms (writes only to run-unique test repos, deleted + verified at the end):
  0. topology capture via REST (both sides)
  1. baseline auth GET /v2/_catalog  (+ ?n=1000 pagination arm)
  2. repo-scoped auth GET /v2/{remote-repo}/_catalog
  3. create run-unique empty local docker repo on both sides (settle 3s)
  4. auth GET /v2/_catalog -> membership + shape (local repo in catalog?)
  5. anonymous GET /v2/_catalog
  6. anonymous GET /v2/{new-local}/tags/list
  7. cleanup DELETE both repos, verify gone

Ticket marker: L043-deepdive (probe 1 of 5).
"""
import base64
import importlib.util
import json
import os
import sys
import time

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

REPO_KEY = "diff-l043d-" + lib.NONCE
EMPTY_IMAGE = "difftest-empty-" + lib.NONCE


def anon_http(side, method, url, headers=None, body=None):
    req = urllib_request_no_redirect(method, url, headers, body)
    return req


def urllib_request_no_redirect(method, url, headers, body):
    import urllib.error
    import urllib.request
    req = urllib.request.Request(url, data=body, method=method)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    opener = urllib.request.build_opener(lib._NoRedirect)
    try:
        with opener.open(req, timeout=30.0) as resp:
            return {"status": resp.status, "headers": dict(resp.headers),
                    "body": resp.read()}
    except urllib.error.HTTPError as e:
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return {"status": e.code, "headers": dict(e.headers or {}), "body": raw}
    except OSError as e:
        return {"status": -1, "headers": {}, "body": str(e).encode()}


def catalog_members(r):
    try:
        doc = json.loads(r["body"].decode("utf-8"))
        if isinstance(doc, dict) and isinstance(doc.get("repositories"), list):
            return doc["repositories"]
    except Exception:
        pass
    return None


def main():
    sides = lib.load_sides()
    facts = {"repo_key": REPO_KEY, "started": lib.epoch(), "arms": {}}

    # arm 0: topology capture
    topo = {}
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET", "%s/repositories" % sides[side_name]["api"])
        try:
            repos = json.loads(r["body"].decode("utf-8"))
            topo[side_name] = {"status": r["status"],
                               "count": len(repos) if isinstance(repos, list) else None,
                               "keys": [x.get("key") for x in repos][:40]
                               if isinstance(repos, list) else None}
        except Exception:
            topo[side_name] = {"status": r["status"],
                               "body_head": r["body"][:200].decode("utf-8", "replace")}
    facts["arms"]["topology"] = topo
    print("arm0 topology A=%s n=%s / B=%s n=%s" % (
        topo["a"]["status"], topo["a"].get("count"),
        topo["b"]["status"], topo["b"].get("count")))

    # arm 1: baseline auth catalog (+ pagination arm)
    for label, suffix in (("baseline", ""), ("paginated", "?n=1000")):
        per = {}
        for side_name in ("a", "b"):
            r = lib.http(sides[side_name], "GET",
                         "%s/v2/_catalog%s" % (sides[side_name]["docker"], suffix))
            per[side_name] = lib.digest_resp(r, body_head=160)
            per[side_name]["members"] = catalog_members(r)
        facts["arms"]["catalog_auth_" + label] = per
        print("arm1[%s] A=%s ct=%s len=%s members=%s | B=%s ct=%s n=%s" % (
            label, per["a"]["status"], per["a"]["content_type"], per["a"]["body_len"],
            (len(per["a"]["members"]) if isinstance(per["a"]["members"], list) else per["a"]["members"]),
            per["b"]["status"], per["b"]["content_type"],
            (len(per["b"]["members"]) if isinstance(per["b"]["members"], list) else per["b"]["members"])))

    # arm 2: repo-scoped catalog on the remote repo face
    per = {}
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET",
                     "%s/v2/%s/_catalog" % (sides[side_name]["docker"], lib.REPO))
        per[side_name] = lib.digest_resp(r, body_head=240)
        per[side_name]["members"] = catalog_members(r)
    facts["arms"]["catalog_repo_scoped_remote"] = per
    print("arm2 repo-scoped /v2/%s/_catalog A=%s n=%s | B=%s n=%s" % (
        lib.REPO, per["a"]["status"],
        len(per["a"]["members"]) if isinstance(per["a"]["members"], list) else None,
        per["b"]["status"],
        len(per["b"]["members"]) if isinstance(per["b"]["members"], list) else None))

    # arm 3: create empty local docker repos on both sides
    created = {}
    for side_name in ("a", "b"):
        body = json.dumps({"key": REPO_KEY, "rclass": "local",
                           "packageType": "docker"}).encode()
        r = lib.http(sides[side_name], "PUT",
                     "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY),
                     body=body, headers={"Content-Type": "application/json"})
        created[side_name] = r["status"]
    facts["arms"]["create_repo_status"] = created
    print("arm3 create %s A=%s B=%s" % (REPO_KEY, created["a"], created["b"]))
    if not all(200 <= s < 300 for s in created.values()):
        for side_name in ("a", "b"):
            dr = lib.http(sides[side_name], "DELETE",
                          "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY))
            facts["arms"].setdefault("cleanup", {})[side_name] = dr["status"]
        lib.write_evidence("p1_catalog_topology.json", facts)
        print("create failed on a side; aborted (cleanup attempted)")
        return 1
    time.sleep(3)  # settle for any indexing lag

    # arm 4: auth catalog with a local repo present -> membership + shape
    per = {}
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET", "%s/v2/_catalog" % sides[side_name]["docker"])
        members = catalog_members(r)
        per[side_name] = lib.digest_resp(r, body_head=200)
        per[side_name]["has_new_local_repo"] = bool(members and REPO_KEY in members)
        per[side_name]["member_count"] = len(members) if isinstance(members, list) else None
    facts["arms"]["catalog_auth_with_local"] = per
    print("arm4 with-local A=%s has_new=%s n=%s | B=%s has_new=%s n=%s" % (
        per["a"]["status"], per["a"]["has_new_local_repo"], per["a"]["member_count"],
        per["b"]["status"], per["b"]["has_new_local_repo"], per["b"]["member_count"]))

    # arm 5: anonymous catalog
    per = {}
    for side_name in ("a", "b"):
        r = urllib_request_no_redirect("GET", "%s/v2/_catalog" % sides[side_name]["docker"],
                                       None, None)
        entry = lib.digest_resp(r, body_head=200)
        entry["members"] = catalog_members(r)
        per[side_name] = entry
    facts["arms"]["catalog_anonymous"] = per
    print("arm5 anon A=%s ct=%s len=%s | B=%s ct=%s len=%s" % (
        per["a"]["status"], per["a"]["content_type"], per["a"]["body_len"],
        per["b"]["status"], per["b"]["content_type"], per["b"]["body_len"]))

    # arm 6: anonymous tags/list on the new empty local repo
    per = {}
    for side_name in ("a", "b"):
        r = urllib_request_no_redirect(
            "GET", "%s/v2/%s/%s/tags/list" % (sides[side_name]["docker"],
                                              REPO_KEY, EMPTY_IMAGE), None, None)
        per[side_name] = lib.digest_resp(r, body_head=200)
    facts["arms"]["anon_local_tags"] = per
    print("arm6 anon local tags A=%s | B=%s" % (
        per["a"]["status"], per["b"]["status"]))

    # arm 7: cleanup
    cleanup = {}
    for side_name in ("a", "b"):
        dr = lib.http(sides[side_name], "DELETE",
                      "%s/repositories/%s" % (sides[side_name]["api"], REPO_KEY))
        cleanup[side_name] = dr["status"]
    facts["arms"]["cleanup"] = cleanup
    verify = {}
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET", "%s/repositories/%s" % (
            sides[side_name]["api"], REPO_KEY))
        verify[side_name] = r["status"]
    facts["arms"]["cleanup_verify_status"] = verify
    print("arm7 cleanup DELETE=%s verify(get expect 404)=%s" % (cleanup, verify))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p1_catalog_topology.json", facts)
    print("evidence: %s" % path)
    ok = all(200 <= s < 300 for s in cleanup.values()) and \
        all(s == 404 for s in verify.values())
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
