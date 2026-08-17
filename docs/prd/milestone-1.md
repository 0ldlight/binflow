# PRD — M1 内核基座（Kernel Foundation）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-1.md` |
| 里程碑 | M1 — 内核基座（对应 ROADMAP.md「M1 — 内核基座（当前）」全部条目） |
| 状态 | **v1.3**（8 项用户定案 + §5.5 六项校准全部回写完毕：v1.2 依据 rest-api/repo-semantics，v1.3 依据 auth-model.md（T-23）收口 token/改密/建用户四处） |
| 上游依据 | PRODUCT.md（愿景/Non-goals/成功标准/技术约束）、ROADMAP.md、DECISIONS.md ADR-0002/0003、用户对 8 项开放问题的定案（§9） |
| 下游消费者 | tech-lead（拆票）、architect（ADR/设计）、dev 各角色（实现）、qa-engineer（验收） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-17 | 初版：M1 范围、FR-1~FR-6、兼容矩阵 26 端点、C01~C30 验收命令、8 项开放问题 |
| v1.1 | 2026-08-17 | 纳入用户对 §9 全部 8 项问题的定案：① **Q1 统一 `/binflow` 前缀**（推翻 v1.0 暂行假设「仅 `/artifactory`」）——全文端点路径、C01~C30 命令、§5.1 兼容层级定义（改为「前缀重写后语义兼容」）、§3 场景 D 迁移表述同步更新；`/artifactory/**` 返回 404 并提示新前缀；Docker `/v2/` 协议固定路径例外记入 §5.4。② **Q2 匿名读默认开**（推翻 v1.0 暂行假设「默认关」）——FR-5 模型改写、新增 AC12/AC13、新增命令 C23/C27、新增 NFR-S8。③ **Q5 纯 Go SQLite 零 CGO**——新增 FR-1-AC7、FR-3 技术注记；FR-3-AC10 维持。④ **Q7** module 路径落定 `github.com/lzwzzy/binflow`。⑤ Q3/Q4/Q6/Q8 维持暂行假设转为定案。⑥ §9 改为「已决决策」表。§5.5 待逆向规格校准项不变 |
| v1.2 | 2026-08-17 | 依据已落地的 `docs/reverse/`（rest-api.md / repo-semantics.md，T-21）对 §5.5 六项校准项定案回写 + 采纳 tech-lead 评审 R1/R2/R7：① **R1：客户端 checksum 不一致 → 409**（repo-semantics.md §5 `client-checksums` 策略，高置信度；推翻 v1.1 的 400）——FR-4-AC5、C14、E-11 更新，message 含 received/actual 双值。② **R2：建仓成功 → 200 纯文本**（rest-api.md §2，高置信度；推翻 v1.1 的 201）——FR-3-AC1、C03、E-06 更新；更新走 POST、body key 不一致 400/409 语义补记。③ §5.5 六项全部定案（①DELETE 204 无 body ②checksum deploy 未命中 404 ③409 同 R1 ④mkdir 尾斜杠 201 ⑤token 字段按 BinFlow 自有语义暂定、待 auth-model.md（T-23）⑥item info 字段全集按 rest-api.md §3：含 `lastUpdated`、`size` 为字符串、含 `originalChecksums`）；§5.5 由「待校准」改为「校准记录」，置信度列由「待校准」改「高（已定案）」。④ E-14/E-15 置信度升「高」；FR-4-AC7/E-11 的「待校准」标记移除。⑤ 新增两条增补规格（high-value，不扩 M1 范围）：下载头 `ETag=<sha1>`/`Last-Modified`/`Accept-Ranges: bytes` + 304/416 条件请求语义（FR-4 新增 AC15，P2，Range 416 补入 AC14）、同 checksum 幂等重传免覆盖权限检查（记 §4 FR-4 表后注，FR-5 实现必须保留该分支）。Q1~Q8 已决决策表保持 v1.1 原样 |
| v1.3 | 2026-08-17 | 依据 T-23 产出的 `docs/reverse/auth-model.md`（§3/§5 校准建议，高置信度）收口 §5.5⑤ 并校准四处：① **E-17 token 创建**——请求改 **form-urlencoded**（真实端点只吃 form，JSON 作 BinFlow 扩展）；响应字段集改 `access_token / token_type("Bearer") / expires_in(永不过期时缺省) / scope / refresh_token(仅 refreshable)`，**无 token_id**（BinFlow 超集扩展附 token_id 便于按 id 吊销）；grant_type/scope/expires_in/refreshable/audience 参数语义按规格 §3 落表（M1 子集：username/expires_in/refreshable 可不做）；FR-5-AC4、C21a 更新。② **E-18 revoke**——form 参数 `token` XOR `token_id`（同传 400 `token and token_id are mutually exclusive` / 都缺 400 `token or token_id are required`）；成功 200 纯文本 `Token revoked`；不存在/已吊销仍 200 `Token not found`（幂等）；FR-5-AC6、C21c 更新。③ **E-16 改密**——7.x 无 `PUT /api/security/password`；BinFlow 保留自有路径 + **补真实路径别名** `POST /api/security/users/authorization/changePassword`（userName/oldPassword/newPassword1/newPassword2），**旧口令错误 400（非 401）**；FR-5-AC3、C20 更新。④ **E-19 建用户**——真实为 `PUT /api/security/users/{name}`（201 无 body）；BinFlow 保留自有 POST + 补该兼容路由；GET 列表元素 `{name,uri,realm}`；email/password blank → 400（自有 POST 路由同一条校验链）；FR-5-AC7/AC11、C22a 更新。⑤ §5.1 补**错误体三分层**注记（制品 `errors[]` / 用户管理纯文本 / token OAuth 风格 `{"error","error_description"}`），token 端点采用 OAuth 风格；§5.5⑤ 定案收口、置信度升高；E-16~E-19 置信度升「高」。顺手修复 QA 剧本一处既有缺陷：C20 改密后 `$ADMIN_PW` 未更新会导致 C21/C22 连续 401，补 `export ADMIN_PW` 行 |

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
| 元数据零依赖 | 默认内嵌纯 Go SQLite，`docker compose up` 后无需手工初始化即可用 | PRODUCT 技术约束 + Q5 定案 |
| 工程化 | `make lint` / `make test` / `make build` 全绿，CI 流水线存在且通过 | ROADMAP M1 脚手架条目 |

### 1.3 上游依赖与并行关系

- M1 内的「逆向规格」与「ADR/架构设计」两条 ROADMAP 条目与本报并行产出。本 PRD 中关于 Artifactory 行为的描述**以公开文档与公认行为为暂定依据**，逐条标注置信度（§5.2）；`docs/reverse/rest-api.md` 等规格落地后，若与本文有出入，**以规格为准回写本 PRD**（增量修订，版本号 +0.1）。
- 本 PRD 不规定内部实现（目录结构、SQLite schema、blob 目录推导），那些归 architect 的 ADR 与设计文档；本 PRD 只约束**可观察行为**。唯一例外是 Q5 定案的技术约束（纯 Go SQLite、零 CGO），因其直接影响交付形态（M5 交叉编译矩阵）。

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
- 不做 Artifactory 全量 REST 兼容（只做 §5.2 列出的高频子集，其余走 `/binflow/api/v1` 或 404）。

**M1 里程碑级 Non-goals（后续里程碑交付）**

