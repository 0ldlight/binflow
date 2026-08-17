# 任务看板（BOARD）

> 唯一事实来源。**只有主会话（conductor）可以写本文件**，所有 subagent 只读。
> ticket 由 tech-lead 生成、主会话录入。当前里程碑：M1 内核基座（见 ROADMAP.md）。

## 票据格式

```
- **T-<编号>** [P0|P1|P2] 标题 `role:<agent类型>` `area:<Go包/页面组/部署目标>` `dep:T-x,T-y`
  AC: ①可验证的验收标准 ②… ③…
```

- 优先级：P0 阻塞他人/当前里程碑关键路径；P1 本里程碑应完成；P2 可延后。
- `area` 示例：`internal/storage`、`internal/adapter/docker`、`internal/auth`、`web/src/pages`、`deploy/helm`。
  同一轮并行派发的 ticket，area 不得重叠。
- `dep` 列出必须先完成的 ticket；协议适配类 ticket 必须依赖对应的逆向规格票。

## 📥 待办（todo）

（空——待 tech-lead 依据 PRD/架构/逆向规格拆票后录入）

## 🔨 进行中（doing）

- **T-3** [P0] Artifactory M1 行为规格（clean-room） `role:reverse-engineer` `area:docs/reverse`
  AC: ① rest-api.md / storage-layout.md / config-formats.md / repo-semantics.md(local) 四份规格产出 ② 每条结论标注置信度 ③ 无代码复制/逐行翻译，引用只到类名/方法名级
  状态：2026-08-17 agent 被 API 限流（429，额度 21:19 重置）击落于工作中途，无磁盘产出；待额度恢复重派。
- **T-4** [P0] PRD v1.1 回写 8 项已定案决策 `role:product-manager` `area:docs/prd` `dep:T-1`
  AC: ① Q1 全文 URL 改 `/binflow` 前缀（C01~C30、E-xx 表、场景 D 表述）② Q2 匿名读默认开，FR-5/NFR 联动修订 ③ Q3~Q8 落定，§9 改「已决决策」表
  状态：2026-08-17 PM 被 API 限流击落于回写开始前，无磁盘产出；待额度恢复重派。
- **T-5** [P0] 架构文档对齐 4 项用户决策 `role:architect` `area:docs/design` `dep:T-2`
  AC: ① §7.1 路由表改 `/binflow` 统一前缀 ② 匿名读默认开（auth 中间件与 ACL 决策）③ §6 种子数据改 BINFLOW_ADMIN_PASSWORD 优先、缺省 password ④ module 路径 github.com/lzwzzy/binflow 入文档（新决策追加 ADR-0008+）
  状态：2026-08-17 architect 被 API 限流击落于修订开始前，无磁盘产出；待额度恢复重派。

## 👀 评审中（review）

（空）

## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

- **T-1** [P0] M1 里程碑 PRD `role:product-manager` `area:docs/prd` — done 2026-08-17
  产出 docs/prd/milestone-1.md（486 行）：FR-1~FR-6 全 AC、26 端点兼容矩阵、C01~C30 验收命令。核验通过。
  8 项开放问题已定案（Q1 `/binflow` 前缀；Q2 匿名读默认开；Q3 缺省 password；Q4 如实版本；Q5 纯 Go SQLite；Q6 Range P2；Q7 github.com/lzwzzy/binflow；Q8 devops 起草）→ 回写为 T-4。
- **T-2** [P0] M1 架构设计与元数据 schema 定稿 `role:architect` `area:docs/design` — done 2026-08-17
  产出 docs/design/architecture.md（12 节）+ ADR-0005/0006/0007（零 CGO 依赖基线 modernc.org/sqlite、blob 布局与落盘协议、迁移机制）。核验通过。
  决策对齐修订（/binflow 前缀、匿名读、Q3 口令、Q7 路径）→ T-5。

## 🚫 阻塞（blocked）

（空）
