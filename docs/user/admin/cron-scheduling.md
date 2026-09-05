---
title: 计划任务（cron 调度）与定时备份
sidebar_position: 44
---

# 计划任务（cron 调度）与定时备份

> 适用版本：cron 调度域当前版本（维护三槽 / 定时备份 / 复制调度三消费面 + 只读投影）。**此前的「不引入 cron 计划任务、复制为纯事件驱动」裁定已被推翻**——调度域现已落地，与既有事件驱动轨并存（语义见下文）。
> 本文全部 curl 命令在 HEAD 构建的本地 scratch 实例（127.0.0.1:18096，2026-09-06）上逐条实测复跑；表达式拒绝文案、`nextBackupTime`/`exportPath` 校验族、park/re-arm 形态均为实测原文。

计划任务让三类**全量类操作**按 cron 表达式到点自动执行：

| 调度域 | 承载的操作 | 配置面 |
|---|---|---|
| **maintenance（维护）** | GC 全量回收、unused-cache / virtual 缓存清理 | `GET/PUT /api/v1/system/maintenance`（三槽） |
| **backup（定时备份）** | 实例快照 export（与 CLI export 同一内核） | `/api/v1/system/backups` CRUD 五面 |
| **replication（复制）** | 单条 push 配置的全量同步（Replicate Now 的到点形态） | 复制配置的 `cron_exp` 字段 |

三条设计基线（与手动面 / 事件轨的关系）：

- **调度只触发全量类任务**：制品落库即入队的事件驱动轨不变——调度 fire 不会与事件轨双推（目标侧 sha256 幂等收敛，调度窗内同制品的增量事件不产生重复字节传输）。
- **与「Run Now」并存**：GC 的手动面（dry-run 先行 + 输入实例名确认）、cleanup 的立即清理、复制的 Replicate Now 全部保留——调度只是多一个到点触发器，手动随时可跑。
- **触发即守护**：调度 fire 撞上维护锁（export/GC/import 互斥）会如实记为一次失败（`last_error` 点名锁冲突），**不静默跳过**——排程错峰是运维责任（见[备份手册](backup-restore.md)）。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）与 admin 凭据（写面仅全量 admin；readonly_admin 只读呈现）。
- 实例启动日志可见调度器就绪行（三域注册）：

```bash
# 服务日志（结构化 JSON）：
# {"msg":"scheduler: started","tick_interval":"1m0s",
#  "registered_domains":["backup","maintenance","replication"],...}
```

## cron 表达式（Quartz 六域子集）

BinFlow 使用 **Quartz 形态的 6 或 7 域表达式**（空格分隔）——**不是** Unix cron 的 5 域形态（5 域直接 400，见下文）。表达式校验**全域唯一**（三个调度域 + 备份 `nextBackupTime` 共用一个校验器），合法性由服务端终裁，错误文案点名具体形态。

| # | 域 | 合法值 | 收录的特殊字符 |
|---|---|---|---|
| 1 | Seconds | 0–59 | `, - * /` |
| 2 | Minutes | 0–59 | `, - * /` |
| 3 | Hours | 0–23 | `, - * /` |
| 4 | Day of month | 1–31 | `, - * / ? L`（`L` = 月末日） |
| 5 | Month | 1–12 或 JAN–DEC（大小写不敏感） | `, - * /` |
| 6 | Day of week | 1–7 或 SUN–SAT（1=SUN；`nL` = 最后一个周 n） | `, - * / ? L` |
| 7 | Year（可选） | 1970–2099 | `, - * /` |

收录与拒收要点：

- **两个「日」域必须恰有一个为 `?`**（同时指定两域具体值、或双 `?` 均 400——Quartz 的让位规则）。
- **收录**：`L`（day-of-month 月末）、`nL`（day-of-week 最后周 n）、月/周名缩写、裸 `/N` 步进（`0 0 /4 * * ?` = 每 4 小时，小时域无起始值前缀的官方形态）。
- **拒收**（每个拒绝 message 点名形态，实测原文）：5 域 Unix 形态、`W`（最近工作日）、`#`（第 n 个周 X）、`LW`、`L-<offset>`（倒数第 n 日）、混合名-数区间、day-of-week 数字 0、闭集外任意字符。
- **next-run 严格晚于当前时刻**：保存或查询时按 now 重算下一次触发；错过的时间点不补跑。

