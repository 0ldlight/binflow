# Artifactory 7.161.x 日志体系行为规格（format-spec）

> 置信度标注：`高` = 反编译(E1) + 运行时配置(E2/E3) + 活体(E4) 多源印证；`中` = 双源；
> `低` = 单源或推断。证据源版本：E1=7.161.24 反编译、E2/E4=7.161.20 运行实例、E3=7.161.16 安装包。
> 原样样本存档于 `docs/reverse/logging/evidence/`（下称 evidence/）。
> 本文档为 clean-room 行为规格：描述「当服务发生 X 时，日志文件 Y 产生格式 Z 的行」；
> 配置键名与日志文件名属外部可观察行为，可引用。

---

## 1. 体系总则

1. 单节点部署的全部日志文件位于 `<product-home>/var/log/`（容器内 `/opt/jfrog/artifactory/var/log`），
   按「服务-类别」命名：`<服务名>-<类别>.log`（如 `artifactory-request.log`）。目录实测 87 项（evidence/var-log-inventory.txt）。【高，E4】
2. 四个 logger 家族（写法互不相同，逐族规格见 §2~§5）：
   - **Java 主服务族**（jfrt/jfac/jfcfg/jftpl）：logback 文件 appender，格式由 `var/etc/<svc>/logback.xml` 定义（运行时配置原文四份已存档 evidence/logback-*.xml）。
   - **Go 微服务族**（jffe/jfmd/jfevt/jfevd/jfcon/jfmel/jfob/jfomr/jfrou）：进程内自带 jftext/json 双格式 logger，配置来自 system.yaml `<svc>.logging.*`。
   - **jfbus**（Java 事件总线）：格式族内独立（级别不补齐、全限定类名）。
   - **tomcat JULI**（两个 webapp）：容器级日志，写 `var/log/tomcat/`，按日分文件。
3. **console（stdout/docker logs）与文件是两套独立输出**：console 行带 ANSI 色码；文件行（除 jffe 与 router-traefik 两个特例）无色码。【高，E4】
4. 当 `shared.logging.enableJsonConsoleLogAppenders=true`（默认 false）时，Java 族在 console 增输出
   JSON 形态行（text 与 JSON 互补过滤）；默认关闭时 console 只有文本行。Go 族各服务有自己的
   `<svc>.logging.application.format: jftext|json` 开关；tomcat 的 console 恒为 JSON（见 §6）。【高，E1+E2+E4】
5. trace-id（uber-trace-id）贯穿：同一次客户端请求在 router-request（TraceId 字段）、access-request、
   artifactory-request、metadata-request 等日志中保持同一 32 hex 值（E4 因果对 evidence/e4-probe-causal-pairs.md
   探针 4：`0a4bbab079aeef7e04ce3661f216f447` 同链出现于 4 个文件）。【高，E4】

---

## 2. jftext 服务日志行格式（Java 族）

当 Java 服务（jfrt/jfac/jfcfg/jftpl）产生一条应用日志时，`<svc>-service.log` 增加如下格式的行
（jfrt 实测样本）：

```
2026-09-10T15:52:29.849Z [jfrt ] [INFO ] [817f125a9fe990520f707cd778978419] [.u.TomcatListeningDetector:185] [http-nio-8081-exec-4] - Tomcat connectors: [...]
```

逐字段（以空格分隔首三段，其余为方括号槽位）：

| # | 字段 | 格式规则 | 实测例 | 置信度 |
|---|---|---|---|---|
| 1 | 时间戳 | `yyyy-MM-dd'T'HH:mm:ss.SSS` UTC + 尾缀 `Z`（毫秒 3 位） | `2026-09-10T15:52:29.849Z` | 高 |
| 2 | 服务 tag | `[` + 4 字符 tag + 1 空格 + `]`（恒 6 字符宽） | `[jfrt ]`；tag 集：jfrt/jfac/jfcfg/jftpl/jfbus(§4) | 高 |
| 3 | 级别 | `%-5p` 左对齐补空格至 5 | `[INFO ]` `[WARN ]` `[ERROR]` | 高 |
| 4 | trace-id | `%-16` 最小宽 16：16 hex 短 id 原样；32 hex 长 id 打满 32；无 id 时 16 个空格 | `[817f...419]` / `[                ]` | 高 |
| 5 | 调用点 | 类名缩写 `:行号`，整体右截断+左补齐至 30：类缩写取全限定名**最后 3 段**，超长时左端以 `.` 截缩 | `[.u.TomcatListeningDetector:185]`（原 `...util.TomcatListeningDetector` 压缩） | 高 |
| 6 | 线程名 | `%-20.20` 补齐/截断至 20 | `[http-nio-8081-exec-4]` `[main                ]` | 高 |
| 7 | 分隔 | 空格 `-` 空格 | `-` | 高 |
| 8 | 消息 | 任意文本；异常栈为多行缩进块，跟在消息后 | — | 高 |

