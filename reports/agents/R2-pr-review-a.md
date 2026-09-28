# R2 PR 评审（reviewer-a / correctness）— claude/r2-binm1-residuals vs 89e125ba (origin/develop)

评审范围：89e125ba..399da0ec 全部 6 提交（04847e27 / f6336086 / 46aa3658 / d3536dee / 970c2b1e / 399da0ec）。
评审 worktree：agent-a9b56af5567a45061（detach 于 399da0ec 的隔离副本，只读取证）。

## 结论

**APPROVE**（0 blocking / 3 non-blocking / 范围外上报 2 项）。

## 逐提交意见

### 399da0ec feat(npm): dedupeCacheFacetSteps（T-538/BIN-21）— 重点评审

- `internal/adapter/npm/virtual_packument.go:82` 唯一消费点（`loadPackument`→`loadVirtualPackument`，npm adapter 内 `VirtualMemberOrder` 仅此一处消费，version manifest 同路径）——覆盖面完整，tarball 走 `svc.Get` walk 层（T-530 已 facet 感知）不在此面。
- `dedupeCacheFacetSteps`（:166-185）：先一遍分类记 `isCache` + `hasRemoteBody`，含任一 remote 本体步（`Type==TypeRemote && Facet==FacetPlain`）则滤掉全部 FacetCache 步。核对 `internal/repo/virtual.go:194-205`：cache 步恒由 remote 步拷贝而来（`typ==TypeRemote`），本体步判定条件与 seam 产出不变量一致；`ReadVirtualMember`（virtual.go:661-681）按 key 首匹配且**无 facet 分支**——同 remote 的 cache 步+本体步若无滤除确实产生两次完整 `readRemoteMemberDoc`→FR-20 Fetch + 两条 markDownload 审计行（同仓双查属实，注释无夸大）。
- 过滤不可能清空序列（触发过滤必存在本体步）；`members[0]` 解引用前有 `len(members)==0` 守卫；过滤稳定保序，priority 语义（npm 无短路、只影响序）不受扰；base/hints 选择无观测差（滤掉的 cache 步与本体步对 ReadVirtualMember 是同一调用）。
- `cacheFacetOfStep`（:143-154）：显式 case + 未知 facet 降级 plain 带 WARN——fail-open 且可见，与 maven T-531 先例同形。
- 边界：`mergeVirtualPackuments` / TTL / F5 聚合缓存逐字未动（diff 证实）。

### 04847e27 refactor(replication): CacheSuffix 单源（T-532/BIN-15）

- `internal/remote/projection.go:25` `const CacheSuffix = "-cache"` 与旧字面量值相同，零行为差；`internal/replication` 已有 `internal/remote` import（probe.go:12），无新依赖边；grep 证实 replication 非测试代码无残余 `"-cache"` 字面量。`go test ./internal/replication/` 绿。

### f6336086 fix(test): w-cache → w-remote 改名（T-539/BIN-22）

- 动机核实：`internal/repo/validate.go:69 refuseCacheProjectionKey` 自 T-530 起拒绝任何 `-cache` 后缀建仓，旧 key `w-cache` 的 `CreateRepo` 会 400——main CI 红灯成因成立。
- 9 处改名（CreateRepo、2×seedCacheNode、audit 事件、5×断言 Get）一致，grep 无漏网 `w-cache`；语义不变（cache 表按 repo key 寻址、无后缀依赖）。改名后测试 PASS（0.40s）。

### 46aa3658 docs(compatibility): known-divergence +5（T-536/BIN-19）

- YAML `yaml.safe_load` 通过；5 条目字段形状（id/surface/classification/rationale/authority/review_gate/evidence）与存量一致；UNKNOWN/BUG 定级均附 authority 缺口说明与差分取证门，无「猜测补齐」。

### d3536dee docs(adr): ADR-0051 Errata 一（T-537/BIN-20）

- DECISIONS.md Errata ①-⑤ 与 `docs/design/virtual-four-bucket.md` 5 处批注（§3 第 5 条注、§5.2 表注、§5.3 兼容承诺作废替换 + npm 验收口径声明、O6、F1 残余/F8 扩面行）双向对得上；其中的 as-built 断言我逐一核实为真（walk 层 cache 步走 probeLocalMember、ReadVirtualMember 无 facet 分支、refuseCacheProjectionKey 形状）。

### 970c2b1e test(difftest): L030 batch2 两 case（T-533/BIN-16）

- 两文件 `py_compile` 通过；CASE/run/judge/evidence 形状与既有 case 及 `_mavenlib.py` 契约一致；断言值 side-relative（bn/成员 id/ext 集）防跨侧时间差伪造分歧；凭据仅经子进程 env 注入（_mavenlib.py:232-233），case 文件内无凭据字面量；`finally` 清理 + SetupError→BLOCKED 四态齐。报告如实记录 r1 slug 笔误修正与 r2/r3 稳定复跑、两 case FAIL（B 违例）= 差异已登记为 BUG——用例做了它该做的事。

## 验收五条对照（T-538）

