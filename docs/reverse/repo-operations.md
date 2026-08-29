# repo-operations 制品操作族行为规格（copy / move / flat / 目录 zip / `archive!/` / explode）

> 逆向来源：`reverse-src/artifactory`（batch1-core 资源类 + batch3-addons 实现类）× 官方文档（docs.jfrog.com 五页）双证。
> 消费者：T-339（copy/move 核心）、T-343（归档族）。锚点：主矩阵 §C copy/move 行 + 归档族三行（artifactory-full-feature-matrix.md L103/104/105/118/131）。
> 版本注记：反编译产物为 7.x 新版（jakarta 命名空间 + entitlement 模型）；官方文档 2026-03 更新版。

---

## 0. 端点 / 操作总表

| # | 方法 | 路径 | 关键参数 | 成功 | 失败 | 置信度 |
|---|---|---|---|---|---|---|
| 1 | POST | `/api/copy/{srcRepoKey}/{srcFilePath}` | `to`（必填，`/{targetRepoKey}/{targetFilePath}`）、`dry`(0/1, 默认0)、`suppressLayouts`(0/1, 默认1)、`failFast`(0/1)、`atomic`(字符串布尔，**文档未载**) | 200 + CopyOrMoveResult 流式 JSON | 400/401/403/409（§2.4） | 高（形状+参数+200/400/401/403 双证；`atomic` 仅代码，中） |
| 2 | POST | `/api/move/{srcRepoKey}/{srcFilePath}` | 同上 | 200 + 同上 | 同上 | 高（同上） |
| 3 | POST | `/api/flat/copy/{srcRepoKey}/{srcFilePath}` | `to`、`dry`、`failFast` | 200 + 同上 | 默认整端点 **404**（系统开关默认关，见 §5） | 中（仅代码；官方文档**无此端点**） |
| 4 | POST | `/api/flat/move/{srcRepoKey}/{srcFilePath}` | 同上 | 同上 | 同上 | 中（同上） |
| 5 | GET | `/api/archive/download/{repoKey}/{path}` | `archiveType`（**必填**，zip/tar/tar.gz/tgz）、`includeChecksumFiles`(布尔，默认false) | 200 + 归档字节流（Content-Type 按类型） | 400/401/403/404（§4.2 逐条消息） | 高（双证；四限参错误消息仅代码，中） |
| 6 | GET | `/api/archive/download/{repoKey}`（整仓形态，文档写 path 用空串） | 同上 | 同上 | 同上 | 高（代码双路径 + 文档「use empty string for repository root」） |
| 7 | GET | `/{repoKey}/{archivePath}!/{entryPath}`（内容面，非 /api） | 无查询参数（矩阵参数属性照常适用） | 200 + 成员字节 | 404（成员不存在/读失败）、403（权限/无 Filtered addon）、400（§4.1） | 高（路径形状与「`!` 后必须跟 `/`」双证；错误消息细节仅代码，中） |
| 8 | PUT | `/{repoKey}/{archivePath}` + `X-Explode-Archive: true` | 头 `X-Explode-Archive`（必填 true）、`X-Explode-Archive-Atomic`(可选) | **201 空体**（V-1 已定案 2026-08-29 T-343：规格优先级裁决 文档 > 反编译隐式默认；BinFlow as-built 201 + `X-Binflow-Exploded-Files: <n>` 计数头〔BinFlow 新增〕） | 400（白名单外扩展名/缺文件名/坏归档）、403（无部署权限） | 高（头与类型集双证；成功码定案见 V-1） |
| 9 | POST | `/api/archive/buildArtifacts` | 体：buildName 必填 + buildNumber XOR buildStatus、archiveType(zip/tar/tar.gz/tgz)、mappings | 200 + 归档流（计流量） | 400（校验消息 §4.4）、404（无构建产物） | 中（仅代码；属 build-info 域，本票只登记不展开） |

