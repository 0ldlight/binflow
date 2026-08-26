# Debian 包型（apt 仓库协议 + By-Hash + debPUT）行为规格

- 票据：T-284（FR-91.1 覆盖集第 3 份）；M11 Debian 适配票的前置规格。
- **公开规范锚点（官方优先）**：Debian Wiki [DebianRepository/Format](https://wiki.debian.org/DebianRepository/Format)（仓库格式权威文档：dists 层级、Release 字段、Packages/Sources 索引、by-hash、压缩格式）；apt 客户端行为以该文档 + apt 文档为准。反编译（`reverse-src/artifactory/src/{batch2-protocol/org/jfrog/repomd/debian, batch3-addons/org/artifactory/addon/debian}`，inv-3 §2.4 DEBIAN 行）补规范空白：debPUT 直推与自动索引更新、坐标属性体系、By-Hash 三档策略、trivial 布局、GPG 签名链、管理 REST、virtual 聚合。逐条标注「此条补充公开规范」。
- 置信度：`高` = 官方规范 + 反编译双证（或纯官方定义）；`中` = 仅反编译；`低` = 推断。低置信不作验收依赖。

---

## 1. 基础路径形态

| 面 | 形态 | 说明 | 置信度 |
|---|---|---|---|
| 内容面（apt 客户端访问） | `$BASE/binflow/<repoKey>/dists/...`、`$BASE/binflow/<repoKey>/pool/...` | Debian 是**纯存储路径协议**：apt 直接 GET 仓内文件路径，无独立 API 挂载点（Artifactory 同型：`$BASE/artifactory/<repoKey>/dists/...`）。与 generic 内容面同构，repoKey 第二段。 | 高（官方 + Artifactory 对照） |
| 管理面（reindex/snapshot 等） | Artifactory `$BASE/artifactory/api/deb/...`；BinFlow 建议 `$BASE/binflow/api/deb/...`（与 LC-09 `/binflow/api/...` API 面口径同构）或自有 `/api/v1/repos/{repo}/deb/...`，归 ADR | —（BinFlow 决策待定） |

sources.list 形态（官方）：`deb [选项] <archive-root> <distribution> <component>...`；archive-root 即 `$BASE/binflow/<repoKey>`。

## 2. 仓库布局（layout）

### 2.1 automatic 布局（默认；高——官方定义目录层级 + 代码坐标推导双证）

```
<repoKey>/
  pool/...                                  ← .deb/.dsc 等制品存放区（官方：建议池化但非强制）
    （实际路径由客户端 PUT 决定；Packages 的 Filename 字段 = 仓内相对路径）
  dists/<dist>/
    Release                                 ← 元索引（§4）
    Release.gpg                             ← 脱离签名（有密钥时）
    InRelease                               ← 内联 clearsign 签名（有密钥时）
    <component>/
      binary-<arch>/
        Packages                            ← 未压缩索引（恒生成）
        Packages.gz                         ← 恒生成
        Packages.bz2                        ← 可选格式（默认开）
        by-hash/MD5Sum|SHA1|SHA256/<digest> ← By-Hash 历史地址（§5）
      source/
        Sources（.gz/.bz2/.xz/.lzma 同机制）
```

- 压缩格式：未压缩 + `.gz` 恒生成；可选集合 `optionalIndexCompressionFormats` 默认 `["bz2"]`，可选 `xz`/`lzma`。此条补充公开规范（官方只说"可多格式"，未定默认集）。高（代码 + 官方）。
- 坐标三要素 `<dist>/<component>/<arch>` 不来自路径，来自**制品属性**（§3），索引位置由属性推导（`dists/<dist>/<comp>/binary-<arch>/`）。
- Contents/Translation/diff 索引：官方可选，**BinFlow 不做**（§10）。

### 2.2 trivial 布局（`debianTrivialLayout=true`；中——仅代码）

- 无 dists 层级：`Packages`/`Sources`（及压缩形态）直接生成在**仓根**；全部 .deb 进入同一索引；无 Release 聚合多 dist（Release 按单仓根生成）。适合单发行版简单仓。sources.list 用 flat 形态 `deb <root>/ ./`（官方 Flat Repository Format 章）。
- BinFlow 建议：两种布局都实现，默认 automatic（对齐 Artifactory 默认）。

### 2.3 制品坐标属性体系（此条补充公开规范；高——代码）

| 属性 | 写在 | 语义 |
|---|---|---|
| `deb.distribution` / `deb.component` / `deb.architecture` | 每个 .deb（可多值——一个 .deb 可登记进多个 dist/comp/arch 索引） | 二进制坐标 |
| `dsc.distribution` / `dsc.component` / `dsc.architecture` | 每个 .dsc；`dsc.architecture` 服务端强制 `source` | 源包坐标 |
| `sha256` | .deb 与索引文件（服务端回填） | By-Hash 与校验链输入 |

属性来源：① PUT 矩阵参数（§3）；② REST 属性接口；③ 属性变更/删除同样触发所在坐标的索引重算（增量事件）。

## 3. 上传链（debPUT：PUT 直推 .deb + 自动索引更新）

1. **PUT .deb**（任意仓内路径，惯例 `pool/<comp>/<前缀>/<源包名>/`）携带矩阵参数坐标：
   ```
   PUT $BASE/binflow/deb-local/pool/main/m/mypkg/mypkg_1.0_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64
   ```
   多值用重复矩阵键（多 dist/comp/arch 登记）。校验：checksum 头沿用统一链（malformed 400 / 不符 409）；automatic 布局下**缺坐标属性的服务端不触发索引计算**（静默跳过，仅存储——文件不会出现在任何 Packages；Artifactory 行为，BinFlow 建议改为 400 拒绝缺失坐标的 .deb，登记裁决）。高（代码）/ —（收紧建议）。
2. 服务端解析 .deb 的 `DEBIAN/control`（dpkg 元数据：Package/Version/Architecture/Depends/Description 等），计算三哈希，回填 `sha256` 属性。
3. 触发**增量索引重算**（异步工作队列）：对受影响的每个 (dist,comp,arch) 上下文重建 Packages（事件 ADD/DELETE/OVERRIDE），再生成压缩形态、By-Hash 拷贝（§5）、Release 重算与签名（§4）。
4. .dsc 上传同链：解析 dsc 源描述 → Sources 索引（stanza `Package`/`Directory`=<dsc 所在目录>/等官方字段）；缺 dsc 坐标属性 → 跳过索引（同上）。
5. 删除 .deb/.dsc → 增量事件 REMOVE → 索引/Release 重算。移动/复制 → 源坐标 REMOVE + 目标坐标 ADD。
6. PUT 响应 = 通用内容面 201（索引更新异步，不阻塞响应）。高。

dpkg-scanpackages/apt-ftparchive 上传面：Artifactory **没有**这两个工具的 REST 面——官方工具产出的索引文件可 PUT 覆盖 `dists/...`（generic 语义），但会与服务端自动重算竞争。BinFlow 建议：索引文件路径（`dists/**/Packages*`、`Release*`、`Sources*`、`by-hash/**`）**禁止客户端直写**（403 或忽略+重算），只走 debPUT 链。此条补充公开规范。中。

## 4. Release 元索引与 GPG 签名链

### 4.1 Release 文件内容（字段集官方 + 生成序代码；高）

```
Origin: <repo 配置或 deb.release.origin 属性，缺省仓 key 或空>
Label:  <同上 label>
Suite: <dist>
Codename: <dist>            ← Suite 与 Codename 均取 distribution 名
Date: <RFC「EEE, dd MMM yyyy HH:mm:ss UTC」当前时间>
Acquire-By-Hash: yes        ← 仅当 byHash 策略 ≠ NONE 且 automatic 布局
Components: <空格连接的全部 component>
Architectures: <排序后空格连接；单个时键名为 Architecture；过滤 any/all 伪架构>
MD5Sum:
 <md5>  <size 右对齐 17> <相对 Release 的路径>
SHA1:
 ...
SHA256:
 ...
```

- 校验节条目路径相对 `dists/<dist>/`（如 `main/binary-amd64/Packages.gz`）；**未压缩文件即使不在磁盘也须列条目**（官方要求校验数据覆盖未压缩形态——Artifactory 恒生成未压缩文件，天然满足）。高（官方 + 代码）。
- 校验节按 By-Hash 策略裁剪：`NONE`/`ALL` → MD5Sum+SHA1+SHA256 全列；`SHA256` → 仅 SHA256。此条补充官方规范（官方未定义三档裁剪）。高（代码）。

### 4.2 签名链（高——代码 + 官方双证）

- 密钥：仓库级 GPG 私钥（Artifactory 经 `security/keypair` REST 配置；BinFlow 走自有密钥配置项，M11 依赖「实例密钥对」能力——若无则本里程碑 unsigned 模式）。
- `Release.gpg`：Release 内容的**detached ASCII-armored 签名**；`InRelease`：**cleartext 签名**（armored，正文内嵌 Release 内容 + 签名块），行分隔符归一化。
- 口令经管理请求头 `X-GPG-PASSPHRASE` 传入（reindex 时）；自动重算用仓配置的口令存储。
- **无密钥/无口令时**：跳过签名；若仓内存在旧签名文件则**删除**（避免 apt 拿旧签名验新 Release）。
- 客户端验签：apt 用 `[signed-by=/usr/share/keyrings/bx.gpg]`；无签名仓用 `[trusted=yes]`（qa 用）。

### 4.3 管理端点（Artifactory `/api/deb/...` 形态；高——代码）

| 方法 | 路径 | 语义 |
|---|---|---|
| POST | `reindex/{repoKey}?async=0\|1` | 全仓索引重算（local/virtual；async=1 调度返回提示文案，0 同步完成）；请求头 `X-GPG-PASSPHRASE`；非 Debian 仓 400 `Repository '<key>' doesn't handle debian requests.`；blacked-out 400 |
| POST | `{repoKey}/snapshot`（body `{"targetRepo":..,"tag":..}`）+ GET `{repoKey}/snapshots/{tag}/{path}` | 用户管理快照：把 dists 树复制到目标仓 `snapshots/<tag>/`；GET 按 tag 服务（dists 前缀走快照树，其余回源仓寻址） |
| POST | `indexCached/{repoKey}` | 仅 `<remote>-cache` 仓：为已缓存 .deb 补算坐标属性（键名需 `-cache` 结尾否则 400） |
| POST | `evictItemInCache/{repoKey}?filePath=` | 强制刷新单文件元数据缓存 |

## 5. By-Hash 语义（Acquire-By-Hash）

- 策略三档（仓配置，此条补充官方规范；高——代码枚举 + inv-3 双证）：`ALL`（MD5Sum/SHA1/SHA256 三算法 by-hash 目录 + Release 三节）、`SHA256`（仅 SHA256）、`NONE`（关闭，无 by-hash 目录，Release 仍三节）。
- 开启时：每个索引文件（Packages 及其压缩形态、Sources 同）复制到**同目录** `by-hash/<算法名>/<十六进制摘要>`；`Acquire-By-Hash: yes` 告知 apt 优先按哈希地址取索引。官方语义：当前版必须可取，历史版应保留 ≥2 代——实现为**按代数保留**（`historyCycles` 配置，超出裁最旧）。高（官方）/ 中（代数实现）。
- 好处（官方动机）：索引原子更新期 apt 不会撞 Hash Sum Mismatch；BinFlow 直接受益于写时先 by-hash 后改写正名文件的次序（建议：先写 by-hash 新摘要、再替换正名文件、最后裁旧历史）。
- apt 侧回退：by-hash 失败可回退正名地址（官方允许）。

## 6. 端点/操作总表（内容面全部为普通文件 GET；高）

| 方法 | 路径（相对仓根） | 语义 | 成功 | 错误 | 置信度 |
|---|---|---|---|---|---|
| GET | `dists/<dist>/InRelease`、`dists/<dist>/Release`、`dists/<dist>/Release.gpg` | 元索引三件套 | 200 文件 | 404 | 高 |
| GET | `dists/<dist>/<comp>/binary-<arch>/Packages[.gz/.bz2/...]` | 二进制索引 | 200 | 404 | 高 |
| GET | `dists/<dist>/<comp>/source/Sources[...]` | 源索引 | 200 | 404 | 高 |
| GET | `dists/<dist>/<comp>/binary-<arch>/by-hash/<ALGO>/<digest>` | By-Hash 索引 | 200 | 404 | 高 |
| GET | `<任意制品路径>`（pool/…） | .deb/.dsc/orig.tar 下载（Packages Filename 指向） | 200 流 + checksum 头 | 404 | 高 |
| PUT | `<任意路径>.deb[;矩阵参数坐标]` | debPUT（§3） | 201 | 400/403/409 | 高 |
| DELETE | `<制品路径>` | 删除 + 增量索引更新 | 204 | 404/403 | 高 |
| GET | `snapshots/<tag>/...` | 快照服务（管理面开启时） | 200 | 404 | 中 |

## 7. 校验链（apt 端到端）

1. apt 取 InRelease（或 Release+Release.gpg）→ GPG 验签 → 按 `SHA256:` 节校验 Packages.gz（或走 by-hash 地址）。
2. 解 Packages → 取包条目 `Filename` + `Size` + `MD5sum/SHA1/SHA256` → 下载 .deb → 逐项校验。
3. **服务端义务**：Release 节哈希 = 磁盘索引实测哈希（重算时同步更新）；Packages 节哈希 = .deb 实测（上传链已算）；.deb 的 sha256 属性与响应头 `X-Checksum-Sha256` 一致。
4. `debianDefaultArchitectures` 默认 `i386,amd64`：强制生成的架构集合（即使无对应 .deb 也产出空 Packages——Artifactory 行为；BinFlow 建议默认关，仅按实际坐标生成，登记裁决）。中。
5. ddeb（Ubuntu 调试符号包）：`ddebSupported` 开关存在；BinFlow 不做特判（.deb 同链处理）。中。

## 8. rclass 三态行为

| rclass | 行为 | 置信度 |
|---|---|---|
| local | debPUT + 自动/手动重算 + 签名 + by-hash 全量；dists 树可读 | 高 |
| remote | apt 镜像 pull-through：miss 回源 `<upstream>/<path>`；上游常带嵌套前缀（如 `debian/dists/...`）→ 路径归一化（`debianVirtualPathPrefixNormalization` 语义：请求路径中定位 `/dists/` 截取后缀匹配）；缓存 .deb 异步补算坐标（`indexCached`）；索引文件缓存 TTL 走 remote 元数据语义（可过期类）；PUT/DELETE 拒绝 | 中（interceptor + 归一化代码走读） |
| virtual | 跨成员聚合索引：逐成员取同坐标 Packages/Sources **合并条目**（stanza 级拼接去重），Release 在虚仓根重算（Components/Architectures = 成员并集）；下载 first-found；debPUT → 路由 defaultDeploymentRepoRef（未配置 405，对齐 repo-semantics §8.2） | 中（collector/finalizer 类走读；stanza 去重细节未逐行核对） |

## 9. 真实客户端命令清单（apt 2.x / dpkg；本机 macOS 无 apt，供 qa 在 Debian/Ubuntu 容器执行）

```bash
# ── 服务端准备（curl；$ADMIN = user:pass，$BASE 例 http://localhost:8080）──
# L-d1 debPUT 直推（矩阵参数坐标）
curl -su $ADMIN -T mypkg_1.0_amd64.deb \
  "$BASE/binflow/deb-local/pool/main/m/mypkg/mypkg_1.0_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64"
curl -su $ADMIN "$BASE/binflow/deb-local/dists/stable/main/binary-amd64/Packages"   # 条目出现 + Filename/SHA256 对账
curl -su $ADMIN "$BASE/binflow/deb-local/dists/stable/Release" | head -20           # Acquire-By-Hash/Date/Components
# L-d2 强制全量重算（管理面；BinFlow 路径以 ADR 定案为准，下为建议形态）
curl -su $ADMIN -X POST "$BASE/binflow/api/deb/reindex/deb-local?async=0" -H "X-GPG-PASSPHRASE: <口令>"

# ── 客户端（Debian/Ubuntu 容器）──
# L-d3 无签名仓接入
echo "deb [trusted=yes] http://$HOST:8080/binflow/deb-local stable main" \
  > /etc/apt/sources.list.d/binflow.list
apt-get update                                       # InRelease/Packages 拉取全绿
apt-get install -y mypkg                             # 下载 .deb 并校验
apt-cache policy mypkg

# L-d4 签名仓接入（实例生成 GPG keypair 后）
#   服务端公钥导出 → /usr/share/keyrings/binflow.gpg
echo "deb [signed-by=/usr/share/keyrings/binflow.gpg] http://$HOST:8080/binflow/deb-local stable main" \
  > /etc/apt/sources.list.d/binflow.list
apt-get update

# L-d5 By-Hash 断言（Release 含 Acquire-By-Hash: yes 时）
H=$(curl -su $ADMIN "$BASE/binflow/deb-local/dists/stable/Release" | awk '/^SHA256:/{f=1;next} f{print $1; exit}')
P=$(curl -su $ADMIN "$BASE/binflow/deb-local/dists/stable/Release" | awk '/^SHA256:/{f=1;next} f{print $3; exit}')
curl -su $ADMIN -o /dev/null -w '%{http_code}\n' \
  "$BASE/binflow/deb-local/dists/stable/${P%/*}/by-hash/SHA256/$H"

# L-d6 删除联动
curl -su $ADMIN -X DELETE "$BASE/binflow/deb-local/pool/main/m/mypkg/mypkg_1.0_amd64.deb"
apt-get update && apt-cache policy mypkg             # 索引条目消失

# L-d7 remote pull-through（仓 deb-remote 上游 deb.debian.org）
echo "deb [trusted=yes] http://$HOST:8080/binflow/deb-remote bookworm main" \
  > /etc/apt/sources.list.d/binflow-remote.list
apt-get update && apt-get install -y hello           # 二次 update 断言回源计数=1

# 本地制包（无 dpkg-deb 时）：qa 环境用 dpkg-deb -b 构造最小 .deb
```

认证：apt 不支持 sources.list 行内凭据（新版弃用）；走 `/etc/apt/auth.conf.d/binflow.conf`：
```
machine $HOST:8080
login user
password pass
```
（http 明文仅限 qa；生产 https。）中（apt 侧约定，qa 验证）。

## 10. 显式不做（M11 登记建议）

1. **Contents / Translation / pdiff（.diff/Index）索引**：官方可选，不做（apt 对缺失容错）。
2. **ddeb 特判**：按普通 .deb 处理。
3. **dpkg-scanpackages/apt-ftparchive 服务端工具面**：无对应能力；索引只由 debPUT 链生成。
4. **用户管理快照**（§4.3 snapshot 端点族）：M11 缓议（有 by-hash 历史已覆盖多数回滚诉求），登记候补。
5. **legacy per-component Release 文件**（官方已标 clients must not use）：不做。

## 11. 与公开规范的差异/补充清单

| # | 条目 | 来源 | 置信度 |
|---|---|---|---|
| S1 | debPUT：矩阵参数坐标 + 增量自动索引（官方无上传协议定义） | 反编译 | 高 |
| S2 | 坐标属性体系 deb.*/dsc.*（多值登记）与属性变更触发重算 | 反编译 | 高 |
| S3 | By-Hash 三档策略（ALL/SHA256/NONE）与 Release 校验节联动裁剪 | 反编译 | 高 |
| S4 | 索引压缩默认集（plain+.gz 恒定，bz2 默认可选，xz/lzma 可配） | 反编译 | 高 |
| S5 | Release 生成序（Origin/Label 来源、Suite=Codename=dist、Date 格式、Architecture 单复数） | 反编译 + 官方字段义 | 高 |
| S6 | 签名链（detached + clearsign 双产物、无密钥删旧签名、X-GPG-PASSPHRASE） | 反编译 + 官方验签义 | 高 |
| S7 | trivial 布局（仓根索引 + flat repo） | 反编译 + 官方 Flat 章 | 中 |
| S8 | 管理端点族（reindex/snapshot/indexCached/evict） | 反编译 | 高 |
| S9 | remote 路径归一化（截 `/dists/` 前缀匹配）与缓存坐标补算 | 反编译 | 中 |
| S10 | virtual stanza 级聚合 + Release 重算 | 反编译 | 中 |
| S11 | 强制架构集合（debianDefaultArchitectures i386,amd64 默认） | 反编译 | 中 |
| S12 | 索引文件禁直写建议（BinFlow 裁决项） | 推断/决策 | — |

## 12. M11 拆票就绪度自评

- **可直接拆票**：布局两型（§2）、debPUT 链（§3）、Release/签名（§4）、By-Hash（§5）、端点表（§6）、校验链（§7）、local 行为、客户端命令（§9）。
- **拆票依赖/裁决点**：① GPG 签名依赖「实例密钥对」能力——若 M11 无 keypair 体系，先落 unsigned 模式（`[trusted=yes]` 验收）+ 签名留接口；② 缺坐标 .deb 拒绝（400）vs 静默存储；③ 索引文件禁直写；④ 管理面路径形态（`/binflow/api/deb/...` vs 自有 `/api/v1`）。
- **低置信项**：无「低」级条目；S7/S9/S10/S11 为中，均不阻塞拆票（virtual 聚合可先做「成员序拼接 + Release 重算」最小实现）。

### 待验证清单（动态验证即可升高）

1. virtual 聚合下 apt 真实 update 全链（S10 stanza 去重细节）。
2. remote 归一化对多嵌套前缀上游（如 `ubuntu/dists/...`）的覆盖面（S9）。
3. by-hash 代数保留的默认值与 apt 拿旧摘要重取的行为（S3 世代裁剪）。
4. apt 对 `Architecture:`（单数）与空 Components 的容忍边界。
