# R13 载荷 PR 双审 — Reviewer B（architecture 视角）

```
Ticket:        R13 迭代载荷（origin/develop..HEAD，14 提交；对应 Linear BIN-75..BIN-88 / T-593..T-606）
Role:          code-reviewer (reviewer-b)
Area:          台账保真 / area 边界 / ADR 一致性 / 单源纪律 / 测试硬门 / 防双计 / 提交-代码一致性
Input:         conductor 派发单（评审重点 1-7）；git diff origin/develop..HEAD 全量 46 文件（+3406/−316）；
               docs/compatibility/known-divergence.yaml（HEAD/09c7e93f/develop 三态）；reports/agents/T-593..T-606；
               DECISIONS.md ADR-0052 及 Errata 一/二/三；docs/reverse/{maven-npm-pypi,repo-semantics}.md 勘误 diff
Changes:       评审了 14 提交的逐提交文件清单（git show --stat 全量）+ 四个实现热点逐行追读：
               maven/handler.go writeSidecarBody（动词条件模型）、maven/put.go putFile/putSha512ChecksumFile/
               writePlaneSrvgen/archiveSrvgenDeclared/writeCreated/itemInfo（oc 单源链）、httpapi/router.go
               deployEngineRemote 谓词、remote/fetcher.go 错误外化（报告核对）；上下游追读至 serveSidecar/
               serveSidecarOfPath→writeSidecarDigest、walk.go sidecar 腿、repo.OriginalChecksums 消费点
Files:         台账两批（09c7e93f/3bd35c62）= 抽 6 条 resolved 全对（见下）；11 个代码/测试热点文件逐行；
               12 份 T-报告全文通读（T-593..T-606）；DECISIONS.md ADR-0052 段 + Erratum 三 diff；
               docs/reverse 两文件 append-only 核验（0 删除行）
Tests:         本评审只读取证全绿：go build ./... exit 0；go vet 五包 exit 0；gofmt -l internal/ 空；
               golangci-lint run maven+generic+remote = 0 issues；
               新行为测试定向复跑 18 函数全 PASS（maven 3.045s）+ generic/httpapi/remote/repo/cargo 五包定向全 ok
Commands:      见下方「独立验证命令与输出」
Outputs:       reports/agents/R13-pr-review-b.md（本文件）
Compatibility: 台账保真成立——python yaml 独立重计数 HEAD=143 条、resolved 字段 80 / 无 63（=80+63 ✓）；
               中间态 09c7e93f=139=78+61 ✓；develop 基线=132=74+58 ✓（推进链 132→139→143，+6 flips 与
               两批声称的 4+2 条 resolved 完全对应）；diff 新增 id 恰 11 条=5+2+4 与两批提交信息吻合；
               抽查 6 条 resolved（⑨⑩+c2+c7+BIN-76+BIN-77）evidence 句逐一对回 T-593/594/595/596/597/598/599
               报告 Outputs，数字（28/28×2、20 adjudicated、16/16、18/18、14 腿、23 腿/轮=18 adjudicated+
               setup/teardown）与定性（mvn exit 0、sha1 8237d104、oc verbatim、nongav 201）全部对应无虚报
Security:      无新增攻击面（评审范围内）：T-595 路由键只取终端文件名判等且 no-op 零存储写、401/403 先于
               drain；T-596 写拒绝链零放宽（405→404 同强度）；T-597 NFR-S13 私网上游守卫保留+双面负测腿；
               探针凭据 env-only 零落盘（各报告 Security 段抽查一致）
Performance:   T-594 撤过算=净减；T-598 HEAD 面一次 digestTriple（查表级）；T-599 srvgen×带声明多付一次 SET
               单语句+一次行重读（每部署一次，非热循环）；各报告 Performance 段与代码实读相符
Risks:         GATE 二进制互含并行票在途改动（T-598/T-599 共享 worktree）——活体证据认证的是中间态而非最终
               合包树（两报告已披露+论证腿面不交叠+跨域套件绿+本评审终态 build/vet/lint/tests 全绿缓解）
Blockers:      无
Next:          ① NB-1：合并后 CI 差分腿对终态 B 二进制补一轮确认（或依赖既有双闸流程记录豁免）；② NB-3
               台账卫生票（6 条 UNKNOWN+resolved legacy 条目升分类）交 compatibility-engineer；③ NB-2 报告
               模板统一「NOT_RUN 清单」段名（process 级，下轮起）
```