- 认证面：#1~#6、#9 资源类标注 `RolesAllowed({admin, user})`——匿名可达性取决于实例匿名策略 + 路径权限（官方文档：copy/move「Requires a privileged user (can be anonymous)」；folder zip「privileged user with read permissions」；#7「user with 'read' permission (can be anonymous)」；#8「user with 'deploy' permissions (can be anonymous)」）。置信度高。
- #7 的 `archiveBrowsingEnabled`（仓库配置项）**不门控** `archive!/` 成员读取——该开关只影响浏览器直开行为（Content-Disposition，见 rest-api.md）。成员读取仅受路径读权限约束。中置信（代码路径全量检索无门控点；负向断言待 live 复核）。

---

## 1. copy/move 核心语义（T-339 主消费面）

### 1.1 请求解析与路径归一（高，代码+文档双证）

1. 源路径 `{srcRepoKey}/{srcFilePath}` 逐段解析；查询源 item 元数据，**若源是目录且路径不带尾斜杠，自动补 `/`**（目录 → 树级操作）。
2. 目标 `to=/{targetRepo}/{targetPath}`：源是目录时目标同样补尾斜杠（「搬目录」语义）；源是文件时原样。
3. `dry=1` → dryRun；`suppressLayouts=1`（默认）→ 不做跨布局路径翻译；`failFast=1` → 任一 warning/error 即停。
4. 参数校验失败的消息（逐字）：源 repo key 空 → 400 `Source repository key is empty`；`to` 缺失 → 400 `Target repository key is empty`；源路径空 → warning `Source repository path is empty, path set to root`（继续执行，算仓根）。

### 1.2 目标仓资格预检（先于逐项校验；消息逐字，高置信）

| 条件 | 结果 |
|---|---|
| 目标 repo 是 remote | 400 `Target repository <key> is a remote repository. copy/move to remote repositories is not allowed.` |
| 目标 repo 是 virtual | 400 `Target repository <key> is a virtual repository. copy/move to virtual repositories is not allowed.` |
| 目标是 remote 缓存仓 | 400 `Target repository <key> is a cache repository. copy/move to cache repositories is not allowed.` |
| 目标 repo key 不存在 | 400 `Could not calculate repo path from src=…, target=…: repository <key> not found…` |
| 源 == 目标（同 RepoPath） | 400 `Skipping copy/move <path>: Destination and source are the same` |
| 目标路径以 `.jfrog` 开头且系统开关开（默认**开**） | 404 `Internal metadata request blocked`（`.jfrog`/`.jfrog/**` 全部视为内部元数据） |
| 源 item 不存在（典型路径） | 400（文档口径）；代码存在 warning 分支 `Could not find item at <path>`（见 V-2） |

- 内部系统身份（`_system_`，如 trash can 回收/恢复链）豁免 remote/virtual 目标检查——BinFlow 若复用 move 底座做回收站，需等价的内部身份豁免 seam。中置信。

### 1.3 逐项校验链（树级递归中每个文件/目录都过一遍；顺序即优先级）

| # | 检查 | 失败码 | 消息（逐字摘要） |
|---|---|---|---|
| 1 | 源读权限（canRead） | 403 | `User doesn't have permissions to read '<src>'. Needs read permissions.` |
| 2 | 文件 × 目标 local 仓 snapshot/release 策略 | 400 | `The repository '<key>' rejected the path '<p>' due to a conflict with its snapshot/release handling policy.` |
| 3 | 目标仓 include/exclude patterns | 403 | `…due to a conflict with its include/exclude patterns.` |
| 4 | move 专用：源删除权限（canDelete） | 403 | `User doesn't have permissions to move '<src>'. Needs delete permissions.` |
| 5 | 目标已存在且用户无删除权限 | **401** | `User doesn't have permissions to override '<target>'. Needs delete permissions.`（注意是 401，非 403） |
| 6 | 目录搬到已存在的**文件**下 | 400 | `Can't move folder under file '<target>'.` |
| 7 | 目标不存在且用户无写权限（canDeploy） | 403 | `User doesn't have permissions to create '<target>'. Needs write permissions.` |
| 8 | Release Bundles 仓进出（非系统身份） | 400 | `Moving or Copying from and to a Release Bundles repositories are not allowed.` |

