# QA 报告 T-43 — M2 协议矩阵 + M1 回归基线（PRD §8 剧本 1~5/7/10）

- role: qa-engineer
- 日期: 2026-08-19
- 票据: T-43 [P0]（AC 全文见 reports/agents/T-32.md T-43 节）
- 验收口径: docs/prd/milestone-2.md **v1.1**（D 序列 + E-26 反转 + tags:null 断言注记 + D04c）
- 被测对象: commit **b30016c**（`chore: sprint 122 - T-40 done (M2 dev complete)`），`make build` 产物 16.74 MB
- 环境: darwin/amd64，Go 1.26.5，curl 8.x + jq 1.7.1 直打真栈（三实例：64743 匿名开 / 64744 匿名关 / 64745 kill-9 用），临时数据目录 `/tmp/t43/data-{a,b,c}`
- **隔离口径注记**: 派发时主仓工作树带 T-52 在途改动（`internal/repo/service.go`/`docker_test.go` 已修改）。为可复现起见，QA 在 **b30016c 的独立 git worktree**（`/tmp/t43/src`）构建与跑测，未受在途改动影响；T-52 修复验证归其自己的票。
- **客户端口径注记**: 本机 docker daemon 无法配 insecure-registries（历史票同因），本票按派单要求 **curl 直打协议**；D05/docker CLI 级验证归 T-44。

## 总结论: **PASS（附 2 个 P2 缺陷 + 1 个 P1-watch 测试 flake + 3 项 PRD 口径勘误）**

- 场景行合计 **82**：**80 PASS / 2 FAIL**（FAIL 均为 P2 级，不阻塞 M2 P0/P1 AC 语义）。
- make 三件套 + 零 CGO + 全仓 test：绿（全仓第二跑 12/12 ok；第一跑 1 例 flake，见 F1）。

---

## 剧本 1：M1 回归基线（FR-7-AC2）

### 工程基线

| # | 检查 | 结果 | 证据 |
|---|---|---|---|
| 1.1 | `make build` | ✅ | `bin size: 16.74 MB (bin/binflow-server)`，退出码 0 |
| 1.2 | `make test`（race 全仓） | ✅（附 F1） | 第一跑 11/12 ok + `TestV2ManifestConcurrentTagOverwrite` FAIL（见 F1）；**第二跑 12/12 全 ok RC=0**（storage 232s / repo 156s / httpapi 210s→二跑 54.8s / adapter/docker 52~68s） |
| 1.3 | `make lint` | ✅ | `golangci-lint v2.12.2 run` → `0 issues.` |
| 1.4 | `go vet ./...` / `gofmt -l cmd internal` | ✅ | 均零输出 |
| 1.5 | `CGO_ENABLED=0 go build ./...` | ✅ | 退出码 0（零 CGO 基线维持） |
| 1.6 | `go mod tidy` | ✅ | go.mod/go.sum 无漂移 |

### M1 C 序列 P0 复跑（T-18 场景 3/4 基线，10MB 随机文件 sha256 对账）

| # | 命令 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 1.7 | C03 建仓 | 200 纯文本 | `200` `Successfully created repository 'generic-local'`（CT text/plain） | ✅ |
| 1.8 | C05 列仓 | key+description/type/packageType/url | key 命中、字段全集在 | ✅ |
| 1.9 | C06 查仓 | local/generic | `rclass=local packageType=generic` | ✅ |
| 1.10 | C07 上传 | 201+sha256+size 字符串+createdBy | `201`；sha256 一致；`"size":"10485760"`（字符串）；`createdBy=admin` | ✅ |
| 1.11 | C08 下载 | 200+三 checksum 头+ETag=sha1 无引号 | sha256 一致；X-Checksum ×3 全对；`Etag: <sha1>`；Last-Modified/Accept-Ranges 在 | ✅ |
| 1.12 | C10 item info | 字段全集+size 字符串+originalChecksums | 13 字段全在（缺 0）；size 为 str；checksums/originalChecksums 各含 sha1/md5/sha256；path 以 `/` 开头 | ✅ |
| 1.13 | C13 checksum 一致 | 201 | `201` | ✅ |
| 1.14 | C14 checksum 不一致 | 409+received/actual+无 node | `409` `Checksum error … received '000…0' but actual is '466ac…'`；GET 404 | ✅ |
| 1.15 | C18 删除 | 204 无 body→重复 404→GET 404 | `204`（0 字节 body）→ `404` → `404` | ✅ |

