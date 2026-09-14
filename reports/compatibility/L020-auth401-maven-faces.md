# L020-3 轻差分批 — conan auth401 措辞族 + maven 三小面（双系统对照）

- 日期：2026-09-14（LOOP 020 / L020-3 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 7.161.20；B=BinFlow UAT :8083 `uat-l0203-f94d6b1e`——工作树构建，`git diff f94d6b1e -- cmd internal web deploy charts` 空=产品码逐字节同）
- 纪律：全程串行；ref 健康门先行（双端 ping 200）；臂预算：conan auth401 v1+v2 各 2（实跑 4×2 轮+1 冷却复核）、maven 每面 ≤3（实跑 ①1/②2/③3）；settle 2-3s；复跑门双轮结论稳定（conan c3 第二轮 A 403=登录失败限流瞬态，冷却 15s 后单臂复核回 401 `Bad Credentials`=串行绿）
- 环境：UAT 重建 `binflow:uat-l0203-f94d6b1e-alpine`（.env.uat 版签更新）；构建途中 **Docker Desktop 第 7 次全灭**（docker CLI 挂起/daemon 失联——L015-019 同款第 7 次），按 runbook 处置（杀僵尸 build + kill -9 backend + Docker Desktop 全量重启 + 手工 start postgres artifactory），层缓存续建成功；pro license 跨重建存活（conan repo-create B 200）
- 证据：`reports/compatibility/l0203-wire/`（46 文件：逐臂 .hdr+.body，Authorization 已脱敏）；脚本 `tools/difftest/l0203/{conan-auth401.sh,maven-faces.sh}`（复跑入口，凭据运行时 source env）
- 命名空间：`l0203-conan-auth` + `l0203-mvn-local` 双端建删净（residue A=0 B=0 双轮复核）；audit-probe 全仓扫描无残留；/tmp 脚手架不 commit；token/凭据盘面扫描空

## 任务 1 — conan auth401 措辞族（契约 `conan/auth401-wording` 分立条目的广度与逐字素材）

四臂（v1 面 2 + v2 面 2），文案/CT/信封/WWW-Authenticate 逐字：

| # | 臂（条件） | A（ref） | B（UAT f94d6b1e） | 判定 |
|---|---|---|---|---|
| c1 | v2/users/authenticate 坏凭据（Basic admin:WRONG） | 401；`{"errors" : [ { "status" : 401, "message" : "Bad Credentials" } ]}`（80B pretty）；CT `application/json;charset=ISO-8859-1`；WWW-Auth `Basic realm="Artifactory Realm"` | 401；`{"errors": [ { "status": 401, "message": "invalid credentials" } ]}`（94B indented）；CT `application/json`；WWW-Auth `Basic realm="BinFlow Realm"` | **差异（措辞族）** |
| c2 | v2/conans/search 匿名 | 401；message `Authentication is required`（91B）；同 CT/realm 形态 | **200** `{"results":[]}`（14B compact）；无 WWW-Auth | **差异（新面：匿名读策略）** |
| c3 | v1/users/authenticate 坏凭据 | 401 `Bad Credentials`——**与 c1 逐字同**（第二轮 403 限流瞬态，冷却复核回 401） | 401 `invalid credentials`——与 c1 逐字同 | **差异（同 c1；v1/v2 双面统一）** |
| c4 | v1/conans/<ref> 匿名 | 401 `Authentication is required`——与 c2 逐字同 | **404** `Not Found` envelope（84B）——匿名穿透 auth 后数据面自渲染 | **差异（同 c2 策略面；B 形态=穿透）** |

### 形态结论（auth401）

