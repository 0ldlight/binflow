Ticket:        R7 payload 合并前评审（BIN-44 T-562 stage1/2 + BIN-48 T-566 + T-564 + T-565 三部曲 + L035/L036 取证批）
Role:          code-reviewer (reviewer-b, architecture 维)
Area:          maven adapter（walk/物化）、generic adapter（Location 前缀）、compatibility 体系（契约/台账/金样/matrix）、web 控制台、fern/docs
Input:         conductor 派发（10 提交 5a1e4356..4145b059，diff origin/develop..claude/r7-payload，71 文件 +4162/-229）；通读 contracts/maven-virtual.yaml ⑨⑩⑪⑫ 全文及 diff、walk.go/handler.go/calc.go/put.go/snapshot.go/api.go、generic handler/iteminfo、四组金样 12 文件 + derived-sidecar-validators metadata、known-divergence.yaml、matrix.yaml、L035/L036 报告、五轮 run results.json 与 b-leg evidence、T-565 四文档镜像、RepoDeleteConfirm/repos.ts
Changes:       逐提交评审：契约批（943ce899）↔实现（fd2d0606）逐 ARM 对照；台账收官（4145b059）实枚举复算；金样 B 腿与 run/l035-r7-impl-r1、t566-r1 evidence 值级对账；探针批（14a8943d/0097fadf）probe-only 纪律验证；文档四面 DELETE 级联口径互查；上游追读深度=Parse/parseArtifactFile/artifactFile/recalcSync/afterArtifactDeploy 调用点（put.go:235 writeCreated 前）、NodeReader seam（api.go:23-59）、productPrefix 三角（maven put.go:590 / generic handler.go:251 / httpapi router.go:33）
Files:         docs/compatibility/contracts/maven-virtual.yaml——⑩⑪⑫新增+⑨改写，ARM↔代码逐条互证成立（见结论区）；known-divergence.yaml——+11 条（L036 十条+t8 一条）、3 处翻 resolved 均 fix_ref=fd2d0606 在树；matrix.yaml——201 行零增删零行态变，仅 D12-R18 last_difftest/note 两字段更新；golden/maven/ 四组 12 文件+derived-sidecar 勘误，yaml 全解析通过；internal/adapter/maven/walk.go 242 行——分层正确（adapter 层内、复用 NodeReader/filterMetadataSteps/virtualMetadataSteps，无跨包摸内部结构）；handler.go——walk 插桩位序正确（class gate 409 之后、metadata-strip/file plane 之前；virtual metadata merge 之前，t7 不走 walk 成立）、解析门 GET/HEAD→404 仅读面、PUT/DELETE 维持 400 且 unobserved 理由在注释；calc.go——recalcSync(pom module 腿) 于 writeCreated 前调用，与契约「write-path materialization, synchronous」精确一致；snapshot.go——versionDirPrefix 提取纯重构；generic handler/iteminfo——productPrefix 与 maven 既有形态同构（同名常量/同两处渲染点/注释互注不跨包 import）；tools/difftest 6 case——结构与既有惯例一致（run(ctx)/write_evidence/asserts+raw/netrc 0600 凭据不进 argv）；web（repos.ts/RepoDeleteConfirm/i18n/e2e）——消费面与级联语义一致，退役锚（repo-delete-content/repo-delete-reason）有注记；docs/user+fern+docs/design 四面口径一致
Tests:         go test ./internal/adapter/maven/... ok 31.3s + ./internal/adapter/generic/... ok 11.6s；go test -race -run 'TestPlainSnapshotWalk|TestVirtualPlainWalk|TestModuleMetadata|TestSnapshotMaterialize' ./internal/adapter/maven/ ok 14.6s；五轮差分 results.json 实读：impl-r{1,2} 四 case PASS×2、t566-r{1,2} PASS×2、final-r1 四 case PASS——与契约/台账声称一致；playwright e2e 未跑（本环境无浏览器面，四态=NOT_RUN）
Commands:      git log/diff --stat origin/develop..claude/r7-payload；python3 yaml.safe_load known-divergence/matrix/maven-virtual/13 金样文件 + collections.Counter 复算（98=47+51；open BUG12/UNKNOWN33/INTENTIONAL5/UNSUPPORTED1）；python3 subprocess git show 双版 matrix rows 逐行对账（id 集合与顺序相等、state 零变更、字段级变更仅 D12-R18×2）；diff origin/develop..分支 ledger 增量枚举（+11/-0）+ 五族互引/三锚 python 验证；b-leg.json 值级对账 4 组；go test / -race / go vet（exit 0）/ gofmt -l（空）；make spec-check（in sync, exit 0）；npx tsc --noEmit（exit 0）；凭据字面量 grep 扫描六新 case（零命中）
Outputs:       reports/agents/R7-pr-review-b.md（本文件）
Compatibility: ⑩⑪⑫⑨ 四契约 ARM 与实现逐条一致（触发面/选择键/tie 语义/sidecar 面/metadata 不走 walk/remote 面 unobserved 声明全对上）；⑫ 秒级平手取 walk 序靠后成员=契约 verdict 与代码注释互见，unobserved 臂如实保留；⑨ L033 勘误三处回填齐（契约 l033_erratum/golden known_gaps/台账 rationale）；台账 Z 复算精确命中声称值；五族 Location BUG 新账 B 侧为静态代码证据（B live BLOCKED license 门，如实标注，review_gate 已挂差分门）；divergence_ref 三锚（plain-snapshot-path-resolve-404 / auto-materialize / non-snapshot-spelling-400-vs-404）全部存在且翻面一致
Security:      新增面走查：walk 无路径穿越新增面（候选域被 prefix+四元组约束、Parse 失败即弃）；解析门 404 化不放宽授权（class gate/authorities 前置不变）；difftest case 凭据走 0600 netrc 不进 argv/日志；全 payload 凭据字面量扫描零命中
Performance:   （B 形态：热 path 结构面）walk 仅在 storage miss 后触发一次 ListByPrefix（local 单列/virtual 逐 walking 成员，沿用 metadata merge 同型遍历+filterMetadataSteps 过滤，无新增锁）；recalcSync 使 pom PUT 增加一次同步 module 目录重算（exec 锁内，与既有 unique version 腿同型——PUT 时延换计数面确定性，契约裁定认可）；serveSidecarOfPath 沿用既有「开流取 node 即关」形态
Risks:         ①跨成员 mtime 同秒平手实现取 walk 序靠后成员（A 面未观测，契约已声明为 proxy——若 A 实为非确定性，差分同秒构造将天然难复现）；②非 unique 家园+目录含 ts 候选的 GET 组合未单独取证（实现按 W3 c3「选版与落库来源无关」立场走 resolve——契约 same_face_note 可再显式一句）；③DELETE 回收 async 窗口维持（契约/golden 均注记，T-566 Risks 在案）
Blockers:      无（全部必执行取证项均跑通）
Next:          1) 建议确认 Reviewer A 实例已并行派发（maven/protocol 关键域双审强制，本报告仅 B 维）；2) R8 一票同款修法（五族 Location requestBase+productPrefix，deb/rpm/helm/nuget-bare/cargo——台账 review_gate 已写修法形状）；3) L032 rest/repo-delete-nonempty-cascade 计数臂复验（+1/module 落地后，实现者已列 T-566 Next——conductor 确认建票）；4) license 门解封后五族 Location B 腿活体补跑；5) 范围外发现：version-dir 列表过滤循环三处同形（calc.go:399/snapshot.go:179/walk.go:74），下次开该区域可收敛为一个共享 helper（不阻塞）

