# L010-1 差分报告：?list P2 三参（mdTimestamps/includePropertiesMd5/statsTimestamps）落地 + 合并复验（32 臂 + 延期单臂四组 + 复核两项）

- 模式：**dual**（参照活体 + BinFlow UAT 活体）
- 参照：http://localhost:8082/artifactory（Artifactory Pro 7.161.20，addons 全开）
- BinFlow：http://localhost:8083/binflow（UAT 容器 binflow-ga，**重建至工作树**：HEAD f219e987 + L010-1 P2 改动〔internal/httpapi/storage.go+测试〕+ 并行轨 WIP〔L010-2 httpapi 微票四件、L010-3 规格刷新文档〕——构成快照存 /tmp/l0101-build-tree.txt，逐文件归属在该文件 git status/diff --stat）
- 取证时间：2026-09-12（双端同窗活体对拍；矩阵两轮复跑结论稳定）
- 夹具：`l0101-loc`（双端同建同删），树 `f0.txt / d1/{f1,f2}.txt / d1/d2/f3.txt / d1/d2/d3/f4.txt / d1/d2/d3/d4/f5.txt`；属性：`d1/f1.txt = {p1:[v1,v2]}`、文件夹 `d1 = {fk:[fv]}`（双端同集——BinFlow 用 FR-89.2 查询面 `PUT ?properties=`，参照同面；参照文件夹 PUT 递归泄漏见 §6-b）；`d1/f1.txt` 双端各内容 GET 一次
- 归一：时间戳值（双端时钟不同）、uri 主机前缀；比较 = JSON 语义深比较 + Content-Type（400 臂连 body 一起归一比较）

## 1. 跨面等值性规格（实现前置取证，:8082 活体 2-3+ 臂/参）

### 1.1 mdTimestamps=1

| 维度 | 参照实测 |
|---|---|
| 字段位置 | 条目内 `sha2` 之后、`propertiesMd5` 之前；map 键序字典序（`artifactory.stats` < `properties`） |
| `properties` 键值 | **属性最后变更时刻**（毫秒 ISO8601 `…Z`）；实测随每次属性写移动（f1 首写 11:12:47 → 复写 11:13:16 → 值=11:13:16.819），非上传时刻、非节点 lastModified |
| 出现条件 | 条目**携带属性**才出现（文件与文件夹行同规则）；includeRootPath 的 `/` 条目 = 被查文件夹自身携带属性时同样携带（实测 d1 有属性 → `/` 条目带 mdTimestamps+propertiesMd5） |

### 1.2 includePropertiesMd5=1 —— 哈希算法已复刻（非 UNKNOWN）

**算法（黑盒推导，10 臂验证）**：`propertiesMd5 = md5( ⋈ sorted(keys) × sorted(values) of key‖value )` —— 键升序、值升序、键值直接拼接、**无任何分隔符**。

| 属性集 | 参照 md5 | 验证 |
|---|---|---|
| {p1:[v1]} | c03a74d41225e1c1f65df743e0e49da3 | md5("p1v1") ✓ |
| {p1:[v1,v2]} | e215d43d0a83274a703cca40aa24ce25 | md5("p1v1p1v2") ✓ |
| {a:[1,2]}（写入 2,1） | 9bcd5749ac4d63dbe9f7b314a64b35c9 | = md5("a1a2")——值序规范化为升序 ✓ |
| {pb:[x]} | 569f7a090087f9e7071a247a24ea0809 | md5("pbx") ✓ |
| {pa:[y],pb:[x]}（插入序 pb→pa） | e2dc06da45abc3107844d9baa22accf0 | = md5("paypbx") ✓——键序规范化为升序（插入序 md5("pbxpay") 不匹配） |
| {b:[2]}+{a:[1]} 合并 | f2e49af795161e14acf9d9245473a368 | = md5("a1b2") ✓ |
| {fk:[fv]} 文件夹行 vs 文件行 | aa648e111e3e2e2be4d0bd736d2a1348（**同值**） | 无路径/仓盐 ✓ |
| {a:[1]} / {fk:[fv]} / {p1:[v1]} | 8a8bb7cd… / aa648e11… / c03a74d4… | ✓ |

