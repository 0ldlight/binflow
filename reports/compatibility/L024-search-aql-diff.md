# L024-4 D03 搜索族 + AQL/D11 差分报告

- **模式**：dual（A 参照全程可达）
- **A 参照**：http://172.16.58.130:8082（Artifactory pro 7.161.15，admin；ssh lzw@172.16.58.130 可达容器——badChecksum 真坏臂与审计日志取证用）
- **B 被测**：http://172.16.58.130:8083（BinFlow dev.52c9ba42，L024-3A 交付后）
- **规格基线**：docs/reverse/aql.md §16（L024 增量段）+ docs/reverse/rest-api.md §3（L120 setItemProperties）；实现报告 reports/agents/L024-3A.md
- **工具**：`tools/difftest/l0244/search_aql_diff.py`（双发→normalize→diff）+ jf 2.122.0 真腿（build-publish 链）+ ssh 容器内操作（blob 坏化/审计日志）
- **复跑门**：pass2==pass3 判定集一致（夹具修正前）；pass4 为终态（夹具修正：属性改 jf 式 matrix 参数 `;k=v` 落地——`?properties=` query 与 `;props=` matrix 双端同不落属性〔连错都一致，注记〕；补 1.1-SNAPSHOT 语料）
- **wire 证据**：`reports/compatibility/l024d-wire/{a,b}/`（24 判定单元 + jf 链日志 + 审计/访问日志摘录）

## 0. 摘要

| 口径 | 数 |
|---|---|
| 判定单元 | 24（versions 5 / latestVersion 6 / api-versions 4 / badChecksum 5 / AQL 3 / 观察臂 c01）+ jf D11 链 4 腿 + j03 产物对比 |
| 终态（pass4） | SAME 12 / DIVERGENT 12（去族 **9 个 D-item**：BUG 6 / normalize 提案 1 / 既有裁定引用 1 / 新断链登记 1） |
| 关键定谳 | V-y（两深层 404 活体臂）**双绿**；V-aa（尾逗号怪癖）**本质双绿**（差一字节分隔符）；V-ab（v 非通配机制）**定谳但分歧**；V-z（badChecksum 行形）**证伪**；D11 主断链（AQL property 面）**闭合**，余 setItemProperties 新断链 |

## 1. normalize 口径（提案 fixtures/normalize.yaml#search）

| # | 规则 | 说明 |
|---|---|---|
| S1 | 头 drop + json-family 等价 | 沿 buildinfo N1/N2（vnd.org.jfrog.artifactory.search.*+json ≡ application/json） |
| S2 | 绝对 URL host → `<BASE>` | badChecksum 行 uri、storage uri |
| S3 | AQL 行 `created`/`modified` → `<ts>` shape | 上传时钟值；**毫秒截断面单列 L6** |
| S4 | **AQL 无 `.sort()` 时行序集合化 + `properties[]` 元素序集合化**（提案） | aql.md §16.1-3 明载参照自身元素序不稳；jf 不依赖序——归一后 q01/q02 内容集合双绿 |

## 2. 逐 case 终态（pass4）

### 2.1 D03-R10 versions（5 单元：SAME 3 / DIVERGENT 2）

| case | 判定 | 要点 |
|---|---|---|
| v01-hit | DIVERGENT（L1） | 时间戳名文件行（`2.0-20260916.120000-1` 等）双端同形 ✓；**字面 SNAPSHOT 文件行**：A=`"1.1-SNAPSHOT"`(integration:true) vs B=metadata 展开形 `"1.1-20260915.192244-1"` |
| v02-404 | **SAME** | `Unable to find artifact versions` 逐字 |
| v03-wildcard | **SAME** | `v=1.*` 过滤语义同 |
| v04-postfilter-empty | **SAME** | `v=9.*` → 200 `results:[]`（先全集后过滤执行序，§16.2 补充官方规范项双绿） |
| v05-repos-scope | DIVERGENT（L1 同因） | `repos=` CSV 限定语义本体同 |

### 2.2 D03-R11 latestVersion（6 单元：SAME 4 / DIVERGENT 1 + 1 同因）

