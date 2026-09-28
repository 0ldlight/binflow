# R4-pr-review-b — R4 载荷分支 claude/r4-payload 评审（形态: reviewer-b / architecture）

```
Ticket:       R4 payload PR 评审（1517949b..df43869e，6 提交：884b71c0 / 60c20997 / da8a48de / 5a1f500c / df59bce7 / df43869e）——T-547/T-546后续/T-549/BIN-31、T-541/BIN-24、T-548/BIN-30、T-550/BIN-32
Role:         code-reviewer (reviewer-b，architecture 视角：架构/兼容语义/测试覆盖)
Area:         跨域载荷：docs/compatibility（契约/台账/矩阵）+ docs/reverse 注记 + internal/repo walk 层 + internal/adapter/{maven,npm} + tools/difftest/v2 + .github/workflows/ci.yml
Input:        conductor 派发（六提交清单 + 六项 architecture 重点问）；通读 docs/reverse/virtual-resolution.md §1-§5.1 / maven-npm-pypi.md §2.6、docs/design/virtual-four-bucket.md §3/§8/§9、ADR-0051 Errata 一-①/一-⑤ 相关行、contracts/maven-virtual.yaml 全文、known-divergence/matrix 前后快照、L030/L031/L032 报告、T-541/T-548/T-549/T-550 四份 agent 日志、internal/adapter/maven/virtual_metadata.go 实现面
Changes:      全量 diff 逐提交审读（27 文件 +2142/-34）；上下游追读：walk 层（virtual.go getVirtual/isSnapshotResolutionPath/splitMemberChecksumSuffix）、npm 合并面（mergeVirtualPackuments/repointFilteredLatest/reindex recomputeLatestTag）、maven rider 面（clientSupportsM3SnapshotVersions/writeDerivedMetadata/writeDerivedSidecar）、matrix/台账计数算法复算（PR-158 Review A §4 口径）
Files:        docs/compatibility/contracts/maven-virtual.yaml（新建 3 条目——clean-room 句式合格，判定族与实现 virtual_metadata.go:436-456 逐句一致；见评审报告）｜docs/compatibility/known-divergence.yaml（resolved 回填 2 + L031 新账 1——算术与指针均闭环）｜docs/compatibility/matrix.yaml（2 行 + 计数器 37→38——实算吻合）｜docs/reverse/两文件（仅交叉引用注记，未改断言——铁律合规）｜internal/repo/virtual.go（handle* 半边——规格读法见 NB-3）｜internal/adapter/npm/virtual_packument.go（条件性 latest——语义与测试合格）｜tools/difftest/v2 六 case + _mavenlib.py（py_compile 过、runner 发现门 16 case、seat 探针门设计佳）｜.github/workflows/ci.yml（预算提升——见 NB-7）｜docs/ai-engineering/agent-graph.yaml（replication 归属——与 CLAUDE.md 文件地图一致）｜reports/agents/T-54{1,8,9}.md、T-550.md、L032 报告（证据模板完整）
Tests:        只读取证全绿：go vet 三包无输出；gofmt -l 三目录空；go test ./internal/repo/ -count=1 ok 69.3s（整包）+ T-541 四测 -count=1 ok；go test ./internal/adapter/npm/ -run 'TestMergeLatestTagConditional|TestVirtualLatestTagPassthroughThroughStack|TestVirtualPackumentMergeMatrix' ok；go test ./internal/adapter/maven/ -run 'TestClientSupportsM3SnapshotVersions|TestSnapshotVersionsUAStripping' ok；python3 py_compile 六新 case + _mavenlib OK；runner.py --list 发现含 6 新 case；台账双快照 python 复算（YAML parse + PR-158 §4 算法）：前 79=31+4+44（open BUG 9）、后 80=33+4+43（open BUG 8）——与 T-549 声称逐数吻合；matrix 实算 contract_ref 38 / last_difftest 51 / golden 1 与 summary 全等，rows 201 前后零增删；known-divergence 80 id 唯一
Commands:     git log/show 1517949b..df43869e（六提交全文+diff）；git merge-base --is-ancestor f21f1c09/3b58b082 HEAD（均 YES）；python3 yaml 复算台账/矩阵（两快照）；grep 凭据硬检查（JFrog@/password/api_key/token/AKIA/ghp_/BEGIN KEY 全 diff 零命中；六新 case 源码零字面量）；grep version3 产品面（零命中——契约 binflow_state「BinFlow 无开关面」属实）；go vet/gofmt/go test 上述命令
Outputs:      本文件（reports/agents/R4-pr-review-b.md）
Compatibility: 契约审计合格：① rider/GAV 两 VERIFIED 定级依据成立（L030 case1/case2 修复后双端复跑两轮 PASS，锚 reports/iteration-1542.md:35「L030 修复验收」+ f21f1c09/3b58b082 均为本分支祖先——指针闭环）；② version3 开关 SPECIFIED/medium 定级纪律正确（off 臂双端零取证不立断言、点分拼写/同一性联结两推定显式降置信、grep 证实 BinFlow 确无开关面=恒「开」臂——无猜测补齐）；③ 与 known-divergence 双向指针（resolved.evidence ↔ 契约 detail ↔ matrix D12-R18 note）三向一致；④ validator 族分歧不构成新契约失实——maven-virtual.yaml 对 sidecar 的 HTTP 验证器面保持沉默（只断言内容摘要），T-542 的错误类比只活在代码注释与旧报告（见裁定 2）
Security:     凭据硬检查过：全 diff 与六新 case 源码零凭据字面量；forward-proxy 凭据内存态（token 不入 argv/文件/evidence，Basic 注入前剥离客户端 Authorization）；192.168.120.38 引用=已登记基准实例；T-541 policy 跳过发生在 virtual 读门之后（无新增权限面），config 探针只读不触 password 键
Performance:  T-541 每成员行增一次小 JSON 探针（与 priorityResolution 同量级，拒绝族反而省上游接触）；T-548 repointFilteredLatest O(1) 替代全序扫描（略省）；ci.yml 预算翻倍为容量应对非结构修复——见 NB-7
Risks:        见下方 non-blocking 清单与两条裁定；最大残余=handle* 排除臂与 sidecar validator 族的差分翻案均押在后续票（归属已明）
Blockers:     无环境阻塞。注意：L032/L031 复跑 wire 证据（run/l032-arms-r{1,2,3}/、run/l031-npm-r{4,4b}/）gitignored 本地物，本评审树不可复跑——按仓内既定惯例（结论以 committed 报告+断言键为锚）采信，非本轮新增风险
Next:         交 conductor：① 立 compatibility-engineer 契约修订票（合并处理：rider 条目 unobserved 臂③更新——L032 Arm 6 已证 A 面取齐 .sha1/.md5/.sha256 且 match；sidecar validator 族新账立案 A=LM+IMS 无 ETag；§2.6 措辞细化提案一并收）；② sidecar validator 族 B 修复票（writeDerivedSidecar：补自身 LM、去 ETag、IMS 生效）待契约修订后派 dev；③ kcache BUG 扩展（remote 本体 405 新腿并入 rest/kcache-deploy-404-wording 或立新账）+ conan v1 对齐目标重定 + 非空仓 D-3 终局——T-550 Next 已列，需派票；④ O7（timestamped snapshot 判定缺口）落 design doc §8 O-list（architect/conductor）；⑤ ci 结构性分道票确认在册（Linear 侧本评审不可见）；⑥ R5 台账票：L031 npm/virtual-packument-merge-latest-tag 回填 resolved（锚 T-548.md Outputs + L031 r4/r4b）
```

