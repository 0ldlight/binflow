# 迭代报告 094 — Sprint 094

- 日期：2026-08-18 14:20（T-42 完成触发的收尾轮）
- 里程碑：M2
- conductor：主会话

## 本轮动作摘要

1. **T-42（gc 旗标 + mark 扩容）收尾**：净树核验通过 → done，提交 `69a6039`。
   - gc -c 复用 serve 配置链、--grace-hours 与 days 并存；
   - mark = nodes ∪ docker_refs：agent 论证选 ListRefsByManifest 聚合而非 RefsByBlob（后者会退回 T-9 废弃的反连接路线）——接口选型有据；
   - 真栈冒烟全留档（refs-held 存活/级联删后转候选/orphan 回收/-c 缺文件 exit 1）；
   - 主仓瞬断归属正确（T-37 WIP，agent 净树隔离 + conductor 复现双确认）。
2. 在途：T-37（token 流，编码中——docker 包 WIP 可见）。
3. 遗留登记：T-42 lint 有网补跑；子命令 --help exit 码小票（M1 既有）。

## 看板快照（本轮结束时）

- todo: 6 · doing: T-37 · done: 45 · blocked: 0

## 阻塞与风险

- T-38 等 T-37（唯一在途前置）。

## 下轮计划

1. 收 T-37 → 核验（D04/D04b/D04c + login 冒烟 + 12 包恢复）→ **T-38 派发（blob 域，双 reviewer）**。