族内差异【高，E2 pattern + E4 样本】：
- jfac 文件行在字段 1 前有租户前缀转换符（单节点为空串，不占位）；字段 4 用自定义转换（实测输出 15~32 hex 不等宽度：`[f44175438aa0e125]` 15 hex、`[23d5449dbc5b3517268556a5f3ba12fe]` 32 hex）。【中，宽度规则未见源码】
- console 形态同构但：tag 槽带 ANSI 色（jfrt 绿粗、jfac 黄粗），级别槽按级别着色（INFO 蓝、WARN 黄、ERROR 红）。【高，E4 console-stream-samples.txt】
- 异常节流：同一 logger 每小时最多 5 次完整栈输出（默认 `log.exceptions.throttle.per-logger.limit=5`），
  全局节流默认关闭（limit=-1）；节流期栈被折叠。栈节流行为由 logback 属性
  `log.exceptions.throttle.*` 控制（配置键为外部可观察）。【高，E2 属性 + E1 策略类】
- 根级别 warn；`org.artifactory`/`org.jfrog` 族 info；jersey/spring warn、cxf error（jfrt logback 显式声明）。【高，E2】

## 2a. artifactory-import-export.log（jfrt 族内独立格式）

当导入/导出作业运行时，`artifactory-import-export.log` 行格式为：
`%date [级别] (logger前32字符:行号) 消息`——即字段 2 无服务 tag、字段 5 用圆括号包裹且取 logger 名前缀（非尾 3 段）。【高，E2 pattern】

---

## 3. jftext-go 服务日志行格式（Go 族）

当 Go 微服务产生一条应用日志时，`<svc>-service.log` 增加如下格式行（多两段式样本）：

```
2026-09-10T15:00:35.445Z [jfevt] [INFO ] [16547f9633dd200e] [platform_config_stream.go:105 ] [main                ] [                ] - Starting Platform Config stream Recv loop [access_client]
```

| # | 字段 | 格式规则 | 与 Java 族差异 | 置信度 |
|---|---|---|---|---|
| 1 | 时间戳 | 同 Java（UTC ms + Z） | 无 | 高 |
| 2 | 服务 tag | `[` + 4 字符 + `]`（**无尾空格**，恒 6 字符） | Java 是 tag+空格 | 高 |
| 3 | 级别 | 补齐至 5 | 无 | 高 |
| 4 | trace-id | 16 hex 或空 16 空格或 32 hex（jfmd 实测 32hex 左补零形态 `0000...283a297c113f3905`；jfomr 32hex 原值） | 宽度规则未统一 | 中（宽度规则低） |
| 5 | 调用点 | `<文件名>.go:<行号>` **右侧补空格至定宽**（实测各行补齐后等宽，目测 28~30；精确宽度未见源码） | Java 是类名缩写且左补齐 | 中 |
| 6 | 线程/协程槽 | 恒字面 `main` 补齐至 20（Go 单 main 语义） | — | 高 |
| 7 | **组件槽**（Go 族特有） | 方括号：有值时为组件/子 logger 名（`[access_client]` `[jfconnect-client-initializer]` `[onemodel_subgraph_registrar]`），无值时 16 空格；jfrou 恒为空 `[]`（不补齐）；**jfmd 与 jffe 无此槽** | Java 无此槽 | 高（存在性）；中（补齐规则） |
| 8 | 分隔+消息 | ` - ` + 消息；消息尾部可再跟组件标签重复与 `[spanId][<16hex>] [spanTraceId][<16hex>]`（jfmel/jfob 实测） | — | 高 |

