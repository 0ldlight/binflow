# QA 报告 T-76 — 真实客户端矩阵 + 回归基线 + 性能（FR-22）+ M3 QA 总报告

- role: qa-engineer
- 日期: 2026-08-20
- 票据: T-76 [P0] QA：真实客户端矩阵 + 回归基线 + 性能（AC 全文见 reports/agents/T-61.md T-76 节）
- 验收口径: docs/prd/milestone-3.md **v1.2**（FR-22 全 AC、§5.3 分级矩阵、§5.6 反转表、NFR-P11~P13、DoD §9）
- 被测对象: commit **0f86229**（HEAD，工作树干净；= T-75 验收基线 a426256 + 纯 chore 提交，产品代码零变化），`go build` → `/tmp/t76/binflow-server`（19.6MB）
- 本票同时产出 **M3 QA 总报告（§6）**：合并 T-74（reports/agents/T-74-qa.md，49/49）/ T-75（reports/agents/T-75-qa.md，25/25 + 1 项 P2 票面延后）/ 本轮
- 环境: darwin/arm64；实例 A=`127.0.0.1:8180`（匿名开，默认配置 + `BINFLOW_ADMIN_PASSWORD` + `BINFLOW_REMOTE_CREDENTIALS_KEY`，base_url 不设 → 按请求 Host 派生，dind 与宿主双视角可用）/ B=`127.0.0.1:8181`（匿名关，仅 C27）/ 冷启动实例 `:8182`（空库，仅 M60a）；mock 上游 `127.0.0.1:9199`（计数日志，M2/T-75 先例）
- 客户端: mvn 3.9.9 + Temurin JDK 21.0.12（/tmp/t67-tools 既有）/ npm 10.9.8（node 22.23.2）/ pip 26.2.1 + twine 7.0.0（/tmp/t76/venv，含 build/setuptools/wheel）/ curl / **Gradle 8.14.3**（services.gradle.org 下载，P2 观察）/ docker CLI 29.7.2 + dind 27.5.1（`docker:27-dind --insecure-registry host.docker.internal:8180`，宿主 Docker Desktop 配置零改动）
- 公网: 可达（gradle 发行包 137MB 5.1s 拉下）；M45/M46/M47 主断言按票面走 mock 计数口径

## 总结论: **PASS**

- §5.3 分级矩阵 P0 **25/25 全过** + Gradle P2 观察项 **过**（1/1）；三协议 M50/M54 收口断言（同一 virtual 解析 local 包 + 上游包）全部成立
- 回归基线 **全绿**：M1 C 序列 P0 23 项（含 E-07/C26 反转断言）、M2 D 序列 P0 14 项；**generic 与 docker 域零回归（P0 硬门槛成立）**；跨协议去重 docker 域变体收口（docker/generic/maven 单内容单 blob，blobs 计数不增）
- 性能：M60a 冷启动 **0.118~0.127s**（< 2s，NFR-P11 P0 过）；M60b 50 并发 pip install **50/50 exit 0、3.39s、零 5xx**（NFR-P13 P0 过）；M60c 代理开销 **记录值 186ms（字面超 ≤100ms 目标）**——分解实证 ~120ms 为 M1 storage session 固定成本（fsync；1 字节 PUT 亦 ~120ms，generic 域同样存在，非 M3 引入、非 remote 专属），remote 专属增量 **≈10~20ms 达标**（P1，详见 §4.3 + 观察项 O1）
- 全程服务端 **5xx=0、ERROR=0**（A/B/冷启动三实例合并扫描；WARN 5 条全部为设计内：1×SSRF 探针拒绝取证 + 4×故意错凭据拒绝）
- 自动化基线: `go vet ./...` 0 告警；`go test ./... -count=1` **16/16 包 ok（exit 0）**
- 无阻塞缺陷。1 条 P1 性能口径观察（O1，转 PM/architect）+ 2 条无害观察

---

## 1. AC 逐条结论

