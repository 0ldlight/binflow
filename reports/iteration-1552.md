# 迭代 1552 · R13 战报（BinFlow AI Software Factory）

日期：2026-09-30。conductor 闭卷报告（BOARD.md 冻结只读，任务权威源=Linear binfloow）。

## 一、票池与完成

R13 修复票池 BIN-75..85（11 票）全数 Done + 裁定/微票 3 票（BIN-82/83/85=T-600/601/603 裁定报告；BIN-84=T-602 PRD 域注记；BIN-88=T-606 ADR Erratum 三）+ conductor 台账批两动（09c7e93f + 3bd35c62）+ 双审 B1 安全修复（d413ff10）。BIN-86/87（T-604/605 seam 收敛）结转 Backlog（R14 可捡）。

| 票 | T | 域 | 摘要 | 证据锚 |
|---|---|---|---|---|
| BIN-75 | T-593 | generic | checksum GET sha256 按需计算（computed 恒胜含 wrong 注册） | GATE 28/28 ×2 |
| BIN-76 | T-594 | maven | sidecar GET 按需矩阵（sha256-only 计算/md5·sha1 404 指源/miss 双文案） | GATE 26 腿 ×2 |
| BIN-77 | T-595 | maven | metadata 专用路由（PUT 200 no-op 家族 + GET 按需 miss 措辞 + sha256 主摘要恒胜收窄 walk） | GATE 84 腿 ×2 + mvn |
| BIN-78 | T-596 | httpapi | remote 写拒绝 a/c 臂（generic PUT 引擎 404 + DELETE remote-miss） | GATE 16 腿 ×2 |
| BIN-79 | T-597 | remote | sidecar GET 撤 a-priori 404 接回源链 + 上游故障外化两形 | GATE 18/18 + race |
| BIN-80 | T-598 | maven | sidecar 头面动词条件模型（GET 撤自加头 + HEAD 补 Etag/triple） | GATE 14 腿 ×2 + mvn |
| BIN-81 | T-599 | maven | srvgen 声明头无条件留档 + 非 GAV .sha512 撤 409 | GATE 23 腿 ×2 |
| BIN-82 | T-600 | conductor | REST 错误族联合裁定（3 条目 UNKNOWN→BUG，R14 票 WP1） | 4 轮补证 |
| BIN-83 | T-601 | conductor | 工件 GET 头集族裁定（一拆三：BUG 主面 + 2 INTENTIONAL） | 21 腿 ×2 |
| BIN-84 | T-602 | conductor | PRD RE-05 域注记 | 6-claim 锚表 |
| BIN-85 | T-603 | reverse | 双规格勘误（§1.5 回源改写 + §7.1/7.5 字段名勘误） | 8-锚表 |
| BIN-88 | T-606 | architect | ADR-0052 Erratum 三（决策 4 分域措辞矩阵） | — |

## 二、台账（Z 曲线）

R12 出口 Z=132=74+58 → **R13 出口 Z=143=80+63**（+11 条，+6 resolved）。
分桶：BUG 71r/9o · INTENTIONAL 1r/9o · UNKNOWN 6r/44o · UNSUPPORTED 2r/1o。
动作：六翻 resolved（①BIN-75 ②BIN-76+77 ⑦BIN-78+79 ⑧BIN-77 ⑨BIN-80 ⑩BIN-81）+ 三条 UNKNOWN→BUG（T-600 裁定）+ artifact-get 一拆三 + 九新 UNKNOWN 立案（批一 ×5 + wave-3 ×4）。

## 三、双审（0+0 达成，含一轮 blocking 往返）

