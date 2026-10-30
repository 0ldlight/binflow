#!/usr/bin/env python3
"""wave3e probe 3 — bearer/Basic failure challenge: full 24-cell A-side matrix
+ dual re-verification on the current deployment (task 2 of the wave-3
expansion brief; L043 candidate 1).

Background: B (BinFlow) is internally cross-face INCONSISTENT on 401 shape —
its /v2/ ping face answers Basic challenge + classified copy while its
manifest/content faces answer a Bearer challenge + generic anonymous envelope.
The deep-dive (2026-10-29, B=dev-cfa5da05) captured 4 faces x 6 arms on both
sides; this probe re-captures the COMPLETE grid on the current deployment
(B=dev-fdeabbf2, first auth-face differential after the T-632 auth merge),
completing the fresh A-side 24-cell grid (4 faces x 6 arms) alongside B's.

  faces: F1-ping GET /v2/ | F2-manifest GET remote manifest (Accept schema2)
         F3-tags  GET remote tags/list | F4-rest-ping GET {api}/system/ping
  arms:  anon | bad-basic (unique one-shot user per cell) | garbage-bearer
         | alg:none fake JWT | truncated-real token | empty bearer

Breaker discipline: anon arms first on every face; one-shot usernames (one
failure each); 1s gaps; 4.5s silent windows between faces; any 403 flagged
breaker-suspect (A trip copy self-identifies "recurrent request failures").

Derived per side: challenge family per face for credentialed failures
(A: Basic everywhere incl. manifest face; B: Bearer on content faces),
classification copy class per cell, cross-face consistency sets.

Credential discipline: real tokens issued in memory, truncated to 40%, only
length+sha256-prefix recorded; Authorization values never logged.

Ticket marker: L044-wave3-expansion (probe 3).
"""
import base64
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

NONCE = lib.NONCE


def hostport(side):
    return side["docker"].split("//", 1)[1]


def face_urls(side):
    return {
        "F1-ping": side["docker"] + "/v2/",
        "F2-manifest": "%s/v2/%s/%s/manifests/t1" % (side["docker"], lib.REPO, lib.IMAGE),
        "F3-tags": "%s/v2/%s/%s/tags/list" % (side["docker"], lib.REPO, lib.IMAGE),
        "F4-rest-ping": side["api"] + "/system/ping",
    }


def issue_token(side):
    q = ("service=%s&scope=repository:%s/%s:pull&account=%s" % (
        hostport(side), lib.REPO, lib.IMAGE, side["user"]))
    r = lib.http(side, "GET", "%s/v2/token?%s" % (side["docker"], q))
    try:
        doc = json.loads(r["body"].decode("utf-8"))
        tok = doc.get("token") or doc.get("access_token")
        if r["status"] == 200 and tok:
            return tok
    except Exception:
        pass
    print("   [token issue failed] status=%s" % r["status"])
    return None


def auth_headers(arm, side):
    if arm == "anon":
        return None
    if arm == "bad-basic":
        user = "w3e-bb-%s-%d" % (NONCE[-6:], int(time.time() * 1000) % 100000)
        cred = base64.b64encode(("%s:wrong-password" % user).encode()).decode()
        return {"Authorization": "Basic " + cred}
    if arm == "garbage-bearer":
        return {"Authorization": "Bearer w3e-garbage-" + NONCE}
    if arm == "algnone":
        jwt = ("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0."
               "eyJzdWIiOiJ3M2UtcHJvYmUifQ." + NONCE)
        return {"Authorization": "Bearer " + jwt}
    if arm == "truncated-real":
        token = issue_token(side)
        if not token:
            return {"X-W3e-Skipped": "token-issue-failed"}
        cut = token[:max(1, len(token) * 2 // 5)]
        print("   [truncated-real] %s -> %d%% prefix %s" % (
            lib.token_fingerprint(token), len(cut) * 100 // len(token),
            lib.token_fingerprint(cut)))
        return {"Authorization": "Bearer " + cut}
    if arm == "empty-bearer":
        return {"Authorization": "Bearer"}
    raise ValueError(arm)


def classification(body_head):
    if "Bad Credentials" in body_head:
        return "bad-credentials"
    if "Props Authentication Token" in body_head:
        return "props-token-not-found"
    if "authentication required" in body_head:
        return "generic-anonymous-envelope"
    if "recurrent request failures" in body_head:
        return "breaker-trip"
    if "Forbidden" in body_head:
        return "forbidden"
    if "invalid credentials" in body_head.lower():
        return "invalid-credentials-rest"
    return "other"


def run_cell(side_name, side, face, url, arm, extra_headers):
    headers = dict(extra_headers or {})
    ah = auth_headers(arm, side)
    if ah is None:
        r = lib.anon_http("GET", url, headers=headers)
    else:
        headers.update(ah)
        r = lib.http({}, "GET", url, headers=headers)
    entry = lib.digest_resp(r, body_head=220)
    entry["challenge_family"] = lib.challenge_family(r)
    entry["classification"] = classification(entry["body_head"])
    entry["breaker_suspect"] = (r["status"] == 403)
    print("%s %-13s %-15s -> %s %-6s %-28s %s" % (
        side_name, face, arm, r["status"],
        entry["challenge_family"] or "-", entry["classification"],
        "BREAKER-SUSPECT" if entry["breaker_suspect"] else ""))
    return entry


def main():
    sides = lib.load_sides()
    arms = ["anon", "bad-basic", "garbage-bearer", "algnone",
            "truncated-real", "empty-bearer"]
    facts = {"nonce": NONCE, "started": lib.epoch(),
             "matrix": {f: {} for f in ("F1-ping", "F2-manifest", "F3-tags",
                                        "F4-rest-ping")}}

    # pass 1: anon arms on every face, both sides (before failure-count noise)
    for side_name in ("a", "b"):
        side = sides[side_name]
        for face, url in face_urls(side).items():
            extra = {"Accept": lib.ACCEPT_S2} if face == "F2-manifest" else {}
            facts["matrix"][face][side_name + ":anon"] = run_cell(
                side_name, side, face, url, "anon", extra)
        time.sleep(0.5)

    # pass 2: credentialed failure arms
    for side_name in ("a", "b"):
        side = sides[side_name]
        for face, url in face_urls(side).items():
            for arm in arms[1:]:
                extra = {"Accept": lib.ACCEPT_S2} if face == "F2-manifest" else {}
                facts["matrix"][face][side_name + ":" + arm] = run_cell(
                    side_name, side, face, url, arm, extra)
                time.sleep(1.0)
            time.sleep(4.5)

    # derived: per-side per-face challenge family for credentialed failures
    derived = {}
    cred_arms = arms[1:]
    for side_name in ("a", "b"):
        per_face = {}
        for face in facts["matrix"]:
            fams = sorted({(facts["matrix"][face][side_name + ":" + arm].get("challenge_family") or "none")
                           for arm in cred_arms
                           if facts["matrix"][face][side_name + ":" + arm].get("status") == 401})
            per_face[face] = fams
        derived[side_name + "_credentialed_challenge_by_face"] = per_face
    facts["derived"] = derived
    print("derived A:", derived.get("a_credentialed_challenge_by_face"))
    print("derived B:", derived.get("b_credentialed_challenge_by_face"))

    suspects = sum(1 for f in facts["matrix"].values() for e in f.values()
                   if e.get("breaker_suspect"))
    facts["breaker_suspect_count"] = suspects
    facts["finished"] = lib.epoch()
    path = lib.write_evidence("p3_bearer_matrix.json", facts)
    print("breaker-suspect responses: %d" % suspects)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
