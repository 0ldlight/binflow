#!/usr/bin/env python3
"""BinFlow difftest v2 runner — dual-send differential driver (stdlib only).

Contract (full details in README.md):
  - Case discovery: every *.py under cases/ (files starting with "_" skipped)
    defines either a declarative CASE dict or a programmatic run(ctx) hook.
  - Dual send: the same request goes to side A (Artifactory reference) and
    side B (BinFlow); responses are compared after normalization.
  - Credentials: endpoints and credentials enter ONLY via environment
    variables A_BASE / B_BASE / A_USER / A_PASSWORD / B_USER / B_PASSWORD.
    This file (and every case file) contains zero literal URLs or secrets.
  - Normalize registry: normalize rules apply only when the case's domain
    block in docs/compatibility/fixtures/normalize.yaml carries
    `status: registered`. Anything else -> raw comparison, marked in output.
  - Four states: PASS / FAIL / BLOCKED (environment or precondition
    unavailable) / NOT_RUN (never executed). skip != PASS.
  - Timeouts: default fixed below; per-case `timeout_s` or --timeout may
    tighten or widen explicitly. Never auto-relaxed.

Runner exit codes: 0 = run completed (case states live in results.json,
including FAIL); 2 = usage or discovery error (bad case file, unknown --case).
"""
from __future__ import annotations

import argparse
import base64
import difflib
import hashlib
import importlib.util
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

SCHEMA = "difftest/v2"
PASS, FAIL, BLOCKED, NOT_RUN = "PASS", "FAIL", "BLOCKED", "NOT_RUN"

ENV_BASE = {"a": "A_BASE", "b": "B_BASE"}
ENV_USER = {"a": "A_USER", "b": "B_USER"}
ENV_PASSWORD = {"a": "A_PASSWORD", "b": "B_PASSWORD"}
ALL_ENV_KEYS = ("A_BASE", "B_BASE", "A_USER", "A_PASSWORD", "B_USER", "B_PASSWORD")

DEFAULT_TIMEOUT_S = 30.0  # seconds; fixed default, recorded in every results.json

# Executable normalize vocabulary. A rule is applied to a case only when the
# case requests it AND the case's domain is registered (status: registered)
# in normalize.yaml. Rule ids and semantics are proposed by
# differential-qa-engineer, registered by compatibility-engineer.
VOLATILE_HEADERS = frozenset({
    "date", "age", "via", "server", "set-cookie", "connection",
    "transfer-encoding", "content-length", "x-request-id",
    "x-artifactory-id", "x-artifactory-node-id", "x-jfrog-version",
    "x-powered-by", "x-content-type-options",
})
NORMALIZE_RULES = ("drop-volatile-headers", "base-url-placeholder")

HTTP_METHODS = frozenset({"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"})


class TransportError(Exception):
    """Endpoint unreachable / transport-level failure -> case BLOCKED."""


# ---------------------------------------------------------------- registry

def scan_registered_domains(normalize_path):
    """Line-oriented scan of normalize.yaml -> (registered_domain_set, note).

    A domain counts as registered only when its block contains a
    `status: registered` line. ponytail: line-oriented parsing assumes the
    machine-authored 2-space style of normalize.yaml; swap for a real YAML
    parser only if third-party deps are ever allowed here.
    """
    if not os.path.isfile(normalize_path):
        return set(), "normalize file not found: %s" % normalize_path
    registered, current = set(), None
    with open(normalize_path, encoding="utf-8") as fh:
        for line in fh:
            m = re.match(r"^  ([A-Za-z0-9_-]+):\s*(?:#.*)?$", line)
            if m:  # domain key directly under `domains:`
                current = m.group(1)
                continue
            m = re.match(r"^\s+status:\s*registered\s*(?:#.*)?$", line)
            if m and current:
                registered.add(current)
    return registered, None


# ----------------------------------------------------------------- loading

