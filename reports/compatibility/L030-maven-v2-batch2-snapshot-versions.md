# L030 — difftest v2 Maven 批次二：snapshotVersions 条件合并（BIN-16）+ D-4 POM GAV 反向用例

- 日期：2026-09-28（R2 · iteration-1541 · T-533 / Linear BIN-16）
- 执行人：differential-qa-engineer
- 模式：**dual**（A 面全程可达，无降级）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ 192.168.120.38:8082，经环回 relay 127.0.0.1:18099（/tmp/a-relay.py，stdlib TCP 转发；版本活体复验：GET /api/system/version → "7.161.26"）
- B 面（被测）：BinFlow 本地构建 @ 127.0.0.1:18080（worktree `claude/r2-binm1-residuals` @ 04847e27 `go build -o /tmp/r2-b-bin/binflow-server ./cmd/binflow-server` 一次性构建，绝对路径运行；BINFLOW_HOME=/tmp/r2-bhome-l030 全新 scratch；`/binflow` 前缀；凭据经环境变量注入，未落盘）
- 框架：`tools/difftest/v2`（python3 stdlib-only），新 case 2 个，复用 `_mavenlib`
- 客户端：真实 `mvn deploy:deploy-file`（Apache Maven 3.9.16）构造，runner stdlib HTTP 腿做元数据读取（UA 可控）
- 证据（宿主本地，不入库）：`tools/difftest/v2/run/l030-r1/`、`run/l030-r2/`、`run/l030-r3/`
- 环境清理：两面 difftest- 命名空间收尾为空（A/B `GET /api/repositories` 过滤 `difftest-*` = `[]`）；B 进程与 relay 已停，18080/18099 已释放

## 轮次与稳定性

- r1：两 case 均 FAIL（证据成立）；其中 `maven-snapshot-versions-merge` 的 `virt_m3cap_exts` 断言出现 case 侧 slug 笔误（期望写 `pom+jar`，sorted() 实际 `jar+pom`——语义相同，纯拼写），修正后重跑。
- r2 → r3：**连续两轮逐 case (status, reason) 完全一致**（见下），稳定性判据满足。结论以 r2/r3 为准。

## Case 1 — maven-snapshot-versions-merge（BIN-16 主臂）：FAIL（B 违例）

构造：virtual `difftest-mvn-svv(L1, L2)`（声明序 L1 为基底）；真实 mvn deploy:deploy-file 部署 `com.diff:sv-merge-lib:1.0.0-SNAPSHOT`——L1 一次（buildNumber 1）、L2 两次（buildNumber 2，严格更新）。三腿读取 version-level `maven-metadata.xml`：M3-capable UA（`Apache-Maven/3.9.16 (Java 17.0.11; Mac OS X 15.0)`）、java-agent UA（`Java/1.8.0_391`，正中 Artifactory `[Jj]ava/(.+)` 全匹配）、以及成员仓控制腿。

### 裁定问题：A 面合并是条件性的吗？——**是（live 确认），规格 §5.1 无需勘误**

| 腿 | A 面（7.161.26 实测） | B 面（当前树实测） | 对照 |
|---|---|---|---|
| 构造（6 次 mvn deploy，每面 3 次） | 全部 exit 0；成员 bn L1=1 / L2=2 | 全部 exit 0；成员 bn L1=1 / L2=2 | 一致 |
| virtual GET，M3-capable UA | 200；`<snapshot>` bn=2（L2，取较大）；`<snapshotVersions>` 2 条（jar+pom），值=L2（较新） | 200；**完全同形**（bn=2、2 条、值=L2） | 一致 |
| virtual GET，java-agent UA | 200；`<snapshotVersions>` **0 条（剥除）**；`<snapshot>` bn=2 保留 | 200；`<snapshotVersions>` **2 条照发**（与 capable 腿同体） | **差异** |
| 成员仓 GET（L1），java-agent UA | 200；`<snapshotVersions>` **0 条（剥除）**；`<snapshot>` bn=1 保留 | 200；2 条照发 | **差异** |

wire 证据（r3，r2 同形）：
- A capable 腿 merged：`snapshot{buildNumber=2, timestamp=<T2>}`，sv 值 `1.0.0-<T2>-2`（jar/pom 各一）；A java-agent 腿：sv_count=0，`snapshot` 块存活。
- B 两腿 merged 体逐字节同形（sv_count=2），UA 无任何影响。
- 成员时间戳侧相对（A：T1=20260928.070853/T2=070901；B：T1=070908/T2=070913——断言值设计为成员相对量，跨面时间差不构成伪差异）。

### 判定与处置建议

- **四态：FAIL**（b_spec_violations：`virt_m3no_sv`、`member_m3no_sv` 均 `present`，期望 `stripped`）。
- **分类建议：BUG**。A 面 live 行为与规格 §5.1 逐字吻合（条件合并：`mvn.metadata.version3.enabled` 默认开 + 快照级路径 + 客户端 M3 能力谓词，否则剥除 `<snapshotVersions>`）——**规格正确，非勘误候选**；BinFlow `internal/adapter/maven` 的 `mergeSnapshotVersioning` 无条件并入且成员仓服务路径同样无 M3 谓词，属违例。同一根因两个暴露面：virtual 合并腿 + local 成员腿。
- 修复面（供下轮派票参考，本轮未动产品码）：`clientSupportsM3SnapshotVersions` 谓词（UA 判定：空 UA=支持；Ivy/Wharf 与 `[Jj]ava/(.+)` 全匹配=不支持）需落在 virtual 合并与 local 元数据 GET 两个服务路径；系统开关形态是否对齐 `mvn.metadata.version3.enabled`（默认 true）由 compatibility-engineer 定契约。
- capable 腿双面完全一致（合并算法本体——bn 大者胜、per ext+classifier 取新——已对齐）：**维持正向，无回归**。

