# 仓库语义行为规格 — M1：Local 仓库

> 逆向基线：`o.a.a.repo.service.RepositoryServiceImpl`、`o.a.a.repo.db.DbStoringRepoMixin`、`o.a.a.repo.RealRepoBase`、`o.a.a.engine.UploadServiceImpl/DownloadServiceImpl`、`o.a.a.io.checksum.policy.LocalRepoChecksumPolicy`。
> 官方参照：JFrog "Repository Configuration"（local repo 字段表）。
> 置信度：`高` 双证 / `中` 仅代码 / `低` 推断。

## 1. 路径解析（repoKey + path 的推导规则）

来源：`ArtifactoryRequestBase#calculateRepoPath`、`RestUtils#calcRepoPathFromRequestPath`。

| 规则 | 规格 | 置信度 |
|---|---|---|
| 切分 | 请求路径第一段（首个 `/` 前）= repoKey，其余 = path | 高 |
| 尾斜杠 | `repo/a/b/` → path `a/b` 且标记 isFolder=true（PUT 建目录、GET 目录列表的依据） | 高 |
| matrix 参数 | `repo/a/b.txt;k=v` → path `a/b.txt`，`;k=v` 收进属性集；repoKey 段也可带（`repo;k=v/...`）并同样剥离 | 高 |
| 特殊前缀 | 首段为 `list`（NuGet v2 装饰）或 `simple`（Docker/Debian 装饰）时跳过后再取 repoKey。M1 Generic 不涉及 | 中 |
| dot-segment | `/./`、`/.` 归一化移除；`..` 处理未在走读范围内（BinFlow 建议 Go 侧 `path.Clean` 并拒绝逃逸出 repo 根） | 中（`#removeDotSegments` 只见 `.` 处理） |
| URL decode | path 与 repoKey 均 UTF-8 decode；`+` 保护为字面加号（先 `%2B` 转义再 decode） | 高 |
| 老元数据后缀 | `<path>:properties` / `:statistics` 剥离并把请求标记为元数据操作 | 高 |
| zip 内路径 | `<file.jar>!<inner/path>`（`!` 分隔）拆出 zipResourcePath，M1 不实现 | 中 |

## 2. 请求校验顺序（PUT 上传链，`assertValidDeployPathAndPermissions`）

1. repo 存在性 / 是否 local（virtual 未配 local deployment → 405+`Allow: GET`）。
2. `assertValidPath`：
   - `blackedOut=true` → 拒绝（BlackedOutException，经默认 RepoRejectException 状态 **404**）。**中**（文案确定，最终状态码经 `sendError(var5.getErrorCode())`，`RejectedArtifactException` 未覆写 getErrorCode → 基类默认 404；SnapshotPolicyException 显式 409，推断 BlackedOut 走 404）
   - Maven 系 release/snapshot 开关（Generic 忽略）。
   - includesPattern / excludesPattern 匹配（默认 `**/*` / 空 → 全放行）。**高**
3. 权限：canDeploy（文件）/ canAnnotate（元数据）→ 403；匿名 → 401 挑战。
4. 覆盖检查（`assertOverwrite=true` 路径，见 §3）。
5. 存储配额 → 413。

## 3. 覆盖 / 禁止覆盖语义

来源：`DbStoringRepoMixin#shouldProtectPathDeletion/assertOverride/isFileOverwrite`。

| 场景 | 行为 | 置信度 |
|---|---|---|
| 路径不存在 | 直接新建 | 高 |
| 路径已存在，客户端带 checksum 且与服务端既有 checksum **相同** | 视为幂等重传：**不触发覆盖权限检查**（`differentChecksums` 均 false → isFileOverwrite=false），无 deploy 权限也可完成——除非 `enforcePermissionCheckOnIdenticalChecksumDeploy=true`（默认 false） | 高（代码逐条件可见） |
| 路径已存在且 checksum 不同（或未带 checksum） | 需要对旧节点有 **DELETE 权限**才能覆盖；否则 403（`Not enough permissions to delete/overwrite all artifacts under '<path>' (user: '<u>' needs DELETE permission).`） | 高 |
| 旁车 checksum 文件（`.sha1` 等）与 maven-metadata.xml | 永不触发覆盖检查（可自由重写） | 高 |
| 覆盖时的保留 | `sendOverwritesToTrashcan=true`（默认）→ 旧版本先拷入回收站再覆盖 | 高 |
| 覆盖后的元数据 | `created/createdBy` 保留首次值，`modified/modifiedBy/updated` 更新；旧 checksum 引用关系清除（`clearOldFileData`） | 高 |

**BinFlow 实现要点**：M1 无完整 ACL 时，至少实现「同 checksum 幂等重传免覆盖检查 + 不同 checksum 视为覆盖」的骨架，权限位接 admin/token 即可。

## 4. 删除语义

来源：`RepositoryServiceImpl#undeploy/undeployInternal`、`webdav.methods.DeleteMethod`、`ArtifactDeleteFailStatus`。

