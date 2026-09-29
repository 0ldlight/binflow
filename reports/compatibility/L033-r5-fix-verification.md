# L033 — T-554 R5 三修复双端复验 + handle-seat 臂补跑

- 日期：2026-09-29（R5 · iteration-1544 · T-554 / Linear BIN-36）
- 执行人：differential-qa-engineer
- 模式：**dual**（A 面全程可达，无降级）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ 192.168.120.38:8082（context root /artifactory，本轮 `api/system/version` 活体复验）
- B 面（被测）：**worktree 现态构建**（含 T-551/T-553 未提交修复 + T-541 已合入 walk 层）`bin/binflow-server` @ 127.0.0.1:18248（BINFLOW_HOME=/tmp/r5-bhome-l033 全新 scratch，`/binflow` 前缀；凭据 env 注入零落盘；批末已停、home 已删）
- 框架：`tools/difftest/v2`（python3 stdlib-only）；新 case 1 个（`maven-local-handle-walk-skip`）
- 证据：`tools/difftest/v2/run/l033-r5-r{1,2,3,4}/`（每 case `evidence/<case>/{a,b}-leg.json + summary.json`）
- 环境清理：两面 `GET /api/repositories` 过滤 `difftest-*` = `[]` 各核验一次；B 进程已 kill（18248 释放）、scratch home 已删、/tmp 一次性探针脚本已清

## 轮次与稳定性

| 臂 | case | 轮次 | 稳定性 |
|---|---|---|---|
| Arm A sidecar | maven-member-snapshot-sidecar-304 | r1, r2, r3 | **PASS×3，r1==r2==r3 断言值逐项相同** |
| Arm B deploy 拒绝族 | rest-kcache-deploy-404-wording | r1, r2, r3 | **PASS×3，r1==r2==r3 断言值逐项相同** |
| 席位探针 | maven-module-handle-seat-probe | r1–r4 | BLOCKED/NOT_RUN×4（a=false, b=true 恒定，前置未变） |
| Arm C walk/merge | maven-local-handle-walk-skip（新） | r3, r4（定形版） | **FAIL 稳定，r3==r4 断言值+分歧键集 0 差异**（22 维：11 一致 / 11 分歧） |

r1 Arm C 为探形版（无对照腿，9 分歧维）；r2 Arm C 因 case 缺成功路径清理腿 BLOCKED（case 侧 bug，已修——`finally` 清理补齐；其异常分支已顺手清掉 r1 双面残留，r2 前后双面核验均空），不计入结论。A/B 两臂 r1 起即为定形 case，三轮有效。

## Arm A — T-551 maven sidecar validator 族：**PASS（翻绿确认，17/17 维双端一致）**

构造同 L032 Arm 6（mvn deploy SNAPSHOT 每面，A 经 forward-proxy / B 直连，取证 GET 双面直连；java-agent `Java/1.8.0_391` + capable `Apache-Maven/3.9.16` 双 UA）。

L032 三处分歧维本轮实值（r3 evidence）：

| 维度 | A 面 | B 面 | L032 → L033 |
|---|---|---|---|
| `.sha1`（java-agent）Last-Modified | present（`Tue, 29 Sep 2026 00:40:06 GMT`） | **present**（`00:40:09 GMT`） | absent → **present 翻绿** |
| `.sha1`（java-agent）IMS→ | 304 | **304** | 200 → **304 翻绿** |
| `.sha1`（java-agent）ETag/INM | no-etag | **no-etag** | ETag=digest+INM→304 → **no-etag 翻绿** |

维持面全绿无回归：digest 内容契约（`.sha1/.md5/.sha256` = 所服务体摘要 match×3、capable 控制腿 match、`.sha512` 404 双面同）、body 面（ETag present、LM present、IMS→304、IMS 旧值→200、INM→304、X-Checksum-* 三头族）、java-agent 剥离/capable 合并两 UA 面。`judge()` mismatches 三桶全空（r1/r2/r3 同）。

### 附带取证 — 跨秒 LM 语义差（T-551 Risks 声明的语义差，live 坐实并圈定作用面）

17 维矩阵的 IMS 腿是同秒回访（不咬语义差）。补做跨秒（sleep 1.3s）回访取证（B 面 member 版本级 metadata `.sha1`，两种 UA；A 面等价构造因 wire-PUT snapshot 在 A 上 30s 内不物化版本级 metadata（404）不可达，A 侧结论由 L032/r1-r3 的物化稳定戳机制推出——LM 恒等于部署物化时刻且跨 GET 序列不变）：

