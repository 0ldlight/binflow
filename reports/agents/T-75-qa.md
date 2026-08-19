# QA 报告 T-75 — remote/virtual 行为 + SSRF 安全矩阵（PRD §8 剧本 6~7/9）

- role: qa-engineer
- 日期: 2026-08-20
- 票据: T-75 [P0] QA：remote/virtual 行为 + SSRF 安全矩阵（AC 全文见 reports/agents/T-61.md T-75 节）
- 验收口径: docs/prd/milestone-3.md **v1.2**（FR-20/FR-21 全 AC、NFR-S13~S15、NFR-P14、FR-15-AC9、C3/C5/C4 定案、T-81 NAT64 勘误句）
- 被测对象: commit **a426256**（HEAD，T-75 派发点；= T-74 验收基线 + chore 提交），直接 `go build`（工作树干净，无需 worktree）→ `/tmp/t75/binflow-server`（19.6MB）
- 实例: `127.0.0.1:8080`（匿名开默认；`BINFLOW_ADMIN_PASSWORD` + `BINFLOW_REMOTE_CREDENTIALS_KEY`=32B 随机）；全程独占串行（T-74 后惯例）
- mock 上游: `127.0.0.1:9099`（计数 + Basic 401 + 302 族 + /hang 挂起，脚本存档见 §6）+ `127.0.0.1:9098`（**绑定本机全局 IPv6 GUA** `240e:341:...`——充当不可伪造的"公网首跳"，用于重定向 SSRF 拒绝的真机取证，离线可复现）
- 客户端: mvn 3.9.9 + Temurin JDK 21.0.12（/tmp/t67-tools）；npm 10.9.8（node 22.23.2）；pip 26.1.2 + twine 7.0.0（/tmp/t75/venv）；curl；python3 3.14.6
- 公网状态: 可达（httpbin.org / pypi.org / repo.maven.apache.org 均 200）——**M45 真实 Central 腿照跑**；M46/M47 仍走 mock 并注明（原因见 §3-M46/M47：npmjs packument 路径 M4 边界，本票实证复核）

## 总结论: **PASS**

- **场景组 25/25 应测全过 + 1 项按票面延后**（M55b 聚合浏览，P2，T-61 AC②/T-71 已批延后；当前面行为已记录，§3.2）
- 全程 5xx = **3 条且全部为设计内**（hardFail 仓 502×2 = FR-20-AC5③ 与 T-71 钉板透传、64MB 缓冲上限 502×1 = NFR-S13⑤）；其余状态码分布：200×132 / 404×28 / 201×25 / 400×23 / 405×6 / 401×4 / 304×2 / 204×1；`level:ERROR` 3 条 = 上述 3 条 502 的 access 记录，无其它 ERROR
- SSRF 拒绝 **19 条 WARN 审计行**全数取证（§4.1），零堆栈、零凭据泄漏；日志 grep `tpasswd|badpass|Authorization` = 0；DB `enc:v1:` ≥43 行、明文 0
- 自动化基线: `go vet ./...` 0 告警；`go test ./... -count=1` **15/16 包 ok**；1 FAIL = `docker` 包 `TestIdleSessionEviction` 已知时间性 flake（T-72/T-43 先例，与 remote/virtual 零交集），**定向复跑 `-count=2` ok 全绿**
- 无阻塞缺陷。产出 PRD 勘误建议 2 条 + 观察项 3 条（§5，均不阻塞、单点改动面）

## 1. AC 逐条结论

