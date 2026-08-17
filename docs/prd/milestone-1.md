# PRD — M1 内核基座（Kernel Foundation）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-1.md` |
| 里程碑 | M1 — 内核基座（对应 ROADMAP.md「M1 — 内核基座（当前）」全部条目） |
| 状态 | Draft v1.0（待逆向规格落地后增量校准，见 §5.5） |
| 上游依据 | PRODUCT.md（愿景/Non-goals/成功标准/技术约束）、ROADMAP.md、DECISIONS.md ADR-0002/0003 |
| 下游消费者 | tech-lead（拆票）、architect（ADR/设计）、dev 各角色（实现）、qa-engineer（验收） |

---

## 1. 背景与目标

### 1.1 背景

BinFlow 是用 Go 从零实现的云原生制品仓库，架构与概念模型对齐 JFrog Artifactory。M1 是第一块地基：**没有存储引擎就没有制品，没有仓库模型就没有协议适配，没有认证就没有多租户**。M1 交付一个「用 curl 就能真实完成上传/下载/删除/校验闭环」的 Generic 本地仓库，为 M2（Docker Registry v2）提供存储、元数据与认证底座。

M1 的验收哲学（沿用团队准则）：**一个协议被真实客户端完整走通 > 三个协议只有 happy path**。因此 M1 只做 Generic（raw HTTP），但要求带认证、校验、异常路径、重启一致性全部可验证。

### 1.2 M1 目标

> 一句话：交付一个单二进制、零外部依赖、可用 curl 完成「建仓 → 上传 → 校验 → 下载 → 去重 → 删除」全流程并有认证与路径权限保护的 Generic 制品仓库。

量化目标（M1 门槛，未达即里程碑不完成）：

| 指标 | M1 门槛 | 来源 |
|---|---|---|
| 真实客户端闭环 | curl 走通 §7 验收剧本全部场景，qa-engineer 报告全绿 | PRODUCT 成功标准（raw 部分） |
| 冷启动（空库，二进制） | < 2s | PRODUCT 成功标准 |
| 并发拉取 | 100 并发 GET 10MB 文件，0 错误 | PRODUCT 1000 并发门槛的 M1 阶段值 |
| 去重 | 相同内容在任意多条路径下，filestore 只存一份 blob | PRODUCT 核心能力 1 |
| 元数据零依赖 | 默认内嵌 SQLite，`docker compose up` 后无需手工初始化即可用 | PRODUCT 技术约束 |
| 工程化 | `make lint` / `make test` / `make build` 全绿，CI 流水线存在且通过 | ROADMAP M1 脚手架条目 |

### 1.3 上游依赖与并行关系

- M1 内的「逆向规格」与「ADR/架构设计」两条 ROADMAP 条目与本报并行产出。本 PRD 中关于 Artifactory 行为的描述**以公开文档与公认行为为暂定依据**，逐条标注置信度（§5.2）；`docs/reverse/rest-api.md` 等规格落地后，若与本文有出入，**以规格为准回写本 PRD**（增量修订，版本号 +0.1）。
- 本 PRD 不规定内部实现（目录结构、SQLite schema、blob 目录推导），那些归 architect 的 ADR 与设计文档；本 PRD 只约束**可观察行为**。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M1 条目一一对应）

| # | ROADMAP 条目 | 本 PRD 功能需求 |
|---|---|---|
| 1 | 脚手架：go module、cmd/internal 布局、Makefile、lint/test/CI | FR-1 |
| 2 | 存储引擎：checksum 寻址、去重、上传会话、原子落盘 | FR-2 |
| 3 | 元数据与仓库模型：repo 配置、node 元数据、SQLite 嵌入 | FR-3 |
| 4 | Generic 本地仓库：上传/下载/删除/校验 | FR-4 |
| 5 | 基础认证与权限骨架：admin 用户、API Token、路径 ACL | FR-5 |
| 6 | 开发环境：docker-compose 起本地实例 | FR-6 |
| 7 | QA：generic roundtrip + 存储完整性；README 快速开始 | §7 验收剧本 + FR-6-AC4 |

（「逆向规格」「ADR」两条为流程条目，作为本 PRD 的上游输入，不再单列功能需求。）

### 2.2 Non-goals — M1 明确不做（防范围蔓延）

以下内容 M1 **不做**。实现时遇到「顺手做了吧」的诱惑，一律拒绝并回报主会话：

**产品级 Non-goals（继承 PRODUCT.md，M1–M5 全程有效）**
- 不做 HA 集群 / 联邦复制（active-active）。
- 不做 Xray 式漏洞扫描 / 许可证合规。
- 不做 LDAP / SAML / OIDC（本地用户 + API Token 即止）。
- 不做 Artifactory 全量 REST 兼容（只做 §5.2 列出的高频子集，其余走 `/api/v1` 或 404）。

**M1 里程碑级 Non-goals（后续里程碑交付）**

| 不做项 | 归属 | M1 的隔离边界 |
|---|---|---|
| remote 仓库（代理缓存）/ virtual 仓库（聚合） | M3 | `rclass` 仅接受 `local`；`remote`/`virtual` 创建请求返回 400（§5.2 E-07） |
| Docker Registry v2（`/v2/` 全部端点） | M2 | `/v2/**` 一律 404 标准错误体（E-08） |
| Maven / npm / PyPI 协议适配 | M3 | `/api/npm/**`、`/api/pypi/**`、`/api/pypi-ui/**`、Maven layout 解析一律 404（E-08）；M1 不解析任何包 layout，generic 路径即原始路径 |
| Web 控制台（登录/浏览/上传 UI） | M4 | 不内嵌任何 HTML 页面（go:embed 占位可留，但无路由）；M1 唯一界面是 REST API |
| 搜索、审计日志、GC、配额、备份恢复 | M4 | 全部不做；`/api/search/**` 404 |
| users/groups 完整权限模型（组、权限继承、UI） | M4 | M1 仅：admin + 普通用户 + 单层路径 ACL（§FR-5） |
| Prometheus metrics | M6+ | M1 可观测性底线 = 结构化日志 + `/api/v1/health`（§6.3） |
| 多存储后端（S3） | M6+ | M1 仅本地文件系统 blob 存储 |
| 断点续传（Range/`If-Range`）、分块并发上传 | M2 前置 | M1 定 P2，允许延后，但 M2 docker 层拉取开工前必须落地（开放问题 Q6） |
| 归档解包上传（`X-Explode-Archive`） | 不排期 | M1 显式拒绝（E-09） |

