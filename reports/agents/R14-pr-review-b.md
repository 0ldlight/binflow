# R14 载荷 PR 评审 B（reviewer-b · architecture）

```
Ticket:        R14 载荷 PR 评审（range d9ea6344..HEAD，9 提交，44 文件 +2736/-66）
Role:          code-reviewer (reviewer-b, architecture)
Area:          internal/{repo,remote,httpapi,adapter/generic,adapter/maven} + docs/{design,compatibility,reverse} + DECISIONS.md
Input:         conductor 派发（B 形态清单 6 项）+ 实读 architecture.md §3/§5.1、ADR-0054/0052 Erratum、known-divergence.yaml、maven-virtual.yaml、repo-semantics.md 勘误块、T-607/T-617 报告
Changes:       9 提交逐条 git show；T-608/T-607/T-617 产品码 diff 全读；上下游追读=import 图（go list 直读+传递）、消费点 grep、存量测试改动抽样
Files:         见下逐条结论
Tests:         go build ./... RC0；go vet 五包 RC0；gofmt -l 五包空；新测试独立复跑全 ok（repo 1.9s / remote 3.8s / generic 1.7s / maven 2.1s / httpapi 定向 3.4s）——与各报告 PASS 声称一致
Commands:      git log/show/diff、go list -f '{{join .Imports "\n"}}'、grep、sed 实读锚点、go build/vet/gofmt/test（全部只读）
Outputs:       reports/agents/R14-pr-review-b.md（本体经消息交付，conductor 落盘）
Compatibility: 台账 8 条目实读核对全一致（见结论 3）
Security:      凭据纪律达标——报告与 diff 中仅占位符凭据；无真实凭据值出现
Performance:   SetDownloadDisposition=每下载一次 path.Base+escape+Set；HEAD 侧car 臂 digestsOf 仅 HEAD 面；无热路径劣化
Risks:         BIN-100 仓内零引用（见 Next，conductor 已解析）；BIN-102 face 矩阵落地前 helm BIN-103 修复票有前置依赖（台账已写明）
Blockers:      无
Next:          ①BIN-100 身份确认（已解析，见文末 conductor 附记）；②ADR-0054 工程 ticket 1/2 仍待 Linear 立项（已立 BIN-104/105）；③BIN-107/108 落地时 redactUserinfo 须提升为叶子包（见 NB-3）
```

## 结论：APPROVE（blocking 0 / non-blocking 5）

### 六项关注清单逐条（证据锚）

1. **分层与依赖方向 — 通过**
   - T-608：`internal/repo/downloadheaders.go` 导出两个纯函数，消费方 generic/maven 在 d9ea6344 基线**已** import internal/repo（实读基线 import 块）——零新 import 边；`adapter/* → repo` 正是 architecture.md §5.1 依赖方向条款（L676）明文 sanctioned 的方向；仅消费导出函数、不摸内部结构，合边界规则 1。
   - T-617：`redactUserinfo`（internal/remote/fetcher.go:1203）保持包内未导出，4 个使用文件全在 internal/remote——零新边、零环。直读 import 图：remote→{adapter,metadata,storage}，adapter→{auth,metadata}，故 remote 传递依赖 auth。
   - T-607：config.go canonical 值座（`omitempty`，config.go:166）+ input 指针座（merge-on-omit，config.go:268）与同 struct 既有字段（BlackedOut 等）及 local 族 `checksumPolicyTypes` 先例（map 闭域+gate+*StatusError）逐点同构；两族文案异体并存且在 config.go:800-806 注释中显式说明——一致性成立。

2. **Seam 一致性 — 通过且为正样本**：T-608 的「repo 纯函数导出+消费方薄调用」与 ADR-0054（DECISIONS.md:1518）裁定的 VirtualRouteTarget 单源方向**同构**（adapter-base 方向在 ADR-0054 中已因 repo→remote→adapter 环被否）。无新增第 10 份三别名拷贝：promote.go `virtualDefaultDeployment`（internal/build/promote.go ~L406）与 internal/adapter/deploytarget.go{,_test} 均在位——与裁定「工程票延迟、golden-before-deletion」一致，非虚假完成声明。

3. **兼容策略 — 通过**：409→400 派单偏差按 A 活体四轮证据落地并在 T-607.md Compatibility 字段显式记录（「票面偏差记录」段）——证据条款优先于派单文案，流程正确。台账实读 8 条与 T-615/aa9d107c 声称一致：helm/error-404-media-type=BUG BIN-103（known-divergence.yaml:2591，含防环约束「helm 不得 import internal/httpapi」）；helm/empty-index-status=UNKNOWN R15 探腿（:2606）；api/docker-direct-face=UNSUPPORTED ADR-0010（:2618）；generic/dot-segment-guard=INTENTIONAL NFR-S11（:2629）；trailing-space BUG（T-607 side-finding）；BIN-101（:2512 review_gate）/BIN-102（:2555-2563，envelope.go:33 seam）两 UNKNOWN→BUG 翻转在案。

