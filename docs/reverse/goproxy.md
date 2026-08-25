# Go 包型（GOPROXY 协议）行为规格

- 票据：T-278（FR-87 前置规格票）；实现票 T-285（`internal/adapter/goproxy`，architecture §15.2.4）。
- **行为基准：go.dev/ref/mod「GOPROXY protocol」章**（官方规范，有公开规范即以官方为准）。反编译（`reverse-src/artifactory/src/batch2-protocol/com/jfrog/ph/go/**`，inv-3 §2.3 GO 行）只补规范空白，逐条标注「此条补充官方规范」。
- 本规格同时标注三类内容：**官方规范**（高/中按双证情况）、**反编译补充**（中为主）、**BinFlow 决策**（PRD FR-87 / ADR-0008 定案，非逆向结论，实现票直接引用）。

---

## 1. 基础路径形态（置信度：高——PRD/ADR 定案 + Artifactory 对照）

| 系统 | 形态 | 说明 |
|---|---|---|
| BinFlow（实现口径） | `$BASE/binflow/<repoKey>/<module-path>/@v/...` | `$BASE` = scheme+host(:port)。`/binflow` 前缀为 ADR-0008/E-26 定案；repoKey 直接作第二段，与其余五包型内容面同构（`internal/adapter.Handler` 挂载模型）。**无额外 `goproxy` 路径段**——票据文本的 `$BASE/binflow/goproxy/<repo>/...` 为协议族指称，PRD FR-87 87.1 与 AC2/AC3 字面（`GOPROXY=$BASE/binflow/go-local`、`$BASE/binflow/go-remote`）是权威口径。 |
| Artifactory（参考对照） | `$BASE/artifactory/api/go/<repoKey>/...` | 官方挂载点（JFrog 公开文档口径），BinFlow 不沿用 `/api/go` 段。 |
| 协议形态 | `$base/$module/@v/<$version>.<ext>` | `$base` 之后整体由 GOPROXY 协议定义（官方规范）。 |

路由分发：httpapi 剥离 `/binflow` 前缀后按首段解析 repoKey，按该仓 `packageType=go` 分发到 goproxy adapter——与五包型完全同构，`Protocol()="go"`（包名 `goproxy` 规避 Go 关键字，architecture §15.2.4）。

---

## 2. 端点表（置信度逐行标注）

| 方法 | 路径（相对 `$BASE/binflow/<repoKey>/`） | 语义 | 成功响应 | 错误响应 | 置信度 |
|---|---|---|---|---|---|
| GET | `<module>/@v/list` | 已知版本清单，纯文本每行一个版本；**不应含 pseudo-version** | 200，`text/plain`，版本号/行 | 模块不存在 404；空清单返回 200 空体 | 高（官方 + 代码） |
| GET | `<module>/@v/<version>.info` | 版本元数据 JSON | 200，`application/json`，`{"Version":"v1.0.2","Time":"2024-01-02T03:04:05Z"}` | 404（未找到，可回退） | 高（官方 + 代码） |
| GET | `<module>/@v/<version>.mod` | 该版本 go.mod 原文（无则合成 `module <path>` 单行） | 200，`text/plain; charset=utf-8` | 404 | 高（官方 + 代码） |
| GET | `<module>/@v/<version>.zip` | 模块源码 zip（module zip 格式，官方「Module zip files」章） | 200，`application/zip` | 404 | 高（官方 + 代码） |
| GET | `<module>/@latest` | 最新版本元数据（与 .info 同构）；**官方标注可选端点**，客户端仅在 list 无可用版本时才请求 | 200，`application/json` | 404 | 高（官方 + 代码） |
| GET | `<repoKey>/`（仓根） | 仓可用性探针（go 客户端不调用） | 200 空体 | — | 中（仅代码） |
| PUT | `<module>/@v/<version>.zip` | 上传模块 zip（**厂商扩展——官方 GOPROXY 协议只读无上传**；JFrog/Artifactory 扩展行为，BinFlow 沿用作 local 上传通道，PRD 87.1） | 201（沿用 BinFlow 内容面上传成功码） | 400 校验失败 / 403 无权限或门控 / 409 checksum 不符 | 中（代码 + PRD 决策；官方无此端点） |
| PUT | `<module>/@v/<version>.mod` | 上传 go.mod | 同上 | 同上 | 中（同上） |
| PUT | `<module>/@v/<version>.info` | 上传版本元数据 JSON | 同上 | 同上 | 中（同上） |
| HEAD | `<module>/@v/<version>.mod` | 存在性探测（remote 仓路径上可见） | 200 + `Content-Length` | 未命中 **410 Gone** | 中（仅代码；官方未定义 HEAD） |
| GET | `sumdb/sum.golang.org/supported`、`.../lookup/<mod>@<ver>`、`.../tile/<tile>` | sumdb 代理三端点 | — | — | **M10 不做（M11+ 登记，见 §9）**；BinFlow 对这些路径按未知路径 404 处理 |