```bash
# 常用形态速查（全部实测通过）：
"0 0 /4 * * ?"        # 每 4 小时
"0 12 5 * * ?"        # 每日 05:12
"0 0 2 ? * MON-FRI"   # 周一至周五 02:00
"0 0 2 ? * SAT"       # 周六 02:00
"0 0 3 L * ?"         # 每月最后一天 03:00（L 收录）
"0 30 1 * * ?"        # 每日 01:30

# 拒绝形态（实测文案）：
# 5 域 →  400 "Invalid cronExp 0 0 5 * * for gc: scheduler: invalid cron
#          expression: got 5 fields, want 6 or 7 (a 5-field Unix cron is not a Quartz expression)"
# W  →   400 "... day-of-month field \"15W\": element \"15W\":
#          nW (nearest weekday) is not supported"
```

## 维护域：GC 与缓存清理的三槽

`GET/PUT /api/v1/system/maintenance`（读 = system:read；写 = system:write 仅全量 admin）。三个槽是**闭集**（键名固定，不存在的槽 400）：

| 槽 | fire 时执行 |
|---|---|
| `gc` | GC 全量回收（与 REST/CLI 同一内核，apply 语义 + 配置宽限） |
| `cleanup-unused-cache` | unused 缓存清理全量 pass（`POST /system/cleanup {apply:true}` 同载体） |
| `cleanup-virtual` | 同上——BinFlow 的两族清理共用全量 pass（virtual 聚合不单独缓存，两槽到点执行等价任务，保留双槽与 Artifactory 键名对齐） |

**零预置**：首配前三槽全空（无表达式 = 不调度，单态）。`GET` 恒返回三槽投影：

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>

# 读当前三槽（首配前：cronExp/enabled/nextRun/lastRun 全空）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/maintenance
# {"slots":[{"key":"gc","cronExp":"","enabled":false,"nextRun":"",...},
#           {"key":"cleanup-unused-cache",...},{"key":"cleanup-virtual",...}]}

# 逐槽布防（PUT；全 arm 先验后写——一次 PUT 里双槽其一表达式坏则整体 400 零残留）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/system/maintenance \
  -H 'Content-Type: application/json' \
  -d '{"gc":{"cronExp":"0 0 /4 * * ?","enabled":true},
       "cleanup-unused-cache":{"cronExp":"0 12 5 * * ?","enabled":true}}'
# 200 + 更新后的三槽投影（gc.nextRun = 下一次触发时刻）
```

单槽语义（与复制/备份的同类形态一致）：

| 面 | 行为 |
|---|---|
| 空 `cronExp` | 删行（不调度的单态）——「清除」即提交空串 |
| `enabled: false` | **停用形（park）**：表达式保留、`nextRun` 清空、到点不触发；恢复 `true` 后按 now 重臂 |
| 坏表达式 | **400 `Invalid cronExp <expr> for <slot>: <点名原因>`**，行内零残留（实测文案见上节） |
| 手动面并存 | GC 的 dry-run/apply、cleanup 的 `POST /system/cleanup` 不受影响——「立即清理」就是它们 |
| `lastRun`/`lastStatus`/`lastError` | fire 后回写（`ok`/`failed` 二值；失败点名原因——含维护锁冲突 409 形态） |
| 审计 | 布防/清除落 `maintenance.schedule.set`（detail：key/cronExp/next_run/enabled 或 `action: cleared`）；fire 落 `gc.run`/`cleanup.run`（actor=scheduler） |

控制台对应：**监控 → 维护（GC）**页的「计划任务」卡（三槽行表：表达式编辑 / 保存 / 清除 / 下次 / 上次）。GC 槽的「手动执行」滚向页内既有危险区（dry-run 先行），两个 cleanup 槽带「立即清理」（常规危险确认）。

## 定时备份：到点 export

备份调度域把 [export 内核](backup-restore.md)接上到点触发器——**产物、一致性窗口、维护锁语义与 CLI export 完全相同**，只是无需外部 crontab：

```bash
# 创建（官方单 PUT 形；body 带 backupKey）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/system/backups \
  -H 'Content-Type: application/json' \
  -d '{"backupKey":"nightly","cronExp":"0 0 2 ? * MON-FRI",
       "exportPath":"/backup/binflow","enabled":true}'
