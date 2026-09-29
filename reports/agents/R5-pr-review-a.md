# R5-pr-review-a — Reviewer A（correctness）评审报告

```
Ticket:        R5 载荷 = 分支 claude/r4-report 全部未提交改动（12 修改 + 10 新增；五票：T-551 stage1/2、T-553、T-556、T-552、T-551-contract 终批）— BinFlow 双审 Reviewer A
Role:          code-reviewer (reviewer-a)
Area:          internal/adapter/maven（writeDerivedSidecar / filterMetadataSteps）、internal/repo（getVirtual walk 镜像）、internal/httpapi（deploy 拒绝族路由拦截）、Makefile + .github/workflows/ci.yml（Test 分道）、docs/compatibility 三件 + golden、tools/difftest/v2 新 case
Input:         conductor 派发（形态=reviewer-a、correctness 重点清单）+ 通读 docs/compatibility/{contracts/maven-virtual.yaml, known-divergence.yaml, matrix.yaml} / reports/agents/T-551-{contract,mavenfix},T-552,T-553,T-554.md / reports/compatibility/L033-r5-fix-verification.md / internal/adapter/maven/{rangecond.go,handler.go,virtual_metadata.go} / internal/repo/virtual.go / internal/httpapi/{router.go,middleware.go,envelope.go} / tools/difftest/v2/cases/maven_local_handle_walk_skip.py / run/l033-r5-r{1..6} 证据
Changes:       diff 全量 12 文件 +612/-214 + 未跟踪 10 项；上游追读深度：evalConditional/etagMatch 全谓词树（rangecond.go:134-175）、writeDerivedSidecar 两调用链守卫（handler.go:137-166 + serveVirtualMetadata:96-118）、getVirtual 全循环与 isSnapshotResolutionPath/splitMemberChecksumSuffix、dispatchContent enforce→inner 包裹序与 cacheProjectionParent/splitFirstSegment/CacheProjectionTarget 解析链、make test recipe vs HEAD 逐字节比对
Files:         internal/adapter/maven/virtual_metadata.go — writeDerivedSidecar 翻面逐谓词核（见 Tests）：IMS≥trunc(LM)→304/旧→200、etag="" 使 INM 含 `*` 恒不短路（etagMatch 先行守卫）、304 无 body 无 Content-Length、CL 移条件判定后、HEAD 臂保留；filterMetadataSteps 模块级 hr 跳过删净、mergeMetadataDocs 去重/排序走既有未动代码路径。通过。internal/repo/virtual.go — walk 镜像同翻对称；releasePath/isChecksumSidecarPath 死码删净（全仓 grep 零悬空引用，deb/remote 的 releasePath 系同名异域局部变量）；handleReleases 字段保留携带与 row-read seam 对齐。通过。internal/httpapi/router.go — 两拦截谓词核：leg1 真名 <K>-cache 本地仓优先（Get 命中则拦截不可达）、leg2 maven+remote+PUT 三元组、嵌套/编码路径由 EscapedPath 切分兜住、匿名 401 先行由 enforce 包裹序结构保证且测试断言、writeError 走 json.Encoder 零注入面。通过（1 non-blocking）。Makefile/.github — test-pkgs 目标与 test recipe 字节不变核过；**light lane 多行 PKGS 传参链断裂（blocking 1）**。docs/compatibility 三件 + golden — 台账 85=37+4+44 独立复算逐位吻合、三 resolved + 4 新 UNKNOWN 在位、matrix 38/2/51 吻合、契约条目与产品码实形一致。tools/difftest 新 case — dual-oracle/构造性守卫/混杂对照/finally 清理齐备。通过
Tests:         见 Commands/Outputs——三包哨兵全绿 + lint（隔离缓存）0 issues + vet 0 + 负证逻辑可达性核对（T-556 六腿 vs 旧代码路径逐腿推演成立；T-551 新测试对旧 ETag=digest+零 LM 姿势三臂必翻红）+ L033 证据对账全吻合 + 台账计数独立复算全等 + CI 传参链本地复现（blocking 1 实证）
Commands:      go build ./... && go test ./internal/adapter/maven/ ./internal/repo/ ./internal/httpapi/ -count=1；GOLANGCI_LINT_CACHE=/tmp/lint-r5-review-a golangci-lint run ./internal/adapter/maven/... ./internal/repo/... ./internal/httpapi/...；go vet 同三包；make 版本语义实证：make -f /tmp 复刻 Makefile PKGS="$(go list … | grep …)"（多行形态）→ go list "invalid char '\n'" → CANON 空 → guard exit 2；对照空格分隔形态 COUNT=8 正常；go list ./... | grep -v '/binflow/web/' | grep -v -E '/(httpapi|repo|auth|search)$' | wc -l（=36，恰剔四重包零误伤）；python3 对账 run/l033-r5-r{1,2,3,5,6}/results.json + evidence/{maven-member-snapshot-sidecar-304, rest-kcache-deploy-404-wording, maven-local-handle-walk-skip}/summary.json；python3 yaml 台账/matrix 独立计数；git show HEAD:Makefile 与工作树 test recipe 比对；payload 全文件凭据字面量 grep
Outputs:       ok internal/adapter/maven 21.211s / ok internal/repo 123.075s / ok internal/httpapi 249.037s；golangci-lint "0 issues."（/tmp/lint-r5-review-a 隔离缓存）；vet 无输出；多行 PKGS 复现输出 = malformed import path "…adapter\ngithub.com/…/adapter/cargo\n…" invalid char '\n' + CANON=[] COUNT=0 + guard exit 2；L033 对账：sidecar r3=17 维三桶全空、armB r1==r2==r3 expected 全等三桶全空、walk-skip r5==r6 断言值 True + 分歧键集 True、r3 分歧集==r5 集 minus {virt_modmeta_hwm_status, virt_modmeta_hwm_versions} True（11→9）、r5 b 面 hwm=200/1.0.0-SNAPSHOT；台账复算 total 85 = resolved 37 + gated 4 + open 44（BUG 7/UNKNOWN 35/INTENTIONAL 1/UNSUPPORTED 1）与 matrix changelog 终算逐位同；凭据 grep 零字面量（仅产品 dev 默认注释与 env 注入口径，均存量）
Compatibility: 本视角核对证据-声称一致性：L033 报告 17/17、r5≡r6、r3-set-minus-hwm、22 维 11/11 全部由 run 工件复现（见 Outputs）；r1 results.json 对 armB 标 NOT_RUN 系同轮两次 runner 调用的产物层痕迹（evidence/ 真数据在、三轮 expected 全等、三桶空）——报告声称有据，非虚假证据（non-blocking 3）；Z 终算 85 与第一半「88」口径笔误已在 matrix changelog 如实纠正；契约条目（validator 族/no-etag/IMS 命中→304/跨秒残差单立 unobserved+known_gaps）与产品码逐谓词一致
Security:      攻击面走查：deployNoLocalRepoMessage 的 repoKey 反射经 writeError json.Encoder（SetEscapeHTML(false) 但 json 编码逃逸在）——零注入；拦截谓词只读 row.Type/PackageType/首段后缀，无新解析面（splitFirstSegment 严格 PathUnescape 失败原样透传由 adapter 400 兜底）；无凭据触碰；difftest case 全 Basic env 注入口径；凭据硬检查：将提交 22 文件零 admin/password 字面量
Performance:   writeDerivedSidecar 每 response 一次 time.Now()+一头设置（小 metadata 派生面，非热 path）；T-556 少两布尔判断 + release 路径免一次 splitMemberChecksumSuffix；leg1 拦截多一次父行 Get（仅 -cache 后缀键且行查失败时）；无锁/分配回归
Risks:         ① blocking 1 修复后首轮 main run 五 lane 仍为 NOT_RUN 面（worktree 禁 push）——修完必须观察首轮 lane 包数日志行（echo "lane light: $LANE_PKGS" 输出 36 包）；② nightly race_full 20m/分片 vs httpapi 单包实测 17.6m 共租慢化可超窗（T-552 Risks④ 在案，conductor 重校准决策，非本载荷面）；③ writeDerivedSidecar 跨秒 LM per-request 戳残差已单立 UNKNOWN（maven/derived-sidecar-lm-per-request-stamp）+ stored-stamp 升级微票路径预留——A 形态不裁
Blockers:      无（取证环境完备；make 4.x 行为差异已用双形态实验钉死破坏面，具体落哪种破坏模式由 CI 首轮暴露）
Next:          ① 修复 blocking 1（一行 tr）后 conductor 须盯首轮 main run 五 lane：light lane 日志应列 36 包、五 check 名落位；② nightly race_full 重校准票（T-552 Next② 既有）；③ 范围外交接：Arm C 提案①②③ + 跨秒 LM 残差四 UNKNOWN 已在台账待裁，无需本评审重复立项；④ 本载荷含 repository 域关键面（virtual walk）——reviewer-b 实例（架构/契约形态）应由 conductor 并行派出，本报告为 A 形态单实例
```