特例（实测，E4）：
- **frontend（jffe）调用点槽打印字面 `frontend-service.log`**（非 go 文件:行号）——该服务 caller 信息关闭时的占位输出。【中，成因未定位】
- **frontend-service.log 与 router-traefik.log 文件内含 ANSI 色码**（`[31M[WARN ][39M]`，cat -v 可见）——文件流未剥色，族内唯一。【高，E4】
- jfmelt 行可携带双 span 尾巴：`... [jfconnect-client-initializer] [spanId][622264f7a9e75dde] [spanTraceId][257a98f191d7531e]`。【高，E4】

级别体系：error/warning(或 warn)/info/debug/trace，运行时可调（system.yaml `<svc>.logging.application.level`，注释声明「configurable at runtime」，即支持运行时日志级别 API 调整——与 jfrt 的 Admin 调级 API 对应）。【高，E3 模板注释】

---

## 4. jfbus 行格式（族内独立）

当 jfbus 事件总线记录应用日志时，`jfbus-service.log` 行格式：
`ts [jfbus] [级别（**不补齐**，如 [INFO]）] [trace-id（可空）] [全限定类名:行号（**不缩写**）] [线程名（scheduler 池名，不补齐）] - 消息`。
多行消息（如续行 ` - Will not delete...`）原样保留。【高，E4 样本；轮转策略 UNKNOWN】

事件分片文件 `jfbus-ack-events.log`/`jfbus-consume-events.log`/`jfbus-publish-events.log` 内容为逐行
JSON 对象（实测 `{}` 空对象占位，事件负载未触发）。【中，E4；负载 schema 待验证】

---

## 5. 请求日志（request 族，竖线分隔）

当客户端请求到达某服务时，`<svc>-request.log` 追加一行**竖线分隔**记录。各服务字段序**不完全一致**——
复刻时必须逐服务对齐：

### 5.1 artifactory-request.log（jfrt，11 字段）【高，E1 字段序 + E4 逐字段验证】

```
ts|trace-id|客户端IP|用户名|METHOD|路径|状态码|reqLen|resLen|durationMs|User-Agent
2026-09-10T16:01:56.582Z|27f37f7912d43e17c047bc29420825ae|192.168.65.1|admin|PUT|/example-repo-local/audit-probe-20260911000113/hello.txt|201|25|0|2103|curl/8.7.1
```

- ts：UTC ISO-8601 毫秒 + Z。trace-id：32 hex（或 16 短 id）。
- 用户名取值（E4 实测三分态）：认证用户名（`admin`）/ 匿名放行 `anonymous`（200）/ 认证失败 `non_authenticated_user`（401）。
- 路径为 **router 剥离 /artifactory 前缀后**的路径（探针 4：外部 `/artifactory/api/repositories/...` 记为 `/api/repositories/...`）。
- reqLen=请求体字节数，**无请求体时为 -1**（GET/DELETE 实测 -1，PUT=正文字节数）；
  resLen=响应体字节数（无体=0，如 201/204；流式/未知=-1）。
- durationMs=整数毫秒。User-Agent 原样。
- 内部探活（router→jfrt readiness）同样记录，用户名 `non_authenticated_user`、UA `JFrog-Router/v7.729.47`。

### 5.2 access-request.log（jfac，11 字段，**req/res 顺序与 jfrt 相反**）【高，E2 pattern + E4】

```
ts|trace-id|IP|用户名|METHOD|路径|状态码|resLen|reqLen|durationMs|User-Agent
2026-09-10T16:01:29.452Z|154a78bd603c114c3364566b8f275a80|127.0.0.1|anonymous|GET|/access/api/v1/system/ping|200|2|-1|7|JFrog Access Java Client/...
```

