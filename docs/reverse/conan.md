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

- v1 与 v2 是**同一仓型上的两套端点族**，共用存储；v1 仅 local 仓可用（remote/virtual 收到 v1 请求 → 400 `Unsupported Conan v1 repository request for '<repoKey>'`）。高（代码显式分支）。
- **BinFlow 决策记录**：Artifactory 7.x 有 v1→v2 布局迁移 job（`ConanV2MigrationJob`）与「未迁移则全端点 400」的闸门；BinFlow 是绿地实现，直接落 v2 布局，**无迁移 job、无闸门**，v1 端点按需子集实现（见 §8）。

## 2. 能力协商与认证（v1 三端点是 conan 2 客户端的硬依赖）

Conan 2.x 客户端即使只走 v2 数据端点，**握手必走 v1 三端点**（官方客户端行为，社区 issue 佐证 #17001）：

| 端点（相对 `$BASE/binflow/<repoKey>/`） | 行为 | 置信度 |
|---|---|---|
| `GET v1/ping` | 200 空体 + 能力响应头（下表）。未认证也需可达（客户端探测用）。 | 高 |
| `GET v1/users/authenticate` | 带 Basic 认证 → 200，**响应体纯文本即 token 字符串**（客户端后续以 `Authorization: Bearer <token>` 使用；GitLab 同型返回 JWT）。凭据无效 → 401。 | 高（代码 + GitLab 文档双证） |
| `GET v1/users/check_credentials` | Bearer/Basic 校验 → 200 空体；无效 → 401。 | 高 |

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
| GET | `search?q=<query>` | 仓内 recipe 搜索（`*` 通配，匹配 `name/version@user/channel` 串） | 200 `{"results":["<ref>",...]}` | — | 高 |
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
| DELETE | `conans/<ref>` | 删除 recipe（latest 修订链） | 200 / 404 | 高 |
| POST | `conans/<ref>/packages/delete`（body `{"package_ids":[...]}`） | 批量删包 | 200 | 高 |
| POST | `conans/<ref>/remove_files`、`.../packages/{pid}/remove_files` | 删指定文件 | 200 | 高 |
| PUT / GET | `files/<user>/<name>/<ver>/<channel>/[0/]export|package/...` | **v1 直传通道**：upload_urls 返回的 URL 即指向此（`{base}/v1/files/<存储路径>`）。PUT 201；GET 404 `Path not found` | 高 |

v1 存储路径里修订段用默认值 `0`（`getExportPathDefaultRevision`/`getPackagePathDefaultRevision`）；修订链由服务端在索引层解析 latest。此条补充公开规范。高（代码）。

### 3.3 管理端点（Artifactory 形态 `/api/conan/<repoKey>/...`；BinFlow 建议 `$BASE/binflow/api/conan/...` 或自有 `/api/v1/...`，归 ADR）

| 方法 | 路径 | 语义 | 置信度 |
|---|---|---|---|
| POST | `reindex` | 全仓重建 conan 索引（需 MANAGE 权限；仅 local；异步调度，返回 200 + 提示文案） | 高 |
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
- 节点属性（供搜索/聚合）：`conan.package.name/version/user/channel`（全部上传文件）；`conan.package.author/license/url/vendor`（解析 conanfile.py 所得）；`conan.recipe_hash`、`conan.settings.*`、`conan.options.*`、`conan.requires`（解析 conaninfo.txt 所得）。`<ref>/search` 端点的响应即由这些属性组装。
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
| remote | **仅 v2**（v1 → 400）。上游为另一 conan 服务（BinFlow 远端或 Artifactory）。读路径 pull-through：未命中回源 `<upstream>/v2/conans/...`，落到 `<repoKey>-cache` 同形态路径；`storeArtifactsLocally=false` 时委托 cache 判定（delegate-to-cache 语义：请求先问 cache 仓）。PUT/DELETE 拒绝（对齐 BinFlow 既有 remote 写拒绝）。 | 中（remote handler 走读 + 框架推断） |
| virtual | **仅 v2**。聚合算法：revisions 清单跨成员**按 time 归并去重**（同名修订取首见成员）；files 清单跨成员**并集**；文件内容**first-found**（成员序）；latest 取归并后首项；search 并集。PUT 路由到 `defaultDeploymentRepoRef` 指向的 local 成员，未配置 → 400（错误体含仓 key）。 | 中（merger 类族 + 虚仓 handler 走读；归并细节未逐行核对） |

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
| S7 | 节点属性集（conan.package.*/settings./options./requires/recipe_hash） | 反编译 | 高 |
| S8 | checksum deploy 在 conan 面的启用（X-Checksum-Deploy 透传 404） | 反编译 | 高 |
| S9 | 空修订清单 → 404（GitLab 未定义错误码） | 反编译 | 高 |
| S10 | reindex 两管理端点与 MANAGE 权限 | 反编译 | 高 |
| S11 | virtual 聚合算法细节（归并/并集/first-found） | 反编译（部分推断） | 中 |
| S12 | `_` 占位 user/channel 的字面存储 | 客户端约定 + 推断 | 中 |

## 10. 显式不做（M11 登记建议）

1. **v1 完整数据面**（upload_urls/download_urls/snapshot/digest/remove_files 族）：仅握手三端点必须做。若 qa 需要 conan 1.x 客户端全兼容再立项。
2. **ConanV2 迁移 job**：绿地无 v1 遗留数据，不做。
3. `conanvon`/conan 中心（conancenter）专用搜索语法扩展：`search?q=` 通配子集即可。

## 11. M11 拆票就绪度自评

- **可直接拆票**：v2 端点族（§3.1 全表）、存储布局（§4）、修订语义（§5）、校验链（§6）、local 行为、客户端命令（§8）——依据充分。
- **拆票时需 tech-lead 裁决两点**：① v1 实现子集边界（建议：ping + authenticate + check_credentials + files 通道四项）；② upload_urls 类绝对 URL 的 base 配置来源（系统配置 vs 请求 Host 推导）。
- **低置信/待验证**：virtual 归并细节（S11）、`X-Conan-Server-Version` 自报值取 `0.20.0` 还是随实现自定（客户端只比较不强制，风险低）、`.timestamp` 覆盖阈值默认值。

### 待验证清单（动态验证即可升高）

1. conan 2.x 客户端对 `_` 占位段的真实请求形态（S12）。
2. virtual 面真实 conan 客户端 install 全链（S11 聚合行为）。
3. v2 上传后 latest 端点的最终一致延迟（异步索引）在真实客户端重试窗口内的表现。