### E-26 口径更新（R10：/v2/** 反转，/artifactory/** 维持）

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 1.16 | `GET /v2/`（匿名开实例） | **200 `{}`**（反转） | `200` body `{}`（2 字节） | ✅ |
| 1.17 | `GET /v2`（无尾斜杠） | 200 | `200` | ✅ |
| 1.18 | `GET /v2/`（匿名关实例） | **401 + Bearer 挑战** | `401`；`Www-Authenticate: Bearer realm="http://127.0.0.1:64744/v2/token",service="binflow"`；`Docker-Distribution-Api-Version: registry/2.0` | ✅ |
| 1.19 | `/artifactory/api/system/ping` | 404 + /binflow 提示 | `404` message 含 `/binflow` | ✅ |
| 1.20 | `/binflow/api/npm/xx` | 404 + E-01（status/message） | `404`；errors[].status=404，无 code 字段 | ✅ |
| 1.21 | `/api/system/info`、`/nope`、`/binflow/api/pypi/x` | 404 | 三变体全 `404` | ✅ |

> 注：首轮脚本 C03/C18/E26R-v2 三行 FAIL 为 QA 自身 harness 解析缺陷（zsh printf 参数截断 / awk 字段切错），净手复跑全过——与 T-18 轮「二轮净 instance 自纠误报」同性质，已如实留档。

---

## 剧本 2：仓库与探针（D01 → D04/D04b/D04c）

| # | 场景 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 2.1 | D01 建 docker-local/charts | 200 | `200 Successfully created repository 'docker-local'` / `'charts'`；C06 面验证 `packageType=docker rclass=local` | ✅ |
| 2.2 | D01 保留字 `v2`/`api` | 400 | 均 `400` | ✅ |
| 2.3 | D04 ping 挑战（匿名关） | 401 + realm=/v2/token + service=binflow | 401；realm/service 正确；**无 scope**（与 docker-registry.md §5.1 挑战样例同形——scope 仅库端点推导；PRD DE-13「GET→pull」措辞指 repository 端点，见勘误 C2） | ✅（附注） |
| 2.4 | D04 读挑战 scope | `repository:<name>:pull` | tags/list → `scope="repository:docker-local/acme/app:pull"` | ✅ |
| 2.5 | D04 写挑战 scope | `…:pull,push` | POST uploads → `scope="repository:docker-local/acme/app:pull,push"` | ✅ |
| 2.6 | D04 删挑战 scope | `…:pull,delete` | DELETE manifest → `scope="repository:docker-local/acme/app:pull,delete"` | ✅ |
| 2.7 | D04 `Docker-Distribution-Api-Version` | registry/2.0 | `/v2/`、错误响应全带 | ✅ |
| 2.8 | D04b admin 手工协商 | 200 + token/access_token 同值 + expires_in>0 | `200`；token=access_token（64B）；`expires_in=2592000`；issued_at RFC3339 | ✅ |
| 2.9 | D04b Bearer 打 _catalog | 200 | `200 {"repositories":[…]}` | ✅ |
| 2.10 | D04b POST form（旧客户端） | 200 | `200` token 64B | ✅ |
| 2.11 | D04b 匿名 pull token（匿名开） | 200 | `200` token 64B | ✅ |
| 2.12 | D04c 非 admin 走 /v2/token | 200 | ci-bot `200` token 64B（双入口语义成立） | ✅ |
| 2.13 | D04c 同用户走管理面入口 | 403 | `403 {"error":"invalid_request",…administrator privileges required}` | ✅ |
| 2.14 | D04c 错误凭据不泄露存在性 | 401 同体 | 真用户错 pw 与幽灵用户 401 且 body 逐字节相同 | ✅ |
| 2.15 | D04c offline_token | 400 invalid_request | `400` `{"error":"invalid_request","error_description":"offline_token is not supported"}`（body 形态见缺陷 D3） | ✅（附 D3） |
| 2.16 | 错误体三域隔离 | spec 体 / E-01 / OAuth 各归其域 | /v2 资源面：`{"errors":[{"code":"BLOB_UNKNOWN","message",…}]}`（无 status 字段）；/binflow 未实现端点：`{"errors":[{"status":404,"message"}]}`（无 code）；管理面 token：`{"error":"unsupported_grant_type",…}`；**/v2/token 401 却回 spec 体**（见 D3） | ❌（D3） |

