# iteration-1551 · R12：六票收口（maven 路由门/1024B 守卫/键集单源/vdT 收敛/L041/联合裁定）+ 台账批 3 翻 5 立 2 修订；Z=58（BUG 9→12）（2026-09-30）

一轮六票（BIN-69..74）全 Done。phase-0 保鲜：**PR #187 develop→main**（e64ac966，R10+R11
载荷首次入 main，UAT 源保鲜；保鲜计数器复位）。本轮主轴=R11 台账批修复面兑现 + 两台单源收敛
手术 + R12 台账批落账：**三 flip resolved**（N1 maven 路由门 / N5 generic 1024B 守卫 / oc
键集模型）；**C2/C5/C7 三条 UNKNOWN 联合裁定转 BUG**（T-592 建议稿 + L041 一处关键修正）；
xsha-echo 改判 BUG（动词条件模型）；五新立 + 两修订折叠 R13。台账 Z：**127→132 = 74+58**
（resolved 71→74；open BUG 9→12、UNKNOWN 39→38——BUG 净增=裁定轨道扩容，非质量回退：
六条新 BUG 全部带 R13 工程票）。

## PR / 合并

| PR | 内容 | 状态 |
|---|---|---|
| [#187](https://github.com/0ldlight/binflow/pull/187) | develop→main 周期保鲜（R10+R11 载荷 #186 + R9 报告；15 done ≥10 闸触发） | MERGED（e64ac966；main=1b1e3bfb 内容） |
| [#188](https://github.com/0ldlight/binflow/pull/188) | R12 载荷（11 提交：T-588 5729c092 / T-592 92c588b9 / L041 2a75624b / T-587 abc922fc / T-590 2a4c28c7+3ea65e1e / T-589 d14f34aa+ea552b76 / R12 台账批 262d3a12 / 双审报告 + 战报） | MERGED（双闸：推净 11 提交 + headRefOid 核对；develop 树复验 VirtualDeploymentTarget 与 (node, serverSha256) 签名——#185 事故防线） |

## 轮内裁定（conductor，台账四分类口径）

R12 台账批（262d3a12）：3 flip + 4 reclass + 5 新立 + 2 修订，python 实枚举复核 Z 数学全对。

1. **三 flip resolved**（修复票兑现，双轮活体证据入台账）：
   - **N1 `maven/checksum-put-unrouted-virtual-plane`**（T-587）：拦截臂过部署路由门，未路由
     virtual sidecar PUT 全 405 C5 逐字 + 已路由穿透（miss 404 成员 key / 201 Location 成员源 /
     409 写穿）；GATE 32/32 ×2。**surface 收窄**：读面子句剥离——sha256 unset 双端均按需计算
     200（三腿实测修正原登记），仅 sha1/md5 unset 分歧，划入 C2 族。
   - **N5 `generic/checksum-put-body-size-guard`**（T-588）：1024B 含端点守卫 + 409 逐字
     （ContentLength 臂 + io.LimitReader(1025) 臂）；**A 权威新钉 miss-guard-1025=守卫先于
     存在性探测**；1MB Broken-pipe 同形。
   - **oc 键集模型 `httpapi/original-checksums-key-model`**（T-589）：repo.OriginalChecksums
     签名收窄 (node, serverSha256)，键集=client∪{sha256} 三面坍缩单源（FileInfo / maven+generic
     envelope / nuget）；GATE 12/12 ×2；L041 交叉证（m1-fileinfo-jar/wsrv/#8a）无冲突。
2. **C2/C5/C7 联合裁定转 BUG**（T-592 建议稿 + conductor 复核，一条关键修正）：
   - **C2 `generic/checksum-get-virtual-ondemand`**：T-592 草稿「本地面零触碰」基于 L037 Arm 5
     旧证——**L041 g1-get-sha256-unset 实测 A=200 计算 sha256（generic local 面）推翻该句**；
     终裁=sha256 主摘要按需回显 **virtual+local 两面同缝**，sha1/md5 双面 404 维持（9a/9b 回归腿）。
   - **C5 `maven/metadata-checksum-dedicated-route`**：三分面——PUT 文件名键控专用路由（否决
     拦截豁免，纳入 PUT 面次序模型：remote 拒绝→部署路由门→metadata 路由→sidecar 拦截→普通
     部署）；GET 按需计算（复用 strippedSnapshotMetadata+writeDerivedSidecar）；visibility facet
     拆立独立条目。
   - **C7 `generic/remote-deploy-refusal-form`**：四分面——a generic PUT 404 引擎复用
     （T-553 router.go:2422，docker/cargo 禁翻）；b maven 已一致；c DELETE remote-miss 文案+
     local 探腿门控；d sidecar-GET 撤 a-priori 拒绝接回源代理（NFR-S13 守卫负测）。
3. **xsha-echo 改判 BUG**（`maven/sidecar-get-x-checksum-sha256-echo`）：L041 Arm 1 头面全收
   钉死**动词条件模型**——A GET=零校验头零 Etag / HEAD=Etag+源校验 triple 全键；B GET+HEAD
   自加 Etag+X-Checksum-Sha256（maven 面独有）+ HEAD 缺 Md5/Sha1。
4. **五新立**：N1rest `rest/remote-domain-policy-enum-gate`（UNKNOWN；remoteRepoChecksumPolicyType
   域字段枚举门 400 无 "type" 变体；NB-⑥ 否定副产品——checksumPolicyType 在 remote/virtual 面
   双端同静默丢弃，无需对齐）；N2 `maven/sidecar-get-ondemand-matrix`（BUG；sha256-only vs B
   maven 过算/generic 零算 + miss 指终缀）；N3 `storage/artifact-get-response-headers`（UNKNOWN；
   Content-Disposition/X-Artifactory-* 族 vs B 自加头，与 xsha-echo 缺头/多头防混裁）；
   N4 `maven/srvgen-declared-header-drop`（BUG；A 无条件留档 vs B GAV 丢+非GAV .sha512 409
   三臂两语义；与已 resolved 键集条目防双计分界=值注册处置+409 策略域归本条）；
   `maven/metadata-fileinfo-visibility`（UNKNOWN；A FileInfo 200 vs B 404 node not found，
   T-592 面）。
5. **两修订折叠 R13 联合票**：CT 严格度（精确 application/json 白名单 415 族 + charset→404
   无引号变体）；update-404 文案族（方法族三形态：POST 带引号 / DELETE statusMsg 包 / GET
   400 双端一致）。
6. **白名单九面 collapse 转正**（L041 收敛树首跑，L040 B2 条件性结论关闭）；**N6 关账**
   （.sha1 经 virtual 实体落地双端不可达，触发面消失）；**NB-⑥ 否定**（A 对 remote/virtual 的
   checksumPolicyType 同无门）。

## 完成票（六票全 Done，Linear BIN-69..74）

| 票 | 提交 | 一句话 |
|---|---|---|
| BIN-69/T-587 maven 拦截臂路由门 | abc922fc | 部署路由门前置于读探针（405 C5 逐字+Allow:GET+零副作用；穿透面 factsKey 化 miss/201/409 全成员指）；writeSidecarDigest 不可达 sha512 分支删除；GATE 32/32 ×2 |
| BIN-70/T-588 generic 1024B 守卫 | 5729c092 | ContentLength 臂 + io.LimitReader(1025) 臂双守卫（>1024→409 逐字）；miss-guard-1025 A 新钉=守卫先于存在性探测；maven put.go:383 同守卫先在 |
| BIN-71/T-589 oc 键集单源化 | d14f34aa | repo.OriginalChecksums 签名收窄 (node, serverSha256)，per-algo server 兜底参数（两面漂移使能器）消灭；三 envelope 面坍缩同源；死 helper ×4 删除；GATE 12/12 ×2 |
| BIN-72/T-590 vdT 单源收敛 | 2a4c28c7 | virtualDeploymentTarget 第 8 份锁步拷贝提升 internal/adapter/deploytarget.go 单源（R11 审 B NB-①）；七包消费方薄化+golden 21 例快照守卫；纯 refactor 零行为变化 |
| BIN-73/T-591 探针批 L041 | 2a75624b | 108 腿/轮 ×A/B×双轮六臂（xsha 头面/枚举门/404 文案族/CT 严格度/N6/srvgen 角落）+白名单九面收敛树首跑；pristine detached worktree 溯源门（共享树在途修改防污染） |
| BIN-74/T-592 C2/C5/C7 联合裁定 | 92c588b9 | 三条 UNKNOWN 裁定建议稿（成本口径/路由模型/分面归属）；B1/B2 对账 moot（develop 收敛）；PRD RE-05 与 §1.5 Erratum escalation |

## 双审（载荷 5729c092^..262d3a12，**0+0**——连续第三轮零 blocking）

| Reviewer | 形态 | 结论 | blocking | NB | 范围外 | 报告 |
|---|---|---|---|---|---|---|
| A | correctness | APPROVE | 0 | 6 | 3 | reports/agents/R12-pr-review-a.md |
| B | architecture | APPROVE | 0 | 6 | 5 | reports/agents/R12-pr-review-b.md |

- **并行独立出具（未互看）**。A 复核四代码提交逐项通过：1024B 守卫边界（1024 含端点放行/1025 双臂拒/chunked 有界读/零副作用断言）、路由门 factsKey 四处寻址一致 + Allow:GET 与服务侧逐字节同值、sha512 不可达论证独立复核成立（layout.go 唯一 Algo 生产者链）、T-589「envelope 读 landed node」不变式在 repo/service.go:1186-1221 直接核实 + nuget 旧测试零改动通过=坍缩恒等证明；本地全量（四核心包 + 八消费包 + 新测试族 -race）全绿，台账算术与 L041 树恒等声明脚本复算吻合；活体探针按派发令未复跑（A 凭据 env-only），raw 自洽采信（R10/R11 同款姿势）。
- B 独立复验全链吻合：Z 对账 python 复算 127→132=74+58、open BUG 9→12 全中；八份探针体从 pre-hoist 树机械提取比对（7 份逐字节一致 + helm 缩进/变量名差与自述一致）；`OriginalChecksums(` 全量 grep 使能器零残留；adapter 基座闭包仅 metadata+auth、协议包零互依；T-592 草稿→台账忠实度机械比对（C5/C7 逐字、C2 修正有据非静默）；L041 pristine 溯源门方法学闭环；本地回归复跑全绿。
- **NB 交汇（A-2/B-3 独立同判）**：C5 逐字文案+1024B 常量跨包三副本 → 收敛池候选票（deploytarget.go 先例）。A-6/B-5 同指秒传 md5 回显腿（Watch items 已列）。其余：B-1 ADR-0052 决策 4 待 Errata 三（下轮台账批顺手）；A-3 sha512 死码清理剩三处+过期注释；A-4 policy 门先于路由门的未钉角 + A-5 checksum-deploy 臂次序入模型（并入 R13 探针/T-595 前置探腿）；B-2 C2 gate 行 maven 简写措辞；B-4 N4 surface 陈旧 B 形（下轮台账批刷新）；B-6 virtualRouteTarget 双轨移交 architect。
- **范围外**（A 3 / B 5，交 conductor）：A-1 `internal/build/promote.go:402-416` **第 9 份 vdT 锁步拷贝**（并入 T-590 Next① seam 统一票）；A-2 `internal/httpapi/uploads.go:708-725` **mpu 二别名变体**（缺 deploymentRepository 别名，raw-seeded virtual 在 mpu 面不路由——存量语义分叉，台账查重或立小票）；A-3 `.playwright-mcp/` 卫生（本轮维持未入库）；B：PR #188 推净+headRefOid、T-592 上报三项未执行（BIN-84/85 已立票）、virtual×srvgen overlay 探腿（并入 BIN-76）、L041 两 UNKNOWN 分诊（BIN-82/83）、次序模型 ADR 后议。

## Z 对账（raw 实枚举口径）

R12 终态（conductor 枚举，python yaml 解析复核）：**total 132 = resolved 74 + open 58**
（BUG 12 / UNKNOWN 38 / INTENTIONAL 7 / UNSUPPORTED 1）。较 R11（127=71+56）：**+3 flip
resolved**（N1/N5/键集）；open 56→58——**BUG 9→12**（−3 flip +C2/C5/C7 三 reclass +xsha
改判 +N2/N4 两新立）、**UNKNOWN 39→38**（−4 reclass +N1rest/N3/visibility 三新立净 −1）。
BUG 净增属裁定轨道扩容：六条 R12 新 BUG 全部注册 R13 工程票（BIN-75/76/77/78/79/80/81），
零悬空。

open BUG 12 枚举——存量 6（docker/remote-cache-layout、rest/properties-root-posture、
search/snapshot-row-wildcard-match、search/badchecksum-maven-metadata-exclusion、
maven/bare-directory-get-400-vs-404、storage/deploy-201-itemcreated-envelope-family）+
R12 6（generic/checksum-get-virtual-ondemand、maven/metadata-checksum-dedicated-route、
generic/remote-deploy-refusal-form、maven/sidecar-get-x-checksum-sha256-echo、
maven/sidecar-get-ondemand-matrix、maven/srvgen-declared-header-drop）。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（冻结行集零增删；本轮无新行）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **58**（raw 实枚举口径，open BUG 12 构成见上节）

## R13 候选池（已注册 Linear Todo 十一票 BIN-75..85）

- **BIN-75/T-593**（High）：C2 generic GET sha256 按需回显 virtual+local 两面 + sha1/md5 404 维持。
- **BIN-76/T-594**（High）：N2 maven sidecar GET 按需矩阵（sha256-only/md5+sha1 404/miss 指源；
  TestChecksumPutClientOverlayFallback 测试翻新）。
- **BIN-77/T-595**（High）：C5 metadata 文件名键控路由 + GET 按需计算（一票两 WP + 四角落
  前置探腿）。
- **BIN-78/T-596**（High）：C7 a/c 臂 generic PUT 404 引擎复用 + DELETE remote-miss 文案
  （探腿门控；docker/cargo 禁翻）。
- **BIN-79/T-597**（High）：C7 d 臂 remote sidecar GET 撤 a-priori 拒绝接回源代理（守卫负测）。
- **BIN-80/T-598**（High）：xsha-echo GET 撤自加头 + HEAD 补 Etag/triple（两半面一次收口）。
- **BIN-81/T-599**（High）：N4 srvgen declared 无条件留档 + .sha512 臂撤 409（与 T-589 单源衔接）。
- **BIN-82/T-600**（Medium）：REST 错误形族联合裁定（CT 白名单+404 三形态+N1rest 域字段并票）。
- **BIN-83/T-601**（Medium）：N3 工件 GET 头集族裁定（Content-Disposition 优先）。
- **BIN-84/T-602**（Low）：PRD RE-05 域注记（remote 写拒绝文案域统一）。
- **BIN-85/T-603**（Low）：maven-npm-pypi.md §1.5 Erratum（remote sidecar GET 回源代理）。
- 派发约束：T-594 与 T-598 同 maven handler GET 面**勿同波并行**；T-596/T-597 同台账条目
  分票但 area 不重叠（httpapi+generic vs remote+adapter 读面）。
- 候选池（未立票）：metadata-fileinfo-visibility 补证腿（L042）；repo virtualRouteTarget seam
  双轨裁定（architect SPI 跟进，T-590 Next）；审 A NB 残项（R10 起结转）。

## 环境与安全复核

- A 实例（7.161.26 @ 192.168.120.38:8082）配置零触碰；difftest-* 前缀仓用后 DELETE 复验 0
  （T-587/588/589 三票活体均附 teardown 证据；T-591/L041 四证据轮 residual 全 []+独立复验
  计数 0）；凭据经 /tmp env 注入零落盘零打印。
- B 面 scratch 实例（18156/18157/18170 等）SIGTERM graceful + 端口释放复验 + pgrep 空；
  L041 pristine detached worktree 用后 remove（worktree list 零残留）。
- `.playwright-mcp/` 未入库；reverse-src/ 只读未触碰；BOARD.md 冻结只读维持；本地 Docker 未
  启用（B 面构建=本地 go build）。

## Watch items（转 R13）

- **main 五腿 CI post-#187**：R10+R11 载荷首次入 main——**8/9 绿**（ci + auth/repo/search/
  light/httpapi 五测试腿 + release-dryrun；e2e 常规 skipped），仅 uat 腿在途（deploy UAT 链）；
  R13 首查收尾确认。
- **repo test flake**（R10 起结转；R11/R12 未复现，T-589 五包全量绿）——再显影立 flake 票。
- develop→main 保鲜计数器已复位（#187 后从 0 起算；下次 ≥10 done 或 ≥1 天触发）。
- 非 GAV .sha512 DELETE 空 GAV 触发器全仓 no-op（R11 审 A NB，无数据风险，结转）。
- **双审范围外转入**：`internal/build/promote.go:402-416` 第 9 份 vdT 锁步拷贝（与 repo
  virtualRouteTarget seam 双轨一并交 architect 裁定，T-590 Next①）；`internal/httpapi/
  uploads.go:708-725` mpu 二别名变体（缺 deploymentRepository 别名——存量语义分叉，台账
  查重或立小票）；C5 文案+1024B 常量收敛池票（双审独立同判）；sha512 死码剩三处+过期注释
  （下次触达一并清）。
- T-589 Risks②：checksum-deploy（秒传）envelope md5 回显改注册值逐字——罕见组合 A 形未单独
  活体探（影响面：声明错误值+秒传同现），R13 探针批可捎带（双审 A-6/B-5 同指）。
