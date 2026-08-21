---
title: docker-compose 部署
sidebar_position: 12
---

# docker-compose 部署

> 适用版本：M5 GA v1.0.0（deploy/compose/docker-compose.yml；T-135, FR-36, PB-04）。
> 本文命令属「文档命令，待 QA 复跑」——核心路径与 compose 产物、.env.example 一致。

`deploy/compose/docker-compose.yml` 提供了开箱即用的单容器编排定义，支持 alpine 或 distroless 镜像变体、可选的 nginx 反代 profile、命名卷持久化。从零到 `/readyz` 200 的目标时间 &lt;= 15 分钟（含镜像构建）。

## 前置要求

- Docker >= 24.0（含 `docker compose` 插件）
- 从仓库根目录执行
- 构建依赖：node >= 20（镜像构建阶段需要，含在 Dockerfile 内，宿主机不需要）

## 安装步骤

### 1. 准备环境变量

```bash
cp deploy/compose/.env.example deploy/compose/.env
```

编辑 `deploy/compose/.env`，至少修改 `BINFLOW_ADMIN_PASSWORD`：

```ini
# deploy/compose/.env
BINFLOW_ADMIN_PASSWORD=change-me
```

`.env` 已加入 `.gitignore`，不会误提交。`.env.example` 是跟踪的模板，所有可配置项均带注释说明。

### 2. 启动（默认 alpine 变体）

```bash
docker compose -f deploy/compose/docker-compose.yml up -d --build
```

首次运行会构建镜像（包含 console 构建、docs 站点构建、Go 编译），后续启动直接使用已有镜像。构建缓存加速后续启动。

### 3. 等待健康检查通过

```bash
docker compose -f deploy/compose/docker-compose.yml ps
# binflow-ga  STATUS 显示 "(healthy)"
```

容器内置 HEALTHCHECK（alpine 用 wget、distroless 用 Go 探针），每 10s 检查 `/readyz`。

### 4. 验证 `/readyz`

```bash
curl -s http://127.0.0.1:8080/readyz
# ok
```

## 可选：nginx 反代

```bash
docker compose -f deploy/compose/docker-compose.yml --profile nginx up -d --build
```

nginx 在 443 端口做 TLS 终止并转发 `/v2/` 和 `/binflow/` 到 BinFlow（无 rewrite——路径原样转发，D-106-2 修复）。`location = /binflow` 处理无尾斜杠的控制台入口（BinFlow 自身返回 301 → `/binflow/ui/`）。

配置 nginx 端口（在 `.env` 中）：

```ini
NGINX_BIND_ADDR=127.0.0.1
NGINX_BIND_PORT=8443
```

## 使用 distroless 变体

在 `.env` 中设置：

```ini
BINFLOW_IMAGE_VARIANT=distroless
```

distroless 变体无 shell，HEALTHCHECK 通过内置 Go 探针二进制完成。`docker exec` 调试不可用。

## 配置速查

`.env` 中可配置的全部环境变量（详见 `.env.example` 注释）：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `BINFLOW_ADMIN_PASSWORD` | **必填**（无默认值） | 管理员初始密码，compose 会拒绝空值启动 |
| `BINFLOW_IMAGE_VARIANT` | alpine | 镜像变体：alpine 或 distroless |
| `BINFLOW_VER` | v1.0.0 | 版本标签 |
| `BINFLOW_BIND_ADDR` | 127.0.0.1 | 宿主机绑定地址 |
| `BINFLOW_BIND_PORT` | 8080 | 宿主机绑定端口 |
| `BINFLOW_SECURITY_ANONYMOUS_ACCESS` | true | 匿名读取 |
| `BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS` | 720 | Token 有效期（小时） |
| `BINFLOW_LOGGING__FORMAT` | console | 日志格式 |
| `BINFLOW_LOGGING__LEVEL` | info | 日志级别 |
| `BINFLOW_SERVER__GRACEFUL_TIMEOUT_SECONDS` | 30 | 优雅关闭超时 |
| `BINFLOW_STORAGE__SESSION_TTL_HOURS` | 24 | 上传会话 TTL |
| `BINFLOW_STORAGE__GC_GRACE_HOURS` | 24 | GC 宽限期 |
| `BINFLOW_AUDIT__ENABLED` | true | 审计日志 |
| `BINFLOW_CONSOLE__SESSION_TTL_HOURS` | 24 | 控制台会话 TTL |

## 数据持久化

compose 文件定义了一个命名卷 `binflow-data`，挂载到容器的 `/var/lib/binflow`（blobs + SQLite + sessions）。

```bash
# docker compose down 保留数据卷
docker compose -f deploy/compose/docker-compose.yml down

# down -v 销毁数据卷（谨慎）
docker compose -f deploy/compose/docker-compose.yml down -v
```

## 日志

```bash
# 查看实时日志
docker compose -f deploy/compose/docker-compose.yml logs -f

# 日志驱动：json-file，每个文件最大 10MB，保留 3 个文件
```

## 安全说明

- 默认绑定 `127.0.0.1:8080`——仅本机可访问。若需对外暴露，修改 `BINFLOW_BIND_ADDR=0.0.0.0`，但 **M1 仅 HTTP**，请确保在可信网络内。
- `BINFLOW_ADMIN_PASSWORD` 使用 compose 的 `:?` 强制语法——变量未设置或为空时 compose 拒绝启动，不存在静默回退（ADR-0009）。
- 匿名读取默认开启（`BINFLOW_SECURITY_ANONYMOUS_ACCESS=true`）——内容路径 GET/HEAD 免凭据，写入和 API 路径始终需要认证。
- 容器以非 root 用户运行（alpine 为 10001，distroless 为 nonroot:nonroot）。

## 卸载

```bash
# 停止并删除容器（保留数据卷）
docker compose -f deploy/compose/docker-compose.yml down

# 停止并删除容器 + 数据卷（谨慎）
docker compose -f deploy/compose/docker-compose.yml down -v

# 删除构建的镜像
docker rmi binflow:v1.0.0-alpine binflow:v1.0.0-distroless
```

## 下一步

- [Docker 运行](docker.md) — 单容器 docker run
- [Helm Chart](helm.md) — Kubernetes 部署
- [离线安装](offline.md) — air-gapped 环境
- [Generic 接入](../integrations/generic.md) — curl PUT/GET 上传下载