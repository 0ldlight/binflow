# L036 — R7 跟进探针批（T-564 followup ①②）

- 批次性质：**probe-only**（不改产品码、不 commit、不翻台账、不写 Linear；裁定权在 conductor）。
- A 面（参照）：Artifactory **7.161.26** 活体 `http://192.168.120.38:8082/artifactory`（版本号当日 `/api/system/version` 复验）。
- B 面（被测）：worktree HEAD **943ce899** 自建 scratch `127.0.0.1:18083`（`/binflow` 前缀；BINFLOW_HOME=/tmp/binflow-r7-audit，数据目录 /tmp/binflow-r7-audit/data **留存备复查**；凭据 env 注入零落盘）。
- 复跑门：①的 case 连续两轮（`run/l036-r7fp` 与 `run/l036-r7fp-r1`）结论逐腿相同（11 DIFF 同集合）。
- 环境清理：双面 `difftest-r7fp-*` 仓残留 **0**（各核验一次）；B 实例已停（18083 释放，graceful stop 日志在案）；/tmp/binflow-r7-audit/{data,probes,fixtures} 留存。
- 四态口径：PASS/FAIL/BLOCKED/NOT_RUN；skip≠PASS；推断与实证显式区分。

---

## ① generic mimeType 归属 mini 差分（T-564 followup ①）

固化 case：`tools/difftest/v2/cases/generic_mime_ownership.py`（18 腿矩阵，PUT(wire curl 精确 CT 控制)→GET 回显头+体→FileInfo 三面）。原始证据：`tools/difftest/v2/run/l036-r7fp{,-r1}/evidence/generic-mime-ownership/{a,b}-leg.json + summary.json`（run/ 目录 gitignore，留存本机备复查）。

### 结果总览

- case 两轮均 **PASS**（矩阵完整捕获；probe posture：DIFF 腿是裁定素材不是失败）。
- 18 腿：**一致 7 / 差异 11**；无 BLOCKED、无 skip。真实客户端腿（pip/dotnet）本 mini 差分 **NOT_RUN**（焦点是 mime 归属，非协议栈；wire 级 curl 直采）。

### 矩阵（A mimeType / B mimeType；GET Content-Type 与 FileInfo 恒一致，仅列 mimeType）

| 腿 | 声明 CT | 路径/体 | A | B | |
|---|---|---|---|---|---|
| n-abs-text | 无 | 无扩展名/text | octet-stream | octet-stream | SAME |
| n-auto-text | curl 默认 x-www-form-urlencoded | 无扩展名/text | **octet-stream** | **x-www-form-urlencoded** | DIFF（T-564 观察腿复现） |
| n-formurl-text | 显式 x-www-form-urlencoded | 无扩展名/text | **octet-stream** | **x-www-form-urlencoded** | DIFF |
| n-textplain | text/plain | 无扩展名/text | **octet-stream** | **text/plain** | DIFF |
| n-json | application/json | 无扩展名/json 体 | **octet-stream** | **application/json** | DIFF |
| n-xml | application/xml | 无扩展名/xml 体 | **octet-stream** | **application/xml** | DIFF |
| n-octet-bin | application/octet-stream | 无扩展名/bin | octet-stream | octet-stream | SAME |
| n-custom-bin | application/x-r7fp-custom | 无扩展名/bin | **octet-stream** | **application/x-r7fp-custom** | DIFF |
| n-charset | text/plain; charset=utf-8 | 无扩展名/text | **octet-stream** | **text/plain; charset=utf-8**（参数原样保留） | DIFF |
| n-abs-bin | 无 | 无扩展名/bin | octet-stream | octet-stream | SAME |
| e-txt-abs | 无 | .txt/text | **text/plain** | **text/plain; charset=utf-8** | DIFF（charset 后缀） |
| e-txt-json | application/json | .txt/text | **text/plain**（扩展名赢） | **application/json**（声明赢） | DIFF（冲突裁决相反） |
| e-json-abs | 无 | .json | application/json | application/json | SAME |
| e-json-textplain | text/plain | .json | **application/json**（扩展名赢） | **text/plain**（声明赢） | DIFF |
| e-xml-abs | 无 | .xml | application/xml | application/xml | SAME |
| e-jar-abs | 无 | .jar | application/java-archive | application/java-archive | SAME |
| e-bin-abs | 无 | .bin | octet-stream | octet-stream | SAME |
| e-txt-abs-bin | 无 | .txt/bin 体 | **text/plain** | **text/plain; charset=utf-8** | DIFF（无内容嗅探，纯扩展名） |