- 无请求体时 reqLen=-1（与 jfrt 一致）；trace-id 实测 16 hex（本服务自定义转换输出）。
- **gRPC 调用行**（E4 实测）：IP 槽为 `/127.0.0.1:50920` 形态、方法槽为 `--UNA`（未认证标记）或
  gRPC 方法全路径、状态槽为 gRPC 状态码（0=OK）、路径槽为 `com.jfrog.access.v1.xxx.Resource/Method`：
  `2026-09-10T16:02:16.829Z|00000000000000002f882f91e3cb38c3|/127.0.0.1:50920|jftpl@01m...|--UNA|com.jfrog.access.v1.authentication.AuthenticationResource/VerifyToken|0|-1|-1|3|grpc-java-netty/1.82.4`。【中，--UNA 语义推断】
- access 亦记录自身 webapp 静态资源请求（`/access/webapp/js/...`）。

### 5.3 Java 微服务族（jfcfg/jftpl，11 字段）【高，E2 pattern + E4】

与 5.2 同构（res 在前 req 在后；trace 16 hex；无独立租户槽——单节点 SaaS 分支不生效）。

### 5.4 Go 族 request（多数 11 字段；三个特例）【高，E4；精确宽度规则中】

```
ts|trace-id|IP|用户名|METHOD|路径|状态码|resLen|reqLen|durationMs|User-Agent[|附1|附2]
```

- **metadata-request**：trace-id 为 32 hex **左侧补零**（`00000000000000005cad3e6c406c06d1`）。
- **event-request / onemodel-request**：行尾**多两个空字段**（`...|UA||`，13 字段；空字段语义未证实——
  形似 request-id/extra 预留）。【中】
- **frontend-request**：trace-id 槽**恒空**；reqLen 槽用 `-`（非 -1）；duration 为**毫秒小数**
  （`340.264`，3 位小数；按量级推敲为 ms——44µs 不可能完成鉴权 401 路径，1931.401ms 与同类
  请求 rt 侧耗时同量级）——族内唯一非整数毫秒。【中，量级推断，未见源码】
- **jfbus-request / jfbus-receive-polling**：11 字段，trace 32hex，用户名可为内部消费者主体
  （`jfbus-client-default-tenant-id`）。
- 无请求体时 Go 族 reqLen 实测 **0**（非 -1）——与 Java 族 -1 不同。【高，E4】

### 5.5 router-request.log（Traefik JSON 访问日志）【高，E4】

当任意请求（外部或内部服务间）经过 router 时，`router-request.log` 追加一行 JSON（字段按首字母序）：

```json
{"ClientAddr":"192.168.65.1:29226","DownstreamContentSize":61,"DownstreamStatus":200,"Duration":4854119812,"Overhead":746372,"RequestMethod":"PUT","RequestPath":"/artifactory/api/repositories/audit-probe-repo-20260911","ServiceAddr":"localhost:8081","SpanId":"9f64071f10112c58","StartUTC":"2026-09-10T16:04:02.291926158Z","TraceId":"d8efa380619a54b8b10627015f9693b3","level":"info","msg":"","request_User-Agent":"curl/8.7.1","time":"2026-09-10T16:04:07Z"}
```

- Duration/Overhead 单位**纳秒**；StartUTC 纳秒精度；time 为秒级完成时刻。
- 上游带 `Uber-Trace-Id` 头时行内出现 `request_Uber-Trace-Id`（`<trace>:<span>:<parent>:<flags>` 四段冒号格式）。
- 请求头以 `request_<Header名>` 透传（仅部分头，实测 UA 与 Uber-Trace-Id）。
- 文件权限 600（其余日志 640）。

### 5.6 request-out 族（出站请求）【高，E2 pattern + E4】

| 文件 | 字段序（竖线） |
|---|---|
| access-request-out.log | ts\|trace\|URL\|METHOD\|状态\|res消息\|reqLen\|resLen\|durationMs |
| jfconfig-request-out.log | 同上（E2） |
| topology-request-out.log | ts\|trace(32hex补零)\|URL\|METHOD\|状态\|消息(OK)\|durationMs（7 字段） |
| jfconnect-request-out.log | ts\|trace\|完整URL(外部域名)\|METHOD\|状态\|resLen(-1)\|reqLen\|durationMs |
| artifactory-request-out.log | E2 定义 11 字段（含 repo/method/url+sha1/status/双 length/duration） |
| artifactory-storage-request-out.log | ts\|trace\|repo\|`internal`\|method\|url/sha1\|status\|len\|len\|duration（存储层内部出站） |
| evidence-request-out.log | Go 格式异常实测：秒级 ts、`--UNA`、`%!s(int=794)`、`%!d(string=)`、`%!s(MISSING)` ——**Go 格式化动词错配原样落盘**（复刻需注意：这是上游真实行为，非采集误差）【高，E4】 |