| # | AC（T-75） | 结果 | 证据摘要 |
|---|---|---|---|
| ① | remote M41~M48 全序（上游计数负断言贯穿；M42 全变体 + WARN 取证；M44 五段；M45 定案文案；M46/M47） | ✅ | M41 MISS→HIT 上游计数 2→2 冻结；M42 16 变体直连 400 + 3 式重定向拒绝 + NAT64/6to4/Teredo/zone 全拒 + DNS64 保留侧不拦 + 5 跳上限 + 19 WARN；M43 二次 miss 零上游；M44 五段逐条过（300s 真静默窗 + 期后自动恢复 +7）；M45 mock+真实 Central 双腿 + `.sha1/.md5/.sha256` 三后缀逐字文案；M46/M47 mock 腿 + 真实上游 M4 边界实证；M48 405+Allow / 204→回源 +1 / 未缓存 404 |
| ① | virtual M50~M55b（含 T-72 聚合项补验 + 优先桶/写路由/hardFail 透传钉板） | ✅ | M50 混合解析双头（Resolved-From×Cache）+ mvn 一次解析两类包；M51 声明序+交换即生效；AC9 priorityResolution 越位/复位即时；M52 405+C5 文案逐字+DELETE 405+mvn deploy 服务端 405；M53 路由落 maven-local 双 200；M54 npm/pip 本地+上游一次装齐；M55 maven versions 并集 + npm versions 并集；M55b P2 延后（票面允许）；AC7 stale 命中即成员结果/真 miss 继续、hardFail 502 透传（T-71 钉板）、T-72 上游计数 2→3 校准 + 负缓存 1800s 静默全实证 |
| ② | NFR-S13~S15 全项 + S14 不回显/日志 grep + FR-20-AC10 凭据对错 + NFR-P14 1GB | ✅ | S13 七点逐一（§4.1）；S14 静态加密/掩码/日志/审计四点 + FR-15-AC9 ①~④ 全过（fail-fast 双态 + 明文一次性迁移真机）；AC10 对→200 / 错→404 附 401 摘要；S15 health 全程 200 + socketTimeout 3.03s 快速降级；P14 1GB 流式 **RSS 增量 28KB**（限 256MB）+ sha256 逐位一致 |
| ③ | 日志取证与 mock 脚本存档；任何 5xx 打回附票号 | ✅ | §4 取证汇编 + §6 mock 脚本全文存档；3 条 502 全部设计内、无打回项 |

---

## 2. remote 域场景明细（M41~M48）

### M41 / M41b pull-through 与缓存冻结（FR-20-AC1/AC2）

| 步骤 | 证据 |
|---|---|
| 首取 | `GET /binflow/generic-remote/dir/up.bin` → 200 `X-Binflow-Cache: MISS`，sha256 `9a5f66c2…` == 上游文件实测；上游计数 +1（含预热 1→2） |
| 二取 + 3 次复取 | 全部 `HIT`，body cmp 一致；上游计数 **2 → 2 冻结**（负断言过） |
| M41b 速度 | 首取 0.158s vs 命中 0.030s（≥2x）+ 上游零访问（双判据均过） |
| M41b item info | `GET /api/storage/generic-remote/dir/up.bin` → `checksums:{sha1,md5,sha256}` 三摘要齐 |

### M42 SSRF 全变体（FR-20-AC3/AC12、NFR-S13）——16 直连变体全 400 + 3 式重定向拒绝

直连变体（建仓一律 200——校验在请求时，NFR-S13 口径；GET 一律 **400** E-01，message 含 `private or suppressed upstream` + 类别 + 目标）：

| 变体 | url | category（WARN 取证） |
|---|---|---|
| 回环 v4 | `http://127.0.0.1:9099` | loopback |
| RFC1918 ×3 | `10.1.2.3` / `172.16.0.9` / `192.168.1.9` | private_rfc1918 |
| 云 metadata | `http://169.254.169.254/latest` | link_local |
| 回环 v6 | `http://[::1]:9099/` | loopback |
| 未指定 ×2 | `http://0.0.0.0:9099/` / `http://[::]:9099/` | unspecified |
| ULA | `http://[fd00::1]:9099/` | private_ula |
| 链路本地 v6 | `http://[fe80::1]:9099/` | link_local |
| **NAT64 包私网**（T-65/T-81 修复面） | `http://[64:ff9b::7f00:1]:9099/`（内嵌 127.0.0.1） | loopback（拆解递归命中内嵌地址类别） |
| **6to4 包私网** | `http://[2002:7f00:1::]:9099/`（内嵌 127.0.0.1） | loopback（同上递归） |
| **Teredo** | `http://[2001:0:…:8a2e:3707]:9099/` | teredo（直拒，不拆解——混淆内嵌无可信拆法） |
| **IPv4-compatible** | `http://[::127.0.0.1]:9099/` | loopback（::a.b.c.d 收编拆解） |
| **v4-mapped** | `http://[::ffff:127.0.0.1]:9099/` | loopback（折叠为 v4 后过表） |
| **zone 标识** | `http://[::1%25lo0]:9099/`（URL 形 `%25`） | loopback（zone 剥离后判定） |