---

## 3. 用户与场景（M1 视角）

M1 的「真实用户」是**平台工程师的 CI 脚本**，不是终端人类用户（人类界面 M4 才有）。

- **场景 A（CI 构建产物归档）**：流水线 job 用 `curl -T` 把构建产物（二进制、tarball、SBOM）推入 `build-outputs` 仓库，靠 sha256 确保落盘内容与构建机一致。
- **场景 B（内部工具分发）**：平台团队把 `kubectl` 替代品、内网 CLI 放在 `tools` 仓库，开发机用 `curl -O` + `sha256sum -c` 拉取校验。
- **场景 C（最小权限）**：给 CI 专用账号只授 `ci-out/*` 路径的写权限，越权路径写入被 403 拒绝。
- **场景 D（迁移预演）**：从 Artifactory 迁出的团队用同一套 curl 脚本打 BinFlow，验证路径、认证头、错误码可平移。

---

## 4. 功能需求

约定：优先级 P0 = M1 关键路径，缺一不可；P1 = M1 应完成；P2 = 可延后（延后需主会话在 BOARD 记录）。所有 AC 中 `BASE=http://localhost:8080`（默认端口，可配置）、管理员账号 `admin`、初始口令见 FR-5-AC1，示例统一写作 `$ADMIN_PW`。

### FR-1 脚手架与工程化（devops-engineer）

**用户故事**：作为开发者，我希望 clone 仓库后一条命令完成构建/测试/静态检查，任意 CI 环境拿到的是同一套工程契约，避免「本地能跑 CI 跑不了」。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-1-AC1 | `make build` 退出码 0，产出可执行文件 `bin/binflow-server`（linux/darwin 本机架构即可，交叉编译属 M5）；`./bin/binflow-server --help` 退出码 0 且输出含 `usage` | P0 |
| FR-1-AC2 | `make test` 退出码 0（`go test ./...` 全部 PASS，输出无 FAIL）；`make lint` 退出码 0（golangci-lint 零告警） | P0 |
| FR-1-AC3 | `gofmt -l .` 输出为空（退出码 0、无文件名列出） | P0 |
| FR-1-AC4 | CI 配置存在（`.github/workflows/ci.yml` 或等价物），内容至少触发 lint+test+build 三步；QA 以「文件存在 + 本地 make 三件套全绿」验收，CI 首跑绿由主会话确认 | P1 |
| FR-1-AC5 | `go.mod` 的 Go 版本为当前稳定版；模块路径以 `github.com/<org>/binflow`（或用户定值，见开放问题 Q7）开头；`go mod tidy` 无 diff | P1 |
| FR-1-AC6 | 构建输出打印二进制大小；M1 不设 < 40MB 硬门（GA 门槛），但 > 40MB 时 CI 给 warning | P2 |

### FR-2 存储引擎（dev-go-storage）

**用户故事**：作为平台工程师，我希望仓库落盘的内容永不损坏、相同内容只存一份、上传中断不留脏数据——这样磁盘成本和可信度才配得上「制品仓库」。

语义定义（实现方式归 architect ADR，此处约束可观察行为）：
- blob 以内容 sha256 寻址存储；同一 blob 被多条 node 引用时物理只存一份（引用计数删除）。
- 上传走「上传会话」：先落临时区，流式计算 checksum，**通过后才对用户可见**；会话中断（连接断开 / 进程被杀）不得产生可见 node。
- 删除是幂等的：删除最后一条引用后 blob 可被回收（M1 允许延迟回收，但 node 必须立即不可见）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-2-AC1（去重） | 同一 10MB 内容分别 PUT 到 `a/x.bin` 与 `b/y.bin`（命令 C07 两次），两次均 201；`GET /api/v1/storage/stats`（C12）返回的 blob 总数第二次上传后**不变**；两条路径均能 200 下载且 sha256 一致 | P0 |
| FR-2-AC2（校验寻址） | C10（`GET /api/storage/{repo}/{path}`）返回的 `checksums.sha256` 与本地 `sha256sum` 输出一致（C07 的 jq 断言） | P0 |
| FR-2-AC3（原子可见性） | 构造慢上传：`dd if=/dev/urandom of=big.bin bs=1m count=100`，`timeout -s KILL 3 curl -su admin:$ADMIN_PW -T big.bin --limit-rate 64k $BASE/artifactory/generic-local/acme/big.bin`（退出码非 0）；随后 `curl -s -o /dev/null -w '%{http_code}' .../acme/big.bin` 返回 **404**，且 C12 的 blob 计数较上传前不变（临时文件已清理或在 quarantine 区，不计入 blob 数） | P0 |
| FR-2-AC4（kill -9 一致性） | 上传中 `kill -9 <server_pid>` 后重启服务：已完成的历史上传仍 200 可下载；中断的那条路径 404；`GET /api/v1/health` 200 | P0 |
| FR-2-AC5（覆盖写幂等） | 对同一路径再次 PUT 相同内容 → 2xx；stats blob 计数不变。PUT 不同内容 → 2xx，下载返回**新**内容，旧 blob 若无引用则计数减一（允许延迟） | P1 |
| FR-2-AC6（大文件流式） | 1GB 文件 PUT 成功（201）后 GET 回来 sha256 一致；上传期间服务进程 RSS 增量 < 256MB（QA 用 `ps -o rss` 采样，防全量读入内存） | P1 |

### FR-3 元数据与仓库模型（dev-go-core）

