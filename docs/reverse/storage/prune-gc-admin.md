# prune/gc 管理面行为规格（LOOP 002 / L002-3）

> 域：`/artifactory/api/system/storage/*` 维护端点族 + PUD（Prune Unreferenced Data）状态/报告面 + GC 调度面。
> 证据基线：**A** = 反编译树（`reverse-src/artifactory/backend` 7.161.24 全量 + `reverse-src/artifactory-7.161.20-partial`；两树 StorageResource 功能逐行一致，diff 仅反编译器排版差）；
> **B** = 活体实例 :8082（7.161.20，PostgreSQL 后端，admin basic，2026-09-10 探测）；
> **C** = 官方 REST 文档（docs.jfrog.com：rungarbagecollection / startpudprocess / getpudprocessstatus）。
> 关联规格：`binary-provider-chain.md` §4.3 G 组（G3/G4 已录 prune/gc 端点首证；本票展开为字段级规格，不修改该文件）。
> 版本标注：PUD 三端点 since 7.72（官方）；gc/compress/backup 自 2.x 起（官方）。

## 1. 端点/操作表（`/artifactory/api/system/storage`，类级 `@RolesAllowed({"admin","ha"})`）

| # | 方法+路径 | 鉴权/参数 | 成功响应 | 错误响应 | 异步性 | 置信度 |
|---|---|---|---|---|---|---|
| E1 | `POST /gc` | admin；无请求体 | **200**，`Content-Type: text/plain;charset=utf-8`，chunked 流式进度体（见 §3.1）；无报告 JSON | 无凭证 401（errors JSON 包装，活体）；并发手动 GC 已在跑 → 409 + status 消息体（反编译）；非 admin 403（官方文档+注解） | **同步阻塞**：HTTP 请求持至 GC job 完成（`waitForTaskCompletion`；活体 0.017s—数秒随工作量） | 高（A+B 双源；409 臂仅 A） |
| E2 | `POST /prune/start` | admin；body 可空或 PruneRequestModel JSON（§2.1 四参数） | **202** + `{"info":"Pruning Unreferenced Data task has been submitted"}`；带 startFromDirectory 时文案为 `Prune Unreferenced Data task resumes from directory <dir>`；CT application/json | 已有 prune 任务在跑 → **412** `{"info":"Pruning Unreferenced Data task cannot be started"}`（活体实证，底层 409 被映射为 412）；AoL 非 dashboard 用户 403；其他异常 500 `Cannot start Pruning Unreferenced Data task: <msg>` | **异步 job 化**（202 即回，任务后台跑，无等待） | 高（A+B+C） |
| E3 | `GET /prune/status` | admin | **200** + PruneStatusReport JSON（§2.2，26 字段/4 对象）；CT application/json | 从未跑过 prune → **412** + `{"info":"No Prune task found"}`（官方+反编译；报告持久化，活体不可复现清零态）；401/403 同上 | 只读（读持久化的最近一次报告，非实时快照流） | 高（A+B+C） |
| E4 | `POST /prune/stop` | admin | **202** + `{"info":"Prune task stop request submitted"}`（CT application/json）——**空闲时也返回 202**（只要历史报告存在就写 stop 标记；活体实证） | 无历史报告 → 412 `{"info":"No running Prune task found"}`（反编译）；AoL 403；写标记失败 500 | **异步**：只提交 stop 标记，运行中任务消费标记后转 stopped（活体：stop 后任务在 ~1 个目录内停下） | 高（A+B） |
| E5 | `POST /compress` | admin；无请求体 | Derby 后端：200 流式 text/plain；**PostgreSQL 后端：报错**（§3.1 流式错误行）——活体探测时实例异常退场，**未获 200/错误行实证**（见待验证 V-1） | 非 Derby 报错走流式体；HTTP 码被流式 holder 钉在 200（反编译） | 同步阻塞（同 E1 机制） | 中（A 单源；B 受阻） |
| E6 | `POST /optimize` | admin；无请求体 | **202**，**空体**，无 Content-Type（活体 0.09s）——sharding 均衡器手动触发 | 启动失败且带异常 → 409（statusCode=409 时）或 412（反编译分支） | 异步 job 化（202 即回；与 E1 的 callManualTask(waitForRunning=false) 同型） | 高（A+B；错误臂仅 A） |
| E7 | `POST /backup?key=<key>` | admin；query 参数 key | key 存在且 enabled → 200 流式（立即调度备份） | key 不存在 → **500**；活体形态 `{"errors":[{"status":500,"message":"No backup identified with key 'X'"}]}`（errors JSON 包装）；反编译为 String entity 裸文本——**两源分歧挂 V-2**；key 存在但 disabled → 500（"is disabled"，仅 A） | 备份「立即调度」后返回（未见等待语义） | 中（B 单源/分歧；A 提供分支） |
| E8 | `GET /size` | admin | **200** text/plain，裸数字（filestore 字节数，活体 `35340717`） | 401/403 | 只读 | 高（A+B） |
| E9 | `GET /info` | admin | 200 + binary provider 树 JSON（binary-provider-chain.md §4.1 已规格，此处仅索引） | AoL 非 dashboard → 405；401/403 | 只读 | 高（既有规格） |
| E10 | `POST /exportds` | admin；query `to` | ——**恒抛 IllegalStateException**（"Export data is no longer supported"，@Deprecated） | 500（异常未捕获路径） | n/a | 中（A 单源；未活体） |

