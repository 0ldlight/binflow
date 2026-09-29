# T-551 stage 2 — maven 派生 sidecar validator 族 B 面修复

> stage 2 落码面（internal/adapter/maven/ only）。契约修订为并行票（stage 1，
> compatibility-engineer）：docs/compatibility/contracts/maven-virtual.yaml 的
> working-tree 改动（maven/derived-sidecar-validator-family 条目）非本票产物，
> 本票未触碰 docs/。两 stage 同一证据源（L032 Arm 6 live A 面 17 维取证矩阵）。

```
Ticket:       T-551 maven 派生 sidecar validator 面修复（B 面）第二段 / Linear BIN-33 / P0（L032 Arm 6 判定 BUG）
Role:         dev-registry-adapter（领域实例：dev-package-maven）
Area:         internal/adapter/maven/
Input:        reports/compatibility/L032-t550-followup-arms.md Arm 6（17 维矩阵，r1≡r2≡r3，A=7.161.26 Enterprise+）：
              三处分歧=① .sha1(java-agent) Last-Modified A present/B absent；② IMS→A 304/B 200；③ ETag/INM→A no-etag/B ETag=digest+INM→304。
              已对齐红线=body 面 ETag+LM+双条件 304；sidecar 内容=digest(所服务剥离/合并体)（java-agent 与 capable 腿）；.sha512 404。
              落码点=writeDerivedSidecar（virtual_metadata.go:561；member 面 handler.go:159 与 virtual 面 virtual_metadata.go:116 同谓词同一 helper，R3 3b58b082 后两面齐翻）。
Changes:      ① writeDerivedSidecar validator 族整体翻面：加自身 Last-Modified（派生时刻 time.Now()，UTC，http.TimeFormat 秒级——与 body 面同精度）；
              ② 去掉 ETag 头，evalConditional 改传空 etag——INM（含 digest 本身、引号形、*）无 ETag 可匹配恒不 304（etagMatch 的 etag=="" 先行守卫使其自然失效，不误答）；
              ③ IMS 谓词接通：值≥LM→304、旧值→200（evalConditional 既有 13.2.2 实现，改动仅是传入真实 lastMod 而非零值）；
              ④ Content-Length 移到条件判定之后（304 不带 CL，对齐 writeDerivedMetadata 既有习惯）；
              ⑤ sha1/md5/sha256 三类 sidecar 经同一 helper 一处翻面（两调用链全覆盖）；.sha512 404 分支未动；digestsOfBody 内容路径零改动。
Files:        新增 internal/adapter/maven/derived_sidecar_validator_test.go（行为命名：被测=派生 sidecar validator 族）；
              修改 internal/adapter/maven/virtual_metadata.go（writeDerivedSidecar + 函数注释）；
              修改 internal/adapter/maven/virtual_metadata_test.go（TestVirtualMetadataSidecarAndConditional 陈旧断言翻新：原断言 sidecar 带 ETag=digest——恰是分歧③的旧姿势；body 面 INM 腿的 etag 改从 body 响应取）。
              未动：docs/compatibility/contracts/maven-virtual.yaml（并行 stage 1 产物，working tree 中存在，非本票改动）。
Tests:        TestDerivedSidecarValidatorFamily（新，table-driven 三腿：virtual×java-agent 剥离合并 / virtual×Maven3 整合并 / member×java-agent 剥离文档）×
              每腿维度：body 面 ETag+LM 在、INM→304、IMS 新→304、旧→200（红线）；三 algo sidecar：内容=digest(所服务体)（红线）、LM present+可解析、ETag absent、
              IMS 新→304、旧→200、INM{digest,quoted,*}→200（分歧②③）；.sha512→404（红线）。
              咬合验证（负证）：git checkout 回退 virtual_metadata.go 至修复前后新测试即 FAIL（三腿全部 "Last-Modified = \"\" ... want present and parseable"），git apply 恢复后转绿——测试确实咬住修复。
              回归：TestSnapshotVersionsUAStripping（内容契约 java-agent+capable 腿，未改未断）、TestVirtualMetadataSidecarAndConditional（翻新后过）。
Commands:     go build ./... && go vet ./internal/adapter/maven/ && gofmt -l internal/adapter/maven/ ; echo gofmt-exit:$?
              go test ./internal/adapter/maven/ -count=1
              GOLANGCI_LINT_CACHE=/tmp/lint-t551 golangci-lint run internal/adapter/maven/...
              负证腿：git diff internal/adapter/maven/virtual_metadata.go > /tmp/t551-prod.patch && git checkout -- internal/adapter/maven/virtual_metadata.go && go test ./internal/adapter/maven/ -count=1 -run 'TestDerivedSidecarValidatorFamily|TestVirtualMetadataSidecarAndConditional' ; git apply /tmp/t551-prod.patch
Outputs:      修复态：ok github.com/lzwzzy/binflow/internal/adapter/maven 17.867s（全量 -count=1）；定向三测 --- PASS ×3（0.37s/0.30s/0.24s）；
              gofmt-exit:0（无输出=无未格式化文件）；golangci-lint "0 issues."（隔离缓存 /tmp/lint-t551）。
              回退态（负证）：--- FAIL: TestDerivedSidecarValidatorFamily 三 subtest 全挂（Last-Modified="" 解析失败）+ FAIL 包级；RESTORED 后复绿。
Compatibility: 对照 L032 Arm 6 三处分歧逐项闭合：①LM present（now/秒级，注释标明 A 面为物化后稳定戳、B 为 per-request 派生时刻）；②IMS≥LM→304、旧→200；③ETag 移除、INM 恒 200。
              红线全维持：body 面双条件 304、内容契约三腿 digest=所服务体摘要、.sha512 404、member/virtual 两面同 helper 同翻（R3 配对教训）。
              可差分 surface（供 differential-qa-engineer 复跑 L032 maven-member-snapshot-sidecar-304）：GET /{local|virtual}/{org}/{mod}/{ver}-SNAPSHOT/maven-metadata.xml{,.sha1,.md5,..sha256,.sha512}
              × UA{Java/1.8.0_391, Apache-Maven/3.9.16} × 条件{none, IMS=LM±90s, INM=digest/*}——期望=17 维矩阵 A 列。
              与并行契约（maven-virtual.yaml 新立 maven/derived-sidecar-validator-family 条目）实形一致，无出入。
Security:     无新面：路径仍经 Parse 六字段校验（防 ../）；无 remote 代理改动；无认证改动（sidecar 面沿用既有读授权链）。
Performance:  无影响：每响应多一次 time.Now() 与一个头设置；digest 计算路径未动（大制品不经过此 helper——仅小 metadata 派生面）。
Risks:        LM 稳定性语义差：A 面旁车 LM 为物化节点的稳定戳；B 面派生时刻=now，跨秒两次请求 LM 会前移（客户端以旧 LM 回访 IMS 若跨秒得 200+新 LM，自洽但不字节稳定）——按票面「派生时刻，秒级精度」口径实现，已在注释与本日志声明；若差分复跑发现 A 面戳稳定语义被探测腿咬出，需上 stored-stamp 方案（升级点：给派生体记持久化派生时刻）。
              条件判定与 X-Checksum-* 头族在 sidecar 面的存在性无 A 面取证维度（矩阵未测），维持原状未猜。
Blockers:     无。
Next:         差分复跑 L032 maven-member-snapshot-sidecar-304（17 维矩阵 B 列应 17/17 对齐）后翻 DIVERGENT→FIXED；金样候选（Arm 6 提金①sidecar 17 维矩阵 A 腿）随契约票评审。
```