### 归属规则定性（裁定素材）

- **A（7.161.26）＝扩展名白名单表 + 兜底 octet-stream；请求声明 CT 全忽略；无内容嗅探。** 证据链：所有显式声明腿（n-textplain/json/xml/custom/charset、e-txt-json、e-json-textplain）A 全部返回扩展名推导值；二进制体放 .txt（e-txt-abs-bin）仍 text/plain、文本体无扩展名（n-abs-text）仍 octet-stream——排除 sniff。
- **B（HEAD）＝声明 CT verbatim 存储、存储值权威渲染 GET 头与 FileInfo；无声明→自有表+stdlib 推断→octet-stream**（generic/mime.go：`mimeForNode` 存储值赢）。GET 头与 FileInfo 双面双端恒一致——分歧只在「存储值怎么来」。
- 冲突裁决方向相反：A 扩展名赢、B 声明赢（e-txt-json / e-json-textplain 两腿互为镜像证据）。
- 附带（PUT 渲染面复核）：双端 PUT 201 的 Location/uri/downloadUri 均绝对形过各自 context root（A `/artifactory`、B `/binflow`）——T-563/T-564 修复面稳定，未回归。

### 扩展名表取值差分（generic 仓、无声明 CT、curl -T 不带 CT；15 扩展名）

| ext | A | B | | ext | A | B |
|---|---|---|---|---|---|---|
| .csv | **octet-stream** | **text/csv; charset=utf-8** | | .svg | **octet-stream** | **image/svg+xml** |
| .deb | x-debian-package | x-debian-package ✓ | | .tar | x-tar | x-tar ✓ |
| .gz | **x-gzip** | **gzip** | | .tgz | **x-gzip** | **gzip** |
| .html | **text/html** | **text/html; charset=utf-8** | | .txt | **text/plain** | **text/plain; charset=utf-8** |
| .md | **text/plain** | **text/markdown; charset=utf-8** | | .war | java-archive | java-archive ✓ |
| .pdf | **octet-stream** | **application/pdf** | | .yaml | **text/plain** | **application/yaml** |
| .rpm | **x-rpm** | **octet-stream** | | .zip | application/zip | application/zip ✓ |
| .sha1 | **mimeType 字段缺失** | application/x-checksum | | | | |

12/15 取值分歧（同值仅 .deb/.war/.tar/.zip，加 ① 的 .json/.xml/.jar）。

### 分类建议（conductor 裁）

1. mimeType 归属模型（11 腿）+ 表取值（12/15）：结构性分歧，**对齐 or 登记分歧**二选一——对齐 A 方向 = B generic PUT 忽略请求 CT 改扩展名表（波及 cargo `serveDerivedWrite` 等各 adapter 自有 mime 决策，需全域梳理）；维持 B = known-divergence 登记。**UNKNOWN 待裁**。
2. B 扩展名表 stdlib 回退的宿主依赖：`extensionMimes` 自有表之外的扩展（.pdf/.svg/.png…）走 `mime.TypeByExtension`（OS mime 库）——B 自己的注释（generic/mime.go 头）承认此风险。本机 darwin 给 application/pdf / image/svg+xml，linux 宿主可能不同 → **wire 契约宿主漂移风险**，compatibility-engineer 提案素材（表扩充即消除回退）。
3. A `.sha1` 无 mimeType 字段（特殊 sidecar 处理）——低优先登记录入。

---

## ② 六协议族自引用渲染点审计（T-564 followup ②）