### 5.7 request-trace（jfrt）与 traffic（jfrt）

- `artifactory-request-trace.log`：行带日期前缀（`%date %m`），25MB 轮转，默认空（trace 级请求诊断未开）。【中，E2；行内容 UNKNOWN】
- `artifactory-traffic(-v2/-v3/-xray-traffic)`：流量统计专用；**活体文件全部 0 字节**，行格式 UNKNOWN。
  需要证据：开启流量收集（admin UI Traffic / `artifactory.traffic.` 配置）后取样。【UNKNOWN】

---

## 6. tomcat 容器日志（JULI）与 console JSON

- 两个 webapp（artifactory:8081、access:8040）各持 `logging.properties`，把容器级日志写**同一目录**
  `var/log/tomcat/`，以文件名前缀区分：`tomcat-catalina-`（jfrt 容器）、`tomcat-localhost-`（jfrt localhost 子日志）、
  `tomcat-access-catalina-`（jfac 容器）、`tomcat-access-localhost-`（jfac 子日志）+ `YYYY-MM-DD.log` 日期后缀。
  **按日新文件、不压缩、无保留上限配置**（实测 5 天文件全在）。【高，E2+E4】
- 文件行格式 = JULI 单行格式：`10-Sep-2026 14:58:42.964 INFO [main] org.apache.coyote.AbstractProtocol.start 消息`
  （`dd-MMM-yyyy HH:mm:ss.SSS 级别 [线程] logger消息`）。【高，E4】
- **console（stdout）形态恒为 JSON**（与 enableJsonConsoleLogAppenders 无关）：ConsoleHandler 使用
  SimpleFormatter，其格式串被模板定义为 JSON 模板：
  `{"log_name":"tomcat-catalina.log","app":{"datetime":"...","service":"tomcat","loglevel":"WARNING","class":"org.apache...","message":"..."}}`。【高，E2 配置原文 + E4 console 样本】
- webapp 内应用日志**不**走 JULI（由各服务 logback 接管），JULI 只承载容器/启动器层。

## 6a. Go 族 console JSON schema（当 `<svc>.logging.application.format: json` 时）【高，E4】

```json
{"log_name":"evidence-service.log","app":{"datetime":"2026-09-10T14:54:15.515796739Z","service":"jfevd","loglevel":"info","span.id":"","trace.id":"","traceid":"348c6fc9bbb00e42","class":"access_client_thin.go","line":"131","thread":"main","tenantid":"","message":"..."}}
```

- datetime：Go 服务**纳秒精度**（tomcat 为毫秒）；loglevel 小写；class=go 文件名；line=行号字符串；
  message 为 JSON 转义文本（多行配置 dump 原样嵌入 \n）。
- 实测：evidence 默认 `format: json` + `console: true`；jfrt/jfac/jfrou console 为文本。即 docker logs
  中 JSON 行与文本行**混合存在**。【高，E4】

---

## 7. 审计日志

### 7.1 access-audit.log（令牌审计）【高，E2+E4】

行格式：`ts [AUDIT] 消息`（消息为自由文本键值对）。当用户/服务创建、刷新、删除令牌时：
```
2026-09-10T15:53:15.562Z [AUDIT] Token created by 'jffe@01m...': tokenId='3373780b-...', issuer='jffe@01m...', subject='jffe@01m.../users/jffe@01m...', expiry=1789056195561, refreshable=false
```
动作词实测：Token created / Token refreshed（deleted 推断存在，未在窗口捕获——低）。消息含 tokenId、issuer、subject、expiry（epoch-ms）、refreshable。

### 7.2 access-security-audit.log（安全事件审计，8 字段）【高，E2 pattern + E4】