| # | 口径（virtual-four-bucket.md §5.3 声明 / ADR-0051 Errata 一-④） | 判定 |
|---|---|---|
| ① | 含任一 remote 本体步 → 滤掉**全部** FacetCache 步（不限同 remote） | 满足（virtual_packument.go:169-184；表用例 1「随处即滤」钉住跨 remote 滤除） |
| ② | 按字面实现不做恒滤；无本体步 → cache 步保留 | 满足（:175-177 早退；表用例 3「纯 cache 序列原样保留」——恒滤简化会红） |
| ③ | 上游 packument 恰读一次；repeat tarball 零上游 + 无 X-BinFlow-Cache 头 | 满足（集成测试 plan 层断言存活步 + 执行层 `hits==1` 原子计数；repeat tarball 锚在基线已翻新的 TestVirtualRenderSeams:135-148——不属本票且已就位） |
| ④ | 不改 F5 聚合写 / TTL / 版本合并 | 满足（diff 仅 walk 过滤 + 注释；merge/putIfAbsent/union 逐字未动） |
| ⑤ | table-driven + 真实栈集成能抓回归 | 满足（变异推演：删调用→表用例 1 + 集成 plan 层红；条件写反→表用例 1 红；恒滤→表用例 3 红；未知 facet 误判为 cache→表用例 5 红。前置断言保证非空洞——先证 order 恰发 1 个 cache 步。测试注释如实声明 FR-20 自身缓存会掩盖执行层双取计数、判别在 plan 层——诚实且有齿） |

## Non-blocking（建议，不阻塞合并）

1. `virtual_packument_cache_skip_test.go:44-74` 表缺一行 `[local-plain + cache 步、无 remote 本体]` 形态——可钉住「把『任一 remote 本体步』误读为『任一 plain 步』」的假想错实现（当前 5 行用例对该变异全绿）。补一行 want 原样保留即可。
2. F7 缝预告（交 conductor 登记）：F7（远端抑制）落地日，聚合面保留的 cache 步经 `ReadVirtualMember` 仍走 `readRemoteMemberDoc`→FR-20（virtual.go:680-681，无 facet 分支、带新鲜窗/回源），并非 walk 层的 probeLocalMember 零上游语义——F7 票需同步裁定聚合面 cache 步的读法，否则「抑制本体仍读 cache」的意图在聚合面落空。今日不可达（F7 未实现），代码注释已声明规则对序列陈述。
3. `dedupeCacheFacetSteps` 对未知 facet 的 WARN 在循环内逐步逐请求触发——未来新增 facet 值若忘记同步此 case，日志噪音先于行为异常出现，属可接受的 fail-open 代价（与 maven 先例一致），仅提醒扩 facet 时两处 case 同步。

## 范围外上报（交 conductor，不计入本 PR 结论）

1. T-538 报告 Compatibility 行漂移点 A：npm remote 上游 wire 路径=存储布局 identity 映射（无 UpstreamPath facet），remote 直指 registry.npmjs.org 时 packument 404——pre-existing，建议登记 known-divergence 或 wire 翻译票（报告 Next ② 已列）。
2. T-538 报告漂移点 B：DELETE 成员 remote 仓后 virtual 的 config JSON 成员清单与 member ledger 失同步，聚合静默退化至 POST 重存才恢复——pre-existing 巡检/差分关注项。

## 验证命令与输出（本评审实际执行）

- `git log --oneline 89e125ba..399da0ec` → 恰 6 提交；`git diff --stat` → 17 文件 +1045/-25
- `go build ./...` → exit 0
- `go vet ./internal/adapter/npm/ ./internal/replication/ ./cmd/binflow-server/` → 干净
- `gofmt -l internal/adapter/npm internal/replication cmd/binflow-server` → 空
- `golangci-lint run ./internal/adapter/npm/...` → 0 issues
- `go test ./internal/adapter/npm/ -count=1` → ok 29.221s（T-538 报告声称 29.168s，复现一致）
- `go test ./internal/adapter/npm/ -count=1 -race -run 'TestPackumentWalkCacheFacetDedup|TestVirtualPackumentAggregationSkipsCacheFacetSteps|TestVirtualPackumentMergeMatrix' -v` → 全 PASS（5 子用例 + 2 集成 + 矩阵）
- `go test ./internal/replication/ -count=1` → ok 19.325s
- `go test ./internal/repo/ -count=1` → ok 67.062s
- `go test ./cmd/binflow-server/ -count=1 -run TestT324WiringCronOverRealStack -v` → PASS 0.40s（报告声称全包 -race 88.970s，本评审聚焦跑未带 -race 全包）
- `python3 -m py_compile` 两 difftest case → ok；`python3 -c yaml.safe_load(known-divergence.yaml)` → ok

取证限制（Blockers 口径，不影响结论）：真实 npm CLI / curl 双发 B 面腿与 L030 双系统差分腿需活体 A 参照与网络环境，本评审未复跑——单测/静态证据链完整且与 T-538/T-533 报告自述输出交叉一致（含具体 shasum/字节计数等可证伪细节），按「报告证据内部一致 + 可复现部分全复现」采信。

clean-room 抽查：改动无任何与 reverse-src 逐行对应形态（reverse-src 不在工作树；引用面均为 docs/reverse 行为规格句式）。