def load_case_module(path):
    stem = os.path.splitext(os.path.basename(path))[0]
    spec = importlib.util.spec_from_file_location("difftest_case_" + stem, path)
    if spec is None or spec.loader is None:
        raise ValueError("cannot import case module")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def validate_case(case):
    if not isinstance(case, dict):
        return "CASE must be a dict"
    ident = case.get("id")
    if not ident or not re.fullmatch(r"[a-z0-9][a-z0-9-]*", ident):
        return "case.id must be a lowercase slug, got: %r" % (ident,)
    reqs = case.get("requests")
    if not isinstance(reqs, list) or not reqs:
        return "case.requests must be a non-empty list"
    for i, r in enumerate(reqs):
        if not isinstance(r, dict):
            return "requests[%d] must be a dict" % i
        if r.get("method", "GET").upper() not in HTTP_METHODS:
            return "requests[%d].method invalid: %r" % (i, r.get("method"))
        if not isinstance(r.get("path", ""), str) or not r["path"].startswith("/"):
            return "requests[%d].path must start with '/'" % i
    body_mode = case.get("compare", {}).get("body", "literal")
    if body_mode not in ("literal", "json", "none"):
        return "compare.body must be literal|json|none, got %r" % body_mode
    for rule in case.get("normalize", []):
        if rule not in NORMALIZE_RULES:
            return "unknown normalize rule %r (vocabulary: %s)" % (rule, list(NORMALIZE_RULES))
    return None


# ------------------------------------------------------------- environment

def env_state():
    return {k: ("set" if os.environ.get(k) else "MISSING") for k in ALL_ENV_KEYS}


def side_config(name):
    base = os.environ.get(ENV_BASE[name], "")
    return {
        "name": name,
        "base": base.rstrip("/"),
        "user": os.environ.get(ENV_USER[name], ""),
        "password": os.environ.get(ENV_PASSWORD[name], ""),
    }


def preflight_missing(case):
    """Env keys this case needs but that are absent -> BLOCKED."""
    missing = [k for k in (ENV_BASE["a"], ENV_BASE["b"]) if not os.environ.get(k)]
    if case.get("auth"):
        missing += [k for k in (ENV_USER["a"], ENV_PASSWORD["a"],
                                ENV_USER["b"], ENV_PASSWORD["b"])
                    if not os.environ.get(k)]
    return missing


# ----------------------------------------------------------------- http leg

def http_request(side, method, path, body, headers, timeout_s):
    url = side["base"] + path
    data = body.encode() if isinstance(body, str) else body
    req = urllib.request.Request(url, data=data, method=method.upper())
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    if side["user"] and side["password"]:
        token = base64.b64encode(
            ("%s:%s" % (side["user"], side["password"])).encode()).decode()
        req.add_header("Authorization", "Basic " + token)
    try:
        with urllib.request.urlopen(req, timeout=timeout_s) as resp:
            return {"status": resp.status, "headers": dict(resp.headers),
                    "body": resp.read()}
    except urllib.error.HTTPError as e:  # a real response, not an error state
        raw = b""
        try:
            raw = e.read()
        except Exception:
            pass
        return {"status": e.code, "headers": dict(e.headers or {}), "body": raw}
    except (urllib.error.URLError, TimeoutError, OSError) as e:
        raise TransportError("%s endpoint unreachable: %r" % (side["name"], e))


def redact_headers(headers):
    out = {}
    for k, v in headers.items():
        out[k] = "<redacted>" if k.lower() == "authorization" else v
    return out


# ------------------------------------------------------------- normalize

def normalize_headers(headers, rules):
    out = {}
    for k, v in headers.items():
        if "drop-volatile-headers" in rules and k.lower() in VOLATILE_HEADERS:
            continue
        out[k] = v
    return out


def normalize_text(text, rules, sides):
    if "base-url-placeholder" not in rules:
        return text
    for side in sides:
        if side["base"]:
            text = text.replace(side["base"], "<BASE>")
    return text


def canonical_json(text):
    """Parse and re-serialize with sorted keys; raises on invalid JSON."""
    return json.dumps(json.loads(text), sort_keys=True,
                      separators=(",", ":"), ensure_ascii=False)


# ----------------------------------------------------------------- compare

def fingerprint(resp):
    return {
        "status": resp["status"],
        "body_len": len(resp["body"]),
        "body_sha256": hashlib.sha256(resp["body"]).hexdigest(),
    }


