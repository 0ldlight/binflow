# QA 报告 T-18 — M1 功能矩阵验收（PRD §7 场景 1/3/4/6/7）

- role: qa-engineer
- 日期: 2026-08-18
- 剧本口径: docs/prd/milestone-1.md **v1.3.1**（C03 期望 200 纯文本、C14 期望 409、C28a 带 `-u admin`、C23/C27 匿名双模式、E-26 矩阵）
- 被测对象: `make build` 产物 `./bin/binflow-server`（16.32 MB），commit `00e2d9f` 工作区
- 环境: darwin/amd64，Go 1.26.5，临时数据目录 + 随机端口（8080 被 Docker 占用，用 `serve -c <yaml>` 换 `server.listen`）
- 结论预告: **FAIL（2 个 P1 缺陷，均为管理面授权边界）**；五大场景功能面全绿

## 场景总览

| 场景 | 范围 | 结果 |
|---|---|---|
| 1 工程基线 | make build/test/lint + 零 CGO + gofmt + tidy | ✅ PASS |
| 3 仓库生命周期 | C03/C04/C05/C06/C19 | ✅ PASS |
| 4 制品 roundtrip | C07/C08/C09/C10/C13/C14/C15a-c/C18 + 加测（幂等重传/mkdir/Range/?list） | ✅ PASS |
| 6 认证与 ACL | C02/C20/C21/C22/C23/C27 | ❌ FAIL（C22b 断言点：非 admin 读管理 API 得 200；另附 D3） |
| 7 边界拒绝 | C24/C25/C26 + 路径穿越/非法路径 | ✅ PASS |
| NFR-S1/S2/S3 | SQLite/日志凭据泄漏抽查 | ✅ PASS |
| C28 | ping/version/health | ✅ PASS |

---

## 场景 1：工程基线（FR-1）

| # | 检查 | 结果 | 证据 |
|---|---|---|---|
| 1.1 | `make build` | ✅ | `bin size: 16.32 MB (bin/binflow-server)`，退出码 0 |
| 1.2 | `--help` | ✅ | 退出码 0，输出含 usage（serve/gc 双 subcommand + env 说明） |
| 1.3 | `make test`（race） | ✅ | 11/11 包 ok、0 FAIL：storage 51.9s、httpapi 29.2s、repo 25.4s、auth 21.8s、metadata 20.9s、generic 16.7s、console 3.9s、audit 3.6s、config 4.4s、adapter 2.7s、cmd 2.7s（总时长约 3.5 分钟） |
| 1.4 | `make lint` | ✅ | `0 issues.`（golangci-lint v2.12.2） |
| 1.5 | `gofmt -l .` | ✅ | 空输出 |
| 1.6 | `CGO_ENABLED=0 go build ./...` | ✅ | 退出码 0 |
| 1.7 | `go mod tidy` | ✅ | go.mod/go.sum 无 diff |
| 1.8 | CI 配置存在 | ✅ | `.github/workflows/ci.yml` 含 lint+test+build 三步（FR-1-AC4 P1，QA 验收口径=文件存在+本地三件套绿） |

注：T-7 遗留「CI 首跑绿由主会话确认」，不在本票范围。

## 场景 3：仓库生命周期（FR-3）

| # | 命令 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 3.1 | C03 `PUT /api/repositories/generic-local` | 200 纯文本 | `200` `Successfully created repository 'generic-local'`（CT `text/plain`） | ✅ |
| 3.2 | C04 key=`Bad_Key!` | 400 + E-01 | `400` errors[] `invalid repository key "Bad_Key%21": must start with a lowercase letter` | ✅ |
| 3.3 | C05 `GET /api/repositories` | 200，jq 取出 key；字段 key/description/type/packageType/url | 全字段在；`Cache-Control: no-store` 在；grep -x generic-local 退出码 0 | ✅ |
| 3.4 | C06 `GET /api/repositories/generic-local` | `rclass==local`、`packageType==generic` | 输出 `local` / `generic` | ✅ |
| 3.5 | C19 非空删仓不带参数 | 400，message 含 deleteContent | `400` `repository is not empty: "generic-local" holds 4 node(s); retry with deleteContent=true to remove them` | ✅ |
| 3.6 | C19 `?deleteContent=true` | 2xx，node 全 404 | `200` `Repository generic-local deleted successfully.`；node 404、repo config 404、剩余 0 仓 | ✅ |

