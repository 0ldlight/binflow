#!/usr/bin/env python3
"""wave3e residue check — difftest namespace cleanliness + upstream GC ledger.

Checks:
  1. both repo lists contain NO run-unique test keys (diff-w3e-*, diff-l043d-*)
  2. upstream relay tag census for difftest-docker/smoke (DELETE is 405; the
     manual GC ledger accumulates wave tags: historical nc-*/w3-*/d1|d2l043d-*/
     tf* plus this wave's tw3e*/d1w3e*/d2w3e*)

Ticket marker: L044-wave3-expansion.
"""
import importlib.util
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "wave3e_lib", os.path.join(HERE, "..", "wave3e_lib.py"))
lib = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(lib)


def main():
    sides = lib.load_sides()
    facts = {"started": lib.epoch(), "repos": {}, "upstream": {}}

    test_markers = ("diff-w3e-", "diff-l043d-")
    for side_name in ("a", "b"):
        r = lib.http(sides[side_name], "GET", "%s/repositories" % sides[side_name]["api"])
        try:
            repos = json.loads(r["body"].decode("utf-8"))
            keys = [x.get("key") for x in repos if isinstance(x, dict)]
        except Exception:
            keys = None
        residue = sorted(k for k in (keys or []) if any(k.startswith(m) for m in test_markers))
        facts["repos"][side_name] = {"status": r["status"],
                                     "count": len(keys) if keys else None,
                                     "test_residue": residue}
        print("repos %s -> %s n=%s residue=%s" % (side_name, r["status"],
                                                  facts["repos"][side_name]["count"], residue))

    st, body = lib.upstream_request("GET", "/v2/%s/tags/list" % lib.IMAGE)
    try:
        tags = json.loads(body.decode("utf-8")).get("tags")
    except Exception:
        tags = None
    wave_tags = sorted(t for t in (tags or [])
                       if t.startswith(("tw3e", "d1w3e", "d2w3e", "tf1a",
                                        "d1l043d", "d2l043d")))
    facts["upstream"] = {"status": st,
                         "tag_count": len(tags) if isinstance(tags, list) else None,
                         "difftest_wave_tags": wave_tags}
    print("upstream tags -> %s n=%s; difftest wave tags in ledger: %s" % (
        st, facts["upstream"]["tag_count"], wave_tags))

    facts["finished"] = lib.epoch()
    path = lib.write_evidence("residue_check.json", facts)
    print("evidence: %s" % path)
    clean = all(not facts["repos"][s]["test_residue"] for s in ("a", "b"))
    print("REPO NAMESPACE: %s" % ("CLEAN" if clean else "RESIDUE FOUND"))
    return 0 if clean else 1


if __name__ == "__main__":
    sys.exit(main())
