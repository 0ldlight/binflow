# Sprint 408 迭代报告 — T-217 双 review 收敛 B1，返修在途

**日期**: 2026-08-23 10:21
**上轮**: Sprint 407（T-217 完成 + 偏离裁决）；其间双 review 齐。

## 双 review 结论（同洞收敛，置信度封顶）

- **review-a（架构）REQUEST_CHANGES**：B1 = 替换臂只验 body 不验存量（与 DELETE 臂不对称推演）；偏离裁可独立确认（守卫精确两处、零提权证明属实、usage ∨-臂与族 7 注记一致）。
- **review-c（正确性）REQUEST_CHANGES**：同一 B1 + **对抗探针实证**（carol 同名替换 t-ent → 201 → dave 覆盖集外 read 被吊销；对照组 DELETE 正确 403）。独立红绿 ×4 全过；service 门放宽零提权 grep 证实（非测试调用点仅 httpapi 四处）。

## 处置

合并返修已派（原 agent 续跑）：union(body.repos, 存量) 判定 + 矩阵补腿（403+清单字节不变）+ 红绿自证；non-blocking #1（principal 名字枚举面）登记不修。评审报告归档 T-217-review-a/c.md（`987fe4c`/`9e0c062`）。

## 阶段 0（本轮触发时）

在途 ×1：T-217 返修（transcript 活跃 10:20）。HEAD=`9e0c062`。

## 下轮计划

返修收 → conductor 复验（新腿 + EXPECT=1 矩阵 + race）→ T-217 关账 → 波 5 三线派发（T-218 前端含 governance.ts 词表 / T-219 step-up 含 token handler p.Admin 统一 / T-221 QA-I 含 usage 翻转腿与 V 序列补充）。
