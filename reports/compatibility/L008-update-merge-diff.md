# L008-1b 差分复验报告：update-merge 实现票（ADR-0050 落地复验）

- 双端：参照 = :8082（Artifactory pro 7.161.20，admin/JFrog@2026）；BinFlow = :8085（UAT `uat-l0081b-95b4acad`，`make release` + `build-release.sh`（REGISTRY= PUSH=0 ARCHES=amd64 VARIANTS=alpine）重建自工作树〔含本轨全部改动，基线 develop@95b4acad〕；独立验证容器 binflow-l0081b，不碰共享 binflow-ga〔彼时载 L008-2 镜像〕）
- 取证/复验时间：2026-09-12
- 任务源：ADR-0050 + docs/design/repo-update-merge.md §9.3 差分臂清单；台账 `rest/repo-config-update-merge-semantics`（BUG，authority = ADR-0050）
- 脚本：`tools/difftest/l0081b/update-merge-arms.sh`（curl 双发对拍，归一化比对；本报告结论的证据载体）
- 测试资产：双端 `l0081b-m`/`l0081b-st`/`l0081b-hd` 取证完毕已删净（双端列表复核零残留）

## 0. 对拍前补证：对象族两格证据缺口（:8082 活体，2026-09-12）

任务书两格缺口在实现前补证（产出归规格票，机制面已按此实现）：

| 缺口 | 取证（:8082 POST 更新） | 结论 |
|---|---|---|
| A14 子键未提 disposition | 存量 stats/props 双 true → POST `{contentSynchronisation:{statistics:{enabled:false}}}` → 回显 stats=false **且 props 复位 false** | **显式对象 = 整体替换**（未提子键复位 false，非并集基线）；实现按此落 |
| A15 对象显式 null | 存量 stats/props 双 true → POST `{contentSynchronisation:null}` → 回显双 true 原样 | **null = 保留整族**；实现按此落 |
| （复核）A7 `{}` | 存量双 true → POST `{}` → 四子键全 false | 整族复位（与 L007-3 一致） |
| 输入拼写旁证 | create 面 flat 拼写（statisticsEnabled）**不落库**，nested（statistics.enabled）落库 | 参照输入面认 nested；BinFlow 认 flat（T-317 xsd 锚）——既有 wire 拼写族，非本票轴 |

另：A17（PUT-on-existing 非 admin 403/400 次序）——anonymous 腿 401「Authentication is required」先于一切（与 BinFlow 路由门一致）；authenticated 非 admin 腿因参照实例密码策略拒绝建用户未能取证，**缺口留痕**（probe 臂，记录不裁）。

## 0.5 Review B 追加取证与复验（2026-09-12，B2 blocking 返工）

**B2（socketTimeout 别名对显式 0 语义）：:8082 四探针臂取证**

| 臂 | 参照 :8082 结果 |
|---|---|
| create `socketTimeoutMillis:0` 单臂 | **存 0**（显式 0 = 0，create 面） |
| update（存量 30000）POST `{"socketTimeoutMillis":0}` | **存 0**（update 面同判） |
| create `socketTimeoutMillis:0 + socketTimeoutSecs:30` | **0**——显式 millis-0 **胜过** secs（无 secs 回落） |
| create `socketTimeoutMs:0`／`socketTimeoutSecs:30` | 均 15000——**参照不识这两个拼写**（未知字段面整丢：ms=PRD 别名、secs=M3 席位，皆 BinFlow 自有输入便利） |

实现按取证对齐：`resolveRemoteAlias` 重写（单侧显式 0 = 给定值，双给定时 0 让位非零侧——yield 规则维持）；socket 零解包删除（millis:0 胜过 secs，对齐 D2 取证）。参照无意见的拼写（ms/secs——非参照字段）按 ADR-0050 决策 3 字面统一「显式 0 存 0」。

**B2 双端复验（重建镜像 `uat-l0081b2-95b4acad`，:8086 独立容器，B1/B2 修复后）**

| 臂 | 参照 | BinFlow | 判定 |
|---|---|---|---|
| B2-D1 create `socketTimeoutMillis:0` | 0 | 0 | **SAME** |
| B2-D2 create `millis:0 + secs:30`（优先序） | 0 | 0 | **SAME** |
| B2-D3 update 存量 30000 → `millis:0` | 0 | 0 | **SAME** |
| B2-D4 `socketTimeoutMs:0`（记录臂） | 30000（ms 非参照字段，未知面丢弃存量保留） | 0（BinFlow 自有 alias 席位按 ADR 字面存 0） | **DIVERGENT-BY-DESIGN**——分歧在席位存在性（BinFlow 接受 ms 别名=既有超集输入面，T-290），非零语义；席位内部零语义 ADR 对齐 |

**B1（credential 保留臂吞 GetConfig 错误，P0 数据完整性）**：修法落地（仅 `ErrRemoteConfigNotFound` 容忍，其余错误 wrap 拒绝更新——UpdateConfig 永不携空 Password 覆写存量 sealed 行）；单元腿 `TestRemoteUpdateCredentialKeepSurfacesGetConfigFailure`（装饰器注入瞬态错 → 更新拒绝 + remote_configs 行逐列未动）。服务端修复，无 wire 面变化，不影响既有 13 臂判定。

镜像注：`uat-l0081b2` 为离线组装（DaoCloud alpine 镜像 EOF——tools/difftest/l0072/build-image-offline.sh 同式：cached assets rootfs + 本地 alpine digest 钉基 + 新 dist 二进制；assets 与 VER 无关）。

## 1. 硬断言臂逐臂结论（双端同 body 同序对拍）

