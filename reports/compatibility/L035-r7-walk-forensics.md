# L035 — T-562 stage 1：plain-SNAPSHOT→时间戳 resolve walk 面双端活体取证 + auto-materialize 副探（BIN-44）

- 日期：2026-09-29（R7 · T-562 stage 1 / Linear BIN-44）
- 执行人：differential-qa-engineer
- 模式：**dual**（A 面全程可达，无降级）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ http://192.168.120.38:8082/artifactory（批首 `api/system/version` 活体复验=7.161.26，revision 86126900）
- B 面（被测）：worktree HEAD **0c17635a** 自建 `go build ./cmd/binflow-server` scratch @ 127.0.0.1:18080（`/binflow` 前缀；BINFLOW_HOME=/tmp/binflow-r7-walk，数据目录 /tmp/binflow-r7-walk/data 留存备复查；凭据 env 注入零落盘；批末实例已停、18080 已释放）
- 框架：`tools/difftest/v2`（python3 stdlib-only）；新增 case 5：`maven-walk-trigger-matrix` / `maven-walk-selection-tiebreak` / `maven-walk-sameface-mvn` / `maven-auto-materialize-probe` / `maven-virtual-walk-selection`
- 证据：`tools/difftest/v2/run/l035-r7-r{2,3}/`（主批两轮）+ `run/l035-r7-r{4,5}/`（virtual selection 两轮）+ `run/l035-r7-r1/`（shakedown，tiebreak case 侧 bug 轮，仅 materialize/trigger/sameface 腿有效）；每 case `evidence/<case>/{a,b}-leg.json + summary.json`
- 复跑入口：`bash /tmp/l035-run.sh rN [case …]`（case 集内置；无凭据）
- 性质：**取证票**（dual-oracle，expected=live A；walk resolve 维的 a≠b 是登记中 fence 面 T-562/BIN-44 本体，非新面孔、非批内回归）；裁定权归 conductor / compatibility-engineer

## 轮次与稳定性

| case | r2 | r3 | r2≡r3 断言值 | 有效轮说明 |
|---|---|---|---|---|
| maven-walk-trigger-matrix | FAIL* | FAIL* | 双侧逐项相同 | r1 起有效（r1≡r2≡r3） |
| maven-walk-selection-tiebreak | FAIL* | FAIL* | 双侧逐项相同 | **r2 起有效**（r1 tie 四臂 case 侧路径 bug 409——version 目录缺失，A 的 layout-GAV 门正确拒绝；r1v 验证修复后 r2/r3 为有效两轮） |
| maven-walk-sameface-mvn | FAIL* | FAIL* | 双侧逐项相同 | r1 起有效（r1≡r2≡r3） |
| maven-auto-materialize-probe | **PASS** | **PASS** | 双侧逐项相同 | r1 起有效（r1≡r2≡r3） |
| maven-virtual-walk-selection | FAIL* | FAIL*（r4/r5） | 双侧逐项相同 | **r4/r5 为有效两轮**（首版 r4/r5 作废：scenario 复用成员仓致目录污染、served body 不再标识赢家——case 侧 bug，当轮修复重跑） |

\* FAIL=取证腿预期分歧（B 侧 resolve 全 404 = 登记中 fence 面，喂 stage 2 规格），非回归。

汇总：case 级 **PASS 1 / FAIL 4（均为预期分歧取证腿）/ BLOCKED 0**；稳定性门全部满足（有效两轮断言值逐项相同）。

---

## Part 1 — walk 族取证（目标 1-4）

### W1 触发条件全貌（case maven-walk-trigger-matrix；A oracle r1≡r2≡r3）

构造：unique 家园（`snapshotVersionBehavior=unique`）wire PUT plain pom + plain jar（双端均 201、Location=时间戳改写拼写 `…-20260929.053709-1.*`——改写面双端同，F1 复证；A 改写 ts 取 **UTC**〔053709 vs 本地 13:37〕）。

