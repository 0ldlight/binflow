# R3-pr-review-b — claude/r3-fixes vs origin/develop（iteration-1542 / Reviewer B·architecture）

```
Ticket:        R3 PR 评审（5 提交：51c1a1c4 / 93aa3573 / 9c83678d / df236376 / f21f1c09；对应 T-540/T-542/T-543/T-544/T-545/T-546，BIN-23/25/26/27/28）
Role:          code-reviewer (reviewer-b · architecture 形态)
Area:          internal/adapter/maven、internal/repo、internal/replication(test)、internal/httpapi(test)、docs/compatibility/known-divergence.yaml、tools/difftest/v2/cases、reports/compatibility/L031
Input:         派发指令（评审形态 reviewer-b + 六项评审重点）；docs/ai-engineering/agent-graph.yaml（域所有权核对）；docs/reverse/virtual-resolution.md §5.1/§5.2；reverse-src 抽查（RequestResponseHelper.java L144 / MergeableMavenMetadata.java L28 / DbStoringRepoMixin.java L543）；五份 reports/agents/T-54x.md；L031 差分报告
Changes:       逐提交 git show 通读全部 diff；上下游追读深度：maven put 链（handlePut→putChecksumDeploy/putSidecar/putFile 全序）、handleGet→serveSidecar/serveFile、virtual_metadata 合并面；repo service ResolveMeta/listRowsChecked 主臂 vs 新投影臂对照 + browse.go listRows 远端臂 + virtual.go getCacheProjection/cacheProjectionFolder；replication engine_test 三处断言点；difftest 三 case + _npmlib（含凭据面扫描）
Files:         ① 51c1a1c4 engine_test.go waitAudit——通过（轮询 helper，精确断言未放宽，-count=2 复跑 ok）② 93aa3573 known-divergence +6——通过（schema 与存量一致，计数复算吻合，见 Compatibility）③ 9c83678d virtual.go/service.go 投影本地语义——通过（同一事实源、错误分类与姊妹臂一致，见下）④ df236376 difftest 三 case——通过（凭据零字面量、_npmlib 沿 _mavenlib 既有形态）⑤ f21f1c09 maven 双修——一个 blocking（成员腿 sidecar 失配，见结论区）
Tests:         只读取证（temp worktree @f21f1c09）：go build ./... ok；go vet 四包无输出；gofmt -l 四域空；golangci-lint ./internal/adapter/maven/... ./internal/repo/... = 0 issues；go test maven 四族（谓词 table/四腿 strip/pom 三 case 族/snapshot_routing）ok 1.753s；replication 三用例 -count=2 ok；httpapi TestCacheFaceRestZeroUpstream -v PASS（0.28s）；repo TestCacheProjection|TestT448|TestList|TestResolveMeta 族 ok 2.450s
Commands:      git log/diff/show origin/develop..claude/r3-fixes（逐提交）；git grep UserAgent（UA 先例普查）；git worktree add --detach /tmp/r3-review-b claude/r3-fixes + 上列 go 三门；python3 yaml.safe_load 台账解析（79 条字段集/authority 类型/分类/去重/口径计数）；grep -i password|token|secret difftest 新四文件；reverse-src 三文件定点抽查
Outputs:       reports/agents/R3-pr-review-b.md（本文件）
Compatibility: known-divergence +6 与既有 73 条 schema 逐字段一致（字段集、authority type ∈ {spec_ruling, pending, differential} 既有词表、无重复 id）；Z 口径复算：total 79 = resolved 31 + gated 4 + open 44（BUG 9/UNKNOWN 33/INTENTIONAL 1/UNSUPPORTED 1）——与 T-544 声明逐项吻合。§5.1 契约一致性：T-542 实现的谓词语义（空 UA=支持、[Jj]ava/.+ 全匹配=不支持、Ivy/Wharf 产品 token=不支持）与 reverse-src 反编译行为等价（非逐行翻译），但 **docs/reverse/virtual-resolution.md §5.1 只写「客户端声明支持 M3 快照记号」，UA 判定族角落未入规格**——当前唯一书面锚是 L030 报告 advisory + 台账 rationale，规格面欠一条 compatibility-engineer 形式化；系统开关 mvn.metadata.version3.enabled 未实现（默认开=当前行为等价，已登记随票定契约，T-542 Next 在案）。台账两条 fix-in-flight 条目（BIN-16/D-4）所记「修后 L030 复验翻绿→resolved」条件已由 r3-t542/r3-t543 两轮 PASS 满足，resolved 回填归 T-536 流程（非本 PR 责任）
Security:      无新增攻击面。pom 门在落库前执行（拒绝件零落盘）；encoding/xml 不解析外部实体；4MiB buffer 上限防无界内存（超限流式放行仅少校验，配 WARN）；difftest 凭据全程 env 注入、新四文件 grep 零字面量（user/password 均引 ctx.sides 运行时值）；投影臂读门挂父 key 的 401/403 分裂维持
Performance:   maven 快照级 metadata GET 增加 ≤1MiB 读+解析+重渲染（与合并面同量级）；.pom PUT 增加 ≤4MiB buffer + 一次 XML 解析（后缀短路非 pom 零开销）；投影 REST 面反而消除每次列举一次上游枚举往返；replication waitAudit 仅测试态 10ms 轮询
Risks:         ① T-542 成员腿 body/sidecar 失配（见 blocking 1）② Ivy/Wharf 精确 UA 串无 A 面 wire 取证（按报告 advisory 产品 token 语义实现，已在 T-542 Risks 登记）③ X-Checksum-Deploy 的 .pom 零字节腿不过 GAV 门（无内容可校，T-543 已登记）④ 翻译代理为 B 腿 harness 适配非差分面，BinFlow 若改 wire 布局需同步 case（T-545 Risks ①）
Blockers:     无取证障碍（全部只读命令可跑、结果与实现报告声明一致；唯 httpapi 六包并行 603s 超时为已知负载 flake，单包复跑 ok——与 T-540/T-542 报告自述一致）
Next:          ① 给 conductor：blocking 1 修复建议（见结论区最小改法）＋补 reports/agents/T-546.md（blocking 2）② compatibility-engineer：§5.1 UA 判定族 + mvn.metadata.version3.enabled 开关契约形式化；BIN-16/D-4 台账 resolved 回填 ③ L031 latest 重算差异（BUG 候选）裁 定 → npm adapter 票 ④ 范围外：agent-graph.yaml 无 internal/replication 的 owns 条目（T-546 落笔域无主，建议 conductor 补图）；member sidecar A 面取证差分臂（T-542 Next 已列）⑤ 双审：本票涉 repository/remote cache/protocol/replication 关键域，reviewer-a 实例须并行在案
```