**跨系统等值性证明**：UAT 重建后同一属性集双端 digest 逐字节相同（§2 A21/A22/A23 + 夹具对账：`/d1`、`/d1/f1.txt` 双端同 digest）；单元测试以参照 digest 字面量钉死（TestStorageListP2PropertiesMd5）。
排序口径注记：键按 String 升序（Java 自然序 = ASCII 字节序 = Go sort.Strings，非 ASCII 键的 UTF-16/UTF-8 序差为理论角，未取证——低风险）。

### 1.3 statsTimestamps=1

| 维度 | 参照实测 |
|---|---|
| 键 | `mdTimestamps.artifactory.stats`，值 = `max(lastDownloaded, remoteLastDownloaded)`（实测 = ?stats 的 lastDownloaded 逐毫秒一致：1789213405500 ↔ 11:43:25.500Z） |
| 出现条件 | 文件条目**至少一次下载**才出现；从未下载文件缺席；**文件夹行永不携带**；与 mdTimestamps=1 并用时同 map 双键（A23） |

## 2. 32 臂矩阵全量重跑（A01-A32，L008-3 原矩阵）

**32/32 归一 SAME**（两轮复跑同分布，无 flake）。余 7 臂全部收敛：A15/A16/A17（mdTimestamps.properties）、A21/A22（propertiesMd5）、A23（双键+includeRoot 组合）、A30（artifactory.stats）——含 Content-Type（vendor FileList+json）与 400 臂 body 归一比较全同。

## 3. 延期单臂四组（L009 Review A/B 返工后活体合并复验）

| 组 | 臂 | 结果 |
|---|---|---|
| 空值/空白（B01-B09） | `deep` 裸参 / `deep=` / `depth=` / `listFolders=` / `includeRootPath=%20` / `depth=%20%20` / `mdTimestamps=` / `statsTimestamps` 裸参 / `includePropertiesMd5=%20` | **9/9 SAME**（200 平列表，空值视缺席） |
| int32 界（C01-C06） | `depth=2147483648` / `-2147483649` → 400 `For input string: "<v>"` 逐字；`2147483647` / `-2147483648` → 200；`mdTimestamps=2147483648` / `statsTimestamps=-2147483649` → 400 同文案 | **6/6 SAME** |
| XSS 四角 | `</x>`=201、`</>`=400（envelope 逐字）、`<b>`=201、`<em>`=400；换行角参照路由层 404 不可达（%0A 段不可路由——L009-3 已注记，BinFlow 侧源型锚定：在内拒/在前收，测试钉死） | **4/4 SAME**（可达角） |
| NameValidator 三字面量 | body name `.` / `..` / `&` → 400 `Name cannot be empty link: '<name>'` 逐字；`a&b` → 409（非精确匹配）；entity-key 斜杠（`n1%2Fn2` 无名 body）→ 201 | **全 SAME** |

## 4. 复核一：root includeRootPath `/` 条目 lastModified 形态

- 参照实测：**真实毫秒时间戳**——仓库根 = 存储根目录自身 mtime（仓重建后 = 首次物化时刻 12:07:07.145Z，不随后续子项变动移动）；子文件夹目标 = 被查文件夹自身 lastModified。
- BinFlow 原状：仓库根 `/` 条目渲染 **1970-01-01T00:00:00.000Z**（根无 node 行、空值经 isoMillisUTC 落零时刻；上轮 A26/A27 归一 SAME 系时间戳值归一掩盖）。
- **已修**：仓库根 `/` 条目取 repo 行 CreatedAt（真实"根随仓生"戳，查询无关性成立——不随 deep 参数漂移）；子文件夹臂维持 node.UpdatedAt（空时回退 CreatedAt）。修后 A26/A27 双端形态一致（真实 ISO8601 毫秒戳；BinFlow 秒级 `.000Z` 为既定 audit 派生族拼写，值归一）。

## 5. 复核二：DELETE permissions 未知名两态（台账定级门）

**双端两态逐字 SAME，无分歧**：