```
ts|trace-id(32hex)|操作者IP|操作者|登录主体|实体名|事件类型|实体类型|变更JSON
2026-09-10T16:04:10.516Z|0a4bbab079aeef7e04ce3661f216f447|UNKNOWN|UNKNOWN|jfrt@01m1...|default:audit-probe-repo-20260911|D|PRS|{"removed":{"projectKey":"default","name":"audit-probe-repo-20260911","type":"repo"}}
```

- 事件类型（字段 7，E4 全量统计 131+8+2+1+1+1 行）：`C`=创建、`D`=删除；实体类型（字段 8）：`TKN`=令牌、
  `PRS`=平台资源（仓库/清理策略等）、`LGN`=登录（配 A/D 或 A=Accepted）。【高，E4 计数】
- 操作者 IP/操作者两槽在服务间调用时为 `UNKNOWN`（操作发起者为服务账号而非人）。
- 变更 JSON：键 `added`/`removed`，内为实体属性快照（敏感值脱敏）。
- **E4 关键观察**：管理员 REST 创建仓库（PUT /api/repositories，200 成功）**不产生** PRS_C 审计行；
  删除产生 PRS_D，且 acting principal 记录为 jfrt 服务账号。此条为运行观察，未见官方文档背书。【中】

### 7.3 artifactory-access.log（jfrt 侧授权审计）【高，E2 pattern + E4】

行格式：`ts [trace-id(32hex补宽)] [事件] 消息`。当认证/授权决策产生时：
```
2026-09-10T16:01:56.239Z [27f37f7912d43e17c047bc29420825ae] [ACCEPTED DEPLOY] example-repo-local:audit-probe-20260911000113/hello.txt  for client : admin / 192.168.65.1
```
- 事件词实测：`ACCEPTED LOGIN`（带尾缀 `[token]` 凭据类型）、`ACCEPTED DEPLOY`、`ACCEPTED DOWNLOAD`、`ACCEPTED DELETE`。
- 制品操作消息 = `仓库:路径` + 定宽两空格 + `for client : 用户 / IP`；登录消息 = `for client : 用户 / IP [凭据类型]`。
- 系统内部清理亦记录（`_system_` 主体）。拒绝事件（DENIED）未在窗口触发——推断存在。【低】

---

## 8. metrics 日志（Prometheus 文本 exposition）【高，E2+E4】

- `<svc>-metrics.log` / `<svc>-metrics_events.log` / usage 域 `usage-metrics.log` 行格式：
  `指标名{标签集} 值 <epoch-ms 时间戳>`（Prometheus exposition，尾部多一列抓取时戳）。
- 前缀约定：jfrt 存储/制品指标 `jfsh_*`/`jfrt_*`；jfcfg `jfcfg_*`（含 `tenant_id="single_tenant"` 标签）；
  Go 通用 `app_*`（histogram bucket `le=` 标签 + node_id/success/task_key）。
- `<svc>-metrics_events.log`：低频事件型指标（如 `jfrt_artifacts_gc_next_run_seconds`）。

## 9. derby 诊断日志【高，E4】

`derby.log` 与 `topology-derby.log` 为内嵌 Derby 的 stream error 文件（`derby.stream.error.file` 指定路径）：
非轮转纯文本，首部为版本启动 banner（版本/实例 UUID/数据库目录/JVM 属性），后随错误段。仅在 Derby 事件时追加。

## 10. dummy.log【中，E4+E1】

Go 族进程初始化早期的占位日志文件（Go 运行时串表含该常量名），0 字节常驻，无轮转。创建时机细节 UNKNOWN
（需要证据：strace 容器启动期 open 调用）。

---

## 11. 轮转与保留语义（复刻关键）

### 11.1 Java 族：logback 双触发 + 时间戳归档名【高，E1 策略实现 + E2 配置 + E4 归档名验证】

- **触发**：`25MB`（各 appender MaxFileSize 显式；策略类默认 10MB）**或** 距上次轮转 `23 小时`
  （SizeAndIntervalTriggeringPolicy 默认 interval=82800000ms，阈值 60s）——先到先转。metrics/traffic 族
  用显式 intervalMs（15min=900000 / usage 6h=21600000）替代 23h。