| # | 请求形态 | A 面（live） | B 面（live） | 四态 | 备注 |
|---|---|---|---|---|---|
| t1 | GET plain `.pom` | **200**，服务改写版字节（w1pom） | 404 | 分歧（fence） | F2 复证 |
| t2 | HEAD plain `.pom` | **200**，Content-Length=280（=改写版长度）、ETag=改写版 sha1 | 404 | 分歧（fence） | **HEAD 同走 walk**，验证器（ETag/CL）=目标文件的 |
| t3 | GET plain `.jar` | **200** 服务改写 jar | 404 | 分歧（fence） | jar 扩展同走 |
| t4 | GET plain `.pom` + `Range: bytes=0-15` | **206**，len=16，body 起始 `<?xml version="1` | 404 | 分歧（fence） | **Range 请求在 resolve 后的实体上切片**（非 200 全量） |
| t5 | GET plain `.pom.sha1` | **200**，body=**解析目标的 sha1**（bc33934…=t1 服务体摘要） | 404 | 分歧（fence） | checksum 旁车同样 walk——旁车 GET 返回 resolve 后实体的摘要 |
| t6 | GET plain `.pom.md5` | **200**=目标 md5 | 404 | 分歧（fence） | 同上 |
| t7 | GET version 级 maven-metadata.xml（plain 目录路径） | 200 | **200** | **一致** | F5 复证；元数据文档本身不走 walk（直读） |
| t8 | GET **非 SNAPSHOT** plain 拼写（`gt8-3.0.pom`）于 SNAPSHOT 目录（目录有 ts 候选） | **404** | **400**（layout parse 门：`file name "gt8-3.0.pom" does not spell the version as "3.0-SNAPSHOT" or a timestamped…`） | 分歧（**新亚面**） | walk 门=请求文件名必须携带 `-SNAPSHOT` 拼写；B 在 parse 层 400 拒绝（A 是 honest 404）——stage 2 需同时定 parse 门次序 |
| t9 | GET plain `.pom`，目录仅有 `.jar` ts 候选 | **404** | 404 | **一致** | walk 按扩展族匹配，无跨扩展兜底 |
| t10 | HEAD plain `.pom` 经 virtual（单成员） | **200** len=280 | 404 | 分歧（fence） | virtual 面同走（F6 复证） |

**W1 结论（A oracle，三轮稳定）**：触发 = 请求路径文件名为 plain `-SNAPSHOT` 拼写 + 家园 unique（改写发生）+ 同目录存在同 (artifact, baseRev, 扩展族〔含 classifier〕) 的时间戳拼写候选。GET/HEAD/Range/checksum 旁车四形态全部走 resolve，且验证器/摘要/切片全部针对 resolve 后的实体。version 级 metadata 文档与 GAV 中间目录不涉及（walk 只发生在末端文件名层——目录拼写双端一致，L034 F1' 目录实形证据 + 本次 t8 反证）。

### W2 多版本选择规则精确定义（case maven-walk-selection-tiebreak；r2≡r3，全 PUT 201）

| # | 场景（同 version 目录候选集） | A 服务 | 规格含义 |
|---|---|---|---|
| s1 | 上传序=〔新 ts 20260601 先、旧 ts 20260101 后〕 | **s1-newts** | **选择键=文件名内嵌时间戳，非上传 mtime/上传序**（F4 的隔离定谳；mtime 模型被 s1+s2b 双杀） |
| s2 | 同 ts 20260501，bn 1→2 上传 | **s2-bn2** | ts 并列→取 bn 大者 |
| s2b | 同 ts，bn 2→1 上传（逆序） | **s2b-bn2** | **tie-break=buildNumber 本身，与上传序无关** |
| s3 | 同 ts 20260502，bn 9 vs 10 | **s3-bn10** | **bn 数值比较**（词典序会选 "9"——被否定） |
| s4 | 目录含新 ts jar + 旧 ts pom | pom GET→**旧 ts pom**；jar GET→**新 ts jar** | 按 (artifact, baseRev, **extension**) 族内独立选 max |
| s5 | 目录含 bn2 pom（新 ts）+ bn1 sources.jar（旧 ts，唯一 sources 候选） | pom GET→**bn2 pom**；sources GET→**bn1 sources** | **classifier 族独立 resolve**：不因 max-ts 构建缺 sources 而落空、也不跨族错配 |

**W2 结论（选择规则完整定义）**：`max(文件名 ts, buildNumber)`，ts 逐字符（即数值）比较、**bn 数值比较**；候选域=同目录内匹配 `<artifact>-<baseRev>-<ts>-<bn>[-<classifier>].<ext>` 且 (artifact, baseRev, classifier, extension) 四元组与请求一致者；上传 wall-clock 与存储 mtime 不参与（s1 定谳）。与 metadata `<snapshot>` 指针解耦（L034 F4 已证，本次未变）。