---

## 剧本 3：blob 协议与 manifest（D06→D07→D10→D10b→D12→D13→D13b→D13c→D13d→D14→D09→D11）

**执行口径注记（勘误 C1）**: PRD D 序列命令字面使用 `/v2/<repoKey>/blobs/...`（单段 name），实测 404 `path … carries no registry route`——这是 T-33 已评审 AC「单段 name → 404」的既定行为（name.go 注释在案）。本票按语义意图改用 `<repoKey>/<image>` 全名执行（如 `/v2/docker-local/acme/app/blobs/...`），PRD 命令需 PM 回写（见勘误 C1）。

| # | 场景 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 3.1 | D06 initiating | 202 + Location + UUID + Range: 0-0 | `202`；Location 相对路径 `/v2/…/blobs/uploads/<uuid>`；UUID 在；`Range: 0-0` | ✅ |
| 3.2 | D06 monolithic PUT | 201 + Docker-Content-Digest | `201`；DCD==sha256；Location 含 digest | ✅ |
| 3.3 | D06 GET 回取 | 200 sha256 一致 | 一致（10MB） | ✅ |
| 3.4 | D06 HEAD | 200 + CL + DCD 无 body | CL=10485760；DCD 正确；body 0 字节 | ✅ |
| 3.5 | FR-8-AC8 Range 拉取 | 206 | `206` `Content-Range: bytes 0-99/10485760`，字节与源一致 | ✅ |
| 3.6 | D07 单请求 | 201 | `201` + Location 含 digest | ✅ |
| 3.7 | D10 chunked ≥3 段 | 3×202 Range 递增 | 1M+1M+8M：`[202,202,202]`；Range `0-1048575`→`0-2097151`→`0-10485759` | ✅ |
| 3.8 | D10 终结 + cmp | 201 + 逐位一致 | `201`；GET `cmp` 逐位一致 | ✅ |
| 3.9 | D10b 中断恢复 | 204 + Range: 0-<received> | `204` `Range: 0-2097151`；续传第 3 段终结 `201` | ✅ |
| 3.10 | DE-04 Content-Range 错位 | 416 + Range: 0-<received> | `416` `Range: 0-0` | ✅ |
| 3.11 | D12 digest 失配 | 400 DIGEST_INVALID | `400` `errors[0].code=DIGEST_INVALID` | ✅ |
| 3.12 | D12 无 blob 残留 | GET 404 | `404`；毒化 session 再 PUT → `404`（会话已清） | ✅ |
| 3.13 | D13 cross-repo mount | 201 零传输 | `from=<repoKey>/<image>` 全名 → **201**；charts 侧 GET 200 内容一致 | ✅ |
| 3.14 | D13 bare repoKey from | 降级 202（spec fallback） | `from=docker-local` → `202` 普通会话（代码注释明示 degrade） | ✅（勘误 C1） |
| 3.15 | D13d blob DELETE | 405 UNSUPPORTED | `405` `UNSUPPORTED` | ✅ |
| 3.16 | FR-7-AC4 item info | docker node 可列 | `GET /binflow/api/storage/docker-local/acme/app/blobs/<hex>` → 200 + 字段集 | ✅ |
| 3.17 | D13b 引用缺失 blob | 400 MANIFEST_BLOB_UNKNOWN | `400` `MANIFEST_BLOB_UNKNOWN`；无幽灵 manifest（GET 404） | ✅ |
| 3.18 | FR-9-AC4 by-digest 失配 | 400 DIGEST_INVALID | `400` `DIGEST_INVALID` | ✅ |
| 3.19 | FR-9-AC2 tag 覆盖 | by-tag 新、旧 by-digest 仍在 | 覆盖 v2 → by-tag == 新 body；旧 digest 200 逐位一致 | ✅ |
| 3.20 | D09 catalog | 字典序 + 全部名字（含嵌套） | `["docker-local/acme/app","docker-local/acme/acme…","docker-local/acme/team/app","docker-local/empty/img"]` 嵌套名 `acme/team/app` 在列、全局字典序 | ✅ |
| 3.21 | D09 tags/list | 字典序 | `{"name":"docker-local/acme/app","tags":["v1","v2"]}` | ✅ |
| 3.22 | D09 空仓断言（R4 注记） | `jq '.tags == null'` | digest-only 镜像 → `"tags":null`（python `tags is None: True`）——**非 `[]`**，断言注记口径实测成立 | ✅ |
| 3.23 | D09 分页 | n=1 + Link rel="next" | `Link: </v2/…/tags/list?last=v1&n=1>; rel="next"`；跟随枚举完 {v1,v2} 无重复 | ✅ |
| 3.24 | D09 last 游标 exclusive | 不含 last 本身 | `?last=v1` → `["v2"]` | ✅ |
| 3.25 | D09 n 非法 | 400 PAGINATION_NUMBER_INVALID | n=abc / n=0 / n=-1 / catalog n=abc 四变体全 `400` | ✅ |
| 3.26 | D09 未知 name | 404 NAME_UNKNOWN | `404 NAME_UNKNOWN` | ✅ |
| 3.27 | D13c by-tag DELETE | 405 UNSUPPORTED + tag 不受影响 | `405 UNSUPPORTED`；tag 仍 GET 200 | ✅ |
| 3.28 | D13c by-digest DELETE | 202 → 404s | `202`；by-digest/by-tag 随后 `404` | ✅ |
| 3.29 | D13c 级联与幸存 | tag 行级联、layer 不受影响 | tags 由 [v1,v2]→[v2]；被引用 layer GET 200 | ✅ |
| 3.30 | D13c blob 面仍 200 | 既定语义 | manifest digest 经 `/blobs/` GET → `200`（T-51 双 node 布局契约，**非泄漏**，不报缺陷） | ✅ |
| 3.31 | D11 同内容重推 | stats 不变 | blobs `14 → 14`（同 config+layer+manifest 字节全去重） | ✅ |
| 3.32 | FR-8-AC7 跨协议去重 | generic 后 docker 推同内容 stats 不变 | generic `curl -T` 201 → docker `POST ?digest=` 201；blobs `15 → 15`；docker 侧 GET 逐位一致 | ✅ |
| 3.33 | FR-7-AC5 删仓 | catalog 不再含 | `DELETE ?deleteContent=true` 200 → `scratch-dkr/img` 从 catalog 消失 | ✅ |
| 3.34 | DE-15 referrers | 404 spec 体 | `404` `UNSUPPORTED` spec 体 | ✅ |
| 3.35 | DE-16 未定义 /v2 路径 | 404 spec 体（不出 E-01） | `/v2/foo/bar/baz/qux` → `404 {"errors":[{code:"UNSUPPORTED",…}]}` | ✅ |
| 3.36 | D14 kill -9（chunked 进行中） | 重启后 health 200 / 历史 blob 200 / 残 session 404 / 未终结 blob 404 | `health=200 hist_blob=200 stale_session=404 unfinalized_blob=404` | ✅ |