## 场景 4：制品 roundtrip（FR-4）

10MB 随机文件 `artifact.bin`（sha256 `466acfff…2536`，size 10485760）。

| # | 命令 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 4.1 | C07 PUT | 201 + checksums.sha256 对账 + size 字符串 + createdBy | `201`；`checksums.sha256==本地`；`"size":"10485760"`（JSON 字符串型）；`createdBy:"admin"`；`Location`/`X-Checksum-Sha256` 头在 | ✅ |
| 4.2 | C08 GET | 200，sha256 一致；三 checksum 头 + ETag=sha1 无引号 + Last-Modified + Accept-Ranges | 下载 sha256 一致；`X-Checksum-Sha256/Sha1/Md5` 三头全对；`Etag: abdbc710…`（=sha1，无引号）；两头在 | ✅ |
| 4.3 | C09 HEAD | 200，Content-Length==size，X-Checksum-Sha256 正确 | `10485760`；sha256 头正确（剧本 awk 提取一致） | ✅ |
| 4.4 | C10 item info | 字段全集 + lastUpdated + size 字符串 + originalChecksums | 13 字段全在（uri/downloadUri/repo/path/created/createdBy/lastModified/modifiedBy/lastUpdated/size/mimeType/checksums/originalChecksums）；checksums 与 originalChecksums 各含 sha1/md5/sha256；path `/` 开头；时间 `2026-08-18T00:23:17.000Z` | ✅ |
| 4.5 | C13 带 X-Checksum-Sha256 一致 | 201 | `201` | ✅ |
| 4.6 | **C14 不一致** | **409** + received/actual；落盘无 node | `409` `Checksum error for 'generic-local/acme/bad.bin': received '000…0' but actual is '466acfff…'`；GET bad.bin `404` | ✅ |
| 4.7 | C15a deploy 命中 | 201 零传输，新路径可下载 | `201`；copy.bin 下载 sha256 一致（uniq -c = 2） | ✅ |
| 4.8 | C15b deploy 未命中 | 404 | `404` `no content found for the given checksum` | ✅ |
| 4.9 | 加测 C15c deploy 缺专用头 | 400 | `400` `no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found` | ✅ |
| 4.10 | 加测 C15d deploy 格式非法 | 404 | `404` `malformed sha256 value "notachecksum"` | ✅ |
| 4.11 | C18 DELETE | 204 无 body → 重复 404 → GET 404 | `204:0`（code:bytes）→ `404` errors[] → `404` | ✅ |
| 4.12 | 加测 mkdir 尾斜杠 | 201 | `201` + FolderInfo JSON（uri 以 `/` 结尾，children 列出 copy.bin/v2.bin）；重复 mkdir 仍 201 | ✅ |
| 4.13 | 加测 **同 checksum 幂等重传免覆盖权限** | 仅 write 用户带 checksum 重传同内容 → 2xx；异内容 → 403 | admin 预置 `ci-out/x.bin` 后：ci-bot 带 `X-Checksum-Sha256`（与既有一致）重传 → `201`；带头但异内容 → `403`（message 含 needs DELETE permission） | ✅ |
| 4.14 | 加测 Range 单区间（T-20 面） | 206 字节正确 / 416 | `bytes=0-99` → `206` `Content-Range: bytes 0-99/10485760`，内容 sha256 与 `head -c 100` 一致；`bytes=999999999-` → `416` `Content-Range: bytes */10485760` | ✅ |
| 4.15 | 加测 `?list` | 匿名 403 / 根 400 | 匿名 `403`；`?list`（根）`400` `Cannot list files of root.`；文件目标 `400`；子目录 `?list&deep=1` → 200，files[].uri 相对路径（C17 全量属 T-19） | ✅ |
| 4.16 | 加测 去重抽查（C12 面，完整在 T-19） | 同内容两路径 blob 计数不变 | 4→上传 d/a.bin→5→同内容 d/b.bin→**仍 5** | ✅ |