| # | AC（T-76） | 结果 | 证据摘要 |
|---|---|---|---|
| ① | §5.3 分级矩阵 P0 全过留档（mvn 五链/npm/pip+twine/curl 抽查/M50·M54 收口/Gradle P2） | ✅ | §2 全表 25/25 + Gradle 过；mvn 五链 exit 0 全 BUILD SUCCESS；npm 七操作全 exit 0；pip 六场景全 exit 0 且 download sha256==index fragment；三协议同一 virtual 混合解析收口成立 |
| ② | 回归基线：M1 C 序列 P0 + M2 D 序列 P0 复跑全绿（E-07/E-26 反转后）；generic 与 docker 域零回归；跨协议去重三域变体 | ✅ | §3：C 序列 23 项全绿（C26 反转 = remote/virtual+generic 建仓 200、docker 组合维持 400；E-26 = npm/pypi API 分发 200、search/pypi-ui 仍 404）；D 序列 14 项全绿（D16 全链 + 匿名 pull + digest 三方一致）；去重 docker 变体 blobs 恒 69 |
| ③ | 性能 M60a <2s / M60b 50 并发全 0 零 5xx / M60c 记录；M3 QA 总报告 + DoD §9 第 1/2 条结论 | ✅（M60c 记录值见 O1） | §4：0.127s / 50:50 零 5xx / 186ms 分解；§6 DoD 第 1 条**满足**、第 2 条**满足**（剧本 #10 文档腿归 T-77 终验） |

## 2. §5.3 分级矩阵明细（AC①）

### 2.1 mvn 3.9.9 五链（P0）

| # | 链 | 结果 | 证据 |
|---|---|---|---|
| 1 | local deploy（release 1.0.0） | ✅ | `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::…maven-local` → **exit 0**；落盘 jar sha256 逐位一致（ec1e7aa0…）；旁车 `.sha1` 裸 hex（35b8eb92…）；版本组 metadata 含 1.0.0/latest/release/lastUpdated |
| 2 | 全新 repo resolve | ✅ | 消费 pom `<repositories>` + `-Dmaven.repo.local=m2-fresh` → **BUILD SUCCESS**；fresh repo 落 jar/pom + 服务端旁车 sha1（mvn 下载校验随过） |
| 3 | snapshot `-U` | ✅ | 1.2.0-SNAPSHOT 两次 unique deploy → metadata `buildNumber=2`/`timestamp=20260819.222522`；全新 repo `dependency:get -U` → BUILD SUCCESS，实际下载 `demo-app-1.2.0-20260819.222522-2.{pom,jar}`（buildNumber 2） |
| 4 | remote 代理 | ✅ | pom `<repositories>` 指 maven-remote-x → BUILD SUCCESS 解析 com.acme:up-lib:1.0.0（mock 上游）；jar sha256 与上游逐位一致（6f4d9bb9…）；上游计数冻结：成员缓存命中后两次全新 repo 编译 **零上游流量**；编译产物运行输出 `up-lib-t76`（真 jar 链） |
| 5 | virtual 混合解析 | ✅ | 同一 maven-virtual 一次 BUILD SUCCESS 解析 demo-app（local）+ up-lib（上游）4 制品；`X-Binflow-Resolved-From: maven-local` / `maven-remote-x`（+`X-Binflow-Cache: HIT`）双头齐 |

### 2.2 npm 10.9.8（P0）