方法：`grep requestBase/Location/uri·downloadUri` over 六族 adapter（generic/maven 已修为参照模式：`requestBase(r)+productPrefix+repo+escapePath`，maven/put.go:438、generic/handler.go:259）→ 逐族渲染点清单 → 疑点族 A 面活体（真实客户端优先，curl 补 wire）→ B 面静态判定 + HEAD 活体（仅许可可用族）。

**邻接域发现（先决）**：B scratch（community 档）对 cargo/debian/nuget/rpm/helm 建仓一律 400 `license tier 'community' < 'pro'`——**五族 B 面活体全部 BLOCKED(license)**，B 侧证据=静态代码 + pypi/generic 活体外推（逐行标注）。体系级 blocker：五族差分需 pro-license scratch 或 UAT 换装路径，上报 conductor。

### 逐族审计表

| 族 | 渲染点（文件:行） | 裸根疑点 | A 面实形（7.161.26 活体） | B 面实形 | 四态 |
|---|---|---|---|---|---|
| **pypi** | 上传响应 POST /api/pypi/<repo>/（wire 面，非 adapter 直渲染） | n/a | 200 + **Location** `http://localhost:8081/difftest-r7fp-pypi/r7fp-pkg/1.0.0/r7fp_pkg-1.0.0-py3-none-any.whl`（⚠宿主 localhost:8081、**无 /artifactory**）+ `X-Checksum-Sha256` + CT `application/vnd.org.jfrog.artifactory.storage.ItemCreated+json` | 200 text/plain CL=0，**无 Location、无 checksum 头** | live 双端 PASS（取证完成）；B 缺 Location/checksum 头→**BUG 候选④** |
| | simple index href（simple.go:184） | n（相对） | 相对 `../../r7fp-pkg/1.0.0/r7fp_pkg-…whl#sha256=…`（指**存储布局**路径） | 相对 `../../packages/r7fp-pkg/1.0.0/…whl#sha256=…`（指 /packages/ 协议面，doc.go 有意为之为 href 目标） | live 双端 PASS；href 目标布局分歧→**UNKNOWN 待裁** |
| | 302 no-slash（simple.go:116） | n（相对，有 ruling） | 绝对 `http://localhost:8081/artifactory/api/pypi/…/r7fp-pkg/`（过 context root、宿主怪） | 相对 `r7fp-pkg/`（maven-npm-pypi.md §3.1 已裁相对形） | live 双端 PASS；A 形态含宿主怪，非对齐目标 |
| | FileInfo uri/downloadUri（共享 storage.go:189） | n | 绝对过 /artifactory | 绝对过 /binflow | **同构 PASS** |
| **cargo** | config.json dl/api（index.go:59-75 configDocument） | **n**（`origin+"/binflow/"+repo`——带前缀且与自身内容面路由一致） | **BLOCKED**：建仓 400 `Custom Base URL should be defined prior to creating a Cargo repository`（实例级配置红线不改；该门本身证明 A 的 cargo 自引用依赖实例配置 base URL） | 静态 PASS；live BLOCKED(license) | A live NOT_RUN(实例门)；B static PASS |
| | serveDerivedWrite 201 `Location: path`（handler.go:344） | **y**（裸仓内相对） | 不可实证（仓不可建）；跨族存储面统一形态（deb/rpm/helm/nuget-bare 实证）＝绝对过 context root → **推断** A 同形 | 静态：`Location: <path>` 裸相对 | **BUG 候选⑤（推断级，A 实证缺）** |
| | publish 200 warnings JSON（publish.go `writeJSON(StatusOK)`） | n | 不可实证 | 静态：200 `{"warnings":{…}}` 无 Location（crates.io 协议形） | 双端 NOT_RUN(门) |
| **deb** | writeCreated `Location: rel`（handler.go:552） | **y** | PUT 201 + **Location `http://192.168.120.38:8082/artifactory/<repo>/pool/main/r/r7fp/r7fp_1.0.0_amd64.deb`**（绝对过 context root）；FileInfo uri/downloadUri 绝对；mimeType=x-debian-package | 静态：`Location: <rel>` 裸相对；live BLOCKED(license) | A live PASS；**BUG 候选①** |
| **rpm** | writeCreated `Location: rel`（handler.go:580） | **y** | PUT 201 + **Location `http://…:8082/artifactory/<repo>/rpms/r7fp-1.0.0-1.noarch.rpm`**（绝对）；mimeType=x-rpm；伪 rpm payload 也 201（A 上传不校验 rpm 结构——side note） | 静态：裸相对；live BLOCKED(license) | A live PASS；**BUG 候选①** |
| **helm** | writeCreated `Location: rel`（handler.go:572） | **y** | chart PUT 201 + **Location `http://…:8082/artifactory/<repo>/charts/r7chart-0.1.0.tgz`**（绝对）；mimeType=x-gzip | 静态：裸相对；live BLOCKED(license) | A live PASS；**BUG 候选①** |
| | index.yaml urls（index.go HL-2） | n（已裁相对） | 两面（`/<repo>/index.yaml` 与 `/api/helm/<repo>/index.yaml`）均 `urls: [local://charts/r7chart-0.1.0.tgz]`；**helm 3 真客户端 pull 直接失败：`Error: scheme "local" not supported`**（A 无 Custom Base URL 时自 broken） | 静态：相对 urls（HL-2 ruled，helm 原生可解析形态） | A live PASS（真客户端取证）；建议**维持 B** |
| **nuget** | v3 service index @id（index.go:46 + apiBase handler.go:361 `origin+"/binflow/api/nuget/…"`） | **n**（带前缀） | @id 全绝对过 /artifactory（query/registration/flatcontainer/LegacyGallery/PackagePublish） | 绝对过 /binflow，同构；PackagePublish 指向 flatcontainer（T-287 deviation register 已登记——非新发现） | **同构 PASS**（A live + B static） |
| | v2 push multipart PUT（v2.go:1148） | n | 201 text/plain **无 Location**；落库仓根 `R7fp.Pkg.1.0.0.nupkg` | 201 text/plain `Successfully published NuPkg to: <deployPath>` **无 Location**；落库 flatcontainer/<id>/<ver>/…（nuget.md §5.1 已裁） | 渲染面**同构 PASS**；布局分歧已裁非新项 |
| | v3 flatcontainer push（flat.go:355 `Location: target.nupkg()`） | **y**（repo 相对，锚点错叠：相对解析对 `/…/flatcontainer/<id>/<ver>` 会得到双叠 flatcontainer 路径） | multipart PUT 201 **无 Location** | 静态：201 + `Location: flatcontainer/<id>/<ver>/<id>.<ver>.nupkg` | **B 多渲染 A 没有的头 + 锚点可疑 → BUG 候选③** |
| | bare .nupkg 内容面 PUT（flat.go:516 `Location: rel`） | **y** | PUT 201 + **Location `http://…:8082/artifactory/<repo>/bare/r7fp.pkg.1.0.0.nupkg`** 绝对 + X-Checksum-Sha256 | 静态：`Location: <rel>` 裸相对 | **BUG 候选②** |
| 邻接族 | helmoci/conan/docker/goproxy/npm/uploads.go：Location 渲染点 grep=0 | n | — | 自引用走共享 storage API（storage.go:189 requestBase+prefix） | NOT_RUN（不在本批域） |

