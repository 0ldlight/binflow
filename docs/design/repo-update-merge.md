# ADR 候选稿：仓配置更新合并语义（update-merge）——省略=保留的 patch 模型

| 项 | 值 |
|---|---|
| 状态 | **Accepted（2026-09-12）**——ADR-0050 收编（DECISIONS.md，LOOP 008 L008-1a）：案 A 定谳，分歧处以 ADR 为准；本稿保留为实现票任务书载体（§9） |
| 台账锚 | `docs/compatibility/known-divergence.yaml` → `rest/repo-config-update-merge-semantics`（BUG） |
| 证据 | E4 活体（reports/compatibility/L007-3-update-merge-evidence.md，2026-09-12 :8082 双仓四臂实测）+ L006-a-b-diff.md §1.4 |
| 关联 | matrix D02-R03/R04（翻绿不吸收本账）；T-80「全量替换=Artifactory PUT 模型」旧认知（本稿勘误对象） |

## 1. 背景与问题

BinFlow 的仓配置更新面（PUT/POST `/api/repositories/{key}` → `repo.Service.UpdateRepo`）对 remote 仓 configJSON 是**全量替换**：新 body 省略的字段回落产品默认。参照 Artifactory 是**合并**：省略=保留存量。行为面后果：任何「只发改字段」的客户端脚本（先建仓、后 `POST {"hardFail": true}` 想拧一个开关）会在 BinFlow 静默丢掉全部未回传配置（credential/period/布局/四域全族）——参照上同脚本行为正确。这是族级 BUG（影响 remote 全字段，local 的 caller-owned blob 与 virtual 的 members 同属更新面，见 §6 边界）。

## 2. 参照语义（取证摘要，照证据不猜）

**更新拼写只有 POST**：PUT-on-existing = 400 `Repository key already exists`（create-only，body 完整与否同拒，零副作用）。合并语义三列矩阵（全族实测）：

| 输入形态 | 标量/credential | 数组 | 对象（contentSynchronisation） |
|---|---|---|---|
| 省略 | 保留（url 亦可省） | 保留 | 保留 |
| 显式 null | 清空（credential 可单侧清） | 保留 | 未测（缺口） |
| 显式空值 | 清空（""） | 保留（[]；无清空通道） | 整族复位（{}） |
| 显式值 | 写入（0 与 false 即 0/false） | 覆盖写入 | 子键级写入 |

credential 细节：password 回显为密文块（admin 可读）；省略=保留密文行，`null`/`""`=清。显式 `retrievalCachePeriodSecs:0` 建仓与更新**均存 0**（参照无 0-as-absent 规则）。

## 3. 候选方案对比

| 维度 | A. 严格对齐（POST=merge + PUT=create-only） | B. merge + PUT 更新超集 | C. 维持全量替换（INTENTIONAL） |
|---|---|---|---|
| 语义 | POST 按 §2 三列矩阵合并；PUT-on-existing 对齐 400 | POST 同 A；PUT-on-existing 维持 BinFlow 更新行为（也走 merge） | 现状；台账改 INTENTIONAL 登记 |
| 兼容差 | 零（wire 面与参照同形） | PUT 臂超集差（参照 400 处 BinFlow 200）——须永久登记 | 族级实质分歧照旧（客户端静默丢配置） |
| 迁移冲击 | BinFlow 存量「PUT 改仓」脚本断（改用 POST 或接受 400） | 存量 PUT 脚本零断 | 零 |
| 破坏面 | 破坏 BinFlow 自有用户习惯（非参照习惯） | 无 | 持续伤害「参照习惯迁移者」 |
| 实现量 | merge 基线注入 + PUT 存在性 400 一处 | merge 基线注入 | 零 |
| 维护 | 一套语义 | PUT/POST 双更新入口语义需同步演进（双入口漂移风险） | — |

**C 拒绝理由**：参照生态的标准脚本形态（terraform provider、curl 改配置、UI 保存）大量依赖省略=保留；全量替换要求客户端永远回传全量 body，与参照习惯根本冲突，且丢配置是静默数据损失——兼容目标（ADR-0003 对齐原则）下不可辩护。

