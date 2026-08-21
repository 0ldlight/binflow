# T-143: ADR-0005 Build Tool Runtime Isolation Admission Record

**Auditor**: security-auditor
**Date**: 2026-08-21

## Context

ADR-0005 §3 要求：构建工具链（Go 编译器、Node.js/npm）仅存在于构建阶段，不进运行时镜像。本记录验证运行时的隔离合规性。

## 验证

### Dockerfile 阶段分析

**Dockerfile.alpine** (4-stage):
1. `console` — node:22-alpine, 构建 Vite SPA
2. `docs` — node:22-alpine, 构建 Docusaurus 静态站点
3. `builder` — golang:1.26-alpine, 编译 Go 二进制
4. `runtime` — alpine:3.24, 仅含二进制 + ca-certificates + tzdata

**Dockerfile.distroless** (5-stage):
1. `console` — node:22-alpine
2. `docs` — node:22-alpine
3. `builder` — golang:1.26-alpine
4. `healthcheck-builder` — golang:1.26-alpine (内联 Go 探针)
5. `runtime` — gcr.io/distroless/static-debian13:nonroot

### 运行时层内容

**alpine runtime**: `binflow-server` 二进制 + `ca-certificates` + `tzdata` + 空 `/var/lib/binflow` + 空 `/etc/binflow`
**distroless runtime**: `binflow-server` + `healthcheck-probe` + 空 `/var/lib/binflow`

### 确认隔离

| 构建工具 | 构建阶段 | 运行时 alpine | 运行时 distroless |
|---------|---------|-------------|------------------|
| go | builder | 不存在 | 不存在 |
| node/npm | console, docs | 不存在 | 不存在 |
| TypeScript | console | 不存在 | 不存在 |
| ninja/make | console | 不存在 | 不存在 |
| C++ (node bindings) | docs (nodejieba) | 不存在 | 不存在 |
| Vite/npm lock | console | 不存在 | 不存在 |

## 结论

PASS. 构建工具运行时隔离满足 ADR-0005 要求。所有构建工具严格限于各自的构建 stage，通过 `COPY --from=` 仅传输构建产物进入运行时层。npm 原生 C++ addon（如 nodejieba）存在于 docs-site node 工具链，不进 Go 二进制、不进运行时镜像。