### A 面通则（跨族实证）

- A 的存储面 PUT（内容面，任意 packageType）201 Location **一律绝对形过 context root**（generic/pypi 存储/deb/rpm/helm/nuget-bare 七处实证一致）——T-563 修复的 generic/maven 与此同构；deb/rpm/helm/nuget-bare/cargo 五处 B 侧 `Location: <rel>` 裸相对即同类缺陷。
- A 自身两类「无 Custom Base URL 降级怪」：pypi 上传响应 Location 引 `localhost:8081`（非请求 host、无 context root）、302 引 localhost:8081（有 context root）；helm index urls `local://`（真客户端不可用）。这些是 A 的怪癖取证，**不构成 B 对齐目标**；若对齐 pypi 上传 Location，应取 `requestBase(r)+productPrefix+repo+path`（B 的既有 helper 形态），不照抄 A 怪值。

### BUG 候选清单（供 conductor R8 池立票；均未自行修码/翻台账）

1. **deb/rpm/helm deploy-201 Location 裸相对**（deb handler.go:552 / rpm handler.go:580 / helm handler.go:572）vs A 绝对过 context root——与已修 generic/maven（T-561/T-563/T-564）完全同构的三族批量，修法同款（requestBase+productPrefix）。分类建议 BUG。B 活体因 license 门 BLOCKED，但静态代码 + A 面实证 + generic/maven 已修形态三点闭环。
2. **nuget bare-PUT Location 裸相对**（flat.go:516）——同上第四族。
3. **nuget v3 flatcontainer push 多渲染 Location 且锚点错叠**（flat.go:355）——A 同面 201 无 Location；B 的 repo 相对值经客户端相对解析指向双叠 flatcontainer 路径。建议去掉该头或改绝对前缀形（nuget 域裁定）。
4. **pypi 上传响应缺 Location/X-Checksum-Sha256**（响应头族）——A 有此二头（Location 为 A 宿主怪形，对齐应取 requestBase+prefix 正确形态而非照抄）；分类建议 BUG（wire 补齐类）或 UNKNOWN 待裁（若裁定不跟 A 的怪形）。
5. **cargo serveDerivedWrite Location 裸相对**（handler.go:344）——①同类第五族，A 实证因实例门缺（推断级，仓建不成；跨族统一形态支撑）。
6. （观察项非票）mimeType 归属模型 + 扩展名表取值分歧（①节）——对齐 or 登记分歧，conductor 裁。

