# Conan 包型（v1+v2 修订链协议）行为规格

- 票据：T-284（FR-91.1 覆盖集第 1 份）；M11 Conan 适配票的前置规格。
- **公开规范锚点**：Conan 官方没有独立的 server wire 协议规范页；本规格以 ① Conan 官方 [Package Revisions 文档](https://docs.conan.io/1/versioning/revisions.html)（RREV/PREV 语义）+ ② GitLab Conan v2 API 文档（de-facto 公开实现规范，端点族与响应体形态）为准；反编译（`reverse-src/artifactory/src/batch2-protocol/{com/jfrog/ph/conan, org/jfrog/repomd/conan, org/artifactory/addon/conan}`，inv-3 §2.3 CONAN 行）补规范未写的空白（存储布局/索引文件/时间戳/错误码/管理端点），逐条标注「此条补充公开规范」。
- 置信度：`高` = 反编译 + 公开文档/实现双证；`中` = 仅反编译可见；`低` = 推断待验证。低置信条目不作验收依赖。

---

## 1. 基础路径形态

| 系统 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| BinFlow（实现口径，建议） | `$BASE/binflow/<repoKey>/v1\|v2/...` | 与 goproxy.md §1 同构：`/binflow` 前缀 + repoKey 第二段；Conan 协议自身路径全部以 `v1/`、`v2/` 开头，不与其它内容面冲突，无需额外路径段。**注意**：upload_urls/download_urls 类端点返回**绝对 URL**，服务端必须知道自身对外 base（配置项，勿只取 Host 头）。 | —（BinFlow 决策建议，待 ADR 确认） |
| Artifactory（参考对照） | `$BASE/artifactory/api/conan/<repoKey>/...` | 挂在 `/api/conan` 下（addon `ConanResource @Path("conan")`）。BinFlow 不沿用 `api/conan` 段。 | 高 |

- v1 与 v2 是**同一仓型上的两套端点族**，共用存储；v1 **数据面**仅 local 仓可用（remote/virtual 收到 v1 数据面请求 → 400 `Unsupported Conan v1 repository request for '<repoKey>'`）。**握手三端点（ping / users/authenticate / users/check_credentials）除外——类无关服务**：remote/virtual 上同样可达（能力头追加 `only_v2` 恰恰挂在这些类上，客户端只能从握手响应发现它；conan 2 的 remote login 硬依赖 authenticate）。高（代码 + conan 2.31.2 真机 remote login 实证——T-312 D1，2026-08-27 修订）。
- **BinFlow 决策记录**：Artifactory 7.x 有 v1→v2 布局迁移 job（`ConanV2MigrationJob`）与「未迁移则全端点 400」的闸门；BinFlow 是绿地实现，直接落 v2 布局，**无迁移 job、无闸门**。~~v1 端点按需子集实现~~ **M12 状态注记（2026-08-29）**：CN-1 终裁（BOARD 20:55）后 v1 **全量数据面已随 T-308 落地**（十七端点 + files 直传通道，conan 1.66 真机全链实证）——§10.1 的「不做完整 v1 数据面」为 M11 拆票前立场，已被终裁推翻。

## 2. 能力协商与认证（握手三端点是 conan 2 客户端的硬依赖）

握手三端点（ping / users/authenticate / users/check_credentials）**双前缀**：conan 2.x 客户端实测走 **`v2/users/authenticate`**（conan 2.31.2 活体，T-308 §3——原「必走 v1」口径修正，规格自注的社区 issue #17001 正是此事）；conan 1.x 走 v1。BinFlow as-built 双前缀全覆盖（{v1,v2}/ping、{v1,v2}/users/{authenticate,check_credentials}，T-308；T-312 起三类仓型同服务，见 §1）。

| 端点（相对 `$BASE/binflow/<repoKey>/`） | 行为 | 置信度 |
|---|---|---|
| `GET {v1\|v2}/ping` | 200 空体 + 能力响应头（下表）。未认证也需可达（客户端探测用）。 | 高 |
| `GET {v1\|v2}/users/authenticate` | 带 Basic 认证 → 200，**响应体纯文本即 token 字符串**（客户端后续以 `Authorization: Bearer <token>` 使用；GitLab 同型返回 JWT）。凭据无效 → 401。 | 高（代码 + GitLab 文档双证 + conan 2.31.2/conan 1.66 双客户端活体——v2 前缀用法为活体补充公开规范） |
| `GET {v1\|v2}/users/check_credentials` | Bearer/Basic 校验 → 200 空体；无效 → 401。 | 高 |

能力响应头（每个 conan 端点响应都会带；仅反编译可见，此条补充公开规范）：

| 响应头 | 值 | 置信度 |
|---|---|---|
| `X-Conan-Server-Version` | `0.20.0`（服务端自报的协议版本） | 中 |
| `X-Conan-Server-Capabilities` | `complex_search,checksum_deploy,revisions,matrix_params`；若该仓不支持 v1（remote/virtual）追加 `only_v2` | 中 |
| `X-Conan-Client-Version-Check` | 请求带 `X-Conan-Client-Version` 时返回：客户端 < `0.16.0` → `deprecated`；`< 0.20.0` → `outdated`；`= 0.20.0` → `current`；`> 0.20.0` → `server_outdated` | 中 |

认证门控：仓配置 `forceConanAuthentication=true`（或实例禁匿名）时，匿名请求任意 conan 端点 → **401**（v1/v2 数据端点逐端点断言）。高（代码）。

## 3. 端点表

### 3.1 v2 族（相对 `<conanBase>/v2/conans/`；conanBase = `$BASE/binflow/<repoKey>`）

下记 `<ref>` = `{name}/{version}/{user}/{channel}`（conan 2 的 user/channel 无值时用字面 `_`，官方客户端约定）。此条补充公开规范（GitLab 文档未覆盖 `_` 约定的服务端处理）：`_` 作为普通段存储，检索时按字面匹配。中。

| 方法 | 路径 | 语义 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|
| GET | `<ref>/latest` | 最新 recipe 修订 | 200 `{"revision":"<rRev>","time":"<ISO8601>"}` | 404（无修订） | 高（代码 + GitLab 双证） |
| GET | `<ref>/revisions` | 全部 recipe 修订（按时间降序，首个即 latest） | 200 `{"reference":"<name>/<ver>@<user>/<channel>","revisions":[{revision,time}...]}` | 无修订 404（`Couldn't find revisions`） | 高 |
| GET | `<ref>/revisions/{rRev}/files` | recipe 文件清单 | 200 `{"files":{"conanfile.py":{},...}}` | 404 | 高 |
| GET / HEAD | `<ref>/revisions/{rRev}/files/{path}` | recipe 文件内容 / 存在性 | 200 文件流（HEAD：200 + `Content-Length`） | 404 | 高 |
| PUT | `<ref>/revisions/{rRev}/files/{path}` | 上传 recipe 文件（conanfile.py、conanmanifest.txt、conan_sources.tgz…） | **201** 空体 | 400 非法路径 / 403 无写权限 / 401 匿名 | 高 |
| DELETE | `<ref>` | 删除整个 recipe（全部修订） | 200 | 404 / 403 | 高 |
| DELETE | `<ref>/revisions/{rRev}` | 删除单个 recipe 修订 | 200 | 404（`Couldn't find path '<path>'`） | 高 |
| GET | `<ref>/revisions/{rRev}/packages/{pid}/latest` | 该 packageId 最新 pRev | 200 `{revision,time}` | 404 | 高 |
| GET | `<ref>/revisions/{rRev}/packages/{pid}/revisions` | 全部包修订 | 200 `{"reference":"<ref>#<rRev>:<pid>","revisions":[...]}` | 404 空清单 | 高 |
| GET | `<ref>/revisions/{rRev}/packages/{pid}/revisions/{pRev}/files` | 包文件清单 | 200 `{"files":{...}}` | 404 | 高 |
| GET / HEAD | `<ref>/revisions/{rRev}/packages/{pid}/revisions/{pRev}/files/{path}` | 包文件（conaninfo.txt、conan_package.tgz…） | 200 流 | 404 | 高 |
| PUT | `<ref>/revisions/{rRev}/packages/{pid}/revisions/{pRev}/files/{path}` | 上传包文件 | 201 | 同 recipe PUT | 高 |
| DELETE | `<ref>/revisions/{rRev}/packages/{pid}/revisions/{pRev}` | 删除单包修订 | 200 | 404 / 403 | 高 |
| DELETE | `<ref>/revisions/{rRev}/packages` | 删除该 rRev 下全部二进制包 | 200 | 404（`Couldn't find packages for deletion`） | 高 |
| GET | `search?q=<query>` | 仓内 recipe 搜索（`*` 通配，匹配 `name/version@user/channel` 串；**尾缀 `/*` 是客户端的修订通配，服务端须剥离后匹配**——conan 2.31.2 活体：`conan list "ref/*"` 实发 pattern 含 `/*`，不剥离则 0 命中〔T-308 D2，此条补充公开规范〕） | 200 `{"results":["<ref>",...]}` | — | 高（活体补充） |
| GET | `<ref>/search?q=` | 该 recipe 全部 packageId 的元数据 | 200 `{"<pid>":{"settings":{...},"options":{...},"requires":{...},"recipe_hash":"..."} }` | — | 高 |
| GET | `<ref>/revisions/{rRev}/search?q=` | 限定 rRev 的同上 | 同上 | — | 高 |

### 3.2 v1 族（相对 `<conanBase>/v1/`；**仅 local**）

| 方法 | 路径 | 语义 | 成功/错误 | 置信度 |
|---|---|---|---|---|
| GET | `conans/search?q=` | recipe 搜索 | 200 同 v2 形态 | 高 |
| GET | `conans/<ref>/search?q=` | packageId 元数据（隐式 latest rRev） | 200 | 高 |
| GET | `conans/<ref>/digest` | recipe manifest 下载地址 | 200 `{"conanmanifest.txt":"<abs-url>"}` | 高 |
| GET | `conans/<ref>/packages/{pid}/digest` | 包 manifest 下载地址 | 同上 | 高 |
| GET | `conans/<ref>/download_urls` | recipe 各文件下载地址 | 200 `{"<file>":"<abs-url>",...}` | 高 |
| GET | `conans/<ref>/packages/{pid}/download_urls` | 包各文件下载地址 | 同上 | 高 |
| POST | `conans/<ref>/upload_urls`（body `{"<file>":<size>,...}`） | 换取各文件上传地址（PUT） | 200 `{"<file>":"<abs-url>",...}`；无写权限 403 | 高 |
| POST | `conans/<ref>/packages/{pid}/upload_urls` | 包文件上传地址 | 同上 | 高 |
| GET | `conans/<ref>` | recipe snapshot：文件名→md5 | 200 `{"conanfile.py":"<md5>",...}`（排除 `.timestamp`） | 高 |
| GET | `conans/<ref>/packages/{pid}` | 包 snapshot | 同上 | 高 |
| DELETE | `conans/<ref>` | 删除 recipe——**坐标根整树删（index.json + 全部修订）**：Artifactory 实现直接对 `getRecipePath(user/name/version/channel)`（无修订段）做 repoService.delete，权限门 = canWrite → 403；conan 1.66 参考实现同型（remove_conanfile → delete 整个 revisions 根，controller docstring 明言 "remove all revisions, packages and package revisions"）。~~latest 修订链~~（原直读 2026-08-29 推翻：Artifactory 代码 + 参考实现双证）。**BinFlow as-built（T-308 D8）按 latest 链实现（旧修订存活）——登记分歧，待翻转票（一行改动+断言）** | 200 / 404（canWrite 不过 → 403，代码补充） | 高（双证；原「latest 链」为中置信直读，已修正） |
| POST | `conans/<ref>/packages/delete`（body `{"package_ids":[...]}`） | 批量删包。**幂等语义（T-340 D-F 修正后回写 2026-08-29）**：混批中任一 pid 无树 → 静默跳过，成功恒 200 空 body；未知 ref → 404 `Path not found`；非法 pid → 400。三方对照：Artifactory 对无树 pid 答 **404 `Couldn't find packages for deletion`**（per-pid 枚举 `*` rRev 候选为空时；先到 pid 已删——「删树成功但回 404」形态在 Artifactory 同样存在），且删除范围**跨全部 rRev**；conan 1.66 参考实现（remove_packages）缺失 pid 静默无操作恒 200（跨全部 rRev）；BinFlow as-built 取参考实现的幂等 200 + **latest 解析模型不动**（不跨 rRev，差异登记） | 200（幂等空 body） | 高（三方取证：反编译 + conan 1.66 参考实现 + T-340 真客户端复现） |
| POST | `conans/<ref>/remove_files`、`.../packages/{pid}/remove_files` | 删指定文件 | 200 | 高 |
| PUT / GET | `files/<user>/<name>/<ver>/<channel>/[0/]export|package/...` | **v1 直传通道**：upload_urls 返回的 URL 即指向此（`{base}/v1/files/<存储路径>`）。PUT 201；GET 404 `Path not found` | 高 |

v1 存储路径里修订段用默认值 `0`（`getExportPathDefaultRevision`/`getPackagePathDefaultRevision`）；修订链由服务端在索引层解析 latest。此条补充公开规范。高（代码）。

### 3.3 管理端点（Artifactory 形态 `/api/conan/<repoKey>/...`；BinFlow 建议 `$BASE/binflow/api/conan/...` 或自有 `/api/v1/...`，归 ADR）

| 方法 | 路径 | 语义 | 置信度 |
|---|---|---|---|
| POST | `reindex` | 全仓重建 conan 索引（需 MANAGE 权限；仅 local；**异步调度**：AQL 候选扫描在调用线程完成后逐包发 ConanIndexEvent.Index 入 work queue，REST 返回 200 不等重算完成——代码锚 `reindexRepo` 日志 "Starting asynchronously re-index of {}"，置信度 高〔T-348 复核〕）。**BinFlow as-built（T-308 D5）：同步执行** + 同族 200 文案 `Calculated Conan index for repository '<key>' (path '<p>'): N revisions reindexed.`（文案无规格锚，as-built 固化于测试）——BinFlow 无 conan 异步 job 面，登记差异 | 高（Artifactory 侧）/ as-built（BinFlow 侧同步为已登记差异） |
| POST | `{repoPath}/reindex` | 按路径重建 | 高 |

## 4. 存储布局（此节整体为反编译补充公开规范；置信度：高——路径拼接函数逐条可读，且与 GitLab 响应形态互证）

```
<repoKey>/
  <user>/<name>/<version>/<channel>/            ← recipe 坐标根
    index.json                                   ← recipe 修订索引
    <rRev>/
      .timestamp                                 ← 修订时间标记（latest 排序依据）
      export/
        conanfile.py  conanmanifest.txt  conan_sources.tgz ...
      package/
        <packageId>/
          index.json                             ← 包修订索引
          <pRev>/
            .timestamp
            conaninfo.txt  conanmanifest.txt  conan_package.tgz ...
```

- recipe 索引：`<user>/<name>/<version>/<channel>/index.json`；包索引：`.../<rRev>/package/<packageId>/index.json`。
- `index.json` 内容 = revisions 端点的响应体同构（`{"reference"(或"reference"含 #rRev:pid),"revisions":[{revision,time}]}`，**time 降序**，首个 = latest）。修订排序按 `time` 字符串倒序、同 time 按 revision 倒序。
- `.timestamp`：修订根（`<rRev>/` 与 `<pRev>/` 各一）下由**系统身份**写入的毫秒时间戳文件；**首写后不覆盖**，除非距上次超过阈值（系统参数 conan timestamp override，默认阈值未在走读范围内定位——BinFlow 可固定为「首写定终身高」加配置项）。文件清单端点输出时会排除 `.timestamp`。
- 节点属性（供搜索/聚合）：`conan.package.name/version/user/channel`（全部上传文件）；`conan.package.author/license/url/vendor`（解析 conanfile.py 所得）；`conan.recipe_hash`（**= rrev**——hash 模式下 RREV 即 recipe 内容摘要；原文将该项并入「解析 conaninfo.txt 所得」为来源矛盾，2026-08-29 修正：conaninfo.txt 不含 recipe hash。`<ref>/search` 响应的 `recipe_hash` 字段同取 rrev，T-308 D7 as-built + 测试固化）、`conan.settings.*`、`conan.options.*`、`conan.requires`（解析 conaninfo.txt 所得）。`<ref>/search` 端点的响应即由这些属性组装。
- 物理 blob 仍走 BinFlow 既有 filestore 寻址；上述仅为逻辑路径。

## 5. 修订语义（RREV / PREV / packageId）

- **packageId**：conan 客户端按 settings/options 算出的包指纹（服务端只当不透明 ID 用）。高（官方文档）。
- **RREV / PREV：客户端计算、URL 直传，服务端不重算**。默认 `revision_mode=hash` 时 = conanmanifest.txt 内容（各文件 sha256 清单）的 sha256（v1 为 md5 形态 32 hex；v2 客户端为 sha256 64 hex）；`revision_mode=scm` 时 = VCS 提交哈希。高（官方 revisions 文档；代码中服务端对 rRev/pRev 只做存储与排序，无校验计算）。
- 服务端的「latest」= index.json 首项 = `.timestamp` 最新的修订，**不是**数值最大。高（代码）。
- 同一 rRev 重复 PUT 不同内容：按既有内容面覆盖语义（同 checksum 幂等、异 checksum 需 DELETE 权限，repo-semantics.md §3）；index.json 不去重修订（修订键即 rRev/pRev 字符串）。中。

## 6. 上传/下载/校验链

1. **conan 2 上传时序**（客户端行为，高）：`GET <ref>/latest`（404 即新包）→ 逐文件 `PUT .../files/{path}`（recipe 全套 → 包全套）→ `GET revisions` 确认。服务端在**首个文件 PUT 成功后**写 `.timestamp` 并把修订登记进 index.json（异步索引器完成，毫秒级；PUT 响应不等待索引可见，但 latest 端点必须最终一致）。
2. **下载时序**：`GET <ref>/latest` → `GET .../files`（清单）→ 逐文件 GET → 客户端按 conanmanifest.txt 自校验 sha256。
3. **服务端校验链**：
   - PUT 走 BinFlow 统一 checksum 头（`X-Checksum-Sha256/-Sha1/-Md5`）：malformed → 400；不符 → 409（client-checksums 策略）。
   - **checksum deploy**（能力自报项）：`X-Checksum-Deploy: true` 时零 body 部署，按 sha1/sha256 在库内寻已有二进制建引用；找不到按 404 语义处理（v1 files 通道显式透传 404）。高（代码）。
   - 下载响应带实测 `X-Checksum-*` 头；conanmanifest.txt 的逐文件哈希属客户端域，服务端不解析校验。高。
4. **属性注入**（Artifactory 扩展，BinFlow 可选）：上传 conanfile.py/conaninfo.txt 后解析并写 §4 属性集；矩阵参数与 `artifact_property_<name>` 请求头同样转为节点属性。中。
5. 路径含空格 → 400（`Path is invalid`）。中（代码字面）。

## 7. rclass 三态行为（对照 repo-semantics.md 框架）

| rclass | 行为 | 置信度 |
|---|---|---|
| local | v1+v2 全量（§3）；PUT/DELETE/搜索/reindex。 | 高 |
| remote | **仅 v2 数据面**（v1 数据面 → 400；握手三端点类无关，见 §1）。上游为另一 conan 服务（BinFlow 远端或 Artifactory）。**as-built 2026-08-27（T-312）**：读路径全走 FR-20 引擎（负缓存、TTL 双类、守卫回源、stale-while-error）——①`index.json`（recipe/package 两平面）回源映射到对应 `revisions` 端点，拉回的即索引文档（S2 同构的天然利用）；②两个 marker 文档缓存上游响应体后**逐字服务**：files 清单（`…/<rRev>/.files.json` ↔ `…/revisions/<rRev>/files`）与 packageId 元数据（`…/<rRev>/.search.json` ↔ `…/revisions/<rRev>/search`）；③文件体 `export/<path>` ↔ `…/files/<path>`、`package/<pid>/<pRev>/<path>` ↔ `…/packages/<pid>/revisions/{pRev}/files/{path}`（内容类 TTL 不可变）——出站一跳 UpstreamPath 翻译段序，缓存键/落点全保持存储拼写。**search 不代理**（引擎上游跳是 path 拼接，query 端点不可达——T-287 约束，nuget 先例）→ 404 诚实文案（差异登记：cargo remote 后续以查询键标记路径实现了代理，conan 未跟进，可议新票）。写拒绝：PUT 落共享臂由服务答 RE-05 405 + Allow: GET；DELETE 族 handler 内同文案拒绝；checksum-deploy 经 PutFromBlob 服务侧拒绝。**缓存落点在 remote 仓自身命名空间**（Artifactory 的独立 `<repoKey>-cache` 仓惯例不采纳——BinFlow M3 引擎既定设计，登记差异）；`storeArtifactsLocally` 委托语义未实现（无独立 cache 仓概念）。 | 中（remote handler 走读 + 框架推断）→ **as-built 高（mock 上游逐路径断言 + 自指 BinFlow 上游全协议回路 + conan 2.31.2 真机 remote 全链 install）** |
| virtual | **仅 v2 数据面**。**as-built 2026-08-27（T-312）**：文件体零新代码（svc.Get 已是双桶 first-found 成员解析，local 成员节点 + remote 成员经引擎链，`X-BinFlow-Resolved-From` 随 reader）。revisions/latest 合并：逐成员 ReadVirtualMember 读各自 `index.json`，按修订字符串去重（首见成员行保留）、time 降序排序，latest = 归并后首项。files 清单并集：local 成员出节点事实 + remote 成员出 marker 文档；空并集 → 404 `Path not found`。**packageId 行并集（S11 补记）**：local 成员从事实推导（latest pRev 的 conaninfo.txt 解析）+ remote 成员取 marker 文档，pid 键首见成员胜。**search 并集仅 local 成员**（remote 成员目录不可枚举——成员缝拒绝，T-287/nuget 姿态；差异登记）。成员失败策略：unfound 贡献空、分类失败跳过并记住首个、全部落空且有记住失败 → 浮出该失败（绝不伪装纯 404）。写平面：PUT 走共享臂由服务路由 `defaultDeploymentRepo` → local 成员，**未配置 → C5 405 + Allow: GET**（~~400 含仓 key~~ Artifactory 形态；BinFlow RE-08 全域统一姿态优先，差异登记——文案仍含仓 key）；**写登记尾索引读成员定向**（读目标成员自己的 index.json，防归并视图拷贝污染——npm loadPackumentForWrite 规则，测试断言目标成员索引不含他成员修订）；DELETE → 405 不传播（措辞随有无写路由二态）。 | 中（merger 类族 + 虚仓 handler 走读；归并细节未逐行核对）→ **as-built 高（八集成测 + conan 2.31.2 真机：virtual 聚合读 install 全链 + routed 写归并视图立即可见）** |

## 8. 真实客户端命令清单（conan 2.x；本机无 conan，以下供 qa 在有客户端环境执行）

```bash
# 安装参考：pipx install conan（2.x）
CONAN_REMOTE_URL="$BASE/binflow/conan-local"          # 例 http://localhost:8080/binflow/conan-local
conan profile detect --force                          # 默认 profile

# L-c1 握手与登录（v1 三端点硬依赖验证）
curl -s "$CONAN_REMOTE_URL/v1/ping" -o /dev/null -w '%{http_code}\n'          # 200
curl -s -u user:pass "$CONAN_REMOTE_URL/v1/users/authenticate"                 # token 字符串
conan remote add binflow "$CONAN_REMOTE_URL" --force
conan remote login binflow user -p pass            # 内部走 authenticate → Bearer

# L-c2 上传（新建 recipe + 包）
cat > conanfile.py <<'EOF'
from conan import ConanFile
class HelloConan(ConanFile):
    name = "hello"
    version = "1.0"
    settings = "os", "arch"
    def package(self): pass
EOF
conan create . --user=myuser --channel=stable -pr=default
conan upload "hello*" -r binflow --confirm

# L-c3 下载/安装/检索
conan list "hello/*" -r binflow                      # revisions 链可见
conan list "hello/1.0@myuser/stable/*" -r binflow
rm -rf ~/.conan2/p && conan install --requires=hello/1.0@myuser/stable -r binflow --build=missing

# L-c4 curl 直打端点（对账 revisions 响应体）
curl -s -u user:pass "$CONAN_REMOTE_URL/v2/conans/hello/1.0/myuser/stable/revisions"
curl -s -u user:pass "$CONAN_REMOTE_URL/v2/conans/hello/1.0/myuser/stable/latest"

# L-c5 删除
conan remove hello/1.0@myuser/stable -r binflow -c
```

conan 1.x 客户端（v1 协议族）：BinFlow 决策建议**不做完整 v1 数据面**（v1 已 EOL；仅实现 ping/authenticate/check_credentials 握手三端点 + `files` 通道可延后），登记 M11 拆票时裁决。

## 9. 与公开规范的差异/补充清单

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | 存储布局（§4 全节：坐标目录、index.json、.timestamp、双修订层） | 反编译 | 高（GitLab 响应形态互证） |
| S2 | index.json = revisions 响应体同构、time 降序 | 反编译 | 高 |
| S3 | `.timestamp` 首写不覆盖 + 阈值例外 | 反编译 | 高 |
| S4 | 能力头族与版本比较（§2 表） | 反编译 | 中 |
| S5 | v1 upload_urls/snapshot（md5 清单）/digest 端点族与 `files` 直传通道 | 反编译 | 高 |
| S6 | v1 仅 local、remote/virtual 400 文案 | 反编译 | 高 |
| S7 | 节点属性集（conan.package.*/settings./options./requires）；**recipe_hash 来源修正 = rrev（非 conaninfo.txt，T-308 D7 + 2026-08-29 复核）** | 反编译 + as-built | 高 |
| S8 | checksum deploy 在 conan 面的启用（X-Checksum-Deploy 透传 404） | 反编译 | 高 |
| S9 | 空修订清单 → 404（GitLab 未定义错误码） | 反编译 | 高 |
| S10 | reindex 两管理端点与 MANAGE 权限 | 反编译 | 高 |
| S11 | virtual 聚合算法细节（归并/并集/first-found/**pid 行并集**/**写登记尾成员定向**） | 反编译（部分推断）；as-built（T-312）补记 pid 并集与 marker 文档面 | 中 → 高（as-built） |
| S12 | `_` 占位 user/channel 的字面存储 | 客户端约定 + 推断 | 中 |

## 10. 显式不做（M11 登记建议）

1. **v1 完整数据面**（upload_urls/download_urls/snapshot/digest/remove_files 族）：仅握手三端点必须做。若 qa 需要 conan 1.x 客户端全兼容再立项。**〔M12 状态注记 2026-08-29：CN-1 终裁已推翻本条——v1 全量十七端点 + files 直传通道随 T-308 落地，conan 1.66 真机全链实证；保留原文仅为决策沿革〕**
2. **ConanV2 迁移 job**：绿地无 v1 遗留数据，不做。
3. `conanvon`/conan 中心（conancenter）专用搜索语法扩展：`search?q=` 通配子集即可。

## 11. M11 拆票就绪度自评

- **可直接拆票**：v2 端点族（§3.1 全表）、存储布局（§4）、修订语义（§5）、校验链（§6）、local 行为、客户端命令（§8）——依据充分。
- **拆票时需 tech-lead 裁决两点**：① v1 实现子集边界（建议：ping + authenticate + check_credentials + files 通道四项）；② upload_urls 类绝对 URL 的 base 配置来源（系统配置 vs 请求 Host 推导）。
- **低置信/待验证**：~~virtual 归并细节（S11）~~（T-312 as-built 高，见 §7）；`X-Conan-Server-Version` 自报值取 `0.20.0` 还是随实现自定（客户端只比较不强制，风险低；BinFlow as-built 恒报 0.20.0）、~~`.timestamp` 覆盖阈值默认值~~（BinFlow as-built 取「首写定终身」无阈值例外——TL-3；Artifactory 侧默认阈值仍未在走读范围内定位，维持开放）。

### 待验证清单（动态验证即可升高）

1. ~~conan 2.x 客户端对 `_` 占位段的真实请求形态（S12）~~ **已闭环（2026-08-29）**：conan 1.66 真机匿名 ref `hello/1.0@_/_` wire 恒 `_/_`（client_routes `ref.user or "_"`，T-340 §3）+ conan 2.31.2 上传/安装链（T-308）——`_` 字面存储字面匹配成立。
2. ~~virtual 面真实 conan 客户端 install 全链（S11 聚合行为）~~ **已闭环（T-312 §3，2026-08-27）**：conan 2.31.2 经 virtual `conan install` 成功（local 成员载荷）+ routed 上传后 `conan list "#*"` 归并视图立即可见。
3. ~~v2 上传后 latest 端点的最终一致延迟（异步索引）在真实客户端重试窗口内的表现~~ **BinFlow as-built 已消除**：修订登记在 PUT 请求链内同步完成（读-改-写互斥锁），conan 2.31.2 真机 upload 后 `conan list` 立即可见全量修订（T-308 §3）；Artifactory 的异步索引器语义维持原观察（毫秒级，代码）。
