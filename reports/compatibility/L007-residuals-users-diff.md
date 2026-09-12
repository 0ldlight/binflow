# L007-1 差分复验报告（票 1：permissions residuals 三臂 / 票 2：D04-R02 users/{name} 字段集）

- 双端：参照 = :8082（Artifactory 7.161.20，admin/JFrog@2026）；BinFlow = :8084（独立验证实例 `binflow-l0071-verify`，镜像 `uat-l0071-c1193f5f`，自 develop@c1193f5f + 本轨改动重建——不碰共享 binflow-ga，该容器当时载并行轨 L007-2 的镜像）
- 取证时间：2026-09-12（参照侧，三臂证据为活体重取，与 L006 记录一致无翻案）；复验时间：2026-09-12（本轨镜像）
- 方法：双端同 body 对拍；错误臂比对状态码 + 错误体裁 + 文案；字段集臂比对键集与取值
- 测试资产：双端 l007*（仓/权限目标/用户）已删净（参照侧 DELETE user/repo 皆 200；BinFlow 侧为一次性容器随验证即焚）

## 1. 票 1：permissions residuals 三臂（台账 rest/permissions-v1-response-shape-residuals）

| 臂 | 腿 | 参照 :8082 | BinFlow :8084（本轨后） | 判定 |
|---|---|---|---|---|
| ① DELETE 状态码+文 | DELETE 已存目标 | 200 text/plain `Successfully deleted permission Target 'l007-del'` | 200 text/plain `Successfully deleted permission Target 'l007-del'` | **SAME**（逐字） |
| ② 未知名错误体裁 | DELETE 未知名 | 404 errors-envelope `{"status":404,"message":"Not Found"}` | 404 errors-envelope 同体同文 | **SAME** |
| ② 同族延伸 | GET 未知名 | 404 errors-envelope "Not Found" | 404 errors-envelope 同体同文 | **SAME** |
| ③ admin 主体 | PUT users 含 admin | 400 `The user: 'admin'' has admin privileges, and cannot be added to a Permission Target.`（参照自带双单引号） | 400 同文案逐字（text/plain 载体，见 §3 注） | **SAME**（文案） |
| ③ 优先序佐证 | admin + 未知名用户 | 400 admin 文案（admin 扫描先于 unknown-user 扫描） | 400 admin 文案同 | **SAME** |
| ③ 优先序佐证 | 未识仓 + admin | 400 仓校验文案（仓校验先于 admin 扫描） | 400 仓校验文案同 | **SAME** |
| 新发现（超出台账三臂） | PUT groups 未识组 | 400 `Permission target contains a reference to a non-existing group 'x'.`（组名前无冒号，与 user 臂不同） | 400 同文案逐字（原 `Unable to find group by name 'x'.` 已改） | **SAME**（本轨一并闭合，留痕） |
| 冻结面（负证） | DELETE /api/v1/permissions/{name} | （参照无此面） | 204（单元测试钉死） | 冻结不变 |

结论：台账三臂全数闭合，且 DELETE 未知名与 PUT 未识组两腿随同链路一并收口。建议 `known-divergence#rest/permissions-v1-response-shape-residuals` → resolved，matrix D04-R18 → compatible。

## 2. 票 2：D04-R02 GET /api/security/users/{name} 字段集

参照字段集（admin / l007fresh / anonymous 三形态活体取证，7.161.20 全 addon）：
`name, [email], admin, policyViewer, policyManager, watchManager, reportsManager, profileUpdatable, internalPasswordDisabled, [groups], [lastLoggedIn], lastLoggedInMillis, realm, offlineMode, disableUIAccess, mfaStatus, status, shouldInvite`
（[] = 条件出现：email 未设整键缺席、groups 空集整键缺席、lastLoggedIn 未登录整键缺席；lastLoggedInMillis 恒渲染——admin 行 lastLoggedIn 有值时 millis 仍为 0；status 枚举 ENABLED；无 enabled 布尔、无 adminRole、无 uri、无 source）

| 腿 | 参照 | BinFlow（本轨后） | 判定 |
|---|---|---|---|
| 键集——新增列 | policyViewer/policyManager/watchManager/reportsManager=false、offlineMode=false、mfaStatus="NONE"、shouldInvite=false、lastLoggedInMillis=0、status="ENABLED" | 全部补齐，取值同（false/"NONE"/0） | **SAME** |
| status 闭集 | ENABLED（DISABLED 为 REST 面不可达态，本实例实测 POST {"status":"DISABLED"} 不生效；闭集依据官方文档） | enabled 列直译：true→ENABLED / false→DISABLED（单元测试钉死两态） | **SAME**（映射级） |
| lastLoggedIn 条件出现 | 未登录无键；已登录 `2026-09-11T17:35:34.176Z` | 未登录无键（login 前后活体复验）；已登录 `2026-09-12T04:38:01Z` | **SAME**（键位）；精度差见 §3 |
| 无 enabled 布尔 | 无 | 保留 `enabled: true`（超集） | 超集 tolerated |
| email/groups 形态 | 未设/空集整键缺席 | 恒渲染（""/[]，超集） | 超集 tolerated（裁量记录，§3） |
| adminRole/source | 无 | 保留（超集） | 超集 tolerated |

建议 matrix D04-R02：partial → compatible（带超集注记）。

## 3. 残差与裁量记录（回报，不直改）

1. **错误体裁载体**：参照 400 系全部走 errors-envelope（application/json），BinFlow 本面 400 系为 text/plain（writePlainError）。L006 差分已按「文案逐字」判 SAME（载体差低于阈值）；本轨 404 未知名臂按台账口径升级为 envelope，400 系维持既定纯文本姿态（与同面 missing repositories/unknown repository/unknown user 各腿一致，避免同链路体裁混排）。
2. **超集三键 + email/groups 恒渲染**：console 用户编辑器（web/src/pages/security/UserDetailPage.tsx）回显驱动 off `adminRole`+`enabled`，表单种子 `[...d.groups]`/`d.email`——参照的整键缺席形态会崩 BinFlow 自有前端，故按超集 tolerated；web/ 非本轨可改域，若产品裁「严格裁齐」，需先开 console 前端票。
3. **lastLoggedIn 精度**：参照毫秒（.176Z），BinFlow 秒（Z）——M16 列表面已定谳的 audit 派生族拼写，本面沿用。
4. **新发现（建议立账）**：① 参照建用户自动入默认组 `readers`（groups:["readers"]），BinFlow 无此语义（[]）——用户创建默认组成员面，非字段集问题；② 参照有 `anonymous` 用户行（profileUpdatable=false、internalPasswordDisabled=true），BinFlow 无该行（404）——anonymous 建模级差异。
5. **参照 400 载体佐证**：参照 PUT users/{name} 的各 400 同为 envelope（§1 表内三腿原文已留 /tmp/l0071-legs.txt 摘录于本报告）。

## 4. 命令与证据

- 差分腿脚本：`/tmp/l0071-diff.sh`（双端三臂 + 字段集全腿）+ arm3 重跑（BinFlow 侧 packageType 修正后）
- 单元/包门：见 reports/agents/T-L007-1.md Commands 字段
