# L038 — T-577 / BIN-59 差分收口批：六条 license-gated resolved 活体补拍 + 本轮新面 live 差分

- 日期：2026-09-29；模式 **dual**（A 活体 + B 活体——dev-bypass 解锁后首次五族双端 live）
- A 面：JFrog Artifactory **7.161.26** Enterprise+ @ http://192.168.120.38:8082（当日 `/api/system/version` 复验 `"version":"7.161.26"`, revision `86126900`——与 L037 同日锚一致）；凭据经 `/tmp/r3-difftest.env` source 注入，零落盘零打印不进本报告
- B 面：本 worktree 当前树（含 R9 全部在途修复，PR 前最终 live 证据；`go build -tags dev` → `/tmp/t577/binflow-server-dev`，scratch `BINFLOW_HOME=/tmp/t577/home-r{1,2}` + `BINFLOW_DEV_TIER=pro`，listen 127.0.0.1:18080（r1）/ 18081（r2））。dev-tier 读数复验：`GET /binflow/api/system/license` = `{"licensed":false,"tier":"pro",...}`（诚实：无 license 文档、仅读数覆盖——T-573/BIN-55 机制）。dev 构建产物仅 /tmp 本地 scratch，零 CI/零外发
- 命名空间：双端统一 `difftest-t577-*`；A 实例配置零触碰；B 每轮新实例+重建仓
- **双轮**：r1（B=18080）与 r2（B=18081 新实例）独立跑全腿集；结构级比对 r1≡r2（status/Location/CT/CL/mimeType 零漂移；body 差异仅版本号/时间戳/sha 衍生值，且双端同值同漂——168 腿×双探针结构漂移计数=0）
- 探针资产（/tmp，可复跑）：`/tmp/t577/t577-probe.py`（主探针，批次 A+B1/B2/B4，110 腿/轮）、`/tmp/t577/t577-checksum-probe.py`（B3，/tmp/t574-probe.py 的 difftest-t577 命名空间适配版，58 腿/轮）、`/tmp/t577/nuget409.json`（定向 409 腿三面）；raw 落 `/tmp/t577/probe-r{1,2}.json` + `/tmp/t577/checksum-r{1,2}.json`

---

## 批次 A — 六条 license-gated resolved 活体补拍（review_gate「license 门解锁后补拍双轮」条款兑现）

判定口径：**PASS = 与该条 resolved 证据声称的 A 形一致**（Location = scheme://host/\<context\>/\<repo\>/\<path\> 绝对形过 context root，各端渲染各自 host+context；matrix 坐标不入 Location）。逐腿双端 raw 见 /tmp/t577 JSON（leg 名对账）。