状态码总语义（官方，高）：

- 200 成功；3xx 重定向客户端会跟随；**404/410 = 该代理没有此模块/版本，可能别处有**（GOPROXY 列表逗号分隔时客户端仅在收到 404/410 才回退下一源；管道符分隔任何错误都回退；其余 4xx/5xx 一律终止）。
- 错误响应体 `Content-Type: text/plain`，charset utf-8 或 us-ascii。
- BinFlow M10 错误面统一用 404 表「未找到」；410 语义（已撤回/retract）M10 不实现，端点预留。
- 门控叠加：addon `go` 槽位（pro）未解锁时——建仓 403（errors[] 指名 addon 与档位）、push 403（响应头 `X-Binflow-License-Required`）、**既有内容 pull 200**（降级不劫持数据，PRD FR-85/FR-87 AC5）。此为 BinFlow 门控决策，非逆向结论。

---

## 3. 存储布局：module path escape 与路径映射

### 3.1 `!lower` 转义（置信度：高——官方规范原文 + 反编译 encodeName/normalizeName 双证）

官方规范原文口径：为避免大小写不敏感文件系统的歧义，`$module` 与 `$version` 元素做 case-encoding——**每个大写字母替换为叹号 + 对应小写字母**（`example.com/M` ↔ `example.com/!m`）。这是 GOPROXY 区别于其他包型的规范特色，必须精确实现：

| 方向 | 规则 | 边界 |
|---|---|---|
| wire → 内部（unescape/normalize） | `!x` → `X`（x 为任意字符，取 `Character.toUpperCase`） | **连续两个 `!`（`!!`）非法**，视为畸形模块名 → 400（代码显式抛 IllegalArgumentException）。BinFlow 在 Layout 解析时做此校验。 |
| 内部 → wire（escape/encode，回源上游时） | 大写字母（`Character.isAlphabetic` 且 `c == toUpperCase(c)`）→ `!` + 小写 | 小写与非字母原样透传。版本与模块路径同一套规则（版本如 `v1.0.0-BETA` wire 形态为 `v1.0.0-!b!e!t!a`）。 |
| percent-encoding | 官方完整 escapePath 对不允许字符另有百分号编码；Artifactory 的 encode/normalize 只处理 `!` 分支，另在 lookup 路径上可见 `%2F` → `/` 的解码处理 | BinFlow 建议：客户端正常请求不会出现需要 %-encode 的字符（模块路径字符集受限）；Layout 对非法 percent-encoding 沿用既有 400 路径校验（adapter.ErrBadRequestPath）。 |

### 3.2 存储路径映射（置信度：中——仅反编译可见，此条补充官方规范）

**存储使用解码形态（大写还原）**，转义只存在于 wire：

```
wire:     example.com/!my!mod/@v/v1.0.2.zip
存储路径:  <repoKey>/example.com/MyMod/@v/v1.0.2.zip      （模块段与版本段均解码后落盘）
回源 URL:  <upstream>/example.com/!my!mod/@v/v1.0.2.zip   （再次转义后拼上游）
```

- 目录推导规则：`<decoded-module>/@v/<decoded-version><ext>`，ext ∈ {`.zip`, `.mod`, `.info`}；物理 filestore 仍走 BinFlow 既有 blob 寻址（`filestore/<sha1[:2]>/<sha1>`，storage-layout.md），`@v` 结构只存在于逻辑路径。
- remote 缓存仓（`<repoKey>-cache` 等价机制）同形态落盘，另有**内部元数据标记**（不可作为模块内容被访问）：`<module>/@v/.versionList`（上游 list 缓存）、`<module>@latest.latest`（上游 @latest 缓存——注意无 `/`，反编译可见的形态怪癖）。此条补充官方规范。
- 可过期（expirable）路径集合，用于 remote 缓存刷新判定：`.info`、`.versionList`、`.latest`（及 M11+ 的 `sumdb/*`）。`.mod`/`.zip` 不可过期——官方要求代理对同一版本**永远返回相同内容**。此条补充官方规范。

