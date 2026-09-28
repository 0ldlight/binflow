# R2 PR 评审报告 — Reviewer B（architecture 视角）

```
Ticket:        R2 PR（分支 claude/r2-binm1-residuals vs origin/develop 89e125ba）· 6 提交：
               04847e27 T-532/BIN-15（replication 单源常量）、f6336086 T-539/BIN-22（测试改名修 main CI）、
               46aa3658 T-536/BIN-19（known-divergence +5）、d3536dee T-537/BIN-20（ADR-0051 Errata 一 + 设计批注）、
               970c2b1e T-533/BIN-16（difftest L030 maven batch2 两 case + 报告）、399da0ec T-538/BIN-21（npm Facet 消费）
Role:          code-reviewer (reviewer-b)
Area:          internal/adapter/npm + internal/replication + cmd/binflow-server（测试）+ docs（DECISIONS/design/compatibility）+ tools/difftest/v2
Input:         conductor 派发（形态 reviewer-b）；改动 diff 全量（17 文件 +1045/-25）+ 上下游追读：
               internal/adapter/maven/virtual_metadata.go（T-531 先例全文）、docs/reverse/virtual-resolution.md §5/§6、
               docs/design/virtual-four-bucket.md §3/§5.1/§5.3/§8/§9、docs/compatibility/known-divergence.yaml（头规则 + 既有条目先例）、
               reports/compatibility/L028/L029/L030、reports/agents/T-532/533/536/537/538/539、tools/difftest/v2/runner.py + _mavenlib.py + 既有 case 先例
Changes:       6 提交逐个评审；npm Facet 消费追读至 serveVersion→loadPackument→loadVirtualPackument 全链与 repo.VirtualMemberOrder seam 契约；
               ADR Errata 的 as-built 行号引用抽查 5 处；台账新 5 条对 L028/L029/L030 结论逐条互洽核对
Files:         见「五视角意见」逐文件节
Tests:         见 Tests/Commands 字段（全部只读复跑，全绿）
Commands:      go build ./…（exit 0）；go vet ./internal/adapter/npm/… ./internal/replication/… ./cmd/binflow-server/…（exit 0）；
               gofmt -l 三包（空）；
               go test ./internal/adapter/npm/ -run 'TestPackumentWalkCacheFacetDedup|TestVirtualPackumentAggregationSkipsCacheFacetSteps|TestVirtualRenderSeams' -count=1 -v → 全 PASS；
               go test ./internal/adapter/npm/ -count=1 → ok 28.304s；go test ./internal/replication/ -count=1 → ok 18.990s；
               go test ./cmd/binflow-server/ -run TestT324WiringCronOverRealStack -count=1 → PASS 0.25s；
               python3 -c yaml.safe_load(known-divergence.yaml) → 73 entries 解析成功，新 5 条字段完备、id 无重复；
               python3 -m py_compile 两新 case → OK；git grep '"-cache"' 产品码非注释命中 = 0（单源成立）
Outputs:       reports/agents/R2-pr-review-b.md（本文件）
Compatibility: 见视角 2/3；与 docs/compatibility/contracts/ 无冲突面（ADR Errata 自证 -cache 族无 VERIFIED 契约）；
               known-divergence +5 分类纪律合规（详见视角 3）
Security:      difftest case 凭据全程 env 注入（mvn settings 用 ${env.*}）、错误体 excerpt 240 字截断 + sha256；
               npm dedup 纯内存枚举消费无新增上游面；报告与命令无凭据字面量（本报告同守）
Performance:   dedupeCacheFacetSteps 每请求 O(n) 一次分类+过滤，净减每 remote 一次冗余 ReadVirtualMember/FR-20 会话；无热 path 回归
Risks:         见「non-blocking」与「范围外」；均不阻塞合并
Blockers:      无
Next:          见结论区 non-blocking + 范围外移交清单
```

## 评审报告 R2-PR（形态: reviewer-b）
结论: **APPROVE**

### 必须修改（blocking）
- 无。

