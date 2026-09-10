# E4 主动取样：操作 → 日志行因果对（2026-09-10 16:01–16:05 UTC，实例 7.161.20）

取样方法：对 http://localhost:8082 依序发最小请求集，每步后从容器内 var/log 取对应新增行。
基线行号（操作前）：artifactory-request=1092, access-request=2349, access-audit=132,
access-security-audit=136, artifactory-access=153, artifactory-service=3548。
凭据仅用于命令行，已从样本中省略（本例用户均为 admin/anonymous/non_authenticated_user）。

## 探针 1：GET /artifactory/api/system/ping（无凭据，200）

客户端命令：`curl http://localhost:8082/artifactory/api/system/ping`

| 日志文件 | 新增行（原样） |
|---|---|
| artifactory-request.log | `2026-09-10T16:01:29.461Z\|154a78bd603c114c3364566b8f275a80\|192.168.65.1\|anonymous\|GET\|/api/system/ping\|200\|-1\|2\|54\|curl/8.7.1` |
| access-request.log | `2026-09-10T16:01:29.452Z\|154a78bd603c114c3364566b8f275a80\|127.0.0.1\|anonymous\|GET\|/access/api/v1/system/ping\|200\|2\|-1\|7\|JFrog Access Java Client/7.189.4 88904900  Artifactory/7.161.12 86112900` |

要点：
- 同一 `154a78bd603c114c3364566b8f275a80` trace-id 同时出现在 access-request 与 artifactory-request（router 传播）。
- ping 无凭据 → 用户名记 `anonymous`（HTTP 200 场景）；`res.contentLength=2`（"OK"）。
- jfrt 侧由内部 Access Java Client UA 发起对 access 的鉴权前置调用，故 access-request 中 ip=127.0.0.1。

## 探针 2：制品 PUT → GET → DELETE（example-repo-local/audit-probe-20260911000113/hello.txt，正文 25 字节）

| 步骤 | artifactory-request.log 新增行 | artifactory-access.log 新增行 |
|---|---|---|
| PUT 201 | `2026-09-10T16:01:56.582Z\|27f37f7912d43e17c047bc29420825ae\|192.168.65.1\|admin\|PUT\|/example-repo-local/audit-probe-20260911000113/hello.txt\|201\|25\|0\|2103\|curl/8.7.1` | `2026-09-10T16:01:56.239Z [27f37f7912d43e17c047bc29420825ae] [ACCEPTED DEPLOY] example-repo-local:audit-probe-20260911000113/hello.txt  for client : admin / 192.168.65.1` |
| GET 200 | `2026-09-10T16:01:58.008Z\|90dbd9a6e2fc75e01a42a6f100dd4056\|192.168.65.1\|admin\|GET\|/example-repo-local/audit-probe-20260911000113/hello.txt\|200\|-1\|25\|1281\|curl/8.7.1` | `2026-09-10T16:01:57.643Z [90dbd9a6e2fc75e01a42a6f100dd4056] [ACCEPTED DOWNLOAD] example-repo-local:audit-probe-20260911000113/hello.txt  for client : admin / 192.168.65.1` |
| DELETE 204 | `2026-09-10T16:01:59.157Z\|996890b2269b6cd765aade86d0f68872\|192.168.65.1\|admin\|DELETE\|/example-repo-local/audit-probe-20260911000113/hello.txt\|204\|-1\|0\|1011\|curl/8.7.1` | `2026-09-10T16:01:58.731Z [996890b2269b6cd765aade86d0f68872] [ACCEPTED DELETE] example-repo-local:audit-probe-20260911000113/hello.txt  for client : admin / 192.168.65.1` |

要点：
- 字段 8=reqContentLength（PUT=25 即上传字节数；GET/DELETE 无请求体时=-1），字段 9=resContentLength
  （PUT 201 无响应体=0；GET=25；DELETE 204=0）。与反编译 RequestLogger 字段序一致（E1+E4 双源）。
- 每个操作各生成独立 trace-id，request 日志与 access 授权日志同 id。
- event-request.log / evidence-request.log / metadata-request.log 在此三步中零新增（部署/删除事件走
  jfbus 内部总线，不打 HTTP 请求日志）。

## 探针 3：无凭据 GET /artifactory/api/system/version（期待 401，实测 401）

| 日志文件 | 新增行 |
|---|---|
| artifactory-request.log | `2026-09-10T16:04:02.167Z\|bd43778dea1ada3abfdaf92c1f5dd9bd\|192.168.65.1\|non_authenticated_user\|GET\|/api/system/version\|401\|-1\|0\|12\|curl/8.7.1` |

