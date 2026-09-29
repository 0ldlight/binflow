# L037 — probe-first 五臂差分探针批（T-572 / BIN-54）

- 日期：2026-09-29；模式 **dual**（A 活体全臂 + B 活体臂1/3/5、静态码臂2/4）
- A 面：JFrog Artifactory **7.161.26** Enterprise+ @ http://192.168.120.38:8082（当日 `/api/system/version` 复验 `"version":"7.161.26"`，revision 86126900）；凭据经 `/tmp/r3-difftest.env` source 注入 + 0600 netrc（用毕销毁），零落盘
- B 面：**origin/main 88588705** 干净树（`git archive 88588705 | tar -x -C /tmp/r9-l037-src` 全新构建，避开共享 worktree 并行在途改动）；scratch 实例 `BINFLOW_HOME=/tmp/r9-l037-s{1,2}`，listen 127.0.0.1:**18082**（r1）/ **18083**（r2），数据目录 /tmp 下
- 命名空间：双端仅 `difftest-l037-*`（仓与路径均带前缀）；A 实例配置零触碰
- **双轮**：r1（B=18082）与 r2（B=18083 新实例+重建仓）结论全等；下文表格数值两轮一致处不再重复标注
- 四态：臂1/5 PASS（双端活体钉住）、臂2 A 腿 PASS + B 腿 BLOCKED（license 门）、臂3 PASS（真实 twine/pip）、臂4 A 腿 PASS + B 腿 BLOCKED（license 门）+ 真实 go 客户端腿 BLOCKED（结构性）

---

## Arm 1 — 裸 checksum PUT 归属（T-570 residual#2）：PASS（双端活体，口径差全谱钉住）

构造：generic 仓 `difftest-l037-generic`（maven 域另用 GAV 合法路径腿），`curl -X PUT -T <file>`。

### A 面：终缀 .sha1/.md5/.sha256 PUT = 「client-checksum 写入操作」路由拦截（非文件部署）

| 腿 | A（7.161.26 live） | 说明 |
|---|---|---|
| `PUT arm1/lone.txt.sha1`（源不存在，内容任意） | **404** `{"errors":[{"status":404,"message":"Target file to set checksum on doesn't exist: difftest-l037-generic:arm1/lone.txt"}]}` | 拦截族①：.sha1 |
| `PUT arm1/lone.txt.md5` | **404** 同文案（message 同指源 `arm1/lone.txt`） | 拦截族②：.md5 |
| `PUT arm1/lone.txt.sha256` | **404** 同文案 | 拦截族③：.sha256 |
| `PUT arm1/valid.bin.sha1`（内容=合法 40 位 hex） | **404** 同文案 | 内容无关，纯路径终缀路由 |
| `PUT arm1/noext.sha1`（源名无扩展名） | **404** 同文案（指 `arm1/noext`） | 源扩展名无关 |
| `PUT arm1/lone.txt.sha512` | **201** 正常 ItemCreated envelope，mimeType `application/octet-stream`，GET 200 | **.sha512 不在拦截族** |
| `PUT arm1/lone.txt.asc` | **201** envelope，mimeType `text/plain` | .asc 不在拦截族（普通文件） |
| `PUT arm1/lone.txt.sha1.bak`（终缀非 checksum） | **201** envelope | 仅**终缀**判定 |
| maven 域 GAV 合法 `PUT …/l037-1.0.0.pom.sha1`（pom 不存在） | **404** `Target file to set checksum on doesn't exist: difftest-l037-maven:com/diff/l037/1.0.0/l037-1.0.0.pom` | **域无关**（generic/maven 同形） |

源工件存在态（先 `PUT src.bin`→201，再 PUT sidecar）：

| 腿 | A | 说明 |
|---|---|---|
| `PUT src.bin.sha1`（值=正确 sha1） | **201**，**body 空（CL=0）**，`Location: …/src.bin`（**指源工件，非 .sha1 路径**） | 成功=元数据写，非文件创建 |
| `PUT src.bin.sha1`（值=错误） | **409** `Checksum error for 'arm1/src.bin.sha1': received '0000000000000000000000000000000000000000' but actual is 'f8f37527e2858024794a4acce8ff74906fb27bab'` | **值仍被写入**（见下） |
| `GET src.bin.sha1`（设值后） | **200** CT `application/x-checksum`，body=**已存 client 值**（409 腿后=0000…0，正确腿后=f8f3…） | GET 回显存值非计算值 |
| FileInfo `src.bin` | `checksums`=服务端计算恒真；`originalChecksums.sha1`=**最后一次 PUT 的 client 值（含 409 腿的错误值）** | **409 仍写穿**（write-through） |
| 重 PUT 正确值 | 201，`originalChecksums.sha1` 翻回正确值，GET 翻回 | 覆写语义 |