### 建议改进（non-blocking）
- **[npm 测试覆盖缺口·低风险] internal/adapter/npm/virtual_packument_cache_skip_test.go:52-77**：规则字面的「滤除对象是全部 FacetCache 步，**不限同 remote**」半边（ADR-0051 Errata 一-④ 验收口径第 1 条明文）无表行——现有 5 行里每个 cache 步都配了自己的本体步（remote A 的 cache 因 remote A 的本体被滤）。当前 seam 每 remote 恒成对发射故不可达，但 F7 落地后「无本体步序列」即出现，届时该半边从注释语义变成运行语义。建议 F7 票或下次触文件时补一行 `{remCache(A), remBbody(B)} → want [B]`（一行表驱动，零产品码）。
- **[npm 测试覆盖缺口·低风险] 同文件**：serveVersion 单版本 manifest 面（serve.go:124 经 loadPackument→loadVirtualPackument 同一消费点）与「多 remote 混 plain」形态无真实栈集成腿——共享消费点使风险很低，table 层已有多 remote 形态；补腿价值=钉住 version 面不绕行，可并入下轮差分 case。
- **[台账互洽·移交] docs/compatibility/known-divergence.yaml**：L030 两个已取证 BUG（BIN-16 M3 谓词缺失、D-4 上传侧 GAV 校验缺失）本分支未落账——T-536（46aa3658）先于 L030（970c2b1e）执行，票面范围=R1 残差+D-3，无违规；但分支合并时点这两条差异只存在于 L030 报告里。建议 conductor 尽快派 compatibility-engineer 补登记票（BUG 2 条，evidence=L030），避免窗口期台账少记。
- **[验收口径第 5 条·跟踪] docs/design/virtual-four-bucket.md §5.3 验收声明第 5 条**（npm CLI + curl 双发差分腿）：T-538 仅完成 B 面单边（票面明示口径），A 面差分臂悬置——T-538 Next ① 已登记 surface 清单，须确保下轮差分批次真带上（勿让「票面豁免」沉淀为「永不差分」）。
- **[difftest 可维护性·微] tools/difftest/v2/cases/maven_deploy_gav_mismatch.py:67** `__import__("hashlib").md5(...)` 内联动态导入——建议顶部正常 import hashlib 或 _mavenlib 加 md5_hex helper（与 sha1_hex 对称）；:95 `GDIR_BAD.replace(BAD_ART, "right-lib")` 绕弯写法且结果仅入 raw 未断言，建议直写 "/com/diff/right-lib" 并注释 raw-only。纯可读性，不动行为。

---

## 五视角意见

### 视角 1 — T-538 与 T-531（maven）模式一致性：**一致，无跨协议漂移**

| 维度 | maven T-531（internal/adapter/maven/virtual_metadata.go） | npm T-538（internal/adapter/npm/virtual_packument.go） | 判定 |
|---|---|---|---|
| 过滤时机 | collectVirtualMetadata:264 在 VirtualMemberOrder 之后、成员读取之前 | loadVirtualPackument:82 同层（order 确定后、merge 逻辑前一行消费） | 同构 ✓ |
| 未知 facet fail-open | facetOfStep 显式 switch + slog.WarnContext（member+facet 记录），降级 plain | cacheFacetOfStep 同款显式 switch + 同款 WARN，降级非 cache | 同构 ✓（npm 只需布尔谓词，不设镜像枚举——合理简化，非漂移） |
| 过滤语义 | **无条件**滤 FacetCache（filterMetadataSteps:197） | **条件**滤：序列含任一 remote 本体步才滤全部 cache 步 | **规格驱动的差异**，非漂移：maven 依据 reverse §5.1「跳过所有 cache 仓」（无条件句），npm 依据 §6「只要有序列含任一 remote 本体，就把所有 cache 仓滤掉」（条件句）——两实现各忠实各自行，差异在 ADR-0051 Errata 一-④ 与 T-538 Compatibility 行明文锚定 |
| 消费点收口 | virtual_metadata.go 唯一 VirtualMemberOrder 消费方 | virtual_packument.go 唯一消费方；packument GET（packument.go:51）与 serveVersion（serve.go:124→loadPackument）同点收口；tarball 走 svc.Get/walk 层（T-530 面，本票不动） | ✓ |

npm 实现对条件句的字面忠实度核对：`hasRemoteBody` 判据 = Type==Remote && 非 cache facet；触发后滤**全部** FacetCache 步（不限同 remote、不问 Type）——与 reverse §6 原文及 Errata ④「滤除对象是全部 FacetCache 步」逐字吻合；无本体步序列原 slice 返回（零拷贝、无别名突变）。纯 cache 序列保留分支（F7 形态）有防御性实现 + 表行钉住——满足 Errata ④ 第 2 条「不做恒滤简化」。

### 视角 2 — 文档-代码一致性：**双向一致，无误提前实现/误关闭**

- **§5.3 验收五条 vs 实现**：①字面规则=条件滤 ✓（见视角 1）；②不做恒滤 ✓；③可观测锚——repeat tarball 无 X-BinFlow-Cache 头的钉子在 virtual_render_test.go:128-136（「want none (cache-facet local-semantics serve)」，T-530 已翻新，本分支维持绿）+ 新测试上游计数恰 1（fixture atomic hits）✓；④边界——diff 证实 merge 规则（base/putIfAbsent/union/latest）/failure policy/hints 逐字未动，无 F5 缓存写入 ✓；⑤载体——B 面 npm CLI（view/install/pack/sha256 三方比对）+ curl 已跑，A 面双发差分按票面口径归下轮（non-blocking 跟踪项）。
- **N3/F1-residual**：本分支零 internal/repo/service.go 改动——三腿验收面仅以 ADR Errata 一-③ + 设计 §9 F1 残余行**预登记**形态存在，未实现、未关闭 ✓。O6 翻案触发条件同步预登记 ✓。
- **ADR Errata as-built 引用抽查**（5/5 命中）：service.go:590 委托臂、virtual.go:897-906 CacheProjectionTarget（parent=="" 判据）、validate.go:44+ validateRepoKey 语法先行（service.go:2344-2350 顺序实证 validate→refuse）、projection.go:98-101 handle* 恒 true 自证注释——Errata 的 as-built 声明与代码一致，无「文档偷走样」。
- **T-532 单源声明核证**：产品码非注释 `"-cache"` 字面量仅剩 remote/projection.go:25 常量本体；repo 包经 `cacheProjectionSuffix = remote.CacheSuffix`（virtual.go:890）别名引用——单源成立，T-532/T-539 报告陈述属实。

