# 迭代报告 084 — Sprint 084

- 日期：2026-08-18 13:15（T-33 正确性 review 回报轮）
- 里程碑：M2
- conductor：主会话

## 本轮动作摘要

1. **T-33 正确性 review：REQUEST_CHANGES**（3 blocker）：
   - B1：/v2 上「带凭据但验失败」走 authenticate 硬 401 分支 → Basic 挑战 + **E-01 信封**（实测真栈复现）——违反 NFR-S10 域隔离自断言；docker token 过期续期握手会断。修法：按路径分流渲染（/v2 → spec 体 + Bearer realm）。
   - B2：dot-segment 防线只跑 image 段，repoKey 段（segments[0]）不在防线内——`/v2/../etc/passwd` 404 而非 400。实现者自报修复的同款 bug 在第一段复发。
   - B3：`/v2/_catalog` 无显式占位分支，`_` 前缀优先级未裁定。
   - 正面确认：混合编码探针矩阵全过、路由例外互不干扰、RepoTypes 裁定安全、clean-room 通过。
2. 修复单**待架构 reviewer 回报后合并派发**（同票两视角收齐再回炉，避免修一半又改）。
3. 在途：T-33 架构 reviewer、T-35 reviewer。

## 看板快照（本轮结束时）

- todo: 8 · doing: 0 · review: T-33（正确性已回 REQUEST_CHANGES、架构在途）、T-35 · done: 41 · blocked: 0

## 下轮计划

1. 收 T-33 架构 review + T-35 review → 合并裁决 → 修复单（T-33 三 blocker + 架构面若有）。
2. T-33 修复后终裁 → T-37/T-41 派发。