**DNS64 保留侧**（包裹公网 v4 必须放行）：`http://[64:ff9b::808:808]:9099/`（内嵌 8.8.8.8）与 `http://[2002:808:808::]:9099/` → **404**（assumed offline：本机无 NAT64/6to4 网关，拨号失败按故障降级），**非 400**、ssrf-guard WARN 计数不变（16→16）——清单没有误伤合法 DNS64 上游。

**file:// / gopher://**：建仓即 **400**（`scheme must be http or https`，FR-15-AC3）。

**重定向矩阵（FR-20-AC12，公网首跳真机取证）**：首跳 = 本机 GUA `http://[240e:341:…]:9098`（非豁免仓——GUA 不在清单，首跳合法；二跳 302 目标由 mock 控制）：

| 场景 | 302 Location | 结果 |
|---|---|---|
| 绝对 URL 式 | `http://169.254.169.254/latest/meta-data/` | **400** `hop 1: …link_local address 169.254.169.254` + WARN（首跳 mock log +1 证明真实经过了一跳） |
| scheme 相对式（"302 两式"） | `//169.254.169.254/latest/meta-data/` | **400** 同上 |
| 非 http scheme 跳转 | `gopher://127.0.0.1:704/x` | **400** `scheme_not_http` + WARN |
| 公网同源链 | `/redir/chain1→chain2→chain3→/dir/up.bin`（3 跳） | **200** 跟随成功，hop 计数恰 4（3 跳 + 终资源），body == up.bin |
| 无限自环 | `/redir/loop`→自身 | **恰 5 跳后停止**（hop 计 6），按上游故障降级 404 + offline 窗（NFR-S13④ 上限 5 生效） |

（旁注：曾试 httpbin.org 作公网首跳，其 `/redirect-to` 端点自身间歇 500——BinFlow 按上游 5xx 正确开静默窗，行为无误；改用本机 GUA mock 后完全确定性。）

### M43 负缓存（FR-20-AC4）

`no-such.bin` 首取 404（上游 +1）→ 二取 404（**上游计数不变**，missedRetrievalCachePeriodSecs=1800 窗内零上游流量）。

### M44 上游故障降级五段（FR-20-AC5，v1.1 口径；TTL=5s 仓触发过期回源）

| 段 | 断言 | 实测 |
|---|---|---|
| ① 已缓存（过期）路径 | 200 旧内容 + `X-Binflow-Upstream-Error` | 200 + `X-Binflow-Cache: STALE` + `X-Binflow-Upstream-Error: upstream unavailable: …connection refused`，body cmp == 上游字节 |
| ② 未缓存路径（默认 hardFail:false） | 404 + offline 文案 | 404 `…is assumed offline (no cached copy; retry later).` |
| ③ hardFail:true 对照仓 | 502 | 502 `…(hardFail enabled): upstream unavailable…` |
| ④ 静默期 300s 零上游 + 期后自动恢复 | 窗内（上游已恢复）请求零上游流量；窗过后自动回源 | 杀上游→①②③→**窗内恢复上游**→探针 404 用时 29ms、上游 log 零新增（T-66 留给本票的真机 300s 时序：窗口 05:52 开启，05:58 后探针 **200 MISS**、上游 `up3.bin` +1、内容 cmp 一致） |
| ⑤ health 全程 200 | 故障隔离 | 停机中/恢复后 `api/v1/health` 均 `status: ok` |

### M45 maven 代理（FR-20-AC6/AC13）——mock + 真实 Central 双腿

- **mock 腿**（计数断言）：mvn `dependency:resolve`（pom `<repositories>` 指向 BinFlow maven-remote，驱动形式见勘误 E1）→ BUILD SUCCESS，junit pom+jar 经 BinFlow 落地 `repoA`，**jar 字节 == `MOCK-JUNIT-JAR-BYTES`**（mvn→BinFlow→mock 全链溯源），mock junit 计数 0→2；**全新本地 repo 二次 resolve 零上游**（2→2）、jar cmp 一致。
- **真实 Central 腿**（公网可用，按 PRD 优先口径）：maven-remote-central（`https://repo.maven.apache.org/maven2`，无豁免）→ mvn resolve 真实 junit 4.13.2 **含传递依赖 hamcrest-core 1.3/hamcrest-parent 1.3 全部经 BinFlow**（5 个制品，access log 逐路径取证），jar 真实（PK 头），仓内已缓存（直连 GET 200）。
- **checksum 后缀定案断言**（FR-20-AC13/ME-03）：maven-remote `.jar.sha1/.md5/.sha256` 三后缀与 generic `up.bin.sha1` → 全部 **404** 且 message **逐字** `Checksums are not downloadable.`；上游 `.sha1` 类请求计数 0→0（不回源）；mvn 全链不受影响（两条腿均 BUILD SUCCESS，Maven 3.9 resolver 对缺失 checksum 静默容忍）。
- item info：缓存 node `size:20` + sha256 三摘要在场。

