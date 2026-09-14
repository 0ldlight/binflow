#!/usr/bin/env python3
"""l0121 wiretap — one-shot logging reverse proxy for npm CLI wire forensics.

LOOP 012 / L012-1 (differential-qa-engineer): runs a command (typically the
real npm CLI) against a local listener that forwards every request verbatim
to the real target and logs the full request/response pair. One foreground
process per case — no daemons, no background jobs.

Usage:
  wiretap.py --target http://localhost:8082 --port 19082 --log case.log -- cmd...

Log format (per exchange):
  === REQUEST <n>
  <METHOD> <path-with-query>
  <header lines, credentials redacted>
  body(<n> bytes): <utf-8 text, or sha256 if binary/oversized>
  --- RESPONSE
  HTTP <status> <reason>
  <header lines, credentials redacted>
  body(<n> bytes): <utf-8 text, or sha256 if binary/oversized>

Credentials never reach the log: Authorization/Cookie values are replaced
with <redacted>. The proxy can inject Basic auth (--auth-env VAR) read from
an environment variable so creds stay out of argv and disk.
"""
import argparse
import datetime
import hashlib
import http.client
import http.server
import os
import subprocess
import sys
import threading

REDACT_HEADERS = {"authorization", "proxy-authorization", "cookie"}
HOP_HEADERS = {"connection", "keep-alive", "proxy-authenticate",
               "proxy-authorization", "te", "trailers",
               "transfer-encoding", "upgrade"}
TEXT_LIMIT = 16384

log_lock = threading.Lock()
log_file = None
auth_header = None  # injected Authorization value, or None
counter = [0]


def log(line):
    with log_lock:
        log_file.write(line + "\n")
        log_file.flush()


def body_repr(raw):
    if not raw:
        return "(empty)"
    try:
        text = raw.decode("utf-8")
        printable = all(ch == "\n" or ch == "\t" or ord(ch) >= 32 for ch in text)
    except UnicodeDecodeError:
        printable = False
    if printable and len(raw) <= TEXT_LIMIT:
        return text
    return "<binary/oversized: %d bytes, sha256=%s>" % (
        len(raw), hashlib.sha256(raw).hexdigest())


def relay_headers(headers, inject_auth):
    out = []
    for k, v in headers:
        lk = k.lower()
        if lk in HOP_HEADERS:
            continue
        if lk == "authorization" and inject_auth:
            continue  # replaced by the injected Basic below (fake client token)
        if lk in REDACT_HEADERS:
            v = "<redacted:%s>" % lk
        out.append((k, v))
    if inject_auth:
        out.append(("Authorization", auth_header))
    return out


class TapHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    target_host, target_port = "localhost", 80

    def log_message(self, fmt, *args):  # silence stderr noise
        pass

    def _relay(self):
        length = int(self.headers.get("Content-Length") or 0)
        req_body = self.rfile.read(length) if length else b""
        counter[0] += 1
        n = counter[0]
        path = self.path

        fwd = relay_headers(self.headers.items(), inject_auth=auth_header is not None)

        conn = http.client.HTTPConnection(self.target_host, self.target_port, timeout=60)
        try:
            conn.putrequest(self.command, path, skip_host=True, skip_accept_encoding=True)
            for k, v in fwd:
                conn.putheader(k, v)
            if req_body or self.command in ("PUT", "POST"):
                conn.endheaders(message_body=req_body)
            else:
                conn.endheaders()
            resp = conn.getresponse()
            resp_body = resp.read()
            status, reason = resp.status, resp.reason
            resp_heads = [(k, v) for k, v in resp.getheaders()
                          if k.lower() not in HOP_HEADERS
                          and k.lower() != "content-length"]
        except Exception as exc:  # forward-layer failure: report on the wire
            log("=== REQUEST %d" % n)
            log("%s %s" % (self.command, path))
            log("--- RESPONSE (proxy error: %s)" % exc)
            self.send_error(502, "wiretap upstream error")
            return
        finally:
            conn.close()

        log("=== REQUEST %d  [%s]" % (n, datetime.datetime.now().isoformat(timespec="milliseconds")))
        log("%s %s" % (self.command, path))
        for k, v in relay_headers(self.headers.items(), inject_auth=False):
            log("%s: %s" % (k, v))
        if auth_header is not None:
            log("Authorization: <client creds redacted; upstream sent injected Basic>")
        log("body(%d bytes): %s" % (len(req_body), body_repr(req_body)))
        log("--- RESPONSE")
        log("HTTP %d %s" % (status, reason))
        for k, v in resp_heads:
            log("%s: %s" % (k, "<redacted:set-cookie>" if k.lower() == "set-cookie" else v))
        log("body(%d bytes): %s" % (len(resp_body), body_repr(resp_body)))
        log("")

        self.send_response(status, reason)
        for k, v in resp_heads:
            self.send_header(k, v)
        self.send_header("Content-Length", str(len(resp_body)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(resp_body)

    do_GET = do_PUT = do_POST = do_DELETE = do_HEAD = do_OPTIONS = _relay


class TapServer(http.server.ThreadingHTTPServer):
    daemon_threads = True


def main():
    global log_file, auth_header
    ap = argparse.ArgumentParser()
    ap.add_argument("--target", required=True, help="http://host:port to forward to")
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--log", required=True)
    ap.add_argument("--auth-env", help="env var holding 'user:password' for Basic injection")
    ap.add_argument("cmd", nargs=argparse.REMAINDER)
    args, cmd = ap.parse_known_args()
    cmd = [c for c in args.cmd if c != "--"]
    if not cmd:
        ap.error("no command given")

    if args.auth_env and os.environ.get(args.auth_env):
        import base64
        raw = os.environ[args.auth_env].encode()
        auth_header = "Basic " + base64.b64encode(raw).decode()

    host, port = args.target.split("://", 1)[1].split(":")
    TapHandler.target_host, TapHandler.target_port = host, int(port)
    log_file = open(args.log, "w")

    srv = TapServer(("127.0.0.1", args.port), TapHandler)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()

    env = {k: v for k, v in os.environ.items()
           if not k.lower().endswith("_proxy") and k.lower() != "no_proxy"}
    rc = subprocess.call(cmd, env=env, cwd=os.environ.get("TAP_CWD") or None)
    srv.shutdown()
    srv.server_close()
    log_file.close()
    sys.exit(rc)


if __name__ == "__main__":
    main()