**A vs B**：A 为严格对齐、单更新入口、语义最简；代价是 BinFlow 已发布形态里「PUT 也能改仓」的行为回撤（BinFlow 尚在 UAT 前夜、无外部存量承诺，回撤窗口现在最便宜）。B 保留双入口但制造永久超集差与双入口同步演进负担。**建议 A**；若 conductor 判定 BinFlow 自有 PUT-改仓脚本存量不可破，则退 B 并登记超集差。

## 4. 决策建议（待 ADR 流程采纳）

1. 更新面（POST `/api/repositories/{key}`）采 **merge-on-omit**：省略=保留存量；标量显式 null/""=清空；数组省略/null/[]=保留（清空通道不实现——参照亦无）；对象显式 {}=整族复位；显式值=写入。
2. PUT 对齐 **create-only**：已存在 key → 400 `error when validating repository name: <key> : Repository key already exists`（errors envelope，逐字对齐）。console 前端已按 PUT=create/POST=update 分工，零破坏。
3. **显式 0 = 存 0**（对齐参照；废弃 period 族的 0-as-absent 于更新面）。存量 0-as-absent 是 create 面相邻差异，随本票实现联裁收口（create 面同步对齐「显式 0=0」，fetch 侧 0 的生效语义走既有 0=unset 回落链，wire 回显以存值为准）。
4. soft-seam：本 ADR 定机制（三列矩阵 + create-only）；字面契约（400 文案逐字、密文回显与否等）以规格票/compatibility contract 冻结为准，分歧走 Errata。

## 5. 实现面（票内不写码，定契约）

- `internal/repo` UpdateRepo 的 remote 臂：`parseRemoteConfig` 增加「基线模式」——以**存量 canonical**（GET 回读形）为起步值替代产品默认起步；`remoteConfigInput` 全指针席位（L006-1 已建）逐席位判缺：nil=基线值，非 nil=新值。别名解析（socketTimeoutMs/MissRetrieval 双拼写）规则不变，唯 0-as-absent 分支在基线模式下改为「0 即写 0」。
- credential 族：省略 username/password=保留密文行不动；显式 ""/null=清（sealPassword 空串路径已有）。`Full-replace semantics (the Artifactory PUT model, T-80's ruling)` 注释随实现票勘误。
- httpapi `handleRepoPut`：GetRepo 命中存在 → 400（新错误类型走既有 writeRepoSvcError 映射）；create 臂不变。`handleRepoPost` 不变（语义在 service 层）。
- local 臂：参照 local 同为省略=保留（取证 §1-5），但 BinFlow local 是 caller-owned passthrough blob（无 canonical 席位）——**本票范围=remote 先行**（台账 BUG 本体），local/virtual 的 merge 另立实现票（virtual 的 members 已是显式列表替换语义，参照行为待取证）。
- 验收锚：difftest 双发对拍（curl 改字段脚本：省略族/null 族/{}族/0 族四臂）+ console 编辑回归（全量 body 在 merge 语义下结果不变）。

## 6. 迁移与升级窗口

- **行为变更公告**：依赖「全量替换」清配置的 BinFlow 脚本须改为显式 null；依赖 PUT-改仓的脚本须改 POST。release note 置 breaking changes 首条；无双轨期（参照无 escape hatch，双轨=自造差异）。
- 升级窗口：现存仓的存量 canonical 即基线，无数据迁移；仅行为面变更。
- 技术债台账登记：0-as-absent create 面收口（§4-3）与 local/virtual merge 后续票。

## 7. 与 REST/RFC 的张力（说明，不改变决策）

RFC 9110 语义里 PUT=整体替换、且「省略即抹除」本属 PUT 的正统语义——参照的实际形态反于教科书：**PUT=创建、POST=patch 更新**，省略=保留更像 PATCH 而骑在 POST 上。本决策对齐的是**事实生态**（十年 Artifactory 客户端习惯）而非 RFC 纯度，依据 ADR-0003（行为对齐 Artifactory）优先；代价是 BinFlow 的 REST 面延续参照的非正统动词分工——这是兼容层的既定立场（REST 兼容面整体如此），不是本 ADR 引入的新债。

