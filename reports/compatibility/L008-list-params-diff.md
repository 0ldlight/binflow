# L008-3 差分报告：?list 参数族 as-built 核对（D01-R05 / 票 D 前段）+ users 404 体裁探针（Review A 候选）

- 模式：**dual**（参照活体 + BinFlow UAT 活体，无金样降级）
- 参照：http://localhost:8082/artifactory（Artifactory Pro 7.161.20，addons 全开）
- BinFlow：http://localhost:8083/binflow（UAT 容器 binflow-ga）
- 取证时间：2026-09-12（双端同窗活体对拍；矩阵连续两轮复跑结论稳定）
- **UAT revision 偏差声明**：派发口径为重建至 6e12c20f；实测容器载 `uat-l0072-c1193f5f`（revision `c1193f5f`，L007-2 构建）。等价性论证：`git diff c1193f5f 6e12c20f -- internal/ cmd/` 仅触 `httpapi/{permissions,security,router}.go`（L007-1 的 permissions/users 字段集），**不触 storage 列表面**（storage.go 无 diff）→ 任务 1 全部结论对 6e12c20f 成立；任务 2（users 404）路径 `writePlainError(404, "User not found")` 在两 revision 逐字相同（c1193f5f 中 3 处、6e12c20f 同）→ 结论同样成立。偏差本身已回报 conductor（见工作日志 Risks）。
- 夹具：difftest 专用仓 `l0083-loc`（双端同建同删），树 `f0.txt / d1/{f1,f2}.txt / d1/d2/f3.txt / d1/d2/d3/f4.txt / d1/d2/d3/d4/f5.txt`（d1 下 4 层深度，内容逐字节同，sha1/sha2 双端一致已验）；参照侧经 `PATCH /api/metadata/{path}` body `{"props":{...}}` 给 `f0.txt`/`d1/f1.txt` 设属性 `l008p`（BinFlow 无该写面，见 §6 越界发现——属性侧前提不对称为已知，不影响参数读否的判定）。
- 归一：时间戳值（双端时钟不同）、uri 主机前缀（8082/artifactory vs 8083/binflow）。

## 1. 参照 as-built 语义（活体 + 反编译双证）

反编译锚：`ArtifactResource`（参数全经 `getQueryParameterAsInt` 整数解析）→ `RestAddonImpl.writeStreamingFileList`（布尔化 `==1`）→ `LocalRepoFileListTreeStreamer`/`BaseFileListHelper`（条目构造）。行为规格句式归纳：

1. **参数触发**：`?list` 存在即触发（值无关，`list=` 空值同）；`deep`/`depth`/`listFolders`/`mdTimestamps`/`statsTimestamps`/`includeRootPath`/`includePropertiesMd5` 七参一律按整数解析；**非数值 → 400 errors-envelope `For input string: "abc"`**（Java parseInt 文案逐字）。
2. **deep**：`deep=1` → 递归；**非 "1" 值（0/2/true）= 不递归**（按整数≠1 处理）。
3. **depth**：**仅修饰递归**——无 deep=1 时任意 depth 值均无递归效果（depth=99 仍只列直接子文件）；deep=1 时 depth=N 把递归钳到 N 层、depth≤0（含缺省 0）= 不限。即「depth 单独不递归、deep=1 下 depth 生效」。
4. **listFolders=1**：文件夹行进入 files[]——`uri` **无尾斜杠**（`/d2`）、`size: -1`、`folder: true`、无 sha；与文件行按字母序混排。缺省/0：**任何深度都不出现文件夹行**。
5. **mdTimestamps=1**：条目（文件与文件夹）增 `mdTimestamps: {"properties": <属性最后修改时刻>}`；无属性条目整键缺席。
6. **includePropertiesMd5=1**：有属性条目增 `propertiesMd5: <md5>`（实测 f1 `d12a08186cb2ff8dd009ef9f3bc8089d`）；无属性条目整键缺席。
7. **statsTimestamps=1**（票面清单外的同族第 7 参）：文件条目增 `mdTimestamps: {"artifactory.stats": max(lastDownloaded, remoteLastDownloaded)}`，从未下载则缺席。
8. **includeRootPath=1**：被查询文件夹自身作为**首个条目**出现，`uri: "/"`、size -1、folder true。
9. **条目 uri 形态**：一律**前导斜杠**（`/f1.txt`）；顶层 `uri` 无尾斜杠（`.../d1`），仓库根为 `.../l0083-loc`。
10. **created**：**请求时刻墙钟**（连续两请求 +74ms 漂移实测，非文件夹创建时刻）。
11. **Content-Type**：`application/vnd.org.jfrog.artifactory.storage.FileList+json`。
12. **仓库根可列**：`?list` 打在仓库根返回 200（直接子文件，本夹具 `/f0.txt`）；400「Cannot list files of root」仅在完全无仓库段时。深层组合（deep+listFolders+includeRootPath）在根上同样成立（`/` + 全树）。
13. **文件目标**：400 errors-envelope `Expected a folder but found a file, at: l0083-loc:d1/f1.txt`（repo:path 冒号拼写）。
14. 排序：全树字母序（`/d2/d3/d4/f5.txt` 在 `/d2/d3/f4.txt` 前）。

