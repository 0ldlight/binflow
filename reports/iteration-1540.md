# Iteration 1540 · R1 BIN-M1 Maven 业务闭环收官轮战报

- **日期**: 2026-09-28
- **性质**: 测量轮（首个全凭据差分闭环轮）
- **上下文**: 用户「开始下一轮」；R0（iteration-1539）后凭据到位（B1 双端凭据经环境变量注入、B2 Linear MCP）→ R1 四实现票 + 差分复验 + 四问首次重计。

## 一、本轮完成

### PR #156（develop，2026-09-28T02:35Z 合并）— 四桶实现 + en 棘轮

| 票（Linear） | 内容 | 状态 |
|---|---|---|
| T-529（BIN-6） | internal/remote 投影派生注册表（`<K>-cache` 派生仓注册/生命周期） | done |
| T-530（BIN-7） | virtual 四桶 walk + F1 `<K>-cache` 直访 + DELETE 404（D-2 语义） | done |
| T-531（BIN-8） | internal/adapter/maven 聚合 Facet 消费（cache 步跳过） | done |
| T-526（BIN-9） | Fern en 页数只升不降门（双 CI 腿，baseline=10） | done |
| T-534（BIN-17） | adapter 域陈旧 405 断言翻新（评审 B 爆炸半径发现） | done |
| T-535（BIN-18） | DISCOVERY：四桶 walk 三缝隙 N1/N3/N4（→F2/F4/F8 缝票池） | 登记（Backlog） |

- **评审**: 关键域 A/B 双实例——T-526/T-529/T-531 A+B、T-530 A+B(b2，T-534+replication 翻新后翻 APPROVE)；helmoci 范围外翻新经双评审确认类支配原则接受（ADR-0051）。conductor 未评审自己实现的代码；replication D-2 翻新（conductor 直改）由 B2 报告背书。
- **哨兵**: 全树 `go build` + golangci-lint + 全包 `go test` exit=0（收编时点，27 包全 ok）。

### 差分复验 L029（D-1/D-2 双关闭）

- **D-2 关闭**（r5）：DELETE 经 virtual = 404 ITEM_NOT_FOUND + 成员存活，双面一致。
- **D-1 关闭**（r6/r7 稳定对）：首拉 200+上游 sha256 逐字节一致、`<K>-cache` 投影 200、二次拉取 200。
- **L028 归因修正（诚实记录）**: L028 把首拉 400 归因为「virtual→remote 解析未实现」——r5 复验戳破：400 体真相是 **SSRF 防护（NFR-S13）拒绝 clash TUN fake-ip DNS 解析出的私网 ULA 上游**（fdfe:dcba:9876::22，400 体 sha 与 L028 逐字节一致，L028 时即是此因）。四桶 walk 本身单测早已证明；F1 投影缺失（404）才是真产品缺口。处置：仓库级 `allowPrivateUpstream` 豁免（产品一等旋钮，admin 授予+审计），case 仅 B 腿固化并注明环境原因——**产品默认安全姿态不变，非放宽容差**。
- 全批次 5/5 PASS（r6=r7 稳定判据满足）。证据：`tools/difftest/v2/run/maven-batch1-r{5,6,7}/`。

### 矩阵落账（本批随收官 PR）

- 新行 **D12-R18**（Maven virtual 内容面）→ compatible（conductor 派票授权，L005-3/L007-4 先例款）。
- **BIN-5（BIN-M1 业务闭环父票）→ Done**（Linear，2026-09-28T02:54Z）。

## 二、必答四问（首次全凭据重计）

```
Artifactory observable surface = X：201（matrix.yaml 行数；本轮 +1 新行 D12-R18）
BinFlow matched               = Y：109.5（compatible 95 + 0.5×partial 17 + 0.5×superset 12；上轮基线 108.5）
Known divergence              = Z：33 unresolved（台账 68 条：31 resolved + 4 gated-resolved；BUG 4 / INTENTIONAL 1 / UNSUPPORTED 1 / UNKNOWN 27——本轮零变更，D-3/D-4 未入账）
Unknown                       = N：27（= Z 中 UNKNOWN，限期升级跟踪）
Compatibility Coverage        = Y / (X − ⛔) = 109.5 / 181 = 60.50%（上轮 108.5/180 = 60.28%，+0.22pp）
```

口径：X 认证基准 7.161.26 E+ 活体差分（A 面经 loopback relay）；存量 200 行多按 7.161.15 认证，翻绿按新基准复验后逐行推进。

## 三、Gap 增减

- **修复差异 2**: D-1（virtual→remote 解析+cache 投影，F1 真缺口）、D-2（DELETE 经 virtual 405→404）——双线上关闭，矩阵落账。
- **新回归**: 无（全树哨兵 exit=0；r6/r7 稳定）。
- **新登记 open item 1**: BIN-18（N1/N3/N4 三缝隙，P2——评审 A 范围外发现收编）。
- 顺带闭环：T-524（BIN-11，virtual DELETE 裁定票）随 D-2 线上证据自然关闭（上窗口已 Done）。

## 四、本轮失败与返工（诚实清单）

1. **adapter 域评审爆炸半径**：T-530 四桶改动炸出 `internal/adapter` 陈旧 405 断言两处 → 派 T-534 翻新（Reviewer B 发现）。
2. **replication 域同族失败**：`engine_integration_test.go`/`t317_two_instance_test.go` 旧「PUT/DELETE 皆 405」断言未随 D-2 翻新 → conductor 定向直改+复跑 ok 1.811s。教训：语义翻转票必须 grep 全 virtual-DELETE 断言面，不只 area 包。
3. **D-1 误诊一轮**：r5 仍 FAIL 后才取回 400 体真相（SSRF×环境），浪费一轮；下轮起差分 FAIL 先取错误体再归因。
4. **报告文件名返工**：PR-152→PR-156 评审报告改名三遍才清干净（grep 截断掩盖残留）。

## 五、下一步（R2 候选池，按优先级）

1. D-3 → known-divergence 四分类登记 + D-4 反向用例（compatibility-engineer）。
2. BIN-15（T-532 replication probe `-cache` 硬编码）/ BIN-16（T-533 snapshotVersions 差分臂）。
3. BIN-18（N1/N3/N4 → F2/F4/F8 缝票统一收口）。
4. F1 generic-package 线用例补差分臂；设计 §5.1-实现勘误（architect）。
5. B3 CircleCI nightly + uat_approval（用户侧待办）；MySQL 镜像（BIN-13 阻塞腿）。
6. develop→main 定期合并（上次 2026-09-27T14:19；≥1 天触发已满足，随收官 PR 后执行，CI 44m 腿承受）。

## 六、证据索引

- PR #156: https://github.com/0ldlight/binflow/pull/156（merge 2026-09-28T02:35Z）
- 差分: `reports/compatibility/L028-maven-v2-batch1-diff.md` → `L029-maven-v2-batch1-reverify.md`；run 目录 `tools/difftest/v2/run/maven-batch1-r{3,4,5,6,7}/`
- 矩阵: `docs/compatibility/matrix.yaml`（D12-R18 + summary 201/95/51 + header L029-1 注记）
- 票日志: `reports/agents/T-52{6,9}.md`、`T-53{0,1,4,5}.md`；评审 `PR-156-review-*.md` ×8
- Linear: BIN-5/6/7/8/9/10/11/14/17 Done；BIN-13 In Progress（MySQL）；BIN-12/15/16/18 Backlog