| 面 | B 首答 LM | 跨秒 IMS 回访 | LM 前移 |
|---|---|---|---|
| capable UA（物化存储面） | 00:44:07 | **304** | 否（stable） |
| java-agent 剥离面（**真派生面**） | 00:44:09 | **200** + 新 LM 00:44:10 | **是** |

结论：per-request 派生时刻戳的语义差**真实存在且精确圈定在 java-agent 剥离面**——跨秒持旧 LM 的客户端在 B 得 200+body（A 为 304 空体）；同秒回访（矩阵腿）双面同为 304。capable 面走物化存储节点不受影响。**这是语义差不是断言错**（票面预声明）；影响=剥离面客户端缓存效率，建议登记待裁 + 可选 stored-stamp 升级微票（T-551 Risks 已预留升级点）。

## Arm B — T-553 deploy 拒绝族：**PASS（三腿逐字对齐，翻绿确认）**

构造同 L032 Arm 2（`difftest-r4t550w-{loc,virt,rem}`，GAV 一致 pom）。r3 双面实值（文案逐字，A/B message 字段 JSON 解析后 byte 相同）：

| 腿 | A 面 | B 面 | L032 → L033 |
|---|---|---|---|
| PUT `/<K>-cache/<gav>.pom` | 404 `Could not find a local repository named difftest-r4t550w-rem-cache to deploy to.` | 同 status 同文案（errors[] envelope） | 措辞族分歧 → **逐字对齐** |
| PUT `/<K>/<gav>.pom`（remote 本体） | 404 `Could not find a local repository named difftest-r4t550w-rem to deploy to.` | 同 status 同文案 | **405→404 + 文案族双翻** |
| PUT `/<virt>/<gav>.pom` | 405 + `Allow: GET` + §8.2 固定文案 | 同（逐字） | 一致维持（无回归） |

T-553 的作用域圈定（仅 PUT+maven 包型 remote/-cache）本轮随腿复验：virt 405 腿不受影响；generic 面行为由 T-553 单测钉住未动（本轮未开新 generic 腿，非本票范围）。

## 席位探针 — remote 面：BLOCKED/NOT_RUN（前置未变，如实）

`maven-module-handle-seat-probe` r1–r4 恒 `a=false, b=true`：A 持久化 remote handle* 席位；BinFlow remote canonical 仍不持久化（B 键在、值恒 true）。remote 面模块级臂维持 NOT_RUN by precondition——**但 Arm C 经 local 面把同一问题差分化了**（见下）。

## Arm C — T-541 handle walk 层（local 面，新 case `maven-local-handle-walk-skip`）：FAIL（11 分歧维，r3≡r4 稳定）——**ledger 漂移项的差分定谳输入**

local 席位双面持久化（seat echo `handleReleases=false`/`handleSnapshots=false` 双面 false），构造：virtual [hr(handleReleases=false), hs(handleSnapshots=false), ctl(default)] + wire-PUT（hr←SNAPSHOT hwm / hs←release 拒 / ctl←release hwc + plainsnap SNAPSHOT 对照）。dual-oracle（expected=live a，22 维）。

**先决发现（A 面手工探针，探后即清）**：A 对 hr=false local 的 **release PUT 直接 409**，且对未落地 release 路径的 **直接 GET 也 409**（路径类×策略是成员面读写双门）；B 侧 put.go ME-08 同拒 PUT——「release 制品栖身 hr=false 成员」双面均构造不出，该子面 **NOT_RUN by construction**（矩阵内如实记录两条 409 腿）。