注（4.13）：PRD §4 注记原文「路径已存在**且客户端带 checksum** 与既有值相同 → 幂等重传」。不带 checksum 头的同内容重传实测按覆盖处理（无 delete 权限得 403），与注记「checksum 不同（或未带）→ 视为覆盖」一致，判定合规。

## 场景 6：认证与 ACL（FR-5）——本场景 FAIL

| # | 命令 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 6.1 | C02 未认证访问管理 API | 401 + WWW-Authenticate + E-01 | `401`；`Www-Authenticate: Basic realm="BinFlow Realm"`；errors[] JSON | ✅ |
| 6.2 | 未认证 PUT（FR-4-AC9） | 401 + WWW-Authenticate | `401` + 挑战头 | ✅ |
| 6.3 | C20① 自有路由改密 | 200 | `200` `Password has been successfully changed` | ✅ |
| 6.4 | C20① 认证对+body 旧口令错 | 400 纯文本 `Incorrect username/password`（非 401） | `400` 纯文本 `Incorrect username/password` | ✅ |
| 6.5 | C20② changePassword 别名 | 200 | `200` 同文案 | ✅ |
| 6.6 | C20② 旧口令错 | 400 非 401 | `400` `Incorrect username/password` | ✅ |
| 6.7 | C20 旧口令随即失效 | 旧 401 / 新 200 | `401` / `200` | ✅ |
| 6.8 | C21a token（form） | 200，access_token/token_type=Bearer/scope 非空 + token_id 扩展 | `{access_token, token_type:"Bearer", expires_in:2592000, scope:"api:*", token_id:1}` | ✅ |
| 6.9 | C21a JSON 扩展形态 | 200 | `200` | ✅ |
| 6.10 | C21a 未知 grant_type | 400 OAuth 错误体 | `400` `{"error":"unsupported_grant_type","error_description":"Grant type is not supported: bogus"}` | ✅ |
| 6.11 | C21b token 作 Basic 口令 | 200 | `200` | ✅ |
| 6.12 | C21b X-JFrog-Art-Api 头 | 200 | `200` | ✅ |
| 6.13 | C21c revoke by token | 200 纯文本 `Token revoked`，之后 401 | `200`；token 再用 `401` | ✅ |
| 6.14 | C21c 重复吊销 | 200 `Token not found` | `200` `Token not found` | ✅ |
| 6.15 | C21c 同传 token+token_id | 400 `token and token_id are mutually exclusive` | 逐字一致 | ✅ |
| 6.16 | C21c 都缺 | 400 `token or token_id is required` | 逐字一致 | ✅ |
| 6.17 | C21c revoke by token_id | 200 + token 失效 + 幂等 | `200`/`401`/`Token not found` | ✅ |
| 6.18 | C22a PUT 建用户 | 201 无 body | `201:0` | ✅ |
| 6.19 | C22a email 缺失 / password 空 / `_system_` | 400 | 三者均 `400`（`Please provide a valid user email.` / `…password.` / `Unable to create user.`） | ✅ |
| 6.20 | C22a GET users 列表 | 元素 {name,uri,realm}，无口令字段 | 字段精确三件套；全文件 grep 口令类关键词 0 命中 | ✅ |
| 6.21 | C22b 未授权 PUT | 403 | `403` | ✅ |
| 6.22 | **C22b 非 admin 访问管理 API** | **401/403** | qa-bot `GET /binflow/api/repositories` → **200**（返回全部仓列表）；`GET /api/v1/storage/stats` → 200；`GET /api/v1/health` → 200 | ❌ **缺陷 D2** |
| 6.23 | C22b 授权后 PUT | 201 | `201` | ✅ |
| 6.24 | C22c 无 delete 删 | 403；admin 删 2xx | `403`；admin `204` | ✅ |
| 6.25 | FR-5-AC10 权限对象管理 | GET 列表；DELETE 后授权立即失效 | 列表 `ci-out-rw`/`qa-out-rw`；删 qa-out-rw 后 qa-bot PUT `403` | ✅ |
| 6.26 | C23 匿名 GET 内容 | 200 内容一致 | `200`，sha256 与源一致（uniq -c = 2） | ✅ |
| 6.27 | C23 匿名 PUT / 匿名管理 API | 401 / 401 | `401` / `401` | ✅ |
| 6.28 | C27 关匿名读 | 匿名 401+挑战头；认证 200；read 差异化 | `401` + `Www-Authenticate`；admin `200`；ci-bot（有 read）`200`；qa-bot（无 read）`403`；匿名 item info 亦 `401` | ✅ |
| 6.29 | 加测 E-18 admin-only 面 | revoke 应 admin only | 非 admin revoke → `403` `administrator privileges required` | ✅ |
| 6.30 | 加测 E-17 token 签发授权面 | （见缺陷 D3） | 非 admin `POST /api/security/token` → **200**，签出 `scope:"api:*"` 的 64 字符 token，且该 token 可通过 Basic 认证读管理面 | ❌ **缺陷 D3** |
| 6.31 | 加测 E-16 跨用户改密 | 非 admin 改他人 → 拒绝 | user-a 改 user-b（无论是否知旧口令）→ `403` `only administrators may change another user's password`；admin 改任意用户（须持目标用户旧口令）→ `200`；用户自改 → `200`。与 auth-model §2.1 权限列一致 | ✅ |