### B 面（88588705 live）：generic 域无拦截；maven 域有（T-563 面）

| 腿 | B | 对照 A |
|---|---|---|
| `PUT lone.txt.sha1/.md5/.sha256`（generic，源不存在） | **201** 全量 envelope，`Location`=**自身路径**，GET 200（CT `application/x-checksum`，body=PUT 原文） | **差异**：A 404 拦截 / B 存普通文件 |
| `PUT src.bin.sha1`（generic，错值） | **201**（无校验） | 差异：A 409 |
| maven GAV 合法 lone `.sha1` | **404** `Could not locate artifact. Path: 'difftest-l037-maven/com/diff/l037/1.0.0/l037-1.0.0.pom'.` | 状态同、**文案族不同** |
| maven 正确值 `.sha1` | **201** CL=0，`Location`=源工件绝对形 | **形态一致** |
| maven 错值 `.sha1` | **409** `Checksum error for 'difftest-l037-maven/com/diff/l037/1.0.0/l037-1.0.0.pom.sha1': received … but actual is …`（路径含 repo 前缀） | 状态同、文案近族（A 路径不含 repo） |
| maven 错值后 FileInfo | `originalChecksums.sha1`=**实际值**（拒绝写穿），GET `.sha1` 服务实际值 | **差异**：A 写穿错误值并回显 |
| maven 无 GAV 路径 `.sha1` | **400** maven layout 错误（先于后缀路由） | 差异：A 不做 layout 前置（404 checksum 文案） |

### 归属裁定提案（conductor 裁定；本批只提案）

拒绝条件族（A 的权威口径）：**任意仓型 × 路径终缀 ∈ {.sha1, .md5, .sha256} × 内容无关** → 解释为对源工件的 client-checksum 写入：源缺=404 `Target file…doesn't exist: <repo>:<src>`；值对=201 空体+Location 指源；值错=409 `Checksum error…` 且**值仍写穿 originalChecksums 并被 GET `.sha1` 回显**。.sha512/.asc/非终缀不在族内（普通部署）。

分类建议：
1. **generic 域拦截缺失 → BUG**（B 把 client-checksum 写入当文件部署存盘：多出实体文件节点、GET 面语义漂移〔回显 PUT 原文而非校验和约定值〕、计数面投影见 Arm 5 del3）。修复归属=generic 存储面终缀路由（与 maven 域 T-563 已有拦截对齐）。
2. **maven 域 404 文案族 → BUG（措辞族，低危）**：B `Could not locate artifact. Path: '<repo>/<src>'.` vs A `Target file to set checksum on doesn't exist: <repo>:<src>`。
3. **409 写穿语义 → UNKNOWN（待 authority）**：A 在校验失败时仍持久化 client 值且 GET 回显错误值——像 JFrog 有意（originalChecksums 语义=「客户端宣称值」）而非缺陷；B 拒绝写穿是更保守语义。需裁定对齐 or 登记 INTENTIONAL 差异。
4. maven 409 文案路径拼写（repo 前缀有无）→ 并入 2 同族微差。

---

## Arm 2 — nuget bare PUT 201 X-Checksum-Sha256 取值语义（ledger UNKNOWN nuget/bare-put-201-x-checksum-sha256）：PASS（A 活体全谱）/ B 腿 BLOCKED（license 门）

A 面 `difftest-l037-nuget`，bare 存储面 `PUT /artifactory/<repo>/<path>.nupkg`：

