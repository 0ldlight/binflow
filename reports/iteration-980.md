# Sprint 980 迭代报告 — T-372 收口（`d0cc28a`，B5 全清）；T-371 派发（conan 线收官段）

**日期**: 2026-08-31 02:4x
**上轮**: Sprint 979（等待轮）

## T-372 → done（develop=`d0cc28a`，双远端）——M13 13/23，B5 全清

trash 树常驻节点：锚册先行（console-m8 §4.3 推翻留痕，落码前完成）+ 树尾 TrashTreeNode（admin∪readonly 同门/普通 user 不渲染/点击与 Enter 跳 M12 页面/过滤域免疫）+ 锚册 v1.17。四闸门 + 6 新腿 + 真栈 +2 + 全量 237/0/23skip；console-size 352,850 维持；契约 diff=0。conductor 复验 tsc 0 + ledger PASS。

## 派发

- **T-371**（dev-go-core，conan 包独占，B7）：D-F2 布局迁移——channelFileName 修正 + 启动期幂等 sweep + repo.Service 窄原语 + sha256 三口径对账（照 ADR-0042 骨架 4 AC）。dep T-360 ✓ T-369 ✓。

## 在途 ×2

- **T-361**：重定标后承证轮（ROUND C 起）在跑。
- **T-371**：本轮派发。

## 状态

M13：**13/23**。在途 ×2。HEAD[develop]=`d0cc28a`。剩余：T-361/T-371 → T-373（中期回归）→ T-374/T-375（docs）→ T-376（release+UAT）→ T-377（终验）→ m13-done。