置信度：高（#1/#4/#5/#7 与文档 401/403 档位一致，消息逐字来自代码）。#5 的 401 是文档未载的行为补充。

### 1.4 搬运行为

- **二进制零拷贝**：按 sha1/sha256/md5+length 复用既有 binary（dedupe；系统开关 `copy.artifact.use.nodes.checksums.enabled` 默认 true 走节点校验和直挂）。
- **元数据随行**：文件 info（created/createdBy/modified/modifiedBy）、校验和、**node properties 全量复制**（与 M10/T-317 属性系统、推送复制 syncProperties 同族语义；统计（下载次数）**不**随行）。
- **目录**：浅拷目录节点 + 目录属性；递归子项。
- **unix 式目标调整**（REST 路径默认开）：目标是已存在**目录**时，目标自动变为 `<target>/<源名>`（cp/mv 进目录语义）；目标是已存在文件时，文件级**覆盖**（搬运前先删旧目标，trash can 语义由删除链决定）。
- **move = copy + 删源**：逐文件「拷→删」；目录在子项搬空后删除（`pruneEmptyFolders` 开关语义）；源空目录清理。
- **目标空目录回收**：源目录有子项但全部被拒/跳过时，已建的目标空目录会被删掉（不留孤儿）。
- **copy 触发 Maven 元数据重算**（异步任务，按候选目录集合）；move 不触发。协议仓索引重算不在通用 mover 里——由各包型的存储拦截器 afterCopy/afterMove 钩子驱动（BinFlow 对应「各 adapter reindex 链联动」，PRD 105.2 对齐）。中-高置信。
- **`suppressLayouts=0`（布局翻译开）**：源/目标仓 layout 不同时改走跨布局搬运——按模块坐标（groupId/artifactId…）在目标 layout 下重排路径；此模式下 atomic 被强制 true。`atomic=false`（仅 suppressLayouts=1 时可生效）→ 多事务模式（逐项独立事务，允许部分成功）。中置信（跨布局细节属 LayoutsCoreAddon，未展开）。
- **dryRun**：零副作用；逐项走完整校验链，冲突/权限问题以 error 消息（带 400 码）出现在返回体，但整体 HTTP 状态仍 200（除非有 error → 见 §1.5）；规模以「N artifacts and M folders were copied/moved」消息形式回报（dry 前缀 `Dry run for `）。
- **failFast=0（默认）**：出错继续搬其余子树；结果体是全部 message 的聚合。

### 1.5 响应组装（高置信，代码+文档媒体类型双证）

- 成功（无 error 无 warning）：200，体 `{"messages":[{"level":"INFO","message":"copying <src> to <dst> completed successfully, <N> artifacts and <M> folders were copied"}]}`（dry 模式前缀 `Dry run for copying…`；move 用 moving/moved）。
- 有 error：HTTP 状态 = 最后一条 error 携带码（>0 时），否则 **409** 兜底；体 = `{"messages":[{"level":"ERROR"|"WARN","message":…}, …]}`（全部 error 在前、warning 在后，流式输出）。
- Content-Type：`application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`（或 application/json）。
- level 取值为 logback 枚举名：`ERROR`/`WARN`/`INFO`/`DEBUG`。中置信（大小写来自代码；Artifactory live 验证仍开放——V-3 维持）。**BinFlow as-built（T-339）按代码证据取大写实现并测试固化**；T-343 未触及 copy/move 面，无新增证据。

### 1.6 flat 端点（#3/#4）真实语义 —— **与 PRD「扁平化」措辞有出入，需 PM/T-339 知悉**

