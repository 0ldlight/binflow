# cron 表达式与调度行为锚（M16 FR-150 前置锚，T-435；K70 归位）

> **目的与效力**：为 ADR-0044（cron 调度域，T-436 并行在途）与实现票 T-446（引擎）/T-450（三消费面 BE）/T-462（FE 消费面）提供 Artifactory 侧的**行为锚**——表达式语法子集、校验/拒绝形态、next-run 语义、出厂调度默认值。**本文件不裁定 BinFlow 架构**（数据模型/挂点/并存语义归 ADR-0044）；两文件差异以「软协作缝」对齐（M15 T-407/T-408 先例——Accepted 前对齐、不设硬 dep），对齐结论回写本文件 §6。
>
> **取证口径（票据指定：官方文档主源）**：表达式语法 = **Quartz CronTrigger 官方教程**（Artifactory 直接使用 `org.quartz.CronExpression`——反编译 CronUtils 引用铁证；Quartz 本体不在反编译范围，语法以官方文档为准）+ JFrog 官方维护文档（GC cron 用法）。反编译补白 = 校验时机/拒绝文案/next-run 端点/出厂默认值。t226 活体腿降级（2026-09-03 实例不可达——见 aql.md §14 头注；crontime 预览端点现值待验证 §5）。

## 1. 表达式语法（置信度：高——Quartz 官方文档 + 反编译引用双源）

**6 或 7 域**，空格分隔（**非 Unix cron 的 5 域**——5 域形态在 Quartz 解析器直接不合法）：

| # | 域 | 必填 | 合法值 | 特殊字符 |
|---|---|---|---|---|
| 1 | Seconds | 是 | 0–59 | `, - * /` |
| 2 | Minutes | 是 | 0–59 | `, - * /` |
| 3 | Hours | 是 | 0–23 | `, - * /` |
| 4 | Day of month | 是 | 1–31 | `, - * ? / L W` |
| 5 | Month | 是 | 1–12 或 JAN–DEC | `, - * /` |
| 6 | Day of week | 是 | 1–7 或 SUN–SAT（1=SUN） | `, - * ? / L #` |
| 7 | Year | 否 | 1970–2099 | `, - * /` |

特殊字符语义（官方）：

- `*` 全值；`?` = 「不指定」——**day-of-month 与 day-of-week 两域必须其一为 `?`**（同时指定两域字面值是残缺支持，Quartz 要求二选一让位）。
- `-` 区间；`,` 列表；`/` 步进（`0/15`、`5/15`；`*` 后同 `0/`）。
- `L` 末日/最后周X（`L`=月末；`6L`=最后一个周五；`L-3`=倒数第三日）；`W` 最近工作日（`15W`，不跨月）；`#` 第 n 个周X（`6#3`=第三个周五）；`LW`=最后一个工作日。L/W 不与列表/区间组合。
- 月名/星期名**大小写不敏感**（MON = mon）。
- 官方注意点：夏令时切换小时附近的触发会跳过或重复。

**Artifactory 自带模板验证语法的实例**（出厂配置模板逐字——证明以下形态全部被接受）：`0 0 2 ? * MON-FRI`（周一至五 2:00）、`0 0 2 ? * SAT`、`0 23 5 * * ?`、`0 0 /4 * * ?`（hours 域裸 `/N` 步进形态——无起始值前缀，Quartz 接受且 JFrog 官方文档示例同款 `0 0 /12 * * ?`）、`0 12 5 * * ?`。

## 2. 出厂调度默认值（置信度：高——出厂配置模板逐字 + 官方维护文档互证）

| 域 | 出厂表达式 | 语义 | 备注 |
|---|---|---|---|
| backup-daily | `0 0 2 ? * MON-FRI` | 周一至周五 2:00 | 出厂 enabled |
| backup-weekly | `0 0 2 ? * SAT` | 周六 2:00 | 出厂 **disabled**（保留 336h=2 周） |
| GC（gcConfig） | `0 0 /4 * * ?` | 每 4 小时 | 官方维护文档同口径（"every 4 hours by default"）；UI 列 = Cron Expression / **Next Run Time** / Run Now |
| cleanup（unused cached） | `0 12 5 * * ?` | 每日 05:12 | 官方示例改 12 小时一次用 `0 0 /12 * * ?` |
| virtualCacheCleanup | `0 12 0 * * ?` | 每日 00:12 | 清理超 168h 的缓存 POM |
| 「永不运行」占位 | `0 0 0 ? * * 2099` | 2099 年前不触发 | 反编译常量 CRON_NEVER（补充官方规范——禁用语义的表达式编码） |
| 无预置表达式的内部 job | `0 <随机分> <随机时> ? * *` | 每日一次随机时刻 | 反编译：无 system property 预置时按服务标识哈希散列到小时/分钟（日任务错峰；补充官方规范） |

## 3. 校验时机与拒绝形态（置信度：文案逐字 = 反编译单源〔中〕；码位 = 反编译 + 官方文档族〔高〕）

