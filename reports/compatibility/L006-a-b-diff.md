# L006-1 差分复验报告（票 A：仓配置四域 round-trip / 票 B：经典路径别名）

- 双端：参照 = :8082（Artifactory 7.161.20，admin/JFrog@2026）；BinFlow = :8083（UAT `uat-l0061c-f0edecef`，重建自 develop@f0edecef + 本轨改动含 Review A/B 返工；注：镜像含并行轨 L006-2 的 adapter/docker WIP——与本轨 REST 面无路由交集）
- 取证时间：2026-09-12（参照侧）；复验时间：2026-09-12（UAT 重建后；Review A/B 返工腿复跑于 uat-l0061c）
- 方法：双端同 body PUT 建仓 → GET 回读逐字段比对；别名族逐端点双端对拍
- 测试资产：双端 l006a*/l006a2*/l006b*/l006d*（仓/权限目标/用户）已删净（参照侧 l006a-lay 因创建即 400 本不存在）

## 1. 票 A：configJSON 四域 round-trip（D02-R03+R04）

### 1.1 逐字段表（PUT 建仓带四域 → GET 回读）

| 域 | rclass | 参照 :8082 回读 | BinFlow :8083 回读（修复后） | 判定 |
|---|---|---|---|---|
| repoLayoutRef | local | 顶层回显 `maven-2-default`（所设值） | `maven-2-default`（configuration 内，所设值） | SAME（值）|
| blackedOut | local | 回显 true | 回显 true | SAME |
| maxUniqueSnapshots | local | 回显 7 | 回显 7 | SAME |
| archiveBrowsingEnabled | local | 回显 true | 回显 true | SAME |
| 四域 | local POST 更新（全字段新值） | 全部更新 | 全部更新（simple-default/false/3/false） | SAME |
| repoLayoutRef | remote | 回显所设值 | **修复前丢弃 → 现回显** `ivy-default` | SAME（已闭合）|
| blackedOut | remote | 回显 true | **修复前丢弃 → 现回显** true | SAME（已闭合）|
| maxUniqueSnapshots | remote | 回显 7 | **修复前丢弃 → 现回显** 7 | SAME（已闭合）|
| archiveBrowsingEnabled | remote | 回显 true | **修复前丢弃 → 现回显** true | SAME（已闭合）|
| repoLayoutRef | virtual | 回显所设值 | **修复前丢弃 → 现回显** `simple-default` | SAME（已闭合）|
| blackedOut / maxUniqueSnapshots / archiveBrowsingEnabled | virtual | **参照自身丢弃**（GET 键缺失） | 键缺失（照抄参照，不实现） | SAME（照抄留证）|

### 1.2 默认值表（裸建仓、不带四域）

| 域 | rclass | 参照默认 | BinFlow（修复后） | 判定 |
|---|---|---|---|---|
| repoLayoutRef | local | `maven-2-default`（generic/npm/docker 同值）| 本地 passthrough 不注入默认（键缺失）| DIVERGENT（既定 local 形态，D02-R02 已裁 compatible 的嵌套/无默认族）|
| repoLayoutRef | remote | `maven-2-default` | `maven-2-default`（canonical 注入）| SAME（已闭合）|
| blackedOut / maxUniqueSnapshots / archiveBrowsingEnabled | remote | false / 0 / false | false / 0 / false（canonical 常在）| SAME（已闭合）|
| repoLayoutRef | virtual | 键缺失（无 xsd 默认）| 键缺失（omitempty）| SAME |

### 1.3 非法值形态

| 输入 | 参照 | BinFlow | 判定 |
|---|---|---|---|
| maxUniqueSnapshots: -1 | 接受，回读 -1 | 接受，回读 -1 | SAME |
| maxUniqueSnapshots: "seven" | 500（Java 转换器报文）| 400（decode 报文带字段名）| DIVERGENT（BinFlow 全域统一 decode 400 姿态，非四域特例；建议登记裁量）|
| blackedOut: "yes" | 接受且转为 true | 400 | DIVERGENT（commons-lang 布尔宽容为 Java 库癖性；不建议复刻）|
| repoLayoutRef: 未知布局名 | 400 `Unable to find repository layout by the name: <n>` | 接受并存读 | DIVERGENT（K73：BinFlow 无布局注册表，参照 `/api/repo_layouts` 在本实例亦 404；已定谳 presentation-only）|

### 1.4 既存（非本票引入）差异记录

- **更新合并语义**：参照 POST 更新省略字段=保留存量（remote 四域实测 keep）；BinFlow remote 更新=全量替换回落默认（hardFail/missed 等全字段同族，修复前已如此）；BinFlow local 更新=调用方 blob 整体替换。**族级既有差异，建议另立票裁决**（影响全部 remote 字段，非四域特例）。

## 2. 票 B：经典路径别名（D04-R17+R18 / D06-R17）

### 2.1 别名路径全清单核定（gap-endpoints §4 + 反编译 SecurityResource.java / RestSecurityRequestHandler.java:527-570 / PermissionTargetConfigurationImpl + 活体；Review A 返工后按方言分记）