## 评审报告 R3-pr（形态: reviewer-b）
结论: REQUEST_CHANGES

### 必须修改（blocking）

- **internal/adapter/maven/handler.go:131-135 + handler.go:445-470（serveSidecar/digestOf）** — T-542 成员腿只剥了 body 面，sidecar 面未跟：local 仓快照级 `maven-metadata.xml` 在 java-agent UA 下经 serveSnapshotMetadataStripped（virtual_metadata.go:483）返回剥离体 B'，但同 UA GET `.sha1`/`.md5` 走 serveSidecar→digestOf（handler.go:458 直接读存储 node 的 sha256/digestTriple）＝**未剥离存储体 B 的摘要**——同一客户端拿到的 body/sidecar 对自相矛盾，违反本 adapter 自述契约「digests are computed over exactly what was served」（virtual_metadata.go writeDerivedMetadata 文档注释；virtual 面正是为此走 writeDerivedSidecar，virtual_metadata.go:116）。改动前双面皆存储体（一致），本提交引入失配；且该面恰是 strip 目标客户端类（带校验的 JVM 工具）可达面。T-542 漂移点 b 已诚实登记「A 面未取证不猜」，但 B 自身 pair 一致性不是取证问题。→ 最小改法：handleGet 的 `l.Kind == KindSidecar && l.TargetKind == KindMetadata` 分支复用同谓词（local 仓 + isSnapshotLevelMetadata(Parse(l.Target)) + 谓词拒绝时），serveSidecar 内对命中腿读存储文档→剥 snapshotVersions→writeDerivedSidecar 服务派生摘要（与 virtual 面同姿势）；或补一条 known-divergence 显式登记该面 deferred——二选一，静默失配不可接受。
- **reports/agents/T-546.md 缺失** — 本 PR 六票唯 T-546（51c1a1c4，replication engine_test TOCTOU 修复）无 15 字段工作日志（git ls-tree 全 refs 确认不存在），验证证据只活在 commit message；工作区规则「状态写进 reports/agents/T-<id>.md」「声称完成必须附 15 字段证据」为硬门。→ 从 commit message 提升为日志文件（内容现成），合并前补上。