## 评审报告 R13-pr（形态: reviewer-b）
结论: **APPROVE**
### 必须修改（blocking）
- 无
### 建议改进（non-blocking）
- **NB-1 活体证据的中间态性**：T-598 的 B GATE 二进制含 T-599 在途 put.go 改动、T-599 的含 T-598 handler.go 改动（两报告 Risks ①/② 互证披露，腿面论证成立）；终态树已由本评审 build/vet/lint/18 测试复验全绿，但「最终合包二进制」无专属活体轮——建议合并后差分腿补证或显式记录豁免依据。
- **NB-2 NOT_RUN 标注样式不统一**：T-596/597/598/599 有显式「NOT_RUN 清单/如实清单」段；T-593（Risks ②⑤）、T-594（Risks ①②③ probe 候选）、T-595（「未验证角落」段）实质等价但段名不一——四态口径实质达标（无一项把未探面冒充 PASS），样式统一属 process 改进。
- **NB-3 台账 legacy 口径混用（范围外，非本载荷引入）**：6 条 UNKNOWN 分类却带 resolved 字段（docker/remote-ping-credential-class-message-granularity 等，均已核实本载荷 diff 零触碰）；另存量 54 处 review_gate 文案与 resolved 字段有无不一致——计数口径不受影响（以 resolved 字段为准自洽），台账卫生候选。
- **NB-4 ⑩ 台账 evidence 措辞**：「GATE 23 腿/轮 × A/B × 双轮 … A-vs-B 全 PASS」将 18 adjudicated + setup/teardown/residual 面合计为 23，台账单句读作 23 adjudicated 有轻微歧义（报告原文两者并陈，非虚报）。
- **NB-5 generic handler.go 包内并行交叠**：T-593（serveClientChecksum/serveVirtualClientChecksum）与 T-597 d 臂（serveRemoteChecksum）同文件不同函数并行在途，两报告披露+顺序提交化解、终态绿——area 分区粒度在包级以下无硬边界，建议 conductor 派发时对同文件小臂错峰或并票。
- **NB-6 writePlaneSrvgen best-effort 语义**：class 缝任何 miss（含配置读失败）读作 client 默认，理论上可对真 srvgen 仓误 409——已在代码注释+提交信息声明为 generic checksumPolicySrvgen 容差惯例同款，接受。

## 评审重点逐项结论（对派发单 1-7）