| 面 | 触发 | 形态 | 文案逐字 |
|---|---|---|---|
| 复制配置 REST（cronExp 字段） | 表达式缺失 | 400 | `cronExp is required` |
| 复制配置 REST | Quartz 解析不合法 | 400 | `Invalid cronExp` |
| 复制配置（UI 保存面） | 同上两臂 | UI 错误呈现（走同校验器） | 同族 |
| UI next-run 预览端点 | 空表达式 | 错误码字段 | `emptyCron` |
| UI next-run 预览端点 | 不合法表达式 | 错误码字段 | `invalidCron` |
| UI next-run 预览端点 | `isReplication=true` 且间隔 ≤ 5 分钟 | 错误码字段 | `shortCron`——**复制类调度最小触发间隔 = 5 分钟**（下一触发与再下一触发间距须 > 5min） |
| UI next-run 预览端点 | 算不出未来触发（如年份域已过） | 警告码字段 | `pastCron` |
| 配置文件装载（backup/GC/cleanup XML/YAML） | 非法 cron | 配置装载失败异常族 | CronConfigurationException（ConfigurationException 族） |

**校验器 = 同一个**：全部走 Quartz `CronExpression.isValidExpression`（反编译 CronUtils 单点封装）——即「合法」的定义全域一致 = §1 语法全集；Artifactory **未做**语法子集收窄（域/特殊字符全开放，仅复制类加 5 分钟间隔闸）。BinFlow 若收窄子集（见 §6 建议）即为 C 层差异，须留痕。

## 4. next-run 语义（置信度：高——反编译 + 官方 UI 列名互证）

1. **next-run = Quartz `getNextValidTimeAfter(now)`**：**严格晚于当前时刻**的下一次触发（恰在当前秒的触发点不计入，跳到再下一次）。
2. **计算原点 = 查询/保存当时的当前时间**（无「上次触发」锚定——调度器失联恢复后按 now 重算，不补跑错过的时间点；「过去时间」天然不可配——表达式只能描述未来周期，年份域过期即 `pastCron`）。
3. **UI 预览端点**（反编译；T-462 FE next-run 呈现的 Artifactory 对位）：`GET /ui/api/crontime?cron=<expr>[&isReplication=true|false]`（UI REST 树，admin/user 角色）→ 200 `{"nextTime": "<Java Date.toString 形态>"}`（如 `Wed Sep 03 08:00:00 UTC 2026`——**非 ISO 格式**，wire 怪癖如实登记）；错误臂见 §3 表（错误码以 UI REST envelope 的 error 字段携带）。
4. **维护面呈现**：GC 配置页字段/列 = Cron Expression + Next Run Time + Run Now（官方维护文档）——「配置即见 next-run」是官方 UI 形态锚（T-462 消费）。

## 5. 待验证清单（零静默升格）

| # | 项 | 现值依据 | 验证途径 |
|---|---|---|---|
| C-a | `/ui/api/crontime` 端点现值（路径拼法、nextTime 逐字格式、错误 envelope 形态） | 反编译单源（中） | t226 恢复后只读 GET 探针 |
| C-b | 复制 REST `cronExp` 400 两文案在 7.84.10 现值 | 反编译单源（中） | t226 恢复后 PUT 复制配置（**写操作——INC-1 只读纪律下不可执行**；候 BinFlow e2e 对拍或 Pro 实例授权腿） |
| C-c | 5 分钟最小间隔闸是否也作用于非复制类（备份/GC） | 反编译仅见 isReplication 分支（推断：仅复制类） | 同上授权腿 |
| C-d | `0 0 /4 * * ?` 裸 `/N` 形态在 Quartz 实际触发集（= 0,4,8,…20 还是含 24 环绕） | 出厂模板使用 + 官方示例同款（接受性高；触发集按 Quartz 步进语义推断 0/4/…/20） | BinFlow 引擎对拍断言（T-446 next-run 计算器测试用例自带此臂） |

## 6. BinFlow 子集锚输入（供 ADR-0044 / K70 定案——本节是建议输入，非裁定）

- **语法子集建议**（最小可行 = 覆盖出厂模板与官方示例全部形态）：6 域必填 + 可选第 7 年域；特殊字符收 `* ? , - /` 五种 + 数字/名字域值（`L W #` 是否收录交 ADR——Artifactory 全开放，BinFlow 不收即 C 层差异留痕）；5 域 Unix 形态**拒绝**（对齐 Artifactory：解析器层面即不合法）。
- **拒绝形态建议**：REST 面 400 + `cronExp is required` / `Invalid cronExp` 文案族对齐（复制类）；配置面装载失败。间隔闸（≥5min）是否引入复制面外场景交 ADR。
- **next-run 计算器**：严格晚于 now 的下一次触发（§4-1/2）；对拍断言 = 出厂五表达式 + `CRON_NEVER`（next-run ≈ 2099）+ 裸 `/N` 形态（C-d）。
- **与 ADR-0044 的对齐缝（T-436 软协作）**：本锚钉的是「表达式合法性定义全域唯一 + next-run 严格未来 + 出厂默认值」三件；schedule 实体建模/挂点/与事件驱动并存语义/误触发防护参数（过去时间拒配 400 形态、每域并发上限）**不在本锚范围**——差异与最终口径以 ADR-0044 Accepted 文本为准，回写本节一行留痕（〔待 T-436 对齐回填〕）。