| # | ledger 条目 | 腿 | A（7.161.26 live，双轮） | B（dev 树 live，双轮） | 判定 |
|---|---|---|---|---|---|
| A1 | `deb/deploy-201-location-bare-relative` | `a1-deb-put`（`PUT /<repo>/pool/main/r/t577fp/t577fp_<V>_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64`，双端同请求——B debPUT DB-2 需坐标、A 作中性 deploy 属性） | **201**；`Location: http://192.168.120.38:8082/artifactory/difftest-t577-deb/pool/main/r/t577fp/t577fp_1.0.0_amd64.deb`；X-Checksum-Sha256=体 sha（46a72f20…/4ece7f2e… 两轮各自） | **201**；`Location: http://127.0.0.1:18080/binflow/difftest-t577-deb/pool/main/r/t577fp/t577fp_1.0.0_amd64.deb`（同构绝对形，matrix 坐标不入）；X-Checksum-Sha256 **=A 同值** | **PASS**（r1≡r2） |
| A2 | `rpm/deploy-201-location-bare-relative` | `a2-rpm-put`（`PUT /<repo>/rpms/t577fp-<V>-1.noarch.rpm` 伪 rpm 体——双端均不拒） | **201**；Location `…/artifactory/<repo>/rpms/t577fp-1.0.0-1.noarch.rpm` + XS256=体 sha | **201**；Location `…/binflow/<repo>/rpms/…` 同构绝对形；XS256=A 同值 | **PASS**（r1≡r2） |
| A3 | `helm/deploy-201-location-bare-relative` | `a3-helm-put`（`PUT /<repo>/charts/t577chart-<V>.tgz` 伪 tgz 体——B parseErr 仍存盘 201） | **201**；Location `…/artifactory/<repo>/charts/t577chart-1.0.0.tgz` + XS256 | **201**；Location `…/binflow/<repo>/charts/…` 同构绝对形；XS256=A 同值 | **PASS**（r1≡r2） |
| A4 | `nuget/bare-put-201-location-bare-relative` | `a4-nuget-bare-put`（bare 存储面 `PUT /<repo>/t577.pkg/<V>/t577.pkg.<V>.nupkg` 真 zip nupkg） | **201**；Location `…/artifactory/<repo>/t577.pkg/1.0.0/t577.pkg.1.0.0.nupkg` + X-Checksum-Sha256 | **201**；Location `…/binflow/<repo>/t577.pkg/…` 同构绝对形；XS256=A 同值 | **PASS**（r1≡r2） |
| A5 | `cargo/serve-derived-write-201-location-bare-relative` | `a5-cargo-put`（bare 面 `PUT /<repo>/t577raw/t577-demo-<V>.txt`） | 建仓 **400** `Custom Base URL should be defined prior to creating a Cargo repo`（实例门复验在案，raw 见 create-cargo/A）；PUT 落 repo-absent 面 **405** 空 message——**A 腿 BLOCKED（实例 Custom Base URL 门，不改实例配置红线）** | 建仓 **200**（dev-bypass）；PUT **201**；Location `http://127.0.0.1:18080/binflow/difftest-t577-cargo/t577raw/t577-demo-1.0.0.txt`（绝对形三件套） | **B 腿 PASS**（修后 wire 形态收敛，双轮）；A 腿维持断供标注——该条 review_gate 认可的「跨族通则+修后 wire 形态收敛」证据链成立，**推断级标注维持**（A 实证腿仍待实例配 Custom Base URL） |
| A6 | `nuget/v3-flatcontainer-push-location-anchor-collision` | `a6-nuget-v3-push-addressed` + `b1-push-v3-addressed`（multipart `package` 字段 → `PUT /api/nuget/v3/<repo>/flatcontainer/<id>/<V>`） | **201**，CT text/plain，body `Successfully published NuPkg to: flatcontainer/<id>/<V>/<file>`；**无 Location、无 X-Checksum-Sha256** | **201**；**无 Location、无 X-Checksum-Sha256** | **PASS**（r1≡r2；删 Location 修复对齐 A 形） |

批次 A 判定：**5/6 PASS；1 条（cargo）B 腿 PASS + A 腿 BLOCKED（实例门，非 B 侧问题）**。六条的 review_gate「license 门解锁后补拍双轮」条款全部兑现（cargo 按 gate 认可的替代证据链）。

### 批次 A 附带观察（新面如实入候选，见「新差异候选」节）

- deb/rpm/helm/cargo 存储 PUT 201 的 **CT/body 面**：A=ItemCreated+json envelope（含 checksums/originalChecksums）；B=无 CT 无 body CL=0 纯头 201。白名单未含此面（白名单只有 nuget bare envelope）→ 候选⑤。
- v3 push 201 **CT/body 面**：A=text/plain+`Successfully published…`；B=空 201 → 候选①。

---

## 批次 B — 本轮新面 live 差分

### B1 — nuget 头面（T-575/BIN-57 验收腿；ledger `nuget/bare-put-201-x-checksum-sha256` + `nuget/push-201-extra-x-checksum-sha256`）

| 腿 | A（双轮） | B（双轮） | 判定 |
|---|---|---|---|
| bare PUT 带**正确** `X-Checksum-Sha256` | 201；头=client 原值（9a1ca9c0…/0043ef5f…） | 201；头=client 原值 **A 同值** | PASS |
| bare PUT **无**头 | 201；头=服务端计算体 sha（d6dcfd26…/bd98a810…） | 201；头=同值（与带腿同规：恒渲染） | PASS |
| push **v3 ADDRESSED** | 201 无 XS256 无 Location | 201 无 XS256 无 Location | PASS |
| push **v3 DIRECT**（publish base `/api/nuget/v3/<repo>/flatcontainer`；A 走 v2 catch-all 前缀 `flatcontainer`） | 201 无 XS256 无 Location | 201 无 XS256 无 Location | PASS |
| push **v2 ROOT**（A `/api/nuget/<repo>/`；B `/api/nuget/v2/<repo>/`） | 201 无 XS256 无 Location | 201 无 XS256 无 Location | PASS |
| push **v2 PATH** | 201 无 XS256 无 Location | 201 无 XS256 无 Location | PASS |