1. **台账保真**：通过。三态计数链独立复算全对（132/74/58 → 139/78/61 → 143/80/63）；11 新 id 与两批声称吻合；抽 6 条 resolved（含⑨⑩）evidence 句 vs 报告 Outputs 逐一对上，无证据与预期不符。
2. **area 边界**：通过。14 提交逐一对文件清单 vs 票面 Area：T-593/594/595/598/599 严守单包；T-596 的 generic 测试面+PRD 回写=票据级豁免（报告 Ticket 行显式声明）；T-597 的 conductor 侧域外翻新（cargo/remote_test.go、repo/remote_fetch_test.go）在提交信息（"conductor-side out-of-area renovations"）与 T-597 报告（Risks 1 verbatim 交接 + Next 1）双侧如实登记。NB-5 为唯一包内交叠（披露化解）。
3. **ADR 一致性**：通过。ab98b525 对 DECISIONS.md 仅 1 行插入（append-only ✓，Errata 一/二原行未动，正文原行保留声明+NB-① 编号顺延显式在案）；put.go srvgen 模型与 ADR-0052 决策 6（缝不读 policy、策略门留 adapter——writePlaneSrvgen 在 adapter 读 class 缝）及 T-584 已确立的「SET 无条件化+校验门保策略域」家族语言自洽；决策 6④「header 声明腿不外推」的 NOT_RUN 候选已被 L041 Arm 6 活体+conductor 裁定（BIN-81）正当超越，链条留痕完整。
4. **单源纪律**：通过。oc 单源：writeCreated→itemInfo 唯一经 repo.OriginalChecksums(node)（put.go:1029），archiveSrvgenDeclared 的 svc.Get 重读（put.go:768）是喂该单源的唯一新路径，无请求 declared 二读；ocComputed=true 为 cd 面裁定形态（T-595）非双源。writeSidecarBody：grep 实证生产调用点仅 handler.go:338（serveSidecar 族）+ walk.go:269，maven sidecar 200 面唯一渲染出口维持；writeDerivedSidecar 独立契约族有 doc 注释钉界。
5. **测试覆盖硬门**：通过。协议票 T-593..T-599 全部有活体差分 A/B × 双轮证据（GATE 腿数/drift/residual 逐一在 Outputs）；未探面如实登记（NB-2 样式）；新测试 7 文件全部行为命名、零票号文件名，且本评审定向复跑 18 函数全 PASS。
6. **防双计**：通过。四条新 UNKNOWN 语义分立：sidecar-head-validator-set（generic HEAD validator 集）vs sidecar-get-last-modified（GET Last-Modified，rationale 自注可并探票）vs error-face-ct-charset-head-cl（错误渲染面，自注与 errors-envelope 序列化风格「同族不同面」）vs client-policy-declared-409-wording（409 文案族，与 checksum-oc-sha256-write-through 的 oc 值面、srvgen-declared-header-drop 的注册处置面两向分立，T-599 Compatibility 有显式防双计注记）；与 storage/artifact-get-* 族（工件本体 GET 头）无重叠。
7. **提交-代码一致性抽检**：通过。0ac12377（writeSidecarBody GET 裸 infra 集/HEAD 全 validator 集/INM 惰性化/doc 注释钉模型——handler.go:341-393 逐条对应；本提交零 put.go hunk ✓）；010b0afd（putFile expect 置空 put.go:461-464、putSha512ChecksumFile writePlaneSrvgen 门+同模型 688-703、archiveSrvgenDeclared SET 无条件+重读+node.RepoKey 寻址 749-775、client 策略臂透传不变——逐条对应）。

## 独立验证命令与输出（实跑摘录）

- `python3` yaml 三态计数：HEAD `total: 143 / resolved: 80 / open: 63`；09c7e93f 态 `139/78/61`；develop 基线 `132/74/58`
- `git diff origin/develop..HEAD -U0 -- docs/compatibility/known-divergence.yaml | grep "^+- id:"` → 恰 11 条新 id（与两批声称 5+2+4 吻合）
- legacy 7 条 UNKNOWN+resolved 条目 `git diff | grep -c <id>` → 全部 0 hunk（未被本载荷触碰）
- `grep -rn writeSidecarBody internal/ --include="*.go"`（排除测试）→ 仅 walk.go:269 + handler.go:338/362（定义）
- `go build ./...` → exit 0；`go vet` 五包 → exit 0；`gofmt -l internal/` → 空；`golangci-lint run maven+generic+remote` → `0 issues.`
- `go test ./internal/adapter/maven/ -run '<18 个新行为测试>'` → 全 PASS（ok 3.045s）；generic/httpapi/remote/repo/cargo 定向 → 全 ok
- `git show 66b83f7b -- docs/reverse/*.md | grep -c "^-[^-]"` → 0（append-only ✓）；`git show ab98b525 -- DECISIONS.md` → 1 insertion（原行保留 ✓）