要点：401 时用户名记 `non_authenticated_user`（与 200 匿名场景的 `anonymous` 不同）。

## 探针 4：PUT + DELETE /artifactory/api/repositories/audit-probe-repo-20260911（最小 local repo JSON，正文 42 字节）

客户端：`PUT {"rclass":"local","packageType":"generic"}}` → 200；`DELETE` → 200。

| 日志文件 | 新增行 |
|---|---|
| artifactory-request.log (PUT) | `2026-09-10T16:04:07.141Z\|d8efa380619a54b8b10627015f9693b3\|192.168.65.1\|admin\|PUT\|/api/repositories/audit-probe-repo-20260911\|200\|42\|61\|4838\|curl/8.7.1` |
| artifactory-request.log (DELETE) | `2026-09-10T16:04:12.072Z\|0a4bbab079aeef7e04ce3661f216f447\|192.168.65.1\|admin\|DELETE\|/api/repositories/audit-probe-repo-20260911\|200\|-1\|206\|3769\|curl/8.7.1` |
| access-security-audit.log (DELETE) | `2026-09-10T16:04:10.516Z\|0a4bbab079aeef7e04ce3661f216f447\|UNKNOWN\|UNKNOWN\|jfrt@01m1hjrrqqyxh90anb2daw1avp\|default:audit-probe-repo-20260911\|D\|PRS\|{"removed":{"projectKey":"default","name":"audit-probe-repo-20260911","type":"repo"}}` |
| artifactory-access.log (DELETE) | `2026-09-10T16:04:11.715Z [0a4bbab079aeef7e04ce3661f216f447] [ACCEPTED DELETE] audit-probe-repo-20260911:  for client : admin / 192.168.65.1` |
| metadata-request.log | `2026-09-10T16:04:11.861Z\|00000000000000000a4bbab079aeef7e\|127.0.0.1\|jfrt@01m1hjrrqqyxh90anb2daw1avp\|DELETE\|/api/v1/path/audit-probe-repo-20260911\|404\|45\|0\|111\|JFrog Metadata Java Client/1.0.1` |
| router-request.log (PUT, Traefik JSON) | `{"ClientAddr":"192.168.65.1:29226","DownstreamContentSize":61,"DownstreamStatus":200,"Duration":4854119812,"Overhead":746372,"RequestMethod":"PUT","RequestPath":"/artifactory/api/repositories/audit-probe-repo-20260911","ServiceAddr":"localhost:8081","SpanId":"9f64071f10112c58","StartUTC":"2026-09-10T16:04:02.291926158Z","TraceId":"d8efa380619a54b8b10627015f9693b3","level":"info","msg":"","request_User-Agent":"curl/8.7.1","time":"2026-09-10T16:04:07Z"}` |
| router-request.log (jfrt→metadata 内部) | `{"ClientAddr":"127.0.0.1:50950","DownstreamContentSize":45,"DownstreamStatus":404,"Duration":121005471,"Overhead":690490,"RequestMethod":"DELETE","RequestPath":"/metadata/api/v1/path/audit-probe-repo-20260911","ServiceAddr":"localhost:8086","SpanId":"be282e86292d513f","StartUTC":"2026-09-10T16:04:11.744304538Z","TraceId":"0a4bbab079aeef7e04ce3661f216f447","level":"info","msg":"","request_Uber-Trace-Id":"0a4bbab079aeef7e04ce3661f216f447:8701eb77110c2ebf:8701eb77110c2ebf:0","request_User-Agent":"JFrog Metadata Java Client/1.0.1","time":"2026-09-10T16:04:11Z"}` |

要点：
- 仓库删除产生 access-security-audit `D|PRS` 行，acting principal 为 **jfrt 服务账号**（非发起 admin）——
  审计的是 jfrt→access 的资源登记删除；trace-id 与 REST 请求同链。
- **仓库创建（PUT /api/repositories，200）未产生任何 access-security-audit 行**（窗口内仅删除有 D|PRS；
  全文件仅 1 条 PRS_C，来自启动期 system_repo_cleanup_policy）。观察结论，非官方文档行为。
- metadata-request 的 trace-id 为 32 位左侧补零形态 `00000000000000000a4bbab079aeef7e`（对 64bit id
  补零到 32 hex），与 router 的 32hex 原值同链。
- jfrt 删除仓库时主动调 metadata 清路径（404 亦记录）。

## 收尾清理

- DELETE hello.txt 已 204；audit-probe-repo-20260911 已 DELETE 200；复查 var/log 与 storage 无 audit-probe 残留
  （artifactory-access 尾部 `_system_` 的 GC 置空目录删除属系统例行，与探针无关）。
