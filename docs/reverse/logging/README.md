# Artifactory 日志体系逆向（docs/reverse/logging/）

> Phase 0 全量审计·子任务 #4（宪章 §12 日志复刻 P0/P1 级目标）。
> 证据版本：运行实例 7.161.20（E2/E4）/ 反编译 7.161.24（E1）/ 安装包 7.161.16（E3）。
> 目录角色：本目录是 BinFlow 日志复刻（文件名、行格式、轮转语义、审计事件）的唯一行为规格源。

## 文件导航

| 文件 | 内容 |
|---|---|
| [format-spec.md](format-spec.md) | 行为规格主文档：四族行格式逐字段、请求/审计字段序、JSON schema、轮转与保留语义、trace-id 形态表、待验证清单 |
| [taxonomy.yaml](taxonomy.yaml) | 全量文件清单（evidence-index）：文件 → 服务 → 类别 → 格式 → 轮转 → 证据等级（87+ 文件全覆盖） |
| [evidence/](evidence/) | E4 因果样本与配置原文存档（9 份，见文末索引） |

## 一、logger 家族（四种写法，互不兼容）

| 家族 | 成员（tag） | 行格式 | 配置源 |
|---|---|---|---|
| Java 主服务族 | jfrt（artifactory）、jfac（access）、jfcfg、jftpl | `ts [tag ] [LVL ] [trace] [类缩:行] [线程] - msg` | `var/etc/<svc>/logback.xml`（运行时四份原文已存档） |
| Go 微服务族 | jffe/jfmd/jfevt/jfevd/jfcon/jfmel/jfob/jfomr/jfrou | 同构但 tag 无尾空格、调用点为 `file.go:line`、多「组件槽」、可带 span 尾巴 | system.yaml `<svc>.logging.*`（jftext/json 双格式） |
| jfbus | jfbus | 级别不补齐、全限定类名——族内独立 | 未取（无 etc 配置文件） |
| tomcat JULI | tomcat（两个 webapp 共写 var/log/tomcat/） | 文件=JULI 单行；console=恒 JSON | `app/<svc>/tomcat/conf/logging.properties` |

关键非对称（复刻高频踩坑点，详见 format-spec §5/§12）：
- jfrt 请求日志 reqLen 在 resLen **前**，jfac 及微服务族**相反**；
- 无请求体时 Java 族记 -1、Go 族记 0、frontend 记 `-`；
- frontend 请求时长是**毫秒小数（3 位）**，其余服务为毫秒整数；
- metadata 的 trace-id 32hex 左补零，frontend 恒空；
- jffe 与 router-traefik 的**文件内含 ANSI 色码**（族内唯二）。

## 二、文件类别语义分组

- **service**（`<svc>-service.log`）：应用主日志，人读。
- **request**（`<svc>-request.log` + router-request 特例 Traefik JSON）：入站 HTTP 请求审计线，管道分隔。
- **request-out / storage-request-out**：出站（外部服务或存储层）请求线。
- **audit / security-audit**：access-audit（令牌）、access-security-audit（C/D × TKN/PRS/LGN 结构化审计）、
  artifactory-access（jfrt 侧 ACCEPTED * 授权审计）。
- **traffic / request-trace**：流量统计与请求级 trace（活体未触发，行格式 UNKNOWN——见待验证清单）。
- **metrics / metrics_events / usage**：Prometheus exposition 文本（`名{标签} 值 epoch-ms`）；usage 域在
  `var/data/<svc>/usage/` 独立目录。
- **migration**（sha256/path-checksum/conan-v2）、**federation**、**binarystore**、**import-export**、
  **cleanup-audit**（JSON 行、数字序号轮转 1..1000/1GB）、**shadow**：低频专项作业日志。
- **container / db-diagnostics / event-bus / placeholder**：tomcat JULI、derby、jfbus 事件、dummy.log。

## 三、轮转体系分工（结论）

1. **进程内轮转承担一切常规文件**：
   - Java 族 = logback 双触发（25MB **或** 23h 先到先转）+ 时间戳归档名
     `archived/<base>.<UTC-ts>.log[.gz]`（%i 被时间戳替换而非计数器）、默认保留 10 份、
     compressLatest 语义决定最新归档裸/压；
   - Go 族 = rotation 五元组（25MB/10 文件（metadata 100）/365 天/压缩/留 1 未压）+ **连字符**归档名；
   - metrics/traffic/usage = 定长间隔（15min/6h）+ 大容量（1GB/100MB）变体；
   - tomcat = JULI 按日分文件不压缩。
2. **系统 logrotate 只管一件事**：`var/log/console.log`（聚合 console，`shared.logging.consoleLog.enabled`
   开启时才存在）。Docker 部署恒跳过——容器内 logrotate.conf 为 0 字节占位即此原因。启用部署中为
   hourly cron（55 分）+ daily/copytruncate/dateext stanza，参数取 `shared.logging.rotation.*`。

## 四、trace-id 贯穿链（E4 已证）

客户端请求 → router（Traefik JSON `TraceId`/`Uber-Trace-Id` 头）→ access-request / artifactory-request
（同 32hex）→ metadata-request（左补零 32hex）→ 各服务 service 日志槽（16/32hex 变宽）。
因果对样本：evidence/e4-probe-causal-pairs.md。

## 五、复刻优先级建议（供 conductor 排期）

1. P0：artifactory-request / access-request / router-request / access-security-audit / artifactory-access /
   service 族 jftext（Go+Java 两式）+ 三套轮转语义——外部可观察面最大。
2. P1：metrics 文本、request-out 族、tomcat JULI、审计事件词表扩充（待验证 #2/#3）。
3. P2：traffic/request-trace（格式 UNKNOWN，先解锁采集）、jfbus 事件负载、console JSON 聚合。

## evidence/ 存档索引

var-log-inventory.txt（87 项目录清单）、archived-and-tomcat-inventory.txt、format-samples-runtime.txt（各族
行样本原样）、e4-probe-causal-pairs.md（操作→日志行因果对，探针 4 组）、logback-artifactory.xml /
logback-access.xml / logback-topology.xml / logback-jfconfig.xml（运行时配置原文）、
tomcat-logging-properties.txt、system-template-logging-sections.txt、logrotate-installer-template.txt、
console-stream-samples.txt。