### raw 关键行（采样；全量在 /tmp/binflow-r7-audit/probes/ 与 run/l036-r7fp{,-r1}/evidence/）

```
A pypi twine POST → Location: http://localhost:8081/difftest-r7fp-pypi/r7fp-pkg/1.0.0/r7fp_pkg-1.0.0-py3-none-any.whl
A pypi 302        → Location: http://localhost:8081/artifactory/api/pypi/difftest-r7fp-pypi/simple/r7fp-pkg/
B pypi 302        → Location: r7fp-pkg/
A deb PUT         → Location: http://192.168.120.38:8082/artifactory/difftest-r7fp-deb/pool/main/r/r7fp/r7fp_1.0.0_amd64.deb
A rpm PUT         → Location: http://192.168.120.38:8082/artifactory/difftest-r7fp-rpm/rpms/r7fp-1.0.0-1.noarch.rpm
A helm PUT        → Location: http://192.168.120.38:8082/artifactory/difftest-r7fp-helm/charts/r7chart-0.1.0.tgz
A helm index      → urls:\n    - local://charts/r7chart-0.1.0.tgz   （两 alias 面同）
A helm 真客户端   → helm repo add ok; helm pull → Error: scheme "local" not supported
A nuget v3 index  → "@id": "http://192.168.120.38:8082/artifactory/api/nuget/v3/difftest-r7fp-nuget/query"（@type SearchQueryService）
A nuget v2 push   → multipart PUT 201 text/plain 无 Location（raw CT 体一律 415）
A nuget v3 push   → multipart PUT 201 无 Location
A cargo 建仓门    → 400 "Custom Base URL should be defined prior to creating a Cargo repository"
B 建仓门(五族)    → 400 "license tier 'community' < 'pro'"
B FileInfo(共享)  → uri/downloadUri 绝对过 /binflow（与 A 同构）
```

### NOT_RUN / BLOCKED 如实清单

- cargo A 面：BLOCKED（A 实例级 Custom Base URL 未配——改实例配置越红线；该门即 A 面事实）。
- cargo/debian(deb)/nuget/rpm/helm B 面活体：BLOCKED（scratch community 档 license 门；静态代码为 B 侧证据）。
- 邻接族（helmoci/conan/docker/goproxy/npm）渲染点活体：NOT_RUN（不在本批六族域；静态 grep=0 已覆盖疑点筛查）。
- ①真实客户端腿（pip/twine/dotnet）：NOT_RUN（本机缺 twine/dotnet/pip 二进制；wire 级 curl 已覆盖焦点面）。
- helm B 面真客户端腿：NOT_RUN（license 门，仓不可建）。
