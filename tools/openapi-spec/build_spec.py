#!/usr/bin/env python3
# Build fern/openapi/binflow.json — the OpenAPI 3.1 spec behind the Fern API tab.
#
# Provenance (authority order):
#   1. docs/user/api-reference.md  — the contract page: endpoints, params, error
#      wording and examples are carried VERBATIM (iteration markers such as
#      milestone/ticket ids stripped per the Fern docs rules).
#   2. internal/httpapi/router.go (+ webhooks.go, npm route.go) — route inventory
#      cross-check; divergences follow the router-verified spelling and are
#      annotated in the operation description + the work log.
#   3. Endpoints ahead of the contract page (wire facts read from the handlers):
#      system_{maintenance,backups,schedules,qrl}.go, search_dates.go,
#      repositories_probe.go, security.go (users collection create).
#
# Regenerate:  python3 tools/openapi-spec/build_spec.py
# Validate:    python3 -m json.tool fern/openapi/binflow.json > /dev/null

import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import helpers
import d_content
import d_security
import d_system
import d_search
import d_protocol

OUT = HERE.parent.parent / "fern" / "openapi" / "binflow.json"


def build():
    for m in (d_content, d_security, d_system, d_search, d_protocol):
        m.build()

    spec = {
        "openapi": "3.1.0",
        "jsonSchemaDialect": "https://json-schema.org/draft/2020-12/schema",
        "info": {
            "title": "BinFlow API",
            "version": "1.0.0",
            "summary": "BinFlow artifact repository REST API — the Artifactory-compatible surface, the native /api/v1 surface, and protocol client surfaces",
            "description": (
                "The BinFlow API spans four surfaces:\n\n"
                "1. **Artifact content paths** (no `/api` prefix): `/binflow/<repoKey>/<path>` — upload, download and "
                "delete artifacts; every protocol client goes through here;\n"
                "2. **Management API** (`/binflow/api/*`): repository CRUD, users/groups/permissions, audit, GC, "
                "tokens, search, system info;\n"
                "3. **Docker Registry root-level plane** (`/v2/*`) — separately routed, not under the `/binflow` "
                "prefix (in this spec those paths carry their own root-level server);\n"
                "4. **Webhook event surface** (`/binflow/event/api/v1/*`) — subscription CRUD, test sends, "
                "troubleshooting.\n\n"
                "**Authentication**: Basic (HTTP Basic Auth — at the REST API layer credentials must always be present "
                "and correct in the request; there is no challenge-then-retry, a failed authentication is an immediate "
                "401) and Bearer tokens (recommended for high QPS/CI — validation does not run the argon2 hash). "
                "The console additionally uses a `binflow_session` cookie "
                "(HttpOnly; Path=/binflow; SameSite=Lax) sent automatically with same-origin requests; non-GET/HEAD "
                "writes authenticated by the session that carry a cross-origin `Origin` header → 403.\n\n"
                "**Three error formats**: the errors[] envelope (the primary format: "
                "`{\"errors\":[{\"status\":<code>,\"message\":\"…\"}]}`); plain-text errors (user/group management "
                "domains); OAuth-style errors (token endpoint and the docker domain: "
                "`{\"error\":…,\"error_description\":…}`).\n\n"
                "**Intentionally unrouted paths (not supported, 404)**: `/api/v2/**` (the Artifactory v2 permissions "
                "API — use `/api/v1/permissions`; the repository key-pair association face "
                "`/api/v2/repositories/{key}/keyPairs` is the one exception), `/api/export/**` and `/api/import/**` "
                "(backup/restore is CLI-only), `/api/system/storage/prune/**` (space reclamation goes through GC), "
                "`/binflow/v2/**` (docker endpoints do not live under the `/binflow` prefix), "
                "`/api/system/licenses` (plural — the HA multi-license semantics are not adopted), the search family "
                "`props|users|artifactory|badge` (`prop` is the official singular spelling; the plural `props` is "
                "404), and `/api/flat/copy|move`."),
            "contact": {"name": "BinFlow", "url": "https://binflow.docs.buildwithfern.com"},
        },
        "servers": [
            {"url": "/binflow",
             "description": "BinFlow default context path (http.path in binflow.yaml, default /binflow) — "
                            "shared by the management API, content paths and the event surface"},
        ],
        "security": [{"basicAuth": []}, {"bearerToken": []}],
        "tags": helpers.TAGS,
        "paths": helpers.paths,
        "components": {
            "securitySchemes": {
                "basicAuth": {
                    "type": "http", "scheme": "basic",
                    "description": "HTTP Basic authentication (curl `-u`, CI scripts, client credential "
                                   "configuration — maven settings.xml, .pypirc, npm _auth). Supported on every "
                                   "/binflow/api/* base route; password hashes are argon2id (memory-hard) — prefer "
                                   "Bearer tokens under high concurrency.",
                },
                "bearerToken": {
                    "type": "http", "scheme": "bearer", "bearerFormat": "64-hex",
                    "description": "Access tokens (minted by `POST /api/security/token`; 64-char hex / default TTL "
                                   "30 days). Validation does not run the argon2 hash and vastly outperforms Basic; "
                                   "the docker token flow uses the same table — revocations on the management API "
                                   "take effect on docker tokens immediately.",
                },
            },
            "schemas": helpers.build_schemas(),
        },
    }
    return spec