- 官方文档**无** `/api/flat/*`（undocumented）；系统开关 `flat.copy.move.rest.api.disabled` 默认 **true** → 默认部署直接 404。中置信（代码；无文档对照片）。
- 实际语义（代码推导，非「拍平到单层」）：AQL 驱动的**批量事务化**搬运——
  - 目标规则（rsync 式尾斜杠语义）：`to=/dst/bar`（无尾斜杠）→ 源根目录名**丢弃**，根以下相对子路径原样保留（`foo/a/b.jar` → `bar/a/b.jar`）；`to=/dst/bar/`（尾斜杠）→ 源根目录名保留（→ `bar/foo/a/b.jar`）。**子目录层级不拍平**。
  - 事务分批：`flat.copy.move.bulk.size` 默认 10000 项/事务；逐项过校验器链（basic/读权限/move 删权限/include-exclude/maven/配额）；move 完成后清理源目录树。
  - 附加能力（REST 面未暴露）：逐项加/减属性钩子、`exclude.local.generated`（默认 true，跳过 `.jfrog` 等本地生成物）——trash can 恢复链复用此引擎。
- **对 BinFlow 的含义**：不实现 `/api/flat/*` 与 Artifactory 默认部署兼容（对端默认 404）；PRD 105 的「flat 模式」若指布局扁平化，属 BinFlow 自有扩展，规格上应与 Artifactory flat 端点解耦命名，避免兼容性误判。**建议 PM 回写措辞**。

---

## 2. 目录/整仓 zip 下载（T-343 消费面）

### 2.1 配置（folderDownloadConfig，config 描述符 + 模板双证，高置信）

| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | **false** | 总开关；关时一切路径 403 `Download Folder functionality is disabled.` |
| `enabledForAnonymous` | **false** | 匿名单独开关；匿名且关 → 401 `You must be logged in to download a folder or repository.` |
| `maxDownloadSizeMb` | 1024 | 超限 400（消息含实际 MB 两位小数，逐字模板见 §2.2） |
| `maxFiles` | 5000 | 超限 400 |
| `maxConcurrentRequests` | 10 | 并发信号量；满 → 400 `There are too many folder download requests currently running. Try again later.` |
| `enabledEmptyDirectories` | false | 归档是否包含空目录 |

配置热更新（reload）即时生效，包括并发数 resize。

### 2.2 执行序与错误（顺序即代码执行序，中-高置信）

1. 匿名门（§2.1）→ 401。
2. `archiveType` 解析：缺省/非法 → 400（枚举 zip/tar/tar.gz/tgz）。
3. 仓资格：repo 不存在 → 404 `<key> is not a repository.`；非 local/非 cache → 404 `Downloading a folder or a repository's root is only available for local (or cache) repositories`；仓 blackout → 对应错误码。
4. 路径资格：非根路径不存在 → 404 `Path '<p>' does not exist, aborting folder download`；路径是文件 → 400 `Path '<p>' is not a folder, aborting folder download`。
5. 读权限 → 403 `You don't have the required permissions to download <path>.`。
6. 总开关（§2.1 enabled）→ 403。
7. 预统计（size/fileCount）→ 超限 400：`Size of path '<p>' (<实际>MB) exceeds the max allowed folder download size (<N>MB).` / `Number of files under the path '<p>' (<n>) exceeds the max allowed file count for folder download (<N>).`。
8. 并发槽 → 400（§2.1 消息）。
9. 流式打包（边走边写，不落盘）；`includeChecksumFiles=true` 时把 `.sha1/.md5/…` 边文件一并打入（按仓 checksum 策略）；流量计 DOWNLOAD 条目（BinFlow 对应既有流量/审计面）。
- 例外：`jfrog-support-bundle` 仓豁免总开关与大小/数量限制（仅并发槽仍生效）。中置信。
- Xray 阻断（企业面）→ 403 `Path '<p>' contains blocked artifacts by Xray`——BinFlow 无 Xray，仅登记。

---

## 3. `archive!/` 归档内成员读取（T-343）

### 3.1 路径语法与解析（高置信）