| case | 判定 | 要点 |
|---|---|---|
| l01-default | **SAME** | v 缺省=最新 release `1.1`（text/plain 裸串，跳过 integration） |
| l02-wildcard | **SAME** | `v=1.*` 首命中行 |
| **l03-v-nonwildcard-positive** | **DIVERGENT（L2，V-ab 定谳）** | A=`1.120260915.192244-1`——**v+metadata 时间戳+`-1` 无连字符拼接**（参照怪癖真值）；B=`1.1-20260915.192244-1`（带连字符）。机制定谳：非通配 v=「该版本线的 integration」以 **maven-metadata 快照时间戳**构造（双端同源同秒部署故时间戳同） |
| l04-v-nonwildcard-missing | **SAME** | `v=9.9` → 404 `Unable to find artifact versions` 逐字 |
| **l05-no-release** | **SAME** | 仅 integration 语料 → 404 `Latest release version not found` 逐字（**V-y 臂①活体收口**） |
| **l06-wildcard-nomatch** | **SAME** | `v=9.*` → 404 `Latest integration version not found` 逐字（**V-y 臂②活体收口**） |

### 2.3 D03-R12 /api/versions/{repoKey}/{path}（4 单元：SAME 2 / DIVERGENT 2）

| case | 判定 | 要点 |
|---|---|---|
| p01-default | **SAME** | `{"version":"1.1","artifacts":[]}`（空数组非省略）逐字节 |
| **p02-listFiles** | DIVERGENT（L3） | **尾逗号怪癖本体双绿**（每行含末行 `"path" : "…",` 尾逗号+非严格 JSON，V-aa 多行定谳：恒现）；唯行分隔符 A=`}, {`（逗号+空格）vs B=`},{` |
| p03-404 | **SAME** | 裸 `Not Found` 信封 |
| p04-anon | DIVERGENT（L9 既有族） | 401 措辞/realm（L020-3 广度定案在案） |

### 2.4 D03-R13 badChecksum（5 单元：SAME 2 / DIVERGENT 3）

| case | 判定 | 要点 |
|---|---|---|
| b01-notype | **SAME** | `No checksum type defined` 逐字 |
| b02-badtype | **SAME** | `Checksum type: sha512 is not defined` 逐字 |
| **b03-clean** | DIVERGENT（L4 语义臂） | 无 client 头直传语料：A 旗标全部行（`clientMd5:""` vs `serverMd5`）；B 回 `{"results":[]}`——**A 语义=client-vs-server DB 对比，missing client 即 bad** |
| **b04/b04b-corrupt** | DIVERGENT（L4 行形+语义臂） | 真坏 blob（ssh dd 坏化双端四文件，od 复核 `X02…` 落地）：A 只旗标 x（client 空）**不旗标 y**（client==server 尽管字节已坏）→ **A 不实扫字节定谳**；行形 A=平铺 `{"uri","serverMd5","clientMd5"}` + 顶级 `"limitReached":false`（uri 面按仓有别：mvn/props 仓行带 uri、bad 仓行不带——观察注记）；B=嵌套 `{"checksums":{"md5":{"expected","actual"}}}`（按被证伪的 V-z 形实现）且实扫双旗标 |
| b05-anon | DIVERGENT（L9） | 401 措辞族；**注**：A 暴力防护在复跑中把该臂翻 403 `This request is blocked…`——复跑干扰注记（首跑干净 401 wire 为证据） |

### 2.5 AQL 族（3 单元）

| case | 判定 | 要点 |
|---|---|---|
| **q01-jf-verbatim** | 集合序归一后 **SAME**（L6/L7 挂账） | §16.1 逐字查询（include 裸 property）：properties 键值对象数组内容集合同（build.name/number/timestamp 三键）；created/modified 时钟值异（S3）+ B 毫秒恒 `.000`（L6） |
| q02-root-exclusion | 集合序归一后主体 SAME（L7/L8 挂账） | `path $ne "."` 排根级 ✓（root.bin 双端均不在结果）；无属性行 file2 整键省略 ✓；**wheel.bin 行 A 含 vcs.\* 三键 B 缺**——L8（setItemProperties 未实现）的 AQL 下游直接证据 |
| q03-property-key-form | DIVERGENT（L5） | **展开式语义定谳**：A 有属性行回 `[{"key":…}×N]` **key-only 对象**（无 value 键）；无属性行**省键**。B 有属性行回完整 `{key,value}` 对、无属性行回 `[]`——两面皆错 |