## 2. 32 臂矩阵（目标 d1 除注明外；「同集」= 条目集合一致但携带 §4 形态差）

| 臂 | 查询 | 参照 | BinFlow | 判定 |
|---|---|---|---|---|
| A01 | `?list` | 200 直接子文件 | 200 同集 | 同集（形态差） |
| A02 | `?list&deep=1` | 200 全树纯文件 | 200 全树 **+3 行文件夹行泄漏**（`d2/`、`d2/d3/`、`d2/d3/d4/`，size 0 尾斜杠形） | **DIVERGENT** |
| A03 | `?list&deep=0` | 200 不递归 | 200 同集 | 同集 |
| A04 | `?list&depth=1` | 200 depth 单独无效=直接子 | 200 同集 | 同集 |
| A05 | `?list&depth=2` | 200 仍直接子（depth 无 deep 不递归） | 200 **递归至第 2 层** + `d2/` 泄漏 | **DIVERGENT** |
| A06 | `?list&depth=3` | 200 直接子 | 200 递归 3 层 + 泄漏 | **DIVERGENT** |
| A07 | `?list&depth=4` | 200 直接子 | 200 全树 + 泄漏 | **DIVERGENT** |
| A08 | `?list&depth=0` | 200 无效值忽略 | 200 忽略（n≤0 回退缺省） | 同集 |
| A09 | `?list&depth=abc` | **400** envelope `For input string: "abc"` | 200 静默忽略 | **DIVERGENT** |
| A10 | `?list&depth=99` | 200 直接子 | 200 全树递归 | **DIVERGENT** |
| A11 | `?list&deep=1&depth=2` | 200 钳 2 层（f1,f2,d2/f3） | 200 **不限层全树** | **DIVERGENT** |
| A12 | `?list&listFolders=1` | 200 增文件夹行 `/d2`（size -1） | 200 参数未读，无文件夹行 | **DIVERGENT** |
| A13 | `?list&listFolders=0` | 200 纯文件 | 200 同集 | 同集 |
| A14 | `?list&deep=1&listFolders=1` | 200 全树+文件夹行（`/d2` 形） | 200 全树+文件夹行（`d2/` 形）——集合巧合重合，成因相反（bf 并非读了参数） | **DIVERGENT**（条目 uri 形） |
| A15 | `?list&mdTimestamps=1` | 200 f1 增 `mdTimestamps.properties` | 200 参数未读无该键 | **DIVERGENT** |
| A16 | `?list&listFolders=1&mdTimestamps=1` | 200 文件夹行+属性时刻 | 200 两者皆无 | **DIVERGENT** |
| A17 | `?list&deep=1&listFolders=1&mdTimestamps=1` | 200 全树+两参生效 | 200 仅 deep 语义（泄漏形） | **DIVERGENT** |
| A18 | `?list&includeRootPath=1` | 200 首条目 `/`（folder true） | 200 参数未读无根条目 | **DIVERGENT** |
| A19 | `?list&listFolders=1&includeRootPath=1` | 200 `/`+`/d2`+文件 | 200 均无 | **DIVERGENT** |
| A20 | `?list&deep=1&includeRootPath=1` | 200 `/`+全树 | 200 无根条目 | **DIVERGENT** |
| A21 | `?list&includePropertiesMd5=1` | 200 f1 增 `propertiesMd5` | 200 参数未读无该键 | **DIVERGENT** |
| A22 | `?list&deep=1&includePropertiesMd5=1` | 200 同上（全树） | 200 无 | **DIVERGENT** |
| A23 | 五参全开 | 200 `/` 首条+文件夹行（size -1）+mdTimestamps+propertiesMd5 全生效 | 200 仅 deep 泄漏形，四参全无 | **DIVERGENT** |
| A24 | `?list`（文件 d1/f1.txt） | 400 envelope `Expected a folder but found a file, at: l0083-loc:d1/f1.txt` | 400 envelope `Cannot list files of a file 'l0083-loc/d1/f1.txt'.` | **DIVERGENT**（文案） |
| A25 | `?list&deep=1`（文件） | 同 A24 | 同 A24 | **DIVERGENT**（文案） |
| A26 | `?list`（仓库根） | **200** 直接子文件（`/f0.txt`） | **400** `Cannot list files of root.` | **DIVERGENT** |
| A27 | `?list&deep=1&listFolders=1&includeRootPath=1`（根） | 200 `/`+全树含文件夹行 | 400 | **DIVERGENT** |
| A28 | `?list=`（空值） | 200 同 A01 | 200 同集 | 同集 |
| A29 | `?list&listFolders=true` | **400** envelope `For input string: "true"` | 200 静默忽略 | **DIVERGENT** |
| A30 | `?list&statsTimestamps=1`（加成臂：先下载 f1） | 200 f1 增 `mdTimestamps.artifactory.stats`（下载时刻） | 200 无该键 | **DIVERGENT** |
| A31 | `?list&deep=2` | 200 非 1 值=不递归 | 200 同集（deep≠1 回退） | 同集 |
| A32 | `?list&statsTimestamps=abc`（加成臂） | 400 envelope `For input string: "abc"` | 200 | **DIVERGENT** |

