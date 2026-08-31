# Sprint 975 迭代报告 — T-368 收口（`6b209da`，含 conductor 接线）；T-372 派发

**日期**: 2026-08-31 02:1x
**上轮**: Sprint 974（VM main 同步 + GHA 计费呈报）

## T-368 → done（develop=`6b209da`，双远端）——M13 12/23

旋钮聚合一票：folder_download 六字段 + trashcan.retention_days 全链（config 四文件三处齐 wiring——死键教训结构化不可再犯 + `GET /api/v1/system/settings` 回显臂 + 三测试件 + 默认不变红线钉死：M12 关态 403 逐字/14 天窗口全 PASS）。**conductor 接线**照 §5 精确 diff（main.go 两缝消费 cfg——`t368FolderFromConfig` 即接线钉子）；复验 build/fmt/lint 0 + cmd 19.7s + T368 双 PASS。遗留四项：architecture 路由行/§11.45 归 architect、Helm 键族归 release、文档翻转归 T-375、热更新维持重启生效。

## 派发

- **T-372**（dev-frontend，web/ 独占，B5 后半）：trash 树常驻节点——锚册先行（console-m8 推翻条款回写）+ 树顶层入口最小面 + 四闸门/Playwright/axe。

## 在途 ×2

- **T-361**：承证轮（AC1 双轮 + e2e 三连）收尾中。
- **T-372**：本轮派发。

## 状态

M13：**12/23**。在途 ×2。HEAD[develop]=`6b209da`。