## 评审报告 R5-pr（形态: reviewer-a）
结论: REQUEST_CHANGES

### 必须修改（blocking）

1. **CI light lane 多行 PKGS 传参链断裂——lane 恒红或静默缩水至 1/36 包**（.github/workflows/ci.yml `test` job "Test lane" step + Makefile `PKGS_CANON`）
   - 机理：light lane 以 `LANE_PKGS="$(go list ./... | grep -v '/binflow/web/' | grep -v -E '/(httpapi|repo|auth|search)$')"` 推导补集——`go list` 输出为**换行分隔**（命令替换保留内部换行）；`make test-pkgs PKGS="$LANE_PKGS"` 把含原始换行的值递给 make 命令行变量；`PKGS_CANON := $(if $(PKGS),$(shell $(GO) list $(PKGS)),)` 在赋值面展开该值。
   - 本地实证（make 3.81，复刻 Makefile + 真实 go list 输出）：`$(shell)` 快路径把整个多行 blob 作为**单个参数**交给 go list → `malformed import path "…adapter\ngithub.com/…/adapter/cargo\n…": invalid char '\n'` → PKGS_CANON 为空 → 守卫 `test -n` 触发 exit 2 → **light lane 每次 main push 必红**。
   - CI 面（GNU make 4.x，ubuntu-latest）：最可能走 /bin/sh——换行成命令分隔符：`go list <首包>` 执行、其余各行作命令 not found、$(shell) 只捕到首包 stdout → PKGS_CANON=单包（`github.com/lzwzzy/binflow/cmd/bf`）→ 守卫通过 → **light lane 绿灯只测 1/36 包，35 包静默跌出 main-push -race 覆盖**。两种破坏模式 whichever 先现，gate 都是坏的；后者（绿灯静默缩水）更危险。
   - 佐证：T-552 的本地验证只跑了空格分隔（`PKGS="./internal/metadata/ ./internal/config/ ./internal/auth/"`——复刻验证 COUNT=8 正常）与单包两形态，CI 真实多行形态从未执行（报告 Risks① 自认首轮 NOT_RUN）；Makefile 注释「any spelling works」对多行拼写不成立。
   - 修复（最小一行，.github/workflows/ci.yml Test lane step）：`LANE_PKGS="$(go list ./... | grep -v '/binflow/web/' | grep -v -E '/(httpapi|repo|auth|search)$' | tr '\n' ' ')"`。建议同时把 Makefile 守卫加固为可感知 go list stderr 失败（如 recipe 内 `$(GO) list $(PKGS) >$@.canon 2>$@.err || { cat $@.err; exit 2; }` 形态，或注释如实收窄「fails LOUDLY」到「全列表不可解析才响」——见 non-blocking 2）。
   - 注意：本缺陷不波及四条重包 lane（单包 PKGS 无换行，复刻验证 COUNT=1 正常）；补集正则本身干净（实测恰剔 internal/{httpapi,repo,auth,search} 四包、余 36、总 40）。

