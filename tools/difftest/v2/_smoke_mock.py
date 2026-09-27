#!/usr/bin/env python3
"""Dumb A/B mock endpoints for wiring smoke of difftest v2 cases.

NOT a differential oracle: it exists only to prove the runner -> case ->
subprocess/evidence plumbing when real instance credentials are unavailable
(T-523: A-side credentials pending). Cases run against it will (correctly)
record FAIL assertion values; their value is that every code path executes
end to end without crashing.

Usage: python3 _smoke_mock.py <port>   (serves 127.0.0.1:<port>)

Behaviour: GET /api/system/ping -> 200 "OK"; PUT/POST /api/repositories/* ->
200 {}; other PUT/POST -> 201; DELETE -> 200; any other GET -> 404 JSON
envelope. Stdlib only, threaded.
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _reply(self, status, payload=b"", ctype="application/json"):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(payload)

    def _drain(self):
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)

    def do_GET(self):
        if self.path == "/api/system/ping":
            self._reply(200, b"OK", "text/plain")
        else:
            self._reply(404, json.dumps(
                {"errors": [{"status": 404, "message": "mock miss"}]}).encode())

    def do_HEAD(self):
        self.do_GET()

    def do_PUT(self):
        self._drain()
        if self.path.startswith("/api/repositories/"):
            self._reply(200, b"{}")
        else:
            self._reply(201, b"")

    def do_POST(self):
        self._drain()
        self._reply(200, b"{}")

    def do_DELETE(self):
        self._reply(200, b"{}")

    def log_message(self, fmt, *args):  # keep smoke output quiet
        pass


if __name__ == "__main__":
    port = int(sys.argv[1])
    ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