**Reviewer A（correctness：并发/失败路径）**：首轮 REQUEST_CHANGES 1 blocking——B1：`retrievalFaultMessage` 把可含 userinfo 的上游 URL 逐字渲染进匿名可读 404 body（remote URL 内嵌凭据是合法配置，config 只校验 scheme/host）。NB×5（writePlaneSrvgen 探针竞态仅致可重试 409 / archiveSrvgenDeclared 500-over-landed 姿态一致 / metadata 路由 base 名大小写折叠超 live 证据 / external-hop 故障形未探 / deleteRemoteCache 跨面文案半径）。
**B1 修复 d413ff10**（修复中验证发现泄漏面比原发现更宽）：原始 transport error 实际喂**三个**匿名可读面——URL 插值、同 body 的 cause 文本（Go http 错误引完整 URL，密码 stdlib 掩 `***` 但**用户名明文**；首版 URL-only 修法被自测打回）、downgrade summary（stale `X-Binflow-Upstream-Error` 头 + hardFail 502 body）。修复=单一正则 redactor（`//[^/@?#\s]*@`→`//`，兼容掩码形）落两个渲染卡点；`TestRetrievalFaultRedactsUserinfo` 钉双面。复审翻案 **APPROVE 0 blocking**（A 独立复跑 build/vet/全包 30.9s/race 腿 5.3s 全绿）；范围外发现一条（cfg.URL userinfo 残留面）→ **BIN-99/T-617** 立票（conductor 复核：upstreamPropsURL 派生地址不拷 User、指认字面不成立，真实残留=fetcher.go:1487 WARN 日志站点；票内先复核再定论）。
**Reviewer B（architecture）**：**APPROVE 0 blocking / 6 NB**（首过）。台账计数链独立复算 132/74/58→139/78/61→143/80/63 全对；oc 单源 put.go:1029 + writeSidecarBody 唯一渲染出口 grep 复核；area 边界逐票正确；4 新 UNKNOWN 无重叠。NB-1 建议 merge 后补一轮差分确认（GATE 二进制当时含在途态）；NB-3 legacy 台账卫生超范围；NB-4 腿数措辞；NB-5 T-593/T-597 handler.go 包内重叠已披露；NB-6 writePlaneSrvgen best-effort 容忍。
报告入库 `reports/agents/R13-pr-review-{a,b}.md`（commit 0b5e2344）。

## 四、PR 与保鲜

- **PR #189**（载荷，base=develop）：16 提交两闸合并——闸一推送干净（远端 ref==本地 HEAD 0b5e2344）+ 闸二 headRefOid 核对后 merge（merge commit 3fc1309a）。
- **PR #190**（保鲜，develop→main）：R13 11 Done 票 ≥10 触发；main 独有 16 条均为历史合并提交无内容分叉，按房规 PR 收口（merge commit 1b029799）。main 五腿 CI post-#190 = R14 首查项。

## 五、Watch items（iteration-1553 用）

- **main 五腿 CI post-#190**（R14 首查；#187 后一腿曾需复跑）
- **B 审 NB-1**：merge 后差分确认轮（T-598/T-599 GATE 二进制当时含并行在途态，渲染出口独立但按 NB 补一轮最稳）
- **BIN-99/T-617**：remote URL userinfo 残留面（日志站点 1487 + parse 层拒绝/剥离选项；注意 Go transport 对 URL userinfo 自动 Basic auth，剥离可能改变上游认证行为）
- A 审 NB 结转：metadataChecksumRouteKey base 大小写折叠（补 live 腿或收紧 put.go:221）；external-hop 故障形 retrieval 措辞未探 fetcher.go:1100；deleteRemoteCache 跨面文案半径（conan/deb/nuget/helm/rpm）文档化或收窄
- B 审 NB-3：legacy 台账卫生（超范围，择轮）
- T-600 side-findings ×2：repo list 尾斜杠 400、remote url 尾斜杠回显归一
- TestChecksumPutRoutingBeforeLayout 负载 flake（2/5）；repo test flake（R10 结转）
- T-603 stale assertions ×4；errors-envelope 序列化风格（A Jackson pretty vs B compact，可并 BIN-97/T-615 探腿）
- T-596 ③ A POST generic remote 405 empty message（B 未探 NOT_RUN）
- T-595 pom-parse 侧发现（T-543 域）；T-594 Next② 探针候选
- T-597 NOT_RUN：可达上游 200 形 M1/M2、HEAD-200/.md5-.sha256-200 形、rpm 面 sidecar
- T-598 NOT_RUN：A 304/206 变体、GET INM 撤 Etag 后 A 行为、错值注册 HEAD triple A 面、派生 sidecar X-Checksum-* 面
- T-599 未探：routed virtual 键 ×srvgen 成员 .sha512 臂、metadata×声明组合（probe 候选）
- 金样采集候选：m1-{get,head}-sha1-set 头面全集（/tmp/t598/raw 留档）

## 六、R14 票池（已登记 Backlog）

BIN-89/T-607 REST 错误族三合一 · BIN-90/T-608 下载头集对齐（吸收 generic sidecar HEAD validator-set + Last-Modified 两 UNKNOWN） · BIN-91/T-609 reverse 勘误（+L152 阈值） · BIN-92/T-610 Erratum 四 · BIN-93/T-611 契约行三行 · BIN-94/T-612 local DELETE-miss 扩族 · BIN-95/T-613 generic metadata 路由族 · BIN-96/T-614 wrong-sha256 oc 探腿 · BIN-97/T-615 错误渲染面族探腿 · BIN-98/T-616 client-policy 409 文案族跨包 · BIN-99/T-617 userinfo 残留面。结转：BIN-86/87（seam 收敛）。
