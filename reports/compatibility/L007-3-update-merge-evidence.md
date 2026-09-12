# L007-3 取证报告：仓配置更新合并语义（update-merge）边界 + invalid-value 族补证

- 双端：参照 = :8082（Artifactory pro 7.161.20 rev 86120900，admin 取证）；BinFlow 侧未重跑（as-built 行为以 L006 §1.4 + 本报告 §4 代码走读为准）
- 取证时间：2026-09-12
- 任务源：台账 `rest/repo-config-update-merge-semantics`（BUG）+ `rest/repo-config-invalid-value-family`（UNKNOWN）；前置证据 = L006-a-b-diff.md §1.4/§1.3
- 测试资产：`l007m-remote` / `l007m-zero` / `l007m-full` / `l007m-local` / `l007m-inv` 五仓取证完毕已删净（`l007m` 前缀 GET 复核 NONE）
- 性质：architect 设计票取证（L007-3）——本报告只采证据与记边界，矩阵/台账翻绿归 compatibility-engineer，裁定归产品/ADR 流程

## 1. 总判定：参照更新合并语义三列矩阵（POST /api/repositories/{key}）

参照的「Update Repository Configuration」（POST）对省略字段一律**保留存量**，按输入形态分三列（全族实测，E4 活体）：

| 输入形态 | 标量/字符串（含 credential） | 数组（customHttpHeaders） | 对象（contentSynchronisation） |
|---|---|---|---|
| **省略键** | 保留存量（url/username/password/hardFail/四域/periods/notes/description 全实测 keep） | 保留存量 | 保留存量 |
| **显式 null** | **清空**（username:null → ""；password:null → ""；notes:null → ""） | **保留**（customHttpHeaders:null → 不清） | 未测（证据缺口，下 §5） |
| **显式空值**（""/[]/{}） | 清空（password:"" → ""；description:"" → ""） | **保留**（[] → 不清；未发现数组清空通道） | **整族复位**（{} → 四子键全 false） |
| **显式值** | 写入（含 0 与 false：retrievalCachePeriodSecs:0 → 存 0；hardFail:false → false；maxUniqueSnapshots:0 → 存 0） | 写入（覆盖） | 子键级写入 |

要点：

1. **省略=保留是全族行为**，不限于 L006 已证的 remote 四域——credential 域（username/password 密文行）、period 族、notes/description、数组、对象全一致；且**必填项 url 在更新面也可省略**（POST `{}` 无 url → 200，url 保留）。
2. **标量清空通道 = 显式 null 或空串**；数组无清空通道（[] 与 null 均保留）；对象清空通道 = 显式 `{}` 整族复位。三列形态互异——实现面不能一个规则走三族。
3. **credential 域可单侧清**：`username:null` 清 username 但 password（密文行）保留；`password:null` 单清 password。
4. **显式 0 就是 0**：create 侧与 update 侧均存 0（`retrievalCachePeriodSecs:0`/`missedRetrievalCachePeriodSecs:0` 建仓即回 0/0）——参照无「0 视为缺省」规则（与 BinFlow 的 0-as-absent 相反，见 §4-3）。
5. **local 臂同机制**：`l007m-local` 以 `{description}` 单键 POST 更新，notes/blackedOut/maxUniqueSnapshots/archiveBrowsingEnabled/repoLayoutRef 全保留——合并语义非 remote 特有。

## 2. 边界外发现一：PUT-on-existing = 400 create-only（参照 7.161.20）

- PUT `/api/repositories/{key}` 对**已存在 key** 一律 400：`{"errors":[{"status":400,"message":"error when validating repository name: <key> : Repository key already exists"}]}`——body 完整（rclass+packageType+url 齐全）与否同样拒；无 rclass 的 PUT 先撞 400 `Missing repository type`。拒后零副作用（同 body 再 PUT 带 hardFail:true，GET 复核原值未动）。
- 结论：**参照的更新拼写只有 POST**；PUT=create-only。合并语义只居住在 POST 面。
- BinFlow as-built（internal/httpapi/repositories.go handleRepoPut）：PUT-on-existing → UpdateRepo → 200 "update successfully"。**与参照相抵**。
- 矩阵影响：rest-compat-matrix D02 行 3 注记「key 冲突 400/409 语义已对齐」与本实测相抵（该注记所指臂待 compatibility-engineer 复核——回报不直改）。ADR 候选稿已把此臂并入裁定范围（docs/design/repo-update-merge.md §候选）。