| 不做项 | 归属 | M1 的隔离边界 |
|---|---|---|
| remote 仓库（代理缓存）/ virtual 仓库（聚合） | M3 | `rclass` 仅接受 `local`；`remote`/`virtual` 创建请求返回 400（§5.2 E-07） |
| Docker Registry v2（根路径 `/v2/` 全部端点） | M2 | `/v2/**` 一律 404 标准错误体（E-26） |
| Maven / npm / PyPI 协议适配 | M3 | `/binflow/api/npm/**`、`/binflow/api/pypi/**`、`/binflow/api/pypi-ui/**`、Maven layout 解析一律 404（E-26）；M1 不解析任何包 layout，generic 路径即原始路径 |
| Web 控制台（登录/浏览/上传 UI） | M4 | 不内嵌任何 HTML 页面（go:embed 占位可留，但无路由）；M1 唯一界面是 REST API |
| 搜索、审计日志、GC、配额、备份恢复 | M4 | 全部不做；`/binflow/api/search/**` 404 |
| users/groups 完整权限模型（组、权限继承、UI） | M4 | M1 仅：admin + 普通用户 + 单层路径 ACL（§FR-5） |
| Prometheus metrics | M6+ | M1 可观测性底线 = 结构化日志 + `/binflow/api/v1/health`（§6.3） |
| 多存储后端（S3） | M6+ | M1 仅本地文件系统 blob 存储 |
| 断点续传（Range/`If-Range`）、分块并发上传 | M2 前置 | M1 定 P2（Q6 定案），允许延后，但 M2 docker 层拉取开工前必须落地 |
| 归档解包上传（`X-Explode-Archive`） | 不排期 | M1 显式拒绝（E-25） |
| `/artifactory` 前缀仿真与根路径镜像 | 不排期 | Q1 定案：统一 `/binflow` 前缀；`/artifactory/**` 与其他非 `/binflow` 根路径请求一律 404 + 前缀提示（E-26） |

---

## 3. 用户与场景（M1 视角）

M1 的「真实用户」是**平台工程师的 CI 脚本**，不是终端人类用户（人类界面 M4 才有）。

- **场景 A（CI 构建产物归档）**：流水线 job 用 `curl -T` 把构建产物（二进制、tarball、SBOM）推入 `build-outputs` 仓库，靠 sha256 确保落盘内容与构建机一致。
- **场景 B（内部工具分发）**：平台团队把 `kubectl` 替代品、内网 CLI 放在 `tools` 仓库，开发机用 `curl -O` + `sha256sum -c` 拉取校验；匿名读默认开（Q2 定案），内网开发机免认证即可拉取。
- **场景 C（最小权限）**：给 CI 专用账号只授 `ci-out/**` 路径的写权限，越权路径写入被 403 拒绝。
- **场景 D（迁移预演）**：从 Artifactory 迁出的团队复用既有 curl 脚本打 BinFlow，**需把 base path `/artifactory` 全局替换为 `/binflow`（一次 sed 即可）**；repo key、制品路径、认证头、错误语义保持不变——迁移成本收敛为前缀替换，而非重写脚本。

---

## 4. 功能需求

约定：优先级 P0 = M1 关键路径，缺一不可；P1 = M1 应完成；P2 = 可延后（延后需主会话在 BOARD 记录）。所有 AC 中 `BASE=http://localhost:8080`（默认端口，可配置）、管理员账号 `admin`、初始口令见 FR-5-AC1，示例统一写作 `$ADMIN_PW`。

**路径前缀总则（Q1 定案）**：BinFlow 全部端点统一挂 `/binflow` 前缀——
- 兼容端点：`/binflow/api/...`（对应 Artifactory 的 `/artifactory/api/...`，前缀替换）
- 内容路径：`/binflow/<repo>/<path>`
- 自有端点：`/binflow/api/v1/...`
- 不使用 `/artifactory` 前缀，不做根路径镜像；协议固定路径（Docker `/v2/`）例外见 §5.4。

### FR-1 脚手架与工程化（devops-engineer）

**用户故事**：作为开发者，我希望 clone 仓库后一条命令完成构建/测试/静态检查，任意 CI 环境拿到的是同一套工程契约，避免「本地能跑 CI 跑不了」。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-1-AC1 | `make build` 退出码 0，产出可执行文件 `bin/binflow-server`（linux/darwin 本机架构即可，交叉编译属 M5）；`./bin/binflow-server --help` 退出码 0 且输出含 `usage` | P0 |
| FR-1-AC2 | `make test` 退出码 0（`go test ./...` 全部 PASS，输出无 FAIL）；`make lint` 退出码 0（golangci-lint 零告警） | P0 |
| FR-1-AC3 | `gofmt -l .` 输出为空（退出码 0、无文件名列出） | P0 |
| FR-1-AC4 | CI 配置存在（`.github/workflows/ci.yml` 或等价物），内容至少触发 lint+test+build 三步；QA 以「文件存在 + 本地 make 三件套全绿」验收，CI 首跑绿由主会话确认 | P1 |
| FR-1-AC5 | `go.mod` 的 Go 版本为当前稳定版；模块路径为 `github.com/lzwzzy/binflow`（Q7 定案）；`go mod tidy` 无 diff | P1 |
| FR-1-AC6 | 构建输出打印二进制大小；M1 不设 < 40MB 硬门（GA 门槛），但 > 40MB 时 CI 给 warning | P2 |
| FR-1-AC7 | `CGO_ENABLED=0 go build ./...` 退出码 0（Q5 定案：SQLite 驱动用纯 Go 实现 `modernc.org/sqlite`，零 CGO 依赖，保障 M5 goreleaser 交叉编译矩阵） | P1 |

### FR-2 存储引擎（dev-go-storage）

**用户故事**：作为平台工程师，我希望仓库落盘的内容永不损坏、相同内容只存一份、上传中断不留脏数据——这样磁盘成本和可信度才配得上「制品仓库」。

语义定义（实现方式归 architect ADR，此处约束可观察行为）：
- blob 以内容 sha256 寻址存储；同一 blob 被多条 node 引用时物理只存一份（引用计数删除）。
- 上传走「上传会话」：先落临时区，流式计算 checksum，**通过后才对用户可见**；会话中断（连接断开 / 进程被杀）不得产生可见 node。
- 删除是幂等的：删除最后一条引用后 blob 可被回收（M1 允许延迟回收，但 node 必须立即不可见）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-2-AC1（去重） | 同一 10MB 内容分别 PUT 到 `a/x.bin` 与 `b/y.bin`（命令 C07 两次），两次均 201；`GET /binflow/api/v1/storage/stats`（C12）返回的 blob 总数第二次上传后**不变**；两条路径均能 200 下载且 sha256 一致 | P0 |
| FR-2-AC2（校验寻址） | C10（`GET /binflow/api/storage/{repo}/{path}`）返回的 `checksums.sha256` 与本地 `sha256sum` 输出一致（C07 的 jq 断言） | P0 |
| FR-2-AC3（原子可见性） | 构造慢上传：`dd if=/dev/urandom of=big.bin bs=1m count=100`，`timeout -s KILL 3 curl -su admin:$ADMIN_PW -T big.bin --limit-rate 64k $BASE/binflow/generic-local/acme/big.bin`（退出码非 0）；随后 `curl -s -o /dev/null -w '%{http_code}' .../acme/big.bin` 返回 **404**，且 C12 的 blob 计数较上传前不变（临时文件已清理或在隔离区，不计入 blob 数） | P0 |
| FR-2-AC4（kill -9 一致性） | 上传中 `kill -9 <server_pid>` 后重启服务：已完成的历史上传仍 200 可下载；中断的那条路径 404；`GET /binflow/api/v1/health` 200 | P0 |
| FR-2-AC5（覆盖写幂等） | 对同一路径再次 PUT 相同内容 → 2xx；stats blob 计数不变。PUT 不同内容 → 2xx，下载返回**新**内容，旧 blob 若无引用则计数减一（允许延迟） | P1 |
| FR-2-AC6（大文件流式） | 1GB 文件 PUT 成功（201）后 GET 回来 sha256 一致；上传期间服务进程 RSS 增量 < 256MB（QA 用 `ps -o rss` 采样，防全量读入内存） | P1 |

### FR-3 元数据与仓库模型（dev-go-core）

**用户故事**：作为 Artifactory 迁移用户，我希望仓库的创建/查询/删除走我熟悉的 REST 语义、用熟悉的字段（repo key、rclass、packageType），迁移脚本只改 base path 不改逻辑。