### M46 npm 代理（FR-20-AC7，P1）——mock 腿（真实上游边界实证见下）

packument 经 BinFlow：`dist.tarball` **已重写**为 `http://127.0.0.1:8080/binflow/api/npm/npm-remote/up-pkg/-/up-pkg-1.0.0.tgz`（原上游 9099 不回显）；`npm install up-pkg --registry …/api/npm/npm-remote/` → exit 0 `added 1 package`，`node -e require('up-pkg')` 可执行；`npm view` 1.0.0；上游恰 2 次（packument + tarball）。
**真实 npmjs 边界（本票实证）**：`npm-remote-real → registry.npmjs.org` GET lodash → 404 `Package 'lodash' not found`——npmjs 的 packument 不在 BinFlow/Artifactory 布局路径 `<name>/packument.json`（T-72 已记录的 M4 边界，此处真机复核成立）→ **M46 走 mock 并注明**，符合票面"公网不可用（或布局不兼容）走 mock"缝。

### M47 pip 代理（FR-20-AC8，P1）——mock 腿 + 真实 pypi.org 意外可用（观察 O3）

mock 腿：simple 页 href 重写为 BinFlow `…/packages/packages/six/six-1.16.0-py3-none-any.whl#sha256=fc190f93…`（fragment 保留、上游 `packages/` 前缀保留——T-72 设计形）；`pip install six` exit 0（dist-info 落地）；二次全新 target 安装**零上游**（计数 2→2 冻结）；首次恰 2 次（simple + wheel）。
**真实 pypi.org（观察项 O3）**：`pypi-remote-real → pypi.org` simple/six 页 **200**（真实条目 + sha256 fragment），下载探针 **200**——pypi.org 的 `/packages/…` 会 302 到 files.pythonhosted.org（公网主机），BinFlow 按每跳重过链后**跟随成功**。即真实 PyPI 代理在本构建上**实际可用**（优于 T-72 边界注记的预期）；主断言仍按 mock 计数口径出证。

### M48 remote 写拒绝 + 删缓存回源（FR-20-AC9 / RE-05/RE-06）

PUT → **405 + `Allow: GET`**（`Remote repository 'generic-remote' is a read-only proxy cache; …`）；POST → 405；DELETE 已缓存 → **204** → 再 GET **200 MISS** 上游 +1 内容一致；DELETE 未缓存路径 → **404**（幂等，E-14 语义）。

---

## 3. virtual 域场景明细（M50~M55b + 钉板项）

### 3.1 两桶解析与写路由