## 评审报告 R4-payload（形态: reviewer-b）
结论: **APPROVE**

六项重点问逐项裁定：

**裁定 1（clean-room 铁律）——合规**。contracts/maven-virtual.yaml 全文行为句式（「服务端对请求 User-Agent 头的求值…判定支持/不支持」四族、arms when/status/body）；docs/reverse 两处改动均为「不改上文断言」的交叉引用注记（virtual-resolution.md §5.1 表后 blockquote、maven-npm-pypi.md §2.6 加注），未触碰存量断言与证据锚；契约引用反编译常量名 mvnMetadataVersion3Enabled 系规格既有锚的转引且把点分拼写降置信 medium——version3 开关条目四处缺口（点分拼写/同一性联结/关臂 local 面/关臂生成面）全部如实标注、off 臂「不立断言，禁猜」，无猜测补齐。

**裁定 2（L031 fix-in-flight 回填时机）——留 R5 回填，本分支维持 open 正确**。先例（BIN-16/D-4：R3 修复 → R4 T-549 凭 iteration-1542 已合并锚回填）口径 = 回填证据须锚在已合并的验收报告；T-549（20:40）落账时 T-548（20:43）未在树，「不抢跑」纪律正确。T-548 复跑证据（L031 r4/r4b 两轮 PASS，latest 键 a=b=1.0.0）已随分支落盘于 T-548.md Outputs，R5 台账票可直接闭账——但勿在本分支内追加翻绿（会破坏「滞后一轮」既定节奏且无增量信息）。附带核对：台账算术独立复算全等（前 79=31+4+44 / 后 80=33+4+43，open BUG 9→8，gated 四条 id 未动）；resolved 指针三向闭环。

