# jobs/ — 异步/调度域规格目录（Phase 0 任务 #7）

> 目录化索引：既有 cron 规格入口 + 宪章 §21「异步全景」逐轴对账。Artifactory 的异步执行不止 Quartz cron——
> 还有 work-queue（内存/持久工作队列）、event 消费（jfbus/event-stream 族）、db-scheduler（Access 侧 JDBC
> 调度器）与 HA 分布式执行（Ha 任务/冲突守卫）。本 README 给出逐轴「有证据/UNKNOWN」判定；**不重写既有内容**。

## 1. 既有规格索引（一份 + 关联）

| 文件 | 覆盖面 | 明确不覆盖（文件自声明） |
|---|---|---|
| `../cron-scheduling.md` | Quartz 表达式语法全集（§1）、出厂调度默认值（§2：backup-daily/weekly、GC、cleanup、virtualCacheCleanup、CRON_NEVER、随机错峰）、校验时机与拒绝文案（§3：cronExp is required / Invalid cronExp / emptyCron / invalidCron / shortCron≥5min / pastCron）、next-run 语义（§4：严格未来、`GET /ui/api/crontime`） | schedule 实体建模/挂点/与事件驱动并存语义/误触发防护（§6 显式移交 ADR-0044） |

关联：`../inv-1-core.md`（生命周期治理分区含清理任务目录）、`../replication.md`（事件驱动复制消费面）、
`../inv-4-addons.md`（HA/事件 addon 条目）、storage README §2.2（GC/MPU 清理轴）。

## 2. 宪章 §21 异步全景逐轴对账

证据等级：E1 = 反编译静态观察（模块/类存在性——本轮 grep 定位，路径锚 `reverse-src/artifactory` → `backend-maven/`）。

### 轴 1：work-queue（工作队列）— 基础设施有证据 / 行为语义 UNKNOWN

- **有证据（E1）**：`artifactory-work-queue-api` 模块——接口面 WorkQueue / AsyncWorkQueueService / WorkQueueInfo / WorkItem / WorkItemWrapper / WorkQueueJobDecorator / RemoveWorkQueuePredicate / WorkQueueConflictsGuardProvider + mbean 包装（WorkQueueWrapper〔MBean〕→ 可观测面存在）。
- **有证据（E1）**：消费方 ≈20 个模块——addon-{cargo, chef, composer, conan, debian, docker, federated, gems, mirror, nuget, opkg, pub, puppet, release-bundle, replication, swift, terraform, yum} + addons-common + common + core（包索引计算类工作项为主，如 CargoCalculationWorkItem）。
- **UNKNOWN**：队列语义（重试/退避/上限/死信）、并发与队列名约定、持久化与否（对照轴 4 的 PersistentQueueErrorService 疑为持久腿）、mbean 暴露的 JMX 属性面。**需要**：work-queue-api 全模块细读专项票。

### 轴 2：event 消费 — 基础设施有证据 / 消费语义 UNKNOWN

- **有证据（E1）**：模块族全在——`eventstream`、`event-client`、`jfrog-eventing-client-api/-core/-jfbus-adapter`（含 outbox scheduler）、`jfrog-eventing-facade-api/-impl`、`jfrog-eventing-uem`、`jfrog-eventing-messagebus`、`artifactory-event-queue-api`、`artifactory-addon-jf-event`、`atlassian-event`；运行时另有独立 event Go 服务（:8082 topology，`/event/webapp` 在 SSR 开放路由表）。
- **有证据（spec）**：replication.md 已覆盖事件驱动**复制**的消费语义（域内）。
- **UNKNOWN**：事件总线拓扑（jfbus 主题/订阅模型）、Artifactory 内部事件的产生→投递→消费链、outbox 持久化与重投、event 服务与 Artifactory 主体的分工。**需要**：event-stream/jfbus 专项逆向票 + :8082 `var/log/artifactory-event*` 样本（E4 日志腿与任务 #4 协同）。