# 200 {"backupKey":"nightly","enabled":true,"exportPath":"/backup/binflow",
#      "cronExp":"0 0 2 ? * MON-FRI","nextScheduleBackup":"2026-09-07T02:00:00Z",
#      "lastRun":"","lastStatus":"","lastError":"","createdAt":"...","updatedAt":"..."}

# 列表 / 单查 / 删除
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/backups
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/backups/nightly
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/v1/system/backups/nightly -o /dev/null -w '%{http_code}\n'   # 204
```

| 字段 | 语义 |
|---|---|
| `backupKey` | 备份名（寻址键） |
| `cronExp` | 调度表达式；**空串合法**（payload 行保留、不调度——先把条目建起来再补表达式） |
| `exportPath` | **服务器上的绝对路径**（必须 `/` 开头；相对路径 400 `exportPath must be an absolute server path (starting with '/')`——防目录穿越，`..` 同拒） |
| `nextBackupTime` | 可选的**首跑时刻**（RFC3339）：给了就用它（过去时间 400 `... is not in the future; pick a time after now or omit the field`），不给则按表达式推算。**编辑态不回填**——它是「首跑」可写位，不是 next-run 只读回显 |
| `enabled` | 停用形（park）：表达式保留、到点不导出 |

fire 行为：

- 每次触发在 `<exportPath>/<backupKey>-<UTC 时间戳>` 子目录产出一套完整 export 产物（同秒重复触发以纳秒后缀防混合）；恢复仍走 [import CLI](backup-restore.md#import停机恢复)。
- 触发撞上维护锁（手动 GC/export 在跑）→ `lastStatus: failed` + `lastError` 点名锁冲突，**下轮再试**——排程时与 GC 错峰。
- 删除条目联动清调度行；台账行丢失时 runner 到点自愈（tombstone）。
- **零预置**：实例不自带任何备份条目（与 Artifactory 出厂的 backup-daily/weekly 有意不同——首配即明确）。
- 审计：布防/清除落 `backup.schedule.set`（detail 含 key/cronExp/exportPath/next_run 或 `action: cleared/deleted`）；fire 双层——`backup.schedule.run`（actor=scheduler）+ 载体 `export.run`。

控制台对应：**监控 → 备份 / 恢复**页的「定时备份」卡（列表：Key / cron / 下次备份 / 启用 / 上次运行 / 路径；New Backup 表单含 key 预检、服务端权威的 cron 校验、绝对路径门、`nextBackupTime` 本地时区输入；E1 输入 key 强确认删除）。页内另有 CLI 引导卡——**import 恢复仍是带外 CLI 操作，不做 UI**。

## 复制域：cron 双轨

复制配置新增可选调度轨——**事件轨（落库即推）+ 调度轨（到点全量对账）并存**，一个 `enabled` 开关同时管两轨：

```bash
# 建配置时带 cron（先验后写，坏表达式不落配置行）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/replications \
  -H 'Content-Type: application/json' \
  -d '{"name":"nightly-sync","source_repo":"repl-local",
       "target_url":"http://target.example:8080","target_repo":"mirror",
       "cron_exp":"0 30 1 * * ?"}'                                   # 201 回显 cron_exp + next_schedule_sync

# 已存配置补 / 改 / 清调度（PUT 至少携带 enabled 或 cron_exp 其一）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/v1/replications/1 \
  -H 'Content-Type: application/json' -d '{"cron_exp":"0 30 1 * * ?"}'