**用户故事**：作为 Artifactory 迁移用户，我希望仓库的创建/查询/删除走我熟悉的 REST 端点、用熟悉的字段（repo key、rclass、packageType），迁移脚本只改域名不改逻辑。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-3-AC1（建仓） | C03：`curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/api/repositories/generic-local -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic"}'` → HTTP 201（已存在则 200） | P0 |
| FR-3-AC2（列仓） | C05：`GET /artifactory/api/repositories` → 200，jq 能取出 `key=="generic-local"`，`type/packageType` 字段存在且值合法 | P0 |
| FR-3-AC3（查仓） | C06：`GET /artifactory/api/repositories/generic-local` → 200，`rclass=="local"`、`packageType=="generic"` | P0 |
| FR-3-AC4（repo key 校验） | PUT 建仓 key 为 `Bad_Key!`（大写/特殊字符）→ **400** + §5.2 E-01 标准错误体；合法 key 规则：`[a-z][a-z0-9-]{1,62}` | P0 |
| FR-3-AC5（删仓） | 空仓库 `DELETE /api/repositories/{key}` → 2xx；非空仓库不带参数 → **400**（message 含 `deleteContent` 提示）；带 `?deleteContent=true` → 2xx 且其下所有 node 之后 404 | P1 |
| FR-3-AC6（node 元数据） | C10：item info JSON 至少含 `repo / path / children（目录时）/ size / created / createdBy / lastModified / modifiedBy / downloadUri / uri / checksums{sha1,md5,sha256} / mimeType`；`created` 为 ISO 8601 带时区偏移 | P0 |
| FR-3-AC7（目录语义） | C16：`curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/generic-local/acme/`（结尾斜杠、空体）→ 2xx；`GET /api/storage/generic-local/acme` → 200 且 `children[]` 列出其下条目；对目录 DELETE → 2xx 且递归删除其下全部 node | P1 |
| FR-3-AC8（list 查询） | C17：`GET '/artifactory/api/storage/generic-local?list&deep=1'` → 200，`fileList[]` 含全部文件相对路径；M1 只需 `list/deep/depth` 参数 | P2 |
| FR-3-AC9（SQLite 嵌入） | `docker compose up -d` 后**不执行任何初始化 SQL**，C03 建仓即成功；重启实例（FR-6-AC3）后仓库与 node 全部仍在 | P0 |
| FR-3-AC10（Postgres） | M1 仅 SQLite；若检测到 Postgres 配置，启动时明确报错退出（退出码非 0 + 日志说明「Postgres 支持未启用」，归属见开放问题 Q5） | P2 |

### FR-4 Generic 本地仓库：上传/下载/删除/校验（dev-registry-adapter）

**用户故事**：作为 CI 脚本作者，我希望 `curl -T` 上传后拿到服务端计算的 sha256 用于审计，下载时能从响应头拿到 checksum 做对账，还能不传内容就凭 sha256 秒传——这些是 Artifactory 上已经养成的用法。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-4-AC1（上传） | C07：`curl -su admin:$ADMIN_PW -T artifact.bin $BASE/artifactory/generic-local/acme/artifact.bin` → **201**；响应体 JSON 的 `checksums.sha256 == $(sha256sum artifact.bin)`，`size` 等于文件字节数，`createdBy=="admin"` | P0 |
| FR-4-AC2（下载） | C08：GET 同路径 → 200，落盘文件 `sha256sum` 与源一致；响应头含 `X-Checksum-Sha256 / X-Checksum-Sha1 / X-Checksum-Md5` 且值正确 | P0 |
| FR-4-AC3（HEAD） | C09：`curl -sI` → 200，`Content-Length == size`，`X-Checksum-Sha256` 正确 | P0 |
| FR-4-AC4（客户端校验-一致） | C13：PUT 附带 `-H "X-Checksum-Sha256: $SHA"`（$SHA 为正确值）→ 201 | P0 |
| FR-4-AC5（客户端校验-不一致） | C14：同样 PUT 但头值为 64 位错误十六进制 → **400**，标准错误体（E-01），落盘无该 node（GET 404） | P0 |
| FR-4-AC6（checksum deploy 命中） | C15a：`curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $SHA" $BASE/artifactory/generic-local/acme/copy.bin`（空体）→ **201**，新路径可 200 下载；未传输任何 body | P0 |
| FR-4-AC7（checksum deploy 未命中） | C15b：同上但 $SHA 为不存在内容的 sha256 → **404**（精确码待逆向规格校准，§5.5） | P1 |
| FR-4-AC8（删除文件） | C18：`curl -su admin:$ADMIN_PW -X DELETE .../acme/artifact.bin` → **2xx（BinFlow 定为 204，见 §5.5 校准项）**；随后 GET → 404；重复 DELETE → 404（幂等，不算错误） | P0 |
| FR-4-AC9（未认证写） | 不带认证 PUT → **401**，响应头含 `WWW-Authenticate: Basic realm=...`；错误体为标准 JSON（E-01） | P0 |
| FR-4-AC10（路径安全） | `curl -su admin:$ADMIN_PW -T f $BASE/artifactory/generic-local/a/../../etc/passwd` 与含 `%2e%2e` 编码变体 → **400**，服务进程工作目录与数据目录外无新文件 | P0 |
| FR-4-AC11（非法路径） | 双斜杠、空段（`//`）、超 512 字符路径 → 400 | P1 |
| FR-4-AC12（404 格式） | GET 不存在路径 → 404，body 为 E-01 错误体而非 HTML 栈页 | P0 |
| FR-4-AC13（Content-Type） | 下载响应必带 `Content-Type`，未知扩展名默认 `application/octet-stream`；按扩展名映射为 P2 | P2 |
| FR-4-AC14（Range） | `curl -H 'Range: bytes=0-99'` → 206 且字节数正确；多区间 Range 与 `If-Range` 不做 → 忽略或 200 全量（不得 5xx） | P2 |

### FR-5 基础认证与权限骨架（dev-go-core）

**用户故事**：作为安全管理员，我希望开箱即有一个 admin 账号、能给 CI 发 API Token 并随时吊销、能限制某账号只写指定路径——这是接入企业网络的最底线。

