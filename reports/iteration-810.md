# Sprint 810 迭代报告 — T-327G 派发（by-hash 保留桶缺陷）；CI 仍待用户侧

**日期**: 2026-08-28 06:06
**上轮**: Sprint 809（06:35）

## T-327G 派发（06:06，dev-go-core）

T-327R 登记的引擎侧缺陷：by-hash 保留桶按条目数裁剪而非按代——historyCycles < 每代拼写数时可裁掉当前代（REST 解锁后暴露）。修为按代分组 + 当代永不裁；含边界回归。

## CI 观察

main 仍 #5（failed）——待用户 PR 合并或开 push 触发器；uat-deploy.sh 修复已在 develop，下次 main 变更生效。

## 状态

M11：27/32 + 补票四张全清（T-327G 在途）。HEAD[develop]=`e6084ea`；main=`8305e67`。**收官阻塞在三裁决 + T-329**。
