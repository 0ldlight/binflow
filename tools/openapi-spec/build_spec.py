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
            "summary": "BinFlow 制品仓库 REST API——Artifactory 兼容面 + /api/v1 自有面 + 协议接入面",
            "description": (
                "BinFlow 的 API 分为四个面：\n\n"
                "1. **制品的容路径**（无 `/api` 前缀）：`/binflow/<repoKey>/<path>`——上传、下载、删除制品，"
                "各协议客户端走这里；\n"
                "2. **管理面 API**（`/binflow/api/*`）：仓库 CRUD、用户/组/权限、审计、GC、Token、搜索、系统信息；\n"
                "3. **Docker Registry 根级平面**（`/v2/*`）——独立路由，不走 `/binflow` 前缀（本 spec 中该组路径"
                "以独立的根级 server 表达）；\n"
                "4. **Webhook 事件面**（`/binflow/event/api/v1/*`）——订阅 CRUD/试发/排障。\n\n"
                "**认证**：Basic（HTTP Basic Auth——REST API 层面要求凭据始终在请求中且正确，不存在「先访问后挑战」，"
                "一次性认证失败直接 401）与 Bearer Token（高 QPS/CI 推荐——校验不触发 argon2 哈希计算）。"
                "控制台另有 `binflow_session` 会话 cookie（HttpOnly; Path=/binflow; SameSite=Lax），"
                "随同源请求自动发送；会话认证的非 GET/HEAD 写请求携带非同源 `Origin` 头 → 403。\n\n"
                "**错误响应三种格式**：errors[] 信封（主要格式："
                "`{\"errors\":[{\"status\":<code>,\"message\":\"…\"}]}`）；纯文本错误（用户/组管理域）；"
                "OAuth 风格错误（Token 端点与 docker 域：`{\"error\":…,\"error_description\":…}`）。\n\n"
                "**已保留 / 未实现路径（有意 404）**：`/api/v2/**`（Artifactory v2 权限 API——用 `/api/v1/permissions`；"
                "钥对仓关联面 `/api/v2/repositories/{key}/keyPairs` 例外）、`/api/export/**` 与 `/api/import/**`"
                "（备份恢复仅 CLI）、`/api/system/storage/prune/**`（空间回收走 GC）、`/binflow/v2/**`"
                "（docker 端点不走 `/binflow` 前缀）、`/api/system/licenses`（复数——HA 多证语义不采纳）、"
                "搜索族 `props|users|artifactory|badge`（`prop` 是官方单数拼写，复数 `props` 404）、"
                "`/api/flat/copy|move`。\n\n"
                "本 spec 由契约页 docs/user/api-reference.md 衍生（端点/参数/错误文案/示例逐字保真，"
                "迭代标记去除），路由清单对 internal/httpapi/router.go 核对；与路由不一致处以路由验证文法为准并标注。"),
            "contact": {"name": "BinFlow", "url": "https://binflow.docs.buildwithfern.com"},
        },
        "servers": [
            {"url": "/binflow",
             "description": "BinFlow 默认上下文路径（binflow.yaml 的 http.path，缺省 /binflow）——"
                            "管理面、内容路径与事件面共用此前缀"},
        ],
        "security": [{"basicAuth": []}, {"bearerToken": []}],
        "tags": helpers.TAGS,
        "paths": helpers.paths,
        "components": {
            "securitySchemes": {
                "basicAuth": {
                    "type": "http", "scheme": "basic",
                    "description": "HTTP Basic 认证（curl `-u`、CI 脚本、客户端凭据配置——maven settings.xml、"
                                   ".pypirc、npm _auth）。所有 /binflow/api/* 基础路由均支持；"
                                   "口令哈希为 argon2id（memory-hard），高并发场景请用 Bearer Token。",
                },
                "bearerToken": {
                    "type": "http", "scheme": "bearer", "bearerFormat": "64-hex",
                    "description": "Access Token（`POST /api/security/token` 签发；64 位 hex / 默认 TTL 30 天）。"
                                   "校验不触发 argon2 哈希计算，性能远优于 Basic；docker token 流同表——"
                                   "管理面吊销对 docker token 即时生效。",
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
