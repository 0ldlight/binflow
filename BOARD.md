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

- **T-1** [P0] M1 里程碑 PRD `role:product-manager` `area:docs/prd`
  AC: ① docs/prd/milestone-1.md 覆盖 M1 全部条目，每功能有可验证 AC ② 兼容性矩阵到真实客户端命令级 ③ M1 边界（不做 remote/virtual/控制台/多协议）明确
- **T-2** [P0] M1 架构设计与元数据 schema 定稿 `role:architect` `area:docs/design`
  AC: ① docs/design/architecture.md 含包结构/存储引擎设计/SQLite schema DDL/适配器 SPI/配置模型 ② 新决策追加 ADR-0005+，不推翻既有 ADR ③ 未参考 reverse-src/
- **T-3** [P0] Artifactory M1 行为规格（clean-room） `role:reverse-engineer` `area:docs/reverse`
  AC: ① rest-api.md / storage-layout.md / config-formats.md / repo-semantics.md(local) 四份规格产出 ② 每条结论标注置信度 ③ 无代码复制/逐行翻译，引用只到类名/方法名级

## 👀 评审中（review）

（空）

## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

（空）

## 🚫 阻塞（blocked）

（空）