技术注记（Q5 定案）：元数据库为**纯 Go SQLite**（`modernc.org/sqlite`），进程内嵌入、零 CGO、零外部依赖；schema 设计归 architect ADR。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-3-AC1（建仓） | C03：`curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic"}'` → **HTTP 200**，body 纯文本 `Successfully created repository 'generic-local'`（依据 rest-api.md §2，高置信度；已存在则走更新语义，同为 200；BinFlow 不实现 body `key` 与路径不一致时的 400/409 区分，统一 400） | P0 |
| FR-3-AC2（列仓） | C05：`GET /binflow/api/repositories` → 200，jq 能取出 `key=="generic-local"`，`type/packageType` 字段存在且值合法 | P0 |
| FR-3-AC3（查仓） | C06：`GET /binflow/api/repositories/generic-local` → 200，`rclass=="local"`、`packageType=="generic"` | P0 |
| FR-3-AC4（repo key 校验） | PUT 建仓 key 为 `Bad_Key!`（大写/特殊字符）→ **400** + §5.2 E-01 标准错误体；合法 key 规则：`[a-z][a-z0-9-]{1,62}` | P0 |
| FR-3-AC5（删仓） | 空仓库 `DELETE /binflow/api/repositories/{key}` → 2xx；非空仓库不带参数 → **400**（message 含 `deleteContent` 提示）；带 `?deleteContent=true` → 2xx 且其下所有 node 之后 404 | P1 |
| FR-3-AC6（node 元数据） | C10：item info JSON 至少含 `repo / path（/ 开头）/ children（目录时）/ size（**字符串**） / created / createdBy / lastModified / modifiedBy / lastUpdated / downloadUri / uri / mimeType / checksums{sha1,md5,sha256} / originalChecksums{sha1,md5,sha256}`（字段全集依据 rest-api.md §3，高置信度）；`created/lastModified/lastUpdated` 为 ISO 8601 带毫秒与时区（如 `2026-08-17T12:34:56.789+08:00`） | P0 |
| FR-3-AC7（目录语义） | C16：`curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/generic-local/acme/`（结尾斜杠、空体）→ **201**（rest-api.md §1.1：MKDir 成功 201 + FolderInfo 形态，高置信度）；`GET /binflow/api/storage/generic-local/acme` → 200 且 `children[]` 列出其下条目；对目录 DELETE → 204 且递归删除其下全部 node | P1 |
| FR-3-AC8（list 查询） | C17：`GET '/binflow/api/storage/generic-local?list&deep=1'` → 200，`fileList[]`/`files[]` 含全部文件相对路径；M1 只需 `list/deep/depth` 参数 | P2 |
| FR-3-AC9（SQLite 嵌入） | `docker compose up -d` 后**不执行任何初始化 SQL**，C03 建仓即成功；重启实例（FR-6-AC3）后仓库与 node 全部仍在 | P0 |
| FR-3-AC10（Postgres） | M1 仅 SQLite；若检测到 Postgres 配置，启动时明确报错退出（退出码非 0 + 日志说明「Postgres 支持未启用」；启用里程碑另议，不在本 PRD 承诺） | P2 |

### FR-4 Generic 本地仓库：上传/下载/删除/校验（dev-registry-adapter）

**用户故事**：作为 CI 脚本作者，我希望 `curl -T` 上传后拿到服务端计算的 sha256 用于审计，下载时能从响应头拿到 checksum 做对账，还能不传内容就凭 sha256 秒传——这些是 Artifactory 上已经养成的用法。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-4-AC1（上传） | C07：`curl -su admin:$ADMIN_PW -T artifact.bin $BASE/binflow/generic-local/acme/artifact.bin` → **201**；响应体 JSON 的 `checksums.sha256 == $(sha256sum artifact.bin)`，`size` 等于文件字节数，`createdBy=="admin"` | P0 |
| FR-4-AC2（下载） | C08：GET 同路径 → 200，落盘文件 `sha256sum` 与源一致；响应头含 `X-Checksum-Sha256 / X-Checksum-Sha1 / X-Checksum-Md5` 且值正确；另含 `ETag: <sha1>`（无引号）、`Last-Modified`、`Accept-Ranges: bytes`（rest-api.md §1.4，高置信度） | P0 |
| FR-4-AC3（HEAD） | C09：`curl -sI` → 200，`Content-Length == size`，`X-Checksum-Sha256` 正确 | P0 |
| FR-4-AC4（客户端校验-一致） | C13：PUT 附带 `-H "X-Checksum-Sha256: $SHA"`（$SHA 为正确值）→ 201 | P0 |
| FR-4-AC5（客户端校验-不一致） | C14：同样 PUT 但头值为 64 位错误十六进制 → **409**，标准错误体（E-01），message 含 received 与 actual 两个 checksum 值（格式 `... received '<x>' but actual is '<y>'`，依据 repo-semantics.md §5 `client-checksums` 策略，高置信度）；落盘无该 node（GET 404） | P0 |
| FR-4-AC6（checksum deploy 命中） | C15a：`curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $SHA" $BASE/binflow/generic-local/acme/copy.bin`（空体）→ **201**，新路径可 200 下载；未传输任何 body | P0 |
| FR-4-AC7（checksum deploy 未命中） | C15b：同上但 $SHA 为不存在内容的 sha256 → **404**（rest-api.md §1.3：blob 不存在或 checksum 格式非法均 404，高置信度） | P1 |
| FR-4-AC8（删除文件） | C18：`curl -su admin:$ADMIN_PW -X DELETE .../acme/artifact.bin` → **204 无 body**（rest-api.md §1.1 / repo-semantics.md §4，高置信度）；随后 GET → 404；重复 DELETE → 404（幂等，不算错误） | P0 |
| FR-4-AC9（未认证写） | 不带认证 PUT → **401**，响应头含 `WWW-Authenticate: Basic realm=...`；错误体为标准 JSON（E-01）。Q2 定案：写操作永远需要认证，匿名读边界见 FR-5-AC12 | P0 |
| FR-4-AC10（路径安全） | `curl -su admin:$ADMIN_PW -T f $BASE/binflow/generic-local/a/../../etc/passwd` 与含 `%2e%2e` 编码变体 → **400**，服务进程工作目录与数据目录外无新文件 | P0 |
| FR-4-AC11（非法路径） | 双斜杠、空段（`//`）、超 512 字符路径 → 400 | P1 |
| FR-4-AC12（404 格式） | GET 不存在路径 → 404，body 为 E-01 错误体而非 HTML 栈页 | P0 |
| FR-4-AC13（Content-Type） | 下载响应必带 `Content-Type`，未知扩展名默认 `application/octet-stream`；按扩展名映射为 P2 | P2 |
| FR-4-AC14（Range） | `curl -H 'Range: bytes=0-99'` → 206 且字节数正确；多区间 Range 与 `If-Range` 不做 → 忽略或 200 全量（不得 5xx）。优先级维持 P2（Q6 定案），M2 docker blob 拉取开工前必须补齐。增补规格（rest-api.md §1.4，高置信度）：单区间命中 206 + `Content-Range`；区间非法 → **416** + `Content-Range: bytes */<total>` | P2 |
| FR-4-AC15（条件请求） | 增补规格（rest-api.md §1.4，高置信度）：下载响应头 `ETag: <sha1>`（无引号）、`Last-Modified`、`Accept-Ranges: bytes`；`curl -H "If-None-Match: <etag>"` → **304** 无 body。M1 定 P2（与 Range 同批，M2 前补齐） | P2 |

> 注（repo-semantics.md §3 增补，M1 骨架应实现）：**同 checksum 幂等重传不触发覆盖权限检查**——路径已存在且客户端带 checksum 与既有值相同 → 视为幂等重传直接成功；checksum 不同（或未带）→ 视为覆盖，需对旧节点有删除权限。此语义对 CI 重试场景关键，FR-5 的权限骨架实现时必须保留该分支。

### FR-5 基础认证与权限骨架（dev-go-core）

**用户故事**：作为安全管理员，我希望开箱即有一个 admin 账号、能给 CI 发 API Token 并随时吊销、能限制某账号只写指定路径——这是接入企业网络的最底线。