def compare_responses(a_resp, b_resp, compare_cfg, rules, sides):
    """Compare A vs B on the declared dimensions. Returns (diff, all_same)."""
    diff, same = {}, True

    if compare_cfg.get("status", True):
        ok = a_resp["status"] == b_resp["status"]
        diff["status"] = {"a": a_resp["status"], "b": b_resp["status"], "same": ok}
        same = same and ok

    whitelist = [h.lower() for h in compare_cfg.get("headers", [])]
    if whitelist:
        ah = {k.lower(): v for k, v in normalize_headers(a_resp["headers"], rules).items()}
        bh = {k.lower(): v for k, v in normalize_headers(b_resp["headers"], rules).items()}
        value_diff, only_a, only_b = {}, [], []
        for h in whitelist:
            if h in ah and h in bh:
                if ah[h] != bh[h]:
                    value_diff[h] = {"a": ah[h], "b": bh[h]}
            elif h in ah:
                only_a.append(h)
            elif h in bh:
                only_b.append(h)
        h_same = not value_diff and not only_a and not only_b
        diff["headers"] = {"compared": whitelist, "same": h_same,
                           "value_diff": value_diff, "only_a": only_a, "only_b": only_b}
        same = same and h_same

    mode = compare_cfg.get("body", "literal")
    if mode != "none":
        a_text = normalize_text(a_resp["body"].decode("utf-8", "replace"), rules, sides)
        b_text = normalize_text(b_resp["body"].decode("utf-8", "replace"), rules, sides)
        body_diff = {"mode": mode, "same": True}
        if mode == "json":
            try:
                a_canon, b_canon = canonical_json(a_text), canonical_json(b_text)
                ok = a_canon == b_canon
            except json.JSONDecodeError as e:
                a_canon = b_canon = None
                ok = False
                body_diff["error"] = "body not valid JSON: %s" % e
            if not ok and a_canon is not None:
                body_diff["a_sha256"] = hashlib.sha256(a_canon.encode()).hexdigest()
                body_diff["b_sha256"] = hashlib.sha256(b_canon.encode()).hexdigest()
        else:
            ok = a_text == b_text
            if not ok:
                body_diff["a_sha256"] = hashlib.sha256(a_text.encode()).hexdigest()
                body_diff["b_sha256"] = hashlib.sha256(b_text.encode()).hexdigest()
                first = next(difflib.unified_diff(
                    a_text.splitlines(), b_text.splitlines(),
                    fromfile="a", tofile="b", n=1), None)
                body_diff["first_divergence"] = first if first else "(whitespace/empty)"
        body_diff["same"] = ok
        diff["body"] = body_diff
        same = same and ok
    return diff, same


# --------------------------------------------------------------- execution

def write_evidence(out_dir, case_id, name, payload):
    d = os.path.join(out_dir, "evidence", case_id)
    os.makedirs(d, exist_ok=True)
    rel = os.path.join("evidence", case_id, name)
    with open(os.path.join(out_dir, rel), "w", encoding="utf-8") as fh:
        json.dump(payload, fh, ensure_ascii=False, indent=1, sort_keys=True)
    return rel


def run_declarative(case, sides, out_dir, timeout_s):
    """Execute all requests on both sides; returns (status, reason, requests)."""
    compare_cfg = case.get("compare", {})
    results, evidence = [], []
    for i, spec in enumerate(case["requests"]):
        per = {"request": {
            "method": spec.get("method", "GET").upper(),
            "path": spec["path"],
        }}
        try:
            a_resp = http_request(sides["a"], per["request"]["method"], spec["path"],
                                  spec.get("body"), spec.get("headers"), timeout_s)
            b_resp = http_request(sides["b"], per["request"]["method"], spec["path"],
                                  spec.get("body"), spec.get("headers"), timeout_s)
        except TransportError as e:
            per["transport_error"] = str(e)
            results.append(per)
            return BLOCKED, "transport: %s" % e, results, evidence
        rules = case["_applied_rules"]
        diff, same = compare_responses(a_resp, b_resp, compare_cfg, rules, [sides["a"], sides["b"]])
        per["a"] = fingerprint(a_resp)
        per["b"] = fingerprint(b_resp)
        per["diff"] = diff
        for side_name, resp in (("a", a_resp), ("b", b_resp)):
            per.setdefault("evidence", []).append(write_evidence(
                out_dir, case["id"], "%d-%s.json" % (i, side_name), {
                    "request": dict(per["request"],
                                    headers=redact_headers(spec.get("headers") or {}),
                                    body=spec.get("body")),
                    "response": {"status": resp["status"],
                                 "headers": redact_headers(resp["headers"]),
                                 "body": resp["body"].decode("utf-8", "replace")},
                }))
        results.append(per)
        if not same:
            return FAIL, None, results, evidence
    return PASS, None, results, evidence