## 8. 后果

- 正：族级 BUG 收口（remote 全字段）；参照习惯脚本（部分回传体）迁移零修改；矩阵 D02-R03/R04 可随翻绿（合并语义账吸收）。
- 负：BinFlow 自有「PUT 改仓/全量替换清配置」脚本破坏（UAT 前夜窗口内可接受）；实现+差分验证成本一票。
- 风险：三列矩阵的字面契约（400 文案、{} 复位边界）需规格票冻结；数组无清空通道若未来被差分打脸（发现了参照的清空通道）走 Errata 回填，不动机制。
- 验证载体：curl 脚本族 + terraform provider 习惯形态（改单字段 POST）；差分报告归 reports/compatibility/。

## 9. 实现票任务书（ADR-0050 派发级——dev-go-core；conductor 转派用）

裁定 authority = **ADR-0050**（机制面）；字面量以规格票/contract 冻结为准。范围 = 决策 1/2/3/4/5 的 remote 臂 + description 席位 + PUT 回撤；**local/virtual 臂 merge 不在本票**（ADR-0050 决策 5 另票）。路线 = `parseRemoteConfig` 基线注入 + **指针席位零新增**（既有 `remoteConfigInput` 指针席位复用；非指针 string 席位〔username/password〕经 raw-map 键在场判缺）。

### 9.1 文件级改动清单

| # | 文件 | 改动 | 锚（as-built 行号为 2026-09-12 快照） |
|---|---|---|---|
| 1 | `internal/repo/config.go` | `parseRemoteConfig` 增**基线模式**：签名加 baseline 参数（如 `parseRemoteConfig(config, packageType string, baseline *remoteConfig)`；CreateRepo 传 nil 维持默认起步）。基线模式起步值 = 存量 canonical（GET 回读形）替代产品默认；指针席位逐席判缺——nil=基线值、非 nil=新值。① URL 必填检查在基线模式放宽：基线有 url 即可（更新面 url 可省）；② period 族 0-as-absent 分支**双面移除**（`for … if !f.given \|\| f.value == 0 { continue }` 的 `\|\| f.value == 0` 子句删除，负值拒与别名分歧拒维持——显式 0 即写 0，create 面同步）；③ username/password 非指针席位：复用 `validateRemoteConfigShape` 已解出的 raw map（或等价 raw 解码）判**键在场**——缺键=保留基线（credential 密文行不动），在场且 ""/null=清，在场有值=写入；**零新增结构席位**；④ `contentSynchronisation` 基线：省略=保留整族、显式 `{}`=整族复位 false、显式对象=子键级写入（`parseContentSynchronisation` 起步值由零值改基线值——**未提子键 disposition 待探针臂（9.3-A14）定案后落**，先行按「未提=基线」实现并表驱动钉死）；⑤ 别名解析（socketTimeoutMs/MissRetrieval）规则不变 | `remoteConfigInput` L211-245（L006-1 指针席位）；`parseRemoteConfig` L328-469 |
| 2 | `internal/repo/service.go` | `UpdateRepo` remote 臂（L2576）传 baseline：`current.Config` 解出的存量 canonical（存量 unmarshal 先例已在 L2589-2590 审计段）；credential 保留语义落到 `remote_configs` 行——省略 username/password=保留行内存量（含 sealed password 原样），显式 ""/null=清（`sealPassword("")` 清空路径已有）；**勘误 T-80 注释**（L2627-2630 `Full-replace semantics…` 整段替换为 merge 语义 + ADR-0050 引用）；audit detail 结构不变 | `UpdateRepo` L2507-2665；CreateRepo 调用点 L2196 传 nil |
| 3 | `internal/httpapi/repositories.go` | ① `handleRepoPut` **移除更新臂**（L648-672 的 GetRepo 命中→UpdateRepo 分支）：已存在 key → 400 errors 信封，参照逐字 `error when validating repository name: <key> : Repository key already exists`（建议 handler 直发以控逐字文案；或走 CreateRepo→`ErrRepoExists`→`writeRepoSvcError` 400 面——两路线任选，**文案对拍差分臂 A10 钉死**）；create 臂（含 addon 门、FR-65 分门）不变。② `handleRepoPost` description 席位：raw body 判 `description` 键在场——缺键=传 `current.Description`（保存量），在场（含 null/""）=覆写（null 解码为 ""）；POST 未知 key 404 维持 | `handleRepoPut` L625-704；`handleRepoPost` L708-742 |
| 4 | `web/src`（console） | **零改动**（验收臂回归证明）：`createRepo`=PUT / `updateRepo`=POST 已分工（lib/repos.ts L182-189）；local 仓 quota「全量替换体」策略在 merge 语义下等价（显式全值=全写） | — |
| 5 | `internal/httpapi/keypair.go` | **零改动确认项**：`updateRepoKeypairRef`（L396-406）传全量 merged config，merge 语义下行为不变（回归臂覆盖） | — |