同族非端点澄清：**不存在** `POST /api/system/compact`（老版 Derby compact 已并入 E5 compress；artifactory-rest 全树 grep 仅 AQL 查询参数 `compact` 同名异物）。UI 面另有 `/ui/api/…/maintenance` 家族（§4）。

## 2. 请求/报告 schema（字段级）

### 2.1 PruneRequestModel（E2 请求体，可整个省略）

| 字段 | 类型 | 语义（官方文档 since 7.72 + 反编译互证） | 置信度 |
|---|---|---|---|
| `dryRun` | boolean | true=只统计不删除（估算模式）；缺省按 false 执行。活体：请求带 `{"dryRun":true}` → status 报告 `dryRun:true` 回显 | 高 |
| `startFromDirectory` | string | 从指定分片目录续跑（"00".."ff"）；缺省从头。活体：响应文案切换为 resumes-from-directory | 高 |
| （续跑位置） | int | 从上一目录的 binariesProcessed 位置续跑（官方描述；**JSON 键名未获实证**，反编译模型类不可达——挂 V-3） | 中 |
| `binaryOlderThanDays` | int | 忽略新于 N 天的 blob；0=不看日期（官方描述；键名实证同上） | 中 |

### 2.2 PruneStatusReport（E3 响应体；活体 running/stopped/finished 三态全捕获）

```
{ "status": "<running|stopped|finished|error>",   // error 态仅官方文档记载，活体未触发（V-4）
  "dryRun": <bool>,
  "timing": { "startedAtMillis": <epoch ms>, "startedAt": "<yyyy-MM-dd'T'HH:mm:ss 无时区>",
              "durationMillis": <long>, "duration": "<HH:mm:ss.SSS>",
              "lastUpdatedMillis": <epoch ms>, "lastUpdated": "<同上格式>" },
  "progress": "<N> of 256",                        // 当前/总分片目录；运行中单调递增（活体 3→256 采样）
  "report": { "totalBinariesProcessed": <long>, "totalBinariesCleaned": <long>, "totalBytesCleaned": <long> },
  "lastHandledDirectory": { "name": "<2-hex 分片目录名>", "status": "<finished|stopped|…>",
                            "binariesProcessed": <long>, "binariesCleaned": <long>, "bytesCleaned": <long>,
                            "timing": { …同 timing 六字段… } } }
```

字段计数：顶层 6 键；timing 6 字段（两处复用同一结构）；report 3 字段；lastHandledDirectory 5 字段+内嵌 timing——**合计 26 字段名 / 4 类对象**。此结构补充官方规范（官方仅文字描述，无 schema）。
持久化语义：报告由服务端落 configs 存储（key=STORAGE_PRUNE_REPORT），实例重启后 GET 仍返回最近一次报告；「stopped」是终态记录而非清零。
响应模型 PruneResponseModel 单字段 `info`（string），E2/E4 全部走它。