### 3.3 节点属性（置信度：中——仅反编译可见，此条补充官方规范）

| 属性 | 值 | 用途 |
|---|---|---|
| `go.name` | 解码后的完整模块路径 | `@v/list` 的聚合键（按此属性搜索 + 过滤 `.zip` 结尾的节点） |
| `go.version` | 解码后的版本号 | 版本清单来源 |
| `package.lowercase` | 模块路径小写 | 大小写不敏感搜索兜底 |

上传 `.mod`/`.zip` 后索引器行为：读取同级伴随 `.mod`（路径 ext 替换），解析其中首个 `module <path>` 指令（去引号、按空白切分），将三属性写到 `.mod` 与 `.zip` 两个节点上；`.mod` 缺失时 zip 上传跳过索引（版本回退取文件名去扩展名）。BinFlow 元数据字段命名可自定，但必须支撑「按模块聚合 zip 版本集」的 list 语义。

---

## 4. 语义流程（local 仓）

### 4.1 GET `.info`（置信度：流程中、契约高）

1. 存储 `.info` 存在 → **逐字节回读**（PUT 什么 GET 什么，服务端不重写已存文件——PRD AC1 的 sha256 对账依赖此点）。
2. `.info` 不存在但 `.zip` 存在 → **合成**：`{"Version":"<version>","Time":"<zip lastModified>"}`（RFC3339 UTC），同时把合成 `.info` **回写存储**（系统生成、可过期）。此条补充官方规范。
3. 都不存在 → 404（错误体 `text/plain`）。
- 官方契约（高）：`.info` 的 `Version` 字段必填且必须 canonical；请求路径里的 `<version>` 可以不是 canonical（可用于按分支名/revision 查询），但当请求版本是 canonical 且 major 与模块路径兼容时，响应 `Version` 必须与之一致；`Time` 可选、RFC 3339；其余字段名保留（BinFlow 合成时只输出 `Version`/`Time` 两键——反编译合成分支会序列化出额外字段，客户端容忍未知字段，但 BinFlow 收敛为最小集）。

### 4.2 GET `.mod`（置信度：流程中、契约高）

1. 存储 `.mod` 存在 → 逐字节回读（原文、不修改——官方「original, unmodified go.mod」要求）。
2. 404 且版本以 `+incompatible` 结尾 → **合成** `module <module-path>\n` 单行文本 200（官方要求：模块无 go.mod 时必须返回仅含 module 语句的合成文件；反编译仅在 +incompatible 场景触发合成分支）。此条补充官方规范（触发时机）。
3. 其余 404。

### 4.3 GET `.zip`（置信度：高）

- 存在 → 下载（Content-Type `application/zip`，带 `X-Checksum-Sha256` 等实测校验头，逐字节）。
- 不存在 → 404，`text/plain` 错误体。
- zip 内容格式约束归客户端域（官方「Module zip files」章：条目前缀 `<module>@<version>/`、zip 与解压各 ≤500MiB、go.mod/LICENSE ≤16MiB、无 vendor/、无嵌套模块、大小写折叠唯一）——**服务端上传时不校验 zip 内部结构**（反编译确认上传链只走 checksum/索引，不解析 zip 内容）；BinFlow 沿用（PRD 87.3：.zip 只走 checksum 校验链）。

### 4.4 GET `@v/list`（置信度：中——行为反编译可见，输出形态为 BinFlow 收紧决策）

1. 按属性 `go.name == <module>` 搜索仓内节点，过滤 `.zip` 结尾（**只有 .zip 存在该版本才进清单**——.mod/.info 单独存在不注册版本）。
2. 每版本取 `go.version` 属性，输出每行一个版本。
3. 反编译形态在版本后附加空格 + 时间戳列（`v1.0.2 2024-01-02T03:04:05Z`）——官方未定义该列，真实 go 客户端实测兼容（Artifactory 生产在用反证）。**BinFlow 决策：只输出纯版本号列**（与 proxy.golang.org 形态一致，规避未定义行为）；remote pull-through 场景上游 list 原文透传缓存，不做改写。
4. 无任何版本 → 404「Not found」（模块不存在与零版本在 local 面都可 404；官方允许空清单 200，BinFlow local 无 zip 即视为模块未知，取 404——与官方「空 list 200」的张力记录在 §8）。