| 腿 | A（live，双轮恒等） |
|---|---|
| bare PUT 带 `X-Checksum-Sha256: <正确值>` | **201**；`X-Checksum-Sha256: f11f4afe…5071`（=body sha256=头原值）；`Location: …/pkg.l037/1.0.0/pkg.l037.1.0.0.nupkg`；CT `application/vnd.org.jfrog.artifactory.storage.ItemCreated+json;charset=UTF-8`；envelope 含 `checksums`+`originalChecksums.sha256` |
| bare PUT **不带**头 | **201**；**`X-Checksum-Sha256` 仍在**（=服务端计算的 body sha256，同值 f11f4afe…） | 
| bare PUT 带**错**值（40 个 0） | **409** `Checksum policy 'LocalRepoChecksumPolicy: CLIENT' rejected the artifact 'difftest-l037-nuget:pkg.l037/1.0.2/…'. Checksums info: ChecksumsInfo{checksums={SHA-1=…original='null'…}}`；**无** X-Checksum-Sha256 头 |
| GET `.nupkg` | 200 CT `application/x-nupkg` + `X-Checksum-Md5`/`X-Checksum-Sha1`/`X-Checksum-Sha256` 三头齐 |
| FileInfo | `checksums` 与 `originalChecksums.sha256` 齐（无 client 头时 originalChecksums 亦被服务端填充=计算值） |

域归属腿（push 面是否同规）：

| 面 | A（live） |
|---|---|
| v3 push（multipart `package` 字段 → `PUT /api/nuget/v3/<repo>/flatcontainer/<id>/<ver>`） | **201** CT text/plain body `Successfully published NuPkg to: flatcontainer/pkg.l037/2.0.0/pkg.l037.2.0.0.nupkg`；**无 X-Checksum-Sha256、无 Location**（grep 头计数=0） |
| v2 push（`PUT /api/nuget/<repo>/` multipart） | **201** 同形；**无 X-Checksum-Sha256、无 Location** |

**取值语义结论**：A 的 bare PUT 201 **恒渲染** `X-Checksum-Sha256`=该工件的有效 sha256（client 头提供且校验通过→头原值；未提供→服务端计算。两径同值，因错值直接 409）；不是简单回显——头缺席时由服务端补算。

B 面（88588705）：
- **静态码**：`internal/adapter/nuget/flat.go:512-523` serveBareContent PUT 臂——201 渲染 `Location`（T-567 绝对形）**但不渲染 X-Checksum-Sha256**；请求头经 `handler.go:456 declaredDigests` 消费（X-Checksum-Sha256/Sha1/Md5：畸形→400，良构不合→storage 409）——即 B 校验请求头但响应不回显。push 面（servePush，flat.go ~352）**反向**：201 渲染 `X-Checksum-Sha256`（node.Sha256 服务端计算，注释 "the checksum header stays the only extra"）但不渲染 Location。
- **活体 BLOCKED**（license 门，双轮同形）：`PUT /binflow/api/repositories/difftest-l037-nuget` → **400** `package type 'nuget' is not available (license tier 'community' < 'pro')`（r1=18082 与 r2=18083 两次取证，与 L036 先例一致）。

### 升级/维持提案

- **nuget/bare-put-201-x-checksum-sha256：UNKNOWN → BUG（升级提案）**。未取证面已全部取证完毕：头存在性（A 恒有）、取值语义（有效 sha256，服务端可补算）、校验联动（错值 409 CLIENT-policy）、GET/FileInfo 回显面、push 域归属（push 面无此头——bare 面独有）。B 缺头+已有 Location 渲染基建，对齐成本低。
- **新增差异候选（反向，B 多渲染）**：B 的 v2/v3 push 面 201 渲染 X-Checksum-Sha256，A 同面**无**（本轮 A live 双轮实证）。建议立新 ledger 条目（提案分类 UNKNOWN 待裁：去头对齐 or 保留登记 INTENTIONAL——dotnet 客户端不消费该头，无实证影响）。

---

## Arm 3 — pypi 上传响应 CT/body 消费面（ledger UNKNOWN pypi/upload-response-content-type-body）：PASS（真实 twine 7.0.0 + pip 26.2.1，判决实验双轮）

真实客户端环境：`python3 -m venv /tmp/r9-l037-venv` + `pip install twine build`（twine **7.0.0** / requests 2.34.2）；包=l037-probe（真 sdist+wheel，`python -m build` 产物）。