**判定分布**：同集 6（A03/A04/A08/A13/A28/A31）/ DIVERGENT 26 / skipped 0。**逐字节级 SAME=0**——所有 200 臂均携带 §4 形态差。

## 3. BinFlow as-built 缺参清单（票 D 前段交付）

代码面（`internal/httpapi/storage.go handleStorageList`，c1193f5f 与 6e12c20f 同）仅读 `deep`/`depth` 两参；`listFolders`/`mdTimestamps`/`includeRootPath`/`includePropertiesMd5` 在活体 API 全无读者（仅 `internal/migrate/reader.go` 迁移读面引用）。

**（a）未实现参数（5）**

| 参数 | 参照语义 | BinFlow as-built |
|---|---|---|
| `listFolders` | 文件夹行入列（`/d2` 形、size -1、字母序混排） | 完全未读；且反向病——不带该参也泄漏嵌套文件夹行（见 b-1） |
| `mdTimestamps` | 条目增 `mdTimestamps.properties`（属性修改时刻） | 完全未读；且属性修改时刻无存储跟踪（依赖面，见 §5） |
| `includeRootPath` | 被查文件夹自身为首条目 `/` | 完全未读 |
| `includePropertiesMd5` | 有属性条目增 `propertiesMd5` | 完全未读；需属性规范序列化 md5（等值性规格问题，见 §5） |
| `statsTimestamps`（同族发现） | 文件条目增 `mdTimestamps.artifactory.stats` | 完全未读（BinFlow 已有 ?stats 读面，数据基础在） |

**（b）已实现但语义差（3）**

| 面 | 参照 | BinFlow | 差 |
|---|---|---|---|
| b-1 deep 递归的文件夹行 | 无 listFolders 时任何深度零文件夹行 | deep/depth 递归时**嵌套文件夹行泄漏**（`d2/` 等尾斜杠形、size 0）——代码里「跳过直接子文件夹行」守卫因尾斜杠计入层数而永不命中 | 已实现面输出参照不存在的条目（BUG 级） |
| b-2 depth 独立递归 | depth 单独不递归 | depth 单独即递归（A05-A07/A10） | 递归触发条件相反 |
| b-3 deep=1 下 depth | deep=1&depth=N 钳 N 层 | deep=1 完全忽略 depth（互斥式 if/else） | 修饰语义变互斥 |

**（c）其余行为差（矩阵副产物）**：仓库根可列（ref 200 vs bf 400）；非数值参数校验（ref 400 envelope `For input string: "x"` vs bf 静默 200）；文件目标 400 文案（`Expected a folder but found a file, at: repo:path` vs `Cannot list files of a file 'repo/path'.`）。

## 4. 形态级差异（作用于所有 200 臂）

| 维度 | 参照 | BinFlow |
|---|---|---|
| 条目 uri | 前导斜杠 `/f1.txt`；文件夹行无尾斜杠 `/d2` | 无前导斜杠 `f1.txt`；文件夹行尾斜杠 `d2/` |
| 顶层 uri | `.../api/storage/l0083-loc/d1`（无尾斜杠；根=仓 key 无尾斜杠） | `.../api/storage/l0083-loc/d1/`（尾斜杠） |
| created | 请求时刻墙钟（毫秒，逐请求推进） | 文件夹创建时刻（静态） |
| Content-Type | `application/vnd.org.jfrog.artifactory.storage.FileList+json` | `application/json` |
| lastModified/created 精度 | 毫秒真实值 | 秒级（`.000Z`）——M16 已定谳的 audit 派生族拼写沿用 |

## 5. 补参票面建议（实现归 LOOP 009，供 conductor 派发）