## 3. 语义流程（当客户端…服务端返回…）

### 3.1 gc/compress/backup 流式响应体（ImportExportStreamStatusHolder，此面补充官方规范）

1. 当 GC 运行产生调试级进度时，服务端在 chunked 流里逐条输出 `.`（每 80 个点后输出一个换行）；信息级消息原样+换行；警告/错误输出 `\n<statusCode> : <message>`——全部实时 flush。
2. 当首个字节写出时，响应被钉死为 `200` + `text/plain;charset=utf-8`（即使后续报错，HTTP 码不再变）——**gc 的错误以流内文本呈现，不改变状态码**。
3. 活体 E1 两次：空载 GC 体恰为 `.`（1 字节）；正文进度与 256 目录扫描日志（artifactory.log）一一对应。
4. 当客户端在 GC 执行中掐断连接时，服务端记 brokenPipe 并停止推送，但任务继续跑完（任务不绑连接）。

### 3.2 prune 生命周期

1. 当客户端 POST prune/start（无并发冲突）时，服务端创建单例 Quartz 手动任务（携带 PRUNE_REQUEST_MODEL 属性）→ 立即 202；任务后台遍历 00..ff 全部 256 个分片目录。
2. 当任务运行中，GET prune/status 持续反映 `status:"running"`、`progress` 递增、report 累计。
3. 当客户端 POST prune/stop 时，服务端仅置 STOP 标记（202 即回）；运行中任务消费标记后停止，status 落为 `"stopped"`（终态持久化）。
4. 当 prune 已在跑再 POST start 时，服务端返回 412（canBeStarted 对手动单例任务的互斥；底层码 409 在 prune 资源层被统一改写为 412）。
5. 当 prune/start 带 `startFromDirectory` 时，响应文案为 resumes-from-directory；**活体观察续跑后 progress 仍报 "256 of 256"**（分母恒 256；续跑目录计数口径未定——挂 V-5）。
6. 当 dryRun=true 时全程只统计，status.dryRun 回显 true，report 有 processed 计数但 cleaned=0（估算模式）。

### 3.3 GC 引擎行为（管理端点背后，供对拍日志面）

1. 当手动或定时 GC 触发时，服务端先跑与 prune 同型的 FilestorePruner 全目录扫描，再按策略回收（binary-provider-chain G4 已录；本票补：迭代节奏）。
2. GC 迭代计数器持久化（configs key=ARTIFACTORY_GC_ITERATION）：每轮 +1，**逢第 20 轮（gcSkipFullGcBetweenMinorIterations 默认 20，value 归 0）执行 FULL**——官方 rungarbagecollection 页明确记载 "The default value is 20"，与反编译一致（双源高）。
3. 当 trashcan 启用时常规轮跑 minor（TRASH_AND_BINARIES），FULL 轮=minor 一遍+fullGcCollector 一遍；trashcan 关闭时每轮直接 full。
4. 当 FULL GC 完成后按 `gc.checkBalanceAfterFullGc` 决定是否触发 sharding 均衡（非分片链无操作）。
5. 当 GC job 运行时，暂停 SHA256 迁移任务与回收站清理任务（PAUSE 策略，单例互斥）。

### 3.4 回收资格（承接 G5/G7，未收口）

当 blob 的全部节点引用（含回收站）已删后**短窗口内**手动 prune/gc 不回收（活体两轮 + 本票两轮再证：processed 3/cleaned 0）——资格门槛（事件管道/时间窗）仍 UNKNOWN，见 binary-provider-chain §8-1；本票不重复立项。

## 4. UI/console 面（`/ui/api/…/maintenance` 家族，反编译单源）

| 操作 | 行为 | 置信度 |
|---|---|---|
| `GET maintenance` | 返回维护配置投影（cron 等）；`PUT maintenance` 保存（写 central config 的 gcConfig/cleanupConfig 面） | 中 |
| `POST maintenance/garbageCollection` | 与 E1 同一阻塞内核（callManualGarbageCollect）；成功返回 UI 信封 `{"info":"Garbage collector was successfully scheduled to run in the background"}`——**文案说后台调度，实际请求持到完成**（文案误导，行为与 E1 一致） | 中（A；UI 树活体 401 不可达，V-6） |
| `POST maintenance/compress` | 与 E5 同内核 | 中 |
| `POST maintenance/cleanUnusedCache` / `cleanVirtualRepo` | 远程缓存清理/虚拟仓库清理触发（不在本票 /api 面内） | 中 |