## 断点快照（如被中断）

- 已完成：writeDerivedSidecar 翻面（virtual_metadata.go:567 起；T-556 增补后行号）、新测试文件、陈旧断言翻新、四门+lint 全绿、负证咬合验证。
- 未完成：无（本票范围内的编码与自测全部完成；未 commit——按指令改动留工作树）。
- 断点位置：无悬空状态。

---

# T-556 微票 — maven 模块级清单 handleReleases 跳过翻回（walk 镜像同翻）

> stage 2 落码面续票（conductor 追加，写入域扩权 internal/adapter/maven/ + internal/repo/virtual.go walk 镜像点及其测试腿）。
> ledger review_gate 预注册条件成立（L033 Arm C 定谳：A live 并入 / B 丢弃）。

```
Ticket:       T-556 / Linear BIN-38（L033 Arm C 差分定谳，review_gate 预注册条件成立）/ P0
Role:         dev-registry-adapter（领域实例：dev-package-maven）
Area:         internal/adapter/maven/ + internal/repo/virtual.go（walk 镜像点，扩权）
Input:        reports/compatibility/L033-r5-fix-verification.md Arm C（r3≡r4 稳定，22 维）：
              定谳维=virt 模块级清单（hwm，hr=false 成员的 SNAPSHOT 版本）A=200 versions 含 1.0-SNAPSHOT（并入）/ B=404（filterMetadataSteps 模块级分支整个跳过成员）；
              混杂已排除（plainsnap 对照腿：B 从默认成员照样列入 SNAPSHOT）；版本级 snapshot 面（sv=1）双面一致不动；
              walk 层 release 族面 vacuous（A 对 hr=false local 的 release PUT 与 GET 均 409，「release 制品栖身 hr=false 成员」双面构造不出）。
              virtual_metadata.go:207-213 原注释「differential refutation flips BOTH sites together」兑现条款。
Changes:      ① filterMetadataSteps（virtual_metadata.go）模块级 handleReleases 跳过分支删除（产品码口径的一行翻回），§5.1 snapshot 级 handleSnapshots 跳过不动；
              ② internal/repo/virtual.go walk 镜像同翻：getVirtual 循环内 release-skip 两处条件（cache 分支 releasePath&&!handleReleases、plain 分支同款）删除，snapshot 族跳过保留；
              releasePath 局部变量与 isChecksumSidecarPath（§3.4 旁车豁免谓词，仅服务 release 族分裂，翻后死码且 lint unused 必炸）一并删除；
              handleReleases 字段两站点（virtualMember / metadataWalkStep）保留携带（与 row-read seam 对齐， seats 落地日可能复用），注释标明不再被咨询；
              ③ 相关注释全部翻新（getVirtual 文档段、struct 字段注释、memberHandlePolicy doc、文件头、filterMetadataSteps doc）——均注明 T-556/L033 Arm C 出处与 vacuous 构造不出注记。
Files:        修改 internal/adapter/maven/virtual_metadata.go（filterMetadataSteps + metadataWalkStep 注释）；
              修改 internal/adapter/maven/virtual_metadata_cache_skip_test.go（文件头、TestMetadataWalkLevelPolicySkip 表腿、TestVirtualMetadataModulePolicySkip→重写为 TestVirtualMetadataModuleLevelIgnoresHandleReleases 按证据拼写镜像 hwm 腿）；
              修改 internal/repo/virtual.go（walk 镜像翻 + isChecksumSidecarPath/releasePath 删除 + 注释）；
              修改 internal/repo/virtual_handle_policy_test.go（文件头；TestVirtualHandlePolicyMatrix 四象限 release 腿全改恒 resolve；TestVirtualReleaseSkipDropsCacheFacet→重写更名 TestVirtualReleaseCacheFacetServes（暖缓存经 virt 服务+零上游）；TestVirtualLocalHandlePolicyMatrix 同翻；TestVirtualLocalReleaseSkipSidecarExempt→重写更名 TestVirtualLocalHandleReleasesFalseServesAllPaths（豁免条款随 skip 一并 moot，腿改钉 release+sidecar 双 resolve+direct face 不受影响，加 vacuous 注记））。
              未动：版本级 snapshot 面（TestVirtualMetadataSnapshotPolicySkip 原形状原断言全维持）；树内其他并行票产物（T-553 deploy refusal、契约修订、CI 配置等）零触碰。
Tests:        adapter：TestMetadataWalkLevelPolicySkip（表腿 6：snapshot 级两腿不动、module 级 hr=false 翻为 kept、cache 两腿不动）+ TestVirtualMetadataModuleLevelIgnoresHandleReleases（real-stack：mv-a release 1.0.0 + mv-b SNAPSHOT，翻 hr=false 后两版本均在清单、latest/release=1.0.0——SNAPSHOT 限定符序低于 release，comparator 自身规则）。
              repo：四测试翻新（上述 Files④）——release 恒 resolve ×4 象限、暖缓存零上游服务、snapshot 族键 handleSnapshots 维持、local 面 release+sidecar 双 resolve。
              负证咬合：git diff 两产品文件→checkout 回退→六腿全 FAIL（恰在 hr=false 的 release/module 维：kept 0 steps want true / node not found want rel-body）→git apply 恢复复绿。
Commands:     go build ./... && go vet ./internal/adapter/maven/ ./internal/repo/ && gofmt -l internal/adapter/maven internal/repo; echo gofmt-exit:$?
              go test ./internal/adapter/maven/ ./internal/repo/ -count=1
              GOLANGCI_LINT_CACHE=/tmp/lint-t551 golangci-lint run internal/adapter/maven/... internal/repo/...
              负证腿：git diff internal/adapter/maven/virtual_metadata.go internal/repo/virtual.go > /tmp/t556-prod.patch && git checkout -- 两文件 && go test -run 六测试（两包）；git apply /tmp/t556-prod.patch
Outputs:      修复态：ok internal/adapter/maven 19.802s + ok internal/repo 79.608s（-count=1）；gofmt-exit:0；golangci-lint "0 issues."（隔离缓存）。
              回退态（负证）：TestMetadataWalkLevelPolicySkip[module hr=false] kept 0 steps want true；TestVirtualMetadataModuleLevelIgnoresHandleReleases FAIL；TestVirtualHandlePolicyMatrix ×2 subtest FAIL；TestVirtualReleaseCacheFacetServes "node not found, want rel-body served"；TestVirtualLocalHandlePolicyMatrix ×2 subtest "release Get … want rel-body"；RESTORED 后全绿。
Compatibility: 定谳维闭合：virt 模块级清单 hr=false 成员 SNAPSHOT 版本并入（=A live 实形）；§5.1 版本级 handleSnapshots 面 L033 双面一致维持（单测原断言全保）；walk 镜像两站点语义一致（同翻，vacuous 面零行为损失）。
              可差分 surface（供差分复跑 r5≡r6，case=maven-local-handle-walk-skip）：virt 模块级 metadata GET（hwm 维期望翻 404→200+versions 含 SNAPSHOT）、22 维其余 21 维维持（seat echo/409 拒族/sv=1/ctl 控制组/plainsnap 对照腿）。
              L033 Arm C 三新面孔提案（①409 措辞族 ②成员 GET class 门 ③plain-SNAPSHOT 拼写解析）非本票范围，未动。
Security:     无新面：walk 跳过收窄=更少路径被丢弃；无 upstream/认证/路径校验改动。
Performance:  无影响：少一次布尔判断；isChecksumSidecarPath 调用从 release 路径移除（原每 Get 一次 splitMemberChecksumSuffix）。
Risks:        handleReleases 字段两站点保留携带但零咨询——若未来 seats 落地票需恢复 release 语义，必须凭差分证据两站点同翻回（本票注释已埋条款）；
              walk release 族翻面在 live A 无可构造对照面（vacuous），B 侧行为=「无过滤」由单测钉住而非差分背书——差分复跑只能覆盖模块清单维，如实声明。
Blockers:     无。
Next:         conductor 另派差分复跑（maven-local-handle-walk-skip r5≡r6，期望 22 维中 11 分歧维翻绿 1+维持 10，hwm 维 200+并入）；ledger maven/virtual-metadata-modulereleases-skip 翻 DIVERGENT→FIXED 候选；Arm C 提案①②③（409 措辞族/成员 GET class 门/plain-SNAPSHOT 拼写）待 compatibility-engineer 立项。
```

## 断点快照 T-556（如被中断）

- 已完成：两站点产品码翻回 + 六测试腿重推导 + 四门/lint 全绿 + 负证咬合验证。
- 未完成：无（差分复跑归 conductor 另派；零 commit 按指令）。
- 断点位置：无悬空状态。