模型（对齐 Artifactory 语义，M1 骨架范围）：
- 认证方式：HTTP Basic（用户名+口令，或 用户名+Token 当口令）；或头 `X-JFrog-Art-Api: <token>`。
- **匿名访问（Q2 定案）：默认开启**（对齐 Artifactory 传统）——内容路径 `GET/HEAD /binflow/<repo>/<path>` 匿名可读；一切写操作（PUT/DELETE）与管理 API（`/binflow/api/**` 下除 ping/version 之外的端点）强制认证。配置项 `security.anonymous_access` 默认 `true`，可关闭。
- 授权单元：permission target = `{name, repos[], includePatterns[], excludePatterns[], principals{users{<name>:[actions]}, groups{<name>:[actions]}}}`，actions ∈ `read | write | delete`（`write` 含上传，不含删除；`admin` 用户隐式全权）。端点走 BinFlow 自有 `/binflow/api/v1/permissions`（理由见 §5.2 E-24）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-5-AC1（首次引导） | Q3 定案：服务以 `BINFLOW_ADMIN_PASSWORD=$ADMIN_PW` 启动后，`curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories` → 200；**未设置**该环境变量时使用缺省口令 `password`（`admin:password` 可用），文档必须标注缺省值**仅限评估环境**，生产必须显式设置 | P0 |
| FR-5-AC2（口令错误） | `curl -su admin:wrong -o /dev/null -w '%{http_code}' $BASE/binflow/api/repositories` → **401** | P0 |
| FR-5-AC3（改密） | C20（双路由任一）：① 自有路径 `PUT /binflow/api/security/password` body `{"oldPassword":"...","newPassword":"..."}` → 200；② 真实路径别名 `POST /binflow/api/security/users/authorization/changePassword` body `{"userName":"admin","oldPassword":"...","newPassword1":"...","newPassword2":"..."}` → 200 纯文本 `Password has been successfully changed`；**旧口令错误 → 400（非 401）**纯文本 `Incorrect username/password`；成功后旧口令随即 401、新口令 200（auth-model.md §2） | P1 |
| FR-5-AC4（发 Token） | C21a：`POST /binflow/api/security/token -d 'grant_type=client_credentials'`（**form**，Content-Type `application/x-www-form-urlencoded`；JSON body 作 BinFlow 扩展亦接受）→ 200，返回 `access_token` 非空、`token_type=="Bearer"`、`scope` 非空；BinFlow 超集扩展附 `token_id`（真实 Artifactory 创建响应无此字段，auth-model.md §3.1） | P0 |
| FR-5-AC5（Token 可用） | C21b：`curl -su admin:$TOKEN ...` 与 `curl -su admin:$ADMIN_PW -H "X-JFrog-Art-Api: $TOKEN" ...` 均 200 | P0 |
| FR-5-AC6（吊销） | C21c：`POST /binflow/api/security/token/revoke`（**form**）`-d "token=$TOKEN"` 或 `-d "token_id=$TID"`（两者 XOR）→ **200 纯文本 `Token revoked`**；随后用该 Token 的请求 **401**；重复吊销 → 仍 200 body `Token not found`；同传两者 → 400 `token and token_id are mutually exclusive`；都缺 → 400 `token or token_id is required`（auth-model.md §3.4） | P0 |
| FR-5-AC7（建用户） | C22a（双路由任一）：真实路径 `PUT /binflow/api/security/users/ci-bot` body `{"name":"ci-bot","email":"ci@example.com","password":"...","admin":false}` → **201 无 body**；或自有路径 `POST /binflow/api/security/users` 同 body → 201；`email`/`password` 空 → 400；建好后 `ci-bot` 对仓库任意路径 PUT → **403**（未授权写；匿名读开启时 GET 本就可读，见下方注）（auth-model.md §1.2/§1.3） | P0 |
| FR-5-AC8（授权路径写） | C22b：`POST /binflow/api/v1/permissions` body 授 `ci-bot` 在 repo `generic-local`、pattern `ci-out/**` 上 `read`+`write` → 2xx；随后 `ci-bot` PUT `generic-local/ci-out/y.bin` → 201；授 `read` 前PUT 同路径 → 403；`ci-bot` 访问管理 API（如 `GET /binflow/api/repositories`）→ 401/403（非 admin） | P0 |
| FR-5-AC9（授权路径删） | C22c：`ci-bot` DELETE `generic-local/ci-out/y.bin` → 403（未授 `delete`）；`admin` 对同路径 DELETE → 2xx | P1 |
| FR-5-AC10（权限对象管理） | `GET /binflow/api/v1/permissions` 列出已建对象；`DELETE /binflow/api/v1/permissions/{name}` 后 ci-bot 相关授权立即失效（403） | P1 |
| FR-5-AC11（口令不回显） | `GET /binflow/api/security/users` 的响应不含任何口令/哈希字段；列表元素为 `{"name","uri","realm"}`（auth-model.md §1.2），`GET /binflow/api/security/users/ci-bot` 单用户响应同样无口令字段 | P0 |
| FR-5-AC12（匿名读默认开） | Q2 定案，C23：默认配置下，不带任何认证 `GET /binflow/generic-local/acme/artifact.bin` → **200**（内容与 C08 一致）；不带认证 `PUT` 同仓库任意路径 → **401**；不带认证 `GET /binflow/api/repositories` → **401**（管理 API 不匿名） | P0 |
| FR-5-AC13（关闭匿名读） | C27：配置 `security.anonymous_access: false`（或环境变量 `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false`）重启后，不带认证内容 GET → **401** + `WWW-Authenticate` 头；带认证 GET → 200；且在该模式下 `ci-bot` 的 read 权限可被验证：GET `ci-out/x.bin` → 200，GET 仓库外路径 → 403 | P1 |

> 注：匿名读开启时（默认），内容 GET 对所有人开放，无法用「GET 403」区分未授权用户——read 权限的差异化验证统一放到 `anonymous_access: false` 场景（FR-5-AC13 后半段）。QA 剧本需覆盖两种模式。

### FR-6 开发环境与 README 快速开始（devops-engineer）

**用户故事**：作为新加入的贡献者/评估者，我希望 `docker compose up` 三条命令内拿到一个可用实例，README 的命令逐行复制即可跑通 M1 能力。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-6-AC1（compose 起服务） | 在 `deploy/`（或其 dev 子目录）执行 `docker compose up -d --build`，退出码 0；30s 内 `curl -s $BASE/binflow/api/system/ping` 返回 `OK` | P0 |
| FR-6-AC2（健康端点） | `curl -s $BASE/binflow/api/v1/health` → 200，JSON 含 `"status":"ok"`；`curl -sf` 退出码 0 | P0 |
| FR-6-AC3（持久化） | 上传一个文件后 `docker compose restart`，同路径 GET 仍 200 且 sha256 不变；`docker compose down && up -d`（不 `-v`）后仍 200 | P0 |
| FR-6-AC4（README 快速开始） | 仓库根 `README.md` 含「快速开始」小节：构建 → 启动 → 建仓 → 上传 → 下载 → 校验 五步命令（URL 均用 `/binflow` 前缀）；并按 NFR-S8 明示匿名读默认值与关闭方法；QA 在全新 clone + 空 Docker 环境逐行执行，全部退出码 0。归属（Q8 定案）：devops-engineer 起草，M5 由 tech-writer 收编 | P1 |
| FR-6-AC5（裸二进制启动） | 不用 Docker：`make build && ./bin/binflow-server`（默认参数，数据目录默认 `./data`，可用 `BINFLOW_HOME`/配置文件覆盖）→ ping 同样 `OK`；`Ctrl-C` 干净退出（退出码 0，无协程泄漏告警日志） | P0 |

---

## 5. 兼容性矩阵（本产品核心章节）

### 5.1 兼容层级定义（全项目统一，不留模糊地带）

> **v1.1 修订（Q1 定案）**：BinFlow 不使用 `/artifactory` 前缀，全部端点统一挂 `/binflow`。因此「兼容」指**前缀重写后的语义兼容**：老脚本把 base path `/artifactory` 替换为 `/binflow` 后，方法、参数、状态码、响应关键字段与 Artifactory 一致。

| 层级 | 含义 | 判定标准 |
|---|---|---|
| **兼容** | 路径 = `/binflow` + Artifactory 相应路径去掉 `/artifactory`；方法/参数/成功状态码/响应关键字段与 Artifactory 一致 | 真实客户端脚本**仅改 base path** 即可通过 |
| **兼容（子集）** | 同上，但字段/参数只实现高频子集；未实现字段不返回（不返回错误值） | 命令通过 + 响应为请求字段的超集/子集明示清单 |
| **语义等同但路径不同（/binflow/api/v1）** | 能力对应 Artifactory 某端点，但 BinFlow 用自有路径与更干净的 schema；不承诺原路径 | 仅 `/binflow/api/v1` 文档化的命令通过 |
| **有意不兼容** | 明确决定不做的行为，返回确定性错误（400/404）而非静默忽略或 5xx | 返回码与错误体符合本 PRD 规定 |