## 3. contentSynchronisation 顶层 enabled 异常（留痕，不猜）

- 输入 `{"contentSynchronisation":{"enabled":true,...}}`（两种嵌套形态均试）→ 回显顶层 `enabled` 恒 false，而 `statistics.enabled`/`properties.enabled` 正常落 true；省略键则整族保留（stats/props 存量存活）。
- 顶层 master switch 的 wire 拼写/前置条件未能以已试形态落库——**记为证据缺口**（可能是站点级前置或异拼写；clean-room 不猜）。合并语义本身不受影响：该族「省略=保留、{}=复位」两列已双测闭环。

## 4. BinFlow as-built 对照（代码走读，只读）

1. **remote 更新=全量替换回落产品默认**：internal/repo/service.go UpdateRepo 的 remote 臂走 `parseRemoteConfig(r.Config, ...)`——从产品默认值起步、input 覆盖；省略席位回落默认而非存量。config 键整体省略（`r.Config==""`）才保留。**即台账 BUG 本体。**
2. **PUT-on-existing=更新**（§2）；console 前端 lib/repos.ts 已按 createRepo=PUT（新建）/updateRepo=POST（编辑）分工——PUT 若对齐 create-only，console 面零破坏。
3. **0-as-absent 规则**（T-290 族，config.go）：period 族显式 0 视为缺省保默认——与参照 §1-4「显式 0 存 0」相抵，属 create/update 共有的**相邻差异**（非 merge 票本体；建议随 merge 实现票联裁或另开规格票）。
4. **patch 基建已在**：remoteConfigInput 全指针席位（L006-1 落）+ contentSyncInput 指针 + 别名解析（socketTimeoutMs/MissRetrieval 双拼写）——merge 实现=给 parseRemoteConfig 注入「基线=存量 canonical」起步值，省略席位回落基线；结构零新增。
5. 现行注释 `Full-replace semantics (the Artifactory PUT model, T-80's ruling)`（service.go remote 行 Password 段）与本取证相抵——T-80 认知需在 ADR 流程中勘误（实现票落地时改注释）。

## 5. 证据缺口清单（照证据不猜，未测即记缺口）

| 缺口 | 状态 |
|---|---|
| proxy 族 keep/clear | 参照实例零已配代理（system configuration 无 `<proxies>` 段），无法活体赋值；不猜 |
| 数组清空通道 | [] 与 null 均保留；未发现 REST 通道，如需清空语义须再取证（如 UI 内部面） |
| contentSynchronisation 顶层 enabled 落库拼写 | §3 留痕 |
| 对象族显式 null | 未测（三列矩阵该格空） |
| 参照 GET 回显 password=密文块（admin 可读） | 留痕（BinFlow NFR-S14 恒不回显——既定安全姿态，非本票范围） |

## 6. invalid-value 族（台账 UNKNOWN）取证补充

L006 §1.3 三臂 + 本票补测逐字报文（2026-09-12 活体）：

| 臂 | 输入 | 参照响应（逐字） |
|---|---|---|
| mistyped | `maxUniqueSnapshots:"seven"` | **500** `Error converting from 'String' to 'Integer' For input string: "seven"` |
| 宽容布尔 | `blackedOut:"yes"/"on"/"TRUE"/"off"` | **200 全接受**（yes/on/TRUE→true 族、off→false；echo 实证） |
| 宽容布尔（拒） | `blackedOut:"maybe"` | **400** `Can't convert value 'maybe' to type class java.lang.Boolean` |
| 未知布局名 | `repoLayoutRef:"no-such-layout"` | **400** `Unable to find repository layout by the name: no-such-layout` |

要点：参照自身姿态**不均**——String→Integer 撞 **500**、String→Boolean 拒 **400**、布局名拒 **400**；宽容布尔可落库（"yes"→true 持久化）。裁定素材（两案对比/建议立场/影响面）整理于 docs/prd/ruling-invalid-value-family.md（L007-3 architect 供 PM/conductor 裁定用素材，不构成裁定）。