## 评审报告 R7-pr（形态: reviewer-b）
结论: APPROVE

### 契约↔代码互证明细（重点项 1）
- ⑩ 触发面：`isPlainSnapshotArtifact`（walk.go:42，plain -SNAPSHOT artifact 门）+ storage-miss 探测（svc.Get 命中→ordinary plane；非 ErrNodeNotFound→ordinary plane 渲染授权/分类失败）+ sidecar 面（KindSidecar→TargetKind=artifact 走 walk，t5/t6）——三要素与契约 trigger 段一致；「四元组归约为 (ext, classifier)」成立的前提已核实：parseArtifactFile（calc.go:537）内部强制 filename 前缀=`{Module}-` 且 timestamped 形要求 `{BaseRev}-`，artifact/baseRev 由目录拼写钉死。
- ⑩ 选择键：selectWalkCandidate（walk.go:94）`ts 逐字符比较（yyyyMMdd.HHmmss 定宽=数值序）+ bn int64 数值比较 + ts 平手→bn`，上传序/mtime 不参与——与 W2 s1/s2/s2b/s3 精确一致（s3 bn10>bn9 由 int64 比较保证）。
- ⑩ 统一出口：walk 走 serveNode（handler.go:272 起）——ETag/Content-Length/Range 206/条件 304 全部针对 resolve 后实体，W3 same-face 裁定「one serving path」由共享出口结构性保证。
- ⑪：ServeHTTP 解析门仅 GET/HEAD→404（handler.go:88-92），PUT/DELETE 维持 400 且「A 的 PUT 面未观测不猜」注释在案——与契约 error_behavior/unobserved 臂一致。
- ⑫：serveVirtualWalk 成员内先 W2 选候选、跨成员 `!cand.mtime.Before(best.mtime)`（storage mtime 最后落库者胜、平手取 walk 序靠后）；remote 成员 ListVirtualMember 失败即跳过（unobserved 声明一致）；声明序/filename-ts 不参与的 v2/v3 否证由单测+差分双面钉住。
- ⑨：afterArtifactDeploy pom 腿 recalcSync（calc.go:230）且调用点在 writeCreated 之前（put.go:235）——「PUT 响应返回前物理节点必然落地」措辞与实现精确一致；release version 级 404（m0/m3）与 SNAPSHOT version 级同步物化（m6，`pom && l.Snapshot` 腿）均被单测断言+差分双轮钉住；DELETE 回收 async 残余如实注记未静默。