### 轴 3：db-scheduler — Access 侧有证据 / Artifactory 主侧 UNKNOWN

- **有证据（E1）**：捆绑 `db-scheduler` 模块（com.github.kagkarlsson 库）+ `access-server-core` 的 `org.jfrog.access.server.jobs.*`（JdbcScheduler / JdbcSchedulerBuilder / JFrogJdbcCustomization / JobScheduler / AbstractScheduler / ScheduleData / SchedulerUtils / JobSchedulerName + Trace/Tenant 拦截器）——Access 服务用 JDBC 共享表调度。另有 `atlassian-scheduler-api` + `atlassian-scheduler-caesium` 模块（另一调度抽象）。
- **UNKNOWN**：db-scheduler 承载的具体 job 清单、任务表 schema 与抢占语义、与 Artifactory 主服务 Quartz 的分工边界、caesium 的使用方。**需要**：access-server-core jobs 包专项票。

### 轴 4：HA 分布式执行 — 机制类有证据 / 语义 UNKNOWN

- **有证据（E1）**：`artifactory-core/.../schedule/` 含 HaQuartzTask / MonitoredQuartzTask / QuartzConflictsGuard / ArtifactoryConcurrentExecutor / SingletonTaskAlreadyScheduledException / FullExecutorQueueException / ReplicationThreadPoolTaskExecutor 等 HA/并发原语；`artifactory-storage-common/.../storage/quartz/task/history/`（任务历史持久化类型）；`artifactory-core/queue/error/PersistentQueueErrorService（Impl）`（持久队列错误恢复）。
- **UNKNOWN**：HA 下「同 job 单节点执行」的保证机制（Quartz JDBC clustered store 与自有守卫的叠加关系）、任务接管/错过不补跑的集群语义（cron-scheduling §4-2 已锚单节点语义，集群腿未锚）、任务历史查询面。**需要**：HA 专项票（与任务 #5 企业域交叉；双节点实验需环境）。

### 轴 0：Quartz cron（既有规格已覆盖）— 见 §1；缺 C-a~C-d 四条动态验证（cron-scheduling §5 原挂账）。

## 3. 缺口汇总（供 unknown 队列收割，任务 #8）

| # | 问题 | 为何未知 | 需要什么证据 |
|---|---|---|---|
| J-1 | work-queue 队列语义与可观测面 | 只做了模块存在性定位 | work-queue-api 全模块细读 + JMX 活体查询 |
| J-2 | 事件总线拓扑与消费链 | 模块族庞大未展开 | jfbus/event-stream 专项票 + event 日志样本 |
| J-3 | db-scheduler job 清单与表结构 | Access jobs 包未细读 | access-server-core 专项票 + DB 查表（:8082 postgres 只读） |
| J-4 | HA 单执行的守卫语义 | 类存在但机制未读 | 反编译 HaQuartzTask/ConflictsGuard + 双节点实验 |
| J-5 | 出厂 job 全集与触发时刻 | cron-scheduling §2 覆盖 5+2 出厂项，但内部 job 全集（如 VCS、索引、MPU 清理默认关）未穷举 | config-templates 全扫 + 运行时 scheduler mbean |
| J-6 | `/ui/api/crontime` 现值 | C-a 挂账 | :8082 只读 GET 探针 |

## 4. 阅读序

1. cron-scheduling.md 全文（语法/默认值/拒绝形态/next-run——ADR-0044 输入）
2. 本 README §2 四轴 → 各轴「需要」栏即后续票种子
3. replication.md（事件消费的已规格化域内样本，可作 J-2 的方法论参照）

## 5. 一致性自检

- 与 cron-scheduling.md §6「不在本锚范围」声明一致：本 README 只补基础设施轴，不裁定 BinFlow 调度架构。
- §2 类路径锚均为本轮 grep 实测定位（backend-maven 树），属存在性证据；无行为断言，无结构翻译。
