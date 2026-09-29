# R6 PR #174 Review — Reviewer B（architecture / 契约对齐 / 测试覆盖）

Ticket:        PR #174（0ldlight/binflow，claude/r6-payload → develop，HEAD 434a1207，9 commits）——T-555 DELETE 级联 + T-559 maven 四面孔 + T-561/T-563 前缀族 + T-558/T-560 台账/契约/金样收官
Role:          code-reviewer (reviewer-b)
Area:          rest 仓库管理 DELETE 面（httpapi/repo）+ maven adapter（handle* 策略/旁车 LM/plain 拼写/自引用 URL 前缀）+ docs/compatibility 台账收官
Input:         conductor 派发（评审对象/五维清单）；通读 diff（52 文件 +3915/-344）+ 上下游追读：repo.Service.DeleteRepo 全调用点、maven handleGet/handlePut 全门序、httpapi v1/v2 两删除面、generic adapter 对照面、web 控制台消费点、internal/client 消费点
Changes:       代码 8 文件（repo/api.go+service.go+virtual.go、httpapi/repositories.go+repo_batch_write.go、maven/handler.go+put.go+virtual_metadata.go）；测试 11 文件（4 新建 maven 行为面 + 1 新建级联面 + 6 改写）；difftest case 7（2 新 5 改）；契约/台账/金样/matrix 5 文件；agent 日志 9
Files:         逐文件结论见下方结论区与 Non-blocking 清单；代码面全部 PASS，文档/台账面抽验一致
Tests:         `go build ./...` PASS；`go vet ./internal/adapter/maven/... ./internal/httpapi/... ./internal/repo/...` 零告警；`go test -count=1 ./internal/adapter/maven/...` ok 51.1s；`go test -count=1 ./internal/repo/...` ok 109.3s；`go test -count=1 ./internal/httpapi/ -run 'TestRepositoryDeleteCascade|Compat|Curl'` ok 8.9s；compatibility YAML×8 python yaml.safe_load 全部 OK
Commands:      git -C $WT fetch …develop；git -C $WT diff origin/develop..HEAD --（分批）；grep ErrRepoNotEmpty/deleteContent/productPrefix/Location 全仓；sed 读 config.go/repo.go/repos.ts/api.ts/rangecond.go；go build/vet/test 如上；python3 -c yaml.safe_load ×8
Outputs:       本报告 reports/agents/R6-pr174-review-b.md
Compatibility: 抽验 3 条契约与代码互证一致（见下）；台账 8 条 resolved 的 evidence 链（fix commit + L034 报告锚 + 金样）全闭环且锚文件确在本 PR 内；matrix D02-R05/D12-R18 行态与台账一致；195 冻结行集零增删核实
Security:      DELETE 面仍双门（路由 CapRepoWrite + service admin door，T-217 家族保留——service.go:2958 注释与代码核实）；403/404 回归臂在测试内；无新增攻击面
Performance:   本视角不适用（A 形态主管）；仅注：handleRepoDelete 增加一次 GetRepo+一次全节点 List 计数，与 v2 批量面同构，非热 path
Risks:         ①maven GET 门对「先落地后翻策略」臂（landed 制品 + config 翻 handleReleases=false）会 409——契约 unobserved 未列该臂，按 A 面机制（路径类门与落地无关）推定同构但未直测；②Location/uri 前缀面泛化到其他 adapter 未取证（见范围外）
Blockers:      无（测试与构建均可取证）
Next:          ①范围外发现四项交 conductor（见结论区后）；②建议 Reviewer A 收编 count-before-delete 与 GetRepo→DeleteRepo 间并发窗（本报告未按 B 维度深挖）；③双审形态已满足（A/B 并行各出报告）

## 评审报告 R6-PR#174（形态: reviewer-b）
结论: APPROVE

### 契约↔代码互证抽验（3 条）
1. **rest/repo-delete-nonempty-cascade（VERIFIED）**：契约 arms（200 + JSON 报告体四键 / deleteContent=true 同形冗余 / 计数=文件+folder 行根不计 / rclass 措辞二分）与 repositories.go:830-864 + repo_batch_write.go:586-609 逐点对应：handleRepoDelete 回 writeJSONBody(repoBatchDeleteReport)、countRepoArtifacts 改 len(nodes)（测试 single-root=1、nested=6 反证根行不入列）、repoDeleteStatusMsg 虚拟/其余二分与 v2 面共用。difftest case rest_nonempty_repo_delete.py 断言面（del_count/wording/keys_ok/repokey_ok/success ×4 臂 + member_survives + repo_get_after=400）与契约 arms 同构，且 generic 树构造明示规避 ⑨ maven 自动物化面污染——分面纪律在 case 层落实。
2. **maven-virtual ⑤ handle-policy-reject-409-wording-family（VERIFIED）**：契约 literal_pattern 与 handler.go:469-484 handlePolicyConflictMessage/handlePolicyConflictGETMessage 逐字一致（"rejected the resolution of an artifact '<K>:<path>' due to conflict in the snapshot release handling policy." + GET 追加 "; Path: '<K>:<path>'"）；put.go:93/97 两腿已收敛到同一模板。walk-skip case 升级为全量 message 逐字节对拍 + 634 字符长路径无截断维——断言面与契约 literal 同构。
3. **⑧ derived-sidecar-lm-materialized-stamp（VERIFIED/high）**：writeDerivedSidecar 签名加 lastMod（virtual_metadata.go:583-608），两调用点（成员剥离面=stored node stamp〔strippedSnapshotMetadata 返回 nodeTime(node)〕、virtual 合并面=newestDocTime(docs)）即契约「物化稳定戳=派生输入自身时戳」语义；zero stamp 略头且 evalConditional（rangecond.go:134-146）双 guard（INM 空 etag 不短路、IMS zero 不判鲜）——无假 304 回归口。