### W3 timestamp 拼写臂并票判定（case maven-walk-sameface-mvn c1/c2/c3；r1≡r2≡r3）

| # | 臂 | A 面 | B 面 |
|---|---|---|---|
| c1 | plain PUT→Location=改写拼写 T1；GET plain vs GET T1 | **逐字节相同**（=P1）；二次 plain PUT（T2）后 plain GET **翻为 T2 字节** | plain GET 404（ts GET 200）——fence |
| c2 | HEAD plain vs HEAD T2 | **200/280/ETag 相同**（`etag_eq`）——plain GET 活体 resolve，验证器回显目标 | 404 |
| c3 | **真实 mvn 3.9 deploy:deploy-file SNAPSHOT**（A 经 forward-proxy，mvn exit=0 双端）→ 目录实形 + plain GET member/virtual | 落库=**客户端上传的时间戳拼写**（`walkmvn-1.0-20260929.053829-1.{pom,jar}` + 客户端 maven-metadata.xml 767B）；plain GET member=**200 服务 mvn pom**、virtual=**200 同** | 同构落库（ts 拼写+metadata）；plain GET 404 ×2 |

**W3 并票判定证据：同面（same face）**。理由：①resolve 对 wire-PUT 改写产物（c1）与 mvn deploy 客户端时间戳拼写产物（c3）行为一致——同一目录 walk，选版与落库来源无关；②plain GET 是活体 resolve（c1 二次 PUT 后随目录翻新）非冻结别名；③验证器/字节=目标实体（c1/c2）。契约 `maven/plain-snapshot-path-resolve` 的 unobserved「时间戳拼写臂（mvn deploy unique 构造）」可回填：**与 walk 同面，并入本票裁处**。

### W4 virtual 面（case c4 + maven-virtual-walk-selection r4≡r5 + 手工剖析 13 观察）

- 单成员 virtual：plain GET/HEAD 200（t10/F6）——virtual 层透传成员 walk。
- **多成员跨成员选版（关键新发现）**：

| 场景（virtual=[m1,m2]，各成员一个 ts pom 候选） | A 服务 | 模型检验 |
|---|---|---|
| v1：decl〔旧 ts,新 ts〕上传 旧→新 | **vNEW** | 全局 filename-ts ✓ / mtime ✓ |
| v2：decl〔新 ts,旧 ts〕上传 新→旧 | **vOLD** | **全局 filename-ts ✗（否证）**；mtime ✓ |
| v3：decl〔新 ts,旧 ts〕上传 旧→新 | **vNEW** | 声明序 ✗（否证 last-member）；mtime ✓ |
| （c4：decl〔旧,新〕上传 旧→新，×3 轮） | m2-new | 同 v1 |
| （手工 m1-m4+c1-c4 八形：新 ts 先传、旧 ts 后传，spacing 0~2s、settle 1~12s、带/不带 checksum 头） | **恒 vOLD** ×8 | mtime 模型 13/13 成立 |

**W4 结论**：virtual 多成员 plain resolve 的**跨成员择取键=候选文件的存储/上传 mtime（最后落库者胜）**——与成员声明序无关、与文件名 ts 无关（v2 否证全局 filename-ts）；而**成员内部 walk 的择取键=文件名 (ts, bn)**（W2）。两层键不同，stage 2 实现须分开：成员内按 W2 规则出候选，跨成员按 mtime 择一。（13/13 观察一致、case 两轮稳定；「mtime」无法从外部进一步区分文件 mtime 与成员解析时刻，如实注记。）

### 对齐 A 需要 B 改什么（stage 2 实施面建议，供票面）