通用错误契约（E-01，全端点适用）：非 2xx 一律返回
`{"errors":[{"status":<code>,"message":"<人类可读>"}]}`，`Content-Type: application/json`。未实现端点返回 404 + E-01（message 含 `not implemented in BinFlow` 类字样）；命中 `/artifactory/**` 的请求返回 404 + E-01，message 提示「BinFlow 统一前缀为 /binflow」。

**错误体三分层（v1.3 补注，依据 auth-model.md §0）**：Artifactory 实际是三种错误格式并存，BinFlow 照此分层，E-01 只是制品/通用层的契约：
| 层 | 端点域 | 错误体格式 |
|---|---|---|
| 制品与通用 | 仓储/存储/系统端点（`/binflow/api/repositories|storage|system/**`、`/binflow/<repo>/**`） | E-01：`{"errors":[{status,message}]}` JSON |
| 用户/权限管理 | `/binflow/api/security/users|permissions/**`（含 changePassword） | **纯文本** body（`text/plain`），状态码即错误语义 |
| Token（OAuth 风格） | `/binflow/api/security/token/**` | `{"error":"<code>","error_description":"<msg>"}` JSON（如 `invalid_request`/`invalid_scope`/`unsupported_grant_type` 400、`invalid_grant` 401） |

### 5.2 M1 端点矩阵

「置信度」：高 = 逆向规格（反编译 + 官方文档双证）或用户定案；中 = PRD 暂定待后续规格校准（见 §5.5）。表中路径均为最终路径（已含 `/binflow` 前缀）。

| # | 端点（方法 路径） | Artifactory 行为要点 | 层级 | 优先级 | 置信度 | 验收命令 |
|---|---|---|---|---|---|---|
| E-02 | `GET /binflow/api/system/ping` | 200，body 纯文本 `OK`，无需认证 | 兼容 | P0 | 高 | C01 |
| E-03 | `GET /binflow/api/system/version` | 200 JSON：如实返回 BinFlow 自身 `version`（如 `1.0.0-m1`）+ `revision` + `product:"BinFlow"` 标识字段；**不伪装 Artifactory 版本号**（Q4 定案） | 兼容（子集） | P1 | 高 | C28b |
| E-04 | `GET /binflow/api/repositories` | 200 数组：`key/description/type/packageType/url`；需认证（Q2） | 兼容 | P0 | 高 | C05 |
| E-05 | `GET /binflow/api/repositories/{key}` | 200 仓库配置 JSON，`rclass/packageType` | 兼容（子集） | P0 | 高 | C06 |
| E-06 | `PUT /binflow/api/repositories/{key}` | body `{"rclass":"local","packageType":"generic",...}`；新建成功 → **200 纯文本** `Successfully created repository '<key>'`（rest-api.md §2，高置信度）；更新走 `POST`（200 `Repository <key> update successfully.`，BinFlow M1 亦接受 PUT 重复执行按更新处理）；`remote`/`virtual` → 400（M1 有意不支持） | 兼容（子集） | P0 | 高 | C03 / C04 / C26 |
| E-07 | `PUT /binflow/api/repositories/{key}` rclass=remote/virtual | Artifactory 支持；BinFlow M1 返回 400「supported from M3」 | 有意不兼容（阶段性） | P0 | — | C26 |
| E-08 | `DELETE /binflow/api/repositories/{key}` | 空仓 2xx；非空需 `?deleteContent=true` 否则 400 | 兼容 | P1 | 高 | C19 |
| E-09 | `GET /binflow/api/storage/{repo}/{path}` | item info：`children/checksums{sha1,md5,sha256}/size/created/createdBy/lastModified/modifiedBy/downloadUri/uri/mimeType`；匿名访问边界随 Q2 默认（内容元数据按匿名可读实现，管理 API 除外） | 兼容（子集） | P0 | 高 | C10 / C16 |
| E-10 | `GET /binflow/api/storage/{repo}?list&deep=&depth=` | `fileList[]`（M1 子集：uri+size） | 兼容（子集） | P2 | 高 | C17 |
| E-11 | `PUT /binflow/{repo}/{path}`（上传） | 201 + `Location` 头 + FileInfo 形态 JSON（含 `checksums` 与 `originalChecksums`）；`X-Checksum-Sha256` 预校验不一致 → **409**（repo-semantics.md §5 client-checksums 策略，message 含 received/actual）；`X-Checksum-Deploy: true` 空体秒传，未命中或格式非法 → **404**；需认证（Q2） | 兼容 | P0 | 高 | C07/C13/C14/C15 |
| E-12 | `GET /binflow/{repo}/{path}`（下载） | 200 流式；头 `X-Checksum-Sha256/X-Checksum-Sha1/X-Checksum-Md5`；匿名可读（Q2 默认开） | 兼容 | P0 | 高 | C08 / C23 |
| E-13 | `HEAD /binflow/{repo}/{path}` | 200，`Content-Length` + checksum 头；匿名可读（Q2） | 兼容 | P0 | 高 | C09 |
| E-14 | `DELETE /binflow/{repo}/{path}` | **204 无 body**（rest-api.md §1.1 / repo-semantics.md §4，高置信度）；目录递归删；重复删 404 幂等；需认证（Q2） | 兼容 | P0 | 高 | C18 |
| E-15 | `PUT /binflow/{repo}/{dir}/`（结尾斜杠建目录） | **201** + FolderInfo 形态（rest-api.md §1.1，高置信度） | 兼容 | P1 | 高 | C16 |
| E-16 | 改密（双路由）：`PUT /binflow/api/security/password`（BinFlow 自有，body `oldPassword/newPassword`）+ **别名** `POST /binflow/api/security/users/authorization/changePassword`（真实 7.x 路径，body `userName/oldPassword/newPassword1/newPassword2`） | 成功 200 纯文本（`Password has been successfully changed`）；**旧口令错误 → 400（非 401）**，纯文本 `Incorrect username/password`；`newPassword1≠2`/新旧相同/新口令为空 → 400（文案见 auth-model.md §2.2）；错误体走「用户管理纯文本」层（auth-model.md §2，高置信度） | 兼容（changePassword 路径）/ 语义等同但路径不同（password 路径） | P1 | 高 | C20 |
| E-17 | `POST /binflow/api/security/token` | 请求 **form-urlencoded**（真实端点只吃 form；BinFlow 同时接受 JSON 作扩展）；成功 200 JSON：`access_token`（必返）/ `token_type`（必返，固定 `"Bearer"`）/ `expires_in`（秒，**0=永不过期且该字段缺省**）/ `scope`（必返，空格串）/ `refresh_token`（仅 refreshable 时）；**创建响应无 `token_id`**，BinFlow 作**超集扩展**附加（便于按 id 吊销，无害）。参数：`grant_type`（缺省 `client_credentials`，未知值 400 `unsupported_grant_type`）/`scope`（malformed → 400 `invalid_scope`）/`expires_in`（负数 → 400 `invalid_request`）/`audience`（缺省本实例）/`refreshable`（true 须配有限 expires_in）。**M1 子集**：`username`/`expires_in`/`refreshable` 参数可不实现（admin 无参创建即可）；不做刷新流。错误体走「token OAuth」层（auth-model.md §3，高置信度） | 兼容（子集） | P0 | 高 | C21a |
| E-18 | `POST /binflow/api/security/token/revoke` | 请求 **form-urlencoded**，参数 `token` **XOR** `token_id`：同传 → 400 `token and token_id are mutually exclusive`；都缺 → 400 `token or token_id is required`；成功 → **200 纯文本 `Token revoked`**；目标不存在/已吊销 → **仍 200，body `Token not found`**（幂等不报错）；吊销后该 token 任何请求 → 401。admin only。错误体走「token OAuth」层（auth-model.md §3.4，高置信度） | 兼容 | P0 | 高 | C21c |
| E-19 | 建用户（双路由）：**`PUT /binflow/api/security/users/{name}`（真实路径，201 无 body）** + `POST /binflow/api/security/users`（BinFlow 自有，201）；`GET /binflow/api/security/users` → 200 数组，元素 `{"name","uri","realm"}`；`email`/`password` 空 → 400（真实校验链文案见 auth-model.md §1.3，BinFlow 自有 POST 路由执行同一条链：email blank → 400、password blank → 400、保留名 `_system_` → 400）；响应永不含口令；M1 不做组管理（M4） | 兼容（PUT 路由+GET 列表）/ 语义等同但路径不同（POST 路由） | P0 | 高 | C22a |
| E-20 | 认证头：Basic（口令或 Token 充当口令）、`X-JFrog-Art-Api` | 失败 401 + `WWW-Authenticate: Basic realm=...` + E-01；匿名读默认开（Q2），写与管理 API 恒需认证 | 兼容 | P0 | 高 | C02/C21b/C23 |
| E-21 | 错误体格式 E-01（所有非 2xx） | `{"errors":[{status,message}]}` | 兼容 | P0 | 高 | C12 |
| E-22 | `GET /binflow/api/v1/health` | BinFlow 自有：`{"status":"ok"}` + 各子系统状态 | /api/v1 | P0 | — | C28a |
| E-23 | `GET /binflow/api/v1/storage/stats` | BinFlow 自有（QA/运维用）：blob 数、逻辑字节数、物理字节数（验证去重） | /api/v1 | P1 | — | C12 |
| E-24 | `POST/GET/DELETE /binflow/api/v1/permissions` | 权限对象 CRUD。Artifactory 对应 `/api/v2/security/permissions/*`，BinFlow 不承诺该路径（M4 权限完整版再评估是否补 v2 路径） | 语义等同但路径不同 | P0 | — | C22b/c |
| E-25 | `X-Explode-Archive` 上传头 | Artifactory 解包归档；BinFlow M1 拒绝：400「not supported」 | 有意不兼容 | P1 | — | C25 |
| E-26 | 未挂载与未实现路径 | ① 根路径下一切非 `/binflow` 前缀请求（`/v2/**`、`/artifactory/**`、`/api/**`…）→ 404 + E-01（Q1 定案：无根镜像；`/artifactory/**` 的 message 提示 `/binflow` 前缀）；② `/binflow/api/` 下未实现端点（`search`、`replication`、`npm`、`pypi`、`system/info`…）→ 404 + E-01。**绝不**返回 500 或空 200 | 有意不兼容 | P0 | — | C24 |

