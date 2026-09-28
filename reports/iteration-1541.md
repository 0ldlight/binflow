# iteration-1541 · R2：BIN-M1 残差收口轮（2026-09-28）

轮次目标：收口 R1 遗留的 BIN-M1 残差（单源常量、台账登记、ADR 裁定、差分臂、npm Facet 消费），
并处置轮中发现的主干 CI 红灯。

## PR 与合并记录

| PR | 内容 | 状态 |
|---|---|---|
| [#160](https://github.com/0ldlight/binflow/pull/160) | R2 载荷（7 提交：6 票 + 双审收口）→ develop | MERGED 07:35:34Z（1b788171） |
| [#161](https://github.com/0ldlight/binflow/pull/161) | develop→main 定期保鲜（即时正当：main CI 红灯随 f6336086 修复） | MERGED 07:36:28Z |

双审：Reviewer A（correctness）APPROVE 0 blocking · Reviewer B（architecture）APPROVE 0 blocking
（reports/agents/R2-pr-review-a.md / -b.md）。两人独立点名同一 non-blocking 缺口
（dedup 矩阵缺 local+cache 无本体行）→ 已在 252d55f4 补齐（6/6 PASS）。
合并门=双 APPROVE + 本地哨兵（策略：PR 无 CI）。

## 完成票（Linear 状态已同步）

| 票 | Linear | 内容 | 提交 |
|---|---|---|---|
| T-532 | BIN-15 ✅Done | replication probe `-cache` 字面量收编 `remote.CacheSuffix` 单源 | 04847e27 |
| T-533 | BIN-16 ✅Done | difftest L030 maven batch2：snapshotVersions 条件合并 + D-4 GAV 失配双臂 | 970c2b1e |
| T-536 | BIN-19 ✅Done | known-divergence +5 诚实登记（D-3 UNKNOWN 从严、kcache 404 措辞 BUG、3 条 UNKNOWN） | 46aa3658 |
| T-537 | BIN-20 ✅Done | ADR-0051 Errata 一：N1 对齐不改码 / N3→F1-residual（3-leg 验收+O6 反转预登记）/ N4→F8 降级；§5.3 废止行替换 + npm 验收五条 | d3536dee |
| T-538 | BIN-21 ✅Done | npm virtual packument FacetCache 去重（Errata 一-④ 五条逐条对账通过；npm 11.19.0 真实客户端验证） | 399da0ec |
| T-539 | BIN-22 ✅Done | 主干 CI 修复：cleanup wiring 测试仓名撞 `-cache` 保留后缀（w-cache→w-remote 9 处，守卫不放松） | f6336086 |
| — | BIN-18 ✅Done | N1/N3/N4 DISCOVERY 裁定闭环（无实现债）；后续立 BIN-23/BIN-24 | （裁定随 T-537 落地） |

新立后续票：BIN-23（T-540 F1-residual `<K>-cache` REST 面零上游 3-leg 收口）、
BIN-24（T-541 F8-widened walk 层 handle\* 半边同收）。

## L030 差分结论（reports/compatibility/L030-maven-v2-batch2-snapshot-versions.md）

- **BIN-16 = BinFlow BUG**：virtual/member 两面 `<snapshotVersions>` 无条件合并——A 面在 java-agent UA
  下剥离、M3-capable UA 下合并；B 面两种 UA 都合并（`virt_m3no_sv`/`member_m3no_sv` 双违规）。
- **D-4 = BinFlow BUG**：GAV 失配部署 A 面 409 errors[] + mvn exit 1，B 面 201 落地
  （`suppressPomConsistencyChecks` 在 repo_config_render.go 有渲染点、internal/ 无执行点）。
- 附带观察：B 201 响应 URI 缺 `/binflow` 前缀；B 裸目录 GET 400 vs A 404。
- 两条 BUG 的 known-divergence 登记按文件地界（docs/compatibility/ 唯一写者=compatibility-engineer）
  留待 R3 台账批，沿 L028→T-536 先例。

## 诚实失败与教训

- **主干 CI 红灯漏网**：PR #156（refuseCacheProjectionKey 守卫）经 develop 进 main 后
  run 36372406888 才暴露测试仓名撞车——PR 无 CI 策略下回归只能在 main 见光。修复 f6336086 随本轮进 main
  （#161），转绿结果下轮核。已按策略接受；B3 nightly 建立后另有兜底。
- known-divergence 计数脚本三次才对（字段名/子串过宽/前缀精确），最终口径：
  resolved=顶层 `resolved` 键；gated=无顶层 resolved 且 `review_gate` 以 'resolved' 开头；其余=open。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（本轮无新行；L030 属证据强化非新面）
- **Y**= 109.5（compatible + 0.5×partial + 0.5×superset，不变）
- **coverage**= 60.50%（109.5/181）
- **Z（open 已知偏离）**= **38**（+5：诚实登记五条；总 73 = 31 resolved + 4 gated + 38 open
  —— BUG 5 / INTENTIONAL 1 / UNSUPPORTED 1 / UNKNOWN 31）
- 口径注：Z 上升是登记纪律的产物（把已知差异从「未记账」翻到「已记账」），非行为劣化；
  R3 的修复票（BIN-16/D-4）与台账批会把其中 BUG 类收口。

## 哨兵证据（收编后全量）

- `go build ./...` exit=0；`go vet`（npm/replication/binflow-server）exit=0
- `go test ./internal/adapter/npm/...` ok 28.244s（+此前 30.817s）；`./internal/replication/...` ok 25.285s
- `go test ./cmd/binflow-server/ -race -count=1` ok 88.970s（T-539 验证，其后该包零变更）
- `web/ npx tsc --noEmit` exit=0（本轮 web/ 零触碰，例行全跑）
- golangci-lint 0 issues（Reviewer A 独立复跑）
- 凭据扫描：全部提交物干净（admin/password 只存在于会话 env / /tmp/r2-difftest.env chmod 600）

## R3 候选池

修复与收口：BIN-16 修复票（snapshotVersions UA 条件）、D-4 修复票（suppressPomConsistencyChecks
执行点）、台账批（L030 两 BUG + T-538 两条 pre-existing 漂移：npm remote 上游 wire 布局、
DELETE member 后 virtual 配置/member ledger 失同步）、BIN-23（F1-residual 3-leg）、BIN-24（F8-widened）。
差分臂：npm Facet 过滤 A 面臂（T-538 case 素材已备）、T-536 报告的五条差分随访臂。
携带项：F1 generic-package wire 测试、isSnapshotResolutionPath 臂、T-525、MySQL BIN-13、
B3 nightly + uat_approval（待用户）、main CI 转绿核验（#161 合并后首个 main run）。

## 用户待办（无变化）

B3 CircleCI nightly trigger（03:17 UTC, main）；部署轮次时 `uat_approval` 点击；T-473。