| 腿 | 结果 |
|---|---|
| `twine upload` → A `/api/pypi/difftest-l037-pypi`（A=200 CT `ItemCreated+json` + JSON body） | **exit=0**（whl+tar.gz 两文件，r1=1.0.0 / r2=1.1.0） |
| `twine upload` → B `/binflow/api/pypi/difftest-l037-pypi`（B=200 `text/plain; charset=utf-8` CL=0 空 body） | **exit=0**（r1=1.0.1@18082 / r2=1.1.1@18083） |
| **判决腿①**（r1）：twine → 本地 mangling 代理 → B，代理把 200 改写为 **A 形**（CT `application/vnd.org.jfrog.artifactory.storage.ItemCreated+json;charset=UTF-8` + JSON body） | **exit=0** |
| **判决腿②**（r1+r2 复跑）：同上，代理把 200 改写为 **CT text/html + body `<html>GARBAGE-NOT-JSON</html>`** | **exit=0**（r2 经新代理实例指 B2 18083 再证） |
| `pip download` → A simple 索引（`--trusted-host`） | exit=0，取回 `l037_probe-1.0.0-py3-none-any.whl` |
| `pip download` → B simple 索引（r1/r2） | exit=0，取回 whl |
| `pip install --target` → B 索引（1.0.1） | exit=0，`l037_probe/`+`l037_probe-1.0.1.dist-info/` 落地 |

代理实现：/tmp/r9-l037-proxy.py（stdlib http.server 透明转发+仅改写 200 的 CT/body；/tmp 资产，探针专用）。

**结论**：twine 7.0.0 对上传 200 响应**只看状态码，不读不校验 body 与 Content-Type**（把 body 换成 text/html 垃圾仍 exit=0）；pip 全程不触上传响应。A 的 ItemCreated+json body 与 B 的空 text/plain 在真实客户端消费面**等价**。

### 升级/维持提案

review_gate 预注册条件（「取证 twine/pip 消费面后升级」）**已满足**：提案 **UNKNOWN → INTENTIONAL**（登记 B `text/plain; charset=utf-8` CL=0 为有意差异；消费面零影响、对齐 JFrog 私有 CT 无收益）。终局裁定权 conductor/compatibility-engineer。

---

## Arm 4 — goproxy .info/.mod mime 相邻面：PASS（A 活体取证；本臂不裁，供「另行裁定」证据）

A 面 `difftest-l037-go`，模块 `l037.local/m`（storage 面 PUT 四后缀，r1=v1.0.0 / r2=v1.0.1 双轮）：

| 后缀 | A storage 面 GET | A `/api/go/<repo>/` GOPROXY 面 GET | A FileInfo mimeType |
|---|---|---|---|
| `.info` | 200 **`application/json+info`** | 200 **`application/json+info`** | `application/json+info` |
| `.mod` | 200 **`text/plain+mod`**（无 charset） | 200 **`text/plain+mod`** | `text/plain+mod` |
| `.zip` | 200 `application/zip` | 200 `application/zip` | `application/zip` |
| `.mod.gz` | 200 **`application/x-gzip`** | **404**（CT application/json） | `application/x-gzip` |

（同仓对照：这些拼写为 **go 仓域特有**——generic 仓同后缀落出厂表/兜底，如 .info=octet-stream；支持 mime-ownership 裁定模型「goproxy 域自有 .info/.mod」的归属面。）
> **勘误（2026-09-29，R9 双审 A/B 共同发现）**：本括注中「generic 仓 .info=octet-stream」系未实测推断，与 T-571 62 键 sweep 实测矛盾——A 出厂表实含 `.info`/`.mod` 键，generic 仓同渲染表值 `application/json+info` / `text/plain+mod`（双面 SAME）。台账条目 generic/mime-ownership-model 已载正确口径；此处原文保留、以本勘误为准。

真实 go 客户端腿（A）：**BLOCKED（结构性）**——go1.27.1 拒绝向 http 明文 URL 传凭据：GOPROXY 内嵌 user:pass → `refusing to pass credentials to insecure URL`；NETRC 指向 0600 netrc（含 host 与 host:port 两种条目）→ 仍 401（cmd/go 对 http 不注入 netrc 凭据，仅 https）。A 面 http+basic 的 go 客户端腿在本实例拓扑下不可达，CT 面由 curl 双面取证覆盖（go tool 对 .info/.mod 的 CT 本就不做严格校验，按内容解码）。

B 面（88588705）：活体 BLOCKED（license 门：`package type 'go' is not available (license tier 'community' < 'pro')`，双轮）。**静态码** `internal/adapter/goproxy/handler.go:246-258 contentTypeOf`：`.info`→`application/json`、`.mod`→`text/plain; charset=utf-8`、`.zip`→`application/zip`、default→`application/octet-stream`；@v 路由仅 {zip, mod, info}（layout.go:59），`.mod.gz` 非路由扩展。

### 候选方案（供 goproxy mime「另行裁定」，本臂不裁）