- **归档名**：`var/log/archived/<base>.<UTC时间戳 yyyy-MM-dd'T'HH-mm-ss.SSS>.log[.gz]`——
  FileNamePattern 中 `%i` 被**轮转时刻 UTC 时间戳替换**（非计数器）。E4 归档名逐字验证
  （`access-service.2026-09-03T16-27-23.706.log.gz`）。
- **保留**：默认 maxIndex=10（保留最近 10 个归档，超限删最旧；上限 200）。traffic v2/v3=500、
  metrics=150、federation-metrics=1、cleanup-audit=1000（数字序号版策略，%i=1..N 递增，非时间戳）。
- **压缩**：`compressLatest=false`（默认）→ 最新归档裸 .log，**下一次轮转时**才把上一份压成 .gz
  （E4 归档目录同时存在当日 .log 与历史 .gz，与此语义吻合）；`compressLatest=true` → 立即压缩。
- 服务重启后以**最新归档文件的修改时间**续接 interval 基准（避免重启即轮转）。
- access 族全部 `compressLatest=false`；jfrt 多数 appender true（service/request 默认 false——
  以 evidence/logback-artifactory.xml 逐 appender 为准）。

### 11.2 Go 族：rotation 五元组【高，E3 模板 + E4 归档名验证】

- 配置键（system.yaml `<svc>.logging.application|request|requestOut|metrics.rotation.*`）：
  `maxSizeMb: 25`、`maxFiles: 10`（metadata 模板默认 100）、`maxAgeDays: 365`（优先于 maxFiles）、
  `compress: true`、`keepLastDecompressed: 1`。
- 归档名：`var/log/archived/<base>-<时间戳>.log[.gz]`（**连字符**连接，与 Java 族点号相区分；E4 验证
  `event-request-2026-09-03T17-27-42.368.log.gz`）；时间戳同为 UTC `yyyy-MM-dd'T'HH-mm-ss.SSS`。
- keepLastDecompressed=1：最近 1 份归档不压缩（E4 归档目录最新为 .log、更旧为 .gz，吻合）。

### 11.3 tomcat：按日分文件【高，E2+E4】

JULI FileHandler 每日新文件（`<prefix>YYYY-MM-DD.log`），旧文件留原样不压缩；未配置保留天数（实测不清理）。

### 11.4 router-traefik【中，E3】

system.yaml `router.logging.traefik`（level/format/caller）+ rotation 五元组同 Go 族；实测文件含 ANSI。

### 11.5 logrotate（系统级，仅管 console.log）【高，E3 脚本 + E4 空配置验证】

- **唯一职责**：轮转 `var/log/console.log`（仅当 `shared.logging.consoleLog.enabled=true` 聚合 console 时存在）。
  **Docker 部署恒跳过**（console 重定向关闭以省性能，安装脚本显式短路）——故容器内
  `var/etc/logrotate/logrotate.conf` 为 **0 字节占位**（E4 逐字验证）。macOS 不配置。
- 启用的部署中写入的 stanza（installerCommon.sh 模板，逐条）：
  `daily / missingok / copytruncate / rotate <shared.logging.rotation.maxFiles 默认10> /
  [compress+delaycompress 当 shared.logging.rotation.compress=true 默认开] / notifempty /
  olddir archived / dateext / extension .log / dateformat -%Y-%m-%d-%s / size <maxSizeMb 默认25>M`。
- 执行方式：crontab `55 * * * *`（每小时第 55 分）运行随包二进制
  `<home>/app/third-party/logrotate/logrotate <conf> --state <home>/var/etc/logrotate/logrotate-state`。
- **其余全部日志文件的轮转都由各进程内完成**（logback/Go 内建/JULI），与系统 logrotate 无关——
  这是「logback rolling vs logrotate 分工」的边界结论。

### 11.6 聚合 console（console.log）【高，E2+E3】

`shared.logging.consoleLog.enabled=true` 时各服务 stdout 聚合至 `var/log/console.log`（格式
`shared.logging.consoleLog.format: jftext`）；默认关闭。此文件即 11.5 logrotate 的唯一对象。

---

## 12. trace-id 形态汇总（复刻对齐表）【高，E4 交叉验证】