def self_check(spec, text):
    problems = []

    # iteration markers must be gone
    for m in re.finditer(r"(?:^|[^A-Za-z0-9])(?:T-\d{3}|M\d{1,2})(?:[^0-9]|$)", text):
        problems.append("iteration marker at offset %d: %r" % (m.start(), text[max(0, m.start()-40):m.start()+20]))

    ids = []
    for path, item in spec["paths"].items():
        if not path.startswith("/"):
            problems.append("path not starting with /: %s" % path)
        for m in re.finditer(r"{([^}]+)}", path):
            pass
        tmpl = set(re.findall(r"{([^}]+)}", path))
        for method, o in item.items():
            if method not in ("get", "put", "post", "delete", "patch", "head", "options"):
                problems.append("bad method %r under %s" % (method, path))
                continue
            ids.append(o["operationId"])
            if not o.get("responses"):
                problems.append("%s %s: no responses" % (method, path))
            declared = set()
            for p in o.get("parameters", []):
                if p["in"] == "path":
                    declared.add(p["name"])
                if p["in"] == "query" and p["name"] != "properties":
                    if " " in p["name"]:
                        problems.append("bad query param name %r" % p["name"])
            missing = tmpl - declared
            if missing:
                problems.append("%s %s: path params not declared: %s" % (method, path, missing))
    dupes = {i for i in ids if ids.count(i) > 1}
    if dupes:
        problems.append("duplicate operationIds: %s" % sorted(dupes))

    # every $ref resolves
    names = set(spec["components"]["schemas"].keys())
    for m in re.finditer(r'"\$ref": "#/components/schemas/([^"]+)"', text):
        if m.group(1) not in names:
            problems.append("dangling $ref: %s" % m.group(1))

    return problems, ids


def main():
    spec = build()
    text = json.dumps(spec, ensure_ascii=False, indent=2) + "\n"
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(text, encoding="utf-8")

    problems, ids = self_check(spec, text)
    ops = sum(len([m for m in item if m in ("get", "put", "post", "delete", "patch", "head")])
              for item in spec["paths"].values())
    tags = len(spec["tags"])
    schemas = len(spec["components"]["schemas"])
    print("binflow.json: %d path items, %d operations, %d tags, %d schemas, %d bytes"
          % (len(spec["paths"]), ops, tags, schemas, len(text.encode("utf-8"))))
    if problems:
        print("SELF-CHECK PROBLEMS (%d):" % len(problems))
        for p in problems[:40]:
            print("  -", p)
        sys.exit(1)
    print("self-check: clean")


if __name__ == "__main__":
    main()