---

## 剧本 7 关联：权限面（D22 → D23 → NFR-S11/S12）

| # | 场景 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 7.1 | D22 授权内读 | 200 | ci-bot read on `ci-out/**`：GET 内域 blob 200 内容一致 | ✅ |
| 7.2 | D22 Basic 无权推 | 403 DENIED | `403` `DENIED`（spec 体） | ✅ |
| 7.3 | D22 Bearer 无权推 | 403 DENIED | /v2/token 换 token（仍签发）→ 资源端点 `403 DENIED`（scope 收窄在资源端执行，PRD 口径） | ✅ |
| 7.4 | D22 域外读 | 403 | `403` | ✅ |
| 7.5 | D22 服务端无 5xx | 日志无 5xx ERROR | 全程实例 A 无 5xx（除 O1 断连 WARN 标注行）；无 D22 相关 ERROR | ✅ |
| 7.6 | D23 吊销联动 | Bearer 401 | /v2/token 签发 → Bearer 200 → 管理面 revoke 200 → Bearer `401`（双入口同辖） | ✅ |
| 7.7 | NFR-S11 路径穿越 | 400/404 + 数据目录外无文件 | 5 变体（`../../etc/passwd`、深三级、%2e%2e、..%2f..%2f、blob 段穿越）全 `400`；`find` 无 passwd/etc 逃逸文件 | ✅ |
| 7.8 | NFR-S12 匿名写拒绝 | 401 挑战 | 匿名 POST uploads `401` + Bearer；匿名 manifest PUT `401` | ✅ |
| 7.9 | NFR-S12 mount 无源读 | 降级 202（fail-closed） | ci-bot（目的可写+源不可读）mount → `202` 回退普通会话 | ✅ |
| 7.10 | NFR-S12 mount 有源读 | **201 零拷贝** | ci-bot 持源 image 精确 read（含 `ci-out/x` 精确与 `ci-out/x/**` 两种 pattern）→ 仍 **202**；admin 对照 `201` | ❌ **缺陷 D1** |

