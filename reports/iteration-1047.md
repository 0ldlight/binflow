# Sprint 1047 迭代报告 — T-393 收口（`3cdeb43`，K59/K60 双锚定案）；T-394 服务端小票包派发

**日期**: 2026-08-31 13:2x
**上轮**: Sprint 1046（等待轮）

## T-393 → done（develop=`3cdeb43`，双远端）——M14 5/22

规格双锚：**K59 = 409**（nuget.md §5.4 六断言——flatcontainer GET-only/catch-all 转 v2/BinFlow as-built 403 不一致对照，高 18/中 4/低 1）+ **npm.md 新建 K60 六条定案**（`/-/user/org.couchdb.user:<name>` 族全量规格——本机 npm 实物源码 + live 抓包对拍；**唯一修复面 = httpapi 写门豁免该族 + login 永不 409 不变量**）。附带两发现（pacote 横幅流量/.npmrc 端口参与匹配）。clean-room 合规。

## 派发

- **T-394**（dev-go-core，B3 服务端小票包）：① npm legacy login 修复（K60 豁免窄域 + 真客户端首跑 + npm.md 如实标注回写——T-374 L1 闭环）② helm PVC keep 注解 ③ 启动日志措辞一行。

## 在途 ×2

- **T-383**（建仓收口小票）：推进中。
- **T-394**：本轮派发。

## 状态

M14：**5/22**。在途 ×2。HEAD[develop]=`3cdeb43`。