两轮实例（第一轮因我方脚本时序误报，第二轮干净实例精确定界）复核后，C20/C22a/C22c 全链确认无实现缺陷；初报的「改密 401」「跨用户改密 200」为 QA 自身口令状态污染所致，已撤回。

## 场景 7：边界拒绝（FR-4-AC10/AC11、E-26）

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 7.1 | `/v2/` | 404 | `404` | ✅ |
| 7.2 | `/artifactory/api/system/ping` | 404 + message 提示 /binflow | `404` `no root mirror: BinFlow serves every endpoint under the /binflow prefix …` | ✅ |
| 7.3 | `/binflow/api/npm/xx` | 404 + E-01 | `404` `/binflow/api/npm/xx is not implemented in BinFlow`（jq `.errors[0].status`=404） | ✅ |
| 7.4 | `/api/system/info`、`/nope`、pypi/pypi-ui/search/replication/system/info、未知端点 | 404 | 7 个变体全 `404` | ✅ |
| 7.5 | C25 X-Explode-Archive | 400 | `400` `X-Explode-Archive is not supported in BinFlow M1` | ✅ |
| 7.6 | C26 remote | 400 | `400` `remote docker repositories are supported from M3` | ✅ |
| 7.7 | C26 virtual | 400 | `400` 同上（virtual） | ✅ |
| 7.8 | 保留字 key `api`/`v2` | 400 | 均 `400` `reserved routing segment (ADR-0008)` | ✅ |
| 7.9 | 路径穿越 `--path-as-is`（`a/../../etc/passwd`、`%2e%2e`、`%2E%2E`、`..%2f..%2f`、深三级） | 400 | PUT×5 与 GET×2 全 `400`；数据目录外无逃逸文件 | ✅ |
| 7.10 | 双斜杠/空段/520 字符路径 | 400 | `400`/`400`/`400` | ✅ |
| 7.11 | 5xx/panic 扫描 | 无 | 三份服务日志 0 条 5xx、0 ERROR、0 panic | ✅ |
| 7.12 | 加测 `?properties` / `:properties` 后缀 | 404 E-01 / PUT 409 | `?properties` `404`；PUT `…:properties` `409`（rest-api §1.2 上传主流程定案）；GET `…:properties` 404（该 409 规格仅约束上传流程，GET 放行到内容解析后 404，合规） | ✅ |

