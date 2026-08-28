# BinFlow 离线安装包

BinFlow（云原生制品仓库）的离线安装包。专为 air-gapped（无互联网）环境设计，包含运行 BinFlow 所需的一切。

## 文件布局

```
binflow_offline_<VER>.tar.gz
├── images/
│   ├── binflow-<VER>-alpine.tar       # docker save — alpine 镜像变体
│   └── binflow-<VER>-distroless.tar   # docker save — distroless 镜像变体
├── charts/
│   └── binflow-<VER>.tgz              # Helm Chart 包
├── k8s/
│   ├── deployment.yaml                # K8s Deployment 配置
│   ├── pvc.yaml                       # PVC 配置
│   ├── service.yaml                   # Service 配置
│   ├── secret.yaml                    # Secret 模板
│   ├── ingress.yaml                   # Ingress 注释样例
│   └── kustomization.yaml             # kustomize 构建入口
├── compose/
│   ├── docker-compose.yml             # docker compose 编排文件
│   ├── .env.example                   # 环境变量模板
│   └── nginx.conf                     # nginx 反代配置
├── binaries/
│   ├── binflow-<VER>-linux-amd64      # Linux amd64 二进制
│   ├── binflow-<VER>-linux-arm64      # Linux arm64 二进制
│   └── checksums.txt                  # 二进制文件 SHA256 校验和
├── install-offline.sh                 # 主安装脚本
├── README-offline.md                  # 本文件
└── SHA256SUMS                         # 包内所有文件校验和
```

## 前置条件

### docker-compose 模式（推荐）
- Docker ≥ 24.0（含 docker compose 插件）
- curl（健康检查用）

### kind 模式
- Docker ≥ 24.0
- kind ≥ 0.20
- kubectl

### helm 模式
- Docker ≥ 24.0
- kubectl（已配置集群上下文）
- helm ≥ 3.12

## 使用方式

### 解压

```bash
tar xzf binflow_offline_*.tar.gz
cd binflow_offline_*/
```

### 安装

交互式（选择部署模式）：
```bash
./install-offline.sh
```

直接指定模式：
```bash
./install-offline.sh --compose   # docker compose（默认）
./install-offline.sh --kind      # kind（K8s-in-Docker）
./install-offline.sh --helm      # helm（需已有 K8s 集群）
```

干跑预览（不做任何变更）：
```bash
./install-offline.sh --dry-run
```

More options: `./install-offline.sh --help`

## 功能特性

| 特性 | 说明 |
|---|---|
| Zero external network | 解压 → install → /readyz 200，全程零外部请求 |
| Idempotent | 可重复运行，已有镜像/部署不被重复创建 |
| Clean failure | 失败时自动回滚（删除加载的镜像、停止 compose、卸载 helm） |
| Dry-run | `--dry-run` 打印操作计划，不做任何变更 |
| Checksum validation | 安装前自动验证 SHA256SUMS |

## 零网络验证（G17b）

Bundle 构建清单验证（构建机上执行）：

```bash
# 验证 tar 包完整性
shasum -a 256 binflow_offline_*.tar.gz

# 验证包内文件校验和
tar xzf binflow_offline_*.tar.gz --to-stdout SHA256SUMS | shasum -a 256 -c
```

隔离环境烟测（无互联网，启动 compose）：

```bash
# 1. 解压
tar xzf binflow_offline_*.tar.gz

# 2. 安装（compose 模式）
./binflow_offline_*/install-offline.sh --compose

# 3. 健康检查
curl http://127.0.0.1:8080/readyz

# 4. Docker push/pull（带上管理员 token）
# 5. 通用 PUT/GET
```

## 版本说明

本离线包使用 semver 版本号（`v1.0.0`）。镜像标签、Chart 版本、二进制文件名均与此一致。如需不同版本，请重新构建离线包。

## M11 配置面（T-325）

- **binstore.yaml 存储链（T-306 / ADR-0036）**：air-gapped 环境同样支持把存储链交给独立文件——compose 模式下在 `compose/` 目录放一个 `binstore.yaml` 并按 `deploy/compose/docker-compose.yml` 注释挂载；helm 模式用 `config.binstore.*` values（Chart 1.2.0+）；k8s 清单见包内 `k8s/binstore-configmap.yaml.example`。文件拥有链期间，`BINFLOW_STORAGE__BACKEND` / `BINFLOW_STORAGE__S3__*` 环境变量被忽略（WARN）；`BINFLOW_STORAGE_S3_SECRET_ACCESS_KEY` 始终生效（env-only 秘密）。
- **实例主密钥（T-319/T-305）**：`BINFLOW_REMOTE_CREDENTIALS_KEY`（base64 32 字节）以 enc:v1 密封远程仓库凭据、复制秘密、auth_configs 与 GPG keypair。离线环境在 `.env`（compose）/ Secret（k8s/helm `masterKey.existingSecret`）中生成并备份一次——一旦存在密封行，缺失同钥会拒绝启动。
- **镜像锚定**：compose 引用的 `minio/minio` 与 `minio/mc` 均为版本锚定 tag（非 `:latest`）。2026-08-28 实测 `RELEASE.2025-04-08T15-39-49Z`（mc）可拉取可执行（T-306 曾观察到拉取失败，判定为镜像源瞬态/探测拼写问题）；对可复现性要求高的环境请走本离线包（`docker save` 全量镜像，零外部请求）。

## 故障排除

| 问题 | 检查项 |
|---|---|
| 端口冲突 | 检查 8080 端口是否已被占用：`lsof -i :8080` |
| Docker 未运行 | `docker info` |
| k8s 连接失败 | `kubectl cluster-info`、`kubectl config current-context` |
| 8080 端口已占用 | 编辑 `deploy/compose/.env` 的 `BINFLOW_BIND_PORT` |

## 卸载

```bash
# docker-compose 模式
docker compose -f deploy/compose/docker-compose.yml down -v

# kind 模式
kind delete cluster --name binflow-offline

# helm 模式
helm uninstall binflow
```