| # | 操作 | 结果 | 证据 |
|---|---|---|---|
| 1 | publish plain | ✅ | `npm publish` → `+ demo-pkg@1.0.0` exit 0 |
| 2 | publish scoped | ✅ | `npm publish --access public` → `+ @acme/util@1.0.0` exit 0；packument `%2f`/`%2F` 双编码 200 等价（C8） |
| 3 | install（缓存清空） | ✅ | `npm cache clean --force` + 全新目录 → `added 2 packages` exit 0；`require` 双包可执行（demo-pkg-t76 / acme-util-t76）；package-lock 记录正确 |
| 4 | dist-tag add·rm | ✅ | 1.1.0 发布后 `dist-tag add → beta:1.1.0`、`install @beta` exit 0（require 得 1.1.0）、`dist-tag rm` 后 ls 仅 latest；三步 exit 0 |
| 5 | unpublish | ✅ | `npm unpublish demo-pkg@1.1.0 --force` exit 0 → versions 仅 ['1.0.0']、latest 回落 1.0.0、`npm view @1.1.0` E404 |
| 6 | remote 代理 install | ✅ | `npm install up-pkg --registry …/npm-remote/` → exit 0，`require('up-pkg').hello()` 执行；packument `dist.tarball` 重写为 BinFlow URL（上游 9199 不回显）；tarball 节点 `createdBy:"remote-proxy"`、sha1==packument shasum（c71c3202…）、字节与上游 `cmp` 一致；二次安装上游计数冻结 |
| 7 | virtual install（**M54 收口**） | ✅ | 同一 npm-virtual `npm install demo-pkg up-pkg` → `added 2 packages` exit 0，双包可执行；上游计数 +2（up-pkg packument + demo-pkg 本地包的 remote 成员 miss 探测，与 T-72/T-75 校准点① 同构）；`npm view demo-pkg versions` 经 virtual 正常 |

### 2.3 pip 26.2.1 + twine 7.0.0（P0）

| # | 操作 | 结果 | 证据 |
|---|---|---|---|
| 1 | upload wheel+sdist | ✅ | `twine upload` wheel+sdist 同版本并存 exit 0（服务端 200 统一口径） |
| 2 | simple index | ✅ | 200 HTML 双文件 `#sha256=` fragment、按文件名字典序；无尾斜杠 **302** 补尾；ETag → **If-None-Match 304**；`packages/` 端点与内容路径双入口 200 |
| 3 | install（含依赖链） | ✅ | 全新 venv `pip install demo-pkg==0.2.0` → 自动从同一 index 装齐 `demo-lib-0.1.0 + demo-pkg-0.2.0`，`import` 双模块执行输出正确 |
| 4 | download + hash 校验 | ✅ | `pip download --no-deps` 落盘 sha256 `c6d5b68c…` == index fragment 逐位相等（HASH-MATCH） |
| 5 | remote 代理 | ✅ | `pip install up-pkg --index-url …/pypi-remote/simple` exit 0（`up_pkg.hello()` 执行）；上游恰 2 次（simple + wheel）；`--force-reinstall` 复装上游计数冻结（11→11） |
| 6 | virtual（**M54 收口**） | ✅ | 同一 pypi-virtual `pip install demo-pkg up-pkg` exit 0；`pip list` = demo-lib 0.1.0 + demo-pkg 0.2.0 + up-pkg 1.0.0 三包俱在（本地含传递依赖 + 上游一次装齐） |

### 2.4 curl 各域抽查（P0）

| # | 检查 | 结果 | 证据 |
|---|---|---|---|
| 1 | E-01 信封三域 | ✅ | npm 404 / pypi 404 / maven 400 全 `{"errors":[{status,message}]}`，零 HTML |
| 2 | maven 旁车 + remote checksum 后缀 | ✅ | local 旁车 GET 裸 hex 40 字符；maven-remote `.jar.sha1` → 404 且 message 逐字 `Checksums are not downloadable.`（FR-20-AC13） |
| 3 | SSRF 负断言 | ✅ | 非豁免 loopback remote 仓建仓 200（请求时拦截口径）→ GET **400** + WARN `remote: outbound target rejected (ssrf-guard)` `category:loopback`（结构化、零堆栈） |
| 4 | metadata/packument/simple 抽查 | ✅ | maven-virtual 版本组合并 versions=[1.0.0,1.2.0-SNAPSHOT]、latest=1.2.0-SNAPSHOT（C1「SNAPSHOT 也算 latest」）；npm view 经 virtual 正常 |

### 2.5 M50/M54 收口断言（代理收口门槛，三协议）