1. **对齐 A 拼写**：`.info`→`application/json+info`、`.mod`→`text/plain+mod`（去 charset）；`.zip` 已同；`.mod.gz` 维持不路由（A api 面同 404），storage 面如对齐则 `application/x-gzip`。
2. **维持 B 现值**：A 的 `+info`/`+mod` 后缀形是非标准拼写（非 IANA 注册型）；go 工具按内容解码不校验 CT（本轮 BLOCKED 未实证，属公开协议行为）。
3. 折中：仅 `.mod` 去 charset 差异收敛，`.info` 待真实客户端实证后再裁。
注：本臂 CT 差异与 BIN-53（stdlib 回退删除）正交——此处是 goproxy 域显式渲染值，非回退路径。

---

## Arm 5 — rest/repo-delete-nonempty-cascade 计数臂复核（R7 B 审交办）：PASS（双端活体双轮，口径一致结论）

B 88588705 现状（T-555/BIN-37 已落地）：`DELETE /api/repositories/<key>` = **静默级联**（`repositories.go:825-853`：无守卫；`?deleteContent=true` 接受且忽略；计数=countRepoArtifacts=「files + folder rows」删除前量取）——L032 时代的 400 守卫已被 2026-09-29 D-3 user ruling 翻案，本轮 live 复核证实。

四形态（generic 仓，A/B 同名同构填充；r1/r2 双轮计数全等）：

| 形态 | 内容构成 | A deletedArtifactsCount | B deletedArtifactsCount | 对照 |
|---|---|---|---|---|
| del1 纯平文件 | 3 文件 0 目录 | **3** | **3** | 一致 |
| del2 嵌套 | 2 文件 + 2 目录（d1, d1/d2） | **4** | **4** | 一致 |
| del3 含 sidecar | 2 文件 + 1 目录 + 1 次 client-checksum SET（`.sha1` PUT） | **3** | **4** | **差异（可完全归因）** |
| del4 深层混型 | 4 文件 + 7 目录（x, x/1, x/2, y, d, d/e, d/e/f） | **11** | **11** | 一致 |

statusMsg 双端逐字一致：`Repository '<key>' and all its content have been removed successfully.`；body 键集一致（repoKey/statusMsg/deletedArtifactsCount/success）。

**口径结论**：A=**文件节点+目录行全计**（del4=4+7=11 实证目录计入；del1=3 实证纯文件只计文件；repo 根不计——无仓会数出根行）；client-checksum SET **不产生计数**（A del3=3=2f+1d，元数据写无节点）。B=同一口径（del1/2/4 全等）。**del3 的 3-vs-4 不是计数口径差异**，是 Arm 1 存储模型差异的计数面投影（B 把 `.sha1` 存成实体文件节点→计数+1；A 记元数据→+0）。Arm 1 修复后 del3 自然收敛，计数面无需独立票。

附注（A 面 .sha1 GET 面，Arm 5 副产物）：generic 仓未设 client checksum 时 `GET /f.bin.sha1`=**404**（不按需生成）——A 的 checksum 文件 GET 面仅服务已 SET 的 client 值（Arm 1 已证设值后 200 回显）。B generic 仓存了什么 GET 就回什么（普通文件）。归 Arm 1 差异族。

---

## NOT_RUN / BLOCKED 如实清单

- 臂2/臂4 B 面活体：**BLOCKED**（scratch community 档 license 门拒建 nuget/go 仓；400 文案双轮取证在案；静态码+单测引用为 B 侧证据——与 L036 同一先例）。
- 臂4 真实 go 客户端（A）：**BLOCKED**（go 1.27.1 对 http GOPROXY 的凭据结构性拒发，见 Arm 4 节；非产品面问题）。
- 臂2 dotnet 真客户端腿：NOT_RUN（本机无 dotnet；票面明令「nuget 腿 curl 级可」）。
- 臂3 pip 对 ItemCreated body 的消费：不适用（pip 不触上传响应，twine 是唯一上传消费方，已由判决腿覆盖）。

## 环境处置

- A：`difftest-l037-{generic,maven,nuget,pypi,go,del1..5}` 全部 DELETE，复验 `/api/repositories` 前缀 grep 计数=**0**；实例配置零触碰。
- B：18082/18083 两实例 `difftest-l037-*` 全删（计数 0/0），SIGTERM graceful stop（日志 `"binflow stopped"` ×2 在案），端口释放复验（healthz=000）。
- /tmp：mangling 代理三实例已停；netrc 凭据文件已 `rm`；探针资产（r9-l037-* 脚本/日志/样本）留存 /tmp 备查。
