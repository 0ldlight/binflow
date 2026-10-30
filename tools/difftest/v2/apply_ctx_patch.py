#!/usr/bin/env python3
"""One-shot transformer (2026-10-29): insert the ctx-prefix patch call as the
first statement of every target case's run(ctx). Idempotent: skips files that
already carry the patch line."""
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
CASES = os.path.join(HERE, "cases")

MAVENLIB_FILES = [
    "maven_auto_materialize_probe.py", "maven_deploy_201_location_header.py",
    "maven_deploy_created_envelope.py", "maven_deploy_gav_mismatch.py",
    "maven_local_handle_walk_skip.py", "maven_member_sidecar_forensics.py",
    "maven_module_handle_seat.py", "maven_plainsnap_nonunique_resolve.py",
    "maven_resolve_remote_cache.py", "maven_sidecar_crosssec_stamp.py",
    "maven_snapshot_versions_merge.py",
    "maven_unique_plain_resolve_forensics.py",
    "maven_virtual_delete_passthrough.py", "maven_virtual_deploy.py",
    "maven_virtual_metadata_merge.py", "maven_virtual_walk_selection.py",
    "maven_walk_sameface_mvn.py", "maven_walk_selection_tiebreak.py",
    "maven_walk_trigger_matrix.py", "rest_kcache_deploy_wording.py",
    "rest_nonempty_repo_delete.py", "conan_v1_packages_delete.py",
    "generic_mime_ownership.py",
]
NPMLIB_FILES = [
    "npm_cache_member_layout.py", "npm_cli_tarball_fidelity.py",
    "npm_virtual_aggregate_delete_entry.py", "npm_virtual_packument_merge.py",
]

RUN_RE = re.compile(r"^(def run\(ctx\):)\n", re.M)


def transform(fname, lib):
    path = os.path.join(CASES, fname)
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    patch_line = "    %s.patch_ctx(ctx)\n" % lib
    if patch_line in text:
        return "skip (already patched)"
    m = RUN_RE.search(text)
    if not m:
        return "ERROR: no def run(ctx):"
    end = m.end()
    # keep any immediately-following global declarations first
    while True:
        gm = re.match(r"    global [^\n]+\n", text[end:])
        if not gm:
            break
        end += gm.end()
    text = text[:end] + patch_line + text[end:]
    with open(path, "w", encoding="utf-8") as fh:
        fh.write(text)
    return "patched"


def main():
    bad = 0
    for f in MAVENLIB_FILES:
        r = transform(f, "mavenlib")
        print("%-45s %s" % (f, r))
        bad += r.startswith("ERROR")
    for f in NPMLIB_FILES:
        r = transform(f, "npmlib")
        print("%-45s %s" % (f, r))
        bad += r.startswith("ERROR")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
