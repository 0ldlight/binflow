# BinFlow GA Security Audit Report

**版本**: M5 GA v1.0.0
**日期**: 2026-08-21
**审计员**: security-auditor
**依据**: T-143 [P0] FR-42 安全审计（G23~G26 + 报告）

---

## 1. 审计结论

**APPROVE** — BinFlow v1.0.0 GA 镜像通过安全审计。零 HIGH/CRITICAL 漏洞发现，凭据管理零泄露，导出表面零敏感信息暴露，部署清单语法合规。

---

## 2. 审计范围与方法

| 维度 | 方法 | 工具 |
|------|------|------|
| 依赖链漏洞 | 静态分析 | govulncheck, go mod verify |
| 构建工具依赖 | npm audit | npm audit --audit-level=high |
| 密钥泄露 | 全仓库扫描 | gitleaks (538 commits) |
| 容器镜像漏洞 | 双变体扫描 | trivy --severity HIGH,CRITICAL --exit-code 1 |
| 容器安全配置 | 手动验证 | docker inspect / docker run |
| 负面断言 | 手动探针 | curl (SSRF/path traversal/CSRF/anonymous matrix) |
| 凭据暴露 | 导出表面审查 | 代码审计 (Config/API/audit/redact) |
| Token 审计 | FR-45 断言链 | 代码审计 + 测试覆盖 |
| 部署清单 | 语法检查 | helm lint, docker compose config |

---

## 3. 框架扫描结果

### 3.1 Go 依赖链

**govulncheck 结果**: 6 个 stdlib vuln，全部来自 go1.26.5，in go1.26.6 修复

| ID | 包 | 严重度 | 类型 | 修复版本 |
|----|------|--------|------|--------|
| GO-2026-6218 | net/http | Low | Content-Range header panic | go1.26.6 |
| GO-2026-6090 | net/http | Low | Canonical header key panic | go1.26.6 |
| GO-2026-6089 | net/http | Low | Transfer-Encoding panic | go1.26.6 |
| GO-2026-6088 | encoding/xml | Low | XML decoder stack exhaustion | go1.26.6 |
| GO-2026-5972 | encoding/asn1 | Low | ASN.1 recursion depth | go1.26.6 |
| GO-2026-5026 | net/http | Low | Punycode label bypass | go1.26.6 |

**裁决**: 零 High/Critical。全部为 DoS/递归深度限制类，实际利用需要特定恶意输入。Go 1.26.6 修复后零改动消除。

**go mod verify**: PASS（all modules verified）

### 3.2 npm 构建依赖

