---
title: Helm Chart（Kubernetes）
sidebar_position: 13
---

# Helm Chart（Kubernetes）

> 适用版本：M5 GA v1.0.0（charts/binflow/；T-137, FR-38, PB-06）+ **M14 增补**（PVC `helm.sh/resource-policy: keep` 注解恒注入——卸载幸存/手动清卷口径，T-376 裁决 / T-394）。
> 本文命令属「文档命令，待 QA 复跑」——核心路径与 Chart 产物、values.yaml、NOTES.txt 一致；M14 keep 注解面已实测（`helm lint` + `helm template` 三态 + `--dry-run` NOTES 渲染，2026-08-31，T-397——集群内 `helm uninstall` 幸存 UAT 归 T-399）。

`charts/binflow/` 提供了标准的 Helm Chart，支持单副本部署（架构 section 9 明确 M1 无 HA）、PVC 持久化、可选 Ingress、镜像拉取密钥、资源限制和 HPA（单副本约束）。

## 前置要求

- Kubernetes 集群（>= 1.28）
- Helm >= 3.12
- kubectl 已配置集群上下文
- 对目标 namespace 有部署权限

## 安装步骤

### 1. 添加 Helm 仓库（或本地安装）

```bash
# 从本地 chart 目录安装（开发/评估推荐）
helm install binflow ./charts/binflow

# 从 OCI 仓库安装（发布后）
helm install binflow oci://ghcr.io/lzwzzy/charts/binflow --version 1.0.0
```

### 2. 设置管理员密码

```bash
helm install binflow ./charts/binflow --set admin.password=my-secret-password
```

默认密码为 `password`（values.yaml 中的占位值），**生产环境必须覆盖**。

### 3. 等待 Pod 就绪

```bash
kubectl wait --for=condition=available --timeout=60s deployment/binflow
```

### 4. 获取管理员密码

```bash
export BINFLOW_ADMIN_PASSWORD=$(kubectl get secret binflow-secret \
  -o jsonpath="{.data.BINFLOW_ADMIN_PASSWORD}" | base64 -d)
echo "Admin password: $BINFLOW_ADMIN_PASSWORD"
```

### 5. 端口转发并验证

```bash
kubectl port-forward svc/binflow 8080:8080 &
curl -s http://127.0.0.1:8080/readyz
# ok
```

## 自定义 values

创建 `my-values.yaml`：

```yaml
# 管理员密码
admin:
  password: "strong-password-here"

# 镜像配置
image:
  repository: ghcr.io/lzwzzy/binflow
  tag: "v1.0.0-alpine"
  pullPolicy: IfNotPresent

# 持久化存储
persistence:
  enabled: true
  size: 50Gi
  storageClass: "standard"

# 服务类型（ClusterIP / NodePort / LoadBalancer）
service:
  type: ClusterIP
  port: 8080

# Ingress（可选）
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: binflow.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: binflow-tls
      hosts:
        - binflow.example.com

# 资源配置
resources:
  limits:
    cpu: 2000m
    memory: 1Gi
  requests:
    cpu: 100m
    memory: 256Mi

# 服务配置
config:
  baseUrl: "https://binflow.example.com"
  logLevel: info
  security:
    anonymousAccess: true
```

安装：

```bash
helm install binflow ./charts/binflow -f my-values.yaml
```

升级：

```bash
helm upgrade binflow ./charts/binflow -f my-values.yaml
```

## Chart 结构

