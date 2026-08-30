# Sprint 983 迭代报告 — T-371 收口（`7bb3039`，conan 线收官）；T-375 文档大票派发

**日期**: 2026-08-31 03:0x
**上轮**: Sprint 982（T-361 AC1 双绿登记）

## T-371 → done（develop=`7bb3039`，双远端）——M13 14/23

D-F2 迁移三件套：channelFileName 单数 trim 修复（新写规格布局）+ SweepV1FilesLayout 启动 sweep（谓词三重约束/零仓零扫描/每仓 INFO/失败 fail-the-boot）+ **repo.Service.RewriteSubtreePrefix 窄原语**（零副作用契约）。真客户端 1.66 三测（sweep 行 moved=3 dedup=1 conflicts=0 + 腐蚀面恢复 + 重装摘要全等）+ 2.31.2 全链。**conductor 接线**（runServe 监听器前插 sweep）+ 复验 build/fmt/lint 0 + cmd/conan 绿。ADR-0042 四 AC 对照表在日志。

## 派发

- **T-375**（tech-writer，docs/user + docs-site）：文档增量五类——webhook 使用指南（含 HMAC 验签示例）+ HelmOCI 接入 + 两旋钮 + api-reference + FAQ + conan 用户文档滞后回刷。deps T-362~T-368 全齐 ✓。

## 在途 ×2

- **T-361**：e2e 三连复跑 + 报告回填（收尾）。
- **T-375**：本轮派发。

## 状态

M13：**14/23**。实现票仅剩 T-361（收尾）与 T-378（Q3 翻转小票候派）。HEAD[develop]=`7bb3039`。
