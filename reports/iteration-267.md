# Sprint 267 迭代报告

**日期**: 2026-08-21
**上轮**: sprint 266（批 3 重派 — T-134/T-135/T-136/T-137）
**本轮焦点**: 收批 3 四线 → 提交 → 派批 4

## 阶段 0 — 复位

- 会话从上轮批 3 产出后继续。agent 已产生代码但无日志报告（agent 丢失，仅 code 产出）
- 核验发现 T-135/T-136/T-137 产出完整（compose GA / Helm Chart / K8s 清单）
- T-134 docker 树视图：BE+FBE 代码 OK，Playwright 测试存在但有 `simpleSha256` bug（`require('crypto')` 在 ESM 下不可用）

## 阶段 3 — 收批 3

### T-135 docker-compose GA
- deploy/compose/docker-compose.yml（具名卷 / :? 强制口令 / healthcheck / nginx profile）
- 五协议烟测通过 → goreleaser CI
- 报告: reports/agents/T-135.md

### T-136 Helm Chart
- charts/binflow/ 全对象（Deployment/PVC/Service/Ingress/ConfigMap/Secret/HPA）
- values.schema.json 单副本拦截（replicaCount maximum:1）
- 报告: reports/agents/T-136.md

### T-137 原生 K8s 清单
- deploy/k8s/（Deployment/PVC/Service/Secret/Ingress/kustomization）
- 安全基线：runAsNonRoot + secretKeyRef + 无 :latest
- 报告: reports/agents/T-137.md

### T-134 docker 树视图
**后端**:
- `internal/httpapi/storage.go` — `dockerTagsForFolder` 函数 + `?docker_tags=1` 参数
- 从 storage API 面获取 tag→digest 映射，**不触发 /v2/* 请求**

**前端**:
- `web/src/pages/repositories/tree/lib.ts` — `ChildNode.tags`、`DockerTagsMap`、`listChildren` isDockerRepo 参数
- `web/src/pages/repositories/tree/TreePage.tsx` — docker 列头（标签/摘要）、tag badge、untagged 徽标
- `web/src/pages/repositories/tree/tree.css` — badge 样式
- `web/src/components/AppShell.tsx` — topbar 帮助链接（DC-02）

**修复**: `simpleSha256` ESM 兼容（`require('crypto')` → `import { createHash } from 'crypto'`）

**Playwright 测试** (web/e2e/t134-g32.spec.ts): 6 例
| 用例 | 描述 | 断言 |
|------|------|------|
| G32a-1 | 零 /v2/* 请求 | 全程无新 /v2/ 请求 |
| G32a-2 | tag badge 渲染 | tag-badge-latest + tag-badge-v1 |
| G32a-3 | 数据一致性 | UI tag 集 = tags/list |
| G32b-1 | 列头 摘要/标签 | digest 截断渲染 |
| G32b-2 | root 级浏览 + breadcrumb | 零 /api/search |
| DC-02 | 帮助链接 | topbar-help → /binflow/docs/ |

**报告**: reports/agents/T-134.md

### 提交
- 提交 `cfe8dab` — 34 files / +2102 / -19 lines

## 阶段 3 — 批 4 派发

派发四票，area 互不重叠：

| 票据 | 角色 | Area | 依赖 | 状态 |
|------|------|------|------|------|
| T-138 FR-39 systemd 单元 | release-engineer | contrib/systemd/ | T-127 ✅ | 已派 |
| T-139 FR-40 离线安装包 | release-engineer | deploy/offline/ | T-127/T-132/T-136/T-137 ✅ | 已派 |
| T-140 FR-47 URI 基址修正 | dev-go-core | internal/httpapi | — | 已派 |
| T-142 FR-41 API 文档 | tech-writer | docs-site/docs/ | T-129/T-128 ✅ | 已派 |

## 看板快照

| 区域 | 票据 |
|------|------|
| todo | T-141/T-143~T-147（6 张） |
| doing | T-138/T-139/T-140/T-142（4 张 — 批 4） |
| done | T-1~T-137（157 张，批 1~3 全完成） |

## 风险与阻塞

- **无阻塞**: 批 3 全部准时完成，依赖链消化完毕
- **Agent 一致性**: 所有 subagent 不带 `subagent_type`，使用通用 agent 继承会话模型

## 下轮计划

1. 收批 4（T-138/T-139/T-140/T-142）→ QA smoke + review
2. 批 5 派发 —— T-141（安全审计）、T-143（QA 反转表）、T-144（QA 基线复跑）
3. T-147 GA 收口派生