1. **措辞族双端各自 v1+v2 全域统一**——A 在 conan 两代 API 面恒 `Bad Credentials`（坏凭据）/`Authentication is required`（匿名），B 恒 `invalid credentials`（坏凭据）/匿名不拦。**广度主张成立**：B 渲染点在 auth 中间层（`internal/httpapi/router.go:170-174`，全域 401 统一 `writeError(w,401,"invalid credentials")`+`WWW-Authenticate: Basic realm="BinFlow Realm"`），非 adapter 每端点散写——A 侧同为中间层统一（realm 恒 `Artifactory Realm`）。
2. 措辞族四个差分点：①message（`Bad Credentials` vs `invalid credentials`）②realm 串（品牌面，与产品前缀同性质）③CT charset 后缀（`;charset=ISO-8859-1` vs 无）④信封缩进风格（` : ` vs `: `——L018 §6-R2 归一提案域）。**①为待裁文案面（UNKNOWN，倾向对齐候选——沿 D2 信封族先例）；②③④为归一/品牌域**。
3. **新发现面 A——匿名读策略**：B `DefaultAnonymousAccess=true`（`internal/config/config.go:17-19`，ADR-0009 文档化默认：content GET/HEAD 匿名可读）vs A 实例匿名关（全域 401）。同 payload 建仓对照下行为分叉；B 侧翻转位=`SECURITY_ANONYMOUS_ACCESS=false`（未执行——实例配置红线）。分类 **UNKNOWN 倾向 INTENTIONAL**（产品姿态有 ADR 背书；但 A 参照形态=deny，parity 口径待 conductor 裁）。
4. **新发现面 B——登录失败限流**：连续 4 次坏凭据后 A 第 3 次起 403 `This request is blocked due to recurrent request failures, please try again in 9 second(s)`（倒计时封锁，冷却 ~15s 自愈）；B 4 次全裸 401 无封锁。安全面差——分类 **UNKNOWN**（A 有 brute-force 防护、B 未见对应机制；建议 conductor 立 security 票评估）。

## 任务 2 — maven 三小面（T-L019-1+2 Risks ③④⑤ 终局素材）

fixture：`l0203.test:artver:1.0-SNAPSHOT`（pom 播种→基线 GET→手 PUT metadata→回读 GET→三臂 404）。播种面顺带取证：**双端直 PUT snapshot 目录均把 `artver-1.0-SNAPSHOT.pom` 时间戳改名 `artver-1.0-<ts>-1.pom`**（一致，ItemCreated envelope 键序差=R2 域）。

### 面① 手 PUT maven-metadata.xml 受理码（两案对比）

- **A 案**：`PUT …/1.0-SNAPSHOT/maven-metadata.xml`（586B 客户端形）→ **202**，**0B 空 body，无 CT**——受理即丢弃。
- **B 案**：同 PUT → **201** + `ItemCreated+json` envelope（840B，path/downloadUri 全）——受理 + 文件落地。
- **终态**：双端 GET 回读均为服务端生成形（见面②）——A 不落盘直接丢，B 落盘但读时（或同步重算后）服务端形覆盖。B 的同步重算活体证据：回读体 root `lastUpdated` 从 220123→**220126**（=PUT 时刻重算），`snapshot/timestamp` 仍锚 pom 时刻。
- 分类建议：**UNKNOWN**。语义近平价（客户端内容双端皆不生效、终态同）；受理码+信封差为 mvn deploy 可见面（mvn 对 201/202 均不失败——L019 真实腿 M2 双绿）。

### 面② 手 PUT 后 XML 渲染形态（GET 回读体对照；基线 GET 同形，双轮稳定）

- **A 案**（589B）：`<metadata modelVersion="1.1.0">`（**属性在**）；子元素序 groupId→artifactId→**versioning→version（`<version>` 置尾**）；versioning 内 **lastUpdated 前置**→snapshot→snapshotVersions。
- **B 案**（568B）：`<metadata>`（**无属性**）；子元素序 groupId→artifactId→**version 第三**→versioning；versioning 内 **snapshot 前置**→lastUpdated→snapshotVersions。
- 语义等价面：两组元素集同（唯 A 无独立差异、B 的 snapshotVersions/updated 值双端同构）；**真实 mvn 3.9.16 双端解析通过**（L019 M2/M3 腿）=client-blind。
- 分类建议：**UNKNOWN（对齐候选）**——三处形态差（modelVersion 属性、version 位置、versioning 内序）一次对齐票可收；参照为 A 形。

### 面③ 文件 404 措辞（三臂：存在目录 ghost jar / 整目录 ghost / ghost metadata）

三臂**双端各自完全同形**（breadth：措辞与路径存在性无关）：

- **A 案**：404 `{"errors" : [ { "status" : 404, "message" : "File not found.; Path: 'l0203-mvn-local:l0203/test/…'" } ]}`——CT `application/json;charset=ISO-8859-1`；路径引用 `<repo>:<path>` 冒号分隔；`.;` 双标点为 A 真身 quirk（逐字）。
- **B 案**：404 `{"errors": [ { "status": 404, "message": "Failed to find the requested resource 'l0203-mvn-local/l0203/test/…'." } ]}`——CT `application/json`；路径 `<repo>/<path>` 斜杠分隔+句尾点。
- 分类建议：**UNKNOWN（对齐候选）**——L019 M1/M3 单点观察本轮扩为三臂广度+逐字；对齐则一处渲染函数可收。