**头面 6/6 PASS 双轮**（bare 恒渲染取值语义=「有效 sha256：client 过验原值/缺席服务端补算，两径同值」与 L037 Arm 2 A 形一致；四入口无头与 T-575 四入口负测一致）。CT/body envelope 面**不判 FAIL**（预期 DIFF）：bare 面白名单确认 `nuget/bare-put-201-content-type-body-envelope` live（A ItemCreated+json envelope vs B 纯头 201，双轮同形）。

**白名单外新发现（候选①②）**：
1. **v3 两入口 201 CT/body**：A=`text/plain` + `Successfully published NuPkg to: <path>`；B=空 body（CL=0 无 CT）。B 的 v2 面**有**该 body（`Successfully published…`，v2.go）——仅 v3 servePush 缺。nuget.md #17 文档 A 形在案。
2. **v2 ROOT 落地路径 layout**：A body `Successfully published NuPkg to: t577.root.1.0.0.nupkg`（**平铺**仓根）；B `…: t577.root/1.0.0/t577.root.1.0.0.nupkg`（`<id>/<ver>/` 嵌套）——部署路径分歧。附带 v2 201 CT 微差：A `text/plain` vs B `text/plain; charset=utf-8`。
3. nuget 409 补拍腿（定向，A+r1B+r2B）：bare 错值 → A **409** `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact '<repo>:<path>'. Checksums info: ChecksumsInfo{…}`（与 L037 逐字同族）vs B **409** `Checksum error for '<repo>/<path>': commit upload <session-id>: storage: commit session <session-id>: checksum mismatch: …`（**session id 外泄**在案）——白名单 `nuget/checksum-409-error-wording-family` live 确认（r1B≡r2B modulo session id）。两侧 409 均无 XS256 头。

### B2 — goproxy CT 面（T-576/BIN-58 验收腿；ledger `goproxy/info-mod-content-type`）

模块 `t577.local/m`（r1=v1.0.0 / r2=v1.0.1），四后缀 × 三面（A 三面=B 有线面）：

| 后缀 | A storage GET | B content GET（B 的 GOPROXY 角与存储角色同面） | A api `/api/go` GET | B `/api/go` GET | A FileInfo mimeType | B FileInfo mimeType |
|---|---|---|---|---|---|---|
| `.info` | 200 `application/json+info` | 200 **`application/json+info`**（逐字） | 200 `application/json+info` | **404**（E-26，无该挂载） | `application/json+info` | **`application/json+info`** |
| `.mod` | 200 `text/plain+mod`（无 charset） | 200 **`text/plain+mod`**（逐字） | 200 `text/plain+mod` | 404 | `text/plain+mod` | **`text/plain+mod`** |
| `.zip` | 200 `application/zip` | 200 **`application/zip`** | 200 `application/zip` | 404 | `application/zip` | **`application/zip`** |
| `.mod.gz` | 200 **`application/x-gzip`** | **404** `not found`（PUT 即 404，从未落盘） | 404（json envelope） | 404 | `application/x-gzip` | **404**（无节点） |

**判定**：`.info/.mod/.zip` 三后缀 CT 拼写在 B 全部有线面与 A **逐字一致（双轮）**——BIN-58 条目主张的拼写面 **PASS**。`.mod.gz` api 面 404 A=B 一致。两个白名单外观察入候选（③④）：
3. **`.mod.gz` 存储面**：A go 仓内容面有 raw storage 臂（PUT 201/GET 200 x-gzip/FileInfo x-gzip）；B @v 路由集 {zip,mod,info} 外**无第二存储入口**（PUT/GET/FileInfo 全 404）。
4. **`/api/go` 别名缺席**：A 官方挂载点 200 三值同；B 无（apiProtocolMounts 闭集 {npm,pypi,nuget,helm}）——`docs/reverse/goproxy.md` §挂载对照**明文声明「BinFlow 不沿用 /api/go 段」**（as-built 设计声明在案；INTENTIONAL 形态候选但终裁归 conductor）。

