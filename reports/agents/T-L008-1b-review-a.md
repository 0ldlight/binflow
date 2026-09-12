# 评审报告 T-L008-1b（形态: reviewer-a）

Ticket:        L008-1b — update-merge 实现票（ADR-0050 派发级）P0
Role:          code-reviewer (reviewer-a, correctness)
Area:          internal/repo（repository 域）+ internal/httpapi（transport 面）
Input:         conductor 派发（审工作区未提交改动：config.go / service.go / repositories.go + 2 新测试 + 13 翻面测试）；通读 ADR-0050（DECISIONS.md L1376-1452）、docs/design/repo-update-merge.md §9 任务书、reports/compatibility/L008-update-merge-diff.md、reports/agents/T-L008-1b.md、tools/difftest/l0081b/update-merge-arms.sh
Changes:       三产品文件全 diff 逐行 + 上下游追读：UpdateRepo/CreateRepo 全臂（service.go L2137-2690）、parseRemoteConfig/parseContentSynchronisation/resolveRemoteAlias 全函数（config.go）、handleRepoPut/handleRepoPost/configJSON/repoConfig（repositories.go 全文件）、keypair.go updateRepoKeypairRef（零改动确认属实：传 *current 全量）、repositories_probe.go（自有 body 结构，不受 RawMessage 化影响）、web/src/lib/repos.ts + RepositoryFormPage.tsx（PUT/POST 分工属实；表单空密码 omit 不携带——console 臂不清密文）、fetch 侧 ttldefaults.go/fetcher.go（0=unset 回落链未动属实）
Files:         internal/repo/config.go — 基线模式实现正确（省略=逐席保留、null/"" 清标量、{} 整族复位、显式对象整体替换〔A14 补证推翻任务书「未提=基线」草案，实现按补证落，正确〕、url 更新面可省/显式空白仍拒、别名分歧拒维持、period 族 0-as-absent 双面移除）；config.go:265 validateRemoteConfigShape 返回 raw map 复用（零新增席位，路线合规）；internal/repo/service.go — baseline 注入 + credential merge 落行，**1 处 blocking（见下）**；internal/httpapi/repositories.go — RawMessage 透传（null 存活链验证成立）、PUT 更新臂移除完备（400 逐字、无 rclass 落 create 路径先撞 type 拒——CreateRepo validateRepoTypeDyn 先于 key-exists 检查，A16 次序成立；文案异为既有族，报告如实登记）、description raw-presence 合并正确；13 翻面测试逐个过——均为语义翻面非遮蔽（t80 重写后 PUT-on-existing 400 逐字臂 + 拒后零副作用臂 + rclass-less type-先序臂 + POST 404 臂齐备；t217 翻面保留 family-7 写权证明〔POST 臂〕；t327r/t329de local 臂仅换动词、full-replace 断言原样保留=local 另票范围正确的体现）
Tests:         go build ./... ok；go vet ./internal/repo/... ./internal/httpapi/... 零告警；gofmt -l 两包空；go test ./internal/repo/ -count=1 -run 'TestRemoteUpdateMergeMatrix|TestRemoteUpdateCredentialKeepSealedRow|TestM02ZeroPeriodsStoreZero|TestT317SmartRemotePairMergeOnOmit|TestM02RemoteExplicitValues' ok 1.495s；go test ./internal/httpapi/ -count=1 -run 'TestRepoUpdateMerge|TestRemoteVirtualUpdateREST|TestRepositoriesCRUD|TestRemoteBrowseFlagUpdateFlipsAndKeepsREST|TestRemoteMetadataTTLWireBoundary' ok 2.050s（复跑与实现日志声称一致）
Commands:      go build ./... && go vet ./internal/repo/... ./internal/httpapi/...；gofmt -l internal/repo internal/httpapi；上述两条 go test；另：sed/grep 追读 service.go L2137-2690、repositories.go 全文件、keypair.go L395-405、metadata/substores_remote_virtual.go GetConfig、internal/remote/ttldefaults.go、web/src/lib/repos.ts L104-190、RepositoryFormPage.tsx L304-370、tools/difftest/l0081b/update-merge-arms.sh
Outputs:       reports/agents/T-L008-1b-review-a.md（本文件）
Compatibility: A 形态核对：A10 400 文案逐字（单元 t80 + 差分 A10 双证）；A14/A15 两格按 :8082 补证落实现（对象显式=整体替换未提子键复位 false、null=保留整族）——补证推翻任务书 9.1-④「先行按未提=基线」属证据优先，正确；归一化五项（cs 拼写/echo 形/customHttpHeaders 无席位/password 回显/socketTimeoutSecs 种子拼写）均为既有族，diff 报告如实登记，非本票引入
Security:      credential 链无降级：canonical 恒无 password（NFR-S14 构造性维持）；RawMessage 透传不引入新输入面（typed decode 在 service 层照旧拒错型，mistyped username/password 仍 400 带字段名）；PUT-on-existing 早退反减写面；**blocking 项本身即安全相关（密文静默丢失）**
Performance:   UpdateRepo 新增一次 Remote().GetConfig（仅 passwordSet=false，主键点查 O(1)，可接受）；handleRepoPost body 一次读全 + 两次 Unmarshal（≤1MB 上限不变）；无锁/分配热点影响
Risks:         ① socketTimeout ms 拼写族 0-as-absent 残留（non-blocking，见下）；② bool/period 显式 null=保留（typed 解码不可见）——实现报告 Risks ②③ 已登记，规格票素材，维持；③ 存量 config 不可解码行 fail-closed 需显式带 url——已登记，自愈路径可接受；④ 差分对拍未复跑（需 :8082/:8085 活体，超出只读取证面；脚本与报告实体在案且内部一致）
Blockers:      无取证障碍
Next:          1) **本票属 repository 域=双审强制域，本次仅派 reviewer-a 单实例——请补 reviewer-b（architecture 面）并行评审**；2) blocking 修复后复跑 ./internal/repo/ -run TestRemoteUpdateCredentialKeepSealedRow；3) 规格票素材新增两格：socketTimeoutMillis/socketTimeoutMs 显式 0 的参照行为（probe 臂）+ audit detail descriptionSet 语义注记；4) 台账 resolved/D02 翻绿建议归 compatibility-engineer（不随本评审）