def run_programmatic(case, run_fn, sides, out_dir, timeout_s):
    """Programmatic hook for real-client legs (curl/docker/mvn/...).

    run(ctx) must return a dict with at least {status: PASS|FAIL|BLOCKED,
    reason} and may carry a/b summaries, a diff dict and evidence lines.
    """
    ctx = type("CaseContext", (), {})()
    ctx.sides = sides
    ctx.timeout_s = timeout_s
    ctx.out_dir = out_dir
    ctx.http = lambda side_name, method, path, body=None, headers=None: \
        http_request(sides[side_name], method, path, body, headers, timeout_s)
    ctx.write_evidence = lambda name, payload: write_evidence(
        out_dir, case["id"], name, payload)
    out = run_fn(ctx)
    if not isinstance(out, dict) or out.get("status") not in (PASS, FAIL, BLOCKED):
        return BLOCKED, "run(ctx) must return {status: PASS|FAIL|BLOCKED, ...}", [], []
    return out["status"], out.get("reason"), out.get("requests", []), out.get("evidence", [])


def execute_case(case, run_fn, sides, out_dir, timeout_s):
    if run_fn is not None:
        return run_programmatic(case, run_fn, sides, out_dir, timeout_s)
    return run_declarative(case, sides, out_dir, timeout_s)


# ------------------------------------------------------------------- main

def discover(case_dir):
    if not os.path.isdir(case_dir):
        return []
    return sorted(
        os.path.join(case_dir, f) for f in os.listdir(case_dir)
        if f.endswith(".py") and not f.startswith("_"))


