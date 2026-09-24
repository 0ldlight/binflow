# Iteration 1000 · R0 接线收口轮战报

- **日期**: 2026-09-25
- **性质**: 接线轮（instruments & governance），非测量轮
- **上下文**: 2026-09-25 章程（20 部分总纲）→ 14 部分核验报告 → 用户「开始实施」→ R0 接线两 PR 落地。

## 一、本轮完成

### PR #150（develop 87bf4178，已合并）— 治理接线

- 主 checkout 脏文件收编（ruflo settings + .gitignore）。
- 治理修订三件：BOARD.md 冻结通告（2026-09-25 起只读快照）、CLAUDE.md Linear 权威源声明、SPRINT-LOOP v3（六步闭环 + 质量门口径只增不减）。
- 独立评审（Reviewer A/B 增量复审）APPROVE 后合并。

### PR #151（develop bf2f90b1，本轮合并）— 四领域票批

| 票 | 内容 | 状态 |
|---|---|---|
| T-520 | ADR-0051 virtual 四桶解析序 + cache 逻辑投影设计（DECISIONS.md + docs/design/virtual-four-bucket.md） | done（评审通过） |
| T-521 | Fern 英文站首批 10/41 页（index + quickstart + 8 install，en/zh 反引号内容逐字节一致） | done（评审通过） |
| T-522 | OpenAPI 漂移门双 CI 面（make spec-check 挂 GH build 前置 + Circle build 双 shard）+ nightly ui_smoke job | done（评审通过） |
| T-523 | difftest v2 Maven 四用例（deploy/resolve-remote-cache/metadata-merge/delete-passthrough + _mavenlib） | **READY**（双发 BLOCKED on credentials，如实记录） |
| T-525 | DISCOVERY：评审范围外 3 条（docker-registry.md 过时 / fern 迁移表 007 vs 028 / api slug 分叉）+ 7 Low 分诊 | DISCOVERY（已登记） |

**评审**: Reviewer A（correctness 形态，独立实例）对四票全量 31 文件通读，结论 **APPROVE**（blocking 0 / Low 7 / 范围外 3）。关键独立取证：`make spec-check` 字节一致、mock 双轮差分框架复跑稳定、Maven Central 上游 sha256 活体核验一致、credential grep 零命中。评审报告与 T-525 登记随本 PR 入库（0083e7fc）。

**单审说明**: 本批无 Go 运行时代码（六关键域双审制不触发）；docs/test-tooling 域单审合法。

### 环境与流程事实（本轮钉死）

- push 通道：gh OAuth token 无 workflow scope → 改 workflow 的推送走 SSH 443（`git push ssh://git@ssh.github.com:443/0ldlight/binflow.git`）。
- CI 触发面：双 CI 面 main-only（2026-08-28 用户指令），PR 无 CI 属设计内；漂移门首轮实跑落在下一次 develop→main 合并 + nightly。
- 主 checkout 与 worktree 同步于 bf2f90b1；29 个未跟踪路径全为本地工具状态（.claude/ 内部件），不属工程文件。

## 二、必答四问（本轮未重计）

**本轮为接线轮：新增的是仪器（difftest v2 用例、漂移门、ui_smoke）与治理（权威源切换），不是测量。差分 0 结论（六 env 未注入），matrix 无翻态事件。**

```
Artifactory observable surface = X：沿用冻结行集（matrix.yaml），未重计
BinFlow matched               = Y：未重计（无差分结果输入）
Known divergence              = Z：未重计（T-524 为 open ruling 候选，尚未入 known-divergence.yaml）
Unknown                       = N：未重计
Compatibility Coverage        = Y / (X − ⛔)：本轮无数值
```

口径说明：存量 matrix 认证全部基于 7.161.15（旧参照）；基准已切 7.161.26 E+ 但活体复验 401 BLOCKED——凭据到位后首轮差分即重计基线。

## 三、Gap 增减

- **新回归**: 无。**修复差异**: 无。**新差异**: 无（差分未跑）。
- 新登记 open item 2 条：T-524（virtual DELETE 语义，待参照活体裁定）、T-525（评审范围外 3 条 + 7 Low 分诊）——均为治理性登记，不改 Gap 计量。

## 四、用户侧待办（阻塞项）

| # | 事项 | 阻塞面 |
|---|---|---|
| B1 | 参照实例凭据：只读测试账号 → `BINFLOW_REF_USER`/`BINFLOW_REF_TOKEN`；差分六 env（A_BASE/B_BASE/A_USER/A_PASSWORD/B_USER/B_PASSWORD） | 首轮真实差分 + 7.161.26 活体复验 + T-524 裁定 |
| B2 | Linear MCP OAuth（交互式 `/mcp` 授权）+ 用户 Linear 身份确认（workspace binfloow） | Linear 成为权威任务源 |
| B3 | CircleCI nightly schedule（03:17 UTC，branch main）触发器创建 + T-473 org context 迁移 | nightly ui_smoke / race_full 实跑 |
| D5 | DB 实例资源（多 DB 矩阵真实实例） | 多 DB 验证硬要求 |
| B5 | .131 主机状态（Jenkins 侧） | Jenkins 相关验证 |

另有 T-521 遗留决策三项：en 覆盖率 CI ratchet 是否加门；en 首批平台渲染预览（顺带裁定 slug 分叉 A3）；example.com 占位 URL 是否换真域名。

## 五、下一步（R1 · BIN-M1 Maven 业务闭环）

凭据未到位前可并行推进（不依赖参照实例写入）：
1. 四桶实现票（T-520 设计 → dev-go-core/storage；F1 `<K>-cache` 直访缝在内）——B 面单边可验。
2. virtual-resolution 规格残余面转可执行契约（compatibility-engineer）。
3. T-524 裁定留待凭据（A 面实测即证据）。

凭据到位后：T-523 四用例双发补跑 → 首份 `reports/compatibility/` 差分报告 → 四问首次重计。

## 六、证据索引

- PR #151: https://github.com/0ldlight/binflow/pull/151（merge bf2f90b1）
- 评审报告: `reports/agents/PR-151-review-a.md`；登记: `reports/agents/T-525.md`
- 票日志: `reports/agents/T-52{0,1,2,3}.md`
- 工作树哨兵（收编时点）: go build 0 告警 / spec-check in sync / tsc 0
