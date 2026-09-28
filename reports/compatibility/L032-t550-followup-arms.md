# L032 — T-550 随访五臂（T-536 Next 清单）+ L031 member sidecar A 面取证

- 日期：2026-09-28（R4 · iteration-1543 · T-550 / Linear BIN-32）
- 执行人：differential-qa-engineer
- 模式：**dual**（A 面全程可达，无降级；B 面 stock 二进制 community 许可层——conan 臂 B 实腿不可达，见 Arm 4）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ 192.168.120.38:8082（context root /artifactory；本轮版本复验沿 L030 认证基准）
- B 面（被测）：BinFlow stock 二进制 @ 127.0.0.1:18247（BINFLOW_HOME=/tmp/r4-bhome-t550 全新 scratch，`/binflow` 前缀；凭据经环境变量注入，未落盘；批末已停、home 已删）
- 框架：`tools/difftest/v2`（python3 stdlib-only），新 case 6 个；`_mavenlib` 新增 `start_forward_proxy`（A 面真实客户端腿的环境适配，见「框架侧修复」）
- 客户端：真实 `mvn deploy:deploy-file`（Apache Maven 3.9.16）、真实 `npm publish`（Arm 5 构造腿）、curl/runner stdlib HTTP 腿（断言读取）
- 证据：`tools/difftest/v2/run/l032-arms-r1/`、`run/l032-arms-r2/`、`run/l032-arms-r3/`（每 case `evidence/<case>/{a,b}-leg.json + summary.json`）
- 环境清理：两面 `GET /api/repositories` 过滤 `difftest-*` = `[]`（A/B 各核验一次）；B 进程已 kill、18247 已释放、scratch home 已删

## 轮次与稳定性

- r1 首跑暴露三处 **case 侧/框架侧** 问题（非产品差异），修正后重跑：①`rest_kcache_deploy_wording` 模块级 `POM_PATH` 格式串占位符数错（import 即炸，dry-run 门拦下）；②两处期望值按 live 实测钉正（missing-key repo GET = 400 而非 404；npm API 面 plain DELETE = 405）；③JVM 系统代理拦截（见下）。修毕 r1 重跑定形。
- r1→r2→r3：**三轮逐 case status 完全一致**；r2≡r3 **断言值逐项 0 差异**（`/tmp/r4-stab-t550.py` 对账脚本输出 `assertion-value diffs r2 vs r3: 0`）。结论以 r2/r3 为准。

### 框架侧修复（环境，非产品行为）

本机 clash 系统代理（127.0.0.1:7897）被 macOS JDK 启动时导入为 `http.proxyHost`，且 JDK 的 `nonProxyHosts` 匹配器不识别异常表里的 CIDR（`192.168.0.0/16`）形式——JVM HTTP 栈访问 192.168.x 全部被劫持且代理回 502（`mvn -version` + `-XshowSettings:properties`、`ProxySelector` 五行探针实证）。两层修复均落在 harness（`tools/difftest/v2/cases/_mavenlib.py`）：

1. `mvn_deploy_file` 按 `-Durl` 主机注入 `-Dhttp.nonProxyHosts=<host>|localhost|127.0.0.1`（命令行 -D 先占位，macOS 导入不再覆盖——`java.net.http` 探针 502→200 实证）。
2. wagon/Apache HttpClient 直连仍偶发 `NoHttpResponseException`（VPN utun4 路径上仅该栈受影响；pooling off / ttl1 / native transport / settings 报头均不救）——新增 `mavenlib.start_forward_proxy`：127.0.0.1 全方法透明转发（逐字 relay + 内存态 preemptive Basic，凭据不落盘不入 argv），A 面 mvn 腿经此构造（证据 `mvn_deploy_via=forward-proxy`），B 面直连。取证 GET 全部双面直连不经转发。

## Arm 1 — rest/nonempty-repo-delete-cascade-guard（L028 D-3）：PASS（双面姿势各自钉住）

构造：local `difftest-r4t550-a1`，PUT `com.diff:casc:1.0.0` pom（wire 协议腿），再 DELETE `/api/repositories/<key>`。

| 腿 | A 面（7.161.26 实测） | B 面（stock 实测） | 对照 |
|---|---|---|---|
| DELETE 非空仓 | **200** 静默级联；body `{"repoKey",…,"deletedArtifactsCount":6,"success":true}` | **400** `repository is not empty: "<key>" holds 5 node(s); retry with deleteContent=true` | **差异（已登记 D-3，维持待裁）** |
| DELETE ?deleteContent=true | n/a（已级联） | **200**（显式放行级联） | — |
| 善后 GET /api/repositories/<key> | **400** `{"errors":[{"status":400,"message":"Bad Request"}]}` | **400** 同形同文案 | **一致**（mini 发现：missing-key repo GET 双面都是 400 而非 404，探针核实含 never-existed 键；B 逐字复刻 A） |