# 200 {...,"cron_exp":"0 30 1 * * ?","next_schedule_sync":"2026-09-06T01:30:00Z"}
```

| 面 | 行为（实测） |
|---|---|
| `cron_exp` 空串 | 清除调度（纯事件轨）；**不带该字段创建 = 无调度行** |
| `enabled: false` | **park**：`cron_exp` 保留、`next_schedule_sync` 清空、事件轨同步停——一个开关两轨全停；恢复 `true` 后调度按 now 重臂、停用期积压由事件轨 sweep 排空 |
| 全量调度语义 | fire = Replicate Now 同载体（枚举源仓逐路径入队）；目标侧 sha256 幂等——与事件轨 / 手动 Replicate Now 交叠**零重复字节传输** |
| push 被全局封锁 | fire 为 **skip 非失败**（封锁对两轨同时生效；台账保持，下周期再试） |
| 空 body PUT | 400 `enabled or cron_exp is required; no other field is editable on this face` |
| 坏表达式 | 400 `Invalid cronExp <expr> for <name>: <点名原因>` |
| 审计 | 布防/清落 `replication.schedule.set`（detail：id/name/cronExp/next_run 或 `action: cleared`）；fire 落 `replication.schedule.run`（actor=scheduler） |

控制台对应：治理 → 复制页 targets 表的**「调度」列**（表达式 + 下次同步），仓库编辑页 Replications 节表单的 `cron` 字段（编辑回显 + 双轨 hint）。

## 只读投影：全部计划一页看

`GET /api/v1/system/schedules`（system:read；`?domain=` 闭集过滤，非法域 400）——三域全部调度行的统一投影，也是控制台「服务状态」页调度节的数据源：

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/schedules
# {"schedules":[
#   {"domain":"backup","key":"nightly","cronExp":"0 0 2 ? * MON-FRI","enabled":true,
#    "nextRun":"2026-09-07T02:00:00Z","lastRun":"","lastStatus":"","lastError":""},
#   {"domain":"maintenance","key":"gc","cronExp":"0 0 /4 * * ?","enabled":true,...},
#   {"domain":"replication","key":"1","cronExp":"0 30 1 * * ?",...}]}
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/system/schedules?domain=backup"   # 单域过滤
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/system/schedules?domain=bogus"
# 400 "domain must be one of maintenance, backup, replication (or omitted for every domain)"
```

## 验证

```bash
# 布防后投影可见 nextRun（严格未来的下一次触发）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/system/maintenance | grep -o '"nextRun":"[^"]*"'
# 审计词三域齐备
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=20" | grep -o '"action":"[a-z.]*schedule[a-z.]*"' | sort -u
# "maintenance.schedule.set" / "backup.schedule.set" / "replication.schedule.set"
```

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 400 `Invalid cronExp ... got 5 fields, want 6 or 7` | Unix 5 域形态 | 改用 Quartz 6 域（秒域开头） |
| 400 `... nW (nearest weekday) is not supported` | `W`/`#`/`LW`/`L-n` 未收录 | 用 `L`/`nL` 或改具体日 |
| 400 `exportPath must be an absolute server path` | 相对路径 / 含 `..` | 填服务器绝对路径 |
| 400 `nextBackupTime ... is not in the future` | 首跑时刻已过 | 选未来时刻或省略（按表达式推算） |
| `lastStatus: failed` + lastError 点名锁 | fire 撞维护锁（GC/export/import 互斥） | 与手动维护错峰排程 |
| `next_schedule_sync` 恒空 | 配置停用（park）或 cron_exp 已清 | `PUT {"enabled":true}` 重臂 |

## 下一步

- 备份产物与恢复链：[备份与恢复手册](backup-restore.md)
- GC 手动面与维护锁：[治理指南](governance.md)
- 复制配置 CRUD 与 Replicate Now：[治理指南 · 复制](governance.md#复制push-replication)