### 4.5 GET `@latest`（置信度：高——候选序官方定义，实现中）

1. 收集该模块全部版本（同 list 来源）。
2. 候选优先序（官方）：**语义最高 release 版 > 语义最高 pre-release 版 > 时间最近 pseudo-version**。
3. 取胜出版本的 `.info` 响应返回（含 4.1 的合成链）。
4. 无候选 → 404。

### 4.6 PUT 三件套（置信度：中——反编译 + PRD 决策）

1. 鉴权/门控 → 无权限 403（Artifactory 错误体：`{"error": "forbidden", "reason": "User is not allowed to deploy a package"}`；BinFlow 用既有错误信封）。
2. 校验链（见 §5）→ 失败 400/409。
3. 落盘到 `<decoded-module>/@v/<decoded-version><ext>`，矩阵参数（`;k=v`）与 `X-Artifactory-Property-*` 式附加属性随节点存储（BinFlow：矩阵参数走 FR-89 的 calculateRepoPath 等价单点，不另造机制）。
4. 成功后索引（§3.3）。
5. 校验头：反编译读取 `X-Checksum-Sha1` / `X-Checksum-Md5`（go 层未读 sha256，但 BinFlow 内容面统一支持 `X-Checksum-Sha256/-Sha1/-Md5`——见 §5.3）。

---

## 5. 校验链

### 5.1 版本号合法性（置信度：高——官方 + 反编译正则双证）

URL 与 `.info` 中的版本必须满足（canonical 定义，官方）：

```
canonical:      vMAJOR.MINOR.PATCH[-prerelease][+incompatible]
pseudo-version: vX.0.0-yyyymmddhhmmss-abcdefabcdef
              | vX.Y.Z-pre.0.yyyymmddhhmmss-abcdefabcdef
              | vX.Y.(Z+1)-0.yyyymmddhhmmss-abcdefabcdef
```

反编译参考锚点（等效正则，供实现票直取）：

| 正则 | 语义 |
|---|---|
| `^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$` | 标准 release（无前导零） |
| `^v\d+\.\d+\.\d+(\+incompatible)?$` | release + `+incompatible` 形态 |
| `^v[0-9]+\.(0\.0-\|\d+\.\d+-([^+]*\.)?0\.)\d{14}-[A-Za-z0-9]+(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$` | 三形态 pseudo-version |
| `^v\d+\.\d+\.\d+.*` | @latest 候选粗筛（非 canonical 的分支名等允许出现在请求路径） |

- 请求路径版本非 canonical **不直接 400**（官方允许以分支名/revision 查询 .info）；但对 **PUT**，BinFlow 要求版本 canonical 或 pseudo-version 或 `+incompatible` 形态，否则 400（PRD 87.3「版本号合法性校验」）。
- major 版本一致性：模块路径 `/vN` 后缀（N≥2）必须与版本 major 一致（官方模块路径规则；gopkg.in 特例 `.vN` 点后缀）。校验失败 → 400（PUT）。

### 5.2 `.info` / `.mod` 内容校验（PUT 时；置信度：中——PRD 87.3 决策，契约源自官方）

- `.info`：必须是合法 JSON object；`Version` 必填、canonical、且与 URL 版本一致；`Time` 若存在必须 RFC 3339。失败 → 400（PRD「JSON 语法与版本号合法性校验（bad request 400）」）。
- `.mod`：必须可解析出 `module <path>` 指令，且模块路径与 URL 模块路径一致（解码后比较）。失败 → 400。
- 说明：**Artifactory 上传时不做上述内容校验**（反编译上传链无 JSON/mod 解析校验，任意字节可入）——此为 BinFlow 加严（PRD 决策），规格以 PRD 为准。

### 5.3 `.zip` checksum 链（置信度：高——BinFlow 既有 SPI + 官方约束）