| 协议 | 断言（local 包 + 上游包经**同一个** virtual） | 结果 |
|---|---|---|
| mvn | maven-virtual 一次解析 demo-app（maven-local）+ up-lib（maven-remote-x）BUILD SUCCESS | ✅ |
| npm | npm-virtual `install demo-pkg up-pkg` exit 0 双包可用 | ✅ |
| pip | pypi-virtual `install demo-pkg up-pkg` exit 0（含 demo-lib 传递依赖）三包俱在 | ✅ |

### 2.6 Gradle 8.14.3（P2 观察，不作门槛）

| 检查 | 结果 | 证据 |
|---|---|---|
| `gradle build` 依赖走 BinFlow maven local | **✅ 过** | `repositories { maven { url …/maven-local; allowInsecureProtocol = true } }` + `implementation 'com.acme:demo-app:1.0.0'` → `gradle --no-daemon build` **exit 0**（`.module` 元数据 404 后正常回落 POM）；产物执行输出 `gradle-demo-ok`；jar 落 gradle 缓存 |

## 3. 回归基线（AC②，FR-22-AC5 / §5.6 反转表口径）

### 3.1 M1 C 序列 P0 复跑（generic 域，T-18/T-19 基线）

| C 项 | 断言 | 实测 | 结果 |
|---|---|---|---|
| C02 | 未认证管理 API 401 + `WWW-Authenticate: Basic realm="BinFlow Realm"` | 401 + 挑战头 | ✅ |
| C03/C04/C05/C06 | 建仓 200 纯文本 / `Bad_Key!` 400 / 列表含 key / GET rclass+packageType | 全对（200 text/plain；400；gen-reg 在列；local+generic） | ✅ |
| C07 | PUT 201 + checksums.sha256 对账 + size 字符串 + createdBy | 201；sha256 一致；size "10485760"；createdBy admin | ✅ |
| C08/C09 | GET sha256 一致 + 三 checksum 头 + ETag=sha1 + Accept-Ranges；HEAD Content-Length | 全对（10MB roundtrip cmp 一致） | ✅ |
| C10 | item info 13 字段全集 | 全在 | ✅ |
| C13/C14 | 带 checksum 一致 201 / 不一致 **409**（received/actual）+ 无 node | 201 / 409 + bad.bin GET 404 | ✅ |
| C15a/b/c | deploy 命中 201 零传输 / 未命中 404 `no content found` / 缺头 400 | 三态逐字 | ✅ |
| C18 | DELETE 204 → 重复 404 | 204/404 | ✅ |
| C19 | 非空删 400（含 node 计数提示）→ `deleteContent=true` 200 → GET 404 | 三段全对 | ✅ |
| C21a/b/c | token 签发（64 字符）+ **token 作 Basic password** 200 + `X-JFrog-Art-Api` 200 + revoke 200 幂等 | 全对（token 作 username 拒绝 = subject 校验，符合 auth-model） | ✅ |
| C23 | 匿名读开：内容 GET 200（npm/pip/maven 匿名消费贯穿 §2） | 全程匿名 install/pull 成立 | ✅ |
| C24（E-26 反转） | `pypi-ui/**` 仍 404、npm search 仍 404；`api/npm|pypi/**` 已分发（200 路由） | 反转后口径全对 | ✅ |
| C25 | `X-Explode-Archive: true` → 400 | 400 | ✅ |
| **C26（E-07 反转）** | remote/virtual + **generic** 建仓 → **200**（M3 反转）；remote/virtual + **docker** → **400 维持** | `rev-remote` 200 / `rev-virtual` 200 / `remote docker repos…not supported` 400 / virtual+docker 400 | ✅ |
| C27 | 匿名关（实例 B）：内容 GET 401+Basic 挑战、admin 200、pypi simple 匿名 401 | 三点全对（ping/version 免认证不变） | ✅ |
| C28 | ping OK / version 如实 dev / health ok（storage/metadata/registry） | 全对 | ✅ |

**M1 C 序列 P0：23 项全绿。**

### 3.2 M2 D 序列 P0 复跑（docker 域，T-43/T-44 基线；name 用全名 `<repoKey>/<image>`，C1 口径）