> 计数：**26 条**。兼容/兼容（子集）**18** 条；`/binflow/api/v1` **3** 条；有意不兼容 **5** 条（E-07/E-25/E-26 + 删除码差异见 §5.5）。

### 5.3 M1 核心验收命令（QA 直接引用）

编号与 §4 AC、§5.2 矩阵互相引用。`$BASE/$ADMIN_PW` 见 §4 约定；`jq` 为 QA 环境标配。全部命令已按 Q1 定案使用 `/binflow` 前缀。

```bash
# 环境准备
export BASE=http://localhost:8080
export ADMIN_PW=password            # Q3 定案：启动时设 BINFLOW_ADMIN_PASSWORD 优先；未设置时缺省 password（仅限评估）

# C01 ping（E-02，匿名）
curl -s $BASE/binflow/api/system/ping               # 期望输出: OK

# C02 未认证访问管理 API（E-20；Q2 定案：管理 API 恒需认证）
curl -s -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/repositories   # 401

# C03 建仓（E-06）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic","description":"M1 QA"}' \
  -o /dev/null -w '%{http_code}\n'                   # 200（v1.2 定案：新建即 200 纯文本；重复执行同为 200）

# C04 非法 repo key（E-06）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/Bad_Key! \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic"}' \
  -o /dev/null -w '%{http_code}\n'                   # 400

# C05 列仓（E-04）
curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories | jq -r '.[].key' | grep -x generic-local   # 退出码 0

# C06 查仓（E-05）
curl -su admin:$ADMIN_PW $BASE/binflow/api/repositories/generic-local | jq -r '.rclass,.packageType' # local \n generic

# C07 上传 + 服务端 checksum 对账（E-11 / FR-2-AC2）
dd if=/dev/urandom of=artifact.bin bs=1m count=10
SHA=$(sha256sum artifact.bin | cut -d' ' -f1)
curl -su admin:$ADMIN_PW -T artifact.bin $BASE/binflow/generic-local/acme/artifact.bin \
  | jq -r '.checksums.sha256'                        # 输出 == $SHA（diff 断言）
SIZE=$(stat -f%z artifact.bin)                       # linux: stat -c%s
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/generic-local/acme/artifact.bin | jq '.size'   # == $SIZE

# C08 下载 + 校验（E-12）
curl -su admin:$ADMIN_PW -o dl.bin $BASE/binflow/generic-local/acme/artifact.bin
sha256sum artifact.bin dl.bin                        # 两行相同

# C09 HEAD（E-13）
curl -su admin:$ADMIN_PW -sI $BASE/binflow/generic-local/acme/artifact.bin | \
  awk -F': ' 'tolower($1)=="x-checksum-sha256"{gsub("\r","");print $2}'   # == $SHA

# C10 item info（E-09）
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/generic-local/acme/artifact.bin | jq -r '.checksums.sha256'  # == $SHA

# C12 去重观测（E-23 / FR-2-AC1）
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/storage/stats | jq '.blobs'  # 上传同内容到 b/y.bin 前后值不变

# C13 客户端 checksum 一致（E-11）
curl -su admin:$ADMIN_PW -T artifact.bin -H "X-Checksum-Sha256: $SHA" \
  $BASE/binflow/generic-local/acme/v2.bin -o /dev/null -w '%{http_code}\n'          # 201

# C14 客户端 checksum 不一致（E-11）
curl -su admin:$ADMIN_PW -T artifact.bin -H "X-Checksum-Sha256: $(printf '0%.0s' {1..64})" \
  $BASE/binflow/generic-local/acme/bad.bin -o /dev/null -w '%{http_code}\n'         # 409（v1.2 定案：client-checksums 策略，message 含 received/actual）

# C15 checksum deploy：命中 / 未命中（E-11）
curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $SHA" \
  $BASE/binflow/generic-local/acme/copy.bin -o /dev/null -w '%{http_code}\n'        # 201
curl -su admin:$ADMIN_PW -X PUT -H 'X-Checksum-Deploy: true' -H "X-Checksum-Sha256: $(printf 'f%.0s' {1..64})" \
  $BASE/binflow/generic-local/acme/miss.bin -o /dev/null -w '%{http_code}\n'        # 404（v1.2 定案：blob 不存在或格式非法均 404）

# C16 目录（E-15 / E-09）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/generic-local/acme/ -o /dev/null -w '%{http_code}\n'  # 201（v1.2 定案：MKDir 成功 201）
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/generic-local/acme | jq '.children | length'    # > 0

# C17 list（E-10，P2）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local?list&deep=1" | jq -r '.files[].uri' # 列出全路径

# C18 删除 + 幂等 404（E-14）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/generic-local/acme/artifact.bin -o /dev/null -w '%{http_code}\n'  # 204 无 body（v1.2 定案）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/generic-local/acme/artifact.bin -o /dev/null -w '%{http_code}\n'  # 404
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/acme/artifact.bin           # 404

# C19 删仓（E-08）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/repositories/generic-local -o /dev/null -w '%{http_code}\n'            # 400（非空）
curl -su admin:$ADMIN_PW -X DELETE "$BASE/binflow/api/repositories/generic-local?deleteContent=true" -o /dev/null -w '%{http_code}\n'  # 2xx

# C20 改密（E-16，双路由；v1.3：成功后导出新口令供后续命令使用）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/password \
  -H 'Content-Type: application/json' -d '{"oldPassword":"password","newPassword":"n3w!pw"}' -o /dev/null -w '%{http_code}\n' # 200
curl -su admin:n3w!pw -X POST $BASE/binflow/api/security/users/authorization/changePassword \
  -H 'Content-Type: application/json' \
  -d '{"userName":"admin","oldPassword":"n3w!pw","newPassword1":"n3w!pw2","newPassword2":"n3w!pw2"}' -o /dev/null -w '%{http_code}\n'  # 200（真实路径别名）
curl -su admin:n3w!pw -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/repositories    # 401（旧口令随即失效）
export ADMIN_PW=n3w!pw2                                                                 # 后续命令统一用新口令

# C21 Token 发放/使用/吊销（E-17/E-18/E-20；v1.3：form 形态）
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token \
  -d 'grant_type=client_credentials' | jq -r .access_token)          # form；响应必含 access_token/token_type=Bearer/scope（+BinFlow 扩展 token_id）
curl -su admin:$TOKEN -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/repositories   # 200
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token=$TOKEN" -o /dev/null -w '%{http_code}\n'                # 200 纯文本 "Token revoked"
curl -su admin:$TOKEN -o /dev/null -w '%{http_code}\n' $BASE/binflow/api/repositories   # 401
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token=$TOKEN"                                                  # 200 纯文本 "Token not found"（幂等）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke \
  -d "token=$TOKEN&token_id=x" -o /dev/null -w '%{http_code}\n'      # 400（token 与 token_id 互斥）

# C22 用户与路径 ACL（E-19/E-24；v1.3：双路由建用户 + email 必填）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/security/users/ci-bot -H 'Content-Type: application/json' \
  -d '{"name":"ci-bot","email":"ci@example.com","password":"ci-pw","admin":false}' -o /dev/null -w '%{http_code}\n'  # 201 无 body（真实路径）
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/users | jq -r '.[].name' | grep -x ci-bot   # 退出码 0（列表元素 {name,uri,realm}）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/users -H 'Content-Type: application/json' \
  -d '{"name":"ci-bot-2","password":"ci-pw2","admin":false}' -o /dev/null -w '%{http_code}\n'  # 400（email 缺失，自有路由同校验链）
curl -su ci-bot:ci-pw -T artifact.bin $BASE/binflow/generic-local/other/z.bin -o /dev/null -w '%{http_code}\n'   # 403（未授权写）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/permissions -H 'Content-Type: application/json' -d '{
  "name":"ci-out-rw","repos":["generic-local"],"includePatterns":["ci-out/**"],
  "principals":{"users":{"ci-bot":["read","write"]}}}' -o /dev/null -w '%{http_code}\n'    # 2xx
curl -su ci-bot:ci-pw -T artifact.bin $BASE/binflow/generic-local/ci-out/y.bin -o /dev/null -w '%{http_code}\n'  # 201
curl -su ci-bot:ci-pw -X DELETE $BASE/binflow/generic-local/ci-out/y.bin -o /dev/null -w '%{http_code}\n'        # 403（无 delete）
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/generic-local/ci-out/y.bin -o /dev/null -w '%{http_code}\n'     # 2xx（admin 全权）

# C23 匿名读默认开（Q2 / FR-5-AC12）
curl -s -o anon.bin -w '%{http_code}\n' $BASE/binflow/generic-local/acme/v2.bin     # 200（匿名 GET 内容）
sha256sum artifact.bin anon.bin                                                     # 两行相同
curl -s -X PUT -T artifact.bin -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/anon/x.bin  # 401（匿名写被拒）

# C24 未挂载与未实现端点（E-26）
curl -s $BASE/v2/ -o /dev/null -w '%{http_code}\n'                                  # 404（协议固定路径，M2 挂载）
curl -s $BASE/artifactory/api/system/ping -o /dev/null -w '%{http_code}\n'          # 404（message 提示 /binflow 前缀）
curl -s $BASE/binflow/api/npm/xx | jq -r '.errors[0].status'                        # 404
curl -s $BASE/api/system/info -o /dev/null -w '%{http_code}\n'                      # 404（无根镜像）

# C25 拒绝归档解包（E-25）
curl -su admin:$ADMIN_PW -T artifact.bin -H 'X-Explode-Archive: true' \
  $BASE/binflow/generic-local/acme/z.bin -o /dev/null -w '%{http_code}\n'  # 400

# C26 拒绝 remote/virtual（E-07）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-remote -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"docker","url":"https://registry-1.docker.io"}' -o /dev/null -w '%{http_code}\n'  # 400

# C27 关闭匿名读后（Q2 / FR-5-AC13）
# 以 security.anonymous_access=false（或 BINFLOW_SECURITY_ANONYMOUS_ACCESS=false）重启实例后：
curl -s -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/acme/v2.bin    # 401（+ WWW-Authenticate 头）
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/acme/v2.bin  # 200
curl -su ci-bot:ci-pw -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/ci-out/y.bin    # 200（有 read）
curl -su ci-bot:ci-pw -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/acme/v2.bin    # 403（无 read）

# C28 健康与版本
curl -sf $BASE/binflow/api/v1/health | jq -r .status               # ok
curl -su admin:$ADMIN_PW $BASE/binflow/api/system/version | jq -r '.version,.product'  # 非空版本号 + BinFlow（Q4）

# C29 重启持久化（FR-6-AC3）
docker compose restart && sleep 5
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/ci-out/y.bin  # 200

# C30 慢上传中断 + kill -9（FR-2-AC3/AC4）
timeout -s KILL 3 curl -su admin:$ADMIN_PW -T big.bin --limit-rate 64k \
  $BASE/binflow/generic-local/acme/big.bin; echo $?     # 137（被 kill），非 0
curl -su admin:$ADMIN_PW -o /dev/null -w '%{http_code}\n' $BASE/binflow/generic-local/acme/big.bin  # 404
```