| 时机 | 行为 | 错误 |
|---|---|---|
| PUT（客户端声明校验和） | 支持 `X-Checksum-Sha256/-Sha1/-Md5`；malformed（宽度/十六进制）→ 400；格式合法但与实测不符 → **409**（client-checksums 策略，repo-semantics.md §5 / adapter.ErrInvalidChecksum） | 400 / 409 |
| PUT（未声明） | 服务端计算 sha256/sha1/md5 入库（`binaries`/`nodes` actual 列） | — |
| 下载 | 响应带实测 `X-Checksum-*` 头；blob 内容寻址不可变 → 无需逐次重算（「下载时回算」由存储模型兜底：blob 名即 sha1，路径命中即内容一致） | — |
| remote pull-through | 上游响应若带 `X-Checksum-*` 头则比对（GOPROXY 上游通常不带 → 直接落盘实测）；上游 404/410 透传 404 | 比对不符按 remote 语义拒绝缓存并回错 |
| h1: 哈希 | `h1:<base64>` 是**客户端域**（对 zip 内排序文件清单+内容的 SHA-256 dirhash，zip 容器本身不参与哈希——文件顺序/压缩不影响），**服务端不计算不存储**；go.sum/sumdb 校验全在客户端 | — |

### 5.4 PUT 幂等 / 覆写语义（置信度：高——沿用 BinFlow 既有面）

- 同路径同 checksum 重传 → 幂等成功（不触发覆盖权限检查）。
- 同路径不同 checksum → 覆盖，需对旧节点 DELETE 权限，否则 403。
- 张力登记（不阻塞）：官方要求代理对 `.mod`/`.zip` 同版本内容不可变；BinFlow local 覆盖语义允许改写。默认取舍 = 沿用既有 local 语义（与 generic 一致，客户端侧由 go.sum/sumdb 兜底检测），如需收紧（已存在版本拒绝异内容覆写 → 409）留给后续 ADR，M10 不引入新配置项。

---

## 6. rclass 三态行为

### 6.1 local（置信度：高）

- GET 五端点全量（§4）；PUT 三端点全量（§4.6）。

### 6.2 remote（GOPROXY 上游代理 + 缓存；置信度：中——反编译 registry 模式 + PRD 决策）

- 上游：仓配置 `url`，默认 `https://proxy.golang.org`（PRD 87.1）；上游请求路径 = `<url>/<escaped-module>/@v/<escaped-version><ext>`（**重新转义后拼上游**，§3.1）。
- GET `.info`/`.mod`/`.zip`：缓存命中（remote-cache 同形态路径）→ 直接回缓存；未命中 → 回源、落缓存、给缓存节点打 `go.name`/`go.version`/`package.lowercase` 属性（供 list/搜索）。
- GET `@v/list` / `@latest`：回源取原文，缓存到 `.versionList` / `.latest` 内部标记路径，按 §3.2 可过期集合参与缓存刷新。
- `.mod` 且版本 `+incompatible` → 不回源直接合成（§4.2）。
- 上游故障：对齐 BinFlow 既有 remote 语义（超时/blacked-out/SSRF 防护复用 M3 机制，PRD 87.1）；list/@latest 读取失败按 404 处理（不 5xx——官方 404/410 才允许客户端回退逗号列表）。
- offline 仓：list/@latest 从缓存降级服务（反编译显式分支）。
- PUT：拒绝。Artifactory 返回 400「Unable to publish go package into a remote repository.」；BinFlow 对齐既有内容面 remote 写拒绝行为（以 httpapi 现状为准，405 或 400，实现票核对既有 npm/pypi remote 写拒绝码后统一）。
- **分歧点（PRD 决策，非逆向结论）**：Artifactory 对「直接访问 remote go 仓」显式 400（「remote repository cannot be used without a virtual repository…」，`assertRemoteRepoBlock`）；**BinFlow 允许 `GOPROXY=$BASE/binflow/go-remote` 直接使用**（PRD FR-87 AC3 字面）。规格以 PRD 为准，Artifactory 该行为仅登记不采纳。

### 6.3 virtual（聚合；置信度：中）

- `.zip`/`.mod`/`.info`：**按成员序遍历、首个命中即返回**（first-found）。本地优先 = 把 local 成员排在成员列表前（PRD AC4：本地优先命中本地、未命中走 remote）。命中判定 = 该成员仓能对此路径给出 200。
- `@v/list`：**跨成员并集**（按成员序去重合并；同一版本多成员取首个时间戳）。排序无强约束（客户端自行语义排序）；BinFlow 建议稳定输出：成员序 + 字典序。
- `@latest`：跨成员收集各成员 latest 候选，按 §4.5 候选序取全局最优，返回该成员的原始响应体。
- PUT：按 BinFlow 既有 virtual 部署语义（deploy target 路由），不另造机制。
- external dependencies 重定向（GoExternalDependenciesHelper 模式分流）：**M11+ 不做**（§9）。