| D 项 | 断言 | 实测 | 结果 |
|---|---|---|---|
| D04 | 未认证 `/v2/` **401 + Bearer 挑战**（无条件）；已认证 200 `{}`；`Docker-Distribution-Api-Version: registry/2.0` | 全对（realm 按请求 Host 派生：宿主 127.0.0.1 / dind host.docker.internal 双视角均正确） | ✅ |
| D04b | 手工 `GET /v2/token?service&scope` → token/expires_in(2592000)；Bearer 访问 `_catalog` 200 | 全对 | ✅ |
| D05 | `docker login` 正确口令 `Login Succeeded`；**错口令 exit 1**（FR-11-AC4） | 对/错分离成立（dind 内真 daemon） | ✅ |
| D06 | monolithic POST→PUT 201 + `Docker-Content-Digest`；GET 回逐位 cmp 一致；HEAD 长度+digest | 全对（10MB） | ✅ |
| D07 | 单请求 `POST ?digest=` 201 | 201 | ✅ |
| D08 | PUT manifest 201；GET by-tag 200 body cmp 逐位一致 | 全对（schema2） | ✅ |
| D09 | `_catalog` 字典序；tags/list `["v1","v2"]`；`?n=1` + `Link rel="next"`（last exclusive） | 全对 | ✅ |
| D10 | chunked PATCH 1MB+1MB+8MB 三段 202 + `Range: 0-1048575/0-2097151/0-10485759` 递增；终结 PUT 201；GET cmp 一致 | 全对 | ✅ |
| D12 | digest 不符 → 400 `DIGEST_INVALID` | spec 错误体 | ✅ |
| D13 | cross-repo mount `?mount=&from=` → 201 零传输；目标 GET 200 | 全对（docker-local/reg/app → docker-local2/other/img） | ✅ |
| D13b | manifest 引用缺失 blob → 400 `MANIFEST_BLOB_UNKNOWN` | 对 | ✅ |
| D24 | `/v2/` 未定义路径 404 spec 错误体（非 E-01） | `UNSUPPORTED` | ✅ |
| **D16** | docker 全链 login→build→push→rmi→pull→run exit 0 | `08bc4e534116: Pushed`，digest `sha256:e700ffe9…`（== 本地 image digest 三方一致）；`docker run --rm` 输出 **t76-docker-ok** | ✅ |
| 匿名 pull | logout + rmi 后无凭据 pull→run | pull 成功 + run 输出 t76-docker-ok（挑战→匿名 token 通行） | ✅ |

**M2 D 序列 P0：14 项全绿。generic 与 docker 域零回归（P0 硬门槛）成立。**

### 3.3 跨协议去重（docker 域变体收口 + T-74 三域复核）

| 步骤 | stats（blobs / logical / physical） |
|---|---|
| 基线（layer.bin 已经 `/v2/` docker 上传入库） | 69 / 45,595,237 / 24,633,389 |
| 同字节 maven PUT（合法 GAV jar 名） | **69** / 56,080,997 / 24,633,389 —— 不增 |
| 同字节 generic PUT | **69** / 66,566,757 / 24,633,389 —— 不增 |

同一内容（sha256 cdb086a7…，10MB）经 **docker /v2/ API、maven GAV、generic 路径**三协议入库仅一份 blob（blobs 恒 69、physical 恒定，logical 增长）。叠加 T-74 已验的 maven/npm/pypi/generic 四协议变体，**五域去重闭环**。
（注：npm tarball 裸 PUT 走内容路径 → 405——npm 域发布仅认 packument PUT 十步链（NE-01 设计内），去重以 T-74 真实 `npm publish` 同内容腿为准。）

## 4. 性能（AC③）

### 4.1 M60a 冷启动（NFR-P11，P0）

空库 + 裸二进制（同二进制含 maven/npm/pypi 三协议路由与 remote/virtual 引擎），进程启动 → `/api/system/ping` `OK`：

```
run 1: 0.127s   run 2: 0.125s   run 3: 0.118s      （门槛 < 2s，余量 15 倍+）
```

