Ticket:        L008-1b — update-merge 实现票（ADR-0050 派发级；P0）
Role:          code-reviewer (reviewer-b — architecture/compat 面)
Area:          internal/repo + internal/httpapi（repository 域 → 双审强制域，A/B 双实例齐）
Input:         conductor 派发（同 A 范围）；ADR-0050（DECISIONS.md L1375-1413）；任务书 docs/design/repo-update-merge.md §9；reports/compatibility/L008-update-merge-diff.md；reports/agents/T-L008-1b.md；通读全部 diff + 上下游（metadata remoteStore、envelope、keypair、fetch 侧回落链）
Changes:       config.go / service.go / repositories.go 全量 diff + 13 个翻转/新增测试文件逐个过；validateRemoteConfigShape→parseRemoteConfig 调用面全查（grep 全仓非测试调用点仅 service.go:2196/2589 两处，签名迁移完整）；keypair.go 与 web/src 零改动经 git status 核实
Files:         internal/repo/config.go（merge 骨架忠实 ADR：基线起步/指针判缺/raw-map 键在场/对象整体替换——2 条 blocking 见结论区）；internal/repo/service.go（baseline 注入最小侵入 ✓；credential 保留臂 1 条 blocking）；internal/httpapi/repositories.go（PUT 更新臂移除+A16 次序+A10 逐字 ✓；raw-JSON credential 传输理由成立——typed string/pointer 确实压扁 null）；remote_update_merge_test.go / repo_update_merge_wire_test.go（矩阵覆盖合格：非默认值 seed、row+canonical 双断言）；t80/t317/t217/t327r/browse-flag/metadata-ttl/roundtrip/compat（翻转全部为语义迁移非弱化，抽 3 例核：t80 PUT-on-existing 断言 400 逐字+零副作用+type-先序——强于旧断言；M02ZeroPeriodsStoreZero 断 row 层 0——强于旧；t217 PUT 臂 200→400 同步 ADR-0026 注记）
Tests:         go build + go vet 两包 ok；go test ./internal/repo/ -count=1 -run 'TestRemoteUpdateMergeMatrix|TestRemoteUpdateCredentialKeepSealedRow|TestT317SmartRemotePairMergeOnOmit|TestM02ZeroPeriodsStoreZero|TestM02RemoteExplicitValues|TestT290SocketTimeoutSpellings|TestT290SmartRemoteValidation' → ok 2.416s；go test ./internal/httpapi/ -count=1 -run 'TestRepoUpdateMerge|TestRemoteVirtualUpdateREST|TestRepositoriesCRUD|TestT217ManageOnlyHolderOrthogonality|TestRemoteMetadataTTLWire' → ok 2.564s
Commands:      上述 go build/vet/test 原文；git diff 各文件；grep parseRemoteConfig/validateRemoteConfigShape 调用面
Outputs:       本报告 reports/agents/T-L008-1b-review-b.md
Compatibility: 13 硬断言臂与任务书 §9.3 逐臂对应核实（A1-A13 齐 + A14/A15 由探针升格且实现前补证、次序正确）；五项归一化（§2）裁量合理——customHttpHeaders 无席位的 A4/A6 判 SAME 属容忍面升格路径留痕、password 回显排除为 ADR 决策 4 明示、None≡"" 为既有 echo 形；**缺口：socketTimeoutMillis/socketTimeoutMs 显式 0 单元格无任何差分臂**（A8 只测 retrieval/maxUniqueSnapshots/hardFail；§2.5 明说参照不识 socketTimeoutSecs——即 BinFlow 唯一存 0 的拼写恰是参照不认识的，参照认识的两条 ms 拼写未测）→ blocking #2；T-290 族联裁留痕齐（known-divergence authority 链 adr 化 ✓、matrix D02-R03/R04 补注 ✓、resolved note 与差分报告一致 ✓）
Security:      无新增攻击面；raw-JSON credential 仅透传不落 canonical（NFR-S14 维持）；PUT-on-existing 早退减写面；**但 blocking #1 是凭证完整性问题**（瞬态读错误下 sealed password 被静默清空）
Performance:   UpdateRepo password-omitted 路径 +1 次主键点查 O(1) 可接受；handleRepoPost 双 Unmarshal ≤1MB 维持
Risks:         ① ADR-0050 决策 1 矩阵对象列「显式值=子键级写入」已被 A14 实测推翻（整体替换、未提复位 false）——ADR 自身软缝协议预登记的 Errata 触发条件③已触发，**无人排队回填**（erratum 归 conductor/architect，见 Next）；② bool/period 显式 null、repoLayoutRef 显式 ""（config.go:583 `!= ""` guard 使 ""=保留而非矩阵的清空）三格对参照未取证——规格票素材
Blockers:     无（取证环境完好）
Next:          ① conductor 派 ADR-0050 Errata（对象列显式值行改「整体替换（A14）」，触发条件③留痕）；② socket-0 探针臂补取证或实现补齐后 A 复核消 blocking #2；③ 规格票冻结素材增补：socketTimeoutMillis 显式 0、repoLayoutRef ""、bool/period null 形态；④ breaking note 双变更置下一 release note 首条（义务已在 T-L008-1b Next ⑤ 登记，conductor 执行）

## 评审报告 L008-1b（形态: reviewer-b）
结论: REQUEST_CHANGES

