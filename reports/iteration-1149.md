# Sprint 1149 迭代报告 — T-412 收官（M15 7/25，FR-21-AC8 兑现）；T-416 派发

**日期**: 2026-09-01 13:3x
**上轮**: Sprint 1148（等待轮④）

## T-412 → done

virtual 聚合面打开：`listVirtual` 成员并集（**与 pull 解析同一顺序源**——同一次顺序计算，不引入第二通道；同名路径首成员胜；remote 仅缓存行）+ `getVirtualFolder`（合成 display-only marker 零落库——ADR-0013 不污染成员）。**断言反转② BE 腽**（t406 拒绝钉翻转 + 新增 6 测试矩阵）。repo 全包 race 454.8s 绿 + httpapi 绿；全树 race 待 T-413 收口后 conductor 补跑。

## 派发

- **T-416**（virtual FE 树消费——断言反转② FE 腿，锚册 v1.28）入 FE lane。
- **T-413**（ACL+K63 门）继续在途。

## 状态

M15：**7/25**（T-413/T-416 在途）。