模型（对齐 Artifactory 语义，M1 骨架范围）：
- 认证方式：HTTP Basic（用户名+口令，或 用户名+Token 当口令）；或头 `X-JFrog-Art-Api: <token>`。
- 授权单元：permission target = `{name, repos[], includePatterns[], excludePatterns[], principals{users{<name>:[actions]}, groups{<name>:[actions]}}}`，actions ∈ `read | write | delete`（`write` 含上传，不含删除；`admin` 用户隐式全权）。端点走 BinFlow 自有 `/api/v1/permissions`（理由见 §5.2 E-11）。
- 匿名访问：默认**关闭**，`security.anonymous_access: true` 可开（默认值待用户确认，开放问题 Q2）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-5-AC1（首次引导） | 服务以 `BINFLOW_ADMIN_PASSWORD=$ADMIN_PW` 启动后，`curl -su admin:$ADMIN_PW $BASE/artifactory/api/system/ping`（需认证的端点如 repositories）→ 200；未设置该环境变量时使用文档化默认值（值待 Q3 确认） | P0 |
| FR-5-AC2（口令错误） | `curl -su admin:wrong -o /dev/null -w '%{http_code}' $BASE/artifactory/api/repositories` → **401** | P0 |
| FR-5-AC3（改密） | C20：`PUT /artifactory/api/security/password`，body `{"oldPassword":"...","newPassword":"..."}` → 2xx；旧口令随即 401，新口令 200 | P1 |
| FR-5-AC4（发 Token） | C21a：`POST /artifactory/api/security/token`（body 可空或 `{"username":"admin"}`）→ 200，返回 `access_token` 非空且 `token_id` 存在 | P0 |
| FR-5-AC5（Token 可用） | C21b：`curl -su admin:$TOKEN ...` 与 `curl -su admin:$ADMIN_PW -H "X-JFrog-Art-Api: $TOKEN" ...` 均 200 | P0 |
| FR-5-AC6（吊销） | C21c：`POST /artifactory/api/security/token/revoke` body `{"token_id":"..."}` → 2xx；随后用该 Token 的请求 **401** | P0 |
| FR-5-AC7（建用户） | C22a：`POST /artifactory/api/security/users` body `{"name":"ci-bot","password":"...","admin":false}` → 201；`ci-bot` 登录对任意仓库路径 GET/PUT → **403**（未授权） | P0 |
| FR-5-AC8（授权路径读） | C22b：`POST /api/v1/permissions` body 授 `ci-bot` 在 repo `generic-local`、pattern `ci-out/**` 上 `read` → 2xx；随后 `ci-bot` GET `generic-local/ci-out/x.bin` → 200，PUT 同路径 → 403，GET 仓库内其他路径 → 403 | P0 |
| FR-5-AC9（授权路径写） | C22c：再授 `write` 后 `ci-bot` PUT `generic-local/ci-out/y.bin` → 201，DELETE → 403（未授 `delete`）；`admin` 对同路径 DELETE → 2xx | P1 |
| FR-5-AC10（权限对象管理） | `GET /api/v1/permissions` 列出已建对象；`DELETE /api/v1/permissions/{name}` 后 ci-bot 相关授权立即失效（403） | P1 |
| FR-5-AC11（口令不回显） | `GET /artifactory/api/security/users` 的响应不含任何口令/哈希字段 | P0 |

### FR-6 开发环境与 README 快速开始（devops-engineer）

**用户故事**：作为新加入的贡献者/评估者，我希望 `docker compose up` 三条命令内拿到一个可用实例，README 的命令逐行复制即可跑通 M1 能力。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-6-AC1（compose 起服务） | 在 `deploy/`（或其 dev 子目录）执行 `docker compose up -d --build`，退出码 0；30s 内 `curl -s $BASE/artifactory/api/system/ping` 返回 `OK` | P0 |
| FR-6-AC2（健康端点） | `curl -s $BASE/api/v1/health` → 200，JSON 含 `"status":"ok"`；`curl -sf` 退出码 0 | P0 |
| FR-6-AC3（持久化） | 上传一个文件后 `docker compose restart`，同路径 GET 仍 200 且 sha256 不变；`docker compose down && up -d`（不 `-v`）后仍 200 | P0 |
| FR-6-AC4（README 快速开始） | 仓库根 `README.md` 含「快速开始」小节：构建 → 启动 → 建仓 → 上传 → 下载 → 校验 五步命令；QA 在全新 clone + 空 Docker 环境逐行执行，全部退出码 0 | P1 |
| FR-6-AC5（裸二进制启动） | 不用 Docker：`make build && ./bin/binflow-server`（默认参数，数据目录默认 `./data`，可用 `BINFLOW_HOME`/配置文件覆盖）→ ping 同样 `OK`；`Ctrl-C` 干净退出（退出码 0，无协程泄漏告警日志） | P0 |

---

## 5. 兼容性矩阵（本产品核心章节）

### 5.1 兼容层级定义（全项目统一，不留模糊地带）

| 层级 | 含义 | 判定标准 |
|---|---|---|
| **兼容** | 端点路径、方法、参数、成功状态码、响应关键字段与 Artifactory 一致；老脚本不改可用 | 真实客户端命令（curl 级）不改脚本通过 |
| **兼容（子集）** | 同上，但字段/参数只实现高频子集；未实现字段不返回（不返回错误值） | 命令通过 + 响应为请求字段的超集/子集明示清单 |
| **语义等同但路径不同（/api/v1）** | 能力对应 Artifactory 某端点，但 BinFlow 用自有路径与更干净的 schema；不承诺原路径 | 仅 `/api/v1` 文档化的命令通过 |
| **有意不兼容** | 明确决定不做的行为，返回确定性错误（400/404）而非静默忽略或 5xx | 返回码与错误体符合本 PRD 规定 |

通用错误契约（E-01，全端点适用）：非 2xx 一律返回
`{"errors":[{"status":<code>,"message":"<人类可读>"}]}`，`Content-Type: application/json`。未实现端点返回 404 + E-01（message 含 `not implemented in BinFlow` 类字样）。

### 5.2 M1 端点矩阵

「置信度」：高 = Artifactory 公开文档明确；中 = 公认行为/PRD 暂定，待 `docs/reverse/rest-api.md` 校准（见 §5.5）。

