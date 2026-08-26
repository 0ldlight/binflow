# T-290 评审报告 — smart remote 生效字段子集

- **评审视角**: correctness（正确性优先，兼顾 consistency）
- **结论**: **APPROVE**
- **评审人**: code-reviewer
- **日期**: 2026-08-26
- **范围**: 仅 §3 清单内 16 个文件；工作树中 T-289 在途文件（uploads*/router/server/harness_test/internal/storage/cmd s3 栈）与 web/src untracked 文件未评审。

## 1. 取证命令（全部实际执行）

```
go vet ./internal/repo/ ./internal/remote/ ./internal/metadata/ ./internal/httpapi/   # exit 0
gofmt -l internal/{repo,remote,metadata,httpapi}                                      # 0 文件
go test ./internal/repo/ -run 'TestT290|TestM02' -count=1                             # ok 1.7s
go test ./internal/metadata/ -run 'TestT290|TestT212|TestUsageBackfill|TestDockerUpgrade' -count=1  # ok
go test ./internal/httpapi/ -run 'TestT290|TestM02' -count=1                          # ok 1.2s
go test ./internal/remote/ -run 'TestT290|TestFetchMetadata|TestFetchSocket|TestFetchTimeout' -count=1 -v  # 全 PASS
go test ./internal/remote/ -run 'TestT290' -race -count=1                             # ok 5.2s
go test ./internal/repo/ ./internal/remote/ -count=1                                  # ok 23.0s / 22.6s
```

另核对：`git diff --stat` 确认改动文件集合与工作日志 §3 完全一致；`remote_configs` 的 SQL 读写全部封装在 `metadata/substores_remote_virtual.go`（无其他直读方，扩列无遗漏消费者）；`parseRemoteConfig` 调用点 service.go:1873（create）/2153（update）两条臂都过 M11 拒绝点。

## 2. 逐项发现

### blocker

无。

### major

无。

### minor

1. **minor — `internal/repo/config.go:195-215`（别名一致性：0 在别名比对中被当作实值）**
   `socketTimeoutMs:0, socketTimeoutMillis:800` 会以 "disagree (0 vs 800)" 拒绝，而应用循环里 0 的语义是"未设置，保默认"（`config.go:230-232`）。missed 别名对同理。现状是确定性的、偏严格（能暴露迁移脚本的字段拼写混乱），可以接受；但两处语义不一致且无测试钉住。建议：要么别名比对时空指针/0 视为缺席再比对，要么补一行表驱动用例把"0 vs 值 → 400"钉成显式契约。

2. **minor — `internal/repo/config.go:245,256` + `internal/remote/fetcher.go:981`（新字段无上界，极端值溢出）**
   `socketTimeoutSecs` 接近 `MaxInt64` 时 `f.value * 1000` 溢出为负，`(ms+999)/1000` 派生的 secs 回显也随之变负；`time.Duration(ms)*time.Millisecond` 在 ms > ~9.2e12 时同样溢出。M3 就没有上界校验（同族问题，非回归），且负值已被 400 拦住，仅荒谬输入可触发。建议后续补一个上界（如 ≤86400s）收口，M11 退役 legacy secs 字段时一并处理。

3. **minor — `internal/repo/config.go:117-121`（rejectM11RemoteFields 对尾随垃圾 JSON 失效）**
   `json.Unmarshal` 遇尾随内容报错 → 函数 return nil 放行，而下方 `dec.Decode` 只读首个 JSON 值也放行 → `{"url":...,"contentSynchronisation":true}垃圾` 可绕过按名拒绝。荒谬输入边缘，拒绝点主路径（纯净 JSON）不受影响。可改用同一 Decoder 双读或对 Unmarshal 错误不吞，优先级低。

### note

4. **note — PRD AC4 字面 vs as-built 回显拼写**：AC4 列的第三字段是 PRD 拼写 `missRetrievalCachePeriodSecs`，"回显一致"在 as-built 中落在 canonical 拼写 `missedRetrievalCachePeriodSecs` 上（别名只进不出，`config.go` remoteConfig tag）。裁定合理（canonical 稳定性 + 双拼写冲突 400），已在日志登记 T-293，但 **qa 断言必须用 canonical 拼写**，避免误判回归。
5. **note — M11 拒绝只覆盖 remote 臂**：virtual/local PUT 携带 `contentSynchronisation` 仍按 scenario-D 静默丢弃。字段本身是 remote 语义，可接受；无需动作。
6. **note — 手改 DB 行不经过拒绝点**：直接写库塞入 M11 名字的 config，GET 照常回显，直到下一次 PUT/POST 触发 re-parse 才 400。与 `maskRemoteConfig` 的 best-effort 姿态一致，可接受。

## 3. 四处自有裁定复核（全部维持）

