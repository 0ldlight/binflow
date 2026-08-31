# Sprint 969 迭代报告 — T-369 收口（`12c3f48`，复活即达）；T-368 派发

**日期**: 2026-08-31 01:0x
**上轮**: Sprint 968（配额窗后复位）

## T-369 → done（develop=`12c3f48`，双远端）——M13 11/23

conan D8 整树删翻转：`serveV1RecipeDelete` 唯一生产码改动（坐标根 subtreeExists→Delete 整树删）；M12 断言就地翻转（豁免票号注明）+ table-driven 双 case×7 读腿 + **真客户端双绿**（1.66.0 访问日志全 D8 序列 6.95s + 2.31.2 7.12s；全量 18.6s + -race 50.4s）。conan.md §3.2 D8 分歧行退役。低置信裁量两处测试固化+登记（miss 文案/miss 基面取路径基）。conductor 复验：build 0 + D8 定向 2.3s 绿。遗留：docs/user 两处滞后表述归 T-375 回刷。

## 派发

- **T-368**（dev-go-core，B5 前半）：旋钮聚合一票——folderDownloadConfig 六字段 + trashcan.retention_days（默认不变红线，L14 断言开关化）。

## 在途 ×2

- **T-361**（de-flake）：复活续跑中。
- **T-368**：本轮派发。

## 状态

M13：**11/23**。在途 ×2。HEAD[develop]=`12c3f48`。
