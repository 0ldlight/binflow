# 迭代报告 072 — Sprint 072

- 日期：2026-08-18 11:00（T-29/T-30 双完成触发的收尾轮）
- 里程碑：M2 云原生旗舰 Docker Registry v2
- conductor：主会话

## 本轮动作摘要

1. **T-29（M2 PRD）收尾**：核验通过 → done，与 T-30 同车提交 `6864b99`。
   - 515 行 FR-7~FR-14 + DE-01~DE-17 兼容矩阵 + D01~D24 真实客户端验收命令；
   - M1 遗留 O1~O4 逐条定界（O4 维持现语义归档 M4）；
   - Q1（/v2 冲突）两案七维度对比，倾向根级例外、标注待定。
2. **T-30（M2 架构增量）收尾**：核验通过 → done。
   - **ADR-0010 定案：/v2 根级例外**——与 PRD 独立收敛同一方案（架构理由：反代 rewrite 裸机 docker 不可用违背核心场景；token realm 免重写是决定性附带收益）；
   - §5.3 docker adapter 端点映射 13 行（offset 协议态/mount 零拷贝/mediaType 白名单）；
   - 002 迁移三表 DDL 定稿；token 复用 TokenRegistry + scope 映射。
3. **Q1 状态更新**：架构师已定案（ADR-0010），PRD「待定」标记在 T-31 规格校准后随 v1.1 一并回写（保持「规格校准后回写」的 M1 惯例，避免反复改文档）。
4. T-31（docker-registry 规格）继续在途——三处「待 T-31 校准」点挂 §12。

## 看板快照（本轮结束时）

- todo: 0 · doing: T-31 · done: 34（M1 32 + T-29/T-30）· blocked: 0

## 阻塞与风险

- tech-lead 拆票等 T-31（blob upload 会话语义的单段 name 404 等三处校准点影响首批票 AC）。

## 下轮计划

1. 收 T-31 → 核验 → 派 tech-lead 拆 M2 工程 ticket（/v2 挂载 + blob upload 核心先行）。
2. PM v1.1 回写（Q1 定案 + 校准值）与拆票并行。