- 形态：`GET /{repoKey}/{path>/<name>.<ext>!/{entryPath}`；**`!` 后必须紧跟 `/`**（官方文档明示）。
- 解析：以**首个** `!/` 切分——前半=归档制品路径，后半=成员路径；成员路径内再遇 `!/` 视为**嵌套归档**递归下钻（`a.zip!/dir/b.zip!/f.txt` 合法）。
- 点段（`/./`）归一化默认开启严格模式：`strictArchiveDotSlash`（`request.strict.archive.dot.slash`，默认 **true**）——归档扩展名之后的 `./` 段不被折叠（`a.zip/./x` 保义），且 `a.zip` 扩展名集合取自 mimetypes 配置的 archive 扩展全集。归档扩展名集（mimetypes.xml archive="true"）：zip 家族（zip/nupkg/conda/jar/war/jar.pack.gz）、tar 家族（tar/tgz/tar.gz/gz/bz2/tar.bz2）、xz 家族（xz/tar.xz/nar.xz）、7z、apk。高置信（模板+代码）。
- 矩阵参数属性部署（`;k=v`）在归档路径与成员路径上照常生效（属性挂到归档制品节点）。

### 3.2 服务行为

- **纯流式**：不落盘、不解包；从归档字节流顺序定位 entry，边读边算成员 sha1/md5/size，响应即成员字节（Content-Type 按成员文件名 mime 探测）。
- 成员不存在 → 404 `Unable to find zip resource: '<entry>' using full URI '<uri>'`；流读取失败 → 404 `Failed to get zip resource: '<path>'`。
- **成员 checksum 请求**：`…!<entry>.sha1|.md5|…` → 返回按需计算的成员校验和（非归档文件的）。中置信。
- 权限：只需归档路径的读权限（成员无独立 ACL）；无 Filtered Resources 能力（OSS 档）→ 403 `Direct resource download from zip requires the Filtered resources add-on.`（官方文档页未标 Pro——文档遗漏，代码为凭，见 §6）。
- 下载流量按 DOWNLOAD 计（成员 size）。

---

## 4. exploded archive 解包上传（T-343；M10 E-25 断言反转的落点）

### 4.1 触发与白名单

- 触发头：`X-Explode-Archive: true` **或** `X-Explode-Archive-Atomic: true`（任一即进入解包分支；Atomic 走全有或全无路径）。
- 扩展名白名单（`artifactory.request.explodedArchiveExtensions`）：
  - **代码默认值（开箱生效，ConstantValues）**：`zip,tar,tar.gz,tgz,7z,tar.bz2,xz,tar.xz,nar.xz`；
  - 官方文档口径：`zip, tar, tar.gz, tgz`（四类）；system.properties 模板注释行同四类。
  - **分歧处置**：BinFlow PRD 105.3 已按四类闭集锚定（zip,tar,tar.gz,tgz）——四类为两证交集，其余五类仅代码默认值可见。维持 PRD 四类，白名单外 400。白名单外/无扩展名的拒绝消息（逐字）：`Unsupported archive extension: '<ext>' of possible extensions : '<全集>'` → 400。
- 缺文件名（PUT 路径以 `/` 结尾）→ 400 `Explode archive deployment failed, Missing file name.`。

### 4.2 执行语义