| # | 端点（方法 路径） | Artifactory 行为要点 | 层级 | 优先级 | 置信度 | 验收命令 |
|---|---|---|---|---|---|---|
| E-02 | `GET /artifactory/api/system/ping` | 200，body 纯文本 `OK`，无需认证 | 兼容 | P0 | 高 | C01 |
| E-03 | `GET /artifactory/api/system/version` | 200 JSON：`version`、`revision`（BinFlow 填自身版本，是否伪装 Artifactory 版本号见 Q4） | 兼容（子集） | P1 | 高 | C28b |
| E-04 | `GET /artifactory/api/repositories` | 200 数组：`key/description/type/packageType/url` | 兼容 | P0 | 高 | C05 |
| E-05 | `GET /artifactory/api/repositories/{key}` | 200 仓库配置 JSON，`rclass/packageType` | 兼容（子集） | P0 | 高 | C06 |
| E-06 | `PUT /artifactory/api/repositories/{key}` | body `{"rclass":"local","packageType":"generic",...}`；201 新建 / 200 更新；`remote`/`virtual` → 400（M1 有意不支持） | 兼容（子集） | P0 | 高 | C03 / C04 / E-07 |
| E-07 | `PUT /api/repositories/{key}` rclass=remote/virtual | Artifactory 支持；BinFlow M1 返回 400「supported from M3」 | 有意不兼容（阶段性） | P0 | — | C26 |
| E-08 | `DELETE /artifactory/api/repositories/{key}` | 空仓 2xx；非空需 `?deleteContent=true` 否则 400 | 兼容 | P1 | 高 | C19 |
| E-09 | `GET /artifactory/api/storage/{repo}/{path}` | item info：`children/checksums{sha1,md5,sha256}/size/created/createdBy/lastModified/modifiedBy/downloadUri/uri/mimeType` | 兼容（子集） | P0 | 高 | C10 / C16 |
| E-10 | `GET /api/storage/{repo}?list&deep=&depth=` | `fileList[]`（`uri/size/modified/sha1`…M1 子集：uri+size） | 兼容（子集） | P2 | 高 | C17 |
| E-11 | `PUT /artifactory/{repo}/{path}`（上传） | 201；响应 `checksums`；支持 `X-Checksum-Sha256` 预校验（不一致 400）；`X-Checksum-Deploy: true` 空体秒传（未命中 404/400 待校准） | 兼容 | P0 | 高（400）/ 中（秒传未命中码） | C07/C13/C14/C15 |
| E-12 | `GET /artifactory/{repo}/{path}`（下载） | 200 流式；头 `X-Checksum-Sha256/X-Checksum-Sha1/X-Checksum-Md5` | 兼容 | P0 | 高 | C08 |
| E-13 | `HEAD /artifactory/{repo}/{path}` | 200，`Content-Length` + checksum 头 | 兼容 | P0 | 高 | C09 |
| E-14 | `DELETE /artifactory/{repo}/{path}` | 2xx（BinFlow 定 204，校准项 §5.5）；目录递归删；重复删 404 幂等 | 兼容 | P0 | 中 | C18 |
| E-15 | `PUT /artifactory/{repo}/{dir}/`（结尾斜杠建目录） | 2xx 建空目录 | 兼容 | P1 | 中 | C16 |
| E-16 | `PUT /api/security/password` | body `oldPassword/newPassword`，2xx | 兼容 | P1 | 高 | C20 |
| E-17 | `POST /api/security/token` | 200 `access_token/token_id/expires_in`；M1 子集：不做 refresh_token/audience/scope 完整语义 | 兼容（子集） | P0 | 中 | C21a |
| E-18 | `POST /api/security/token/revoke` | 2xx，吊销后 401 | 兼容 | P0 | 中 | C21c |
| E-19 | `POST/GET /artifactory/api/security/users` | 建/列用户；响应永不含口令；M1 不做组管理（M4） | 兼容（子集） | P0 | 高 | C22a |
| E-20 | 认证头：Basic（口令或 Token 充当口令）、`X-JFrog-Art-Api` | 失败 401 + `WWW-Authenticate: Basic realm=...` + E-01 | 兼容 | P0 | 高 | C02/C21b |
| E-21 | 错误体格式 E-01（所有非 2xx） | `{"errors":[{status,message}]}` | 兼容 | P0 | 高 | C12 |
| E-22 | `GET /api/v1/health` | BinFlow 自有：`{"status":"ok"}` + 各子系统状态 | /api/v1 | P0 | — | C28a |
| E-23 | `GET /api/v1/storage/stats` | BinFlow 自有（QA/运维用）：blob 数、逻辑字节数、物理字节数（验证去重） | /api/v1 | P1 | — | C12 |
| E-24 | `POST/GET/DELETE /api/v1/permissions` | 权限对象 CRUD。Artifactory 对应 `/api/v2/security/permissions/*`，M1 不承诺该路径（M4 权限完整版再评估是否补 v2 路径） | 语义等同但路径不同 | P0 | — | C22b/c |
| E-25 | `X-Explode-Archive` 上传头 | Artifactory 解包归档；BinFlow M1 拒绝：400「not supported」 | 有意不兼容 | P1 | — | C25 |
| E-26 | `/api/system/info` 及 `/api/search/**`、`/api/replication/**`、`/v2/**`、`/api/npm/**`、`/api/pypi/**` 等 | 未实现端点：404 + E-01（message 注明未实现）；**绝不**返回 500 或空 200 | 有意不兼容 | P0 | — | C24 |

> 计数：**26 条**。兼容/兼容（子集）**18** 条；`/api/v1` **3** 条；有意不兼容 **5** 条（E-07/E-25/E-26 + 删除码差异见 §5.5）。

### 5.3 M1 核心验收命令（QA 直接引用）

编号与 §4 AC、§5.2 矩阵互相引用。`$BASE/$AUTH/$ADMIN_PW` 见 §4 约定；`jq` 为 QA 环境标配。