4. **提交纪律 — 通过，一项待确认**：9 提交全 conventional（fix×3/docs×6，scope 正确）；票-提交映射 T-604→BIN-86、T-607→BIN-89、T-608→BIN-90、T-609→BIN-91、T-610→BIN-92、T-611→BIN-93、T-615→BIN-97、T-617→BIN-99 全部可追（提交 subject+报告+台账三向一致）。**BIN-100 仓内零引用**（reports/、known-divergence.yaml、全 md/yaml grep 均空）——评审时无 Linear 工具无法核其状态（解析见文末 conductor 附记）。T-607.md/T-617.md 15 字段齐全且带四态测试记账与 NOT_RUN 披露，T-617 的 20 站点残留面审计表为范本级披露。

5. **文档票锚点 — 通过**：parseVirtualConfig 精确在 internal/repo/config.go:744；repo-semantics Erratum-2/3（L157/L165）语义与 T-609 声称逐条相符（后缀剥离源解析/第 2 故障开窗）且编号唯一性声明在案；maven-virtual 条目 13/14 在 :816/:901，walk.go overlayClient 臂（walk.go:264-270 邻域）与契约文互证。

6. **测试覆盖策略 — 通过**：7 个新测试文件全部行为命名（download_header_set_test.go×2 / downloadheaders_test.go / repo_config_ct_gate_test.go / repo_config_notfound_family_test.go / remote_checksum_policy_rest_test.go / userinfo_redact_test.go），函数名行为式、票号在头注释；存量票号命名文件仅 CT 助手补齐（t215/uploads 抽样 diff 证实断言行零改）。

### 必须修改（blocking）

- 无。

### 建议改进（non-blocking）

1. **NB-1（提交信息精度）** 95665949 声称「append-only proven by strict-prefix mechanical check」，实际 DECISIONS.md numstat=2+/1-（Erratum-三行尾追加补记句=1 改 1 增 + Erratum-四新行）。改动本身在同提交信息中已披露、无隐瞒，但「append-only/strict-prefix」措辞与事实不符——勿将该 claim 作为后续勘误提交的先例引用（对照：39c8db52 的 27+/0- 声称与 numstat 精确相符）。
2. **NB-2（锚点精度）** 76f6f268 提交信息锚「7.2/L263」：勘误块实际落 L157-167，现行 L263 已是 §8.4/8.5 地带；勘误块自身的章节级锚（§7.2 步骤 2/§9 M3-2）正确，仅提交信息行号锚过期。
3. **NB-3（前瞻环风险，BIN-107/108）** `redactUserinfo` 留在 internal/remote 对 httpapi 两站点（repositories_probe.go:116 / repositories.go:324）可安全复用（httpapi 已 import remote）；但 auth 域站点（internal/auth/ldap.go:432/444）**不可** import remote（auth→remote→adapter→auth 成环）。auth 域票落地时应把该正则提升为叶子包（或窄拷贝），不得从 auth import remote。
4. **NB-4（错误链）** ssrfguard.go CheckURL parse 错误臂由 `fmt.Errorf(%w)` 改 `errors.New(拼接)`，丢掉了 url.Parse cause 的 errors.As 链——因 cause 内嵌全量 URL 属被迫取舍且无消费方依赖该链，可接受，建议在注释中补一句「链式包装被安全要求放弃」。
5. **NB-5（单源收编边界）** internal/adapter/docker/remote_face.go:34 保有独立 `hdrContentDisposition` 常量（registry spec 面固定合成名 manifest.json/sha256__hex，非路径派生双形态）——downloadheaders.go:18 注释已声明该姿态；若未来探腿证实 docker 面同为路径派生双形态，届时并入单源。

### 范围外

① BIN-100 仓内不可追溯（解析见附记）；② ADR-0054 工程 ticket 1/2 待立项提醒（conductor：已落 BIN-104/105）；③ adapter/generic、adapter/maven 直读 import storage/metadata 系 d9ea6344 之前既有形态（是否属 §5.1 两例外覆盖范围未在本载荷内核实，不构成本 range 违规）。

---

## Conductor 附记（收编时解析，2026-09-30）

- **范围外① BIN-100 解析**：BIN-100/T-618 = main CI red 修复（npm audit brace-expansion 三 advisory，lockfile 6 行），其载体提交 = **d9ea6344 本身**——即本评审 range 的**基线提交**（经 PR #192 合入 develop，093193ae），按 range 定义（d9ea6344..HEAD，开区间不含基线）自然零命中，非笔误非欠账。Linear BIN-100 已 Done。
- **NB-3 已加固**：BIN-107（T-623 ldap）票面已按本 NB 更新为硬约束——auth 票落地时 redactUserinfo 提升叶子包或 auth 本地窄实现，禁 auth import remote。
- **NB-4 处置**：载荷已冻结（评审后零改），随 BIN-101/T-619 波次顺带补注（同 internal/remote 包）。
- **NB-1/NB-2 处置**：记录在案，作为后续勘误提交信息措辞与行号锚的教训（勿引为先例）。