| 维（22） | A 面（live） | B 面（live） | 对照 |
|---|---|---|---|
| seat echo ×2 | false / false | false / false | 一致（local 席位双面在） |
| 拒绝腿 status ×2（rel→hr、snap→hs） | 409 / 409 | 409 / 409 | 一致（部署门双面在） |
| member_hr_modulemeta（hwm 模块清单，成员直读） | 1.0.0-SNAPSHOT | 1.0.0-SNAPSHOT | 一致（成员面不受 handle 影响） |
| virt 版本级 snapshot metadata（§5.1 面） | 200，sv=1 | 200，sv=1 | 一致（跳过键=handleSnapshots，hr 保留快照→双面并入） |
| virt resolve release（ctl 成员）+ 模块清单（hwc） | 200 / 1.0.0 | 200 / 1.0.0 | 一致（控制组） |
| **virt 模块级清单（hwm，hr=false 成员的 SNAPSHOT 版本）** | **200，versions=1.0.0-SNAPSHOT（并入）** | **404（整个成员被丢弃）** | **分歧——THE ledger 漂移项定谳：参照并入，B 丢弃** |
| 拒绝腿措辞族 ×2 | `The repository '<K>' rejected the resolution of an artifact '<K>:<path>' due to conflict in the snapshot release handling policy.`（GET 腿追加 `; Path: '…'`） | `Repository '<K>' rejected deployment of '<K>/<path>': handling of releases is disabled (handleReleases=false).`（ME-08） | 分歧（status 同 409，文案族不同——新面孔提案①） |
| 成员 GET class 门 ×2（未落地拒族路径直读） | **409**（读路径也执行路径类×策略） | **404** | 分歧（status+语义——新面孔提案②） |
| virt resolve snapshot（hwm plain-SNAPSHOT pom） | 200 | 404 | 分歧但 **handle 不可归因**（见对照腿） |
| 对照腿 ×3（plainsnap 同拼写放 ctl） | 直读 200 / virt 200 / virt 模块清单含 1.0.0-SNAPSHOT | 直读 **404** / virt **404** / 清单含 1.0.0-SNAPSHOT | **混杂坐实**：B 对 plain-SNAPSHOT 拼写 GET 恒 404（默认成员也 404）→ 该拼写解析是独立的既有面孔（提案③）；同时证明模块清单维（上行粗体）**不受混杂**——B 从默认成员照样在 virt 模块清单列入 SNAPSHOT 版本，hwm 的 404 是 handleReleases=false 跳过分支所致 |

### 归因与裁定输入（Arm C 的三段论）

1. **`maven/virtual-metadata-modulereleases-skip` 漂移项定谳**：A live=**并入**（模块级清单保留 handleReleases=false 成员的 SNAPSHOT 版本）；B=**丢弃**（`filterMetadataSteps` levelModule 分支整个跳过成员）。ledger review_gate 预写条件「差分偏参照并入即一行翻回微票」**成立**——建议微票翻回该分支，且按 virtual_metadata.go:207-213 代码注释「differential refutation flips BOTH sites together」与 `internal/repo/virtual.go` walk 层 release-skip 镜像**同翻**（walk 层 release 族面因「release 制品栖身 hr=false 成员」构造不出而 vacuous，翻回无行为损失）。终局裁定权在 compatibility-engineer。
2. **快照族 walk 面**：经 plain-SNAPSHOT 拼写**不可测**（B 对该拼写恒 404，控制腿固化在 case 内自归因）；时间戳拼写构造（mvn deploy unique）留给后续票。
3. **新面孔提案 ×3**（均无既有 ledger 登记，复核 known-divergence.yaml 确认）：①deploy-409 措辞族（status 同、文案族异）；②成员 GET class-policy 门（A 409 vs B 404——A 读路径也执行路径类×策略检查）；③plain-SNAPSHOT 拼写解析（B 404 vs A 200，成员直读+virt 双面）。

## 汇总与状态机反馈

| 臂（ledger 项） | case | 四态 | 分类建议 / 翻态建议 |
|---|---|---|---|
| maven/derived-sidecar-validator-family（T-551） | maven-member-snapshot-sidecar-304 | **PASS×3** | **resolved 候选**（17/17 维双端一致，r1≡r2≡r3）；附跨秒 LM 语义差注记（java-agent 剥离面 per-request 派生 vs A 物化稳定戳——建议登记新条目或入 Risks 微票） |
| rest/kcache-deploy-404-wording（T-553） | rest-kcache-deploy-404-wording | **PASS×3** | **resolved 候选**（三腿 status+文案逐字对齐，virt 405 腿无回归） |
| maven/virtual-metadata-modulereleases-skip | maven-module-handle-seat-probe（remote 面） | BLOCKED/NOT_RUN×4 | remote 前置未变维持；**local 面差分已定谳（见 Arm C），建议微票翻回模块级跳过分支（双站点同翻）** |
| （新）handle* local 面矩阵 | maven-local-handle-walk-skip | FAIL（r3≡r4） | 定谳输入=模块级并入（B 丢弃=BUG 建议）；新面孔提案①②③待裁 |