---

## 7. 真实客户端命令清单（qa 可直接引用；映射 PRD L10–L13）

### 7.1 环境变量矩阵（置信度：高——官方「Private modules」章）

| 变量 | 作用 | BinFlow 场景 |
|---|---|---|
| `GOPROXY` | 代理 URL 列表，逗号（仅 404/410 回退）/ 管道（任何错误回退） | `GOPROXY=$BASE/binflow/go-local`；多源 `GOPROXY=$BASE/binflow/go-virt,https://proxy.golang.org,direct` |
| `GOPRIVATE` | 私有模块 glob；**同时是 GONOPROXY/GONOSUMDB 的默认值** | 私有全走 BinFlow：`GOPRIVATE='*'`（AC2 用法）；精确：`GOPRIVATE=example.com/*` |
| `GONOPROXY` | 不走代理、直连 VCS 的模块 | 一般不用（BinFlow 是唯一源） |
| `GONOSUMDB` | 不对 sum.golang.org 校验的模块 | 私有模块前缀，如 `GONOSUMDB=example.com`（官方「Private proxy serving all modules」配方） |
| `GOSUMDB` | `off` 完全关闭 sumdb（air-gapped QA 用） | 断网环境必设，否则 sumdb 校验卡死 |
| `GOINSECURE` | 允许 http 明文拉取的模块前缀；**不关闭 sumdb**（常见坑：http 实例需同时 `GOPRIVATE` 或 `GOSUMDB=off`） | `GOINSECURE=$HOST`（http dev 实例） |
| `GOFLAGS` | 附加旗标（如 `-modcacherw`） | 无 go 协议特有旗标 |
| `.netrc` | 代理 HTTP basic 认证（`machine <host>` + `login`/`password`）；或 URL userinfo `GOPROXY=http://user:pass@host/binflow/go-local` | BinFlow 需支持 basic auth（内容面 GET 默认匿名 ADR-0009，PUT 必须鉴权） |

命名勘误（防实现票踩坑）：**`GONOSUMCHECK` 不是真实 go 环境变量**（历史讹传），真实面是 `GONOSUMDB`/`GOPRIVATE`/`GOSUMDB`；`-insecure` 是已废弃的 `go get` 旗标（不是 GOFLAGS 值），现代替代 `GOINSECURE`。

### 7.2 命令清单

```bash
# L10 local 三件套（curl PUT；$ADMIN = user:pass）
curl -su $ADMIN -T mymod.zip "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.zip"
curl -su $ADMIN -T mymod.mod "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.mod"
curl -su $ADMIN -T info.json "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.info"
curl -su $ADMIN "$BASE/binflow/go-local/example.com/mymod/@v/list"            # 含 v1.0.2
curl -su $ADMIN "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.info"     # 逐字节 + sha256 对账
# 大写模块转义验证（存储 example.com/MyMod）：
curl -su $ADMIN "$BASE/binflow/go-local/example.com/!my!mod/@v/v1.0.2.info"

# L11 真实客户端 local（scratch module）
export GOPROXY="$BASE/binflow/go-local" GOPRIVATE='*'
go mod init example.com/scratch
go mod edit -require=example.com/mymod@v1.0.2 && go mod download example.com/mymod@v1.0.2
cat > main.go <<'EOF'
package main
import _ "example.com/mymod"
func main() {}
EOF
go build ./...    # 全链绿

# L12 remote pull-through（二次执行断言回源计数=1 或明显提速）
export GOPROXY="$BASE/binflow/go-remote"
go mod download golang.org/x/mod@v0.17.0 && go mod download golang.org/x/mod@v0.17.0

# L13 virtual（members: go-local, go-remote——本地优先）
export GOPROXY="$BASE/binflow/go-virt"
go mod download example.com/mymod@v1.0.2     # 命中 local
go mod download golang.org/x/mod@v0.17.0     # 未命中走 remote

# http 明文实例补充（dev 环境）
export GOINSECURE="$HOST" GOPRIVATE='*'      # 或 GOSUMDB=off
```