### 9.2 单元验收（表驱动，`go test ./internal/repo/... ./internal/httpapi/...`）

1. 三列矩阵 × 三族（省略/null/空值/显式值 4 形态 × 标量/数组/对象 3 族）——remote 臂逐席位断言存量保留/清空/复位/写入；
2. url 更新面可省（POST `{}` → 200、url 保留）；create 面 url 必填不回退；
3. credential 单侧清（username 清 password 留 / 反之亦然；省略双留、密文行不动）；
4. 显式 0 存 0（retrieval/missed/socketTimeout 族；create+update 双面）+ 负值拒 + 别名分歧拒回归；
5. PUT-on-existing → 400（完整 body / 无 rclass 两臂）；PUT 新建 200 文案不变；POST 未知 key 404；
6. description 省略保留 / null/"" 清空；
7. `go build ./...` + `go vet ./...` + `gofmt` 零告警（既有门）。

### 9.3 差分臂清单（curl 双发对拍，tools/difftest 脚本化，报告归 reports/compatibility/）

硬断言臂（对拍 SAME）：A1 省略族×标量（POST `{"hardFail":true}` 单键 → 全字段 keep）；A2 省略族×credential（密文行保留；BinFlow 不回显 password 为既有 NFR-S14 差异账，对拍面排除该字段）；A3 null 族×标量（username/notes/description:null → 清）；A4 null 族×数组（customHttpHeaders:null → 保留）；A5 空值族×标量（password:""/description:"" → 清）；A6 空值族×数组（customHttpHeaders:[] → 保留）；A7 空值族×对象（contentSynchronisation:{} → 整族复位）；A8 显式值族（hardFail:false / maxUniqueSnapshots:0 / retrievalCachePeriodSecs:0 → 存 0/false 回显）；A9 url 省略（POST `{}` → 200 url 保留）；A10 PUT-on-existing 完整 body → 400 逐字（errors 信封）；A11 PUT-on-existing 拒后零副作用（GET 复核原值）；A12 PUT 新建回归（200 create 文案）；A13 console 编辑回归（全量 body 保存 → GET 回读不变）。

探针臂（记录不裁，产出归规格票）：A14 对象部分子键 vs 存量（未提子键 disposition——定 9.1-④ 终稿）；A15 对象族显式 null；A16 PUT-on-existing 无 rclass 的报错次序（`Missing repository type` vs key-exists 谁先）；A17 PUT-on-existing 非 admin 门序（403/400 次序）。

### 9.4 边界与不做

fetch 侧 0 的生效语义不动（既有 0=unset 回落链，ADR-0050 决策 3）；数组清空通道不实现（参照亦无）；local/virtual merge、GET 回显密文块（NFR-S14 对立面）不在本票；矩阵/台账回填归 compatibility-engineer（本票产出差分报告供其引用）。