| 场景 | 结果 | 证据 |
|---|---|---|
| M50a 本地成员 | ✅ | `maven-virtual/com/acme/demo-app/1.0.0/…jar` → 200，`X-Binflow-Resolved-From: maven-local`，body == mvn deploy 产物（sha 237d22a1…） |
| M50b remote 成员 | ✅ | `…/junit/junit/4.13.2/junit-4.13.2.jar` → 200，`Resolved-From: maven-remote` + `Cache: HIT`（成员缓存复用，上游 junit 计数 2→2 零新增） |
| M50c 收口门槛 | ✅ | mvn 经**同一个** maven-virtual 一次解析 demo-app（本地）+ junit（上游）4 制品全过 BUILD SUCCESS（PRD「代理收口」门槛成立） |
| M51 声明序 | ✅ | 同 GAV `ord`：[local,remote] → 内容 A（sha 2784dfde…，Resolved-From maven-local）；UpdateRepo 交换为 [remote,local] → **下一次 GET 即**内容 B（sha b377ce41…，Resolved-From maven-remote，上游 +1）——成员变更逐请求现算 |
| M51 变体（AC9 优先桶） | ✅ | 后声明成员 maven-local 标 `priorityResolution:true` → GET 得 **A**（优先桶整体前置于声明序）；复位 false → 回 **B**；两向即时生效 |
| M52 写拒绝 | ✅ | PUT → 405 + `Allow: GET` + **C5 文案逐字相等**（`No local repository was configured as local deployment repository for the (maven-virtual) virtual repository.`）；DELETE → 405（Allow: GET）；mvn deploy 指向 virtual（带凭据）→ 客户端 exit 1 + 服务端 access log `PUT /binflow/maven-virtual… status:405` |
| M53 写路由 | ✅ | 配 `defaultDeploymentRepo: maven-local` 后 mvn deploy 1.1.0 经 virtual → **exit 0**；virtual GET 200（Resolved-From maven-local）+ **成员实落** `GET /binflow/maven-local/…1.1.0.jar` 200 |
| AC7 stale/真 miss（FR-21-AC7，C3 定案） | ✅ | TTL=5s remote 成员：新鲜取 ST3-V1（MISS）；过期 + 上游文件删除 → GET 得 **STALE 旧副本**（`Resolved-From: stale-remote` + `Cache: STALE` + `X-Binflow-Upstream-Error: upstream 404 (expired copy served)`，**未**落到持 ST3-LOCAL 的后位成员）；对照：stale 服务同请求写入的负缓存行使**下一请求**成为真 miss → 落 maven-local（Resolved-From: maven-local）——T-66/T-71 钉板的组合行为在真机复现 |
| T-71 钉板：hardFail 成员透传 | ✅ | virtual-hf [maven-local, hf-dead-remote(死端口, hardFail)] 未缓存 GAV → **502 原样透传**（`…(hardFail enabled): upstream unavailable…`），未被后位成员掩蔽、不 404 |
| 探索性 miss 无痕（ADR-0013） | ✅（间接） | M50c/T-72 校准的计数行为与 T-71 单测一致；本票未单独构造（单测 `TestVirtualExploratoryMissLeavesNoNegativeRow` 已钉，真机负缓存静默证据见下） |

### 3.2 聚合（T-72 补验）

| 场景 | 结果 | 证据 |
|---|---|---|
| M54 npm | ✅ | `npm install demo-pkg up-pkg --registry …/api/npm/npm-virtual/` → exit 0 `added 2 packages`，本地包（npm-local publish 真发布）+ 上游包（npm-remote 代理）一次装齐，双包 require 可执行 |
| M54 pip | ✅ | twine 上传 demo-pkg wheel+sdist 至 pypi-local → `pip install --index-url …/pypi-virtual/simple demo-pkg six` exit 0，两个 dist-info 俱在 |
| M55 maven 合并 | ✅ | 成员1（local）lib 1.0.0 + 成员2（remote mock）lib 1.1.0 + 其 maven-metadata.xml → `GET maven-virtual/com/acme/lib/maven-metadata.xml` versions=**[1.0.0,1.1.0]**、latest/release=1.1.0、lastUpdated=成员最大值；旁车=合并体摘要（200）。（注：首取恰逢 T-68 版本组计算异步窗（O2），异步落地后并集正确） |
| M55 npm 合并 | ✅ | npm-local merge-pkg 1.0.0 + 上游 merge-pkg 1.1.0 → `npm view merge-pkg versions --json` = `["1.0.0","1.1.0"]` |
| **T-72 校准点①（上游计数 2→3）** | ✅ | M54 安装序列上游恰 **+1 = 本地-only 包 `demo-pkg/packument.json` 的 miss 探测**（up-pkg 的 packument/tarball 已在成员缓存）；与 T-72 `virtual_render_test` 2→3 同构 |
| **T-72 校准点②（本地-only 包负缓存 1800s）** | ✅ | 探测后再 `npm view demo-pkg`（virtual）→ demo-pkg 上游探测计数 **1→1 不变**（成员负缓存行静默期内不再打上游）；pip 侧同构（`simple/demo-pkg/` 1 次后静默） |
| M55b / RE-09 聚合浏览 | **N/A（P2 票面延后）** | T-61 AC②「P2 聚合浏览 M55b 可延后」+ T-71 已登记；当前实测 `GET /api/storage/maven-virtual/com/acme/lib` → 200 单 item 形态（无聚合 children 字段），与延后状态一致 |