1. **resolve 点位**：`internal/adapter/maven/handler.go handleGet`——`serveFile`/`serveSidecar` 前插入 plain 拼写 resolve：layout 满足 `Snapshot && !Timestamped`（即 plain 文件名）且 `svc.Get(plain)` 未命中时，列 version 目录，按 W2 四元组族匹配候选、`max(ts, bn 数值)` 择一，以该节点服务（字节/验证器/Range 均落目标实体——c1/c2/t2/t4）。
2. **旁车面**：`serveSidecar` 的 target 同样过 resolve（t5/t6：body=目标摘要）。
3. **virtual 面**：virtual GET 的 walk 走成员迭代，成员内 resolve 出候选后**跨成员按存储 mtime 择取**（W4；勿用 filename-ts 跨成员比较）。
4. **parse 门次序（t8 亚面）**：非 SNAPSHOT/非时间戳文件名的 GET 当前 400（layout parse 先拒）；A 是 404（路由层 honest miss）。需把「unparseable 文件名的 GET」的次序/状态对齐（404 化或保持 400 待裁——A 证据在案，倾向 404，裁定归 conductor）。
5. **翻面测试**：`internal/adapter/maven/plain_snapshot_resolve_test.go TestPlainSnapshotUniqueHomeBoundary` 第 91-93 行（plain GET=404 断言）随 stage 2 落地翻红即翻面——该测试自带注记指向本票。

---

## Part 2 — auto-materialize 副探（known-divergence `maven/deploy-put-version-metadata-auto-materialize`，UNKNOWN，取证→裁）

case `maven-auto-materialize-probe`（r1≡r2≡r3 **case 级 PASS**——读路径行为双端全对齐）；default maven local 仓（双端 repo config 原文留 evidence）。

| # | 臂 | A 面（live） | B 面（live） | 对照 |
|---|---|---|---|---|
| m0 | release wire PUT pom 1.0.0 → version 级 metadata（30s 界） | **404@30s——release 不物化 version 级** | **404@30s 同** | **一致** |
| m1 | module 级 metadata（`…/mata/maven-metadata.xml`） | **200@0.1s**，versions=[1.0.0]、latest=release=1.0.0、lastUpdated 时间戳形态 | **200@0s 同形**（读时派生） | **一致**（内容/合并行为） |
| m2 | 第二版本 1.1.0 PUT → module 合并 | versions=[1.0.0,1.1.0]、latest=release=**1.1.0** | 同 | **一致** |
| m3 | version 级 metadata 1.1.0 | 404@30s（release 恒不物化） | 404@30s | **一致** |
| m4 | DELETE 1.0.0 pom（204）→ 回收 | module 列表**立即**降为 [1.1.0]；1.0.0 version 级仍 404 | 同（2.1s 内） | **一致** |
| m5 | 物化 metadata 的 `.sha1` 旁车 | 200=metadata 体摘要（r1 raw） | （m0 未触发时跳过；r1 A 侧已证） | 一致（A 侧） |
| m6 | SNAPSHOT plain PUT（unique 家园）→ version 级 metadata（60s 界，**GET 前先列目录**） | 目录清单 **GET 前即含 maven-metadata.xml**（lastModified=pom 后 39ms）→ **写路径同步物化**；metadata 结构：snapshot{timestamp=20260929.054234, buildNumber=1}+snapshotVersions[pom→3.0-…-1] | **同构**（58ms，GET 前已在清单；结构同形） | **一致**——**B 已在写路径物化 SNAPSHOT version 级 metadata** |
| 追加 | release PUT 后 repo 根 deep list（手工 mc 探针） | files=〔pom + **`/com/diff/mc/maven-metadata.xml` 物理节点**〕 | files=〔pom〕**无 metadata 节点**（读时派生） | **分歧（计数面）**——A 恒 +1/module；B 无节点 |

**副探结论（裁定素材，conductor 裁）**：
1. **台账 surface 需修正**：A 的写路径物化是 **module 级**（pom PUT 即物化，物理节点，立即可读、合并、随 DELETE 回收）；**release 的 version 级 metadata 双端都不物化**（404@30s）。T-555 s0/s4 的「files=2/3」系 deep-list 计数无法区分层级——**level 归因当时写错为 version 级**（计数本身没错：+1/module）。
2. **L033「A snapshot 30s 不物化 version 级」被否证**：unique 家园 plain PUT 下 A 写路径**同步**物化（39ms，GET 前在目录清单）——×2 轮稳定。
3. **读路径行为双端全对齐**（m0-m6 PASS ×3 轮）：module 级内容/合并/回收、SNAPSHOT version 级结构/时机。
4. **残余分歧=存储节点存在面（计数面）**：A +1/module（物理 metadata 节点进 list 与 deletedArtifactsCount）；B 读时派生无节点。对齐方向两出口：①对齐 A=写路径物化 module 级（计数面随之同形，L032 A=6-vs-B=5 类口径差消失）；②维持 B=读时派生+计数口径注记（凡 list/count 对照臂按面分别钉形）。**无 authority 可代裁，维持 UNKNOWN 上报**——但 surface 描述建议随本证据修订（version 级→module 级）。

