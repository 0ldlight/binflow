"""T-555 fix verification (ledger rest/nonempty-repo-delete-cascade-guard,
contract rest/repo-delete-nonempty-cascade; L028 D-3 / L032 Arm 1 lineage):
DELETE /api/repositories/{key} — the post-fix aligned posture.

A-side live (7.161.26, T-555 live probe 2026-09-29): non-empty repo DELETE
answers 200 with a JSON report body
{"repoKey","statusMsg","deletedArtifactsCount","success":true} — silent
cascade, wording by rclass (local/remote = "... and all its content have
been removed successfully.", virtual = plain "... has been removed
successfully."), count = files + folder sentinel rows (repo root NOT
counted), ?deleteContent=true accepted as a no-op synonym, empty repo
count=0. T-555 removed BinFlow's 400 confirmation gate; this case pins the
ALIGNED posture on BOTH sides (judge: static expected from the contract +
live-anchored counts).

Construction deliberately uses GENERIC repos with hand-shaped trees so the
count is constructable identical on both sides — the maven-layout count gap
(A auto-materializes version-level maven-metadata.xml on wire PUT, +1 per
artifact) is a separate registered face (maven/deploy-put-version-metadata-
auto-materialize, UNKNOWN) and must not pollute this arm.
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
    "id": "rest-nonempty-repo-delete-cascade",
    "title": "DELETE /api/repositories/{key} — aligned silent 200 cascade: "
             "report body shape, rclass wording, count semantics, flag "
             "synonym, empty and virtual arms",
    "layer": "L2",
    "domain": "rest",
    "auth": True,
    "timeout_s": 60,
}

TAG = "l034"
NESTED = "difftest-%s-nested" % TAG      # local generic, nested tree -> 6
FLAG = "difftest-%s-flag" % TAG          # local generic, ?deleteContent=true -> 3
EMPTY = "difftest-%s-empty" % TAG        # local generic, no content -> 0
VMEM = "difftest-%s-vmem" % TAG          # local generic WITH content (member)
VIRT = "difftest-%s-virt" % TAG          # virtual over VMEM -> 0, plain wording

CONTENT_MSG = "content"
PLAIN_MSG = "plain"


def _generic_local(key, **extra):
    payload = {"rclass": "local", "packageType": "generic"}
    payload.update(extra)
    return key, payload


def _put_file(ctx, side, repo, rel):
    return ctx.http(side, "PUT", "/%s/%s" % (repo, rel),
                    body=("difftest l034 payload :: %s/%s\n" % (repo, rel)).encode())


def _delete_report(ctx, side, key, query=""):
    """DELETE the repo and grade the report body.

    Returns (asserts, raw) with asserts shaped so a/b compare field-level:
      del_<tag>_status / _count / _wording / _repokey_ok / _success /
      _keys_ok (the four contract keys all present)
    """
    asserts, raw = {}, {}
    d = ctx.http(side, "DELETE", "/api/repositories/%s%s" % (key, query))
    raw["status"] = d["status"]
    raw["body"] = d["body"].decode("utf-8", "replace")
    raw["content_type"] = next(
        (v for k, v in d["headers"].items() if k.lower() == "content-type"), "")
    try:
        doc = json.loads(raw["body"])
    except ValueError:
        doc = None
    if d["status"] != 200 or not isinstance(doc, dict):
        return None, raw  # caller records the broken posture verbatim
    msg = doc.get("statusMsg") or ""
    has = doc.get("repoKey") == key
    wording = (CONTENT_MSG if " and all its content have been removed" in msg
               else PLAIN_MSG if " has been removed successfully" in msg
               else "other:%s" % msg[:120])
    count = doc.get("deletedArtifactsCount")
    keys_ok = all(k in doc for k in
                  ("repoKey", "statusMsg", "deletedArtifactsCount", "success"))
    asserts["del_status"] = "%d" % d["status"]
    asserts["del_count"] = "%d" % count if isinstance(count, int) else "absent"
    asserts["del_wording"] = wording
    asserts["del_repokey_ok"] = "ok" if has else "mismatch"
    asserts["del_success"] = "true" if doc.get("success") is True else "other"
    asserts["del_keys_ok"] = "ok" if keys_ok else "missing-keys"
    raw["extra_keys"] = sorted(set(doc) - {
        "repoKey", "statusMsg", "deletedArtifactsCount", "success"})
    raw["json_body"] = True
    return asserts, raw


def _leg(ctx, side):
    out, rawall = {}, {}
    mavenlib.ensure_repos(ctx, side, [
        _generic_local(NESTED), _generic_local(FLAG), _generic_local(EMPTY),
        _generic_local(VMEM),
    ])
    # nested tree: a/b/c/f1.bin + a/b/f2.bin + a/f3.bin -> 3 files + 3 folders = 6
    for rel in ("a/b/c/f1.bin", "a/b/f2.bin", "a/f3.bin"):
        r = _put_file(ctx, side, NESTED, rel)
        if r["status"] not in (200, 201):
            raise mavenlib.SetupError("nested PUT %s -> %d" % (rel, r["status"]))
    # flag tree: x/y/f.bin -> 1 file + 2 folders = 3
    r = _put_file(ctx, side, FLAG, "x/y/f.bin")
    if r["status"] not in (200, 201):
        raise mavenlib.SetupError("flag PUT -> %d" % r["status"])
    # virtual member carries content, then virtual on top
    r = _put_file(ctx, side, VMEM, "m/n/member.bin")
    if r["status"] not in (200, 201):
        raise mavenlib.SetupError("vmem PUT -> %d" % r["status"])
    mavenlib.repo_put(ctx, side, *mavenlib.maven_virtual(
        VIRT, [VMEM], packageType="generic"))

    # ---- arm 1: nested, no param
    a1, raw = _delete_report(ctx, side, NESTED)
    rawall["nested"] = raw
    if a1 is None:
        raise mavenlib.SetupError("nested DELETE broken on %r: %s" % (
            side, raw.get("body", "")[:200]))
    for k, v in a1.items():
        out["nested_" + k] = v
    g = ctx.http(side, "GET", "/api/repositories/" + NESTED)
    out["nested_repo_get_after"] = "%d" % g["status"]

    # ---- arm 2: ?deleteContent=true synonym (same shape as arm 1)
    a2, raw = _delete_report(ctx, side, FLAG, query="?deleteContent=true")
    rawall["flag"] = raw
    if a2 is None:
        raise mavenlib.SetupError("flag DELETE broken on %r: %s" % (
            side, raw.get("body", "")[:200]))
    for k, v in a2.items():
        out["flag_" + k] = v

    # ---- arm 3: empty repo -> count 0
    a3, raw = _delete_report(ctx, side, EMPTY)
    rawall["empty"] = raw
    if a3 is None:
        raise mavenlib.SetupError("empty DELETE broken on %r: %s" % (
            side, raw.get("body", "")[:200]))
    for k, v in a3.items():
        out["empty_" + k] = v

    # ---- arm 4: virtual with live member -> count 0, plain wording,
    # member survives
    a4, raw = _delete_report(ctx, side, VIRT)
    rawall["virt"] = raw
    if a4 is None:
        raise mavenlib.SetupError("virt DELETE broken on %r: %s" % (
            side, raw.get("body", "")[:200]))
    for k, v in a4.items():
        out["virt_" + k] = v
    m = ctx.http(side, "GET", "/api/repositories/" + VMEM)
    out["virt_member_survives"] = "%d" % m["status"]

    ctx.write_evidence("%s-leg.json" % side, {"asserts": out, "raw": rawall})
    return out


# static expected from the contract + T-555 live-anchored counts; graded on
# BOTH sides (the aligned posture, not a per-side pin).
EXPECTED = {
    "nested_del_status": "200", "nested_del_count": "6",
    "nested_del_wording": CONTENT_MSG, "nested_del_repokey_ok": "ok",
    "nested_del_success": "true", "nested_del_keys_ok": "ok",
    "nested_repo_get_after": "400",
    "flag_del_status": "200", "flag_del_count": "3",
    "flag_del_wording": CONTENT_MSG, "flag_del_repokey_ok": "ok",
    "flag_del_success": "true", "flag_del_keys_ok": "ok",
    "empty_del_status": "200", "empty_del_count": "0",
    "empty_del_wording": CONTENT_MSG, "empty_del_repokey_ok": "ok",
    "empty_del_success": "true", "empty_del_keys_ok": "ok",
    "virt_del_status": "200", "virt_del_count": "0",
    "virt_del_wording": PLAIN_MSG, "virt_del_repokey_ok": "ok",
    "virt_del_success": "true", "virt_del_keys_ok": "ok",
    "virt_member_survives": "200",
}


def run(ctx):
    per_side = {}
    keys = [NESTED, FLAG, EMPTY, VIRT, VMEM]
    try:
        for side in ("a", "b"):
            per_side[side] = _leg(ctx, side)
    except mavenlib.SetupError as e:
        return {"status": "BLOCKED", "reason": "setup: %s" % e}
    finally:
        for s in ("a", "b"):
            mavenlib.cleanup_repos(ctx, s, keys)

    status, reason, mism = mavenlib.judge(per_side, EXPECTED)
    ev = ctx.write_evidence("summary.json", {
        "expected": EXPECTED, "a": per_side.get("a"), "b": per_side.get("b"),
        "mismatches": mism,
        "note": "aligned posture (T-555): 200 silent cascade + JSON report "
                "body; count = files+folders, root excluded (generic-tree "
                "construction keeps the maven auto-materialize face out); "
                "wording by rclass; flag synonym; empty=0; virtual=0+plain "
                "with member surviving"})
    return {"status": status, "reason": reason,
            "evidence": [ev, "evidence/%s/a-leg.json" % CASE["id"],
                         "evidence/%s/b-leg.json" % CASE["id"]],
            "requests": [{"step": "generic trees (nested/flag/empty/virt+"
                                  "member); DELETE x4 (one with flag); GET "
                                  "after; member survival"}]}