```
charts/binflow/
├── Chart.yaml              # Chart 元数据（version: 1.0.0, appVersion: 1.0.0）
├── values.yaml             # 默认配置值
├── values.schema.json      # JSON Schema 校验（强制 replicaCount <= 1）
├── templates/
│   ├── _helpers.tpl        # 模板辅助函数
│   ├── deployment.yaml     # Deployment（单副本，Recreate 策略）
│   ├── service.yaml        # Service（ClusterIP 默认）
│   ├── pvc.yaml            # PVC（RWO，20Gi 默认）
│   ├── secret.yaml         # Secret（admin 密码 + 可选 remote credentials key）
│   ├── configmap.yaml      # ConfigMap（binflow.yaml 服务器配置）
│   ├── ingress.yaml        # Ingress（可选，默认禁用）
│   ├── hpa.yaml            # HPA（可选，默认禁用，maxReplicas=1）
│   ├── serviceaccount.yaml # ServiceAccount
│   └── NOTES.txt           # 安装后提示
```

## 关键配置说明

### 镜像变体

```yaml
image:
  repository: ghcr.io/lzwzzy/binflow
  tag: "v1.0.0-alpine"       # 或 v1.0.0-distroless
```

镜像标签说明：
- `v1.0.0-alpine`：基于 alpine:3.24，含 shell + wget，适合调试
- `v1.0.0-distroless`：基于 gcr.io/distroless/static-debian13:nonroot，无 shell，最小攻击面

### 存储

默认创建 20Gi RWO PVC。生产环境建议根据制品规模调整：

```yaml
persistence:
  enabled: true
  size: 200Gi
  storageClass: "fast-ssd"
```

也可使用已有 PVC：

```yaml
persistence:
  enabled: true
  existingClaim: "my-existing-pvc"
```

**注意**：SQLite WAL 模式要求独占写入——多副本共享 PVC 被明确禁止（架构 section 9），values.schema.json 强制 `replicaCount \<= 1`。

### 安全上下文

Pod 以非 root 用户运行（distroless 为 uid 65532）。默认安全上下文可在 values 中自定义：

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  readOnlyRootFilesystem: true
```

### Ingress

启用 Ingress 时注意路径规则：

```yaml
ingress:
  enabled: true
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "0"   # 无限制（制品上传）
    nginx.ingress.kubernetes.io/proxy-read-timeout: "35" # 对齐优雅关闭
```

路径要求（与 ADR-0008/0010 一致）：
- `/readyz`、`/healthz` —— 无前缀
- `/v2/` —— Docker Registry v2 API（根级例外）
- `/binflow/` —— 所有 BinFlow 端点（管理 API + 内容路径）

## 卸载

```bash
helm uninstall binflow
```

**PVC 有意幸存（M14 起显式保证）**：chart 自建的 PVC 带 `helm.sh/resource-policy: keep` 注解（恒注入——即使用户在 `persistence.annotations` 里写了同键 `remove`，冲突时 keep 单键胜出，T-376 裁决 / T-394）。因此 `helm uninstall` 只删工作负载，**数据卷与制品数据保留**——这是防误删的刻意姿态，不是没卸干净；同 namespace 重新 `helm install` 同名 release 会复用幸存的 PVC（PVC 名 = `<release>-binflow`）。`NOTES.txt` 安装完成时会印出本说明与清卷命令。

彻底删除数据须手动清卷（二选一）：

```bash
# 按名（NOTES.txt 同款；PVC 名 = <release>-binflow）
kubectl delete pvc --namespace <ns> binflow-binflow
# 按标签（release 名非 binflow 时把 instance= 值换成实际 release 名）
kubectl delete pvc -l app.kubernetes.io/instance=binflow
```

两条边界：`persistence.existingClaim` 复用外部 PVC 时本节不适用（chart 不管那个卷的死活）；keep 姿态对 `helm delete`/`helm uninstall` 两种拼法同样生效，**没有配置项可以关掉**——要「卸载即删卷」的自动化请在上层 CI 里显式补 `kubectl delete pvc`。

## 下一步

- [原生 K8s 清单](k8s.md) — 不用 Helm 的 kubectl apply 路径
- [离线安装](offline.md) — air-gapped 环境
- [docker-compose 部署](compose.md) — 单机编排
- [备份与恢复手册](../admin/backup-restore.md) — export/import CLI