### 建议改进（non-blocking）

1. **leg1 拦截掩盖非 not-found 查询错**（internal/httpapi/router.go:2317-2320）：T-553 腿 1 位于 `Repos.Get` 的 `err != nil` 分支内，未区分「仓不存在」与「存储层故障」——若 `<K>-cache` 行查返回 500 类错误而父行查恰好成功，答 deploy 拒绝 404 而非诚实 500（writeRepoLookupError 的 notfound/other 分流在此路径不可达）。实际暴露极窄（父行查成功即证 store 健康、子行 miss 即真 not-found）。→ 改法：谓词前加 `errors.Is(err, …NotFound)` 类收窄（store 有对应哨兵错误时），或维持现状并注释声明该取舍。
2. **Makefile 注释高估 typo 响亮度**（Makefile:128-133 注释 vs PKGS_CANON 实现）：`go list` 对混合列表中的单个 typo 只把坏项落 stderr、好项照常出 stdout——「a typo'd static lane fails LOUDLY」仅在**整条 lane 全不可解析**时成立，混合列表 typo 是静默缩水（stderr 可见但 job 转绿）。今日四 lane 皆单包（typo→空→exit 2 响亮），暴露面=未来多包 lane。→ 改法：注释收窄一句，或守卫加 `$(GO) list` stderr 感知。
3. **difftest 轮次目录产物层分叉**（tools/difftest/v2/run/l033-r5-r1/results.json）：r1 的 results.json 对 rest-kcache-deploy-404-wording 标 NOT_RUN（同轮两次 runner 调用、后次覆写 results.json 只记自跑 2 case），而 evidence/rest-kcache-deploy-404-wording/summary.json 为真 PASS 数据（已核 r1==r2==r3 expected 全等、三桶全空）——L033 报告 PASS×3 有据非虚，但 results.json 与 evidence 的分叉会误导未来审计对账。→ 改法：一轮一目录（partial 轮用独立目录名）或 results.json 追加合并；至少 L033 报告补一行 r1 产物层注记。
4. **writeDerivedSidecar 304 保留 X-Checksum-* 头**（internal/adapter/maven/virtual_metadata.go:574-575）：条件命中早退路径携 X-Checksum-Sha256/Sha1——RFC 允许、与 body 面既有姿势一致、无 A 面取证维度，维持原状可接受；仅作记录。

