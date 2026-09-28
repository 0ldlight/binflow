# iteration-1543 · R4：L031 BUG 修复 + maven 契约形式化 + main CI 预算收口轮（2026-09-28）

轮次目标：收口 R3 遗留首查项（main CI 红灯）、修复 L031 判定的 npm `dist-tags.latest` BUG、
maven-virtual 契约形式化 + BIN-16/D-4 台账 resolved 回填、F8-widened 收口（T-541）、
L032 差分随访批次，并把 R5 票（validator 契约修订 / CI 分道）立进 Linear。

## PR 与合并记录

| PR | 内容 | 状态 |
|---|---|---|
| [#166](https://github.com/0ldlight/binflow/pull/166) | R4 载荷（7 提交：5 工作项 + agent-graph 补图 + 双审报告）→ develop | MERGED 13:43:30Z |
| [#167](https://github.com/0ldlight/binflow/pull/167) | develop→main 定期保鲜（即时正当：T-547 预算修复须进 main 才能收 CI 红灯） | MERGED 13:48:19Z |

双审（本轮**首轮即双 APPROVE、零 blocking 回合**，R3 以来第一次）：
Reviewer A（correctness）**APPROVE** 0 blocking / 5 non-blocking；
Reviewer B（architecture）**APPROVE** 0 blocking / 7 non-blocking
（reports/agents/R4-pr-review-a.md / -b.md）。合并门=双 APPROVE + 本地哨兵（PR 无 CI 策略不变）。

## 轮首：main CI 红灯诊断（T-547，conductor 直改）

R4 首查项兑现：R3 保鲜（#164）后的首个 main run **36411042188** failure。
跨 run 包级耗时对比定性（不烧 40min 本地 A/B）：R3 未触碰的 auth +26% / search +91%、
repo +75%，httpapi 1296.8s→FAIL 1800.080s——每包二进制 30min `-race` 预算被整体变慢的
runner 顶穿，无单个挂死测试，**容量问题非回归**。修复=TEST_TIMEOUT 30m→60m
（884b71c0，ci.yml 证据注释；先例=33881407081 的 10m→30m）。
Reviewer B 治理裁定：10→30→60 翻倍序列到此为止，第三次上调不可接受 →
结构性收口立票 **BIN-34/T-552**（重包 httpapi/repo/auth/search 各自独立 lane/runner 并行分道）。

## 完成票（Linear 状态已同步）

| 票 | Linear | 内容 | 提交 |
|---|---|---|---|
| T-547 | （无票，conductor 直改） | main CI 36411042188 诊断 + TEST_TIMEOUT 60m 预算修复 | 884b71c0 |
| T-541 | BIN-24 ✅Done | F8-widened：walk 层 virtualMember 携 handle 对 + getVirtual 三跳（§3.4/§3.5/§3.6）+ checksum sidecar 豁免；四象限矩阵测试 | 5a1f500c |
| T-548 | BIN-30 ✅Done | L031 BUG 修复：`repointFilteredLatest` 条件重算替换无条件 recompute；npm CLI 11.19.0 双端验收 | df59bce7 |
| T-549 | BIN-31 ✅Done | maven-virtual.yaml 契约 3 条（UA rider VERIFIED / version3 开关 SPECIFIED / GAV 门 VERIFIED）+ 台账回填 | da8a48de |
| T-550 | BIN-32 ✅Done | L032 差分随访 6 臂（T-536 五臂 + L031 member-sidecar 取证） | df43869e |
| （补图） | （conductor） | agent-graph.yaml dev-go-core owns/writes 补 internal/replication（T-546 遗留） | 60c20997 |

## L031 修复验收（T-548）

- `dist-tags.latest` 条件重算：base 成员 latest 目标版本在合并版次集内存活 → 原样透传；
  被过滤掉 → 重指向最大可见版本；base 无 latest → 合并面不预戴冠（crownLatest 归 read 时投影）。
- 双端三 case 连续两轮 **PASS**（npm CLI 11.19.0；latest 键 B 2.0.0→1.0.0 与 A 一致）。
- 单测：TestMergeLatestTagConditional（preserve×3 / recompute×2 / absent×1）+
  穿栈腿（base latest 1.0.0 < union max 3.0.0 时 packument 与 dist-tags 两面都答 1.0.0）。

## L032 差分结论（reports/compatibility/L032-t550-followup-arms.md）

6 臂，r1≡r2≡r3 稳定；一致 3 / 分歧 2 / 跳过 1。**两条新 BUG 候选（登记归 R5 compatibility 批）**：

1. **kcache deploy 措辞族**：`<K>-cache` 腿 B 404 措辞错 + 新发现 remote body 腿 A 404 §2.1
   逐字节 vs B 405 read-only-proxy——同一族两面双错。
2. **sidecar validator 族**：`.sha1` 面 A=Last-Modified+If-Modified-Snapshot 无 ETag vs
   B=ETag+If-None-Match 无 LM，三方分裂；digest 内容契约全绿 14/17——T-542 当年猜错了 validator
   形状（内容对、验证器错）。→ **BIN-33/T-551** 两段式：先契约修订（A 面 LM+IMS 实形）再 B 面修复。

另钉死：**UNKNOWN D-3**（非空仓级联删除 vs 确认门）证据已 ruling-ready，但 INTENTIONAL 判定
需权威（用户裁定/ADR/spec 票）；conan v1 = A 面 400 errors-envelope；conan 真腿 B 面库存许可证
不可达（诚实标注）；`<virt>-cache` wire 流从不物化 → WITH-ENTRY 臂 UNMEASURED。

## 诚实失败与教训

- **maven_module_handle_seat 臂 NOT_RUN**：T-550 隔离 worktree 自 origin/main 切出（并行派发时
  T-541 未在其树内）——前置条件缺失如实标记，R5 可重跑。
- **HTTPS HTTP2 间歇故障**：gh/git 走 HTTPS 报 "HTTP2 framing layer"，且一次 `git fetch` 静默失败
  导致同步分支自陈旧 develop 切出（PR 创建报 No commits）——改走 SSH remote
  （ssh://git@ssh.github.com:443）重做后 #167 正常。教训：网络抖动后必验分支基。
- **共享 lint 缓存假警报**：Reviewer A 首跑 golangci-lint 见 48 条——系其他 worktree 的陈旧缓存
  污染；隔离缓存复跑 0 issues。教训：lint 缓存按 worktree 隔离或复跑核实。
- 双端差分 BLOCKED→自愈：A_BASE 裸 8082 漏 `/artifactory` 前缀，agent 探活后仅修 env 不动 case。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（无新行；L032 属既有面证据强化）
- **Y**= 109.5（compatible + 0.5×partial + 0.5×superset，不变）
- **coverage**= 60.50%（109.5/181）
- **Z（open 已知偏离）**= **43**（-1：总 80 = 33 resolved + 4 gated + 43 open——BIN-16/D-4 修复
  双端复验后回填 resolved（+2），L031 BUG 新登记 open（+1）后本轮已修复、resolved 回填归
  BIN-33/T-551 随票；python yaml 复算核对）
- 口径注：L032 两条新 BUG 候选**未入账**（Z 不含）——登记是 R5 compatibility-engineer 批的票面动作。

## 哨兵证据（收编后全量）

- `go build ./...` exit=0；`go test ./...` 16 包全 ok（httpapi 324s / repo 224s，-count=1）
- web/ `npx tsc --noEmit` exit=0；golangci-lint 0 issues（隔离缓存复跑核实）
- 凭据扫描：全部提交物零字面量（admin/password 只在会话 env / /tmp/r3-difftest.env chmod 600；
  双审 A/B 独立凭据硬检查通过）

## R5 候选池（Linear 已立 2 票）

- **BIN-33/T-551** [P2]：sidecar validator 族契约修订（A 面 LM+IMS 实形）→ maven B 面修复
  （writeDerivedSidecar 去 ETag/INM、加 LM/IMS 304 面）；同票携带 L031 resolved 回填
  （锚 T-548.md Outputs）。
- **BIN-34/T-552** [P2]：CI Test 腿并行分道（结构性收口；预算翻倍序列止于 60m）。
- **差分**：kcache deploy 措辞族修复（含 remote 405 腿）；maven_module_handle_seat 臂重跑
  （T-541 已在树）；L032 两 BUG 候选台账登记批。
- **裁定**：D-3 ruling-ready，需权威拍板（用户/ADR/spec 票）。
- **携带**：201 envelope `/binflow` 前缀 architect 票；T-536 剩余随访；MySQL BIN-13；
  T-525/BIN-12；B3 nightly + uat_approval（待用户）；Jenkins VM .131 待用户开机。
- **R5 首查项**：#167 合并触发的首个 main run 转绿核验（60m 预算生效后首跑）。

## 用户待办（无变化）

B3 CircleCI nightly trigger（03:17 UTC, main）；部署轮次时 `uat_approval` 点击；T-473；
Jenkins 岛 VM .131 开机（当前不可达）。