### B3 — checksum PUT 抽核（T-574/BIN-56 验收腿；脚本=/tmp/t574-probe.py 的 t577 命名空间适配）

**已落地 9 腿期望 PASS——实测 9/9 PASS（status+message 逐字节 A=B，双轮）**：

| 腿组 | 腿 | 双端形态（逐字节） |
|---|---|---|
| generic 源缺 ×5 | g-miss.sha1/.md5/.sha256/noext/hexok | 404 `Target file to set checksum on doesn't exist: difftest-t577-generic:<src>` |
| maven GAV 源缺 ×2 | m-miss-gav(.sha1)/-md5 | 404 同文案（`…:difftest-t577-maven:com/diff/t574/1.0.0/t574-1.0.0.pom`） |
| 非 GAV 路由序 | m-miss-nogav | 404 同文案（`…:difftest-t577-maven:foo/bar.txt`）——修复前 B=400 layout 错 |
| 409 路径去 repo | m-set-wrong | 409 `Checksum error for 'com/diff/t574/2.0.0/t574-2.0.0.jar.md5': received '000…0' but actual is '2bf1…'`（无 repo 前缀，逐字） |

**白名单 5 组预期 DIFF live 确认（双轮同形）**：generic 值对 201 形态（A=CL0+Location 指源 vs B=envelope+Location 自身）；generic 值错 409（g-set-wrong/del-seed-set：A 409 vs B 201）；del3 计数 **A=3 vs B=4**（两轮 3/3 与 4/4）；FileInfo originalChecksums 写穿（g：A oc.md5=000…0 vs B=计算值；m 同形）；maven 409 后 GET 回显（m-get-after-wrong A=000…0 vs B=2bf19ef6…——BIN-60 SPI 缝）。另两白名单：m-neg-nogav-sha512 **A=201 vs B=400**（`maven/sha512-put-non-layout-deploy`）；g-get-unset 404/404 文案族（A `File not found.; Path: '<repo>:<src>'` 指源 vs B `Failed to find the requested resource` 指 .sha1 路径——`generic/checksum-get-unset-404-wording`）。

### B4 — 五族 FileInfo mimeType 收敛腿（T-571/BIN-53 补拍；httpapi/mime.go 渲染表 license-gated 域）

| 仓 | 后缀样本 | A mimeType | B mimeType | 判定 |
|---|---|---|---|---|
| deb | `.deb`（A1 制品） | application/x-debian-package | 同 | PASS |
| deb | `.txt`（dists 裸存臂） | text/plain | 同 | PASS |
| deb | `.tar.gz`（末段 .gz 规则） | application/x-gzip | 同 | PASS |
| rpm | `.rpm`（A2 制品） | application/x-rpm | 同 | PASS |
| rpm | `.txt` | text/plain | 同 | PASS |
| nuget | `.nupkg`（A4 制品） | application/x-nupkg | 同 | PASS |
| nuget | `.nuspec` | application/x-nuspec+xml | 同 | PASS |
| nuget | `.txt` | text/plain | 同 | PASS |
| helm | `.tgz`（A3 制品） | application/x-gzip | 同 | PASS |
| helm | `.yaml` | text/plain | 同 | PASS |
| cargo | `.crate` | **A 断供**（建仓 400 门，见 A5） | application/octet-stream（表 miss 兜底） | B 单边观察（无 A 对照面，如实标注） |
| cargo | `.json`（`index/config.json`） | A 断供 | PUT **404** `{"errors":[{"detail":"not found"}]}`——cargo `index/*` 非裸存储入口 | B 单边观察（同上） |

**可对照 12/12 腿 PASS（A=B 逐值，双轮）**；cargo 两腿=A 断供（实例门）+B 单边观察，不立差异候选（无对照面）。

---

## 白名单确认数

**9/9 项白名单全部 live 确认**（双轮同形，均记「已登记 BUG live 确认」，非 FAIL 非回归）：generic 值对 201 形态 / generic 值错 409 / del3 计数 3-vs-4 / FileInfo originalChecksums 写穿 / maven 409 后 GET 回显（BIN-60 SPI 缝）/ nuget bare 201 CT+body envelope / nuget 409 文案族 / maven 非 GAV .sha512 PUT 201-vs-400 / generic GET 未设值 404 文案。

## 新差异候选（白名单外，如实上报 raw，不自行裁定——分类建议全 UNKNOWN 待裁）

