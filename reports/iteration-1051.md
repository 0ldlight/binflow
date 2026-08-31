# Sprint 1051 迭代报告 — T-394 收口（`d6395c1`，npm login 真客户端绿）；K56 品牌资产生产化派发

**日期**: 2026-08-31 13:4x
**上轮**: Sprint 1050（等待轮）

## T-394 → done（develop=`d6395c1`，双远端）——M14 7/22

服务端小票包三落：① **npm legacy login**（窄域豁免谓词〔repo 类型==npm 承重墙〕+ **裸交互 login 真客户端全链绿**——T-374 L1 闭环 + M13「不可用」注记显式作废）；② helm PVC keep 注解（四态渲染）；③ 启动措辞（钉死测试+活体）。httpapi 274.5s + helm lint 0 + 全仓 lint 0。conductor 复验 build/T394×5/helm lint 全绿。遗留：400-vs-401 逐字另裁；whoami 403 误导性 E403 归 conductor。

## 派发

- **K56 品牌资产生产化**（ux-designer 插空——FE lane 被 T-384 占用）：候选 1 转正（Q2 窗已闭）wordmark/lockup 转 path + npm/go 图标转 path + conan 换色 #669ACC（T-381 L02）——为 T-389 接线解锁。

## 在途 ×2

- **T-384**（用户/组收口小票）：推进中。
- **K56**：本轮派发。

## 状态

M14：**7/22**。在途 ×2。HEAD[develop]=`d6395c1`。
