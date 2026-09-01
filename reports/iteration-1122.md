# Sprint 1122 迭代报告 — UAT AFTER 全标落档（M14 线上收口完毕）；M15 立项稿落盘

**日期**: 2026-09-01 09:0x
**上轮**: Sprint 1121（等待轮⑤）

## UAT AFTER（T-399 清单执行完毕——deploy_uat 已于 08:5x 完成）

| 标记 | 结果 |
|---|---|
| 换装翻转 | `uat.13ca2d6` → **`uat.4cb3fa6`**（= m14-done 合并提交）；healthz/ui 200 |
| M1 governance docs push-replication | BEFORE=0 → **3** ✅（T-398 治理指南随 PR 内嵌） |
| M2 favicon 指纹 | `favicon-996d22f6.ico` ✅（品牌面维持） |
| M3 settings M13 键族 | 200；folder_download 在位 + trashcan.retention_days=14 ✅ |
| M4 T-405 新路由 | PUT /v1/replications/99999 → **404 "replication config not found: 99999"** 逐字 ✅ |
| M5 制品 roundtrip | create 200 → **PUT 201** → GET 回读 → del 200 → gone 404 ✅；scratch 仓清理完毕（仓库基线复原双仓） |

**插曲（零产品缺陷，留痕）**：首两轮 roundtrip 的 put 显示 405——journal 定位系我的 curl 漏写 `-X PUT`（`--data-binary` 默认 POST，内容面正确拒 405）；真 PUT 日志全 201。教训：嵌套 ssh 单行里的多 curl 命令逐条核对 -X 动词（本会话第三类引号坑）。

## M15 立项稿

milestone-15.md 已落盘（PM 收尾中——ROADMAP 立项行/终稿未报）；完成通知后 conductor 审 → tech-lead 拆票。

## 状态

**M14 全线闭合**（22 票 + 增补 + 热修 + 终验 + tag + PR #48 + UAT AFTER 六标全绿）。M15 待审。