| # | 候选面 | A 形 | B 形 | raw 锚 |
|---|---|---|---|---|
| ① | nuget v3 push 201 CT/body（ADDRESSED+DIRECT 两入口） | text/plain + `Successfully published NuPkg to: <path>` | 空 body CL=0（v2 面有 body——仅 v3 servePush 缺） | probe-r{1,2}.json `a6/b1-push-v3-*` |
| ② | nuget v2 ROOT 落地路径 layout（+v2 201 CT charset 微差） | 平铺 `<id>.<ver>.nupkg`；CT `text/plain` | `<id>/<ver>/<id>.<ver>.nupkg` 嵌套；CT `text/plain; charset=utf-8` | probe-r{1,2}.json `b1-push-v2-root` |
| ③ | goproxy `.mod.gz` 存储面 | raw storage 臂可存（PUT 201/GET x-gzip/FileInfo x-gzip） | @v 路由集外无存储入口（三面 404） | probe-r{1,2}.json `b2-*-modgz` |
| ④ | goproxy `/api/go` 别名 | 200 官方挂载点（三值同） | 无挂载（E-26 404）——goproxy.md 明文「不沿用」，INTENTIONAL 形态候选 | probe-r{1,2}.json `b2-api-get-*` |
| ⑤ | 五族存储 PUT 201 CT/body envelope（deb/rpm/helm/cargo/nuget-v3-push） | ItemCreated+json envelope（checksums+originalChecksums） | 无 CT 无 body CL=0（generic 仓 B 有 envelope——五族 adapter 面。”《未登记） | probe-r{1,2}.json `a1/a2/a3/a5/b1-push-v3-*` |

## Z 相关翻面建议证据链（翻面归 conductor，本批只产证据）

- **五族 Location 条目**（`deb/rpm/helm/nuget-bare/cargo …location-bare-relative`）：review_gate「license 门解锁后补拍双轮」兑现——**A1~A4+A5-B腿 双轮 PASS**（cargo 维持推断级+A 腿 BLOCKED 标注，gate 认可的替代证据链成立）→ 建议 resolved 维持并销活体欠账。
- **`nuget/bare-put-201-x-checksum-sha256`**（BIN-57 头面）：bare 两腿（原值回显/服务端补算同值）+push 四入口无头，**双轮 PASS** → resolved 证据链齐。
- **`nuget/push-201-extra-x-checksum-sha256`**：四入口 201 无 XS256 无 Location **双轮 PASS** → resolved 证据链齐。
- **`goproxy/info-mod-content-type`**（BIN-58）：三拼写 B 全线面逐字 A 形（storage GET+FileInfo；B 单面双角色）**双轮 PASS** → 建议 resolved（附候选③④两观察移交裁定）。
- **maven/checksum-put-404-wording 的 409 半面**（T-574 遗留缝前状态）：409 文案去 repo 逐字+路由序 404 **双轮 PASS**（写穿半面维持 BUG——白名单确认组）。
- **回否定性（证据与预期不符=0）**：无任何「矩阵宣称 ✅ 但差分红」腿；R9 在途修复（T-567/T-571/T-574/T-575/T-576）全部按宣称形态 live 复现。

## NOT_RUN / BLOCKED 如实清单

- A5/B4 cargo A 腿：**BLOCKED**（A 实例 Custom Base URL 建 cargo 仓门——不改实例配置红线；400 raw 在案）。
- dotnet 真客户端腿：**NOT_RUN**（本机无 dotnet；票面明令 nuget 腿 curl 级可）。
- 其余全腿：PASS 或白名单确认（见各节）。

## 环境处置

- A：`difftest-t577-*` 全部 DELETE；终验 `/api/repositories` 前缀 grep=**0**（residual [] 在三轮探针 finally 复验 + 收尾独立复验）；实例配置零触碰。
- B：18080/18081 两实例 `difftest-t577-*` 全删（各轮 residual=[]）；SIGTERM graceful stop（日志尾行 `"binflow stopped"` ×2 在案）；端口释放复验（healthz=**000** ×2）；无残留进程（pgrep 空）。
- /tmp/t577：探针脚本+raw JSON+两 home（server.log/数据目录）留存备查；dev 构建产物未出 /tmp；凭据零落盘（本报告与 raw JSON 均无凭据字节）。