---

## 剧本 10 关联：M1 遗留抽查（O1 / O3 / Content-Type）

| # | 场景 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 10.1 | O1 generic 慢上传中断 | 无 5xx 级 ERROR；client_disconnect 标注 | `--limit-rate 64k` + kill -9 curl（4MB）：access 行 `level=WARN` + `client_disconnect=true` + `disconnect_reason=context_canceled`（status 字段留 500 为 T-41 设计）；中断路径 GET 404 零残留；断连归因的 level=ERROR 计数 **0** | ✅ |
| 10.2 | O1 docker 域中断 | 同上 | 慢 PATCH 中断 → `416 INFO` 收卷（非 5xx 不触发降级标注，语义正确）；无 ERROR | ✅ |
| 10.3 | O3 gc 旗标 | `-c`/`--grace-hours` 被接受 | `gc -c gc.yaml --grace-hours 1` → exit 0，`gc: mode=dry-run grace=1h0m0s … candidates=0`；`gc -c f --help` 打印 usage（exit 1 = Go flag 包缺省行为，T-42 已登记小票） | ✅ |
| 10.4 | Content-Type 映射 | `curl -TI x.json` → application/json | json→`application/json`；txt→`text/plain; charset=utf-8`；zip→`application/zip`；未知扩展→`application/octet-stream`（回退不变） | ✅ |
| 10.5 | §6.3 health registry 字段 | 只增字段 | `{"status":"ok","storage":…,"metadata":…,"registry":{"status":"ok"}}` | ✅ |

---

## 缺陷清单（发现不修，附票号定位）

### D1 [P2] 非 admin 的 cross-repo mount 永远降级 202（零拷贝挂载仅 admin 可用）
- **现象**: 持有源 image read 权限（精确 pattern `ci-out/x` 与 `ci-out/x/**` 均试）的非 admin 用户 `POST …/blobs/uploads/?mount=<digest>&from=<repoKey>/<image>` → 202 普通会话，永不 201 零拷贝；admin 同请求 → 201。匿名同理（不可达，写门拦截）。
- **根因（已定位源码）**: `internal/adapter/docker/blob.go` `canMountFrom`（约 :340）把 scope 字符串 `scopeActionPull`（`"pull"`）直接作为 action 传给 `Authorizer.Can`，而 Can 的 action 域是 `auth.ActionRead`（`"r"`，auth/api.go:22）；`rowAllows`（auth/authorizer.go:84-92）对未知 action 走 `default: false` → 非 admin（无 `p.Admin` 短路）恒 false → tryMount 静默降级。对照路由门（handler.go:260）经 `canActions` 映射后传 `mapped[0]`，是正确写法。
- **影响**: spec 合法的降级（无客户端破坏、无越权——fail-closed 方向），但 DE-03/D13 的零拷贝优化对 crane/oras 高频路径（PRD FR-8-AC6 注记）在非 admin 下失效，全部退回全量上传。
- **修法建议**: `canMountFrom` 改传 `canActions[scopeActionPull][0]`（一行）+ 补非 admin mount 201 的回归用例（T-38 现有测试仅覆盖 admin）。
- **归属票**: T-38（internal/adapter/docker blob 域）。