- **四态：PASS**（期望按各面可达姿势非对称钉住：`a_delete_nonempty=200+cascade`、`b_delete_nonempty=400+deleteContent-hint`、`b_delete_forced=200`、双面 `repo_gone=status=400`）。PASS 语义 = 姿势稳定钉住，**非**裁定一致。
- **分类建议：UNKNOWN（维持）**——A 静默级联 vs B 确认门是产品语义分歧，终局裁定权在 compatibility-engineer；本臂证据（级联计数、提示文案、善后态）已可支撑裁定。

## Arm 2 — rest/kcache-deploy-404-wording：FAIL（B 违例；已登记 BUG live 确认 + **新增 remote 腿分歧**）

构造：`difftest-r4t550w-{loc,virt,rem}`（virt 无 defaultDeploymentRepo；remote url 仅占位）；GAV 一致 pom（`com.diff:wd:1.0.0`，路径与内容一致——排除 B 的 GAV 前置检查遮蔽路由判定，探针 2026-09-28 复核）。

| 腿 | A 面（live） | B 面（live） | 对照 |
|---|---|---|---|
| PUT `/<K>-cache/<gav>.pom` | **404** `Could not find a local repository named difftest-r4t550w-rem-cache to deploy to.`（§2.1 逐字） | **404** `Failed to find the repository 'difftest-r4t550w-rem-cache' specified in the request.` | **差异（已登记 BUG：状态同、措辞族不同——live 确认）** |
| PUT `/<K>/<gav>.pom`（remote 本体） | **404** `Could not find a local repository named difftest-r4t550w-rem to deploy to.` | **405** `Remote repository '<K>' is a read-only proxy cache; deployments to remote repositories are not accepted.` | **差异（本轮新增：状态+措辞双分歧）** |
| PUT `/<virt>/<gav>.pom` | **405** + `Allow: GET` + `No local repository was configured as local deployment repository for the (<virt>) virtual repository.`（§8.2 逐字） | **405** + `Allow: GET` + 同文案 | **一致**（405 关联臂维持对齐，无回归） |

- **四态：FAIL**（b_spec_violations：`put_kcache_wording`、`put_remote_status`、`put_remote_wording`；405 virt 腿全绿）。
- **分类建议：BUG（扩展同一登记项）**——A 两腿均落规格 §2.1 的 404 家族文案；B 的 `<K>-cache` 腿是已登记措辞 BUG 的 live 确认，remote 本体腿为本轮**新增**违例面（405+不同文案族），建议并入同一 ledger 项或立新 D-id，归 compatibility-engineer 裁定。
- 顺带观察（不立案）：check-order——GAV 不一致 pom 下 B 先答 409 GAV 错误、A 先走路由检查（探针 2026-09-28）；与 D-4 家族相关，留待后续批次。

## Arm 3 — maven/virtual-metadata-modulereleases-skip：NOT_RUN（前置未合入，如实）

- 前置探针（case `maven-module-handle-seat-probe`）：PUT remote `handleReleases=false` 后 GET 配置回显——**A 回显 false**（canonical 持久化席位）、**B 回显 true**（remote canonical 不持久化 handle*，`internal/remote/projection.go` 注释明示"席位落地之日 projection 镜像行值"）。
- **四态：BLOCKED/NOT_RUN**（reason 注明前置 `a=false, b=true`）——T-541（F8-widened handle* 半边）未合入本 worktree 树，模块级臂不猜测、不实现臂体；两席都变 false 之日探针即翻转，臂体随其专属票落地。
- 证据：`evidence/maven-module-handle-seat-probe/{a,b}-seat.json`（`cfg_keys_present` 双面都含 handleReleases/handleSnapshots 键名——B 键在、值恒 true）。

## Arm 4 — conan/v1-packages-delete-virtual-plain-404：PASS（A 实形钉住；B stock 门内不可达）

构造：conan local+virtual（A），`POST /api/conan/<virt>/v1/conans/hello/1.0/difftest/stable/packages/delete`（curl 等价 stdlib 腿）。

| 面 | 实测 | 说明 |
|---|---|---|
| A live | **400** + `errors[]` envelope：`Unsupported Conan v1 repository request for 'difftest-r4t550c-virt'`（Content-Type application/json） | **"A 臂实形未知"就此定案**：非 404、非 plain body |
| B stock | conan repo 创建即被许可门拒：400 `package type 'conan' is not available (license tier 'community' < 'pro')`；v1 面因 repo 不存在落 404 repo-lookup | stock 二进制无法自授许可（verifykey 无签发路径）——B 实腿不可达是**环境事实**，非产品差异 |