| 态 | 参照 :8082（7 臂） | BinFlow :8083 |
|---|---|---|
| 从未建成（never-1/2 + 早前 never-a~d） | 404 envelope `{"errors":[{"status":404,"message":"Not Found"}]}` | 404 envelope 同 |
| 建成→删除 | 200 `Successfully deleted permission Target 'l0101-gone'`（纯文本） | 200 同文案 |
| 已删再删 | 404 envelope `Not Found` | 404 envelope 同 |

**与 L009-3 观察冲突**：L009-3 记「参照 DELETE 未知名 200（car-l009 从未建成仍 200）」**不可复现**（本轮 7 臂 404 一致，含重启后实例）。差异假说：该观察取于参照实例崩溃前的异常态（本轮取证中该实例发生一次 DB 连接级 wedge + 重启，见 §6-c）。**定级建议**：「DELETE 未知名 200」候选**不立账**（当前双端零分歧）；L009-3 观察登记为不可复现异常态记录。若后续再观察到 200 形态再立账并附实例状态。

## 6. 新发现（越界记录，仅立账建议）

- **(a) 元数据面计数污染（BUG 候选，阻 A30 于脏实例上收敛）**：BinFlow `GET /api/storage/{path}`（item-info）与 `?properties` GET/PUT/DELETE 均经 `storageNode → ReposSvc.Get` **计入 downloadCount/lastDownloaded**。隔离探针（UAT 活体）：新文件上传后 stats=0 → `PUT ?properties` 后 =1 → `GET ?properties` 后 =2 → item-info GET 亦 +1；参照侧属性已写已读的 f2 无 artifactory.stats（A30 参照臂）。根因：计数器随内容面 Get 直乘（ADR-0044 K69「direct」臂的附带伤害）；`?stats` 面已有 statsNode 非计数解算先例。**建议立账** `rest/storage-metadata-reads-count-downloads`（classification: BUG；authority: differential，本报告 §6-a）；修复方向 = 元数据面换非计数解算（List-based 精确匹配臂 + 各面拼写回退语义；virtual 解算语义需单独验证）。本轮夹具以「属性只放在有下载的 f1 与文件夹 d1」规避，矩阵 32/32 不受影响。
- **(b) 参照属性写面递归怪癖（信息记录）**：参照 `PUT /api/storage/{folder}?properties=` 默认**递归应用**到全部后代（实测 fk 泄漏至 5 文件 + 3 文件夹行；多键逗号形 400 `Properties value cannot be empty.`、重复键段 400）。BinFlow FR-89.2 为显式 `recursive=1`（决策 11.40 族既有裁量）——供 properties 契约排差参考，非本轮立案。
- **(c) 参照实例完整性事件**：取证中段 :8082 发生 DB 连接 wedge（HikariCP closed-connection 级联、Access gRPC 全超时，~17 分钟不自愈）→ `docker restart artifactory`（数据卷未动）→ ~11 分钟启动恢复，夹具逐文件核对无损失。重启后行为与本报告全部结论同窗取证。

## 7. 翻绿/立账建议（归 compatibility-engineer 裁定）

1. `rest/storage-list-params-family`（UNSUPPORTED_FEATURE 五参未读）→ **resolved**：七参全实现（P0+P1 = L009-2，P2 三参 = 本轮），evidence = 本报告 §1-§3。
2. `rest/storage-list-recursion-semantics` → resolved（L009-2 证据 + 本轮 32 臂复跑）。
3. matrix D01-R05 → compatible（32/32 归一 SAME 两轮）。
4. **新立** §6-a 计数污染 BUG 账。
5. §5 DELETE 两态：不立账；L009-3 观察 ≠ 可复现分歧。

## 8. 复跑与清理

- 复跑门：矩阵两轮连续 47/47（A×32+B×9+C×6）归一 SAME 逐臂一致；延期组与复核组双端逐字核对。
- 实例清理：双端 `l0101-loc`（bf `deleteContent=true`）、参照 `l0101-ev`、双端权限目标 `l0101-pn`/`n1<b>n2`/`n1</x>n2`/`n1/n2`/`l0101-gone-b` 全删；/tmp/l0101 过程件随报告落库删除；凭据运行时自 .env.uat 读入环境变量，未落任何文件或报告。