| 路径 | 动词 | 参照 | BinFlow 挂载 | 判定 |
|---|---|---|---|---|
| `/api/security/permissions` | GET | 200 `[{name,uri}]`（admin）| 200 `[{name,uri}]`（security:read；uri 为 contextUrl+转义名）| SAME（值/形）|
| `/api/security/permissions/{name}` | GET | 200 v1 形（letters 动作/扁平 patterns）| 200 v1 形（r/w/n/d/m；includes 空→`**`）| SAME（letter 集序差为 normalize 级——参照 HashSet 无序）|
| `/api/security/permissions/{name}` | GET 未知名 | 404 errors-envelope "Not Found" | 404 纯文本 | DIVERGENT（错误体裁，本面既定纯文本姿态）|
| `{name}` GET 路径段非法转义（如 `%zz`）| GET | 400（JAX-RS 层拒）| 404（withNameUnescaped 解码失败原样透传→查找落空）| DIVERGENT（留痕；非正常客户端面）|
| `{name}` PUT——**v1 方言 body**（`repositories` 键+扁平 pattern 串+letters）| PUT | 201；详情回读 r/mxm/d/w/m/n（zzz、x 静默清位）| **201**；回读 r/w/n/d/m（mxm 无席位静默弃）| SAME（核心）；残余=**mxm**（参照真有 managedXrayMeta 位，BinFlow 无 xray 席位——台账条目）|
| `{name}` PUT——BinFlow 富方言 body（`repos`+数组 patterns+words）| PUT | （参照不识 `repos` 键→repositories null→400 missing）| 201（混合接受臂：别名共存，双拼写不一致 400）| BinFlow 超集臂（富方言在经典面额外可用——超集差，登记）|
| `/api/security/permissions/{name}` | PUT 路径名≠body 名 | 409（专属文案）| 409（同文案逐字）| SAME |
| `{name}` PUT body 无名 | PUT | 201（反编译证实：isNotBlank 跳过 409→以 entityKey 建/换目标；NameValidator 恶意名 400）| 201（路径键先于族-4 门注入——B1 并集不可绕过，负例钉死）| SAME（BinFlow 未复刻 NameValidator/XSSValidator 恶意名 400——台账 residuals）|
| `{name}` PUT repositories 缺失/空数组 | PUT | 400 双文案（missing. / must contain at least one）| 400 同双文案逐字 | SAME |
| `{name}` PUT 未知仓/未知用户 | PUT | 400 `…non-existing repository 'x'.` / `…non-existing user: 'x'.` | 400 同文案逐字 | SAME |
| `{name}` PUT admin 为主体 | PUT | 400 `The user: 'admin'' has admin privileges, and cannot be added to a Permission Target.`（注意参照文案本身的双单引号）| 接受并建权（BinFlow 权限模型无此禁令）| DIVERGENT（台账 residuals；BinFlow 模型级差异）|
| `/api/security/permissions/{name}` | POST | **400 `{"errors":[{400,"Bad Request"}]}`（addon 层 updateSecurityEntity 只认 users/groups，permissions 落空臂）** | 400 同体（errors envelope "Bad Request"；门=admin 读族，对齐参照 isAdmin 前置）| SAME（Review B 返工后挂载——原「死动词不挂」表述过强已废）|
| `/api/security/permissions/{name}` | DELETE | 200 纯文本 `Successfully deleted permission Target 'x'` | 204（与现行 v1 面 handler 同体）| DIVERGENT（记录）|
| `/api/permissions`（任务书原述） | GET | 404（不存在该拼写）| 404 | SAME（真实 v1 路径为 `/api/security/permissions`）|
| `/api/system/backup[/{key}]`、`/api/system/backups` | GET/PUT/DELETE | **全 404——参照无此 REST 面**（唯一 backup 端点=POST `/api/system/storage/backup?key=`，BinFlow 已挂）| 404 | SAME（D06-R17 的「经典别名」前提证伪）|

### 2.2 D06-R17 核定结论

参照（活体+反编译双证）不存在 `/api/system/backup/{key}` 配置 CRUD 族：公开 /api 面仅有 storage 触发器；备份配置 CRUD 只在 UI 内部面（`artifactory-rest-ui/.../backups/BackupResource.java`，cfx 平面）。BinFlow 的 `/api/v1/system/backups` 为超集（BinFlow-native 面）。**无别名可挂——照「证据优先不猜」处理，建议 matrix 行按此复核。**

## 3. matrix 翻绿建议（回报，不直改）

| 行 | 现态 | 建议 | 依据 |
|---|---|---|---|
| D02-R03（PUT 建仓）| partial | → compatible | 四域三 rclass round-trip 闭合（§1.1/1.2）；残余=族级更新合并语义（§1.4，另票）|
| D02-R04（POST 更新）| partial | → compatible（带 §1.4 注记）或维持 partial 待合并语义票 | 同上 |
| D04-R17（v1 列表）| partial | → compatible | 别名挂载 + `{name,uri}` 形（§2.1）|
| D04-R18（v1 详情/PUT/DELETE）| partial | → partial 降残余 或 compatible（裁量）| GET/PUT(v1 方言)/POST/DELETE 四动词已挂、双文案与 409 逐字同；残余=DELETE 204-vs-200 文本、未知名错误体裁、admin 主体 400、mxm 席位四项（均 §2.1 留痕）|
| D06-R17（备份族）| partial | 复核重分类：别名前提证伪 | 参照无此面（§2.2）；BinFlow `/api/v1/system/backups` 为超集 |