| 日志 | 形态 | 样本 |
|---|---|---|
| router-request (TraceId) | 32 hex 原值 | `0a4bbab079aeef7e04ce3661f216f447` |
| artifactory-request | 32 hex（或 16 短） | 同上 |
| access-request | 16 hex（自定义转换，实测 15~32 变宽） | `154a78bd603c114c` |
| metadata-request | 32 hex **左侧补零** | `00000000000000000a4bbab079aeef7e` |
| event/onemodel/jfconfig/evidence/jfcon/jfmel/jfob/jfbus-request | 16 hex | `50d88ace3d23cece` |
| frontend-request | 恒空 | `` |
| 服务日志槽（jfrt/jfac 文件与 console） | 16 或 32 hex，空时 16 空格 | `[                ]` |

---

## 13. 待验证清单（低置信度/UNKNOWN 汇总）

| # | 条目 | 为何未知 | 需要的证据 |
|---|---|---|---|
| 1 | artifactory-traffic / -v2 / -v3 / -xray-traffic 行格式 | 活体文件 0 字节，未触发流量收集 | 在 admin UI 开启 Traffic 收集后取样（或反编译 traffic entry writer 输出序列化） |
| 2 | access-security-audit 完整事件词表（除 C/D、TKN/PRS/LGN 外） | 窗口内只触发 6 种组合 | 构造用户/组/权限/密码策略变更等操作逐类取样 |
| 3 | artifactory-access DENIED/REJECTED 事件词 | 窗口内无拒绝成功样本 | 无权限请求 + 错误凭据探针 |
| 4 | jfevt/jfomr request 行尾两个空字段语义 | 恒空，无非空样本 | 触发带 request-id 的事件路径（如 UI 长轮询） |
| 5 | access-request `--UNA` 槽语义（gRPC 未认证标记 vs 其他） | 仅见未认证 grpc 行 | 构造已认证 gRPC 调用对比 |
| 6 | Go 族调用点槽精确补齐宽度、组件槽补齐规则 | Go 源码未在反编译树（仅伪码） | 反编译产物含 jf-go-commons logger 源，或生成足量样本统计列宽 |
| 7 | jfbus 三事件文件负载 schema | 恒 `{}` | 触发事件订阅（webhook/分发）后取样 |
| 8 | jfbus 各文件轮转策略 | 无 etc/logback 文件可读 | 在 app/jfbus 安装目录定位 logback 配置或反编译 jfbus 模块 |
| 9 | dummy.log 创建时机 | 0 字节无内容 | 启动期 strace/lsof 观测 |
| 10 | frontend 调用点槽为何打印字面日志文件名 | caller=false 占位行为的实现未读 | Go 侧 logger caller 关闭分支伪码确认 |
| 11 | jfac trace 转换 15 hex 样本的成因（`f44175438aa0e125`） | 单样本奇长 | 多样本统计 + access 日志转换器伪码 |
| 12 | 仓库创建无 PRS_C 审计是否跨版本稳定 | 单版本单实例观察 | 7.161.16/24 上重复探针 4 |
| 13 | tomcat JULI 归档保留是否真的无上限 | 5 天样本全在，未见证清理 | 长期实例（>1 月）核对 tomcat/ 目录 |
| 14 | evidence-request-out 的 Go 格式错配（%!s/%!d/MISSING）是否上游缺陷常态 | 仅 4 行样本 | 更长窗口采样确认稳定性 |

---

## 14. 证据出处索引

- E4 活体：evidence/e4-probe-causal-pairs.md（探针因果对）、format-samples-runtime.txt（各族行样本）、
  var-log-inventory.txt + archived-and-tomcat-inventory.txt（目录全清单）、console-stream-samples.txt。
- E2 运行时配置：evidence/logback-{artifactory,access,topology,jfconfig}.xml、tomcat-logging-properties.txt、
  system-template-logging-sections.txt。
- E3 安装包：evidence/logrotate-installer-template.txt（installerCommon.sh 节选）。
- E1 反编译（clean-room 只读，结论以行为语句呈现）：jfrog-logging 模块（轮转/触发策略语义）、
  artifactory-traffic 模块（request 行字段序）、artifactory-common（console 双过滤器开关）、
  jfrog-commons-logging-extra（jftext/json 配置模板族）、backend-go（Go 串表佐证 dummy.log/archived/字段名）。