## NFR-S1/S2/S3 抽查

| # | 检查 | 结果 | 证据 |
|---|---|---|---|
| N1 | SQLite（binflow.db/-wal/-shm）grep 明文口令 | ✅ | `Adm-Pw-T18`、`n3w!pw`、`ci-pw`、`qa-pw-1`、`evil-pw-1` 全部 0 命中 |
| N2 | SQLite grep 已发 token 明文 | ✅ | token `fa415418dab1…` 0 命中（只存 sha256 摘要） |
| N3 | 服务日志 grep Authorization/凭据 | ✅ | 三份日志：header 值 0 命中、口令 0 命中（仅 2 条命中为 changePassword **URL 路径**字段，属正常访问日志路径名） |
| N4 | 日志结构化 | ✅ | 每请求一行 JSON：time/level/method/path/status/duration_ms/remote_addr/user（匿名记 anonymous）/bytes_in/out/request_id |

## C28

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 8.1 | ping | `OK` 免认证 | `OK` | ✅ |
| 8.2 | version | 如实版本 + product:BinFlow | `{version:"dev", revision:"dev", product:"BinFlow"}`（Q4 不伪装；`dev` 为无 ldflags 构建的如实值） | ✅ |
| 8.3 | health（v1.3.1 勘误：`-sfu admin`） | 200 status ok + storage/metadata | `{status:ok, storage:{status:ok}, metadata:{status:ok}}`，curl -sfu 退出码 0 | ✅ |
| 8.4 | storage stats | blob/逻辑/物理字节 | `{blobs:3, logical_bytes:…, physical_bytes:…}` | ✅ |

---

## 缺陷清单（发现不修，附定位建议）

### D2 [P1] 非 admin 用户可读全部管理面只读端点（C22b 断言点）
- **现象**：非 admin（如 qa-bot）`GET /binflow/api/repositories` → 200（返回**全部**仓库配置，含该用户无任何 read 授权的仓）；`GET /binflow/api/v1/storage/stats` → 200（全实例 blob/字节统计）；`GET /binflow/api/v1/health` → 200。PRD FR-5-AC8/C22b 明文「ci-bot 访问管理 API（如 GET /binflow/api/repositories）→ 401/403（非 admin）」。
- **边界**：写面正确（建仓/建用户/删权限对象 → 403 `administrator privileges required`）；users 列表与 permissions 列表正确 403；`/api/v1/health` 是否放开可议（健康探针常需匿名/低权，但 PRD §6.3 未豁免）。
- **复现**（干净实例）：
  ```
  curl -su user-a:<pw> -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/repositories   # 200（期望 401/403）
  curl -su user-a:<pw> $BASE/binflow/api/v1/storage/stats                                 # 200
  ```
- **期望**：非 admin 对 `/binflow/api/**` 管理面（除 ping/version，及产品决定豁免的 health）返回 403（已认证）或收敛为仅返回其被授权仓库。
- **定位建议**：internal/httpapi 管理端点的授权中间件分级（T-14 middleware 链 / T-15 repositories+stats handler）；BOARD T-15 遗留项「repo 列表无按调用者过滤（M1 无数据面）」相关但状态码层面不符 PRD，建议新缺陷票。