### 2.6 D11 腿（jf 2.122.0 真腿 + 日志窗观察）

| 腿/观察 | A | B | 判定 |
|---|---|---|---|
| upload（matrix build 属性） | exit=0（201） | exit=0（201） | SAME |
| **AQL 发射观察** | 审计日志不记 search REST（DEPLOY/BUILD_CREATE/PROPERTY_UPDATED×3 在案）；触发条件（upload 201+checksum 头，c01 证 A=201+Location+X-Checksum-Sha256）成立 | **访问日志实证**：`POST /api/search/aql → 200`（两独立腿 19:18:57/19:19:15 各一条，223B 入=§16.1 形态） | **D11 主断链闭合**——L023 的 `Unknown AQL field: property` 已消失 |
| build-publish | exit=0（Build info successfully deployed + buildInfoUiUrl） | **exit=1**：`PUT /api/storage/l024d-aql-local/jf/1/wheel.bin → 404 "…is not implemented in BinFlow"`（"Setting properties..." 步）+ strconv.Atoi "dev" 尾错（version 债既有） | DIVERGENT（**L8 新断链**） |
| j03 属性通道产物 | GET /api/build/l024d-jf-app/1：modules[].artifacts[]（name/sha1/path/originalDeploymentRepo 全齐） | **逐位同** | **SAME** |
| c01 upload PUT wire | 201 + Location + X-Checksum-Sha256 | 同 | SAME |

**「jf 未触发 AQL」现象双端定谳**：L024-3A 登记的「三组 publish 零 AQL」在本实例**不成立**——B 访问日志两腿均实证发射（条件=upload 201+checksum 头，满足即发）；A 侧同客户端同触发条件（审计不记 search REST 属日志面非行为面）。L024-3A 现象归因候选=其隔离实例当时 upload 响应形态差异——非本票可证，登记不猜。

## 3. D-item 详单

- **L1【BUG】versions 行·字面 SNAPSHOT 文件**（v01/v05）：A 回字面 `1.1-SNAPSHOT`；B 回 metadata 展开形。时间戳名文件双端同展开形——分歧仅字面语料。B 修法：versions 行取存储路径版本段（不读 metadata 展开）。
- **L2【BUG】latestVersion v 非通配线构造**（l03）：A=`v + <metadata 快照时间戳> + "-1"` **无连字符**（`1.120260915.192244-1`）；B 带连字符。B 修法：按 A 无分隔拼接（参照怪癖即真值）。
- **L3【BUG-minor】/api/versions listFiles 行分隔符**（p02）：A=`}, {`；B=`},{`。尾逗号怪癖本体已双绿。
- **L4【BUG】badChecksum 语义+行形**（b03/b04/b04b）：双面——①A=DB client-vs-server（missing=bad、真坏字节不触发〔y 臂四 blob od 复核实坏〕）；B=实扫（missing 不触发、真坏触发）。②行形：A 平铺 `{uri,serverMd5,clientMd5}`+顶级 `limitReached:false`；B 嵌套 `checksums{expected,actual}`。**V-z 证伪关闭**（spec 勘误：行形=A 平铺形；语义=DB 对比；B 的实扫是超集检测但非参照行为——若保实扫需叠加 missing=bad 面，裁定归 compat）。
- **L5【BUG】AQL property.key 展开式**（q03）：A key-only `{key}` 投影 + 无属性省键；B 完整对 + 空数组。**§2.3 语义定谳回填**：`include("property.key")`→元素 `{"key":k}`；`include("property.value")`→`{"value":v}`（推论待活体）；无属性行省键。
- **L6【BUG-minor】AQL created/modified 毫秒截断**：B 恒 `.000`（A `.408/.397`）。
- **L7【normalize 提案】AQL 序两面**：无 `.sort()` 行序 + properties 元素序集合化（§16.1-3 参照自不稳 + jf 不依赖）。
- **L8【BUG·新断链】`PUT /api/storage/{repoKey}/{path}`（setItemProperties）未实现**：jf build-publish "Setting properties..." 步 404（rest-api.md §3 L120 规格在案：204/`Properties value cannot be empty.`/非法字符 400）；A 审计 `PROPERTY_UPDATED`×3 实证。**修复后 jf publish 主链可通**（version 债另计）。jf 实发形态=PUT /api/storage/<path>（props 经 URL，bytes_in=0）。
- **L9【既有裁定引用】401 措辞族**（p04/b05）：L020-3 广度定案（A `Bad Credentials`/B `invalid credentials` + realm 名）——非本域新项；b05 复跑遭 A 暴力防护 403 干扰注记。

