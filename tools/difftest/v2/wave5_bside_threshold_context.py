#!/usr/bin/env python3
"""wave5 standalone — B-side breaker threshold context check (3 sends).

Context fact for the wave5 report header: the briefing describes B as
"dev-cfa5da05+阈值2" while the live version endpoint reports dev-aceee772.
One nonexistent username, THREE bad-Basic sends to the B REST ping, >=1.15s
apart (L043 wave-2 methodology): threshold-2 bind-next renders 401,401,403;
threshold-5 renders 401,401,401. Zero persistence (username does not exist;
A/B ledgers self-heal). A-side N=3 is established twice (L043 wave2, L044
wave3 task-1) and is NOT re-measured here.

Evidence: run/l043-probe/bside_threshold_context.json. Exit code always 0.
"""
import base64
import json
import os
import time
import urllib.error
import urllib.request

B_BASE = os.environ["B_BASE"].rstrip("/")
B_AUTH = base64.b64encode(("%s:%s" % (os.environ["B_USER"],
                                      os.environ["B_PASSWORD"])).encode()).decode()
USER = "w5thr" + format(int(time.time() * 1000), "x")


def bad_send():
    token = base64.b64encode(("%s:difftest-wrongpass-w5" % USER).encode()).decode()
    req = urllib.request.Request(B_BASE + "/binflow/api/system/ping")
    req.add_header("Authorization", "Basic " + token)
    t0 = time.time()
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            st, body = r.status, r.read()
    except urllib.error.HTTPError as e:
        st, body = e.code, e.read()
    except Exception as e:
        st, body = None, repr(e).encode()
    dt = int((time.time() - t0) * 1000)
    text = body.decode("utf-8", "replace")
    cd = None
    for frag in text.replace("(", " ").replace(")", " ").split():
        if frag.isdigit():
            cd = int(frag)
            break
    return {"status": st, "elapsed_ms": dt, "countdown_candidate": cd,
            "recurrent_wording": "recurrent" in text.lower(),
            "body_head": text[:200]}


def main():
    rows = []
    for i in range(1, 4):
        rows.append(bad_send())
        if i < 3:
            time.sleep(1.15)
    out = {"user_prefix": USER, "sends": rows,
           "status_sequence": [r["status"] for r in rows],
           "reading": ("threshold-2 live (3rd send 403)" if rows[2]["status"] == 403
                       else "threshold > 2 (3rd send still %s)" % rows[2]["status"])}
    print(json.dumps(out["status_sequence"]), out["reading"], flush=True)
    here = os.path.dirname(os.path.abspath(__file__))
    d = os.path.join(here, "run", "l043-probe")
    os.makedirs(d, exist_ok=True)
    with open(os.path.join(d, "bside_threshold_context.json"), "w", encoding="utf-8") as fh:
        json.dump(out, fh, ensure_ascii=False, indent=1)


if __name__ == "__main__":
    main()