---

## 4. NFR 取证

### 4.1 NFR-S13（SSRF 七点）

| 点 | 断言 | 证据 |
|---|---|---|
| ① scheme | 建 400 + 请求时断言 | file/gopher 建 400；302→gopher 跳转 400 `scheme_not_http` |
| ② 全 IP 清单 + 过渡格式 | 16 直连变体全 400；NAT64/6to4 拆解递归、Teredo 直拒、v4compat 收编、zone 剥离、v4-mapped 折叠；豁免仓（allowPrivateUpstream）放行（M41~M48 全部功能面即豁免腿） | §2-M42 表 + §4.1 末 19 行 WARN 全录 |
| ③ DNS rebinding（拨号钉 IP + Control 二次校验） | 单测面（T-65 双 review 钉板）；真机可观察面 = 直连拒绝发生在 `phase:"check"`（预连接零外发包） | WARN 行 phase 字段 |
| ④ 重定向每跳重过链 + 上限 5 | 3 式重定向全拒（hop 1）、公网同源 3 跳跟随成功、自环恰 5 跳停止 | §2-M42 重定向表 |
| ⑤ 缓冲型 64MB 上限 → 502 | npm packument 类 65MB（declared 68157440）→ **502** `upstream response exceeds buffered body limit`，30ms 内快速失败 | `npm-remote-bulk/bulkpkg` |
| ⑥ socketTimeout 按仓 | socketTimeoutSecs=3 仓 vs /hang（永不响应）→ 404 offline 用时 **3.033s**（ResponseHeaderTimeout 生效、超时不重试） | generic-remote-slow |
| ⑦ WARN 结构化零堆栈 | 19 条 `remote: outbound target rejected (ssrf-guard)`，字段 repo/target/category/phase/ip，无堆栈 | §4.1 下方全录 |

19 条 WARN 全文（取证存档，含类别与 phase）：

```
ssrf-loopback   127.0.0.1:9099                loopback         check  ip=127.0.0.1
ssrf-priv10     10.1.2.3:9099                 private_rfc1918  check  ip=10.1.2.3
ssrf-priv172    172.16.0.9:9099               private_rfc1918  check  ip=172.16.0.9
ssrf-priv192    192.168.1.9:9099              private_rfc1918  check  ip=192.168.1.9
ssrf-meta       169.254.169.254               link_local       check  ip=169.254.169.254
ssrf-v6loop     [::1]:9099                    loopback         check  ip=::1
ssrf-unspec     0.0.0.0:9099                  unspecified      check  ip=0.0.0.0
ssrf-v6unspec   [::]:9099                     unspecified      check  ip=::
ssrf-v6ula      [fd00::1]:9099                private_ula      check  ip=fd00::1
ssrf-v6link     [fe80::1]:9099                link_local       check  ip=fe80::1
ssrf-nat64-priv [64:ff9b::7f00:1]:9099        loopback         check  ip=64:ff9b::7f00:1   ← NAT64 拆解递归
ssrf-6to4-priv  [2002:7f00:1::]:9099          loopback         check  ip=2002:7f00:1::     ← 6to4 拆解递归
ssrf-teredo     [2001:0:…:8a2e:3707]:9099     teredo           check                      ← 直拒
ssrf-v4compat   [::127.0.0.1]:9099            loopback         check  ip=::7f00:1
ssrf-v4mapped   [::ffff:127.0.0.1]:9099       loopback         check  ip=127.0.0.1
ssrf-zone       [::1%lo0]:9099                loopback         check  ip=::1%lo0           ← zone 剥离
redir-pub6      169.254.169.254               link_local       check                      ← 302 绝对式 hop1
redir-pub6      169.254.169.254               link_local       check                      ← 302 scheme 相对式 hop1
redir-pub6      gopher://127.0.0.1:704/x      scheme_not_http  check                      ← 跳转 scheme 断言
```

### 4.2 NFR-S14 + FR-15-AC9 + FR-20-AC10（凭据链）

