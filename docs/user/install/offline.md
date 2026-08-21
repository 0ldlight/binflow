---
title: 离线安装（air-gapped）
sidebar_position: 16
---

# 离线安装（air-gapped）

> 适用版本：M5 GA v1.0.0（deploy/offline/ + install-offline.sh；T-139, FR-40, PB-08）。
> 本文命令属「文档命令，待 QA 复跑」——核心路径与 install-offline.sh 产物、README-offline.md 一致。

在没有互联网访问的环境中，BinFlow 提供离线安装包（`binflow_offline_<VER>.tar.gz`），由 `make offline-bundle` 构建。包含 Docker 镜像、Helm Chart、K8s 清单、二进制文件和安装脚本——解压后全程零外部请求。

## 离线包构建（在有互联网的构建机上）

```bash
# 前置：构建多平台二进制 + Docker 镜像
make release
bash deploy/release/build-release.sh
helm package charts/binflow -d .

# 构建离线包
make offline-bundle
# 产物：deploy/offline/binflow_offline_v1.0.0.tar.gz
# 校验文件：deploy/offline/binflow_offline_v1.0.0.tar.gz.sha256
```

### 离线包文件布局

```
binflow_offline_v1.0.0.tar.gz
├── images/
│   ├── binflow-v1.0.0-alpine.tar          # docker save — alpine 镜像变体
│   └── binflow-v1.0.0-distroless.tar      # docker save — distroless 镜像变体
├── charts/
│   └── binflow-v1.0.0.tgz                  # Helm Chart 包
├── k8s/
│   ├── deployment.yaml                     # K8s Deployment
│   ├── pvc.yaml                            # PVC
│   ├── service.yaml                        # Service
│   ├── secret.yaml                         # Secret 模板
│   ├── ingress.yaml                        # Ingress 样例
│   └── kustomization.yaml                  # Kustomize 入口
├── compose/
│   ├── docker-compose.yml                  # docker compose 编排
│   ├── .env.example                        # 环境变量模板
│   └── nginx.conf                          # nginx 反代配置
├── binaries/
│   ├── binflow-v1.0.0-linux-amd64          # Linux amd64 二进制
│   ├── binflow-v1.0.0-linux-arm64          # Linux arm64 二进制
│   └── checksums.txt                       # 二进制 SHA256 校验和
├── install-offline.sh                      # 主安装脚本
├── README-offline.md                       # 说明文件
└── SHA256SUMS                              # 包内所有文件校验和
```

## 安装模式

`install-offline.sh` 支持三种安装模式：

| 模式 | 前置要求 | 适用场景 |
|---|---|---|
| **docker-compose**（默认，推荐） | Docker >= 24.0 + docker compose | 单机，最简单 |
| **kind** | Docker >= 24.0 + kind >= 0.20 + kubectl | K8s-in-Docker 测试环境 |
| **helm** | Docker >= 24.0 + kubectl + helm >= 3.12 + 已有集群 | 正式 K8s 集群 |

## 安装步骤

### 1. 传输到目标机器

将离线包传输到无互联网的目标机器（U 盘、内网共享、snapshot 等）：

```bash
# 在目标机器上
scp user@build-machine:/path/to/binflow_offline_v1.0.0.tar.gz .
```

### 2. 校验包完整性

```bash
# 校验外层 tar 的 sha256
shasum -a 256 binflow_offline_v1.0.0.tar.gz
# 与构建机上 deploy/offline/binflow_offline_v1.0.0.tar.gz.sha256 对账

# 解压并校验包内所有文件
tar xzf binflow_offline_v1.0.0.tar.gz
cd binflow_offline_v1.0.0_*/
shasum -a 256 -c SHA256SUMS
# 所有文件应显示 "OK"
```

### 3. 安装

**交互式（选择模式）**：

```bash
./install-offline.sh
# 选择 1) docker-compose  2) kind  3) helm
```

**直接指定模式**：

```bash
# docker-compose 模式（最常用）
./install-offline.sh --compose

# kind 模式
./install-offline.sh --kind

# helm 模式
./install-offline.sh --helm
```

**干跑预览**（不执行任何变更）：

```bash
./install-offline.sh --dry-run --compose
```

### 4. 验证

```bash
# docker-compose / kind 模式：端口转发后验证
curl -s http://127.0.0.1:8080/readyz
# ok

# helm 模式
kubectl port-forward svc/binflow 8080:8080 &
curl -s http://127.0.0.1:8080/readyz
```

## docker-compose 模式详解

`--compose` 模式执行流程：

1. **校验 SHA256SUMS**——包完整性前置检查
2. **docker load 镜像**——加载 alpine 和 distroless 两个镜像 tar
3. **准备 .env**——从 `.env.example` 复制并生成随机管理员密码
4. **docker compose up -d**——启动服务
5. **等待 /readyz 200**——最多 30s，超时退出

管理员密码：

```bash
# 随机生成的密码写在 compose/.env 中
cat compose/.env | grep BINFLOW_ADMIN_PASSWORD
```

## kind 模式详解

`--kind` 模式执行流程：

1. 校验 SHA256SUMS
2. docker load 镜像
3. 创建 kind 集群（如 `binflow-offline` 已存在则跳过）
4. `kind load docker-image` 导入镜像到集群
5. `kubectl apply -k k8s/` 部署
6. 等待 deployment ready + 端口转发验证

## helm 模式详解

`--helm` 模式执行流程：

1. 校验 SHA256SUMS
2. docker load 镜像
3. 验证 kubectl 集群上下文
4. `helm install binflow charts/binflow-<VER>.tgz`（已安装则 upgrade）
5. 等待 deployment ready

ImagePullPolicy 设为 `IfNotPresent`——镜像已通过 `docker load` 存在本地 docker daemon 中。

## 故障处理

`install-offline.sh` 在失败时自动回滚：
- 已加载的 docker 镜像 → 删除
- 已启动的 compose 栈 → 停止
- 已安装的 helm release → 卸载
- 已创建的 K8s 资源 → 删除

常见问题：

| 问题 | 处置 |
|---|---|
| 端口 8080 被占用 | 编辑 compose 模式的 `compose/.env` 中 `BINFLOW_BIND_PORT` |
| SHA256SUMS 校验失败 | 包在传输过程中损坏，重新传输 |
| Docker 未运行 | `docker info` 确认 daemon 状态 |
| K8s 连接失败 | `kubectl config current-context` 检查上下文 |

## 卸载

```bash
# docker-compose 模式
docker compose -f compose/docker-compose.yml down -v

# kind 模式
kind delete cluster --name binflow-offline

# helm 模式
helm uninstall binflow
```

## 零网络验证（G17b）

离线包设计保证：解压后到 `/readyz` 200 全程零外部请求。验证方法：

```bash
# 1. 在隔离环境（断网）中
tar xzf binflow_offline_v1.0.0.tar.gz

# 2. 安装并验证
./binflow_offline_*/install-offline.sh --compose

# 3. 检查零外部网络请求
curl -s http://127.0.0.1:8080/readyz
# ok
```

## 下一步

- [docker-compose 部署](compose.md) — 在线 compose 方式
- [Helm Chart](helm.md) — 在线 Helm 方式
- [单二进制安装](binary.md) — 裸机安装