### 建议改进（non-blocking）

- docs/reverse/virtual-resolution.md §5.1「客户端声明支持 M3 快照记号」未展开 UA 判定族（空=支持、[Jj]ava/.+、Ivy/Wharf token）——实现语义与反编译等价但规格面缺口，建议 compatibility-engineer 票形式化（连同 mvn.metadata.version3.enabled 开关契约，T-542 已 carry）。
- 清洁室卫生：产品码标识符 `clientSupportsM3SnapshotVersions` 与反编译 RequestResponseHelper 方法名逐字相同。行为等价有 wire 证据、结构非逐行翻译（Go 惯用法、自有容错姿势），不构成违规；后续命名建议行为描述式以保持距离。
- X-Checksum-Deploy 的 .pom 零字节腿绕过 GAV 门、409 Content-Type/JSON 缩进风格差、不可解析 pom 放行——均已在 T-543 Compatibility 登记，维持登记即可。
- internal/repo/virtual.go:959 listCacheRows 错误文案用父 key 拼写而同臂 ACL 错误用投影 key——writeServiceError 按 errors.Is 分类不读文案，观测面无差（T-540 Risks 已自注）；不改亦可。
- 台账两条 fix-in-flight（BIN-16/D-4）复验条件已满足，resolved 回填 + matrix 联动勿漏（T-536 流程）。

### 架构面核对结论（评审重点逐项）

1. **UA 谓词分层**：正确。`clientSupportsM3SnapshotVersions` 收敛于 internal/adapter/maven（协议私有能力协商），repo/service 层零感知；全仓普查无其他 adapter 的客户端 UA 嗅探先例（httpapi/uploads.go:1045 为传输帧探测、replication 为出向自报，皆非同类）——无需抽公共谓词，抽了才是为单一实现造接口。
2. **GAV 409 位置**：正确。门在 putFile 首位（策略门 409 之后、unique 改名与存储链之前），拒绝件零落盘；virtual 路由经 handlePut 既有 factsKey/member row 取 **member** cfg——suppress 旋钮语义绑定落库仓，与「maven 链 consult MEMBER」既有架构裁定一致。
3. **投影本地语义读**：无第二事实源。listCacheRows＝browse.go:215 listRows 同一 `md.Nodes().ListByPrefix` 通道去 remoteBrowseRows 折入；错误分类（401/403 分裂、ErrNodeNotFound、wrap 带上下文）与 ResolveMeta/listRowsChecked 姊妹臂同形；读门挂父 key 与 getCacheProjection §2.2 姿势一致；folder 合成 marker 沿主臂 non-local 形。
4. **台账 schema**：与存量一致，计数复算吻合（见 Compatibility 字段）。
5. **测试覆盖**：suppress 三态（absent=default false / 显式 true）覆盖；UA 边界（大小写、空、空白、Ivy/Wharf/Gradle/curl 十行 table）覆盖；httpapi 3-leg 为**真可计数零上游断言**（up.hits.Load() 增量==0，对照腿>0）非仅状态码；replication 修复未放宽任何内容断言。缺口仅 member sidecar 面（blocking 1 的测试面）。
6. **文件地界**：五票写入均匹配所报角色 owns——T-540 dev-go-core（internal/repo+internal/httpapi ∈ owns L66）、T-542/T-543 dev-registry-adapter（internal/adapter ∈ L81）、T-544 compatibility-engineer（docs/compatibility ∈ L57）、T-545 differential-qa-engineer（tools/difftest+reports/compatibility ∈ L114）；run/ 产物 gitignored 沿既有批次惯例。唯一地界异常＝T-546 无日志（blocking 2）+ internal/replication 在 agent-graph 无 owns 条目（范围外，交 conductor）。
```