### 5.4 后续里程碑协议边界（docker/mvn/npm/pip 如何「不影响 M1」）

M1 通过以下四条边界保证后续协议可以**追加**而非**返工**：

1. **路径命名空间（Q1 定案下的分配）**：BinFlow 自有与兼容端点统一挂 `/binflow`；内容路径 `/binflow/<repo>/<path>`。后续协议：
   - **M2 Docker Registry v2：根路径 `/v2/` 是协议固定路径**——docker/podman/crane 等客户端只认 `host[:port]/v2/`，不支持子路径前缀。这是 Q1「统一前缀」决策的**协议例外**：M2 设计时由部署层反代（或直接端口路由）把 `/v2/` 指到 BinFlow；对 M1 的影响仅为 `/v2/**` 返回 404（E-26）。
   - M3 npm/PyPI：`/binflow/api/npm/**`、`/binflow/api/pypi/**`、`/binflow/api/pypi-ui/**`；Maven 仓库内容直接走 `/binflow/<maven-repo>/<layout path>`（layout 解析按 repo 的 packageType 分发，M1 的 generic 直通逻辑必须做成按 packageType 可扩展的分发点——这是对 architect 的需求输入，不是实现指令）。
2. **repo 模型前向兼容**：`packageType` 字段 M1 接受 `generic`；M2+ 追加 `docker` 等值时**不得**改动 `rclass/packageType` 字段语义与既有 generic 行为；`rclass` 的 `remote/virtual` 值 M3 启用。
3. **认证前向兼容**：M1 的 Basic/Token 中间件必须可按路径前缀挂载（M2 的 `/v2/` 匿名可探——与 Q2 匿名读默认开一致、`/v2/` 内容操作需 Bearer，M3 各协议匿名读开关独立、默认值遵循 Q2 先例）；M1 不做 Bearer/token-endpoint 本体。
4. **明确不承诺**：M1 不包含任何 Docker manifest/blob 语义、Maven metadata 语义、npm tarball 语义、PyPI simple index 语义；QA 在 M1 只验证 E-26（这些路径 404）。

### 5.5 校准记录（v1.2 依据 rest-api/repo-semantics 收口 ①~④⑥；v1.3 依据 auth-model.md 收口 ⑤，六项全部定案）

v1.0/v1.1 的六项待校准项，依据 `docs/reverse/` 逆向规格定案如下；全部为高置信度（反编译 + 官方文档双证）。全文对应位置（FR-AC / E-xx / C 命令）已同步更新。