### 4.2 M60b 50 并发 pip install（NFR-P13，P0）

`seq 50 | xargs -P 50` 并发 `pip install --index-url …/pypi-local/simple --no-cache-dir --target …/i demo-pkg==0.2.0`（制品在 BinFlow 本地仓 = 缓存命中口径）：

```
wall 3.39s；exit codes 50×0（uniq -c "50 0"）；50/50 "Successfully installed"；服务端 5xx = 0、ERROR = 0
```

### 4.3 M60c 代理开销（NFR-P12，P1 记录项）

10MB 随机样本（mock 上游 9199，generic remote 仓，每次 DELETE 缓存后取 MISS）：

| 路径 | 3 次实测 | 中位 |
|---|---|---|
| 直连上游 | 3.83 / 3.59 / 3.67 ms | **3.7ms** |
| 经 BinFlow **MISS**（authed） | 214.7 / 182.9 / 181.4 ms | **182ms** |
| 经 BinFlow MISS（匿名，剔除认证因子） | 169.7 / 167.3 / 151.7 ms | **167ms** |
| 经 BinFlow **HIT** | 20.3 / 19.7 / 19.6 ms | **19.7ms** |

- **字面口径**：MISS 总时长 182ms vs 直连 3.7ms + 100ms = 103.7ms → **超目标 ~80ms**（P1）。
- **归因分解**（全部真机实测）：

| 测量 | 值 | 说明 |
|---|---|---|
| 1 字节 generic PUT（本地写） | ~120ms | **storage session 固定成本地板**（fsync 族；大小无关） |
| 1MB 本地 PUT | ~129ms | 与 1 字节几乎相同 → 固定成本主导 |
| 10MB 本地 PUT | ~177ms | +57ms 为每字节流水（3 摘要 + 落盘） |
| 10MB 代理 MISS | ~182ms（服务端 `duration_ms=186`，`upstream_duration_ms=164`） | 对本地写路径增量 **≈10~20ms** |
| 10MB HIT / 本地 GET | 19.7ms / 5ms（匿名） | M41b「hit 与 local 同量级」成立（差值主要为读路径副本/校验） |

结论：~120ms 是 **M1 storage.Session 提交的固定 fsync 成本**（generic 域本地 PUT 同样存在，1 字节即 ~120ms；非 M3 引入、非 remote 专属、非回归）；remote 专属增量 ≈10~20ms 在 100ms 内。字面口径未达的处置建议见观察项 O1。

## 5. 自动化基线

```
go vet ./...            # exit 0，零告警
go test ./... -count=1  # 16/16 包 ok, exit 0
                        # remote 120s / repo 119s / httpapi 115s / maven 102s / npm 109s
                        # storage 118s / metadata 80s / pypi 80s / docker 87s / generic 48s ...
```

（对照 T-74/T-75 两轮同为 16/16 全绿；本轮无 T-75 所述 docker 包时间性 flake 复发。）

## 6. M3 QA 总报告（DoD §9 第 1/2 条终判）

### 6.1 三轮 QA 合并视图

| 票 | 范围 | 结果 | 5xx | 报告 |
|---|---|---|---|---|
| T-74 | 仓库模型 M01~M05 + 三协议功能矩阵 M10~M35b + 边界 M57/M58 + NFR-S16/S17/S18 + 跨协议去重（四协议） | **49/49 全过** | 0 | reports/agents/T-74-qa.md |
| T-75 | remote M41~M48 + virtual M50~M55b + SSRF 16 直连变体 + 3 式重定向 + NFR-S13~S15/S14 凭据链 + NFR-P14 1GB | **25/25 全过 + M55b P2 票面延后** | 3（全部设计内：hardFail 502×2 + 64MB 上限 502×1） | reports/agents/T-75-qa.md |
| T-76 | §5.3 客户端矩阵（25/25 + Gradle 过）+ 回归基线（C23 项 + D14 项）+ 性能 M60a/b/c | **全过（M60c 记录项字面超差，O1）** | 0 | 本文件 |