### 必须修改（blocking）
1. **internal/repo/service.go:2657-2661** — credential 保留臂吞掉 GetConfig 的瞬态错误：`if cur, gerr := s.md.Remote().GetConfig(...); gerr == nil { row.Password = cur.Password }` 把一切非 nil 错误（含 wrapExec 的 DB 瞬态失败）当作「create-crash 无行可保」，随后 UpdateConfig 用空 Password 覆写行——**瞬态读失败 = sealed password 静默丢失**，违反 ADR-0050 决策 4「省略=保留（密文行不动）」的失败面，也违反仓规「错误一律 wrap 带上下文」。同函数十行之下 UpdateConfig 自己就示范了正确判法（errors.Is(ErrRemoteConfigNotFound) 分流）。最小改法：
   ```go
   default:
       cur, gerr := s.md.Remote().GetConfig(ctx, r.RepoKey)
       if gerr == nil {
           row.Password = cur.Password
       } else if !errors.Is(gerr, metadata.ErrRemoteConfigNotFound) {
           return nil, fmt.Errorf("remote config %q: %w", r.RepoKey, gerr)
       }
       // not-found: the healer below creates it with an empty password.
   ```
2. **internal/repo/config.go:485-490（连带 469 与 603-615 的参数序）** — socketTimeout 别名对的显式 0 残留 0-as-absent，ADR-0050 决策 3 落地不完整且两别名对互相矛盾：`socketTimeoutMillis:0`（canonical）与 `socketTimeoutMs:0`（alias）经 resolveRemoteAlias + 485-490 的 resolved-zero→nil 均落「缺省保留」；而同为 canonical 拼写的 `missedRetrievalCachePeriodSecs:0` 落「存 0」（469 行参数序里 canonical 在 a 位、b==nil 时返回 a=ptr0；socket 对 canonical 在 b 位、bv==0 时返回 a=nil）。矛盾实况：**同矩阵同格（canonical 标量显式 0），missed 存 0、socket 保默认**。且参照认识的拼写恰是 socketTimeoutMillis（差分 §2.5：参照不识 socketTimeoutSecs），任务书验收 §9.2-4「显式 0 存 0（…socketTimeout 族；create+update 双面）」实际只由参照不认识的 socketTimeoutSecs 拼写满足；差分 13 臂无一覆盖 ms 拼写显式 0。改法二选一：(a) :8082 补探针（POST `{"socketTimeoutMillis":0}` 单臂 + `{"socketTimeoutMs":0,"socketTimeoutSecs":30}` 优先序臂）——若参照存 0 则移除 485-490 的 nil-ing 并让 resolved-0 直接入 loop（ms-wins 优先序随之对 0 生效），若参照保默认则在本票报告登记该格为已证偏离并撤回本条；(b) 无法补证时按 ADR 决策 3 字面统一为「显式 0 存 0」。当前状态两头都不是：无证据、内部不一致、验收项半满足。

### 建议改进（non-blocking）
1. **DECISIONS.md ADR-0050 决策 1 矩阵**——对象列「显式值=子键级写入」已被 A14 实测推翻（整体替换、未提子键复位 false）；其软缝协议自登记的 Errata 触发条件③已成立，应回填留痕（代码按证据落是对的，册面不跟上会漂移）。归 conductor 派 architect。
2. **internal/repo/config.go:583**——repoLayoutRef 显式 "" 走 `*in.RepoLayoutRef != ""` guard = 保留基线，与矩阵「标量 ""=清空」相左；该格对参照未取证，进规格票探针清单。
3. **internal/repo/t290_smart_remote_test.go:190/195**——行名「zero keeps defaults」「ms double zero keeps default」在 ADR-0050 后失实（同行 metadataRetrievalTimeoutSecs:0 现已存 0；断言仅不拒故仍绿）——改名防误读。
4. **差分报告 §1 A4/A6**——判 SAME 的主字段（customHttpHeaders）在 BinFlow 无席位，SAME 实为「其余席位 keep + 席位缺席容忍」；已留升格路径，台账翻绿时保持该限定语可见（resolved note 已载，维持即可）。
5. **A17 authenticated 非 admin 门序**——参照侧未取证（密码策略拒建用户），缺口已留痕；不阻本票。
6. **breaking note**——PUT-on-existing 200→400 与全量替换→merge 双变更的 release note 首条义务已在 T-L008-1b Next 登记；升级窗口面（存量 canonical 即基线、无数据迁移）在 ADR 后果与设计稿 §6 齐备，conductor 于下次发布执行即可。

### 核验通过项（审查面 1-5 逐条）
- ADR 忠实性：三列矩阵/PUT=create-only/console 零破坏（web/src 无 diff ✓）/credential 单侧清/raw-map 键在场（validateRemoteConfigShape 返 raw map 的调用面完整：唯一调用方即 parseRemoteConfig，签名迁移闭合）——除上述 0 语义一格外部齐；fetch 侧 0=unset 回落链未动（分层正确，ADR-0012 勘误三边界①维持）。
- 架构：baseline 传递链最小侵入（service.go UpdateRepo 一处 unmarshal + 第三参；CreateRepo nil）；指针席位零新增达成（username/password 走 raw-map 判在场，无新结构席位）。
- 13 臂差分与 ADR 逐臂对应 ✓；归一化五项裁量合理 ✓。
- 测试翻面抽 3 例（t80/M02Zero/t317）均为语义迁移且断言强于旧版，无弱化。
- keypair.go 零改动声称成立（local-only 面 + local 臂仍 caller-owned replace，ADR 决策 5 范围外）；T-290 族翻转与决策 3 联裁留痕齐（known-divergence/matrix/报告三处一致）。