### 五维结论
1. **Area 纪律与分层：PASS**。adapter/maven 未摸 httpapi 内部（productPrefix 以包内 const 自持并注释 httpapi 侧同值事实——依赖方向 httpapi→adapter 正确，跨包共享反会破向）；repo 包 DeleteRepo 签名保留 deleteContent 形参（接口稳定，忽略语义入 doc comment）；ErrRepoNotEmpty 哨兵删除后全仓零残留引用（grep 核实），v1 writeRepoSvcError 与 v2 repoBatchDeleteErrOf 两映射面同步摘除，无孤儿语义缺口；repoDeleteStatusMsg/handlePolicyConflict*Message 均同包共享，放置合理。
2. **契约对齐一致性：PASS**。上列 3 条互证；台账 8 条 resolved（cascade-guard、d08 成功体半边、uri 前缀、Location 头、409 文案族、class 门、plain 存储面、LM 戳）evidence 链三件套齐（fix commit 6812f271/23b70a82/4481359f/531ea002 + L034-r6.md 锚 + 金样/契约翻态），锚文件均在本 diff 内；⑨ auto-materialize 如实立 DIVERGENT/UNKNOWN 不预写方向（不猜纪律）；matrix 注释内 44+43→45+42 两态为同日时序演进（T-563 mini-diff 后更新），非矛盾。
3. **测试覆盖充分性：PASS**。四面孔（409 文案族/class 门/sidecar LM/plain resolve）各有行为命名单测（handle_policy_refusal_test.go:40,80 / derived_sidecar_stamp_test.go:24,78 / plain_snapshot_resolve_test.go:29,80）+ 差分 case 双锚；级联面 repository_delete_cascade_test.go 219 行 table-driven 七臂含 404/403 回归；compat_test/curl_compat_test 旧 400 断言全部翻新（无残留假阳性）；plain 家园边界（unique home）单测显式钉 NOT_RUN 面。新测试文件均按行为命名、票号入头注释——合规。
4. **兼容语义风险（400→200 破坏性变化）：受控**。破坏性语义有 user_ruling（D-3，2026-09-29）+ 台账 BUG 裁定背书；deleteContent 两拼写均受理（web/CLI 既有调用不断裂）。残留 400 假设全部为**注释/文案/文档层**（见 non-blocking ①），无行为性断裂；web 强确认层（输入 key）仍在，误删防护未裸奔。
5. **金样资产结构：PASS**。四组新金样均循 derived-sidecar-validators 先例（metadata/request/response 三件套 + known_gaps 如实列 + review 行 + instance_versions 双端锚）；repo-delete-report-body 为 rest 域首金样，known_gaps 明示「statusMsg 转述级锚、逐字节以 wire evidence 为准」——不越证据边界。

### 必须修改（blocking）
- 无。

### 建议改进（non-blocking）
- internal/client/repo.go:188-190 — DeleteRepo doc comment 仍称「answers 200 plain text；非空仓 demands ?deleteContent=true」，现为 200 JSON 报告体 + 参数忽略。注释级失真（deleteJSON nil 目标不受行为影响）→ 随范围外 D-票改三行注释。
- internal/adapter/maven/handler.go:140-146 vs put.go:91-99 — 路径类谓词 `l.Snapshot || l.Timestamped` × handle* 拒绝判定在读写两门各写一份，后续策略维度演进有漂移风险 → 可收敛为一个 `refusedByHandlePolicy(cfg, l) bool` 小助手（同包，无分层代价）。
- docs/compatibility/contracts/maven-virtual.yaml ⑥ unobserved 清单未列「先落地后翻策略」（landed 制品 + config 改 handleReleases=false 后直读）臂；现实现按 A 机制（门与落地无关）应 409，但未直测 → 下次 L 系补一腿或补 unobserved 一行。

### 范围外发现（交 conductor，不入本票结论）
1. **web 控制台 DELETE 面陈旧**：web/src/lib/repos.ts:243-250（注释称 400 门；apiText 现会把 JSON 报告体原文当成功文案展示）、web/src/i18n/locales/en/repositories.ts:265 及 zh 对应键（「非空仓必须勾选」误导性文案）、web/src/pages/repositories/RepoDeleteConfirm.tsx:19-21（400 两段流注释）→ 建议 web 微票：deleteRepo 改 apiJSON 消费 statusMsg + 文案/注释对齐 D-3。
2. **docs/design 三处 400 契约陈述失真**：console-ux.md:472、console-m8.md:324、frontend-rewrite-audit.md:629 仍写「非空不带 deleteContent=400」→ 建议并入上述 D-票或 architect 卫生票。
3. **前缀面泛化缺口**：internal/adapter/generic/handler.go:245 与 generic/iteminfo.go:64 的 201 Location / uri/downloadUri 仍裸根（无 /binflow）——与 T-561/T-563 同 bug 类；T-561 台账已明示「泛化面未取证」限定 maven，纪律无瑕疵，但 generic/nuget 等面大概率同根因 → 建议立探针票取证后立账（勿直接猜着改）。
4. reports/agents/R6-pr172-review-a.md（PR#172 的评审报告）随本 PR 入树——工作日志随任务分支提交符合现行惯例，仅提示 conductor 收编时留意归属。
