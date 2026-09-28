# R4-pr-review-a — Reviewer A（correctness）评审报告

```
Ticket:        R4 载荷分支 claude/r4-payload（6 提交：884b71c0 / 60c20997 / da8a48de / 5a1f500c / df59bce7 / df43869e）— BinFlow 主仓 PR 评审
Role:          code-reviewer (reviewer-a)
Area:          internal/repo（virtual walk）、internal/adapter/npm（packument 合并）、internal/adapter/maven（注释）、tools/difftest/v2、docs/compatibility、.github/workflows/ci.yml
Input:         conductor 派发（T-id=R4-pr、范围=git diff 1517949b..df43869e、形态=reviewer-a）+ 通读 docs/reverse/virtual-resolution.md §3-§5 / maven-npm-pypi.md §2.6 / docs/compatibility/{contracts/maven-virtual.yaml, known-divergence.yaml, matrix.yaml} / reports/agents/T-{541,548,549,550}.md / reports/compatibility/L032 / internal/{repo,adapter/npm,adapter/maven,httpapi,remote} 上下游
Changes:       diff 全量 27 文件 +2142/-34；上游追读深度：virtual.go 全文（getVirtual/expandVirtualMembers/virtualMemberOrder/plainSteps/isSnapshotResolutionPath/splitMemberChecksumSuffix）、npm disttag.go（crownLatest）/reindex.go（recomputeLatestTag 调用面）、httpapi/repositories.go configJSON（handle* 持久化路径 remote/local 两臂）、remote/projection.go（投影继承注释）、maven virtual_metadata.go filterMetadataSteps 与 config.go RepoConfig 双源
Files:         internal/repo/virtual.go — 跳过逻辑与规格 §3 规则 4/6 逐句吻合，旁车豁免=规格原文（checksum 旁车文件不受此限）；默认配置（absent=双 true）行为逐分支比对为零变更。通过。internal/adapter/npm/virtual_packument.go — repointFilteredLatest 条件性与 L031 r2≡r3 证据吻合；crownLatest 只补缺席 latest，读时/合并两面无交叉；recomputeLatestTag 仅存 reindex.go（成员存储面），面分离成立。通过。internal/adapter/maven/virtual_metadata.go — 仅注释（漂移点双站点同翻钉死），行为零改动。通过。tools/difftest/v2/cases/ 六新 case — 期望值全部静态钉死（spec/live 锚），judge() 双面-vs-期望且 equal-but-unexpected 也 FAIL，fixture 翻转逃逸结构性不存在。通过。docs/compatibility 三件 — 契约 off 臂禁猜纪律执行到位；计数复算全对。通过。ci.yml — 30m→60m 有 run 证据+方差归因+结构性跟进注记。通过（见 non-blocking 4）。
Tests:         见 Commands/Outputs——四包全绿 + 新测试 -race 绿 + lint 0 issues + difftest 发现门 16 case + 契约/台账/矩阵计数复算全等
Commands:      在 df43869e 干净提取树（/tmp/r4review-src，git archive）执行：go build ./... && go vet ./internal/{repo,adapter/npm,adapter/maven}/ && gofmt -l（空）；go test ./internal/repo/ ./internal/adapter/maven/ -count=1；go test ./internal/adapter/npm/ -count=1；go test ./internal/repo/ -run 'TestVirtualHandlePolicy|TestVirtualReleaseSkipDropsCacheFacet|TestVirtualLocalHandlePolicyMatrix|TestVirtualLocalReleaseSkipSidecarExempt' -count=1 -race -v；go test ./internal/adapter/npm/ -run 'TestMergeLatestTagConditional|TestVirtualLatestTagPassthroughThroughStack|TestVirtualPackumentMergeMatrix|TestVirtualPackumentAggregationSkipsCacheFacetSteps' -count=1 -v 与 -race；GOLANGCI_LINT_CACHE=/tmp/gcl-cache-r4review golangci-lint run ./internal/repo/... ./internal/adapter/npm/... ./internal/adapter/maven/...；python3 tools/difftest/v2/runner.py --list；python3 七文件 ast.parse；python3 yaml 计数对账（matrix 38/51/1、台账 80=33+4+43、open BUG 8）；git diff 新增行凭据 grep
Outputs:       ok internal/repo 80.945s / ok internal/adapter/maven 19.667s / ok internal/adapter/npm 28.990s；新测试全 PASS（四象限×2 + cache-facet 拒绝 + 旁车豁免 + latest 条件表 6 臂 + 真栈穿透双面）；-race ok 10.687s/2.873s；golangci-lint "0 issues."（隔离缓存复跑；首跑命中他 worktree 陈缓存，弃用）；runner --list=16 case（六新全发现）；计数对账与 T-549 声称逐位相同；凭据 grep 仅命中报告纪律描述文本，零字面量
Compatibility: A 形态核对项：T-548 与 L031 live（r2≡r3 及修复后 r4≡r4b）一致；T-541 与 §3.4/§3.6 高置信规格逐句一致（差分腿候选已在票内登记）；T-549 契约 off 臂 SPECIFIED/medium 禁猜纪律正确
Security:      攻击面走查：memberHandlePolicy 只读 config JSON、宽容默认、不触凭据键；路径仅做后缀/分段判定（validateNodePath 门不变，无新增拼接面）；forward-proxy 仅绑 127.0.0.1:0、凭据内存态、log_message 静默；_b_leg SQLite 插值全部编译期常量+库内读回值（scratch 库）；凭据硬检查通过（新增行零凭据字面量，唯一 grep 命中=报告纪律描述文本）
Performance:   memberHandlePolicy 每成员行每请求一次 ~百字节 JSON 探针（与 priorityResolution 探针同量级）；repointFilteredLatest O(1) 替代原全序扫描略省；跳过分支省探针与上游接触。无热点回归
Risks:         ① handle* 仅作用于下载面：聚合 browse/listing（getVirtualFolder 与 plainSteps 系 walk）不适用 handle* 策略——local 成员经 passthrough 携 handle*=false 时 browse 可见而下载 404（A 面 browse 行为未取证，非本票回归）；② T-548 角落：base 无 latest 而后成员有→并集补位后条件性规则作用于补位值，未经 live 对拍（真实 registry latest 恒在，严重度低）；③ release 族二元判定对非 maven 包型同样门控（handle* 写进 generic/npm 成员 passthrough 即生效）——票内 Risks② 已登记、差分候选在册
Blockers:      无（取证环境完备：df43869e 干净提取树全命令可跑）
Next:          ① O 族/differential 候选：browse 面 vs 下载面 handle* 一致性差分腿（A 面 browse 对 handle*=false 成员的可见性）；② maven_module_handle_seat.py 探针文案将随 T-541 合入而过期（reason 指向"T-541 未合入"，实际 T-541 不落 canonical 席位）——建议随 canonical 席位票顺手改一行；③ CI Test 腿 60m 后若续涨需结构性拆分（已在提交注记，devops 跟进）；④ 本分支含 virtual resolution handle* 面（repository 域关键面）——建议补 reviewer-b 实例（架构/契约视角，本报告为 A 形态单实例）
```