```bash
# 环境准备
export BASE=http://localhost:8080
export ADMIN_PW=password            # 若按 Q3 采纳环境变量引导，则取启动时设定值

# C01 ping（E-02）
curl -s $BASE/artifactory/api/system/ping            # 期望输出: OK

# C02 未认证拒绝（E-20）
curl -s -o /dev/null -w '%{http_code}\n' $BASE/artifactory/api/repositories   # 401

# C03 建仓（E-06）
curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"M1 QA"}' \
  -o /dev/null -w '%{http_code}\n'                   # 201（重复执行为 200）

# C04 非法 repo key（E-06）
curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/api/repositories/Bad_Key! \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic"}' \
  -o /dev/null -w '%{http_code}\n'                   # 400

# C05 列仓（E-04）
curl -su admin:$ADMIN_PW $BASE/artifactory/api/repositories | jq -r '.[].key' | grep -x generic-local   # 退出码 0

# C06 查仓（E-05）
curl -su admin:$ADMIN_PW $BASE/artifactory/api/repositories/generic-local | jq -r '.rclass,.packageType' # local \n generic

# C07 上传 + 服务端 checksum 对账（E-11 / FR-2-AC2）
dd if=/dev/urandom of=artifact.bin bs=1m count=10
SHA=$(sha256sum artifact.bin | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T artifact.bin $BASE/artifactory/generic-local/acme/artifact.bin \
  | jq -r '.checksums.sha256'                        # 输出 == $SHA（diff 断言）
SIZE=$(stat -f%z artifact.bin)                       # linux: stat -c%s
curl -su admin:$ADMIN_PW $BASE/artifactory/api/storage/generic-local/acme/artifact.bin | jq '.size'   # == $SIZE

# C08 下载 + 校验（E-12）
curl -su admin:$ADMIN_PW -o dl.bin $BASE/artifactory/generic-local/acme/artifact.bin
sha256sum artifact.bin dl.bin                        # 两行相同

# C09 HEAD（E-13）
curl -su admin:$ADMIN_PW -sI $BASE/artifactory/generic-local/acme/artifact.bin | \
  awk -F': ' 'tolower($1)=="x-checksum-sha256"{gsub("\r","");print $2}'   # == $SHA

# C10 item info（E-09）
curl -su admin:$ADMIN_PW $BASE/artifactory/api/storage/generic-local/acme/artifact.bin | jq -r '.checksums.sha256'  # == $SHA

# C12 去重观测（E-23 / FR-2-AC1）
curl -su admin:$ADMIN_PW $BASE/api/v1/storage/stats | jq '.blobs'          # 上传同内容到 b/y.bin 前后值不变

# C13 客户端 checksum 一致（E-11）
curl -su admin:$ADMIN_PW -T artifact.bin -H "X-Checksum-Sha256: $SHA" \
  $BASE/artifactory/generic-local/acme/v2.bin -o /dev/null -w '%{http_code}\n'          # 201

# C14 客户端 checksum 不一致（E-11）
curl -su admin:$ADMIN_PW -T artifact.bin -H "X-Checksum-Sha256: $(printf '0%.0s' {1..64})" \
  $BASE/artifactory/generic-local/acme/bad.bin -o /dev/null -w '%{http_code}\n'         # 400

# C15 checksum deploy：命中 / 未命中（E-11）
curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $SHA" \
  $BASE/artifactory/generic-local/acme/copy.bin -o /dev/null -w '%{http_code}\n'        # 201
curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $(printf 'f%.0s' {1..64})" \
  $BASE/artifactory/generic-local/acme/miss.bin -o /dev/null -w '%{http_code}\n'        # 404（§5.5 校准项）

# C16 目录（E-15 / E-09）
curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/generic-local/acme/ -o /dev/null -w '%{http_code}\n'  # 2xx
curl -su admin:$ADMIN_PW $BASE/artifactory/api/storage/generic-local/acme | jq '.children | length'    # > 0

# C17 list（E-10，P2）
curl -su admin:$ADMIN_PW "$BASE/artifactory/api/storage/generic-local?list&deep=1" | jq -r '.files[].uri' # 列出全路径

# C18 删除 + 幂等 404（E-14）
curl -su admin:$ADMIN_PW -X DELETE $BASE/artifactory/generic-local/acme/artifact.bin -o /dev/null -w '%{http_code}\n'  # 204
curl -su admin:$ADMIN_PW -X DELETE $BASE/artifactory/generic-local/acme/artifact.bin -o /dev/null -w '%{http_code}\n'  # 404
curl -s -u admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/artifactory/generic-local/acme/artifact.bin        # 404

# C19 删仓（E-08）
curl -su admin:$ADMIN_PW -X DELETE $BASE/artifactory/api/repositories/generic-local -o /dev/null -w '%{http_code}\n'            # 400（非空）
curl -su admin:$ADMIN_PW -X DELETE "$BASE/artifactory/api/repositories/generic-local?deleteContent=true" -o /dev/null -w '%{http_code}\n'  # 2xx

# C20 改密（E-16）
curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/api/security/password \
  -H 'Content-Type: application/json' -d '{"oldPassword":"password","newPassword":"n3w!pw"}' -o /dev/null -w '%{http_code}\n' # 2xx

# C21 Token 发放/使用/吊销（E-17/E-18/E-20）
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/artifactory/api/security/token -H 'Content-Type: application/json' -d '{}' | jq -r .access_token)
curl -su admin:$TOKEN -o /dev/null -w '%{http_code}\n' $BASE/artifactory/api/repositories   # 200
TID=$(curl -su admin:$ADMIN_PW $BASE/artifactory/api/security/token 2>/dev/null | jq -r '.tokens[0].token_id' 2>/dev/null) # 若提供列接口；否则创建时记录
curl -su admin:$ADMIN_PW -X POST $BASE/artifactory/api/security/token/revoke \
  -H 'Content-Type: application/json' -d "{\"token\":\"$TOKEN\"}" -o /dev/null -w '%{http_code}\n'                          # 2xx
curl -su admin:$TOKEN -o /dev/null -w '%{http_code}\n' $BASE/artifactory/api/repositories   # 401

# C22 用户与路径 ACL（E-19/E-24）
curl -su admin:$ADMIN_PW -X POST $BASE/artifactory/api/security/users -H 'Content-Type: application/json' \
  -d '{"name":"ci-bot","password":"ci-pw","admin":false}' -o /dev/null -w '%{http_code}\n'  # 201
curl -su ci-bot:ci-pw -o /dev/null -w '%{http_code}\n' $BASE/artifactory/generic-local/ci-out/x.bin  # 403（未授权，先建 x.bin 由 admin 完成）
curl -su admin:$ADMIN_PW -X POST $BASE/api/v1/permissions -H 'Content-Type: application/json' -d '{
  "name":"ci-out-rw","repos":["generic-local"],"includePatterns":["ci-out/**"],
  "principals":{"users":{"ci-bot":["read","write"]}}}' -o /dev/null -w '%{http_code}\n'    # 2xx
curl -su ci-bot:ci-pw -T artifact.bin $BASE/artifactory/generic-local/ci-out/y.bin -o /dev/null -w '%{http_code}\n'  # 201
curl -su ci-bot:ci-pw -X DELETE $BASE/artifactory/generic-local/ci-out/y.bin -o /dev/null -w '%{http_code}\n'        # 403（无 delete）
curl -su ci-bot:ci-pw -T artifact.bin $BASE/artifactory/generic-local/other/z.bin -o /dev/null -w '%{http_code}\n'   # 403（越权路径）

# C24 未实现协议端点（E-26）
curl -s $BASE/v2/ -o /dev/null -w '%{http_code}\n'          # 404
curl -s $BASE/api/npm/xx | jq -r '.errors[0].status'        # 404

# C25 拒绝归档解包（E-25）
curl -su admin:$ADMIN_PW -T artifact.bin -H 'X-Explode-Archive: true' \
  $BASE/artifactory/generic-local/acme/z.bin -o /dev/null -w '%{http_code}\n'  # 400

# C26 拒绝 remote/virtual（E-07）
curl -su admin:$ADMIN_PW -X PUT $BASE/artifactory/api/repositories/docker-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io"}' -o /dev/null -w '%{http_code}\n'  # 400

# C28 健康与版本
curl -sf $BASE/api/v1/health | jq -r .status                # ok
curl -su admin:$ADMIN_PW $BASE/artifactory/api/system/version | jq -r .version  # 非空（值见 Q4）

# C29 重启持久化（FR-6-AC3）
docker compose restart && sleep 5
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/artifactory/generic-local/ci-out/y.bin  # 200

# C30 慢上传中断 + kill -9（FR-2-AC3/AC4）
timeout -s KILL 3 curl -su admin:$ADMIN_PW -T big.bin --limit-rate 64k \
  $BASE/artifactory/generic-local/acme/big.bin; echo $?     # 137（被 kill），非 0
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/artifactory/generic-local/acme/big.bin  # 404
```