## 差异清单汇总（本轮 8 项：conan 4 + maven 3 + 环境注记 1）

| # | 面 | 分类建议 | 一句证据 |
|---|---|---|---|
| V1 | conan 401 message 措辞 | UNKNOWN（对齐候选族） | c1/c3 双端 v1+v2 统一 `Bad Credentials` vs `invalid credentials`（l0203-wire 逐字） |
| V2 | WWW-Authenticate realm 串 | 归一/品牌域 | `Artifactory Realm` vs `BinFlow Realm`（实例品牌面，同产品前缀性质） |
| V3 | 401/404 CT charset + 信封缩进 | 归一候选（L018 §6-R2/R7 域） | `application/json;charset=ISO-8859-1`+` : ` vs `application/json`+`: `（全臂一致规律） |
| V4 | **匿名读策略（新面）** | UNKNOWN 倾向 INTENTIONAL | A 全域 401 `Authentication is required` vs B ADR-0009 默认放行（c2 200/c4 404 穿透） |
| V5 | **登录失败限流（新面，security）** | UNKNOWN | A 连续坏凭据第 3 次起 403 倒计时封锁（9s 自愈）vs B 4 次全裸 401 |
| V6 | maven 手 PUT metadata 受理码 | UNKNOWN | A 202 空 body vs B 201+ItemCreated；终态双端服务端形（B lastUpdated 220123→220126 重算活证） |
| V7 | maven metadata XML 渲染形态 | UNKNOWN（对齐候选） | A modelVersion 属性+version 置尾+lastUpdated 前置 vs B 无属性+version 第三+snapshot 前置（f2a/f2b 逐字在档） |
| V8 | maven 文件 404 措辞 | UNKNOWN（对齐候选） | A `File not found.; Path: '<repo>:<p>'` vs B `Failed to find the requested resource '<repo>/<p>'.` 三臂同形 |
| — | （一致面注记）snapshot 直 PUT 文件名时间戳改写 | 一致 | 双端 `artver-1.0-SNAPSHOT.pom`→`artver-1.0-<ts>-1.pom`（s1 双轮） |

## 回归对照（上轮清单 → 本轮）

- L018 D2②（conan auth401-wording 分立条目素材缺口）：**补齐**——v1+v2 广度+头/CT/信封逐字+渲染点代码锚（router.go:173）。
- L019 Risk③（受理码 201/202 单点）：**仍在**，扩为受理码+信封+重算活证完整两案（V6）。
- L019 Risk④（XML 渲染形态单点观察）：**仍在**，扩为基线+回读双臂逐字+元素序三级定位（V7）。
- L019 Risk⑤（M1/M3 404 措辞单点）：**仍在**，扩为三臂广度+逐字（V8）。
- **新增**：V4 匿名读策略、V5 登录失败限流（两新面均非上轮在册项）。
- fixed：0 / 仍在：3（L019 三面）/ 新增：2（V4/V5；V1-V3 为 D2② 的细化拆分非新增）。

## 回流清单

- **conductor UNKNOWN 待裁**：V1（对齐候选）、V4（匿名姿态：ADR-0009 vs 参照 deny——parity 口径）、V5（security 立票评估）、V6/V7/V8（maven 三面裁定——对齐票 or INTENTIONAL 收）。
- **normalize 提案增量**（归 compatibility-engineer，沿 L018 §6 框架）：realm 串→品牌占位（R7 headers 域扩展）；CT charset 后缀与信封缩进已covered by R2/R7——无新规则私加。
- **契约反馈**：`conan/auth401-wording`（DIVERGENT）素材齐可评；maven 三面建议 known-divergence 新行（V6/V7/V8）待裁定后落。
- **提金候选**（A 侧逐字在档 l0203-wire）：maven metadata XML A 形（f2a/f2b）、文件 404 A 措辞（f3a-c）、conan 401 `Bad Credentials`/`Authentication is required` 双文案（c1-c4）。
- **环境上报**：Docker Desktop 第 7 次全灭（7 次/6 天——频次持续，devops 立案在议）。

## 复跑门

双轮连跑结论稳定（conan 4 臂+maven 6 臂；唯一瞬态=A c3 第二轮 403 限流，15s 冷却单臂复核回 401=串行绿协议通过，限流行为本身记为 V5 证据）。机读证据对账：case id（c1-c4/f1/f2a/f2b/f3a-c）↔ l0203-wire/ 文件名一一对应。