**裁定 3（T-541 规格读法）——可接受的结构近似，已登记，唯差分臂指向需补准**。§3.4 旁车豁免（.sha1/.md5/.sha256 单源 splitMemberChecksumSuffix）与 §3.6（快照族跳 handleSnapshots=false + 全 cache 步）实现忠实；「release 可解析=二元家族划分」替代 GAVC 解析器是对 §3.4「路径可解析出模块信息」的**放宽**（一切非快照非旁车路径皆视为 release 族），T-541 Risks② 已登记为差分关注。但 Risk② 的措辞重心（「非 maven 包型成员也会被门控」）指错了方向——A 面可表达 handleReleases=false 的恰是 **maven** 仓，更锐利的未测臂是「maven 域非 GAVC 路径（如根级 archetype-catalog.xml / 非 GAV 布局文件）：A 不跳（不可解析出模块信息）vs B 跳」——该臂 A/B 双面今日皆可构造，应作为差分腿候选补入（见 NB-3）。O7 候选仅存在于 T-541.md，未落 design doc §8 O-list（O1-O6 所在），有丢失风险（NB-4）。

**裁定 4（T-542 派生契约 vs L032）——确系猜错，修订归 compatibility-engineer，流程已正确挂起**。T-542 的 writeDerivedSidecar 注释自认「与 local sidecar 面同一服务端计算契约」——该类比无 A 面证据，L032 Arm 6 以 17 维矩阵证伪 3 维（A=sidecar 自身 LM+IMS、无 ETag vs B=ETag+INM、无 LM），内容契约（digest-of-stripped-body）14/17 双面全对。关键正面事实：**新契约 maven-virtual.yaml 未重复该错误猜测**（对验证器面沉默，只断言内容）——无正式契约需要撤回；错误类比仅存于代码注释与 T-542 旧报告。修订路径正确：契约修订票（compatibility-engineer，以 L032 矩阵为 A 面证据）先行，B 修复（writeDerivedSidecar）后派——T-550 Next① 已挂，需 conductor 派票落账（NB-1/NB-2）。

**裁定 5（测试覆盖登记）——足够**。T-548 排除模式生效臂：Risks① + Compatibility 可差分 surface + Next④ 三处登记（现树无读时版本滤器、双面不可构造——「未验证角落」定性诚实，单元表以滤后 doc 形状钉住）；reindex 无条件重指面不动（reindex.go:203 核实，L012/L013-r15 两 resolved local 面账背书）。T-541 四象限矩阵 + cache-facet drop + 旁车豁免 + local passthrough 通路（真实写面）覆盖充分，-race 过。seat 探针门（NB-5 的措辞问题不掩设计优点：臂体不实现，探针不过永不假裁）。

