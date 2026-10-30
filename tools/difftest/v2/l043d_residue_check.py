#!/usr/bin/env python3
"""L043 deep-dive — residue check after the probe session.

Verifies on BOTH sides:
  - REST repository list contains no l043d/diff-catalog test repo keys
  - (upstream relay residue is DELETE-405 by design -> printed for the GC
    ledger, not an error)

Ticket marker: L043-deepdive (cleanup verification).
"""
import importlib.util
import json
import os
import sys

_spec = importlib.util.spec_from_file_location(
    "l043d_lib", os.path.join(os.path.dirname(os.path.abspath(__file__)), "l043d_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)

PATTERNS = ("l043d", "diff-catalog-", "difftest-empty-")


def main():
    sides = lib.load_sides()
    residue = {}
    ok = True
    for name in ("a", "b"):
        r = lib.http(sides[name], "GET", "%s/repositories" % sides[name]["api"])
        try:
            keys = [x.get("key") for x in json.loads(r["body"].decode("utf-8"))]
        except Exception:
            print("%s: repository list unreadable (status %s)" % (name, r["status"]))
            ok = False
            continue
        hits = [k for k in keys if k and any(p in k for p in PATTERNS)]
        residue[name] = hits
        print("%s: %d repos, test-residue hits: %s" % (name, len(keys), hits or "none"))
        ok = ok and not hits

    # upstream residue: report-only (DELETE is 405 on the relay)
    st, body = lib.upstream_request("GET", "/v2/%s/tags/list" % lib.IMAGE)
    try:
        tags = json.loads(body.decode("utf-8")).get("tags") or []
    except Exception:
        tags = []
    gc = [t for t in tags if t.startswith(("nc", "w3", "d1l043d", "d2l043d", "tf"))]
    print("upstream %s tags=%d; GC-ledger entries (DELETE 405): %s" % (
        lib.IMAGE, len(tags), gc))
    lib.write_evidence("residue_check.json", {
        "checked": lib.epoch(), "repo_residue": residue,
        "upstream_gc_ledger": gc})
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