### 范围外交接（不阻塞本载荷）

- nightly race_full 20m/分片 vs httpapi 单包 17.6m 实测的预算重校准——T-552 Risks④ 已登记，归 conductor。
- Arm C 三新面孔（409 措辞族/成员 GET class 门/plain-SNAPSHOT 拼写解析）+ 跨秒 LM 残差四 UNKNOWN 已在台账、review_gate 两程内升级默认——归 compatibility-engineer 裁定链，无需评审侧动作。

## 追加 — blocking 1 修复复核（同日，conductor 直改后）

- 修复实态（.github/workflows/ci.yml "Test lane" step）：推导管道尾接 `| tr '\n' ' '`，注释如实记录两条失效路径（make 3.81 malformed-path / make 4.x sh 分割只测首包）与修复形态依据（Makefile target 与 T-552 本地验证实际行使的空格分隔形态）——与本报告 blocking 1 的机理定性逐句一致。
- 复核取证（本 reviewer 亲跑，worktree 树上）：
  - 推导链形态：`go list ./... | grep -v '/binflow/web/' | grep -v -E '/(httpapi|repo|auth|search)$' | tr '\n' ' '` → **单行 36 词（换行数 0）**——多行断裂源消除。
  - 全量干跑：`make -n test-pkgs PKGS=<上述 36 词> TEST_TIMEOUT=60m` → 守卫通过；**L026-4 预算隔离仍正确触发**——`internal/metadata` 单独首段（BUDGET_SUB 分支），其余 35 包全量进第二段 `go test -race` 命令行——无丢词、无误剔，PKGS_CANON 规范化对空格分隔形态保词。
  - conductor 等效验证（2 包真实跑：console ok 1.736s / metrics ok 1.981s）+ 上述 36 词干跑，两段证据合围：推导形态 → make 变量 → 规范化 → 守卫 → 预算分段全链成立，「只测首包」路径已不可达。
  - 四条重包 lane（单包 PKGS）不涉改动，原验证继续有效。
- 残余 NOT_RUN（既有 Next① 不变）：修复后首轮 main run 五 lane 实跑仍待 conductor 观察——light lane 日志 `echo "lane light: …"` 应列 36 包、五 check 名（test (httpapi)/…/test (light)）落位。
- non-blocking 4 条维持登记（R6 池素材），不动。

终局结论（Reviewer A / correctness）：**APPROVE**——blocking 1 已闭合且经独立复核取证；其余载荷面（T-551/T-553/T-556 产品码与测试、T-551-contract 台账/契约/金样、difftest 新 case）在首轮评审中已全部核过无 correctness 缺陷。