客户端请求时序（qa 断言可依据，官方）：解析 latest 时先 `@v/list` 后 `@latest`（list 无可用版本才请求 @latest）；选定版本后 `.info` → `.mod` → `.zip`。显式版本（`@v1.0.2`）不触发 @latest。

---

## 8. 与公开规范的差异/补充清单（逐条「此条补充官方规范」）

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | PUT 三端点（官方协议只读）及校验头/错误体形态 | 反编译 + JFrog 惯例 | 中 |
| S2 | 存储路径用解码形态（大写还原），wire/存储/回源三态转换 | 反编译 | 中 |
| S3 | `.versionList`/`.latest` 内部缓存标记路径（后者无 `/` 的形态怪癖） | 反编译 | 中 |
| S4 | 可过期集合（.info/.versionList/.latest）与 .mod/.zip 不可过期 | 反编译（官方只给不可变要求） | 中 |
| S5 | `.info` 缺失时由 zip mtime 合成并回写 | 反编译 | 中 |
| S6 | `.mod` 404 + `+incompatible` 触发合成（官方定义了合成义务，触发时机为代码补充） | 反编译 | 中 |
| S7 | `@v/list` 版本行附时间戳列（Artifactory 形态）；BinFlow 收紧为纯版本列 | 反编译 + 生产反证 | 中 |
| S8 | list 仅聚合 `.zip` 存在的版本 | 反编译 | 中 |
| S9 | HEAD `.mod` 未命中 410 | 反编译 | 中 |
| S10 | remote 缓存节点打 go.* 属性、offline 降级、`.mod` +incompatible 不回源 | 反编译 | 中 |
| S11 | virtual first-found / 并集 / 全局 latest 的聚合算法 | 反编译 | 中 |
| S12 | `!!` 双叹号非法 → 400 | 反编译（官方只给正向转义规则） | 中 |
| S13 | local 零版本取 404（官方允许空清单 200；空 200 仅在 remote 透传上游空 list 时出现） | BinFlow 收紧决策 | —（决策） |

BinFlow 明确不采纳的 Artifactory 行为：remote 直连 400 阻断（§6.2 分歧点）；VCS git 直连 remote 模式（§9）；list 时间戳列（S7）。

## 9. 显式不做（M11+ 登记，PRD §2.2）

1. **sumdb 代理**：`sumdb/sum.golang.org/{supported,lookup/*,tile/*}` 三端点不实现（收到按未知路径 404）。影响：客户端不能把 `GONOSUMDB` 指到 BinFlow 做校验库镜像；私有模块用 `GOPRIVATE`/`GONOSUMDB` 前缀跳过公网 sumdb。
2. **external dependencies 重定向**：virtual 仓按模式分流到独立远端仓（GoExternalDependenciesHelper 行为面）不做；BinFlow virtual 只做成员序聚合。
3. **VCS git 直连 remote**（GitHub/GitLab fetcher 家族、`.jfrog/<module>/commits|tags` 缓存、cache cleanup 任务）：不做，remote 仅 GOPROXY registry 上游模式。
4. **curation/Xray 版本过滤**（curated version selection）：不做。

## 10. 置信度分布与待验证清单

- 高：端点表 GET 五行与状态码总语义、`!lower` 转义规则、版本 canonical/pseudo 文法、`.info` JSON 契约、`.mod` 合成义务、latest 候选序、checksum 链 400/409、PUT 幂等/覆写、环境变量矩阵与认证方式。
- 中：PUT 错误体细节、解码存储布局与三态转换、内部标记路径、`.info`/`@latest` 合成回写、list 的 zip-only 聚合、HEAD 410、remote 缓存属性/降级、virtual 聚合算法、`!!` 400、S4 可过期集合。
- 低：无（未发现需降级到「低」的条目；下列待验证项均为中→高的补证点）。

**待验证清单（动态验证即可升 高）**：
1. go 客户端对 list 行尾时间戳列的实际解析行为（S7——BinFlow 已决策输出纯版本列，验证仅为补证）。
2. remote 仓 PUT 拒绝码在 BinFlow 既有 httpapi 的对齐值（405 vs 400，§6.2）。
3. 合成 `.info` 的最小字段集在真实 `go mod download` 下的兼容性（只输出 `Version`/`Time`）。
4. `.zip` GET 的 Content-Type 用 `application/zip` 与真实客户端兼容性（proxy.golang.org 观察值为准核对）。