## 评审报告 R4-pr（形态: reviewer-a）
结论: APPROVE

### 必须修改（blocking）
- 无。

### 建议改进（non-blocking）
1. **browse/下载面策略不一致**（internal/repo/virtual.go:344-356 vs getVirtualFolder/plainSteps 调用族）：handle* 跳过仅落下载 walk；getVirtualFolder 收全量 order、listing/copy/move 的 plainSteps walk 均策略盲。local 成员 passthrough 携 handleReleases=false（今日写面可达）时，聚合 browse 列出该成员制品而虚拟 GET 404。规格 §3 只覆盖下载策略、A 面 browse 未取证——非回归（browse 行为本票未动），建议登记差分候选/O 族观察臂。→ 改法：无需本票动码；差分腿取证后若 A 面 browse 也跳，则 getVirtualFolder 入口加同款 family 过滤。
2. **T-548 未对拍角落**（internal/adapter/npm/virtual_packument.go:207-217）：base 无 latest、后成员有 latest 时，并集补位（first-wins over absent key）后条件性规则作用于补位值——live 未验证。与已登记的「排除模式臂不可达」同族角落；真实 registry latest 恒在，低危。→ 改法：L 腿扩臂或注释补一行角落声明即可。
3. **探针文案过期风险**（tools/difftest/v2/cases/maven_module_handle_seat.py:8-10, 84-88）：docstring 称 "T-541 lands the seat"，实际 T-541 只落 walk 跳过、canonical 席位仍缺席（httpapi remote 臂 configJSON 无 handle* 座位，L032 echo=true 将维持）——T-541 合入后该 case 的 NOT_RUN reason「T-541 … not merged in this tree」变为不实描述。→ 改法：reason 改指 canonical 席位票（一行措辞）。
4. **CI 预算翻倍的挂起遮蔽面**（.github/workflows/ci.yml:113）：60m 预算下真挂起测试最坏 60m 才暴露（原 30m）。证据链成立（无单点挂起、全包均匀变慢、auth/search 未触碰 +26%/+91% 判 runner 方差），且结构性跟进已注记——接受，但若 Test 腿耗时续涨应先拆 lane 而非再调预算。
5. **非 maven 包型的 release 门控**（internal/repo/virtual.go:351-356）：release 族=「非快照且非旁车」的二元划分使 generic/npm 成员的普通文件路径也受 handleReleases 门控（规格原文条件是「路径可解析出模块信息」）。T-541 Risks② 已如实登记+差分候选在册；handle* 写进非 maven 仓本属 off-label 用法，维持登记即可。
