# iteration-1546 · R7：plain-SNAPSHOT walk 全族 + module 级 auto-materialize + generic prefix 泛化 + DELETE 消费面清尾 + L035/L036 双探针 + 台账 Z=51（2026-09-29）

轮次目标：R6 候选池四票全清（T-562 walk 全族 / T-564 generic 前缀泛化 /
T-565 DELETE 消费面清尾 / T-566 auto-materialize module 级物化）、取证先行
两批（L035 walk forensics → 契约定稿 → 实现；L036 mimeType 归属 + 六族渲染
点审计 → 台账 intake）、台账收官批（+10 条、walk 红半边收口、四契约
VERIFIED、金样 B-leg 回填）。

## PR 与合并记录

| PR | 内容 | 状态 |
|---|---|---|
| [#177](https://github.com/0ldlight/binflow/pull/177) | R7 载荷（12 提交：T-564 / T-565 / L035 / L036 / T-562 两段 / T-566 / 台账收官 / sha512 blocking 修复 / 评审报告集）→ develop | MERGED 08:10:34Z（aa145598） |
| [#178](https://github.com/0ldlight/binflow/pull/178) | develop→main 保鲜（R6 残余 2 提交 + R7 载荷全量一并落 main，UAT 窗口与五 lane CI 覆盖 walk 面；硬触发未满但协议密度成立） | MERGED 08:11:48Z（6c1e4fb4，main push CI 已触发=R8 首查项） |
| [#179](https://github.com/0ldlight/binflow/pull/179) | R7 战报（claude/r7-payload 尾挂单提交 → develop，#176 同模式；随 R8 保鲜入 main） | MERGED（见 develop log） |

双审（protocol 域 → A/B 双强制；conductor 未自审）：Reviewer A（correctness）
首轮 **REQUEST_CHANGES**（1 blocking：sha512 旁车 walk 腿——serveSidecar/
writeSidecarDigest 拆分后闸滞留 serveSidecar，local+virtual 两腿可达 digest
lookup 的 500 ledger-gap 面，违 L032 derived-sidecar 契约 pinned 臂；3
non-blocking：① virtual walk 文件腿双重 applyReaderHints ② virtual walk
无 X-BinFlow-Resolved-From 提示头 ③ 500 消息失 repo/path 上下文）。修复
61e05e53：闸下移共享出口 writeSidecarDigest（签名加 node），双调用点归一；
随票带走 ①③；回归测试 TestPlainSnapshotWalkSha512SidecarGate（local+virtual
双面断言 404 非 500 + sha1 仍 200）。A 按其自处方**单点复核 APPROVE**（仅
sha512 腿 + 回归测试）：独立取证刻意异构于落库测试（timestamped 直种目录、
双成员 virtual、HEAD 腿、non-unique 对照、.sha256 对照）——walk 腿 sha512
GET/HEAD local+virtual 全 404（原 500 消失）、普通面三处 404 无回归、
.sha256 walk 仍 200 = 解析目标摘要（闸 sha512 限定未过捕）；闸门复跑全包
ok 25.458s、walk/sidecar 族 -race ok 9.186s；新增 1 条 cosmetic non-blocking
（virtual 旁车腿 404 消息用 best.path 而非请求拼写，一词之差，不动）。Reviewer B（architecture）首轮 **APPROVE**（0
blocking / 4 non-blocking：filter 循环收敛性候选、契约⑩ trigger ② 可显式化、
L036 五族 B 侧静态证据待 license 解锁活体腿、台账增量实为 11 非 10——采纳并
修正战报口径；范围外交办：rest/repo-delete-nonempty-cascade 计数臂复核微票
入 R8 池）。报告：reports/agents/R7-pr-review-a.md（含复核附录）/ -b.md（bcb97884）。
**连续第 4 轮双审抓到真实 bug**——双审制度持续自证。

## 完成票（Linear 状态已同步）

| 票 | Linear | 内容 | 提交 |
|---|---|---|---|
| T-562 | BIN-44 ✅Done | plain-SNAPSHOT walk 全族两段收口：stage 1 取证（L035：W1 触发面 / W2 成员内 max(filename ts, bn 数值) / W3 same-face / W4 跨成员存储 mtime / t8 非快照拼写门 / t9 无跨扩展名回退）+ 契约四份定稿（⑨ walk-resolve / ⑩ cross-member-selection / ⑪ sidecar-resolve / ⑫ non-snapshot-gate-404）+ A-leg 金样四组；stage 2 实现（servePlainWalk 拦截面 + serveVirtualWalk 跨成员面 + serveNode 单一下载出口——ETag/304/Range 针对解析后实体；t8 GET/HEAD 400→404 翻面，PUT 维持 400 未观测面不猜） | 0097fadf / 943ce899 / fd2d0606 / 61e05e53 |
| T-564 | BIN-46 ✅Done | generic deploy 201 Location/envelope 携产品前缀（handler.go productPrefix 同值常量三拷互注：maven put.go / generic handler.go / httpapi router.go）——A 面取证先行（L036 复核 7 站点恒绝对过 context root）后修，未盲改 | 5a1e4356 |
| T-565 | BIN-47 ✅Done | DELETE 200 级联消费面清尾三面：web 控制台删仓交互与文案（e6f186b0）、docs/design 三处 400 陈述翻新（cecd136e）、fern+OpenAPI 同步（fccd1165：repositories.mdx / build_spec.py DELETE 200 报告体 + deleteContent 冗余语义 + operations.mdx 措辞；spec-check 对齐）+ docs/user 镜像（9f06b56c） | fccd116e / e6f186b0 / cecd136e / 9f06b56c |
| T-566 | BIN-48 ✅Done | auto-materialize module 级物化（对齐 A）：calc.go afterArtifactDeploy pom 腿 module 触发 recalcAsync→recalcSync（writeCreated 前同步物化）；version 级/delete/metadata 触发面不变——L035 勘误 T-555 时期 version 级归因（deep-list 计数无法分层所致误判） | fd2d0606 |
| （台账批） | 随各票 | known-divergence +10 条（L036 intake：五族 Location BUG×5 / nuget v3 push Location BUG / pypi 上传响应头 BUG / mimeType+pypi-href+stdlib-host-drift UNKNOWN×3）+ t8 及 stage-1 批 2 条 resolved flip；walk 族红半边（L033 ctl 腿残差）收口；matrix D12-R18 一行（201 冻结行集零增删）；contracts ⑨⑩⑪⑫ VERIFIED；金样 walk 四组 B-leg 回填 | 4145b059 |

## L035 / L036 取证两批（probe-first 纪律）

- **L035（walk forensics，T-562 stage 1）**：W1-W4 + t8/t9/t10 触发矩阵与选
  择规则取证（A=7.161.26 活体）；四裁定落 BIN-44 实现票后才动工。副产品勘误
  L033「30s 不物化」记录（被 W 副探否证）。
- **L036（T-564 followups ①②，probe-only 不改产品码）**：① mimeType 归属
  18 腿矩阵（一致 7 / 差异 11）——**A=扩展名白名单表+octet-stream 兜底、
  声明 CT 全忽略、无嗅探；B=声明 verbatim 存储**；扩展名表 15 腿 12/15 取值
  分歧（含 B stdlib 回退宿主漂移风险）。② 六族自引用渲染点审计：A 存储 PUT
  201 Location **一律绝对过 context root**（7 站点实证）→ 五族 B 侧裸相对
  定性 BUG 同族批量；A 两类「无 Custom Base URL 降级怪」（pypi
  localhost:8081 / helm local:// 真客户端不可用）**不构成对齐目标**。体系级
  blocker：B community 档 license 门挡 cargo/deb/nuget/rpm/helm 建仓——五族
  活体差分需 pro scratch 或 UAT 路径（用户决策项）。

## Z 对账（raw 实枚举口径，R6 统一后第二轮）

R7 终态（agent + conductor + Reviewer B 三方独立枚举逐位吻合）：**total
98 = resolved 47 + open 51**（BUG 12 / UNKNOWN 33 / INTENTIONAL 5 /
UNSUPPORTED 1）。较 R6（87=45+42）：**+11 新开**（L036 十条 + t8 所在
stage-1 批一条——B 复核纠正初报「+10」为 11，采纳）、**2 flip resolved**
（t8 非快照拼写门 + walk 族红半边 L033 ctl 腿残差）。open 净 42→51（+9）：
本轮为取证大年——L036 把五族 Location 等系统性分歧从「未观察」变「已定性
BUG 入账」，是 Gap 显影而非回归；R8 五族批量修复票已备。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（冻结行集零增删；D12-R18 行态更新）
- **Y**= 109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12，不变）
- **coverage**= 60.50%（181 适用行口径，不变）
- **Z（open 已知偏离）**= **51**（raw 实枚举口径，见上节；open BUG 12：
  R6 存量 5 + L036 新 7——五族 Location（deb/rpm/helm/nuget-bare/cargo）+
  nuget v3 push Location + pypi 上传响应头）

## 诚实失败与教训

- **git fetch URL 形态静默不更新 origin/*（conductor 自身，本轮最大教训）**：
  `git fetch <url> develop main` 只写 FETCH_HEAD 不动 tracking refs，加
  `2>/dev/null` 后完全无声 → 用陈旧 origin/develop 误诊「develop 缺 R6 内容/
  squash 重复」→ 启动无谓 cherry-pick（3 文件冲突）→ abort + 重建分支
  claude/r7-payload。修正：显式 refspec
  `git fetch <url> develop:refs/remotes/origin/develop main:refs/remotes/origin/main`。
  教训：URL 形 fetch 的 ref 语义与 remote 形不同，跨机对账前必须显式 refspec
  并复核 merge-base。
- **push refspec 笔误当场纠正**：`git push <url> claude/r7-payload:claude/r7-payload`
  源写成本地不存在的远端名 → "Everything up-to-date" 假象；改 `HEAD:claude/r7-payload`
  后真推。教训：push 输出「up-to-date」≠ 送达，须对 SHA。
- **双审 blocking（第 4 轮连续）**：sha512 闸在函数拆分后滞留旧调用点——
  共享出口不变量（「闸必须在所有路径的汇合处」）在重构时最易丢；修复即把
  闸下移到汇合点并配双面回归。
- **agent 侧 repo-package 首跑 FAIL 237.9s（尾部截断丢测试名）**：t562-impl
  如实上报；两次隔离重跑绿 + conductor 全 39 包复跑 GO_TEST_EXIT=0 → 负载
  争用归因成立（并发 agent 下共用 build/test 缓存争用）。教训：并发波次下
  的单包 FAIL 先隔离复跑再归因，截断日志不作唯一证据。
- **台账增量初报 10 实为 11**：conductor 手工汇总漏 t8 所在批一条，Reviewer
  B 独立枚举抓出——三方枚举对账再次证明必要（R5 教训的执行面）。

## 哨兵证据（收编后全量，终态树）

- `go build ./...` exit=0；全仓 `go test ./... -count=1` **GO_TEST_EXIT=0，
  39 包全 ok**（storage 138.7s / webhook 74.1s 最长；显式 rc 捕获）——首轮
  internal/scheduler TestSchedulerFailureLandsAndRearms 单次 FAIL，第二轮
  全绿 + 隔离 `-count=3` 绿 → **定性全量并发负载时序 flaky**（载荷未触
  scheduler 包，与 R7 变更无关；若 main CI 复现则立票，列入 R8 观察池）
- golangci-lint ./internal/adapter/maven/... 0 issues（隔离缓存）；gofmt/vet
  clean；tsc --noEmit exit=0（T-565 web 面）；make spec-check PASS
- 双审凭据扫描：零字面量（评审报告与战报无凭据面）
- A 复核产物：独立取证异构于落库测试（timestamped 直种、双成员 virtual、
  HEAD 腿、.sha256 对照）walk 腿 sha512 全 404 / .sha256 仍 200 未过捕 /
  -race ok 9.186s；复核闸门全绿（报告复核附录）

## R8 候选池

- **五族 Location 批量修复**（BIN-49 候选）：deb/rpm/helm/nuget-bare/cargo
  `Location: <rel>` → requestBase+productPrefix 绝对形（T-561/T-563/T-564
  同配方；活体腿 BLOCKED license 门——A-only oracle + 单测先行）。
- **nuget v3 push Location 锚点错叠**（flat.go:355）+ **pypi 上传响应
  Location/X-Checksum-Sha256 补齐**（对齐 generic 形，不照抄 A 宿主怪值）。
- **mimeType 归属模型**（UNKNOWN 待裁）：对齐 A（扩展名白名单+忽略声明 CT，
  波及各 adapter）or 登记分歧——probe-first 立票。
- **license 门体系决策（用户项）**：五族活体差分需 pro scratch 或 UAT 路径。
- 微票池：rest/repo-delete-nonempty-cascade 计数臂复核（B 交办）、Reviewer
  A non-blocking ②（virtual walk Resolved-From 提示头）、B non-blocking
  （filter 收敛候选 / 契约⑩显式化）、B 扩展名表 stdlib 回退扩表提案
  （compatibility-engineer）、t104 dind CI 复跑、docs/user 里程碑标记清理、
  MySQL BIN-13、T-525/BIN-12、跨秒 LM 残差微票、R5 B 审两条、
  scheduler TestSchedulerFailureLandsAndRearms 负载 flaky 观察项（main CI
  复现则立票）。

## 用户待办（更新）

B3 CircleCI nightly trigger（03:17 UTC, main）；部署轮次时 `uat_approval`
点击；T-473；Jenkins 岛 VM .131 开机；**新增：五族 license 门解法决策（pro
scratch 实例 or UAT 换装路径）**；scratch 控制台（:8080，PID 59251）用毕
告知即可停。