| 臂 | 输入 | 参照 :8082 | BinFlow :8085 | 判定 |
|---|---|---|---|---|
| A1 省略×标量 | POST `{"hardFail":true}` | 全席位 keep（url/credential/period/四域/cs） | 同（keep-set 归一化逐键同） | **SAME** |
| A2 省略×credential | （A1 同体；password 回显对拍面排除） | 密文行保留 | 密文行保留（单元层 `TestRemoteUpdateCredentialKeepSealedRow` 断言 sealed 行逐字节不动；NFR-S14 恒不回显=既有安全姿态账） | **SAME**（排 password 回显面） |
| A3 null×标量 | POST `{"username":null}` | username 清空 | 同（归一化 None≡""：参照回显空串、BinFlow 键省略——echo 形差，值语义同清） | **SAME** |
| A4 null×数组 | POST `{"customHttpHeaders":null}` | 存量数组保留 | 无此席位（scenario-D 未知字段面）；其余席位 keep | **SAME**（语义面；席位 echo 差为既有未知字段族，见 §3） |
| A5 空值×标量 | POST `{"description":""}` | 清空 | 同 | **SAME** |
| A6 空值×数组 | POST `{"customHttpHeaders":[]}` | 存量数组保留（无清空通道） | 同 A4 | **SAME**（同上注记） |
| A7 空值×对象 | POST `{"contentSynchronisation":{}}` | 四子键全 false | 同（归一化 nested↔flat 后同） | **SAME** |
| A8 显式值族 | POST `{"retrievalCachePeriodSecs":0,"maxUniqueSnapshots":0,"hardFail":false}` | 存 0/0/false | 同（0-as-absent 双面收口后） | **SAME** |
| A9 url 省略 | POST `{}` | 200，url 保留 | 同 | **SAME** |
| A10 PUT-on-existing 完整 body | PUT 完整建全体 | 400 `error when validating repository name: <key> : Repository key already exists`（errors 信封） | **逐字同** | **SAME** |
| A10' PUT-on-existing 无 rclass | PUT `{"url":...}` | 400 `Missing repository type`（type 先于 key-exists） | 400 `package type ""...`（type 门先于 key-exists——次序同，文案异） | 次序 SAME／文案 DIVERGENT（既有文案族，A16 probe 记录） |
| A11 拒后零副作用 | 拒后 GET 复核 | 原值未动 | 同 | **SAME** |
| A12 PUT 新建回归 | PUT 新 key 完整体 | 200 `Successfully created repository '<key>'` | 同文案 | **SAME** |
| A13 console 全量表单回归 | POST 全量 seed 体再存 | GET 回读与对端同形 | 与参照逐键同 | **SAME** |
| A14 对象部分子键（新取证格） | POST 单子键对象 | 未提子键复位 false（§0 补证） | 同（未提=false，整体替换） | **SAME** |
| A15 对象显式 null（新取证格） | POST `{"contentSynchronisation":null}` | 整族保留（§0 补证） | 同 | **SAME** |

**总判定：13 硬断言臂全 SAME（含 A10 逐字文案）；对象族两格缺口（A14/A15）补证后双端同判。**

## 2. 归一化说明（对拍面裁剪，均非本票引入）

1. **cs 输入拼写**：参照输入认 nested（statistics.enabled），BinFlow 认 flat（statisticsEnabled，T-317 xsd 锚）——seed/A14 臂按端各异体送入、比对落库后状态（归一化 nested↔flat）；既有 wire 拼写族。
2. **清空格 echo 形**：参照回显显式空串（`"username":""`），BinFlow omitempty 键省略——归一化 None≡""；值语义同清。
3. **customHttpHeaders 席位**：BinFlow 无此席位（repo-semantics 7.1 传输层选项行，scenario-D 容忍面）——A4/A6 比对「不清空」语义与其余席位 keep；字段 echo 差为既有未知字段族，若后续立票补席位则两臂自动升格为字段级对拍。
4. **password 回显**：恒排除（NFR-S14 vs 参照密文块——既有安全姿态账，ADR-0050 决策 4 明示不随本票翻）。
5. **socketTimeoutSecs 种子拼写**：参照 create 面不识 socketTimeoutSecs（存默认 15000），BinFlow 识（M3 席位）——种子改用双端同识的 socketTimeoutMillis；该拼写差为既有输入面族。

## 3. matrix / 台账翻绿建议（回报，不直改）

| 条目 | 现态 | 建议 | 依据 |
|---|---|---|---|
| known-divergence `rest/repo-config-update-merge-semantics` | BUG | **resolved**（authority = ADR-0050） | §1 全臂 SAME；实现票落地 + 差分复验双件齐 |
| matrix D02-R03（PUT 建仓） | partial | → compatible，注记回填「PUT-on-existing 400 对齐闭环（A10/A11/A12 逐字）」 | §1 |
| matrix D02-R04（POST 更新） | partial | → compatible（L007-3 补注臂随闭环） | §1 三列矩阵 |
| 新立规格票素材 | — | 对象族三格（A7/A14/A15）+ A16 次序/文案 + A17 缺口 + 参照 cs 输入认 nested——字面契约冻结素材归 compatibility-engineer | §0 |

## 4. 既有差异留痕（非本票引入，不随本票裁）

- A16 文案：BinFlow type 门文案（`package type ""...`／`invalid repository type`）vs 参照 `Missing repository type`——create 面既有文案族。
- A17 authenticated 非 admin 门序：未取证（参照实例密码策略拒建用户）；BinFlow 门序 = 路由 family-7 门 → 400（ADR-0026 勘误候选已预登记该臂不可达）。
- 参照 cs 顶层 enabled 落库拼写未解（L007-3 §3 留痕维持）。
