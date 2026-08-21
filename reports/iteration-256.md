# 迭代报告 256 — Sprint 256（M4 全票收口 + DoD 核查轮）

- 日期：2026-08-21 09:35
- 里程碑：M4
- conductor：主会话

## 本轮动作摘要

1. **T-124 → done（D-106-1 闭合）**：FolderMarkerSHA 契约常量 + 按值排除（红绿锚与 QA 错误串逐字一致）+ import/GC mark 同查同修 + 真机 mkdir→export→import full→folder info→sha 逐位→二次 export 全 PASS。conductor 复核（build/lint 0/三锚测试/四包）。提交 `cb7a212`。
2. **M4 全部票收口**：原始票单 T-89~T-107（19 票）+ 增补票 T-108~T-124（17 票）全部 done；看板 done 141、todo 0、doing 0。
3. **M4 DoD 六条核查完成**（reports/m4-dod-checklist.md）：第 1~5 条全部满足（证据逐条落表）；第 6 条（tag m4-done）依安全底线待用户确认。
4. M5 债务清单已归档 BOARD（ADR-0016 实现草案、docker 树数据源、windows 锁运行时、token 审计、docker blob GC 候选、文档补遗等）。

## 看板快照（本轮结束时）

- todo: 0 · doing: 0 · review: 0 · done: 141 · blocked: 0 —— **M4 票面全清**

## 下步

- 向用户请示 tag `m4-done`（含 DoD 核查摘要）；确认后执行 tag + 推送 + ROADMAP 切 M5。