| 断言 | 实测 |
|---|---|
| AC10 正确凭据 | cred-remote（h/tpasswd，AES-GCM 入库）GET `protected/secret.bin` → **200** 内容逐位一致 |
| AC10 错误凭据 | cred-remote-wrong 同路径 → **404** + `(upstream answered 401 401 Unauthorized; credentials refused or insufficient)`（401 视为 unfound + 摘要，v1.1 定案；上游计数 +1 证明确实尝试、不负缓存不锁 offline——后续可热修） |
| S14③ config 不回显 | GET repositories/cred-remote：`username:"h"` 在、**password 缺省**、整个响应体 grep `tpasswd` = false |
| S14④ 日志无凭据 | serve.log（三代）grep `tpasswd\|badpass\|"Authorization"` = **0** |
| S14①/AC9① 静态加密 | DB+WAL grep：`enc:v1:` ≥43 行、明文口令 **0** 行 |
| S14⑤ 放行审计 | audit_events：`repo.create|generic-remote|{"allowPrivateUpstream":true,…}`、`ssrf-loopback|…|false` 等在库（含 update 的 from→to 形态，32 行携带该字段） |
| AC9② 无钥 fail-fast | 停服 → 不带 `BINFLOW_REMOTE_CREDENTIALS_KEY` 重启 → 进程退出，`panic: …remote repository "cred-remote" stores encrypted credentials but no master key is configured: set BINFLOW_REMOTE_CREDENTIALS_KEY (base64 of exactly 32 bytes) and restart` |
| AC9③ 重设钥重启 | healthz 200 + cred-remote Basic 链 200（内容一致）；**缓存跨重启直接 HIT**（冷启动不回退） |
| AC9④ 存量明文迁移 | 停服直写 DB `password='legacy-plain-pw'` 模拟升级 → 带钥启动 → 行变 `enc:v1:JjUmvOHtG…`、明文行 **0**、healthz 200、错误凭据行为不变（401→404 附摘要） |

### 4.3 NFR-S15 / NFR-P14

- S15：M44 五段全过（见 §2）；health 全程 200；挂起上游 3.03s 快速降级不拖垮服务。
- **P14**：`generic-remote-big` 代理 **1GiB**（/dev/zero 全零文件）——响应 sha256 `49bc20df…` == 上游实测逐位一致，服务进程 RSS 基线 152088KB → 采样峰值 152116KB，**增量 28KB**（限 256MB，余量 4 个数量级；与 T-66 单测 200MB 上限口径同向，真机更优）。

---

## 5. PRD 勘误建议与观察项（转 conductor，均不阻塞）

| # | 条目 | 发现 | 建议 |
|---|---|---|---|
| E1 | §5.4 M45 命令 | `mvn dependency:get -DremoteRepositories=…` 在 **mvn 3.9.9 捆绑的 maven-dependency-plugin 3.7.0 上不生效**（plugin.xml 中该参数 `property: None`，plugin 3.6.0 同样不覆盖 session central——实证 `_remote.repositories` 记 `>central=`、BinFlow 零请求） | M45 驱动形式改为 pom `<repositories>` + `dependency:resolve`（本报告已按此双验证收），或注明需 plugin `<configuration>` 内嵌（String 型） |
| E2 | §5.4 M42 日志取证命令 | PRD 的 grep/journalctl 组合命令与实际日志形态不符（实际为 JSON slog 到 stdout） | 改为 `grep 'ssrf-guard' <serve.log>` + 字段说明（本报告 §4.1 即取证形态） |
| O1 | M44 观测 | 重定向自环的 5 跳上限停止按「上游故障」降级（404 + offline 窗），非 400 | 与 PRD「上限 5 跳」无码值约定冲突，行为合理（故障 vs 拒绝二分一致）；建议 PRD 注记一句即可 |
| O2 | M55 观测 | 版本组 metadata 的计算为**异步触发**（release pom 属异步类）：POM PUT 后紧接的 virtual 合并请求可能短暂只见 remote 成员版本（异步落地后并集正确） | 设计内行为（T-68 触发分类）；若要消除窗口，需 pom 触发改同步或聚合面读计算值——M4 评估，非缺陷 |
| O3 | M47 观察项 | 真实 pypi.org 代理**实际可用**（simple 200 + 下载 200：pypi.org `/packages/…` 302 → files.pythonhosted.org，公网跳转每跳过链后跟随成功）——优于 T-72 边界注记 | 建议 T-77 文档可提「PyPI 真上游可用（经跳转）」；npmjs 的 packument 路径边界经本票真机复核**确认存在**（404 `Package 'lodash' not found`），M4 前 npm 真上游不可代理的结论维持 |

