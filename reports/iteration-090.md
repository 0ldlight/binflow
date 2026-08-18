# 迭代报告 090 — Sprint 090

- 日期：2026-08-18 13:45（T-35 修复回报触发的收尾轮）
- 里程碑：M2
- conductor：主会话

## 本轮动作摘要

1. **T-35 修复复审通过 → done**，提交 `4344da3`：
   - B1 refs 泄漏窗口：DeleteRepoRefs 后置 + reviewer 探针的确定性复刻测试（straggler PutRefs 不再幸存）；
   - B2 幂等零变更：跳过行写 + GetManifest 存储行读回（零授权重推不能漂移 provenance）；TagRepointed 三方定约；
   - B3 哨兵拆分：ErrInvalidManifest（400 manifest 族）/ErrInvalidCursor（400 分页族），ErrInvalidImage 回归纯语义。
2. 在途：T-37（token 流）、T-41（断连日志）。T-38 前置仅剩 T-37。

## 看板快照（本轮结束时）

- todo: 7 · doing: T-37、T-41 · done: 43 · blocked: 0

## 阻塞与风险

- 无。M2 docker 域基座三层（路由/元数据/服务编排）全部闭环。

## 下轮计划

1. 收 T-37 → 核验 → **T-38 派发（blob 域——M2 最大正确性面，双 reviewer）**；收 T-41 → 核验。
2. T-42（gc 旗标）可在 T-38 编码期间填宽。
