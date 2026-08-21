---
title: Docker 运行
sidebar_position: 11
---

# Docker 运行

> 适用版本：M5 GA v1.0.0（alpine 与 distroless 两个镜像变体，linux/amd64 + linux/arm64 多架构）。
> 本文命令属「文档命令，待 QA 复跑」——核心路径（docker run → /readyz）语法与 Dockerfile、compose 产物一致。

BinFlow 提供两个 Docker 镜像变体，均以非 root 用户运行，构建自 `deploy/release/`：

| 变体 | 镜像标签 | 特点 |
|---|---|---|
| **alpine**（默认） | `binflow:v1.0.0-alpine` | 基于 alpine:3.24，含 shell + wget（HEALTHCHECK）+ ca-certificates + tzdata |
| **distroless** | `binflow:v1.0.0-distroless` | 基于 gcr.io/distroless/static-debian13:nonroot，**无 shell**，最小攻击面，Go 探针二进制做 HEALTHCHECK |

## 前置要求

- Docker >= 24.0（含 BuildKit 支持）
- 拉取镜像（或本地 `docker build`）：
  ```bash
  docker pull ghcr.io/lzwzzy/binflow:v1.0.0-alpine
  # 或 distroless
  docker pull ghcr.io/lzwzzy/binflow:v1.0.0-distroless
  ```

## 快速启动（alpine）

```bash
docker run -d \
  --name binflow \
  -p 127.0.0.1:8080:8080 \
  -e BINFLOW_ADMIN_PASSWORD=change-me \
  -v binflow-data:/var/lib/binflow \
  ghcr.io/lzwzzy/binflow:v1.0.0-alpine
```

关键参数说明：

| 参数 | 说明 |
|---|---|
| `-p 127.0.0.1:8080:8080` | 绑定 loopback——M1 仅 HTTP，绑定到非 loopback 前请确保在可信网络内 |
| `-e BINFLOW_ADMIN_PASSWORD` | 管理员初始密码（**必填**，ADR-0009 要求 env-only，永不进入 YAML） |
| `-v binflow-data:/var/lib/binflow` | 持久化数据卷（blobs + SQLite + sessions） |

## 快速启动（distroless）

```bash
docker run -d \
  --name binflow \
  -p 127.0.0.1:8080:8080 \
  -e BINFLOW_ADMIN_PASSWORD=change-me \
  -v binflow-data:/var/lib/binflow \
  ghcr.io/lzwzzy/binflow:v1.0.0-distroless
```

distroless 变体无 shell，无法 `docker exec` 进去调试。HEALTHCHECK 通过内置 Go 探针二进制 `/usr/local/bin/healthcheck-probe` 完成。

## 验证 `/readyz`

```bash
# 等待容器健康（alpine 有 wget HEALTHCHECK，distroless 有 Go 探针）
docker ps --filter name=binflow
# STATUS 列显示 "(healthy)"

# 或直接 curl
curl -s http://127.0.0.1:8080/readyz
# ok
```

## 查看日志

```bash
docker logs -f binflow
```

## 自定义配置

所有配置项均通过环境变量传入（config 全大写、点改下划线、双下划线分隔层级）：

```bash
docker run -d --name binflow \
  -p 127.0.0.1:8080:8080 \
  -e BINFLOW_ADMIN_PASSWORD=my-secret \
  -e BINFLOW_SECURITY_ANONYMOUS_ACCESS=false \
  -e BINFLOW_LOGGING__LEVEL=debug \
  -e BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS=168 \
  -v binflow-data:/var/lib/binflow \
  ghcr.io/lzwzzy/binflow:v1.0.0-alpine
```

## 资源限制

```bash
docker run -d --name binflow \
  --cpus 2 --memory 1g \
  -p 127.0.0.1:8080:8080 \
  -e BINFLOW_ADMIN_PASSWORD=change-me \
  -v binflow-data:/var/lib/binflow \
  ghcr.io/lzwzzy/binflow:v1.0.0-alpine
```

## 镜像构建（从源码）

如需从源码构建镜像（例如定制后构建）：

```bash
# alpine 变体
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f deploy/release/Dockerfile.alpine \
  -t binflow:v1.0.0-alpine \
  --build-arg BINFLOW_VER=v1.0.0 \
  --build-arg BINFLOW_REVISION=$(git rev-parse --short HEAD) \
  .

# distroless 变体
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -f deploy/release/Dockerfile.distroless \
  -t binflow:v1.0.0-distroless \
  --build-arg BINFLOW_VER=v1.0.0 \
  --build-arg BINFLOW_REVISION=$(git rev-parse --short HEAD) \
  .
```

构建上下文必须是仓库根目录。四个阶段：console（node:22-alpine 构建 SPA）→ docs（node:22-alpine 构建 Docusaurus 文档站）→ builder（golang:1.26-alpine，零 CGo）→ runtime（alpine:3.24 或 distroless static-debian13:nonroot）。

## 卸载

```bash
docker stop binflow
docker rm binflow
# 删除数据卷（谨慎）
docker volume rm binflow-data
```

## 下一步

- [docker-compose 部署](compose.md) — 多服务编排 + nginx 反代
- [Helm Chart](helm.md) — Kubernetes 部署
- [Generic 接入](../integrations/generic.md) — curl PUT/GET 上传下载