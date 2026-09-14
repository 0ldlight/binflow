#!/usr/bin/env python3
"""L018 conan wire-capture proxy (differential-qa-engineer).

Forwards every HTTP request to $UPSTREAM verbatim (method/path/headers/body),
records the exchange to $LOGDIR/<leg>.wire: request line + headers (Authorization
redacted) + body sha256/size (+ full body when text and <=16KiB), response
status + headers + body sha256/size (+ full body under same bound).

Stdlib only. Usage: wireproxy.py <port> <logdir> <leg>   (UPSTREAM env required)
"""
import hashlib
import http.client
import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UPSTREAM = os.environ["UPSTREAM"]  # host:port
LOGDIR, LEG = sys.argv[2], sys.argv[3]
LOGPATH = os.path.join(LOGDIR, LEG + ".wire")
TEXT_CT = ("text/", "application/json", "application/x-www-form-urlencoded")
DROP_H = {"host", "content-length", "connection", "accept-encoding", "proxy-connection"}
lock = threading.Lock()


def _fingerprint(body: bytes) -> str:
    return f"{len(body)}B sha256={hashlib.sha256(body).hexdigest()[:16]}"


def _maybe_full(body: bytes, ct: str) -> str:
    if len(body) <= 16384 and (ct.startswith(TEXT_CT) or not ct):
        try:
            return "\n" + body.decode("utf-8")
        except UnicodeDecodeError:
            pass
    return ""


class Proxy(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):  # silence stderr noise
        pass

    def _relay(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length) if length else b""
        req_h = {k: v for k, v in self.headers.items() if k.lower() not in DROP_H}
        host, port = UPSTREAM.split(":")
        conn = http.client.HTTPConnection(host, int(port), timeout=60)
        try:
            conn.request(self.command, self.path, body=body, headers=req_h)
            resp = conn.getresponse()
            rbody = resp.read()
        finally:
            conn.close()
        rh = {k: v for k, v in resp.getheaders() if k.lower() not in {"connection", "transfer-encoding", "content-length"}}
        self.send_response(resp.status)
        for k, v in rh.items():
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(rbody)))
        self.end_headers()
        self.wfile.write(rbody)

        def redact(h):
            return {k: ("REDACTED" if k.lower() == "authorization" else v) for k, v in h.items()}

        entry = {
            "request": f"{self.command} {self.path}",
            "req_headers": redact(req_h),
            "req_body": _fingerprint(body) + _maybe_full(body, self.headers.get("Content-Type", "")),
            "response": resp.status,
            "resp_headers": rh,
            "resp_body": _fingerprint(rbody) + _maybe_full(rbody, resp.getheader("Content-Type", "")),
        }
        with lock:
            with open(LOGPATH, "a") as f:
                f.write(json.dumps(entry, indent=1) + "\n----\n")

    do_GET = do_PUT = do_POST = do_DELETE = do_HEAD = _relay


if __name__ == "__main__":
    os.makedirs(LOGDIR, exist_ok=True)
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1])), Proxy).serve_forever()