| 场景 | 行为 | 置信度 |
|---|---|---|
| 成功（文件或目录树） | **204** 无 body；目录删除后自动 prune 空父目录（多事务模式下） | 高 |
| 路径不存在 | 404 `Could not locate artifact. Path: '<repoPath>'` | 高 |
| repo 不存在 | 404 `Could not find storing repository by key: '<key>'` | 高 |
| 无 DELETE 权限 | 403 `Not enough permissions to delete/overwrite ...`（hideUnauthorizedResources=true 时伪装 404 `Could not locate artifact...`） | 高 |
| 删除仓库根（`DELETE /repo`） | 非 project-admin → 403 `No permissions to delete content of repository. This requires admin privileges...` | 高 |
| 内部系统目录 | 404（伪装不存在） | 中 |
| 回收站联动 | 删除前复制到 `auto-trashcan`（见 storage-layout.md §5；trashcan.enabled=true 时） | 高 |
| blob | 从不直接删；交 GC | 高 |
| `atomic` 参数 | `?atomic=true` 单事务（全成或全败）；默认多事务（部分成功可能残留） | 中 |

## 5. checksum 策略（local 仓库专属）

来源：`LocalRepoChecksumPolicyType`（枚举）、`LocalRepoChecksumPolicy#verify`。

repo 配置字段 `checksumPolicyType`，两个合法值：

| 值 | 上传时 | 读取时（ETag/X-Checksum 头、REST checksums 字段） | 置信度 |
|---|---|---|---|
| `client-checksums`（默认） | 客户端带的 checksum 与服务端实测**必须一致**，否则 `409`（`Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact '<repoPath>'. Checksums info: ...`）；未带则服务端计算后接受 | 优先返回客户端声明值（original），缺失回退 actual | 高 |
| `server-generated-checksums` | 客户端值不匹配也接受，以服务端实测为准 | 返回服务端实测值 | 高 |

- 校验只对「客户端显式提供了 checksum」的类型执行；没提供的类型跳过。
- 三种算法（sha1/sha256/md5）逐一独立校验。
- `.sha1` 旁车文件登记的不匹配：`client-checksums` → 409；`server-generated-checksums` → 静默接受（详见 rest-api.md §1.5）。
- 下载时 `binaries` 表 actual 值即响应头 `X-Checksum-*` 与 ETag 的来源。
- **制品不可变性**：同一 repo path 的已存文件只能被「覆盖」或「删除」，没有 in-place 修改；blob 层内容寻址天然不可变。**高**

## 6. 其它 local 配置字段对 M1 的行为影响

| 字段 | 默认 | M1 行为 | 置信度 |
|---|---|---|---|
| `includesPattern` / `excludesPattern` | `**/*` / 空 | Ant 风格通配；命中排除或不含于包含 → 上传 404 拒绝（`Rejected by include/exclude patterns` 类文案）。BinFlow 建议实现 `**`、`*` 两级 | 高（默认值）/ 中（拒绝状态码：走 assertValidPath → RepoRejectException 默认 404） |
| `blackedOut` | false | true 时拒上传（404，见 §2）；下载侧对 blacked-out repo 的文件列表请求返回 404 | 中 |
| `handleReleases` / `handleSnapshots` | true/true | 仅 Maven 系语义；Generic 忽略 | 高 |
| `archiveBrowsingEnabled` | false | 影响 Content-Disposition 注入（见 rest-api.md §1.4）；M1 可忽略 | 中 |
| `propertySets` | 空 | 属性校验集；M1 不实现 | — |
| `notes` / `description` | 空 | 纯展示 | 高 |

## 7. remote / virtual（占位 — M3 补）

> 以下仅留章节骨架，内容到 M3 逆向 `o.a.a.repo.http.HttpRepo`、`o.a.a.repo.virtual.VirtualRepo`、`o.a.a.repo.cache.*` 后补齐。

- 7.1 remote 仓库：pull-through 代理、缓存失效（miss 期 / ttl）、SSRF 防护边界、`blackedOut`/`allowRemoteDownload` 联动。
- 7.2 virtual 仓库：成员解析顺序（`priorityResolution`）、local deployment repository 指定、聚合浏览语义。
- 7.3 remote-cache 的存储复用：cache repo 在 `nodes.repo` 中以 `<remoteKey>-cache` 键存在（推断，待验证）。

## 8. 与官方文档的差异 / 补充

- 「同 checksum 幂等重传不触发覆盖权限检查」：官方文档未记载（只有代码可见），对 CI 重试场景行为关键 → **本条为反编译补充官方规范**。
- BlackedOutException 最终 HTTP 状态（404 vs 409）：官方文档无记载；本规格标注中置信度待动态验证。
- 回收站属性名（`trash.*` 五项）：官方 UI 有展示但字段名未见文档，属代码补充。

## 待验证清单

1. blacked-out repo 上传的确切 HTTP 状态（404 推断自 RepoRejectException 默认值，未动态复现）。
2. includesPattern 不匹配时的确切状态码与文案（走读确认走 404 链路，文案未逐字捕获）。
3. `DELETE /{repoKey}`（仓库根，非空仓库）在 admin 下的行为分支（全删 or 需二次确认——REST 层 `DELETE /api/repositories/{key}` 才是删仓库；此处仅删内容，需与 repo 删除区分）。
4. remote-cache 仓在 DB 中的 repoKey 命名（`<key>-cache`）。