## 4. spec 勘误/回填输入（docs/reverse/aql.md §16 / rest-api.md）

1. §16.5-R13 V-z 关闭：命中行形=平铺 `{uri[,serverMd5,clientMd5|serverSha1,clientSha1]}`+顶级 `limitReached:false`；语义=DB client-vs-server 对比（missing client=bad；**不实扫字节**——真坏 blob 不触发，活体四点实证）；uri 键按仓出现差异（mvn/props 仓带、bad 仓不带）留待反编译复核。
2. §16.3 V-ab 关闭：v 非通配=`v+metadata 快照时间戳+\"-1\"` **无连字符拼接**（`1.120260915.192244-1`）；线内无 SNAPSHOT 时 getArtifactVersions 空 → 404 同文案。
3. §16.4 V-aa 关闭：listFiles=1 尾逗号**每行恒现**（含末行）；行分隔符 `}, {`。
4. §16.2 补：字面 `-SNAPSHOT` 文件名直传 → versions 行=**字面串**（integration:true）；展开形仅当文件以时间戳名存储（或 maven deploy 链）。B 现实现按 metadata 展开回显——勘误回填后需对齐。
5. §2.3 补：`include("property.key")` → key-only 对象；无属性行整键省略（与 §16.1-4 同律）。
6. §16.6 V-x 维持待验证；V-y 双臂活体收口（文案逐字双绿）。
7. **D11 断链档案更新**：主断链（AQL property 面）已闭合（本轮 B 侧 POST /api/search/aql 200×2 实证）；余链=`PUT /api/storage` setItemProperties（rest-api.md §3 L120 已规格、B 未实现）+ /api/system/version 版本串债（jf strconv 尾错）。

## 5. matrix D03 翻态建议（落账归 conductor/compatibility-engineer）

| 行 | 建议 | 依据 |
|---|---|---|
| D03-R10 versions | **不翻**（L1） | 4/5 单元绿；字面 SNAPSHOT 行分歧 |
| D03-R11 latestVersion | **不翻**（L2） | 5/6 单元绿（含 V-y 双臂）；v 非通配构造分歧 |
| D03-R12 /api/versions | **候裁态**（仅 L3 一字节 + L9 既有族） | 尾逗号怪癖/默认形/404 全绿 |
| D03-R13 badChecksum | **不翻**（L4 语义+行形） | 双 400 绿；核心扫描面分歧 |
| D03-R14 archive | 维持 not_applicable（既裁） | — |
| D03-R18 docker manifests | 维持 absent（既裁口径） | — |
| **D11（跨域登记）** | 主断链闭合登记；**新开 setItemProperties 缺口**（归 storage REST 域行——rest-api.md §3 L120） | §2.6 |

提金候选（A 侧绿面 wire）：l05/l06（V-y 双 404 逐字）、p01/p02（versions-by-props 形+尾逗号怪癖字节）、v04（后置过滤空集）、b01/b02（双 400）、q01（§16.1 逐字 AQL 含 properties）、jf 链审计/访问日志摘录。

## 6. 环境处置

双端 l024d-* 四仓删净（deleteContent）；l024d-jf-app build 删净（A /build 404 复核）；**四枚坏化 blob 物理清除**（A filestore 2 枚 + B blobs 2 枚，rm 后 shard 目录复核仅剩无关 blob）；jf config l024da 移除；/tmp 清理。ssh 操作仅触本票命名空间。