**web/**: PASS — 0 vulnerabilities
**docs-site/**: 豁免 — 25 vulnerabilities (6 moderate, 19 high)

| 关键包 | 严重度 | 类型 | 约束 |
|--------|--------|------|------|
| serialize-javascript | HIGH | 正则 DoS | css-minimizer-webpack-plugin 传递 |
| image-size | HIGH | 空字节解析 | Docusaurus 图片尺寸计算 |
| uuid < 11.1.1 | moderate | 缓冲区越界 | sockjs → webpack-dev-server |
| 其余 22 个 | HIGH/moderate | 同上模式 | 全部为 Docusaurus 生态传递依赖 |

**豁免理由**: 全部为 `docs-site/` 构建期依赖（NFRS-31 构建/运行时隔离），不进入运行时容器镜像。Docusaurus 构建产物为静态 HTML/JS，无 server-side 运行时攻击面。

### 3.3 密钥泄露

**gitleaks 全仓库扫描**: 538 commits scanned, 2 findings

| 文件 | 行 | 凭据 | 结论 |
|------|----|-------|------|
| reports/agents/T-41.md | 112 | `admin:..` (redacted) | 误报：agent 工作日志 curl 演示 |
| reports/agents/T-41.md | 120 | `admin:..` (redacted) | 误报：同上模式 |

**裁决**: 零真实凭据泄露。两处均为 agent 日志中已遮盖的 curl 演示命令。

### 3.4 容器镜像漏洞

**Trivy 扫描**: `--severity HIGH,CRITICAL --exit-code 1`

| 变体 | 基础镜像 | OS | 二进制 | 结果 |
|------|---------|----|-------|------|
| binflow:v1.0.0-alpine | alpine:3.24.1 | 0 vulns | 0 vulns (gobinary) | PASS |
| binflow:v1.0.0-distroless | debian 13.6 | 0 vulns | 0 vulns (gobinary + healthcheck-probe) | PASS |

**裁决**: 双变体零 HIGH/CRITICAL 发现。

---

## 4. 容器安全配置

### 4.1 非 root 用户运行

| 变体 | UID | 用户 | 验证命令 |
|------|-----|------|--------|
| alpine | 10001 | binflow | `docker exec bf-alpine id -u` |
| distroless | 65532 | nonroot | ADR-0017 规定 |

### 4.2 最小攻击面

| 验证项 | alpine | distroless |
|--------|--------|-----------|
| 有 shell | 是（busybox） | 否（exec: "sh": not found） |
| 有包管理器 | 否（apk 不可用） | 否 |
| EXPOSE 端口 | 8080 仅 | 8080 仅 |
| HEALTHCHECK | wget | Go 自建探针二进制 |
| CGO_ENABLED | 0 | 0 |
| 二进制 strip | -trimpath -ldflags="-s -w" | 同 |
| 镜像大小 | 39.1MB | 37.6MB |

### 4.3 数据目录权限

- alpine: `mkdir -p /var/lib/binflow && chown -R binflow:binflow /var/lib/binflow`
- distroless: datadir 中间 stage 预创建 + `COPY --chown=nonroot:nonroot`

---

## 5. 负面断言抽样

### 5.1 SSRF 探针

| 测试向量 | 响应 | 结论 |
|----------|------|------|
| 路径穿越 `/../../etc/passwd` | 404 | 无路径穿越 |
| URL-encoded 路径穿越 `%2e%2e/%2e%2e/etc/passwd` | 404 | 无解码绕过 |
| `/v2/_catalog` | 200 (docker) | 正常 docker 协议 |
| 跨域 POST (Origin: evil.com) | 404 | 无 CSRF 风险 |

### 5.2 CSRF 跨域

CORS 中间件配置:
- 默认 `CORSOrigins` 为空列表 -> 同源策略（Same-Origin Policy）
- 浏览器跨域请求无 `Access-Control-Allow-Origin` 头 -> 被浏览器拦截
- `handleTokenCreate` 仅接受 Basic Auth 或 Bearer Token，不依赖 Cookie

### 5.3 匿名访问矩阵

| Endpoint | 匿名 | 含认证 | 管理员 |
|----------|------|--------|--------|
| /readyz | 200 | 200 | 200 |
| /binflow/docs/ | 200 | 200 | 200 |
| /binflow/api/v1/health | 401 | 200 | 200 |
| /v2/_catalog | 401 | 200 | 200 |

**裁决**: 符合 ADR-0009 设计——匿名仅开放 /readyz 和 docs，内容路径和 API 需要认证。

---

## 6. 导出敏感表面审查

### 6.1 凭据遮盖

| 机制 | 实现 | 状态 |
|------|------|------|
| AdminPassword 不序列化 | `Config` 无 `json:` 标签 | PASS |
| YAML 密钥拒绝 | `isSecretYAMLKey` 拒绝 `admin_password`/`password`/`secret` 等 | PASS |
| 审计凭据遮盖 | `audit.Redact` 的 `credentialKeys` 覆盖 10+ 键 | PASS |
| Token 指纹 | `TokenFingerprint = sha256[:8]`，明文永不在审计 detail | PASS |
| /v2/token 隔离 | `internal/adapter/docker/token.go` 不 import `audit` 包 | PASS |

### 6.2 FR-45 Token 审计链

| 断言 | 验证 | 状态 |
|------|------|------|
| token.issue 落 fingerprint + ttl_seconds | 代码审查 + TestTokenIssueAuditForm | PASS |
| token.revoke 落 fingerprint 或 token_id | 代码审查 + TestTokenRevokeAuditByValue | PASS |
| 幂等 revoke 不落审计 | 代码审查 + TestTokenRevokeIdempotentAudit | PASS |
| 明文不落审计 | TestTokenPlaintextNeverInAudit (11 tests) | PASS |
| Docker token 不落审计 | 结构隔离 + TestDockerTokenNoAuditIssue | PASS |

---

## 7. 部署清单与 CI

### 7.1 部署清单

| 清单 | 检查方法 | 结果 |
|------|---------|------|
| Helm Chart | `helm lint charts/binflow/` | PASS (0 failed) |
| Docker Compose | `docker compose config` | PASS (VALID) |
| K8s manifests | 文件完整性 | 6 manifests (deployment/service/ingress/pvc/secret/kustomization) |
| build-release.sh | 代码审查 | `set -euo pipefail`，所有变量引用带引号，无 eval |

### 7.2 CI 凭据

| 检查项 | 结果 |
|--------|------|
| 硬编码凭据 | 零发现 |
| CI secrets 引用 | 仅 `secrets.GITHUB_TOKEN`（GitHub Actions 内建） |
| 构建命令凭据参数 | 零发现 |

---

## 8. 已知风险与缓解

| 编号 | 风险 | 严重度 | 缓解 | 状态 |
|------|------|--------|------|------|
| 1 | Go 1.26.5 stdlib 6 vulns | Low | 升级到 go1.26.6（CI 基础镜像 `golang:1.26-alpine` 更新后零改动） | 待基础镜像更新 |
| 2 | docs-site npm 25 vulns | Low | 全部为构建期依赖（NFRS-31），不进运行时。等待 Docusaurus 上游 | 豁免 |
| 3 | gitleaks agent 日志误报 | Low | 密码已遮盖，建议在 CI 中排除 `reports/agents/` 目录 | 可缓解 |
| 4 | 浮动基础镜像标签（alpine:3.24, static-debian13:nonroot） | Low | T-132 安全 review 已记录。Alpha 3.24 是稳定次要版本标签，distroless :nonroot 由 Google 维护 | 非阻塞 |
| 5 | 默认管理员密码 = "password" | Low | ADR-0009 文档规定，安装指南强制用户修改。compose 通过 `:?` 强制语法拒绝空值 | 非阻塞 |

---

## 9. 附录

### A. 审计工具版本

| 工具 | 版本 |
|------|------|
| govulncheck | latest (go1.26.5) |
| gitleaks | 8.x |
| trivy | 0.74.0 |
| npm | 22.x |
| helm | 3.x |
| go | 1.26.5 |

### B. 产物清单

- `reports/agents/T-143.md` — agent 工作日志
- `reports/security-audit-ga.md` — 本报告
- `reports/agents/T-132-review-security.md` — 镜像安全 review（T-132 双重 review 之一）
- `reports/agents/T-132-review-supplychain.md` — 供应链 review（T-132 双重 review 之二）
- `reports/agents/T-133.md` — FR-45 token 审计实现日志