- 上传字节先落临时目录（`to_extract_<父路径>_<时间戳>_<文件名>`），解压到临时文件夹，逐文件部署到 `PUT 路径的父目录 + 归档内相对路径`，**归档文件本身不落库**；完成后临时物清理。
- 部署排除项：系统文件（`.jfrog` 等）、全局 excludes、文件名含 `maven-metadata.xml` 的条目（跳过并记 debug，不报错）。
- 权限：对目标路径的部署权限（canDeploy）→ 403 `User is not authorized to deploy to specified repo path.`
- 并行度：`artifactory.explode.archive.threads` 默认 1（顺序部署；>1 时先非 checksum 后 checksum 两批并行）；超时 `explode.archive.timeout.minutes` 默认 60 分钟。官方文档注记 7.96.3 起支持并行。
- 失败映射：Illegal（扩展名等）→ 400；流/IO → 404 `Explode archive deployment failed. View log for more details.`；statusHolder 有错 → 按 mapper 取码（500 兜底）。
- 成功响应码：官方文档 **201 Created**；反编译代码成功路径仅 flush（响应基类默认 200，未显式设 201）。**V-1 定案（2026-08-29，T-343）**：按规格优先级「官方文档 > 反编译隐式默认」定 **201 + 空体**——与 BinFlow 自有 PUT 201 惯例一致；无活体 Artifactory 可达（环境无实例），按文档定案在此留痕，live 实测值若异再翻转。BinFlow as-built：201 空体 + `X-Binflow-Exploded-Files: <n>` 计数头（BinFlow 新增）。
- 成功体：空（无 JSON）。

---

## 5. 配置/系统开关键集（本域全量）

| 键 | 默认 | 作用 | 置信度 |
|---|---|---|---|
| `folderDownloadConfig.*` 六字段 | §2.1 | 目录 zip | 高 |
| `artifactory.request.explodedArchiveExtensions` | 代码 `zip,tar,tar.gz,tgz,7z,tar.bz2,xz,tar.xz,nar.xz` / 文档四类 | explode 白名单 | 中（分歧见 §4.1） |
| `artifactory.explode.archive.threads` / `.timeout.minutes` | 1 / 60 | explode 并行与超时 | 高（代码+文档 threads 双证） |
| `artifactory.request.strict.archive.dot.slash` | **true** | `archive!/` 点段严格模式 | 中（仅代码） |
| `artifactory.move.copy.block.internal.metadata.target.enabled` | true | `.jfrog` 目标阻断（404） | 中（仅代码） |
| `artifactory.flat.copy.move.rest.api.disabled` | **true** | flat 端点整面 404 | 中（仅代码） |
| `artifactory.flat.copy.move.bulk.size` | 10000 | flat 事务分批 | 中 |
| `artifactory.flat.copy.move.check.interceptors` / `.full.revert.on.abort` | true/true | flat 拦截器/中断全回滚 | 低（仅常量名，行为未展开） |
| `artifactory.flat.copy.move.exclude.local.generated` | true | flat 跳过本地生成物 | 中 |
| `artifactory.copy.artifact.use.nodes.checksums.enabled` | true | copy 二进制挂接模式 | 中 |

---

## 6. Q4 license 门控取证（PRD 105.4 开放问题终裁输入）

**结论：Artifactory 把整个操作族全部列入 pro 档 entitlement，无一在 OSS 档。** 双证如下：

| 能力 | 官方文档口径 | 反编译 OSS 默认实现行为 | 置信度 |
|---|---|---|---|
| copy / move（#1/#2） | Copy/Move Item 页均注 `Requires Artifactory Pro` | REST 面经 Advanced REST addon（AddonType `rest`，枚举标 `pro`）分发；无 license 时抛 MissingRestAddonException → **400** text/plain `This REST API is available only in Artifactory Pro (see: jfrog.com/artifactory/features). If you are already running Artifactory Pro please make sure your server is activated with a valid license key.` | 高 |
| 目录 zip（#5/#6）+ buildArtifacts（#9） | `Requires Artifactory Pro` | 同上（RestAddon 默认实现 MissingRestAddonException → 400） | 高 |
| `archive!/`（#7） | 文档页**未标**（遗漏） | Filtered Resources addon（AddonType `filtered-resources`，`pro`）缺失 → **403** `Direct resource download from zip requires the Filtered resources add-on.` | 高（代码为凭，补官方文档空白） |
| explode（#8） | `Requires Artifactory Pro` | RestCoreAddon OSS 默认 → **400** `This REST API is available only in Artifactory Pro.` | 高 |
| flat（#3/#4） | 无文档 | 同 copy/move 走 RestAddon（pro），且默认另有 404 系统开关 | 中 |