### 5.4 后续里程碑协议边界（docker/mvn/npm/pip 如何「不影响 M1」）

M1 通过以下四条边界保证后续协议可以**追加**而非**返工**：

1. **路径命名空间预留**（全部挂在根路径，与 `/artifactory` 内容路径互不冲突）：
   - M2：`/v2/**`（Docker Registry v2 + OCI + token 认证流 `/v2/token` 或等价）
   - M3：`/api/npm/**`、`/api/pypi/**`、`/api/pypi-ui/**`、Maven 仓库内容直接走 `/artifactory/<maven-repo>/<layout path>`（layout 解析按 repo 的 packageType 分发，M1 的 generic 直通逻辑必须做成按 packageType 可扩展的分发点——这是对 architect 的需求输入，不是实现指令）
2. **repo 模型前向兼容**：`packageType` 字段 M1 接受 `generic`；M2+ 追加 `docker` 等值时**不得**改动 `rclass/packageType` 字段语义与既有 generic 行为；`rclass` 的 `remote/virtual` 值 M3 启用。
3. **认证前向兼容**：M1 的 Basic/Token 中间件必须可按路径前缀挂载（M2 的 `/v2/token` 匿名可探、`/v2/` 需 Bearer，M3 各协议匿名读开关独立）；M1 不做 Bearer/token-endpoint 本体。
4. **明确不承诺**：M1 不包含任何 Docker manifest/blob 语义、Maven metadata 语义、npm tarball 语义、PyPI simple index 语义；QA 在 M1 只验证 E-26（这些路径 404）。

### 5.5 待逆向规格校准项（不阻塞开发，规格落地后回写）

| 项 | PRD 暂定值 | 校准来源 |
|---|---|---|
| DELETE 文件成功状态码 | 204 No Content | docs/reverse/rest-api.md（Artifactory 实际 200 vs 204） |
| checksum deploy 未命中的状态码 | 404 | 同上 |
| X-Checksum-Sha256 不匹配的状态码与 message 文案 | 400 | 同上 |
| mkdir（结尾斜杠 PUT）行为 | 2xx 建目录 | 同上 |
| token 响应字段名（`access_token`/`token_id`/`expires_in`） | 按 Artifactory 公开文档 | docs/reverse/auth-model.md |
| item info 字段全集（M1 子集是否缺迁移工具依赖字段） | §FR-3-AC6 列表 | docs/reverse/rest-api.md |

---

## 6. 非功能需求（NFR）

### 6.1 性能（M1 底线）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P1 冷启动 | 空库启动到 `ping` 返回 `OK` < 2s（QA：启动后 0.1s 间隔轮询计时） | P0 |
| NFR-P2 并发拉取 | 100 并发 GET 同一 10MB 文件（QA 用 `seq 100 \| xargs -P100 -I{} curl -sf -o /dev/null ...`），退出码全 0、服务端 0 条 5xx 日志 | P0 |
| NFR-P3 流式上传 | 1GB PUT 全程服务进程 RSS 增量 < 256MB（FR-2-AC6） | P1 |
| NFR-P4 空载内存 | 启动完成、无请求 60s 后 RSS < 100MB（PRODUCT 成功标准前移观测，M1 记录不设硬门，M5 GA 设门） | P2 |