**裁定 6（ci.yml 60m）——治标但透明，附条件放行**。容量论证具足（具名 run、httpapi ~1300s 名义值=72% 预算、R3-未触碰包 +26%/+91% 的全域慢化、无单点挂起、10m→30m 同型先例），结构性分道（并行 lane）显式让位 devops 后续票。10→30→60 的翻倍序列本身是症状累积信号：若出现第三次提升应视为治理红线。结构性票在 Linear 侧，本树不可验证在册（NB-7）——conductor 需确认。

### 必须修改（blocking）
- 无。

### 建议改进（non-blocking）
1. **[契约时效] contracts/maven-virtual.yaml:64** unobserved 臂③（local .sha256 旁车 java-agent 腿「A 面未取证」）已被同载荷 L032 Arm 6 证据收回（A 面取齐三摘要 match×3，java-agent，成员面）；且括注「BinFlow 三摘要模型外恒 404」与主语 .sha256 自相矛盾（.sha256 在三摘要模型内，恒 404 的是 .sha512+）。→ 修订票内更新该臂并澄清括注主语。
2. **[契约精度] contracts/maven-virtual.yaml:102-107** evidence_gaps 实为 3 bullets（关臂 local 面与生成面合于一条），T-549.md 报告口径为「四处」——数目口径不一致，纯计数修辞，修订票顺笔对齐。
3. **[差分臂指向] reports/agents/T-541.md Risks② / internal/repo/virtual.go:355-360** 二元家族划分的差分关注臂应补「maven 域非 GAVC 路径」腿（handleReleases=false maven 成员 + 非 GAV 布局文件：A 服务 vs B 跳过——双面今日可构造，比「非 maven 包型」臂更可能翻案）。
4. **[登记落位] O7 候选**（timestamped snapshot 判定缺口，T-541.md Risks③/Next②）未入 docs/design/virtual-four-bucket.md §8 O-list（O1-O6 之家）——交 conductor/architect 落位，防丢。
5. **[措辞失准] tools/difftest/v2/cases/maven_module_handle_seat.py:9-10 + L032 报告 Arm 3** 「T-541 lands the seat / 未合入本树」——T-541 明确不落 canonical 席位（无 handle* 持久化），合入后 B 探针仍回 true、臂仍 NOT_RUN；前置应指向 canonical 座位落地票（T-541 Next③）。门逻辑本身无误（探针决定），仅注释会误导复跑预期。
6. **[未测角落补充] internal/adapter/npm/virtual_packument.go:244-262** repointFilteredLatest 的「base 文档自悬空」触发臂超出 L031 证据（L031 只证透传臂），代码注释已自认；未来排除模式差分腿应含 dangling-base 腿（A 面行为未知）。
7. **[CI 治理] .github/workflows/ci.yml:109-115** 预算翻倍缺在册结构性票的树内证据（Linear 不可见于本评审）；conductor 确认 devops 分道票存在，并将「第三次提升=红线」写入该票验收。

### 范围外发现（交 conductor）
- runner 既有 case `npm-virtual-packument-merge` 的 title 仍写「dist-tags latest recompute」（runner --list 可见）——语义已改条件性，标题陈旧（该 case 文件不在本 diff，断言本身经 T-548 复跑 PASS）。
- L032 顺带观察：GAV 不一致 pom 下 B 先答 409、A 先走路由检查（check-order）——D-4 家族邻面，报告已留后续批次，无需本载荷处理。

### 取证注意
- 评审 worktree 原始 HEAD（9d0a0e3c）不含载荷，为只读评审 detach 至 df43869e，报告写毕已切回原分支；本评审零产品码写入。
- run/ 级 wire 证据（l032-arms、l031-npm-r4*）为 gitignored 本地物，本轮按 committed 报告 + 断言键复核算采信（仓内既定惯例）。