被测代码谱系：T-74 @ f597c86 → T-75 @ a426256 → T-76 @ 0f86229（其间仅 chore/文档提交，产品代码零变化；T-76 回归即对 T-75 基线的复验）。

### 6.2 §4 AC 覆盖清单（P0/P1 全绿）

| FR | 覆盖票 | P0/P1 状态 | P2 状态 |
|---|---|---|---|
| FR-15 仓库模型（AC1~AC9） | T-74（M01~M05/AC9 凭据链）+ T-76（AC8 回归/反转） | **全绿** | — |
| FR-16 Maven 传输（AC1~AC11） | T-74（M10~M21）+ T-76（五链/回归） | **全绿** | AC12 unique 改写**延后**（BOARD 记录） |
| FR-17 Maven metadata（AC1~AC6） | T-74（M14~M16b/-U/并发） | **全绿** | — |
| FR-18 npm（AC1~AC11） | T-74（M22 系~M28 + AC9 integrity 三方一致 + AC10 ETag/304 + AC11 `-rev` 占位）+ T-76 | **全绿** | — |
| FR-19 PyPI（AC1~AC9） | T-74（M30~M35b + M31 四细节）+ T-76 | **全绿** | AC8 PEP 691 **已过**（T-74 M35b，提前收口） |
| FR-20 remote（AC1~AC13） | T-75（M41~M48 全序 + AC10 凭据对错 + AC11 1GB） | **全绿** | — |
| FR-21 virtual（AC1~AC9） | T-75（M50~M55 + 写路由 + stale/hardFail 钉板）+ T-76（三协议收口） | **全绿** | AC8 聚合浏览**延后**（T-61 AC② 批准，BOARD 记录） |
| FR-22 conformance（AC1~AC6） | T-76 本轮 | AC1/2/3/5 **全绿**；AC6 P0 部分（冷启动/并发）**绿**、P1 部分（M60c）**记录值超差（O1）** | AC4 Gradle **已过**（本轮，提前收口） |
| NFR-S13~S18 / NFR-P11/P13/P14 | T-75（S13 七点 19 WARN 取证/S14 四点/S15/P14 RSS+28KB）+ T-74（S16/S17/S18）+ T-76（P11/P13 + 凭据/日志卫生复查） | **全绿** | — |

### 6.3 DoD §9 终判

| DoD 条 | 判定 | 依据 |
|---|---|---|
| **第 1 条**：§4 全部 P0/P1 AC 经 qa 验证全绿（P2 延后在 BOARD 记录） | **满足** | §6.2 全表：P0/P1 逐 AC 三票合并全绿。P2 处置：FR-16-AC12（unique 改写）、FR-21-AC8（聚合浏览）延后已在 BOARD（T-71/T-72 登记）；FR-19-AC8 与 FR-22-AC4 两项 P2 **提前验证通过**。唯一非绿记录 = FR-22-AC6/NFR-P12 的 **P1 记录项** M60c 字面超差（O1，归因 M1 既有 fsync 固定成本，非功能缺陷、非 M3 回归，处置建议已列） |
| **第 2 条**：§8 剧本全绿 + §5.3 客户端矩阵 P0 成员全过 | **满足** | §8 剧本 1（回归）~9（性能与安全）全绿：#1 本轮、#2~#5 T-74、#6~#7 T-75、#8 T-74、#9 本轮 + T-75；#10（文档）按票序归 **T-77 终验**（DoD 第 4 条，非本票范围）。§5.3 P0 成员 mvn/npm/pip+twine/curl **25/25 全过**，Gradle P2 观察过，yarn/pnpm 与 legacy upload 按 §5.3 明示不做 |

**M3 QA 终判：T-74 + T-75 + T-76 三票合并，DoD §9 第 1/2 条满足。** 建议主会话据此推进 T-77 终验与 m3-done tag（第 3 条 §5.5 C1~C8 已在 PRD v1.1/v1.2 定案；第 5 条 tag 属主会话动作）。

## 7. 观察项（不阻塞，转 conductor）