### 6.2 安全底线

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S1 口令存储 | 用户口令用 bcrypt 或 argon2id 哈希存储；元数据库文件中 grep 不到明文口令 | QA 检查 SQLite 文件内容 |
| NFR-S2 Token 存储 | Token 只存哈希（创建时明文仅返回一次） | 同上，grep 不到已发 Token 明文 |
| NFR-S3 日志脱敏 | 日志不得出现口令、Token 明文、Authorization/X-JFrog-Art-Api 头值 | 上传/登录全流程后 grep 服务日志 |
| NFR-S4 路径穿越 | FR-4-AC10（含 URL 编码变体） | C 序列 + 变体 |
| NFR-S5 临时文件 | 上传临时文件权限 0600，完成/中断后删除或入隔离区且不被任何路径引用 | FR-2-AC3 |
| NFR-S6 TLS | M1 不要求内置 TLS（由部署层反代终结，compose 里 traefik/nginx 属 M5 部署矩阵）；文档必须明示「M1 明文 HTTP，仅限内网/本机」 | FR-6-AC4 README 有此提示 |
| NFR-S7 SSRF | M1 无 remote 仓库，无出站请求；如实现中引入任何 HTTP 出站调用须回报主会话重新评审 | 代码评审项 |

### 6.3 可观测性底线

- 每请求一行结构化日志（JSON 或 key=value）：`time, level, method, path, status, duration_ms, remote_addr, user（认证后）, bytes_in/out`；未认证请求 `user=-`，**绝不**记录认证头。
- `/api/v1/health`（E-22）：`status` + 至少含 `storage`（数据目录可写）与 `metadata`（SQLite 可用）两个子系统状态。
- 日志级别可由配置/环境变量调整（`debug/info/warn/error`），默认 `info`。
- Prometheus 指标、审计日志、请求审计表均为 M4/M6+，M1 不做（§2.2）。

---

## 7. M1 验收剧本（QA roundtrip + 存储完整性总纲）

qa-engineer 按顺序执行，产出 `reports/agents/T-<qa票>-qa.md`，全绿 = 里程碑 DoD 第 2 条满足：

1. **工程基线**：FR-1-AC1..AC3（make build/test/lint）。
2. **冷启动**：FR-6-AC5 裸二进制 + FR-6-AC1 compose 双路径，NFR-P1 计时。
3. **仓库生命周期**：C03 → C04 → C05 → C06 → C19（建/校验/列/查/删）。
4. **制品 roundtrip**：C07 → C08 → C09 → C10 → C13 → C14 → C15 → C18（含服务端 checksum 对账、客户端校验、秒传、删除幂等）。
5. **存储完整性**：C12 去重前后对比 + FR-2-AC3 慢上传中断 + FR-2-AC4 kill -9 重启 + FR-2-AC6 1GB 流式（NFR-P3 采样）。
6. **认证与 ACL**：C02 → C20 → C21 → C22（含 NFR-S1/S2/S3 抽查）。
7. **边界拒绝**：C24 → C25 → C26 → FR-4-AC10/AC11（路径安全与非法路径）。
8. **压力**：NFR-P2 100 并发。
9. **文档**：FR-6-AC4 README 快速开始全新环境逐行复跑。
10. **持久化**：C29（compose restart / down+up 两轮）。

---

## 8. M1 DoD（对齐 ROADMAP「里程碑完成定义」）

1. §4 全部 P0/P1 AC 通过 qa-engineer 验证并附命令输出证据（P2 延后须在 BOARD 记录）；
2. §7 剧本全绿；
3. 逆向规格 4 份（rest-api / storage-layout / config-formats / repo-semantics M1 部分）已落地，且 §5.5 校准项已回写本 PRD；
4. README 快速开始可复跑；
5. 主会话完成 `m1-done` tag。

---

## 9. 开放问题（需用户/上游决策，本 PRD 不擅自拍板）

| # | 问题 | 影响面 | PRD 暂行假设（仅供开发不停工，用户可推翻） |
|---|---|---|---|
| Q1 | 兼容端点是否**只**暴露在 `/artifactory` 前缀下，还是同时在根路径提供镜像（部分脚本硬编码无前缀路径）？ | 全部兼容端点；迁移体验 | 仅 `/artifactory` 前缀，不做根镜像 |
| Q2 | 匿名读默认值：Artifactory 传统默认「开」，BinFlow 建议安全默认「关」。取哪个默认？ | FR-5、全部 GET 的 401 行为 | 默认关，配置可开 |
| Q3 | admin 初始口令引导：环境变量 `BINFLOW_ADMIN_PASSWORD` + 文档化默认值（对齐 Artifactory 的 `password`），还是首启生成随机口令打印一次，还是强制首登改密？ | FR-5-AC1、部署文档、QA 剧本取值 | 环境变量优先，缺省 `password`（文档标注仅限评估） |
| Q4 | `/api/system/version` 是否如实返回 BinFlow 自身版本（如 `1.0.0-m1`），还是伪装 Artifactory 版本号以安抚按版本探测的迁移工具？ | E-03；Artifactory 迁移工具兼容性 | 如实返回 BinFlow 版本，另附 `product:"BinFlow"` 类字段 |
| Q5 | ROADMAP 未写明 Postgres 可选支持落在哪个里程碑（ADR-0003 说「可选」）。M1 只做 SQLite 是否符合预期，Postgres 归 M4/M5 还是 GA 后？ | FR-3-AC10；元数据 schema 设计的抽象程度 | M1 仅 SQLite，Postgres 不承诺里程碑 |
| Q6 | Range/断点续传现为 P2。M2 Docker blob 拉取强依赖 Range；是否提前到 M1 P1？ | FR-4-AC14；M2 排期风险 | 维持 P2，M2 开工前补 |
| Q7 | Go module 路径组织名（`github.com/<org>/binflow` 的 `<org>`）未定，影响 go.mod 与 import 路径，后改成本高。 | FR-1-AC5 | 暂用 `github.com/binflow/binflow`，待用户定名 |
| Q8 | 根 `README.md` 的唯一写入者在文件地图中未指定（ROADMAP M1 含「README 快速开始」）。归 devops-engineer 还是 tech-writer？ | 流程归属，避免双写 | devops-engineer 起草，tech-writer M5 收编 |

---

*本 PRD 由 product-manager 依据 PRODUCT.md v1 与 ROADMAP.md M1 条目撰写；与 `docs/reverse/` 规格冲突时按 §5.5 流程回写修订。*