- **四态：PASS**（期望按双面各自可达姿势钉住：`a_v1pkgdel=400+Unsupported…`、`a_v1pkgdel_carrier=errors-envelope`、`b_conan_gate=400+license-gate`、`b_v1pkgdel=plane-unreachable(license-gate)`）。
- **裁定输入**：ledger 原提案（B plain→envelope-404 对齐）**仍会三重分歧**（status 400 vs 404、carrier envelope vs plain、措辞族）——A 实形已具备，待 compatibility-engineer 改判；licensed B 复测随许可可用后补。
- 适配注记（harness，非产品差异）：A 服务于 `/api/conan/<repo>/v1/...`，B 挂内容面 `/<repo>/v1/...`（apiProtocolMounts 无 conan 槽）——各腿打各自面的拼写。

## Arm 5 — rest/virtual-aggregate-delete-entry-arm：PASS（A 构造不可达如实 + B drift 臂钉住）

**A 面（构造取证，live）**：枚举五种 npm wire 流（local-only packument 合并、remote+local 混合合并、显式 `virtualRetrievalCachePeriodSecs=600`、经 virt 的 remote 源 tarball、经 virt 的 local 源 tarball；源仓经真实 npm publish 构造）——**`<virt>-cache` 聚合仓在 live A 上经任何 wire 流均不物化**（`/api/storage/<virt>-cache/**` 恒 404 `Unable to find item`，对照 `<rem>-cache/.npm` 200 列表）。无条目 DELETE（npm API 面 plain DELETE）= **405** `Method Not Allowed`（unpublish 需 `-rev` 形；§7.5 存储面 404 姿势已由既有 `maven-virtual-delete-passthrough` case 钉住）。成员仓不受影响（200）。
→ **WITH-ENTRY 双面对照 UNMEASURED（构造不可达，如实记录，不冒充）**。

**B 面（drift 行即 own storage，`internal/repo/service.go deleteVirtualOwnStorage`，generic 仓避开 maven layout 门）**：SQLite 种 drift 行（复用成员 pom blob）→ 虚拟 GET 不可见（404，**own-storage 行对虚拟 GET 不可见**——DELETE-only 可见性，本轮实测发现）→ **DELETE `/virt>/drift/agg-entry.bin` = 204** → 再 DELETE = 404 `Could not locate artifact. Path: '…'`（幂等 404）→ 成员仓 200 存活、行计数归零。

- **四态：PASS**（A 四项 + B 四项各自钉住；FAIL 才表示姿势漂移）。
- **分类建议**：无新分歧登记——A 面聚合仓 wire 不可达是规格 §6「TTL 物化」表述与 live 的落差（`virtualRetrievalCachePeriodSecs` 语义待 compatibility-engineer 复核），B drift 半边行为与 §7.5 own-storage 语义一致。

## Arm 6 — L031 member sidecar A 面取证（T-542 Next / R3 双审 B'①）：FAIL（B 派生契约姿势与 live A 三处分歧）

构造：每面 local maven（`snapshotVersionBehavior=unique`），真实 mvn deploy `com.diff:sd-lib:1.0.0-SNAPSHOT`（A 经 forward-proxy、B 直连；wire 字节等价），poll 至 version-level `maven-metadata.xml` 含 snapshotVersions，然后 17 维取证矩阵（java-agent UA `Java/1.8.0_391` / capable UA `Apache-Maven/3.9.16 (…)` 双面直连）。

| 维度（成员仓 maven-metadata.xml 及其 sidecar） | A 面（live 7.161.26） | B 面（T-542 派生契约实现） | 对照 |
|---|---|---|---|
| body java-agent：sv 剥除 / snapshot 块存活 | 0 条 / 存活 | 0 条 / 存活 | 一致 |
| body capable：sv 存在 | present | present | 一致 |
| body：Last-Modified / ETag 存在 | 均有 | 均有 | 一致 |
| body：IMS→304 / IMS 旧值→200 / INM→304 | 304 / 200 / 304 | 304 / 200 / 304 | 一致 |
| body：X-Checksum-* 头族 | md5+sha1+sha256 | md5+sha1+sha256 | 一致 |
| `.sha1`/`.md5`/`.sha256`（java-agent）= 所服务体的摘要 | match×3 | match×3 | **一致（派生内容契约正确）** |
| `.sha1` capable 控制腿 = capable 体摘要 | match | match | 一致 |
| `.sha512` | 404 | 404 | 一致 |
| **`.sha1`（java-agent）：Last-Modified 存在** | **present**（自身 LM，晚 body 数秒） | **absent** | **差异 ①** |
| **`.sha1`（java-agent）：If-Modified-Since→304** | **304** | **200**（无 LM 无从 304） | **差异 ②** |
| **`.sha1`（java-agent）：ETag/If-None-Match** | **no-etag**（A 不发 ETag） | **ETag=digest，INM→304** | **差异 ③** |

