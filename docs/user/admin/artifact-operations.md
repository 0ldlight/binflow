---
title: 制品操作族（copy / move / zip / archive! / explode）
sidebar_position: 50
---

# 制品操作族（copy / move / zip / archive! / explode）

> 适用版本：M12 起（T-339 copy/move + T-343 归档族；行为规格 `docs/reverse/repo-operations.md`，Q4 终裁：整族照搬 **pro 门控**）。**M13 增补**：`folder_download` 六字段配置旋钮落地（T-368，默认不变）。整族共用一个 feature 槽 **`repo-operations`**——community 实例对族内任何动词答 **403 + `X-Binflow-License-Required: repo-operations`**（真二进制 curl 实测，T-339 §7）。
> 本文命令取 T-339/T-343 工作日志的真实输出（真二进制 + curl / httptest 真服务端栈）；旋钮段为 T-375 双实例实测（2026-08-30）。

## 用途

五个动作覆盖 Artifactory 的制品生命周期操作面：

| 动作 | 端点 | 一句话 |
|---|---|---|
| copy | `POST /binflow/api/copy/{srcRepo}[/{srcPath}]?to=/…` | 树级复制（零拷贝，blob 引用复用） |
| move | `POST /binflow/api/move/{srcRepo}[/{srcPath}]?to=/…` | 树级搬移（copy + 源删除） |
| 目录/整仓打包下载 | `GET /binflow/api/archive/download/{repo}[/{path}]?archiveType=…` | 流式 zip / tar / tar.gz，不落盘 |
| 归档内成员读取 | `GET /binflow/{repo}/{archive}!/{entry}` | 不解包直接读归档里的单个文件 |
| 解包部署（explode） | `PUT /binflow/{repo}/{path}` + `X-Explode-Archive: true` | 上传归档的同时原地展开 |

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）与 **pro 及以上 license**（槽 `repo-operations`；`GET /api/v1/addons` 可见其实时判定）。
- copy/move 是逐文件权限管线：源路径需要 `read`，目标需要 `write`；move 另需源的 `delete`（见下方校验链）。
- 门序：**认证 → RBAC → license**（匿名 401 先于一切；T-283 裁定 RBAC 先于 license 门）。

## copy / move

### 端点形状

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>

# 复制单个文件
curl -su admin:$ADMIN_PW -X POST \
  "$BASE/binflow/api/copy/generic-local/acme/a.bin?to=/staging/a.bin" | jq .
# {
#   "messages": [
#     {"level":"INFO","message":"copying generic-local/acme/a.bin to staging/a.bin completed successfully, 1 artifacts and 0 folders were copied"}
#   ]
# }

# 干跑（不落任何数据，消息带 Dry run 前缀）
curl -su admin:$ADMIN_PW -X POST \
  "$BASE/binflow/api/copy/generic-local/acme?to=/staging/acme&dry=1"
# messages[0].message 前缀 "Dry run for "

# 搬移目录树（源目录搬空后自底向上剪除）
curl -su admin:$ADMIN_PW -X POST \
  "$BASE/binflow/api/move/generic-local/acme?to=/archive/acme"
```

参数（query）：

| 参数 | 取值 | 行为 |
|---|---|---|
| `to` | `/{targetRepo}[/{targetPath}]`（必填） | 目标；空 key → 400 `Target repository key is empty` |
| `dry` | 0/1 | 干跑（默认 0）；dry 与真实跑同一聚合规则 |
| `failFast` | 0/1 | 解析接受；BinFlow 逐项管线天然即部分成功姿态 |
| `suppressLayouts` | 0/1 | 解析接受；跨布局翻译不实现，所有取值行为等同 1 |

- 响应 **200 + `{"messages":[{"level","message"}]}`**，Content-Type 为 Artifactory 的 vendor 形 `application/vnd.org.jfrog.artifactory.storage.CopyOrMoveResult+json`；errors 在前 warnings 在后。HTTP 状态 = 最后一条 error 的码，error 无码 → 409 兜底，无 error → 200。
- `/api/flat/copy`、`/api/flat/move` **不实现**（404——Artifactory 默认部署同样如此）。
- 大树实测：万节点（10,101 节点）dry run 570ms，零 5xx，抽样 sha256 对账一致（T-339 服务层矩阵）。

### 校验链（顺序即优先级）

| 序 | 检查 | 拒绝形态 |
|---|---|---|
| 0 | 目标 remote/virtual | 400（copy/move 只落 local） |
| 0 | 源/目标 repo 不存在 | 400 `Could not calculate repo path from src=…, target=…: repository <key> not found` |
| 0 | 源 == 目标 | 400 `Skipping <verb> <path>: Destination and source are the same` |
| 1 | 源读权限 | 403（逐字 Artifactory 文案） |
| 3 | 目标 include/exclude 模式 | 403 |
| 4 | move 的源删除权限 | 403 |
| 5 | 目标已存在且无删权限 | **401**（override 消息，Artifactory 特例） |
| 6 | 目录落文件下 | 400 |
| 7 | 目标写权限 | 403 |

BinFlow 扩展：目标仓配额逐项检查（超限逐文件 413 消息，不失败整请求）。

### 数据语义

- **零拷贝**：blob 台账引用复用，物理字节不搬（测试断言台账行数不变）；属性全量随行（覆盖 = 删旧再拷，旧属性不残留）；`created`/`createdBy`/`modified` 随行（`modified_by` 列不存在，如实登记）。
- **源仓型**：local 直读；remote = 本地缓存行树 + 文件 miss 经引擎回源落地后拷贝；virtual = 文件取首个持有成员（local 持有或 remote 缓存行）、目录取首个持有 folder 行成员；**move 源为 virtual → 400**（删除不穿虚拟，RE-08 同款措辞）。
- copy 成功后异步触发 CopyMoveObserver（候选目录去重排序；move/dry 不触发）——复制引擎的推送面随之收敛。
- 审计：op 级一条 `artifact.copy` / `artifact.move`。

## 目录 / 整仓打包下载

```bash
curl -su admin:$ADMIN_PW \
  "$BASE/binflow/api/archive/download/generic-local/acme?archiveType=zip" -o acme.zip