UI 树挂载前缀与鉴权：本票活体对 `/ui/api/v1/...`（含既有 auth-integration.md 已实证的 ldapgroups 端点）basic admin 全 401——该实例的 UI API 树存在额外鉴权门（会话/CSRF），**挂 V-6，不影响 /api 面结论**。

## 5. 调度面（后台 GC 如何被定时触发）

| 机制 | 行为 | 置信度 |
|---|---|---|
| cron 来源（自托管） | 启动与 gcConfig 变更时按 central config 的 `gcConfig/cronExp` 建 Quartz cron 任务（"Binaries Garbage Collector"）。**出厂默认 `0 0 /4 * * ?`（每 4 小时）**——config-templates/artifactory.config.xml 与活体 GET /api/system/configuration 双证。**此条收口 storage-layout §5 与 storage/README §2.2-6 的 cronExp 挂账**：Quartz cron 就是自托管调度源，此前 G8 的「cron 存疑」按本条修正 | 高（A+B+模板三源） |
| cron 来源（AoL/SaaS） | 不读 cronExp；由 `gc.intervalSecs`（默认 86400s）+ serviceId 哈希生成错峰 cron（分钟=hash%60，起始小时=hash%intervalHours） | 中（A 单源；自托管实例不可观测） |
| config 热更 | 当 PUT /api/system/configuration 改 gcConfig 时，调度任务立即 reschedule（CentralConfigKey.gcConfig 监听） | 高（A） |
| 回收站清理任务 | 仅当 `gc.trashBinariesCleanup.enabled=true`（**出厂 false，默认不跑**）才注册重复任务：间隔 900s，initialDelay<0 时取 [interval/2, interval*1.5) 随机错峰 | 高（A；出厂值见 G8 常量表） |
| 常量族 | gc.intervalSecs=86400 / gcSkipFullGcBetweenMinorIterations=20 / gc.numberOfWorkersThreads=3 / gc.maxRunTimeMinutes=360 / gc.sleepBetweenNodesMillis=20 等（ConstantValues 全表已在 binary-provider-chain G8 转录，此处不重复） | 高 |

## 6. BinFlow 现状对账与差异清单（供 compatibility-engineer 契约化）

BinFlow 现有面（internal/httpapi/）：`POST /binflow/api/v1/system/gc`（同步 mark-sweep，dry-run 默认 body {apply,graceHours}，200 JSON {candidateCount,candidateBytes,deletedCount}，锁冲突 409）、`POST/GET /api/v1/system/cleanup`、`GET/PUT /api/v1/system/maintenance`（六 cron 槽）。

