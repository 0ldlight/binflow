#!/usr/bin/env python3
"""L043 deep-dive probe 3 — bearer/Basic failure challenge cross-face matrix.

Candidate (known-divergence.yaml docker/bearer-failure-shape): status layer
SAME (401) but shape layer systematically divergent — A classified message +
Basic re-challenge vs B generic message + Bearer re-challenge; and B is
inconsistent ACROSS ITS OWN FACES (ping face: Props-classified + Basic
challenge; manifest face: generic + Bearer challenge).

This probe builds the full cross-face matrix on both sides:
  faces: F1 /v2/ ping | F2 remote manifest GET | F3 remote tags/list |
         F4 REST /api/system/ping
  arms:  anon | bad-basic (unique throwaway user per request) |
         garbage bearer | alg:none fake JWT | truncated real token |
         empty bearer

Breaker discipline (both sides run failure-count breakers keyed by username):
  - bad-basic uses a FRESH unique username per request (1 failure each, far
    below the trip threshold of either side)
  - anon arms run FIRST on every face so a later-armed anonymous principal
    cannot masquerade as shape evidence
  - any 403 observed is flagged "breaker-suspect" in evidence (A 403 body
    self-identifies with the recurrent-failure wording)

Credential discipline: real tokens are issued in memory, truncated to 40%,
and only length+sha256-prefix recorded; Authorization values never logged.

Ticket marker: L043-deepdive (probe 3 of 5).
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


def auth_headers(arm, side):
    """Authorization header for an arm; None -> anonymous."""
    if arm == "anon":
        return None
    if arm == "bad-basic":
        user = "l043d-bb-%s-%d" % (NONCE[-6:], int(time.time() * 1000) % 100000)
        cred = base64.b64encode(("%s:wrong-password" % user).encode()).decode()
        return {"Authorization": "Basic " + cred}
    if arm == "garbage-bearer":
        return {"Authorization": "Bearer l043d-garbage-" + NONCE}
    if arm == "algnone":
        jwt = ("eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0."
               "eyJzdWIiOiJsMDQzZC1wcm9iZSJ9." + NONCE)
        return {"Authorization": "Bearer " + jwt}
    if arm == "truncated-real":
        token = issue_token(side)
        if not token:
            return {"X-L043d-Skipped": "token-issue-failed"}
        cut = token[:max(1, len(token) * 2 // 5)]
        print("   [truncated-real] issued token %s -> using %d%% prefix %s" % (
            lib.token_fingerprint(token), len(cut) * 100 // len(token),
            lib.token_fingerprint(cut)))
        return {"Authorization": "Bearer " + cut}
    if arm == "empty-bearer":
        return {"Authorization": "Bearer"}
    raise ValueError(arm)


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


def main():
    sides = lib.load_sides()
    arms = ["anon", "bad-basic", "garbage-bearer", "algnone",
            "truncated-real", "empty-bearer"]
    facts = {"nonce": NONCE, "started": lib.epoch(),
             "matrix": {f: {} for f in ("F1-ping", "F2-manifest", "F3-tags", "F4-rest-ping")}}

    # pass 1: anon arms on every face (before any failure-count noise)
    for side_name in ("a", "b"):
        side = sides[side_name]
        for face, url in face_urls(side).items():
            headers = {"Accept": lib.ACCEPT_S2} if face == "F2-manifest" else {}
            r = lib.http({}, "GET", url, headers=headers)
            entry = lib.digest_resp(r, body_head=220)
            entry["breaker_suspect"] = (r["status"] == 403)
            facts["matrix"][face][side_name + ":anon"] = entry
            print("%s %s anon -> %s %s" % (side_name, face, r["status"],
                                           (entry["www_authenticate"] or "")[:80]))
        time.sleep(0.5)

    # pass 2: credentialed failure arms; throwaway users, small gaps
    for side_name in ("a", "b"):
        side = sides[side_name]
        for face, url in face_urls(side).items():
            for arm in arms[1:]:
                headers = {"Accept": lib.ACCEPT_S2} if face == "F2-manifest" else {}
                headers.update(auth_headers(arm, side) or {})
                r = lib.http({}, "GET", url, headers=headers)
                entry = lib.digest_resp(r, body_head=220)
                entry["breaker_suspect"] = (r["status"] == 403)
                facts["matrix"][face][side_name + ":" + arm] = entry
                print("%s %s %-15s -> %s %s %s" % (
                    side_name, face, arm, r["status"],
                    (entry["www_authenticate"] or "")[:60],
                    "BREAKER-SUSPECT" if entry["breaker_suspect"] else ""))
                time.sleep(1.0)
            time.sleep(4.5)  # let any armed window (4s) self-heal between faces

    facts["finished"] = lib.epoch()
    suspects = sum(1 for f in facts["matrix"].values() for e in f.values()
                   if e.get("breaker_suspect"))
    facts["breaker_suspect_count"] = suspects
    path = lib.write_evidence("p3_bearer_matrix.json", facts)
    print("breaker-suspect responses: %d" % suspects)
    print("evidence: %s" % path)
    return 0


if __name__ == "__main__":
    sys.exit(main())