**BinFlow 终裁建议（供 conductor/PM 上 BOARD 裁定）**：维持 PRD 105.4「暂行不门控」合理——①BinFlow pro 档语义对标的是「Artifactory 商业 entitlement 的能力集」，但 copy/move 是 Trash can（FR-106）与回收站恢复的共同底座（Artifactory 内部以 `_system_` 身份绕过门），门控会连带波及删除/恢复链；②本域在 Artifactory 属「license 商业化分层」而非能力边界，BinFlow 单产品档位模型（community/pro）已按能力对齐原则多处落地。**但须在主矩阵「Artifactory license 档」列回写 pro 标记**（当前该五行的门控标记为空——105.4 的「无 license 门标记在案」前提被本票推翻），并知会 PM 在 PRD 105.4 留痕。若终裁改为门控：`archive!/` 门控码应对齐 403（Filtered 语义），copy/move 族对齐 400（REST addon 语义），trash 链内部豁免。

---

## 7. 与公开规范的差异/补充汇总

- **文档未载、代码可见**（本规格补充项）：`atomic` 查询参数；`/api/flat/*` 端点及其默认 404；响应体 `messages[]` 结构与 level 枚举；逐项权限消息与 **401 override 码**；`.jfrog` 目标阻断 404；`includeChecksumFiles` 默认 false 之外的执行序与逐字错误模板；`jfrog-support-bundle` 豁免；explode 的临时目录/排除项/404 IO 码；`archive!/` 嵌套归档与成员 checksum 后缀；strictArchiveDotSlash；`archiveBrowsingEnabled` 不门控成员读取。
- **文档与代码分歧**：~~explode 成功码 201(文档)/200(代码)（V-1）~~ **已定案 201（文档优先，T-343 2026-08-29）**；explode 白名单四类(文档)/十类(代码默认)（§4.1 处置：维持 PRD 四类闭集）；`archive!/` Pro 标注缺失（文档遗漏）。
- **无 202/异步任务形态**：copy/move 是同步 200 逐消息流式返回——文档与代码双证均无 202+status 任务模型（大操作仅靠流式聚合与 failFast 缓解；Artifactory 的异步面在本域只有 maven 元数据重算/explode 并行线程等内部任务）。**票面提问的「202+status 形态」结论：不存在，勿实现。**（高置信）

---

## 8. 待验证清单（live 7.84.10 或升级版实测）

| # | 项 | 现有证据 | 验证法 |
|---|---|---|---|
| ~~V-1~~ | ~~explode 成功响应码 201 vs 200~~ **已定案（2026-08-29 T-343）**：文档优先 → 201；BinFlow as-built 201 空体（+ 计数头新增）。Artifactory live 实测仍开放（非阻塞，异值再翻转） | 文档 201 / 代码默认 200 | （保留）live 实例 `curl -X PUT -H 'X-Explode-Archive: true'` 观察状态码 |
| V-2 | 源不存在时 400 vs 200+WARN | 文档 400 / 代码存在 warning 分支 | copy 一个不存在路径 |
| V-3 | 消息 level 大小写（`ERROR` vs `error`） | 代码 logback 枚举 name() | 读任意 copy 失败响应体 |
| V-4 | `archive!/` 对 `.tar.bz2/.xz/.7z` 等扩展的实测支持面 | mimetypes 全集 vs 文档只提 zip 系 | PUT tar.bz2 后取成员 |
| V-5 | flat 端点在默认部署的可达性（404） | 代码常量默认 true | POST /api/flat/copy/… |
| V-6 | `archiveBrowsingEnabled=false` 时 `archive!/` 是否仍可用 | 代码无门控点（负向断言） | 关闭开关后取成员 |
| V-7 | dryRun 对 maven 元数据重算零触发 | 代码 copy 成功路径才入候选 | dry=1 后对比 maven-metadata.xml mtime |