| # | 差异点 | Artifactory | BinFlow 现状 | 分类建议 |
|---|---|---|---|---|
| D1 | gc 端点路径/族 | `/api/system/storage/gc`（含 compress/optimize/backup/size/info 同族 10 端点） | `/api/v1/system/gc` 单端点，**storage 族 9 端点全缺** | REFACTOR（终案：202 异步 + status 报告） |
| D2 | gc 异步性 | 同步阻塞 + 流式 text/plain 进度（请求持到完成） | 同步阻塞 + 最终 JSON——异步形态双方都不是，但 BinFlow 无流式体、无 202 | REFACTOR 按 L002 终案（202 + status） |
| D3 | prune 独立面 | prune/start/stop/status 三端点，202 异步 job + 持久化 26 字段报告 | **无 prune 端点**（prune 仅 maintenance cron 槽语义，gc 引擎 dry-run 形态） | REFACTOR 新增 |
| D4 | 状态/报告 schema | PruneStatusReport（status/dryRun/timing×2/progress/report/lastHandledDirectory） | gc 无 GET 状态面（ADR-0015 erratum ② 记 P2 债）；cleanup GET 有 stats/lastRun（结构不同源） | REFACTOR 新增（对齐字段集） |
| D5 | 并发互斥码 | prune 重复启动 412；gc 409 | gc 锁 409（一致）；无 prune 面 | 契约化时对齐 |
| D6 | dry-run 语义 | prune 请求体 dryRun（默认 false=真删） | gc 请求体 apply（默认 false=dry-run）——**默认极性相反** | 契约化裁定（建议保持 Artifactory 极性在新 prune 面） |
| D7 | 调度配置面 | gcConfig/cronExp（XML config，默认 0 0 /4 * * ?）热更 reschedule | maintenance 六槽 cron（GET/PUT /api/v1/system/maintenance） | 形态差异，映射即可 |
| D8 | GC 迭代节奏 | 20 轮 minor 夹 1 轮 FULL（持久化计数器） | 无 minor/full 之分（单次 mark-sweep） | 契约化裁定（可作 known-divergence） |
| D9 | 错误信封 | prune 族 `{"info": "…"}`；backup `{"errors":[…]}`（V-2 分歧）；gc 流内文本 | writeError 统一信封 | 契约化对齐 |
| D10 | compress/optimize/backup/size/info | 存在（E5–E9） | 全缺（compress 有 BinFlow 语义不同的 metadata 压缩面） | 契约化裁定优先级 |

## 7. 待验证清单（低置信度/分歧，转差分或活体票）

| # | 条目 | 缺口 |
|---|---|---|
| V-1 | compress 在 PostgreSQL 后端的真实 HTTP 形态（流式错误行 + 200？还是 5xx） | 本票活体探测时实例异常退场（容器 Exited(0)，与宿主机 DHCP 换 IP 叠加），未复测；反编译单源 |
| V-2 | backup 坏 key 的响应信封：活体 errors-JSON vs 反编译 String 裸文本 | 两源分歧——疑部署 build 差异或响应过滤器改写；需差分复测 |
| V-3 | PruneRequestModel 第 3/4 参数的精确 JSON 键名（续跑位置 int、binaryOlderThanDays） | 模型类在 jfrog-storage-common 库，两反编译树均不可达；活体响应文案只回显 startFromDirectory |
| V-4 | status="error" 态的完整报告形态 | 官方枚举记载，活体未触发（需制造 prune 故障，如 filestore 只读） |
| V-5 | startFromDirectory 续跑时 progress 分母/计数口径（活体见 "256 of 256"） | 需大 filestore 或日志比对定位 |
| V-6 | /ui/api/v1 树的鉴权门（本实例 basic 401，含已知好端点）与 maintenance 家族响应信封 | UI 面全反编译单源；需会话型客户端（浏览器/JFrog UI 流量）取证 |
| V-7 | gc 并发手动触发 409 的活体复现（decompile-only） | 需两个并发长 GC（大 filestore） |
| V-8 | （承接）GC 回收资格窗口 G5/G7 | 见 binary-provider-chain §8-1，本票未推进 |

## 8. 取证命令摘要（可追溯）

- A 树定位：`grep -rln "system/storage" reverse-src/artifactory/backend` → StorageResource/SystemResource；`find reverse-src -name "FilestorePrunerJob.java"` → partial 树；GCServiceImpl/TaskServiceImpl/ConstantValues/GcConfigDescriptor 同法。
- B 活体（:8082，admin basic）：`curl -D- /api/system/storage/prune/status`（finished 全量 JSON）；python 双线程竞速抓 running/stopped 态与并发 412；`-X POST /prune/start -d '{"dryRun":true}'`（202→status.dryRun 回显）；`POST /gc`（200 text/plain 体 `.`，0.017s）；`POST /optimize`（202 空体）；`POST /backup?key=nonexistent`（500 errors 信封）；`GET /size`（text/plain 数字）；`GET /api/system/configuration`（XML 含 gcConfig cronExp `0 0 /4 * * ?`）；无凭证 401×2。
- C 官方：docs.jfrog.com/artifactory/reference/{rungarbagecollection,startpudprocess,getpudprocessstatus}（PUD since 7.72、四参数、status 枚举含 error、412 "No Prune task found"、GC full cycle 默认 20 轮）。