# 需要 checksum 伴随文件时：
curl -su admin:$ADMIN_PW \
  "$BASE/binflow/api/archive/download/generic-local/acme?archiveType=zip&includeChecksumFiles=true" -o acme.zip
```

- `archiveType` 必填枚举：`zip`（Deflate）/ `tar` / `tar.gz|tgz`（gzip）——缺省或未知值 400。
- 仅 **local 仓**：仓不存在 404 `…is not a repository.`；remote/virtual 404 `only available for local (or cache) repositories`；路径缺失 404 / 路径是文件 400；无读权限 403。
- `includeChecksumFiles=true`：从 blobs 台账**生成** `.sha1/.md5/.sha256` 伴随条目（BinFlow 磁盘无边文件，ADR-0006——这是本条目的 BinFlow 语义）。
- 流式打包不落盘（io.Pipe），审计一次 DOWNLOAD 行（带 archiveType/files/bytes，非逐文件）。

**限额与开关（folderDownloadConfig 六字段，M13 起可配）**：默认 `enabled=false`、`enabledForAnonymous=false`、`maxDownloadSizeMb=1024`、`maxFiles=5000`、`maxConcurrentRequests=10`、`enabledEmptyDirectories=false`；超限/超并发按 Artifactory 逐字消息拒绝（MB=1024²）。

**配置旋钮（`folder_download` 段，M13 T-368 落地——重启生效）**：

```yaml
# binflow.yaml（Artifactory folderDownloadConfig 六字段 → BinFlow snake_case 拼写）
folder_download:
  enabled: true                     # 总开关；BINFLOW_FOLDER_DOWNLOAD__ENABLED
  enabled_for_anonymous: false      # 匿名打包下载（实例关匿名时开了也 401）
  max_download_size_mb: 1024        # 0 = 不限
  max_files: 5000                   # 0 = 不限
  max_concurrent_requests: 10
  enabled_empty_directories: false  # 空目录条目入包
```

- 生效值可经 `GET /binflow/api/v1/system/settings` 回显核对（admin / readonly_admin；实测缺省实例回 `"enabled": false` + `1024/5000/10`）。
- 语义要点（实测）：开 `enabled` 后目录打包 200（zip 条目与树一致）；**匿名腿** = `enabled_for_anonymous=false` 或实例关匿名 → 401 `You must be logged in to download a folder or repository.`；关 `enabled` → 403 `Download Folder functionality is disabled.`；`max_files=1` 超限 → 400 逐字文案。camelCase 拼写（`maxDownloadSizeMb` 等）被 strict schema **拒绝**——从 Artifactory 复制配置请改 snake_case。

## 归档内成员读取：`archive!/`

```bash
# 读 zip 里的单个文件（内容面，匿名读默认开启时无需凭据）
curl -s $BASE/binflow/generic-local/acme/bundle.zip!/README.md

# 嵌套归档：成员路径里再遇 !/ 会递归下钻
curl -s $BASE/binflow/generic-local/acme/bundle.zip!/inner.jar!/META-INF/MANIFEST.MF