- **四态：FAIL**（b_spec_violations 3 项；14/17 维一致——含最核心的「sidecar=剥离体摘要」内容契约双面全对）。
- **裁定结论（B'① 之问：B 面姿势是否与 A 一致？——内容一致、缓存验证器族不一致）**：T-542 的派生契约在**无 A 面证据时猜了 ETag+INM、丢了 LM+IMS**；live A 对 sidecar 用 **LM+IMS、不发 ETag**。
- **分类建议：BUG（validator 族错位；根因在契约层）**——建议 compatibility-engineer 以本臂 A 面证据修订 T-542 派生契约（`.sha1/.md5/.sha256`：有自身 Last-Modified、无 ETag、IMS 谓词生效），B 侧修复面=writeDerivedSidecar（登记不修，本轮未动产品码）。
- 注：body 面（ETag+LM+双条件）A/B 全一致——T-542 修复的主体面维持，无回归。

## 汇总与状态机反馈

| 臂（ledger 项） | case | 四态 | 分类建议 |
|---|---|---|---|
| #① rest/nonempty-repo-delete-cascade-guard | rest-nonempty-repo-delete-cascade | PASS（姿势钉住） | UNKNOWN（维持待裁） |
| #② rest/kcache-deploy-404-wording | rest-kcache-deploy-404-wording | FAIL | BUG（kcache 措辞 live 确认 + remote 腿 405 新增） |
| #③ maven/virtual-metadata-modulereleases-skip | maven-module-handle-seat-probe | BLOCKED/NOT_RUN（前置） | —（席位探针=门） |
| #④ conan/v1-packages-delete-virtual-plain-404 | conan-v1-pkgdel-virtual | PASS（双面各自钉住） | A 实形已定，待裁（三重分歧维持） |
| #⑤ rest/virtual-aggregate-delete-entry-arm | npm-virtual-aggregate-delete-entry | PASS（A UNMEASURED 如实） | 无新分歧；§6 TTL 物化语义待复核 |
| L031 sidecar 取证 | maven-member-snapshot-sidecar-304 | FAIL | BUG（validator 族；契约修订提案） |

- **回归对照**（vs ledger / 上轮）：#① 仍在（无恶化）；#② 仍在 + **新增** remote 本体 405 腿；#③ 仍在（前置未合入，非恶化）；#④ 无变化（新增 A 实形证据）；#⑤ B 半边从代码引证升级为 live 实证，A 半边 UNMEASURED 维持；sidecar 面内容契约**fixed 确认**（digest-of-stripped-body 双面对齐）+ **新增** validator 族分歧 3 维。
- **状态机反馈**：无 IMPLEMENTED→VERIFIED 翻态建议（本轮全部为差异确认/取证）；建议**新增 1 条 ledger 项**（sidecar validator 族，证据即本臂矩阵）并**扩展 1 条**（kcache BUG 并入 remote 腿）。
- **提金候选**（A 面通过 case，交 compatibility-engineer 评审入 `docs/compatibility/golden/`）：①sidecar 17 维矩阵（A 腿，含 LM/ETag/条件 GET 实值）；②conan v1 400 envelope（status+carrier+措辞）；③deploy 拒绝族三腿 404/404/405 文案逐字；④非空仓级联 200 body 形。
- **层级索引**：L2（repo DELETE 善后）、L5（deploy 拒绝族 / conan v1 / sidecar 协议面）、L6（handle* 席位探针）、L7（virtual own-storage DELETE）。

## 复跑

```
bash /tmp/r4-run-t550.sh r1   # 内部: source /tmp/r3-difftest.env; A_BASE=…:8082/artifactory
                               # B_BASE=…:18247/binflow; DIFFTEST_B_DB=…binflow.db;
                               # runner.py --out run/l032-arms-rN --case <6 ids>
python3 tools/difftest/v2/runner.py --list   # 发现门（16 case 含新增 6）
```

机读产物：`tools/difftest/v2/run/l032-arms-r{1,2,3}/results.json`（schema `difftest/v2`；case id 与本报告臂号一一可对账）。