| # | 项 | v1.1 暂定值 | **定案**（v1.2 收口 ①~④⑥，v1.3 收口 ⑤） | 依据 |
|---|---|---|---|---|
| ① | DELETE 文件成功状态码 | 204（待校准） | **204 无 body**；重复删 404 幂等 | rest-api.md §1.1、repo-semantics.md §4（高） |
| ② | checksum deploy 未命中状态码 | 404（待校准） | **404**（blob 不存在或 checksum 格式非法均 404；两专用 checksum 头都缺 → 400） | rest-api.md §1.3（高） |
| ③ | X-Checksum 不一致状态码与文案 | 400（待校准） | **409**，message 含 received/actual 双值（`Checksum error for '<path>': received '<x>' but actual is '<y>'` 类文案；repo 默认策略 `client-checksums`）；若 repo 配置 `server-generated-checksums` 则容忍接受（M1 只实现默认策略） | repo-semantics.md §5（高） |
| ④ | mkdir（尾斜杠 PUT）行为 | 2xx 建目录（待校准） | **201** + FolderInfo 形态 JSON | rest-api.md §1.1（高） |
| ⑤ | token 响应字段名 | 按 Artifactory 公开文档（待校准） | **已定案（v1.3，auth-model.md §3.1 高置信度）**：真实响应字段集 = `access_token`（必返）/ `token_type`（必返 `"Bearer"`）/ `expires_in`（秒，0=永不过期时缺省）/ `scope`（必返）/ `refresh_token`（仅 refreshable）；**创建响应无 `token_id`**。BinFlow 按真实字段集返回 + `token_id` 作超集扩展；请求 form-urlencoded（JSON 为 BinFlow 扩展）。revoke 语义同步定案（form、token XOR token_id、200 `Token revoked`/`Token not found` 幂等）。详见 E-17/E-18 与 auth-model.md §5.1/§5.2 | auth-model.md §3（高，T-23 已落地） |
| ⑥ | item info 字段全集 | §FR-3-AC6 简表（待校准） | 按 rest-api.md §3 定案：含 `lastUpdated`；`size` 为**字符串**；含 `originalChecksums{sha1,md5,sha256}`；`path` 以 `/` 开头；目录 `children[]` 按名排序（FR-3-AC6 已更新） | rest-api.md §3（高） |

另两条增补规格一并纳入（不扩大 M1 范围，均为 P2）：ETag=sha1（无引号）+ 304/416 条件请求语义（FR-4-AC15）、同 checksum 幂等重传免覆盖权限检查（§4 FR-4 注，FR-5 实现必须保留该分支）。

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
| NFR-S8 匿名读暴露面（Q2 定案的一致性约束） | 匿名读**默认开启**是明确决策（对齐 Artifactory 传统）。README 快速开始与配置参考必须：① 明示该默认值；② 给出关闭方法（`security.anonymous_access: false` / `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false`）；③ 提示非本机/非可信内网部署应关闭。FR-5-AC13 验证关闭后行为 | QA 检查 README 含 `anonymous_access`；C23/C27 行为验证 |

### 6.3 可观测性底线

- 每请求一行结构化日志（JSON 或 key=value）：`time, level, method, path, status, duration_ms, remote_addr, user（认证后；匿名请求记 anonymous）, bytes_in/out`；**绝不**记录认证头与凭据。
- `/binflow/api/v1/health`（E-22）：`status` + 至少含 `storage`（数据目录可写）与 `metadata`（SQLite 可用）两个子系统状态。
- 日志级别可由配置/环境变量调整（`debug/info/warn/error`），默认 `info`。
- Prometheus 指标、审计日志、请求审计表均为 M4/M6+，M1 不做（§2.2）。

---

## 7. M1 验收剧本（QA roundtrip + 存储完整性总纲）

qa-engineer 按顺序执行，产出 `reports/agents/T-<qa票>-qa.md`，全绿 = 里程碑 DoD 第 2 条满足：

1. **工程基线**：FR-1-AC1..AC3、AC7（make build/test/lint + 零 CGO 构建）。
2. **冷启动**：FR-6-AC5 裸二进制 + FR-6-AC1 compose 双路径，NFR-P1 计时。
3. **仓库生命周期**：C03 → C04 → C05 → C06 → C19（建/校验/列/查/删）。
4. **制品 roundtrip**：C07 → C08 → C09 → C10 → C13 → C14 → C15 → C18（含服务端 checksum 对账、客户端校验、秒传、删除幂等）。
5. **存储完整性**：C12 去重前后对比 + FR-2-AC3 慢上传中断 + FR-2-AC4 kill -9 重启 + FR-2-AC6 1GB 流式（NFR-P3 采样）。
6. **认证与 ACL**：C02 → C20 → C21 → C22 → C23（匿名读默认开）→ C27（关闭匿名读，含 read 权限差异化验证）（含 NFR-S1/S2/S3/S8 抽查）。
7. **边界拒绝**：C24 → C25 → C26 → FR-4-AC10/AC11（路径安全与非法路径；C24 同时验证 `/artifactory/**` 与根路径 404 + 前缀提示）。
8. **压力**：NFR-P2 100 并发。
9. **文档**：FR-6-AC4 README 快速开始全新环境逐行复跑。
10. **持久化**：C29（compose restart / down+up 两轮）。

---

## 8. M1 DoD（对齐 ROADMAP「里程碑完成定义」）

1. §4 全部 P0/P1 AC 通过 qa-engineer 验证并附命令输出证据（P2 延后须在 BOARD 记录）；
2. §7 剧本全绿；
3. 逆向规格 5 份（rest-api / storage-layout / config-formats / repo-semantics / auth-model）已落地，§5.5 六项校准已全部回写本 PRD（v1.2 + v1.3 完成，无遗留）；
4. README 快速开始可复跑；
5. 主会话完成 `m1-done` tag。

---

## 9. 已决决策（原「开放问题」，2026-08-17 用户全部定案）

| # | 问题 | 决策（v1.1 落定） |
|---|---|---|
| Q1 | 兼容端点挂 `/artifactory` 前缀、根镜像还是统一自有前缀？ | **统一 `/binflow` 前缀**（推翻 v1.0 暂行假设）：兼容端点 `/binflow/api/...`、内容路径 `/binflow/<repo>/<path>`、自有端点 `/binflow/api/v1/...`；不用 `/artifactory`，不做根路径镜像。`/artifactory/**` 访问 404 并在 message 提示新前缀；迁移脚本需把 base path `/artifactory` 改为 `/binflow`（§3 场景 D）。协议固定路径（Docker `/v2/`）为例外，见 §5.4 |
| Q2 | 匿名读默认值：Artifactory 传统「开」还是安全默认「关」？ | **默认开**（推翻 v1.0 暂行假设）：内容路径 GET/HEAD 匿名可读；一切写操作与管理 API 强制认证。`security.anonymous_access` 默认 `true`，可关（FR-5-AC12/AC13、C23/C27、NFR-S8） |
| Q3 | admin 初始口令引导方式？ | 环境变量 `BINFLOW_ADMIN_PASSWORD` 优先，缺省 `password`（维持暂行假设，转为定案）；文档标注缺省值**仅限评估环境**，生产必须显式设置（FR-5-AC1） |
| Q4 | `/api/system/version` 如实返回还是伪装 Artifactory 版本？ | **如实返回 BinFlow 自身版本**（如 `1.0.0-m1`），另附 `product:"BinFlow"` 标识字段，不伪装 Artifactory 版本号（E-03） |
| Q5 | 元数据库实现与 Postgres 时机？ | **纯 Go SQLite**（`modernc.org/sqlite`，零 CGO，保 M5 交叉编译矩阵；此前讨论的 Derby/H2 方案否决）。FR-1-AC7（`CGO_ENABLED=0` 构建可过）+ FR-3 技术注记；FR-3-AC10（检测到 Postgres 配置报错退出）维持，Postgres 启用里程碑另议 |
| Q6 | Range/断点续传是否提前到 M1 P1？ | **维持 P2**，不提前；M2 Docker blob 拉取开工前必须补齐（FR-4-AC14） |
| Q7 | Go module 路径 `<org>`？ | **`github.com/lzwzzy/binflow`**（用户定值，FR-1-AC5） |
| Q8 | 根 README.md 唯一写入者？ | **devops-engineer 起草**（M1），tech-writer 于 **M5 收编**（FR-6-AC4） |

---

*本 PRD v1.1 由 product-manager 依据 PRODUCT.md、ROADMAP.md M1 条目与用户对 8 项开放问题的定案修订；与 `docs/reverse/` 规格冲突时按 §5.5 流程回写修订。*