# 成员 checksum（按需流式计算，返回裸 hex）
curl -s $BASE/binflow/generic-local/acme/bundle.zip!/README.md.sha256
```

- 按**首个 `!/`** 切分（解码后）；成员名**逐字匹配**、不折叠点段（`./strict.txt` 只匹配 `./strict.txt`，折叠形 404）；非 GET → 405 `Allow: GET`。
- 可读归档集：zip 家族（zip/jar/war/ear/nupkg/conda/apk）+ tar 家族（tar/tar.gz/tgz）；**bz2/xz/7z 不支持**（Go 标准库无 reader，白名单外 400）。
- 权限骑归档路径本身的读门（经存储层 Get 继承：匿名 401 / 认证无权 403 / remote 拉取 / virtual 成员路由全继承）；BinFlow 无 `archiveBrowsingEnabled` 类开关（V-6 负向钉死）。
- 404 逐字：`Unable to find zip resource: '<entry>' using full URI '<uri>'`；嵌套中间层有 256MiB 有界缓冲（外层始终流式）。

## 解包部署：explode

```bash
# 上传 zip 并原地展开（归档文件本身不落库）
curl -su admin:$ADMIN_PW -T bundle.zip \
  -H 'X-Explode-Archive: true' \
  $BASE/binflow/generic-local/acme/bundle.zip -i | head -3
# HTTP/1.1 201 Created
# X-Binflow-Exploded-Files: 7        ← BinFlow 新增的条目计数头
# （body 为空）

# 全有或全无模式（失败时补偿式回滚已落地条目）
curl -su admin:$ADMIN_PW -T bundle.zip \
  -H 'X-Explode-Archive-Atomic: true' \
  $BASE/binflow/generic-local/acme/bundle.zip
```

- 白名单 = `zip` / `tar` / `tar.gz` / `tgz`（闭集）；白名单外 / 无扩展名 → 400 逐字；PUT 路径尾斜杠 → 400 `Explode archive deployment failed, Missing file name.`；头值非 `true`/`false` 拼写 → 显式 400。
- 成功 **201 + 空体**（文档优先裁决，V-1）+ `X-Binflow-Exploded-Files: <n>` 计数头；**归档原件不落库**（随后 GET 404），每个条目经标准存储管线落地（权限/配额/审计/复制钩子全链随行）。
- 默认（非 Atomic）accumulate-then-answer：条目失败记录后继续，首个错误定响应码，部分落地保留；`X-Explode-Archive-Atomic: true` = 补偿式回滚已落地条目。
- 排除项：首段 `.jfrog` 系统文件、文件名含 `maven-metadata.xml`（静默跳过）；**zip-slip 防御**：`../`/绝对路径/空段条目 → 整单 400（BinFlow 安全新增）。
- 目标父目录无 `w` → 403 `User is not authorized to deploy to specified repo path.`；匿名 401；remote 目标 405。
- CORS：`X-Explode-Archive-Atomic` 已加入 allow-headers（控制台/浏览器直传可用）。

## pro 门控姿态（整族一槽）

```bash
# community 实例（真二进制 curl，T-339 实测）
curl -u admin:password -X POST "$BASE/binflow/api/copy/src/a.bin?to=/dst/a.bin" -i
# HTTP/1.1 403 Forbidden
# X-Binflow-License-Required: repo-operations
# {"errors":[{"status":403,"message":"license required: addon 'repo-operations' needs tier 'pro' (current: none)"}]}
```

- 与 Artifactory 的差异（有意登记）：Artifactory 自家 addon 拒绝是 400 text/plain；BinFlow 统一走 403 + errors[] 信封 + 头（D4 形态，CI 可按头分支）。
- 匿名请求先吃 401（认证门先于 license 门）；GET /api/copy/** → 404（仅 POST 有路由）。
- license 降级语义与包型一致：**已复制/搬移的数据不受影响**，只有动词关门。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 403 + `X-Binflow-License-Required: repo-operations` | community 档 / license 过期（D2） | 装 pro license；读面不受影响 |
| 400 `…only be performed on local repositories` | 目标是 remote/virtual（或 DELETE 类似形态） | copy/move 只落 local |
| 400 `Skipping … Destination and source are the same` | 源 == 目标 | 检查 `to` |
| 401（override 消息） | 目标已存在且当前用户无删权限 | 授 delete 或换目标路径 |
| 403 `…not authorized to deploy…` | explode 目标父目录无 `w` | 授 write |
| 400 `Explode archive deployment failed, Missing file name.` | PUT 路径尾斜杠 | 给出目标文件名 |
| 400（zip-slip） | 归档条目含 `../`/绝对路径 | 修归档内容 |
| 404 `Unable to find zip resource: …` | 成员名拼写/点段不逐字 / 归档不含该成员 | 用归档工具核对成员名 |
| 打包下载 403（已认证且有读权限） | `folder_download.enabled` 默认 `false`（M13 起可开，重启生效） | 配置段置 true 重启；临时用 `!/` 成员读取或逐文件下载 |

## 下一步

- 删除捕获与回收站（同一生命周期的下半程）：[Trash can 管理](trash-can.md)
- 档位与门控语义：[License 与 Add-ons 管理](license.md)
- 端点契约速览：[API 参考 · M12 增补速览](../api-reference.md#m12-增补速览t-347a)
