# iteration-1542 · R3：L030 BUG 修复 + F1-residual 收口轮（2026-09-28）

轮次目标：修复 L030 判定的两条 maven BUG（BIN-16 snapshotVersions 条件合并 / D-4 GAV 失配 409）、
收口 F1-residual（`<K>-cache` REST 面 3-leg）、台账批登记（L030 两 BUG + T-538 残差）、
npm facet 过滤差分臂（L031），并处置轮首发现的 main CI 第二处 flake。

## PR 与合并记录

| PR | 内容 | 状态 |
|---|---|---|
| [#163](https://github.com/0ldlight/binflow/pull/163) | R3 载荷（6 提交：5 票 + 双审 blocking 修复）→ develop | MERGED 10:38:25Z |
| [#164](https://github.com/0ldlight/binflow/pull/164) | develop→main 定期保鲜（即时正当：T-546 flake 修复须进 main 收口 CI 红灯） | MERGED 10:39:51Z |

双审：Reviewer A（correctness）**APPROVE** 0 blocking / 6 non-blocking；
Reviewer B（architecture）初判 **REQUEST_CHANGES** 2 blocking → 修复 3b58b082 → Reviewer B'（architecture，
全新实例）复核 **APPROVE**——含阴性对照实证（新测试借调到修复前代码 f21f1c09 上恰在 blocking 1 断言失败，
缺陷复现；修复后 maven 包 ok 18.160s、replication ok 18.510s 独立复跑）
（reports/agents/R3-pr-review-a.md / -b.md）。合并门=双 APPROVE + 本地哨兵（策略：PR 无 CI）。

**blocking 修复内容**（3b58b082）：① T-542 成员面 sidecar 失配——剥离后 body 与 `.sha1`/`.md5`
摘要自相矛盾，抽 `strippedSnapshotMetadata` helper，sidecar 分支同谓词走 `writeDerivedSidecar`
（virtual 面同姿势），测试补双向配对一致腿；② T-546 补 15 字段工作日志。

## 完成票（Linear 状态已同步）

| 票 | Linear | 内容 | 提交 |
|---|---|---|---|
| T-540 | BIN-23 ✅Done | F1-residual：`<K>-cache` REST 面 3-leg（cached-rows-only 零上游 / miss 404 零上游 / 无 T-448 回归控制腿） | 9c83678d |
| T-542 | BIN-25 ✅Done | maven snapshotVersions UA 条件合并（修 BIN-16 BUG；java-agent UA 剥离 / M3-capable 合并） | f21f1c09 |
| T-543 | BIN-26 ✅Done | maven GAV 失配 409 + suppressPomConsistencyChecks 执行点接活（修 D-4 BUG；错误体与 A 面逐字节一致） | f21f1c09 |
| T-544 | BIN-27 ✅Done | known-divergence 台账批 +6（L030 两 BUG fix-in-flight + T-538 残差四条） | 93aa3573 |
| T-545 | BIN-28 ✅Done | L031 npm facet filter 差分臂（A 面双端对照 + 3 case） | df236376 |
| T-546 | （无票，conductor 直改） | main CI run 36392573067 flake：replication 审计断言 TOCTOU，waitAudit 轮询 helper | 51c1a1c4 |

## L030 修复验收（T-542/T-543）

- **BIN-16 修复后**：L030 case1 双端复跑 **PASS（连续两轮）**——java-agent UA 下 virtual/member 两面
  `<snapshotVersions>` 剥离、`<snapshot>` 块保留；M3-capable UA 合并；真实 Apache Maven 3.9.16 验收。
- **D-4 修复后**：L030 case2 双端复跑 **PASS**——GAV 失配 PUT 409 + errors[]（错误体与 A 面活体捕获
  逐字节一致，含 "…valid Maven repository root path." 尾）；拒绝路径零落盘（GET 404）；
  `suppressPomConsistencyChecks` 死配置接活（默认 false=校验开，true → 201）。
- **评审加固**（3b58b082）：成员面 sidecar 派生摘要配对一致（双向断言）。

## L031 差分结论（reports/compatibility/L031-npm-v2-facet-filter.md）

- 一个根 BUG 候选：virtual packument `dist-tags.latest` 语义——上游 latest 指向低于 max 版本时，
  A 面保留 base 成员 tag（1.0.0），B 面重算为 max（2.0.0）；低severity；r2≡r3 稳定、双客户端一致。
  → R4 裁定票（npm adapter）。
- T-538 FacetCache 去重外部对齐确认。
- A 面语义记录：`<K>-cache` 在 Artifactory 非 first-class repo key（双端一致 400）；
  A 腿 remote 上游必须走 8081 直连端口（router 8082 self-reference 404）。

## 诚实失败与教训

- httpapi 六包并行 603s 超时（T-540/T-542 报告自述）——负载 flake，单包复跑 ok（237.7s），如实留档。
- Reviewer B 抓到 T-542 引入的 body/sidecar 配对失配（A 面未取证 ≠ B 面可自相矛盾）——
  双审制度本轮直接拦下一个真 BUG；blocking 2（T-546 日志缺失）是流程硬门兑现。

## 四问（Compatibility 四问）

- **X（矩阵行）**= 201（本轮无新行；L031 属证据强化非新面）
- **Y**= 109.5（compatible + 0.5×partial + 0.5×superset，不变）
- **coverage**= 60.50%（109.5/181）
- **Z（open 已知偏离）**= **44**（+6：T-544 台账批登记；总 79 = 31 resolved + 4 gated + 44 open
  —— BUG 9 / UNKNOWN 33 / INTENTIONAL 1 / UNSUPPORTED 1；python yaml 复算核对）
- 口径注：Z 上升仍是登记纪律产物；其中两条 BUG（BIN-16/D-4）本轮已修复且双端复验翻绿，
  resolved 回填归 compatibility-engineer 票（R4 池）。

## 哨兵证据（收编后全量）

- `go build ./...` exit=0；vet/gofmt/golangci-lint 四域零告警
- `go test ./internal/adapter/maven/ -count=1` ok（18.860s 修复前 / 17.994s blocking 修复后）
- web/ `npx tsc --noEmit` exit=0（本轮 web/ 零触碰，例行全跑）
- 凭据扫描：全部提交物干净（admin/password 只存在于会话 env / /tmp/r3-difftest.env chmod 600；
  双审 A/B 独立凭据硬检查通过）

## R4 候选池

- **裁定/形式化**：L031 `dist-tags.latest` BUG 裁定票（npm adapter）；§5.1 UA 判定族 + 
  mvn.metadata.version3.enabled 开关契约形式化（compatibility-engineer）；BIN-16/D-4 台账 resolved 回填
  （复验条件已满足）；L031 A 面 member sidecar 取证差分臂（T-542 Next）。
- **架构**：201 envelope URI/downloadUri/Location 缺 `/binflow` 前缀——跨 adapter external-base seam
  （architect 票：requestBase 只吃 scheme://host，前缀被 httpapi router 剥掉）；
  agent-graph.yaml 补 internal/replication owns 条目（conductor）；
  派生成员面 sidecar 无 Last-Modified/If-Modified-Since 304（与 virtual 面同姿势，随 §5.1 形式化两面一起收——
  Reviewer B' non-blocking ①）。
- **携带**：BIN-24（T-541 F8-widened）、T-536 五条差分随访臂、F1 generic-package wire 测试、
  isSnapshotResolutionPath 臂、T-525/BIN-12、MySQL BIN-13、B3 nightly + uat_approval（待用户）、
  main CI 转绿核验（R3 保鲜合并后首个 main run）。

## 用户待办（无变化）

B3 CircleCI nightly trigger（03:17 UTC, main）；部署轮次时 `uat_approval` 点击；T-473。
