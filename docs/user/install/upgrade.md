---
title: 升级与版本说明
sidebar_position: 17
---

# 升级与版本说明

> 适用版本：M5 GA v1.0.0。本文描述 v1.0.0 起步的升级流程（出口：二进制替换）。跨版本升级涉及元数据迁移链 001~007。

BinFlow v1.0.0 是初始 GA 版本。升级航线从本版本开始。

## 版本号语义

BinFlow 使用 [semver](https://semver.org/)（`MAJOR.MINOR.PATCH`）：

| 增量 | 含义 | 示例 |
|---|---|---|
| **MAJOR** | 不兼容的 API/数据格式变更 | `1.x → 2.0` |
| **MINOR** | 向后兼容的功能新增 | `1.0 → 1.1` |
| **PATCH** | 向后兼容的缺陷修复 | `1.0.0 → 1.0.1` |

- v1.0.0 之前的版本（M1~M4 里程碑）是开发迭代版本，不在本文升级路径覆盖范围内。
- 预发布标签：`v1.0.0-rc.1`、`v1.0.0-beta`。
- 构建元数据：如 `v1.0.0+20260821`。

## 升级策略

### 二进制替换（裸机 / systemd）

```bash
# 1. 备份数据（建议）
binflow-server export -c binflow.yaml --output /backup/bf-pre-upgrade

# 2. 下载新版本
curl -fsSLO "https://github.com/example/binflow/releases/download/v1.0.1/binflow_${VER}_linux_amd64.tar.gz"
grep "binflow_v1.0.1_linux_amd64.tar.gz" binflow_v1.0.1_checksums.txt | shasum -a 256 -c

# 3. 停机
sudo systemctl stop binflow

# 4. 替换二进制（systemd 路径）
sudo cp binflow-server /usr/local/bin/binflow-server
sudo chmod 755 /usr/local/bin/binflow-server

# 5. 启动
sudo systemctl start binflow

# 6. 验证
curl -s http://127.0.0.1:8080/readyz
# ok
```

### Docker / compose

```bash
# 1. 拉取新镜像
docker pull ghcr.io/lzwzzy/binflow:v1.0.1-alpine

# 2. 重建容器（docker-compose）
docker compose -f deploy/compose/docker-compose.yml up -d --build

# 3. 验证
curl -s http://127.0.0.1:8080/readyz
```

### Helm

```bash
# 1. 更新 Chart
helm upgrade binflow ./charts/binflow \
  --set image.tag=v1.0.1-alpine \
  --set admin.password=$(kubectl get secret binflow-secret -o jsonpath="{.data.BINFLOW_ADMIN_PASSWORD}" | base64 -d)

# 2. 验证
kubectl rollout status deployment/binflow
kubectl port-forward svc/binflow 8080:8080 &
curl -s http://127.0.0.1:8080/readyz
```

### 原生 K8s

```bash
# 编辑 kustomization.yaml 或 deployment.yaml 中 image tag
kubectl apply -k deploy/k8s/
kubectl rollout status deployment/binflow
```

## 迁移路径：export/import（跨大版本）

当元数据格式不兼容时（MAJOR 升级），使用 [export/import CLI](../admin/backup-restore.md) 作为迁移路径：

```bash
# 1. 旧实例在线 export
binflow-server export -c old-binflow.yaml --output /backup/bf-v1-data

# 2. 停机 + 清空新实例数据目录
sudo systemctl stop binflow
rm -rf /var/lib/binflow/data && mkdir -p /var/lib/binflow/data

# 3. 新实例 import
binflow-server import -c new-binflow.yaml --input /backup/bf-v1-data --verify full

# 4. 启动
sudo systemctl start binflow
```

export/import 是**唯一支持跨大版本的迁移方式**。直接替换二进制然后启动（服务端自动迁移）仅适用于同 MAJOR 版本的 MINOR/PATCH 升级。

## 元数据迁移链

BinFlow 服务端在启动时自动执行元数据迁移（`metadata.Open` → 自动迁移）。迁移文件位于 `internal/metadata/migrations/sqlite/`，按编号顺序执行。每个迁移都是幂等的（`INSERT OR IGNORE` 模式可重入）。

### 迁移文件说明

| 编号 | 里程碑 | 说明 | 向后兼容 |
|---|---|---|---|
| **001** | M1 | 初始 schema：repositories、remote_configs、blobs、nodes、users、tokens、permission_targets、permission_principals、audit_events、virtual_members | —（基线） |
| **002** | M2 | Docker 域：docker_manifests、docker_tags、docker_refs | 可 |
| **003** | M3 | Remote 域扩展：remote_configs 加 content_ttl_seconds/metadata_ttl_seconds/allow_private_upstream、重命名 blocked_out；remote_cache 加过期索引；blobs 加 sha1 索引 | 可 |
| **004** | M4 | 控制台/治理域：users 加 email；audit 查询索引；groups、user_groups、web_sessions、repo_usage 表 | 可 |
| **005** | M4 | 配额回填：从 nodes SUM 回填 repo_usage 数据（ON CONFLICT DO NOTHING） | 可 |
| **006** | M4 | 用户组 username 索引：user_groups(username) 索引 | 可 |
| **007** | M5 | 文件夹行回填（ADR-0016）：目录级别节点的物化（递归 CTE 从已有 nodes 生成祖先目录行） | 可 |

### 跨版本升级的迁移路径

```
v1.0.0 ───→ v1.0.1 ───→ v1.1.0 ───→ v2.0.0
 │            │            │            │
 └── 001~007  └── 008+     └── 009+     └── export/import 迁移
```

- **PATCH 升级**（`1.0.0 → 1.0.1`）：二进制替换，服务启动时自动运行增量迁移（如 008_fix_something.sql）
- **MINOR 升级**（`1.0.x → 1.1.x`）：二进制替换 + 自动迁移
- **MAJOR 升级**（`1.x → 2.0`）：export/import 迁移路径

### 迁移说明

- **幂等性**：所有迁移文件都是幂等的——重复执行不损坏数据
- **向后兼容**：M1~M5（001~007）均为向后兼容的增量迁移（仅加表、加列、加索引、回填数据）
- **SQLite 专属**：当前仅 SQLite 驱动支持（Postgres 占位目录 `migrations/postgres/` 待后续同步）
- **执行时机**：`binflow-server serve` 启动阶段自动执行，执行失败则进程退出（不启动服务）
- **验证**：迁移执行后可在日志中看到 `applied migration NN` 条目

## 升级回滚

```bash
# 如果新版本有问题，回退到旧版二进制：
sudo systemctl stop binflow
sudo cp /usr/local/bin/binflow-server.bak /usr/local/bin/binflow-server
sudo systemctl start binflow
```

**注意**：如果新版本执行了不可逆的迁移（如删除列），回滚旧版本可能导致启动失败。在生产环境升级前务必执行 export 备份。

## 建议的升级流程

1. **阅读 Release Notes**——查看每个版本的 CHANGELOG / Release Notes，确认是否需要 export/import 迁移
2. **备份**——`binflow-server export -c ... --output /backup/pre-upgrade`
3. **非生产环境验证**——先在 staging 环境执行升级
4. **停机窗口**——MINOR/PATCH 升级通常秒级（二进制替换 + 自动迁移）；MAJOR 升级需要 export/import 时间（与数据量正相关）
5. **验证**——`/readyz` 200 + 核心功能烟测（某制品 PUT/GET 往返）
6. **回滚预案**——旧二进制保留 + 备份保留

## 下一步

- [备份与恢复手册](../admin/backup-restore.md) — export/import CLI 详解
- [FAQ 与故障排查](../faq.md)