### D3 [P1] 非 admin 可签发 scope=api:* 的 API Token（E-17 面）
- **现象**：非 admin `POST /binflow/api/security/token -d grant_type=client_credentials` → 200，签发 `scope:"api:*"`、64 字符 token；该 token 以 Basic 口令形态可读管理面（叠加 D2）。auth-model.md §3 明确 token 管理端点族为 admin only（§3.3/§3.4「admin only」；§3.1 username 参数语义「非 admin 只能填自己」的前提是端点本身授权受限）。PRD FR-5-AC4 未显式写 admin only，但 FR-5-AC8 的管理面非 admin 拒绝原则覆盖此端点。
- **加重因素**：与 D2 叠加后，任意普通用户可签发长效（默认 30 天）token 持久化访问管理面读数据。
- **复现**：`curl -su user-a:<pw> -X POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials' | jq .` → 200 + access_token。
- **期望**：非 admin 签发 → 403（或按 Artifactory 语义允许「只能为自己签」的受限 scope，但 M1 无 scope 模型，建议直接 admin only，与 revoke 端点现有行为对齐——revoke 已正确 403）。
- **定位建议**：internal/httpapi security handler 的 token 签发授权检查（T-15）；revoke 已有 `administrator privileges required` 分支，签发处缺同款前置。

### 观察项（不构成缺陷，供 product-manager/conductor 裁决）
- **O1**：E-16 自有路由 `PUT /api/security/password` 认证口令错误时返回 401 + errors[] JSON。PRD §5.1 错误体三分层把 `/api/security/users|permissions/**`（含 changePassword）划为纯文本层，`/api/security/password` 字面不在该前缀内，且 401 挑战带 WWW-Authenticate 符合 E-20。判定：合规（401 场景 PRD 未规定纯文本），不改判。
- **O2**：C22a 重复 PUT 已存在用户 → 409 `The user already exists: qa-bot`。auth-model §1.3 定义 PUT 为 create-or-replace（已存在→更新→201），BinFlow 拒绝重复。PRD v1.3 E-19 未对重复 PUT 定案，属「BinFlow 自有语义」可接受范围，但与真实 Artifactory 行为有差异，建议 M2 评估是否对齐 replace 语义。
- **O3**：C17 根目录 `?list` → 400 `Cannot list files of root.` 为 PRD v1.2 定案行为（非缺陷）；子目录 list 的 files[] 元素含 `sha1/sha2` 扩展字段（PRD E-10 M1 子集为 uri+size，超集无害）。

---

## DoD 第 2 条判定（§7 场景覆盖）

| §7 场景 | 本票覆盖 | 判定 |
|---|---|---|
| 1 工程基线 | 是 | 绿 |
| 3 仓库生命周期 | 是 | 绿 |
| 4 制品 roundtrip | 是（含校准项复核全部通过：DELETE 204/重复 404、checksum-deploy 三态、幂等重传免覆盖、mkdir 201、item info 字段全集与 size 字符串） | 绿 |
| 6 认证与 ACL | 是（C02~C23/C27 双模式 + 加测） | **红（D2/D3）** |
| 7 边界拒绝 | 是（E-26 矩阵 10 变体 + 路径穿越 7 变体） | 绿 |

场景 2/5/8/9/10 归 T-19（存储完整性/性能/持久化/README 复跑），不在本票范围。

**总结论：FAIL**——功能面（存储、roundtrip、协议语义、边界安全）全部达标且质量扎实；但 FR-5-AC8 的 C22b 断言点未过（D2），且 D3 与之叠加形成管理面读数据的权限旁路。两缺陷均集中在管理面授权分级，建议以一张修复票收口（httpapi 管理端点 + token 签发的 admin-only 检查统一收口），修复后仅需回归场景 6（预计 10 分钟），无需全量重跑。

## 环境与清理

- 全程使用 `mktemp -d` 临时数据目录与随机端口，两轮实例（第一轮主测、第二轮边界精确定界）均已停止，临时目录与凭据文件已删除；仓库自带 `data/` 目录未动（测试前后均为空）；无 Docker 资源、无残留进程。测试口令均为一次性随机值，未写入任何提交文件。