### D2 [P2] tags/list 存在性探测在「已认证无读权限」路径打 ERROR 日志（污染 5xx/ERROR 计数）
- **现象**: ci-bot（对 docker-local/ci-out/x 无 read——注：其权限在 repo 内其它前缀）`GET /v2/…/tags/list` → 正确 404 NAME_UNKNOWN，但服务端记 `"level":"ERROR","msg":"docker: tagless-image probe failed","error":"read docker-local: permission denied"`。
- **根因**: T-40 为绕过 T-35 ListTags 零 tag 契约缺陷在 adapter 加的 `imageListed` 存在性探测（catalog.go），探测失败（含权限拒绝这种正常分支）一律 ERROR 级。
- **影响**: 纯日志卫生——正是 T-41/O1 力保的「ERROR 计数 = 真故障」信号被污染；QA/运维 grep ERROR 会误报。
- **归属票**: T-40 workaroud 分支（`internal/adapter/docker/catalog.go`）；**T-52 修复 repo.ListTags 后该分支可删**（T-40 遗留第 1 条已预告）——T-52 验收时应连带确认此 ERROR 消失。
- **复现**: `curl -su <无该 repo 读权限用户> $BASE/v2/<repo>/<无权限 image>/tags/list` → grep 服务日志 `tagless-image probe failed`。

### D3 [P2] /v2/token 两条错误路径的错误体格式互斥（401=spec 体 / 400=OAuth 体）
- **现象**: 错误凭据 `GET /v2/token` → 401 body `{"errors":[{"code":"UNAUTHORIZED",…}]}`（spec 体）；`offline_token=true` → 400 body `{"error":"invalid_request",…}`（OAuth 体）。同一端点两种格式。
- **PRD 口径自相矛盾在先**: §5.2 注与 T-32-R1/T-37 说 token 端点域用 OAuth 体；FR-11-AC7 又说 offline_token 400 用「spec 错误体」；NFR-S10 说 `/v2/**` 全部 spec 体。实现两条路径各取其一，恰好两不沾。
- **影响**: 客户端不解析 401 body（docker login 只看挑战头），实际兼容性无损；属契约分层不一致。
- **处置建议**: 先由 PM 裁定 /v2/token 非 2xx 统一格式（建议 OAuth 形，与 T-37 主实现与 §5.2 注一致），T-37 改另一分支 + PRD 三处措辞对齐。
- **归属票**: T-37（adapter/docker token 域）+ PM 勘误。

### F1 [P1-watch] TestV2ManifestConcurrentTagOverwrite 全仓并行负载下偶发 500（flake）
- **现象**: `make test`（race 全仓）第一跑：`internal/httpapi` `TestV2ManifestConcurrentTagOverwrite` FAIL——`writer failed: layer 500`（20 路并发 blob POST 之一返回 500，docker_manifest_test.go:802）。
- **复现性**: 隔离重跑 ×4 全绿；httpapi 整包重跑绿；**全仓第二跑 12/12 全绿**。两次全仓一过一挂 → 负载敏感型 flake（怀疑并行包压力下 SQLite 写竞争瞬时错误被映射为 500 UNKNOWN；run-1 日志未见对应服务端 ERROR 行，证据不足以下定论）。
- **处置建议**: 转 dev 复现定位（`go test -race -count=N` 全仓并行下压测；检查 blob 上传路径对 SQLITE_BUSY/锁超时类错误的映射是否应 5xx）。CI 若偶红按此归因。**不阻塞本票结论**（第二跑全绿 + 5 次定向全绿），但需在 m2-done 前有定论。