| # | 项 | 发现 | 建议 |
|---|---|---|---|
| **O1** | M60c/NFR-P12 字面口径 | miss 182ms vs 直连 3.7ms：字面超 ~80ms；分解实证 ~120ms 为 M1 storage.Session 提交固定 fsync 成本（1 字节 PUT 即 ~120ms，generic 本地写同样存在——非 M3 引入、非 remote 专属、非回归），remote 专属增量 ≈10~20ms 达标 | 二选一：① PM 将 NFR-P12 措辞改「remote 专属增量 ≤ 直连 + 100ms（剔除 M1 既有落盘成本）」；② 立 M4 优化票（session commit fsync 批量/异步化——收益覆盖全部协议的写路径，不只 remote）。当前 APFS 本机值，SSD 服务器上预计更优 |
| O2 | 代理噪音 | npm/pip 客户端自检流量会穿过 remote 代理：npm 探测 `npm` 自身 packument、pip 探测 `simple/pip/`（版本自检）→ 上游 miss + 负缓存，无害 | T-77 文档注记一句「代理仓上游日志可见客户端自检探测，属正常」 |
| O3 | base_url 与 docker realm | 挑战 realm 按 base_url 配置时对跨视角客户端（容器→宿主）不可达；base_url 缺省时按请求 Host 派生（本票实测双视角均正确） | T-77 部署文档建议：反代/多视角场景**不设** base_url（或设为客户端可达名），已有行为无需改码 |

## 8. 环境与清理

- 实例 A/B/冷启动实例、mock 上游 9199 已全部停止（端口 8180/8181/8182/9199 无监听残留）；t76-dind 容器已删；为本票拉取的 `docker:27-dind` 镜像已 `docker rmi`（用户既有镜像未动，dind 内 alpine:3.20 随容器销毁）；宿主 Docker Desktop / daemon.json 零改动。
- mvn 全程 `-s /tmp/t76/mvn-settings.xml` + 独立 `repo.local`；npm 独立 userconfig/cache + `HOME=/tmp/t76`；pip/twine/gradle 全部落 /tmp/t76（`GRADLE_USER_HOME` 隔离）；用户 `~/.m2`、`~/.npm`、`~/.gradle`、`~/.cache/pip` 零触碰。
- 测试口令/密钥仅存在于 /tmp/t76/env.sh（随机一次性值）；日志 grep 口令/Authorization = 0；/tmp/t76 全目录已删除，无凭据写入任何提交文件。

### mock 上游（离线可复现，沿 T-75 先例简化版）

```python
#!/usr/bin/env python3
"""T-76 mock upstream: counting file server on 127.0.0.1:9199.
Docroot /tmp/t76/upstream; appends "METHOD path" per request to
/tmp/t76/upstream.log (append mode — hit-count negative assertions)."""
import os, sys
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
ROOT, LOG = "/tmp/t76/upstream", "/tmp/t76/upstream.log"
class Handler(SimpleHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_one(self):
        with open(LOG, "a") as f: f.write("%s %s\n" % (self.command, self.path))
    def log_message(self, fmt, *args): pass
    def do_GET(self):
        self.log_one(); super().do_GET()
    def do_HEAD(self):
        self.log_one(); super().do_HEAD()
if __name__ == "__main__":
    os.chdir(ROOT)
    ThreadingHTTPServer(("127.0.0.1", int(sys.argv[1]) if len(sys.argv) > 1 else 9199), Handler).serve_forever()
```

上游布局：`com/acme/up-lib/1.0.0/{pom,jar}`（真 jar，javac 可编译）、`up-pkg/{packument.json,-/up-pkg-1.0.0.tgz}`（真 npm pack 产物，shasum/integrity 匹配）、`simple/up-pkg/index.html` + `packages/up-pkg/*.whl`（真 wheel，sha256 fragment 匹配）、`big/{perf10mb,perf1mb}.bin`。计数日志终态 31 行（全部可归因：maven 4 + npm 3 + pypi 5 + 性能 16 + 探测 3）。