## Case 2 — maven-deploy-gav-mismatch（D-4 反向用例）：FAIL（B 违例）

构造：virtual `difftest-mvn-gavv(L, defaultDeploymentRepo=L)`；POM 内容 GAV=`com.diff:right-lib:1.0.0` vs 部署路径 `com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom`；三腿：一致控制 PUT / 不一致 PUT / 真实 mvn deploy-file（`-DartifactId=wrong-lib` + `-DpomFile`=right-lib 内容）。

| 腿 | A 面 | B 面 | 对照 |
|---|---|---|---|
| 一致控制 PUT | 201 | 201 | 一致（无误报） |
| 不一致 PUT | **409**；错误体 `errors[]` 形；路径 GET 404（未落盘） | **201** created；路径 GET 200（已落盘存储） | **差异** |
| mvn deploy-file（GAV 不一致） | **exit 1**（tail：`409 Conflict`）；路径 GET 404 | **exit 0**；路径 GET 200 | **差异** |

wire 证据（r3）：
- A 409 体形态：`{"errors":[{"status":409,"message":"The target deployment path 'com/diff/wrong-lib/1.0.0/wrong-lib-1.0.0.pom' does not match the POM's expected path prefix 'com/diff/right-lib/1.0.0'. Please verify your POM content for c…"}]}`，body sha256 `278e10beabc3d90ea5f797a6fe80d1a39d8eef376a6f4cb301030fce5d63247b`。
- B 201 体形态：created-envelope（`uri`/`downloadUri`/`repo`…），body sha256 `e1d4890528d65aec373480867c2023f26b3b347ec4900f07383b632185ac0d2a`。
- 规格锚：docs/reverse/virtual-resolution.md §5.2 上传侧注——经 virtual 落 defaultDeploymentRepo 的 pom 做「pom 坐标 ↔ 目标路径一致性校验，不一致按 suppressPomConsistencyChecks 决定拒绝与否」；D-4（L029 登记）的 A 面 409 本轮复验成立。

### 判定与处置建议

- **四态：FAIL**（b_spec_violations：`put_mismatch_status`=201、`put_mismatch_get`=200、`mvn_mismatch_exit`=zero、`mvn_mismatch_path_get`=200）。
- **分类建议：BUG**。BinFlow 在 repo 配置投影面渲染了 `suppressPomConsistencyChecks` 旋钮（internal/httpapi/repo_config_render.go，默认 false）但 internal/ 无任何执行点——「宣称了旋钮却无校验」按 BUG 计；D-4 从「反向用例候选」升格为已取证的 B 面缺口。按票约束只登记不修。

## 批次汇总

| case | L029 基线 | L030 | 差异分类建议 |
|---|---|---|---|
| maven-snapshot-versions-merge | （新） | FAIL | BUG（BIN-16 主臂，B 违例 §5.1） |
| maven-deploy-gav-mismatch | （新，D-4 候选） | FAIL | BUG（D-4，上传侧校验缺失） |

- 差异分类建议：BUG 2 / UNSUPPORTED 0 / INTENTIONAL 0（终局裁定权在 compatibility-engineer/conductor）/ UNKNOWN 0。
- 回归对照：L029 五 case 本批次未复跑（票面范围=两臂；其结论由 L029 r6/r7 稳定对背书）；两臂均为新增差异，无「仍在/恶化」项。
- 提金候选：A 面 capable 腿 merged 输出可作 golden 候选（conditional-merge 正例）——但含侧相对时间戳，入金前需 compatibility-engineer 定义 normalize（时间戳占位）后再评，本轮仅提案。

## 顺带观察（未裁定，移交后续批次）

1. B 面 201 created-envelope 的 `uri`/`downloadUri` 渲染为 `http://127.0.0.1:18080/<repo>/...`，缺 `/binflow` 产品前缀（A 面对应体带 `/artifactory` 前缀）——响应体自指形态差异，非本票断言面。
2. B 面对「不存在目录的裸 GET」（`/difftest-mvn-gavl/com/diff/right-lib`，无文件名）回 400，A 回 404——目录级 GET 语义，后续 L2/L3 批次可立案。

## 遗留与建议处置

1. **BIN-16 主臂**：规格 §5.1 维持（无需勘误）；BinFlow M3 谓词缺失 → 建议下轮派产品修复票（virtual 合并腿 + local 成员腿两处），修后以本 case 复验翻绿。
2. **D-4**：B 面上传侧 pom 一致性校验未实现（旋钮已渲染无执行）→ 建议并入同一修复票或独立票；known-divergence.yaml 落账归 T-536/compatibility-engineer 流程，本轮未动。
3. matrix.yaml 落账、known-divergence 登记、契约翻态：归 conductor 评审后处理（本报告即证据源）。