1. **P1 三字段按 PRD FR-90 终版** — 站得住。PRD 90.2（milestone-10.md:258 段）明确三字段为 `socketTimeoutMs`/`metadataRetrievalTimeoutSecs`/`missRetrievalCachePeriodSecs`，Q7（:481）终裁 `enableTokenAuthentication`/`contentSynchronisation` 归 M11 且"不做 inert 字段"。派单文本与 PRD 冲突时按 PRD 落 + 按名 400，是对"诚实不做 inert 面"（90.1 filestore 501 同款）的正确执行。代码、单测、wire 测试三层都有钉。
2. **missed 双拼写 canonical 留 Artifactory 拼写** — 站得住。canonical `missedRetrievalCachePeriodSecs` 是 M3 既有字段（remote_virtual_test.go 既有断言），改成 PRD 拼写反而破 M3 回显兼容；PRD 拼写作输入别名 + 分歧 400 沿 virtualConfigInput 先例（config.go:322-361）。有一处不对称值得一票登记（已归 T-293）：miss 族 canonical 取 Artifactory 拼写，而 socket 超时新字段 canonical 取 PRD 拼写 `socketTimeoutMs`（xsd `socketTimeoutMillis` 为别名）——由 M3 历史解释得通（前者已有 canonical、后者是全新字段），非缺陷。
3. **按名 400 而非全局未知字段 400** — 站得住，且是唯一不破回归红线的落法。`TestM02bRemoteConfigValidation`（remote_virtual_test.go:203 "unknown Artifactory fields tolerated (scenario D)"）钉死 M3 容忍契约且本票零改动仍绿；逐字实现全局 400 必破该钉与 AC5。按名 400 + 错误体点名 M11 指引达成了 AC4 字面（`contentSynchronisation` → 400），且 wire 层 RawMessage 透传保证了拒绝点单一（repo.Service），httpapi 不自作主张。PRD↔as-built 分歧登记 T-293 的处置正确。
4. **014 列消费优先级（行列 > canonical JSON > legacy secs > 默认）** — 站得住。单一解析点 `effectiveSocketTimeoutMs`/`effectiveMetadataWait`（fetcher.go:152-177），优先级矩阵有表驱动测试（含负行列=unset 臂）；`clientFor` 池签名与 Client 构建同用该解析点，配置变更触发重建有测试（`TestT290ClientSignatureTracksMs`）。pre-014 行为不变的守卫见 §4。

## 4. 回归风险评估：低

- **defaultPolicy 种子遮蔽修复**：`loadRepo`（fetcher.go:934）以 defaultPolicy 为种子再 unmarshal 行 JSON，T-290 三字段在种子里恒零、默认延迟到消费点解析——`TestT290DefaultPolicyDoesNotShadowLegacyRows` 双断言（种子为零 + legacy 30s 行解析 30000ms）钉死。这是本票最容易埋雷的点，自擒并加守卫，处理正确。
- **既有 M3 断言零改动**：`TestM02RemoteExplicitValues`（secs=30 输入 → 回显 30，因 ceil(30000/1000)=30 精确往返）与 `TestM02bRemoteConfigValidation` 均未改且全绿；改动文件清单确认无其他既有测试被触碰（metadata 三个改动均为回卷基建补列）。
- **scenario-D 维持**：单测 + wire 两层各有容忍臂（t290_smart_remote_test.go:216-223、t290_smart_remote_wire_test.go:109-116）。
- **迁移幂等/双方言**：014 两方言文件语句逐字相同（INTEGER NOT NULL DEFAULT 0 ×3，方言公共子集），幂等性依赖版本台账（同 003 ALTER 族先例）；三处回卷测试基建（internal_test/t212/usage）都补了对应 DROP COLUMN，重放路径已验证（TestT212*/TestUsageBackfillMigration/TestDockerUpgradeFromM1Database 全绿）。postgres README 补 013/014 版本史，lockstep 义务履行。
- **并发**：无新增 goroutine；`clientFor` 池换签名重建路径 -race 抽查绿；per-repo singleflight 等待上限只在等待者臂上取值，无共享可变状态。
- **分层**：wire（httpapi RawMessage 透传）→ 域（repo 解析/校验/拒绝）→ 存储（metadata 扩列 CRUD）→ 消费（remote 解析点），依赖方向无违反；无跨包摸内部结构；错误全部 wrap 且点名上下文；context 显式传递。
- **clean-room 抽查**：字段名/默认值属公开 REST API 表面（repo-semantics §7.1 行为规格，高置信），实现为 BinFlow 自有结构（别名归并、effective* 解析点、镜像列契约），无逐行对应嫌疑。

## 5. 范围外移交（conductor）

1. PRD DoD 余量条款要求 P2 清理 cron 收窄在 **BOARD 留痕**（日志 §1 已声明归 conductor）——请确认留痕后再走 qa。
2. T-296 文档票需覆盖 `docs/user/api-reference.md` remote 字段表（四字段 + 双拼写 + M11 拒绝语义），qa 前用户文档缺口仍在。
3. T-293 终裁两项：PRD"未知字段 400"与 scenario-D 的分歧处置追认；`socketTimeoutMs`（PRD）vs `socketTimeoutMillis`（xsd）canonical 拼写追认。

## 6. 结论

**APPROVE**。四处自有裁定全部站得住；无 blocker/major；3 条 minor（别名-0 语义、上界溢出、尾随垃圾绕过拒绝点）均可后置到 M11 或随 T-293 收口，不构成本票返工条件。建议按日志 §7 清单提交（严格点名文件，剔除 T-289 在途文件）。