另：RE-11 `/api/v1/remote/stats` REST 端点 404——T-66 AC③ 批准的 P2 延后（`Engine.Stats` 数据面已就位），随票记录不阻塞。

## 6. mock 上游脚本存档（AC③；离线可复现）

```python
#!/usr/bin/env python3
"""T-75 mock upstream: counting file server + Basic-auth + redirect + hang endpoints.
Serves /tmp/t75/upstream as docroot; appends "<METHOD> <path>" per request to
/tmp/t75/upstream.log (append mode — hit-count negative assertions survive
kill/restore cycles). Bind forms:
  python3 mock_upstream.py 127.0.0.1 9099          # loopback mock (exempted repos)
  python3 mock_upstream.py <global-v6-GUA> 9098 -v6 # "public" first hop (no exemption;
                                                    # redirect-hop SSRF denial取证)
"""
import base64, os, sys, time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

ROOT, LOG = "/tmp/t75/upstream", "/tmp/t75/upstream.log"
CRED = base64.b64encode(b"h:tpasswd").decode()

class Handler(SimpleHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_one(self):
        with open(LOG, "a") as f: f.write("%s %s\n" % (self.command, self.path))
    def log_message(self, fmt, *args): pass
    def do_GET(self):
        self.log_one(); path = self.path.split("?")[0]
        def redir(loc):
            self.send_response(302); self.send_header("Location", loc)
            self.send_header("Content-Length", "0"); self.end_headers()
        if path == "/redir/meta-abs": return redir("http://169.254.169.254/latest/meta-data/")
        if path == "/redir/meta-rel": return redir("//169.254.169.254/latest/meta-data/")
        if path == "/redir/gopher":   return redir("gopher://127.0.0.1:704/x")
        if path == "/redir/chain1":   return redir("/redir/chain2")
        if path == "/redir/chain2":   return redir("/redir/chain3")
        if path == "/redir/chain3":   return redir("/dir/up.bin")
        if path == "/redir/loop":     return redir("/redir/loop")
        if path.startswith("/hang"):
            time.sleep(300); self.send_response(200); self.end_headers(); return
        if path.startswith("/protected/"):
            if self.headers.get("Authorization", "") != "Basic " + CRED:
                body = b"unauthorized\n"
                self.send_response(401); self.send_header("WWW-Authenticate", 'Basic realm="mock"')
                self.send_header("Content-Length", str(len(body))); self.end_headers()
                self.wfile.write(body); return
        super().do_GET()
    def do_HEAD(self):
        self.log_one(); super().do_HEAD()

class V6HTTPServer(ThreadingHTTPServer):
    address_family = __import__("socket").AF_INET6

if __name__ == "__main__":
    os.chdir(ROOT)
    bind = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 9099
    suffix = sys.argv[3] if len(sys.argv) > 3 else ""
    if suffix:
        Handler.log_one = (lambda s: lambda self: open(LOG+s,"a").write(
            "%s %s\n" % (self.command, self.path)))(suffix)
    V6HTTPServer((bind, port), Handler).serve_forever()
```

上游布局（docroot）：`dir/{up,up2,up3}.bin`、`junit/junit/4.13.2/{pom,jar}`（无依赖 pom）、`com/acme/{ord 1.0.0 jar, lib maven-metadata.xml + 1.1.0}`、`protected/secret.bin`、`up-pkg/{packument.json, -/up-pkg-1.0.0.tgz}`（真 npm pack 产物 + sha1/integrity 匹配）、`merge-pkg/packument.json`、`simple/six/index.html`（href 指向 `packages/six/…whl#sha256=…`，whl 为真构建产物）、`bulkpkg/packument.json`（65MB 随机）、`big/big.bin`（1GiB）。

计数日志（终态）：`upstream.log` 43 行 / `upstream.log-v6` 18 行（首跳接触证据）。

## 7. 清理

- 实例 A、mock 9099、mock6 9098、RSS 采样器已全部停止；/tmp/t75（二进制、数据目录、venv、npm/mvn/pip 工作区、1GB+65MB 样本、日志）已删除；端口 8080/9099/9098 无监听残留；无任何凭据写入仓库文件（credkey 仅存在于已删除的 /tmp/t75）。
