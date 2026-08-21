---
title: 原生 K8s 清单
sidebar_position: 14
---

# 原生 K8s 清单（kubectl apply -k）

> 适用版本：M5 GA v1.0.0（deploy/k8s/；T-136, FR-37, PB-05）。
> 本文命令属「文档命令，待 QA 复跑」——核心路径与 K8s 清单产物一致。

不使用 Helm 时，`deploy/k8s/` 提供了一套完整的 Kustomize 清单，`kubectl apply -k` 即可部署。

## 前置要求

- Kubernetes 集群（>= 1.28）
- kubectl >= 1.28
- 镜像已推送至集群可访问的 registry（默认 `ghcr.io/lzwzzy/binflow`）

## 安装步骤

### 1. 准备 Secret

编辑 `deploy/k8s/secret.yaml`，替换管理员密码占位符：

```yaml
stringData:
  BINFLOW_ADMIN_PASSWORD: "<replace-with-a-strong-password>"
```

远程代理密钥（可选，M3+）：

```bash
openssl rand -base64 32
# 将输出填入 secret.yaml 的 BINFLOW_REMOTE_CREDENTIALS_KEY
```

### 2. 配置镜像

编辑 `deploy/k8s/kustomization.yaml`，指定镜像：

```yaml
images:
  - name: binflow
    newName: ghcr.io/lzwzzy/binflow
    newTag: v1.0.0-distroless
```

镜像变体选择：

| 变体 | 标签 | 说明 |
|---|---|---|
| distroless（推荐） | `v1.0.0-distroless` | 无 shell，Go 探针做 liveness/readiness |
| alpine | `v1.0.0-alpine` | 含 shell + wget，可调试 |

如果使用 alpine 变体，需要将 deployment 中的探针从 `exec` 改为 `httpGet`：

```yaml
livenessProbe:
  httpGet:
    path: /readyz
    port: http
readinessProbe:
  httpGet:
    path: /readyz
    port: http
```

### 3. 应用清单

```bash
kubectl apply -k deploy/k8s/
```

Kustomize 会按照以下顺序创建资源：

1. `secret.yaml` — Secret（管理员密码 + 可选远程凭证密钥）
2. `pvc.yaml` — PersistentVolumeClaim（RWO，20Gi 默认）
3. `deployment.yaml` — Deployment（单副本，Recreate 策略）
4. `service.yaml` — Service（ClusterIP，端口 8080）

### 4. 等待 Pod 就绪

```bash
kubectl rollout status deployment/binflow
# deployment "binflow" successfully rolled out
```

### 5. 验证

```bash
# 端口转发
kubectl port-forward svc/binflow 8080:8080 &

# 健康检查
curl -s http://127.0.0.1:8080/readyz
# ok

# 获取管理员密码
kubectl get secret binflow-secret -o jsonpath="{.data.BINFLOW_ADMIN_PASSWORD}" | base64 -d
```

## 可选：启用 Ingress

`deploy/k8s/ingress.yaml` 默认被注释（Kustomize 不包含它）。取消 `kustomization.yaml` 中 ingress.yaml 的注释，并编辑 Ingress 的 host：

```yaml
# kustomization.yaml
resources:
  - secret.yaml
  - pvc.yaml
  - deployment.yaml
  - service.yaml
  - ingress.yaml   # 取消注释
```

## 使用 Overlay（GitOps 友好）

创建独立的 overlay 目录，不改动原 base：

```bash
mkdir -p deploy/k8s/overlays/prod
```

`deploy/k8s/overlays/prod/kustomization.yaml`：

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../.

patches:
  - target:
      kind: Deployment
      name: binflow
    patch: |
      - op: replace
        path: /spec/replicas
        value: 3       # 注意：SQLite 单写，replicas > 1 仅供 ingress 测试
  - target:
      kind: Ingress
      name: binflow
    patch: |
      - op: replace
        path: /spec/rules/0/host
        value: binflow.mycompany.com

images:
  - name: binflow
    newName: ghcr.io/lzwzzy/binflow
    newTag: v1.0.0-distroless
```

应用：

```bash
kubectl apply -k deploy/k8s/overlays/prod
```

## 清单文件说明

| 文件 | 功能 | 自定义项 |
|---|---|---|
| `secret.yaml` | Secret | 管理员密码、远程凭证密钥 |
| `pvc.yaml` | 持久化卷 | 存储大小、StorageClass |
| `deployment.yaml` | 工作负载 | 镜像、环境变量、探针、资源限制 |
| `service.yaml` | 网络入口 | Service 类型、端口 |
| `ingress.yaml` | 可选 Ingress | Host、TLS、annotations |
| `kustomization.yaml` | Kustomize 入口 | 资源列表、镜像标签、标签 |

## 安全说明

- 部署使用 `enableServiceLinks: false`——避免 K8s Service 环境变量与 BinFlow 的 `BINFLOW_` 配置命名空间冲突
- Pod securityContext: `runAsNonRoot: true`，distroless 镜像执行 uid 65532
- Secret 中的密码通过 `secretKeyRef` 注入，不进入 Pod spec 明文（`valueFrom`）
- 单副本部署（Recreate 策略）：SQLite WAL 模式要求独占写入

## 卸载

```bash
kubectl delete -k deploy/k8s/
```

PVC 不会自动删除（Customize `prune` 需要 `--prune` 标志）。手动清理：

```bash
kubectl delete pvc binflow-data
```

## 下一步

- [Helm Chart](helm.md) — 功能更完善的 Helm 版本
- [离线安装](offline.md) — air-gapped 环境（kind/helm 模式）
- [备份与恢复手册](../admin/backup-restore.md) — export/import CLI