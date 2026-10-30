#!/usr/bin/env python3
"""L043 deep-dive probe 0 — re-provision the docker-remote fixture.

Environment fact found on 2026-10-29: the wave-2 fixture remote repo
`diff-docker-remote` is GONE on both sides (A 404 "Repository ... not found",
B 404 NAME_UNKNOWN) — torn down between waves. This script re-creates it
(idempotent PUT) with the wave-2 topology:
  - A: remote repo diff-docker-remote -> upstream :32100, storeArtifactsLocally
  - B: same + allowPrivateUpstream=true (BinFlow refuses private-HTTP
    pull-through without it — wave-1 config-face divergence, known)
then verifies the fixture is live on both sides (manifest GET t1, schema2
Accept) and warms both remote caches once.

No deletes; only the known fixture repo key is touched.

Ticket marker: L043-deepdive (probe 0 of 5).
"""
import importlib.util
import json
import os
import sys

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

REPO_KEY = "diff-docker-remote"
UPSTREAM = "http://192.168.120.36:32100"


def main():
    sides = lib.load_sides()
    facts = {"repo": REPO_KEY, "upstream": UPSTREAM, "steps": {}}

    for name in ("a", "b"):
        cfg = {"key": REPO_KEY, "rclass": "remote", "packageType": "docker",
               "url": UPSTREAM, "storeArtifactsLocally": True}
        if name == "b":
            cfg["allowPrivateUpstream"] = True
        r = lib.http(sides[name], "PUT",
                     "%s/repositories/%s" % (sides[name]["api"], REPO_KEY),
                     body=json.dumps(cfg).encode(),
                     headers={"Content-Type": "application/json"})
        facts["steps"]["create_" + name] = {
            "status": r["status"],
            "body_head": r["body"][:160].decode("utf-8", "replace")}
        print("create %s -> %s %s" % (name, r["status"],
                                      r["body"][:80].decode("utf-8", "replace")))

    # liveness + warm: manifest GET t1 both sides (schema2 Accept)
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET",
                     "%s/v2/%s/%s/manifests/t1" % (sides[name]["docker"],
                                                   lib.REPO, lib.IMAGE),
                     headers={"Accept": lib.ACCEPT_S2})
        entry = lib.digest_resp(r, body_head=80)
        entry["shape"] = lib.manifest_shape(r)
        facts["steps"]["warm_" + name] = entry
        print("warm %s t1 -> %s ct=%s len=%s" % (
            name, r["status"], entry["content_type"], entry["body_len"]))

    # tags/list baseline both sides (needed by later probes; records live set)
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET",
                     "%s/v2/%s/%s/tags/list" % (sides[name]["docker"],
                                                lib.REPO, lib.IMAGE))
        entry = lib.digest_resp(r, body_head=240)
        facts["steps"]["tags_" + name] = entry
        print("tags %s -> %s %s" % (name, r["status"],
                                    entry["body_head"][:160]))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p0_refixture.json", facts)
    print("evidence: %s" % path)
    warm = (facts["steps"]["warm_a"]["status"], facts["steps"]["warm_b"]["status"])
    return 0 if all(s == 200 for s in warm) else 1


if __name__ == "__main__":
    sys.exit(main())