---

## 诚实纪律记录（case 侧 bug 与作废轮）

- r1 tiebreak：arm() 路径缺 version 目录 → A layout-GAV 门 409（正确拒绝）→ 四 tie 臂 404 假象。修复后 r2/r3 为有效两轮。
- 首版 virtual-walk-selection r4/r5：scenario 复用成员仓 → 目录残留上轮候选、served body 不再标识赢家（v2/v3 假绿）。作废重跑，fresh repos per scenario，r4/r5 有效。
- 手工 mirror 探针（q 形）与首版 case 结论冲突 → 逐变量剖析（checksum 头/spacing/settle 全排列 8 形 + fresh-member case ×2）定位污染根因，最终模型 13/13 一致。
- 未测面（NOT_RUN 如实）：walk 请求经 remote/cache 仓投影（`<K>-cache`）；`.sha256` 旁车 walk；plain DELETE 的 resolve 语义；t8 对应 PUT 形态（非 SNAPSHOT 文件名 PUT 的 A 行为）；auto-materialize 多 module 非递归边界、覆盖 PUT 重算。均未在票面目标内，留 stage 2/后续。

## 状态机反馈与建议（不翻台账，仅提案）

| 对象 | 建议 |
|---|---|
| 契约 `maven/plain-snapshot-path-resolve` unobserved「时间戳拼写臂」 | 回填：**与 walk 同面**（W3 c3 mvn 构造同 walk）——并入 T-562 stage 2 |
| known-divergence `maven/plain-snapshot-path-resolve-404`（walk 族红半边） | stage 2 规格输入齐备（W1 触发全集 + W2 选择规则 + W4 virtual 双层键 + t8 parse 门亚面）；修复票落 地后本批 5 case 即复验门 |
| known-divergence `maven/deploy-put-version-metadata-auto-materialize` | surface 修订建议：物化层级 version→**module** 级；release version 级双端 404（该亚面可从 unobserved 移除）；SNAPSHOT 同步物化双端一致（unobserved 边界定案）；残余分歧=module 级物理节点/计数面，对齐方向仍 UNKNOWN 待裁 |
| 提金候选（交 compatibility-engineer 评审） | ①W1 触发矩阵 A 腿（t1-t10 + Location 改写拼写/UTC ts 形态）；②W2 选择规则 A 腿八场景（tie/numeric-bn/classifier/extension 锚）；③W4 virtual 跨成员 mtime 择取 A 腿三场景（+手工 8 形注记）；④auto-materialize A 腿（module 级物化/合并/回收 + release version 级 404@30s + SNAPSHOT 同步物化目录清单-before-GET 锚） |
| L033 注记「A snapshot 30s 不物化」 | 建议随本报告证据勘误（unique 家园构造下同步物化） |

## 层级索引

L5（maven 协议：plain resolve 触发/选择/旁车/parse 门）、L7（virtual 跨成员择取键）、L8（存储面：module 级 metadata 物化/回收/计数口径）。

## 机读对账

- 主批：`run/l035-r7-r{2,3}/results.json`（schema `difftest/v2`）+ `run/l035-r7-r{4,5}/results.json`；case id 与本报告节一一对应（trigger-matrix→W1、selection-tiebreak→W2、sameface-mvn→W3+c4、virtual-walk-selection→W4、auto-materialize-probe→Part 2）。
- r2 vs r3 断言稳定性：双侧逐项相同（脚本核验输出 `ALL_STABLE True`）；r4 vs r5 同。

## 断点快照

无中断。双面 `difftest-l035-*` 残留 0（各核验一次）；B 实例已停（18080 释放、graceful stop 日志在案）；数据目录 /tmp/binflow-r7-walk/data 与 /tmp/l035-run.sh 留存；**未 commit**（case 5 个新文件 + 本报告 + 工作日志在工作树待 conductor 处置）。