## 评审报告 T-L008-1b（形态: reviewer-a）
结论: REQUEST_CHANGES

### 必须修改（blocking）
- internal/repo/service.go:2656-2663 密码保留臂把**非 NotFound 的 GetConfig 错误静默当作「无行可保」**——`if cur, gerr := s.md.Remote().GetConfig(...); gerr == nil { row.Password = cur.Password }`：gerr 为 store busy / 瞬时 SQL / scan 错误（wrapExec 面，见 metadata/substores_remote_virtual.go:57-79）时 row.Password 留 ""，随后 UpdateConfig 用空密码**覆盖存量 sealed 行——一次省略 password 的无关配置更新在瞬时故障下静默清掉上游凭据**，直接违反任务书 9.1-②「省略 username/password=保留行内存量（含 sealed password 原样）」与 ADR-0050 决策 4。注释只论证了 ErrRemoteConfigNotFound（create-crash 愈合窗）一种情形。建议最小修法：
  ```go
  cur, gerr := s.md.Remote().GetConfig(ctx, r.RepoKey)
  switch {
  case gerr == nil:
      row.Password = cur.Password
  case errors.Is(gerr, metadata.ErrRemoteConfigNotFound):
      // the healer below creates it with an empty password
  default:
      return nil, fmt.Errorf("remote config %q: %w", r.RepoKey, gerr)
  }
  ```
  （repositories 行此刻已提交、remote_configs 行保持旧值，重试可愈——严格优于静默清密文。）

### 建议改进（non-blocking）
- internal/repo/config.go:479-490+603-615 socketTimeout **ms 拼写族**显式 0 在两面仍是 0-as-absent（resolveRemoteAlias 单侧 0 返回 absent + `socketMs==0 → nil` 坍缩）：`{"socketTimeoutMillis":0}` / `{"socketTimeoutMs":0}` 更新=保留基线、建仓=回落 15000；而同族 `socketTimeoutSecs:0` 与 `missedRetrievalCachePeriodSecs:0` 均存 0——任务书 9.2-④「socketTimeout 族（create+update 双面）」仅 secs 拼写满足，ms 格无单测钉死、差分 A8 只测了 retrievalCachePeriodSecs:0。可辩护为「别名解析规则不变」的既定坍缩（注释在案），但 canonical 拼写跨两族不对称（missed 的 canonical 0 存 0、socket 的 canonical 0 保留）应登记：a) 实现报告 Risks 补一行；b) 规格票加 probe 臂（参照 :8082 POST `socketTimeoutMillis:0` 存 0 还是保留）；fetch 侧生效面两侧等价（0=unset 链未动），风险仅在 wire 回显格。
- internal/httpapi/repositories.go:721-731 handleRepoPost 由 Decoder.Decode 改 json.Unmarshal 全量反序列化——POST 尾随垃圾现在 400（旧版忽略）。更严、与 config blob 的 validateRemoteConfigShape 姿态一致，大概率同参照；行为变化留痕即可。
- internal/repo/service.go:2688-2692 audit detail `descriptionSet` 现反映**合并后**的 description（省略键时 handler 传 current.Description）——省略 description 的更新在有描述的仓上记 descriptionSet:true，审计语义微漂移；如审计契约在意，可由 handler 显式传 presence 布尔。
- （维持实现报告既有登记，无需动作）bool/period 显式 null=保留、A17 门序缺口、对象族证据单实例——Risks ②③④ 在案。
