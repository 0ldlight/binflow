# Sprint 1144 迭代报告 — T-411/T-414 双双收官（M15 5→6/25）；B3 双票派发（T-413 主轴 + T-412 副线）

**日期**: 2026-09-01 12:5x
**上轮**: Sprint 1143（等待轮③）

## T-411 → done（执行内核）

internal/metadata/aqlquery.go（NodeQuery IR + NodeQueryer 缝 + 参数化 SQL 编译）+ internal/search/plan.go（AST→IR）+ match.go（转义内核）。**P95 大幅余量**（item 7.7-38.5ms / property join 37-107ms vs 预算 500/800）；性能腿抓出真缺陷（双 DISTINCT 物化 1.9s→EXPLAIN 兑现索引消费）；**ADR 第三处分歧（path=父目录）**→ 勘误已派 T-408 作者。

## T-414 → done（FE 早波）

三页列选器（T-387 形态复用）+ member-pop 对比度清账；**锚册 v1.27**（28 名）；四门 + 净实例 284✓（唯一红=在册假阳性串行绿）+ 服务端 diff=0 + SPA +2.2KB。

## 派发（B3 双 lane）

- **T-413**（P0 主轴：ACL 织入 + K63 三件门——408 超时定案携带）
- **T-412**（P1 副线：virtual 聚合 service——FR-21-AC8 兑现，与 search 错峰）
- T-408 作者：path 勘误第三笔在途

## 状态

M15：**6/25**（T-413/T-412 在途）。