### 台账/金样/matrix 复算（重点项 2/3/4）
- Z 复算（python 实枚举）：total 98 = resolved 47 + open 51；open=BUG 12/UNKNOWN 33/INTENTIONAL 5/UNSUPPORTED 1——与声称逐位一致。增量 +11 条（L036 十条：五族 Location BUG×5+nuget push+pypi 头=BUG×7，mime-ownership/pypi href/stdlib 漂移=UNKNOWN×3；另 stage-1 批 t8 新账 1 条），零删除。
- 翻面：auto-materialize、t8 两账 resolved（fix_ref=fd2d0606）+ walk 主条目残余段收口追记，fd2d0606 在分支树。
- 五族互引：deb 条 rationale 携同族名册（「同族五条本批入账…R8 一票同款修法」），rpm/helm/nuget-bare/cargo 四条回指 deb——hub 模式链路完整；divergence_ref 三锚全部落在存在的 ledger id 上。
- 金样 B 腿对账（≥4 组值级）：trigger-matrix（t2 len=280 且 ETag=bc33934…=t5 sha1 交叉一致；t4 206+len16 且 hex 前缀解码=`<?xml version="1`；t8/t9=404；t10=280）；selection（八断言 w2s1/s2/s2b/s3/s4×2/s5×2 与 golden r7_closeout 逐键同值）；virtual（v1 vNEW/v2 vOLD/v3 vNEW）；auto-materialize（m0 404@30s/m1 1.0.0/m2 1.1.0|1.1.0/m3 404/m4 [1.1.0]+404/m6 200@0s；m5 B 侧跳过在 golden 如实声明）。
- matrix：双版逐行对账 id 集合与顺序全等、state 零变更、唯一字段级变更=D12-R18 last_difftest（补 L035→impl→t566→final 链）+note——201 冻结纪律成立，summary 计数器与声明一致（contract_ref 38/golden_ref 3/last_difftest 52）。

### 必须修改（blocking）
- 无

### 建议改进（non-blocking）
- walk.go:74 / snapshot.go:179 / calc.go:399 三处 version-dir 列表过滤循环同形（rel==""/含"/"/metadataFileNames 跳过）——下次开该区域收敛为共享 helper。
- 契约 ⑩ trigger ②（unique 家园）在实现中由 ③（存在 ts 候选）蕴含而非读仓配置判定——W3 c3「选版与落库来源无关」已背书该立场，same_face_note 可再显式覆盖「非 unique 家园+客户端 ts 落库」组合一句。
- L036 五族 Location 新账 B 侧证据为静态代码（B live BLOCKED license 门）——已如实标注，解封后活体腿补跑即可（review_gate 已挂）。
- 台账增量为 11 条而非任务简报所述 10（t8 属 stage-1 批）——批次注记内部自洽，仅口径精度备注。

### 范围外交办（交 conductor）
- rest/repo-delete-nonempty-cascade 计数臂复验建票确认（L032 A=6-vs-B=5 口径差随 +1/module 应消解——实现者已列 T-566 Next，勿遗）。
- 双审实例 B 已出；确认 Reviewer A（correctness 维）并行在途。