- **回归对照（vs L032）**：fixed **2**（kcache 措辞族+remote 405 腿；sidecar validator 族 3 维）；仍在 **1**（席位 remote 前置，非恶化）；**新增** 3 面孔（Arm C 提案①②③，其中③为控制腿暴露的既有面）。L032 其余臂（#①非空 DELETE、#④conan、#⑤聚合）本批未开腿，不在回归对照面内。
- **提金候选**（交 compatibility-engineer 评审）：①Arm B 三腿 404/404/405 文案（B 面已对齐，升级为双端一致金样，L032 候选④的完成态）；②Arm C A 腿 22 维矩阵（A 面 handle* local 行为首次取证，含 409 逐字文案与模块级并入实形）；③Arm A 17 维矩阵 A 腿（L032 候选①维持，附跨秒回访注记）。
- **层级索引**：L5（sidecar 协议面 / deploy 拒绝族）、L6（remote handle* 席位探针）、L7（virtual walk/merge handle* 跳过）。

## 复跑

```
bash /tmp/r5-run-t554.sh rN   # source /tmp/r3-difftest.env; A_BASE=…:8082/artifactory
                              # B_BASE=…:18248/binflow; runner --out run/l033-r5-rN
                              # --case×4（脚本留存 /tmp，无凭据）
python3 tools/difftest/v2/runner.py --list   # 发现门 17 case（新增 maven-local-handle-walk-skip）
# B 面实例重建（本批实况）：
#   CGO_ENABLED=0 go build -trimpath -o bin/binflow-server ./cmd/binflow-server
#   BINFLOW_HOME=/tmp/r5-bhome-l033 BINFLOW_SERVER__LISTEN=127.0.0.1:18248 \
#   BINFLOW_DATA_DIR=…/data bin/binflow-server serve（BINFLOW_ADMIN_PASSWORD env 注入）
```

机读产物：`tools/difftest/v2/run/l033-r5-r{1,2,3,4}/results.json`（schema `difftest/v2`；case id 与本报告臂号一一可对账；A/B 两臂三轮 + Arm C 两轮断言值稳定对账输出见工作日志 Commands 节）。

## 追加节 — T-556 修复验证（模块级 handleReleases 跳过翻回，r5≡r6，2026-09-29 同日）

T-556 按本报告 Arm C 定谳输入落码：`internal/adapter/maven/virtual_metadata.go` filterMetadataSteps 删除 levelModule handleReleases 跳过分支（snapshot 级 handleSnapshots 跳过保留——§5.1 面 L033 r3≡r4 双面同）；`internal/repo/virtual.go` walk 镜像 getVirtual 两处 release-skip 同删（双站点同翻规则）、sidecar 豁免谓词随 moot 删。B 二进制自 worktree 现态重建（新 scratch home 同路径重铸），全 4 case 复跑 r5、r6 两轮。

**验收门（面级 PASS×2，r5≡r6）**：

| 验收维 | r5 | r6 | 对照 |
|---|---|---|---|
| virt_modmeta_hwm_status | **200/200** | **200/200** | B 从 404 翻 **200**，与 A 并入面一致 |
| virt_modmeta_hwm_versions | **1.0.0-SNAPSHOT 双列** | 同 | B 从 404 翻 **列 1.0.0-SNAPSHOT** |
| 面关联控制组（sv=1 / hwc 清单 / plainsnap 清单 / seat echo×2） | 全 b==a | 全 b==a | 无波及 |
| Arm A（sidecar 17 维，virtual_metadata.go 同文件回归守卫） | PASS | PASS | 无回归 |
| Arm B（deploy 拒绝族三腿） | PASS | PASS | 无回归 |
| 席位探针（remote 面） | BLOCKED（a=false, b=true） | 同 | 前置未变维持 |

- **稳定性**：`walk-skip r5==r6 assertion values: True` + `r5==r6 divergence keysets: True`；`r5 divergence set == r3 set minus hwm×2: True` 且 `no NEW divergence vs r3: True`——**恰好翻掉目标两维，零新增、零恶化**。
- **case 级四态如实**：`maven-local-handle-walk-skip` 整 case 维持 **FAIL（9 分歧维）**——dual-oracle 口径下 case 级 PASS 在三张已提案新面孔（409 措辞族 / 成员 GET class 门 / plain-SNAPSHOT 拼写）修复前不可达，这是设计使然（case 不掩盖登记中分歧），非 T-556 验收失败。**T-556 的验收对象=模块级面，该面 6 维连续两轮 b==a（PASS×2）**。
- **ledger 建议（更新）**：`maven/virtual-metadata-modulereleases-skip` → **resolved 候选**（修复验收 r5≡r6；与 T-551/T-553 两项合并为 L033 三 resolved 候选）；三张新面孔提案维持待裁。
- 证据：`tools/difftest/v2/run/l033-r5-r{5,6}/`（批末双面 difftest-* 核验为空、B 进程已停、scratch home 已删）。