def main(argv=None):
    here = os.path.dirname(os.path.abspath(__file__))
    repo_root = os.path.normpath(os.path.join(here, "..", "..", ".."))
    ap = argparse.ArgumentParser(description="BinFlow difftest v2 runner")
    ap.add_argument("--cases", default=os.path.join(here, "cases"))
    ap.add_argument("--out", default=os.path.join(here, "run"))
    ap.add_argument("--normalize", default=os.path.join(
        repo_root, "docs", "compatibility", "fixtures", "normalize.yaml"))
    ap.add_argument("--case", action="append", dest="case_ids",
                    help="run only these case ids (others -> NOT_RUN)")
    ap.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_S,
                    help="per-request timeout in seconds (default %(default)s)")
    ap.add_argument("--dry-run", action="store_true",
                    help="discover only; every case -> NOT_RUN")
    ap.add_argument("--list", action="store_true", help="list case ids and exit")
    args = ap.parse_args(argv)

    # ---- discovery
    loaded, errors = [], []
    for path in discover(args.cases):
        ident = os.path.splitext(os.path.basename(path))[0]
        try:
            mod = load_case_module(path)
        except Exception as e:  # noqa: BLE001 - case file is untrusted input
            errors.append((ident, path, "import failed: %r" % e))
            continue
        run_fn = getattr(mod, "run", None)
        case = dict(getattr(mod, "CASE", {})) if getattr(mod, "CASE", None) is not None else {}
        if run_fn is None and not case:
            errors.append((ident, path, "no CASE dict and no run(ctx) hook"))
            continue
        if run_fn is None:
            err = validate_case(case)
            if err:
                errors.append((ident, path, err))
                continue
        else:
            case.setdefault("id", ident)
        case["_file"] = os.path.relpath(path, here)
        loaded.append((case, run_fn))

    ids = [c["id"] for c, _ in loaded]
    dupes = {i for i in ids if ids.count(i) > 1}
    for c, _fn in list(loaded):
        if c["id"] in dupes:
            errors.append((c["id"], c["_file"], "duplicate case id"))
            loaded = [(cc, f) for cc, f in loaded if cc["id"] != c["id"]]

    if args.list:
        for c, _ in loaded:
            print(c["id"], c.get("title", ""))
        return 0 if not errors else 2

    selected = set(args.case_ids or []) or None
    if selected:
        known = set(ids)
        unknown = selected - known
        if unknown:
            print("unknown --case id(s): %s" % ", ".join(sorted(unknown)),
                  file=sys.stderr)
            return 2

    # ---- registry
    registered, registry_note = scan_registered_domains(args.normalize)

    # ---- execution
    os.makedirs(args.out, exist_ok=True)
    results = []
    for case, run_fn in loaded:
        entry = {
            "id": case["id"],
            "title": case.get("title", ""),
            "layer": case.get("layer"),
            "file": case["_file"],
            "timeout_s": case.get("timeout_s", args.timeout),
            "status": NOT_RUN,
            "reason": None,
            "duration_ms": None,
            "requests": [],
            "evidence": [],
        }
        domain = case.get("domain")
        requested = list(case.get("normalize", []))
        if domain and domain in registered and requested:
            applied, mode, note = requested, "normalized", None
        elif requested:
            applied, mode = [], "raw"
            note = ("normalize rules %s not applied: domain %r not registered "
                    "(status: registered absent in normalize.yaml); raw comparison"
                    % (requested, domain))
        else:
            applied, mode, note = [], "raw", "no normalize rules requested"
        entry["normalize"] = {"domain": domain, "requested": requested,
                              "applied": applied, "mode": mode, "note": note}
        case["_applied_rules"] = applied

        if selected is not None and case["id"] not in selected:
            entry["reason"] = "not selected by --case filter"
            results.append(entry)
            continue
        if errors and any(e[0] == case["id"] for e in errors):
            entry["reason"] = "case file error: %s" % next(
                e[2] for e in errors if e[0] == case["id"])
            entry["status"] = BLOCKED
            results.append(entry)
            continue
        if args.dry_run:
            entry["reason"] = "dry-run: discovery only, no execution"
            results.append(entry)
            continue
        missing = preflight_missing(case)
        if missing:
            entry["status"] = BLOCKED
            entry["reason"] = "missing required env: %s" % ", ".join(missing)
            results.append(entry)
            continue

        started = time.monotonic()
        try:
            status, reason, reqs, evidence = execute_case(
                case, run_fn,
                {n: side_config(n) for n in ("a", "b")},
                args.out, entry["timeout_s"])
            entry["status"], entry["reason"] = status, reason
            entry["requests"], entry["evidence"] = reqs, evidence + [
                e for r in reqs for e in r.get("evidence", [])]
        except TransportError as e:
            entry["status"], entry["reason"] = BLOCKED, "transport: %s" % e
        except Exception as e:  # noqa: BLE001 - a case crash must not kill the batch
            entry["status"] = BLOCKED
            entry["reason"] = "case raised %s: %r" % (type(e).__name__, e)
        entry["duration_ms"] = int((time.monotonic() - started) * 1000)
        results.append(entry)

    for ident, path, err in errors:
        if not any(r["id"] == ident for r in results):
            results.append({"id": ident, "title": "", "layer": None,
                            "file": os.path.relpath(path, here),
                            "timeout_s": args.timeout, "status": BLOCKED,
                            "reason": "case file error: %s" % err,
                            "duration_ms": None, "requests": [], "evidence": [],
                            "normalize": {"domain": None, "requested": [],
                                          "applied": [], "mode": "raw",
                                          "note": "case never validated"}})

    report = {
        "schema": SCHEMA,
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "runner": {"argv": sys.argv, "timeout_default_s": DEFAULT_TIMEOUT_S,
                   "dry_run": args.dry_run},
        "env": env_state(),
        "normalize_registry": {
            "path": os.path.relpath(args.normalize, repo_root),
            "registered_domains": sorted(registered),
            "note": registry_note,
        },
        "cases": results,
    }
    out_path = os.path.join(args.out, "results.json")
    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump(report, fh, ensure_ascii=False, indent=1)
        fh.write("\n")

    counts = {s: sum(1 for r in results if r["status"] == s)
              for s in (PASS, FAIL, BLOCKED, NOT_RUN)}
    print("results: %s  (%s)" % (
        out_path, " ".join("%s=%d" % kv for kv in counts.items())))
    return 0


if __name__ == "__main__":
    sys.exit(main())