### 视角 3 — known-divergence 台账质量：**+5 条字段齐整、分类纪律合规、与 L 系列互洽**

- 机器验证：YAML 解析 73 entries；新 5 条 id/surface/classification/rationale/authority/review_gate/evidence 七字段全非空；73 id 无重复。
- 分类纪律：D-3 票面倾向 INTENTIONAL 但三源 grep 无 authority → 按台账铁律落 UNKNOWN+pending-ruling（正确从严）；#2 BUG+spec_ruling 组合有存量先例（maven/version-metadata-pom-prerequisite）；rationale 明注「A 面活体逐字未拍、先行落账」且 review_gate 挂差分臂——披露完整。
- 与 L028/L029 互洽：L029 报告实证 D-1/D-2 已闭（r5-r7 稳定对）、D-3 移交——条目 1/5 的引用与限定（「L028/L029 仅覆盖无条目臂」）逐条核对无误；与 d08/repo-delete-response-envelope 的查重辩析成立（语义面≠体裁面）。
- 与 L030 互洽：**无矛盾**（T-536 先于 L030 执行），但 L030 两 BUG 未落账=窗口期缺口（见 non-blocking 移交项）。

### 视角 4 — difftest case 可维护性：**符合 v2 惯例，r2≡r3 稳定性判据已落地**

- 两 case 的 CASE dict（id/title/layer/domain/auth/timeout_s）、run(ctx)→{status,reason,evidence,requests} 返回形、_leg(side) 双腿结构、write_evidence 落点、SetupError→BLOCKED+清理，与既有 T-523 先例（maven_virtual_metadata_merge.py 等）逐项同构；runner.py 契约（ctx.http/sides[base,user,password]/timeout_s）核对无缺。
- 复用 _mavenlib（poll_until 预算不放宽、mvn 凭据 ${env.*} 注入、cleanup 含 deleteContent=true 重试防 key 滞留——重跑稳定性前提）；断言值侧相对化（bn/成员 id/ext 集）防跨面时间差伪差异。
- r2≡r3：L030 报告「轮次与稳定性」节明文记录 r2→r3 逐 case (status,reason) 完全一致 + r1 的 slug 笔误修正留痕——判据落地 ✓。r1 笔误暴露的是期望值拼写（pom+jar→jar+pom），修正仅动 case 侧期望常量，不触碰判据结构，处置得当。
- 微瑕见 non-blocking（__import__ 内联、raw-only 探针行）。

### 视角 5 — 测试覆盖面缺口点名（均 low risk，不构成 REQUEST_CHANGES）

1. 「不限同 remote」滤除半边无表行（non-blocking 第 1 条）。
2. serveVersion 面与多 remote 真实栈腿缺位（共享消费点兜底）。
3. 纯 cache 序列（F7 形态）仅 table 防御性钉住——当前不可达，形态正确。
4. 未知 facet 的 WARN 仅 table 层验证（日志旁证在 T-538 报告 Outputs 留痕）——够用。

---

## 范围外上报（交 conductor，不影响本 PR 结论）

1. **PR 无 CI 策略的回归暴露面**：T-539 根因链显示 PR 分支不跑 CI（main-only 触发），此类回归只能 main 暴露（本次烧掉两个 CI run 才发现）。T-539 报告已按既有策略接受并指望 B3 nightly 兜底——建议把「B3 nightly 建立」提级跟踪，或给 PR 加最小冒烟腿（tsc/lint/go build 级），避免每轮靠 main 红灯当探测器。
2. **L030 顺带观察两条**（B 201 created 体自指 URI 缺 /binflow 前缀；裸目录 GET 400 vs A 404）——已在 L030 报告遗留节登记未裁定，确认有归属即可。
3. **npm remote 上游 wire 布局差距**（无 UpstreamPath facet，直连 npmjs 时 packument 端点 404，B 面验证靠布局翻译代理）——T-538 Compatibility 登记、Next ② 已指向 architect/compatibility-engineer；提醒勿让其沉淀为隐性测试专用债。

## 断点快照
- 已完成：全部 6 提交评审 + 只读取证（build/vet/gofmt/npm/replication/cleanup-wiring 测试、YAML/py_compile/单源 grep/ADR 行号抽查）。
- 未完成：无。
- 断点位置：无（结论已出）。