---

## PRD 口径勘误（转 conductor → PM，非产品缺陷）

| # | 项 | 现状 | 建议 |
|---|---|---|---|
| C1 | **D 序列命令的单段 name 形态** | PRD D06/D07/D10/D12/D13 字面用 `/v2/<repoKey>/blobs/...` 与 `from=<repoKey>`；实测单段 name 404（T-33 已评审 AC「单段 name → 404」）+ mount `from` 要求全名（代码注释明示 `<repoKey>/<image>`） | PRD 命令改 `<repoKey>/<image>` 全名形态（D01 建仓名与镜像名分离的示例），避免后续票照抄误判 |
| C2 | DE-13 scope 推导措辞「GET→pull」 | ping 挑战无 scope（与 docker-registry.md §5.1 挑战样例同形，scope 仅 repository/catalog 端点推导）；实测 repository 端点四形态（pull / pull,push / pull,delete / registry:catalog:*）全对 | DE-13 注明「ping 挑战无 scope；GET→pull 指 repository 资源端点」 |
| C3 | offline_token 错误体格式 | FR-11-AC7 写「spec 错误体」、§5.2 注写「OAuth（token 端点）」、NFR-S10 写「/v2/** 全 spec」三处打架；实现 400=OAuth / 401=spec（见 D3） | PM 单选一种（建议 OAuth），三处措辞 + 实现一并收敛 |

---

## DoD §9 第 2 条判定（本票覆盖部分：剧本 1-5/7/10）

| §8 剧本 | 覆盖 | 判定 |
|---|---|---|
| 1 回归基线 | C 序列 P0 九项 + E-26 反转 | **绿**（FR-7-AC2 满足） |
| 2 仓库与探针 | D01/D04 全系/D04b/D04c | **绿**（D3 为格式分层项，不伤语义） |
| 3 blob 协议 | D06/D07/D10/D10b/D12/D13/D14 | **绿** |
| 4 manifest | D13b/D13c/D13d（+FR-9-AC2/AC4、FR-7-AC4） | **绿** |
| 5 去重与 stats | D11 + generic↔docker 跨协议变体 | **绿** |
| 7 权限面 | D22/D23 + NFR-S11/S12 变体 | **绿**（NFR-S12 mount-有读变体挂 D1，P2 spec-legal 降级） |
| 10 M1 遗留收口 | O1（T-41 口径）/O3/Content-Type | **绿** |
| 6/8/9 五客户端/性能/烟测 | 不在本票（T-44/T-45） | 未涉及 |

**本票结论: PASS。** 剧本 1-5/7/10 的 AC 语义全部达成；D1/D2/D3 三个 P2 缺陷与 F1 flake 登记，建议随 T-52/T-44 窗口收口（D2 预期随 T-52 自然消失；F1 需 m2-done 前定论）。

---

## 环境与清理

- 三实例（64743/64744/64745）已停止；`/tmp/t43`（worktree、数据目录、脚本、凭据）已删除；`git worktree remove` 已执行；仓库自带 `data/` 未动。
- 测试口令均为一次性随机值，未写入任何提交文件；`reports/agents/T-43-qa.md` 为唯一产出。
- 主仓工作树未做任何修改（T-52 在途改动原样保留）。

## 方法论留档（供后续 QA 票复用）

- 大 body curl（≥1MB --data-binary）会触发 `Expect: 100-continue`  interim 响应，解析须取**最后一个** header 块（本票首轮 4 行假 FAIL 全部由此与 zsh printf 参数截断引起，净手复跑撤销）。
- 单段 name 的 /v2 URL 在匿名关实例上会先吃 401 挑战（auth 门先于路由解析，scope 按路径启发式推导）再在匿名开实例上 404——两种实例行为差异属既定分层，勿混淆为不一致缺陷。