- **P0（已实现面输出错数据，客户端立刻可见）**：① 修 b-1/b-2/b-3 递归语义族——无 listFolders 时零文件夹行、depth 单独不递归、deep=1 下 depth 钳层；② 条目 uri 形态改前导斜杠 + 文件夹行去尾斜杠 + 顶层/根 uri 去尾斜杠。
- **P1（纯列表形状参数，无存储依赖）**：listFolders=1（文件夹行 size -1、字母序混排）、includeRootPath=1（首条目 `/`）。
- **P2（参数+元数据依赖，需先定规格）**：mdTimestamps=1（需属性修改时刻跟踪）；includePropertiesMd5=1（需属性规范序列化的 md5——**跨系统等值性是规格问题**：客户端若用该值做迁移校验则必须逐字节同，需 compatibility-engineer 先向 reverse-engineer 提属性序列化格式规格票）；statsTimestamps=1（?stats 读面数据已在，只差入列）。
- **P3（校验与体裁）**：七参非数值 → 400 errors-envelope `For input string: "<v>"`；仓库根放开列表（200 直接子文件 + 组合参成立）；文件目标文案改 `Expected a folder but found a file, at: <repo>:<path>`；created 改请求时刻；Content-Type 改 vendor media type。
- matrix 翻态建议：D01-R05 维持 partial（本轮差分把 partial 的证据面从「未核对」收敛为「五参未实现+三处递归语义差」，confidence medium→high）；known-divergence 建议新立一条 `rest/storage-list-params-family`（classification: UNSUPPORTED_FEATURE，五参未读）+ 一条 `rest/storage-list-recursion-semantics`（classification: BUG，b-1/b-2/b-3）——由 compatibility-engineer 裁定入账，本报告为 authority: differential 证据。

## 6. 附录 A：users 404 体裁探针（Review A 候选，任务 2）

`GET /api/security/users/nosuchuser-l0083`（双端 admin 凭据，未知名）：

| 端 | status | Content-Type | body（逐字） |
|---|---|---|---|
| 参照 :8082 | 404 | `application/json` | `{"errors":[{"status":404,"message":"Not Found"}]}` |
| BinFlow :8083 | 404 | `text/plain; charset=utf-8` | `User not found` |

**结论：分歧实锤**——体裁（envelope vs 纯文本）+ 文案（泛化 `Not Found` vs 具名 `User not found`）双差；钉死的纯文本即代码 `writePlainError`（security.go，GET 与 PUT 单用户两处同拼写）。注意参照文案是**泛化 Not Found**（与未知路径 404 同文案），非具名——若 BinFlow 补 envelope，文案应为 `Not Found` 而非直译现有具名文案。
**台账立账建议**：`rest/users-v1-get-unknown-style`，classification: BUG（体裁面，与 L007-1 §3 裁量记录的「本面 400 系纯文本姿态」同族——该裁量当时只豁免 400 载体，404 未知名臂 L007-1 已按台账升级为 envelope，此处 GET 单用户 404 是同族漏网腿），authority: { type: differential, ref: 本报告 §6 }，review_gate: 并入 users 面体裁统一票（建议与 L007-1 §3 残差 1 同票收口）。

## 7. 附录 B：越界发现（仅记录，不立案不修）

1. BinFlow 无属性写面：`PATCH /api/metadata/{path}` 404、`PUT /api/storage/{path};p=v` 矩阵形 404（参照矩阵形返回 400「Properties value cannot be empty」——其活体有效写面为 metadata PATCH `{"props":{...}}`）。属性写面属独立缺面，供 compatibility-engineer 排查存量矩阵覆盖。
2. 空 `?properties` 读面：无属性时参照 404 envelope「No properties could be found.」vs BinFlow 200 `{"properties":{}}`——一行记录，非本票域。

## 8. 复跑与清理

- 复跑门：矩阵脚本两轮连续执行，32 臂 status 判定逐臂一致（run2 摘要 `ref=200 bf=200` ×23 + 6 个非 200 臂分布不变）；无 flake。
- 证据留存：本报告内联各臂判定与逐字证据（A16/A21/A30 字段级、A09/A24/A26/A29/A32 错误体裁、users 404 双端三行）；原始响应双端全量 32 臂暂存 /tmp/l0083-cases/（本机取证过程件，报告落库后随清理删除——判定可由 §1 语义 + 本报告内联证据反向复现）。
- 实例清理：双端 `l0083-loc` 仓 DELETE（见下）；无用户/权限目标/制品残留；/tmp/l0083-* 全删；凭据未落盘（BinFlow 口令运行时自 .env.uat 读入环境变量，未写入任何文件或本报告）。
