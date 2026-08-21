# PRD — M5 发布矩阵与文档中心（GA：多平台发布 / 部署矩阵 / Docusaurus 文档中心 / 安全审计 / 性能基准 / M4 债务收编）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/milestone-5.md` |
| 里程碑 | M5 — 发布矩阵与文档中心（GA，对应 ROADMAP.md「M5」节全部条目 + §2.1 收编的 M4 债务） |
| 状态 | **v1.0**（T-125 初版：FR-34~FR-47、端点矩阵 PB/DC/DM 三域 13 条、G01~G35 验收命令（CLI/curl + 各部署形态烟测剧本）、回归基线反转表 7 行、M4 债务归置 10+12 条（入 M5 八 / M6+ 二，余见 §6.4）、四项开放问题附暂行（发布渠道 / HPA 呈现 / 烟测环境可得性 / GA 版本号）） |
| 上游依据 | PRODUCT.md（核心能力 7 多元部署 + 成功标准四条：15 分钟跑通 / <40MB / 1000 并发 / 真实客户端）、ROADMAP.md M5 节与 DoD、M1 交付基线（milestone-1.md v1.3.1 @m1-done，现行 v1.3.2）、M2 交付基线（milestone-2.md v1.3 @m2-done，现行 v1.4）、M3 交付基线（milestone-3.md v1.2 @m3-done，现行 v1.3）、M4 交付基线（milestone-4.md v1.3 @m4-done）、ADR-0004（部署矩阵六产物 + 离线包）、ADR-0005（零 CGO / 依赖准入）、ADR-0010（/v2 根级例外——ingress 直通断言依据）、ADR-0011（Docusaurus：embed 主交付 / docs-site 聚合 / 匿名可读 / 5~15MB 预算与 fallback）、ADR-0016（目录实体化不变量 + T-119 实现草案两票）、architecture.md §9（部署架构约定：PVC / 健康检查 / 单副本拦截）与 §11 技术债台账（含 20 remote 不材料化）、docs/reverse/rest-api.md §3（?list / uri 族，高置信度）、reports/agents/T-119.md（实现草案）、T-106-qa.md（部署烟测先例与**不可达形态降级口径** §2.5）、T-103/T-104 QA（D-1 / D-104-2）、T-121（勘误台账与遗留移交）、BOARD.md done 区各票遗留登记 |
| 下游消费者 | tech-lead（拆票）、architect（ADR-0011 增补终裁 K1 / 镜像供应链 K2）、release-engineer（goreleaser / 镜像 / compose / Helm / K8s / systemd / 离线包）、security-auditor（FR-42）、dev-go-core（FR-44 BE / FR-45 / FR-47）、dev-frontend（FR-44 FE / FR-46）、devops-engineer（docs-site 脚手架 / CI）、tech-writer（FR-41 五类内容）、qa-engineer（G 序列 + 四里程碑回归 + GA 总矩阵） |

---

## 0. 修订记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-08-21 | 初版（T-125）：M5 范围、FR-34~FR-47（发布六面 + 文档中心 + 安全审计 + 性能基准 + M4 债务收编四面）、端点矩阵 PB/DC/DM 13 条、G01~G35 验收命令、四项开放问题附暂行（Q1 发布渠道 / Q2 HPA 呈现 / Q3 烟测环境可得性 / Q4 GA 版本号）、M4 债务归置定案（入 M5 8 / M6+ 2，另收编 / 关闭 12 项见 §6.4 速裁表）、Docusaurus 构建形态暂行待 architect 终裁（K1） |

---

## 1. 背景与目标

### 1.1 背景

M1~M4 交付了一个**功能完整的制品仓库**：五协议、三仓型、控制台、治理四件、备份恢复——但 BinFlow 至今是「仓库里的源码项目」而非「可获取的产品」：二进制只有本机构建形态（`--version` 仍为 `dev`，T-16/T-45/T-106 三处登记的版本注入欠账），部署矩阵六形态里四种产物不存在（T-106 §2.5 不可达矩阵：goreleaser dist / Helm Chart / 原生 K8s 清单 / systemd / 离线包），文档是 GitHub 目录里的 11 篇 Markdown 而非可搜索的站点。GA 的定义就是把「能跑」变成「能取、能装、能查、能审计」。

M5 同时是**行为面冻结前的最后一个里程碑**：两处对齐 Artifactory 参考行为的缺口（隐式目录 404、token 审计词表不实）在 GA 前修正的成本远低于 GA 后（GA 后修即 breaking）——这是 M4 债务中「产品完整性面」四项入 M5 的根本理由（§6.4）。

M5 的用户价值排序（对齐 PRODUCT「离线与受限网络 / 平台工程」双场景）：

1. **发布矩阵**（goreleaser 六平台 + 镜像双变体 + 五部署产物 + 离线包）——PRODUCT 成功标准「部署矩阵每种方式按文档 15 分钟内从零跑通」的兑现面；
2. **文档中心**（Docusaurus 五类，随二进制 embed）——air-gapped 场景「出问题当场可查」的差异化（ADR-0011 已定 embed 主交付）；
3. **安全审计 + 性能基准**——GA 的合格证：1000 并发拉取、<40MB、<2s 冷启动、零 Critical/High；
4. **M4 债务收编（产品完整性面）**——目录实体化（ADR-0016）、token 审计、docker 树数据源与特化视图、storage uri 基址族：GA 前最后一次行为面修正窗口。

### 1.2 M5 目标（GA）

> 一句话：交付可获取的产品——六平台二进制、双变体 multi-arch 镜像、五部署产物与离线包、随二进制走的中文文档中心、安全审计与性能基准报告，并在行为面冻结前修正四处 Artifactory 对齐缺口。

量化门槛（未达即里程碑不完成）：

| 指标 | M5 门槛 | 来源 |
|---|---|---|
| 发布矩阵 | 六平台二进制 + checksums 全产；镜像双变体 ×2 架构 manifest list；compose / Helm / K8s / systemd / 离线包五产物 lint + 烟测通过 | ROADMAP M5 前三条 / ADR-0004 |
| 15 分钟标准 | 每种部署形态按文档从零到 `/readyz` ≤ 15 分钟（G10 记录口径） | PRODUCT 成功标准 |
| 文档中心 | `/binflow/docs/` 匿名可用、离线（零外网）可搜索、五类内容齐备、中文 | ROADMAP M5 第四条 / ADR-0011 |
| 安全 | govulncheck / trivy / 密钥扫描零 Critical/High（或豁免清单经用户确认） | ROADMAP M5 第五条 |
| 性能 | **1000 并发拉取零错误**（PRODUCT 成功标准首次全量兑现）；冷启动 < 2s；空载 RSS < 100MB；单二进制压缩产物 < 40MB（六平台） | PRODUCT 成功标准 / NFR-P22 |
| 既有零回归 | M1 C / M2 D / M3 M / M4 W 四序列 P0 复跑全绿（断言按 §5.6 反转表更新） | M2 FR-7-AC2 先例 + GA 硬门槛 |
| 债务收编 | §6.4 定案入 M5 的 8 项全部完成（P2 项可延后至 m5-done 前收口） | 本 PRD §6.4 |

### 1.3 上游依赖与并行关系

- **架构依赖（architect）**：ADR-0011 增补（Docusaurus 构建形态终裁——搜索方案 / 资产 self-host 细节 / fallback 触发线，K1）；镜像供应链与 tag 策略（K2，随 Q1 渠道定案联动）；ADR-0016 已定案（实现票可直接派）。**M5 无新迁移**（007 回填归 FR-44 实现票，双方言）。
- **脚手架先行（devops-engineer）**：`docs-site/` Docusaurus 脚手架票必须在文档矩阵票前（ADR-0011 后果条款明文：类似 T-7 先行——node 依赖 / build 链 / embed 复制 / `/binflow/docs` 路由 / check-size 实测）。
- **发布角色（release-engineer）**：FR-34~FR-40 主体；烟测沿 T-106 先例（含**不可达形态降级口径**——本 PRD §7 Q3 定案化）。
- **QA**：G 序列 + 四里程碑 P0 回归 + GA 总矩阵（FR-43-AC4）；windows / systemd 条件腿按 Q3。
- ** BOARD 只读**：本 PRD 与 ROADMAP M5 节为需求面；票据拆解归 tech-lead。

---

## 2. 范围

### 2.1 In scope（与 ROADMAP M5 条目一一对应 + 债务收编）

| # | ROADMAP 条目 | 本 PRD 功能需求 |
|---|---|---|
| 1 | goreleaser 多平台二进制 + 校验和 | FR-34（六平台 / 版本注入 / check-size） |
| 2 | Docker multi-arch 镜像（distroless / alpine 双变体） | FR-35 |
| 3 | docker-compose 产物、Helm Chart（persistence/ingress/HPA）、原生 K8s 清单、systemd + 安装脚本、离线安装包 | FR-36 + FR-37 + FR-38 + FR-39 + FR-40 |
| 4 | 帮助文档中心（五类） | FR-41（Docusaurus / embed / 离线可用；含 O-106-2 与 N6 两项文档债务收编） |
| 5 | 安全审计 + 性能基准 | FR-42 + FR-43 |
| 6 | （收编，ROADMAP 本次增补一行）M4 债务：产品完整性面 | FR-44（ADR-0016 BE+FE）+ FR-45（token 审计）+ FR-46（docker 树数据源 + 特化视图）+ FR-47（storage uri 基址族，P2）+ FR-34-AC5（windows 锁运行时验证，条件腿） |
| 7 | （收编）§7.1 两行补遗 / 文档类债务 | FR-41-AC6 文档矩阵 + architect 增量票顺带（§6.4 #6） |

（逆向规格无 M5 新票——发布面无 Artifactory 对应；DM-01/DM-02 依据既有 rest-api.md §3 高置信度与 ADR-0016 取证。）

### 2.2 Non-goals — M5 明确不做（防范围蔓延）

**产品级 Non-goals（继承 PRODUCT.md，全程有效）**：不做 HA/联邦、不做 Xray、不做 LDAP/SAML/OIDC、不做 Artifactory 全量 REST 兼容、不做 UI 高级分析。

**M5 里程碑级 Non-goals**：

| 不做项 | 归属 | M5 的隔离边界 |
|---|---|---|
| HA / 多副本（任何形态） | M6+ | Helm values.schema 拦截 `replicaCount>1`（FR-37-AC2）；HPA 模板默认 disabled 且启用时强制 `maxReplicas=1`（§7 Q2 暂行）；真水平扩展依赖对象存储（M6+ S3 后端） |
| S3 / 对象存储后端 | M6+ | 部署产物只支持 PVC / 本地卷；文档明示单副本 + RWO 约束 |
| Prometheus 指标 / OpenTelemetry | M6+ | 可观测性维持结构化日志 + 既有健康端点（§6.3）；性能基准走外部压测工具，不加指标面 |
| 英文文档（i18n en locale） | M6+ | Docusaurus i18n 骨架就位但只交付 zh（用户既定「中文文档」）；en 为后续 locale 增量 |
| 包管理器分发（brew/scoop/deb/rpm/Chocolatey） | M6+ | 只交付 tar.gz / zip + checksums 与离线包 |
| `bf` CLI、Artifactory 迁移工具 | M6+ | 维持 M4 §2.2 裁定 |
| docker blob GC 候选（无 manifest 引用的 blob node 纳入回收） | M6+ | §6.4 #5：E3 已按每写原子接受（P2），GC mark 语义（docker_refs 感知补集）不在 GA 前改动 |
| CLI `gc` 双遍 mark 性能优化 | M6+ | §6.4 #10：性能基准（FR-43）记录 GC 吞吐基线供 M6+ 决策，本里程碑零优化 |
| immutable tag / tag retention / maxUniqueSnapshots | M6+ | 维持 M4 §2.2 收窄裁定（「M5+ 评估」至此落为 M6+：保留策略需调度框架支撑） |
| docker 类 remote 仓（Docker Hub pull-through） | M6+ | 维持 400「not supported」；T-111 remote docker FetchError 500 随之捆绑 M6+（§6.4 速裁） |
| 审计 CSV 导出、全局/用户级配额、不停机 import | M6+ | 维持 M4 §2.2 |
| 文档评论 / 反馈组件 / 站点统计 | 不排期 | docs 站静态只读，零第三方脚本（NFR-S29 离线断言的前提） |
| Artifactory 官方 Helm Chart 兼容 / values 兼容 | 不排期 | charts/binflow 为自有形态，不承诺其 chart 接口兼容 |

---

## 3. 用户与场景（M5 / GA 视角）

- **场景 A（15 分钟评估）**：平台工程师在全新笔记本上按文档任选一种形态（compose / Helm / systemd / 离线包），15 分钟内从零得到一个能 `docker push` 的 BinFlow——「按文档跑通」本身就是验收。
- **场景 B（air-gapped 安装）**：受限机房运维拿到 `binflow-offline_<ver>.tar.gz`：内含镜像、Chart、二进制、校验和与安装脚本——全程零外网；浏览器打开 `/binflow/docs/` 就地查安装与排查文档（断网可搜索）。
- **场景 C（异构集群）**：arm 集群上的 SRE `docker pull binflow:<ver>` 直接得到 arm64 镜像（manifest list 自动选择）；安全基线严的环境改用 distroless 变体（无 shell）。
- **场景 D（安全合规评审）**：安全团队索要 GA 材质证明：依赖漏洞扫描、镜像扫描、密钥扫描、权限模型复核报告 + 发布物 checksums 清单——逐项可复跑。
- **场景 E（性能选型）**：容量规划师拿基准报告做决策：1000 并发拉取曲线、冷启动与内存、export/import 吞吐、GC 吞吐基线。
- **场景 F（迁移收尾体验）**：从 Artifactory 迁移的团队在 GA 前发现的最后两处「心智不一致」被修正：GET 一个从未 mkdir 的目录得到 200 FolderInfo（不是 404）；token 签发/吊销在审计页可查。

---

## 4. 功能需求

约定：`BASE=http://localhost:8080`、`admin`/`$ADMIN_PW` 沿用；`VER=<GA 版本号>`（§7 Q4 暂行 v1.0.0）；`DIST=dist/`（goreleaser 产物目录）。优先级 P0/P1/P2 沿用 M1 定义。G 序列命令见 §5.4。

### FR-34 goreleaser 多平台二进制与版本注入（release-engineer / devops-engineer）

**用户故事**：作为平台工程师，我在一台 mac 上执行一条 make 目标，拿到 linux/darwin/windows × amd64/arm64 六个压缩包与 checksums 文件——`--version` 告诉我每个包跑的是什么版本，而不是 `dev`。

行为规格：

- **工具与平台**：goreleaser（构建侧工具，**不入 Go 运行时依赖**——ADR-0005 依赖准入：构建链工具与运行时依赖隔离，NFR-S31）；六平台 `linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64`；全平台 `CGO_ENABLED=0` 维持。
- **产物形态**：`binflow_<VER>_<os>_<arch>.tar.gz`（windows 用 `.zip`）内含 `binflow-server`（windows 为 `binflow-server.exe`）+ LICENSE + README 摘要；`binflow_<VER>_checksums.txt`（sha256，goreleaser 校验和产物）。单二进制含 console + docs 两套 embed（ADR-0002 / ADR-0011）。
- **版本注入**：ldflags 注入 `version` 与 `revision`（git short sha）；`binflow-server --version` 输出 `binflow-server <VER> (<revision>)`；启动结构化日志首行与 `GET /binflow/api/v1/health` 的 `version` 字段同值（health 只增字段原则，§6.3）。裸 `go build` 回退 `dev (dev)`（现状语义）。
- **check-size（PRODUCT 预算）**：每平台**压缩产物 ≤ 40MB**；docs embed 增量单独记录（预算 ≤ 15MB，gzip 口径，ADR-0011）。超限处置：docs 走 ADR-0011 fallback（独立 tar 附带、二进制不含 docs 资产重建）——触发即属 §7 Q1 联动的用户确认事项，不静默降级。
- **可跑性矩阵**：darwin（本机 arch）+ linux/amd64（容器）为 P0 真跑腿；windows/amd64 为条件腿（§7 Q3，含锁运行时验证 AC5）；linux/arm64（qemu 容器）P1 抽查；darwin/amd64 与 windows/arm64 产物存在 + checksums 过 + 版本字符串抽查（`strings | grep`）。
- **Makefile 目标**：`release-snapshot`（`goreleaser release --snapshot --clean`，本地免发布全产）与 `release`（真发布动作，须 Q1 定案 + 用户确认后启用）；CI 增发布流水线 job（dry-run 校验产物存在性）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-34-AC1 | G01：`make release-snapshot` 退出码 0；`ls dist/` 六平台产物 + checksums 齐全；`shasum -a 256 -c binflow_<VER>_checksums.txt`（在 dist 内逐产物）全 OK | P0 |
| FR-34-AC2 | G02：darwin 与 linux/amd64 二进制 `--version` 输出 `<VER>` 非 `dev`；`/api/v1/health` 与启动日志 version 同值；不可跑平台抽一产物 `strings <binary> \| grep <VER>` 命中 | P0 |
| FR-34-AC3 | G03：六平台压缩产物逐一 ≤ 40MB；记录 docs embed 前后增量（MB）进 QA 报告；超限走 fallback 须用户确认记录 | P0 |
| FR-34-AC4 | G04：darwin + linux/amd64 `serve` 起服 `/readyz` 200、冷启动 < 2s（W37 口径）、`/binflow/ui/` 与 `/binflow/docs/` 200 | P0 |
| FR-34-AC5 | G05（条件腿，Q3）：windows/amd64 `serve` 起服 + **锁运行时验证**——serve 运行中执行 `export` 成功、并发第二个 `export`/`gc` 退出码非 0（LockFileEx 互斥生效，T-96 遗留收口）；环境不可达按 §7 Q3 降级口径记录并经用户确认 | P1 |
| FR-34-AC6 | G04b：`docker run --platform linux/arm64`（qemu）起 dist 内 arm64 二进制 `/readyz` 200 | P1 |
| FR-34-AC7 | 回归：bare binary 形态五协议烟测 + console/session 链复跑绿（T-106 形态 A 等价口径） | P0 |

### FR-35 Docker multi-arch 镜像双变体（release-engineer）

**用户故事**：作为 arm 集群上的运维，我 `docker pull binflow:<VER>` 直接能跑——不用关心 CPU 架构；安全团队要求最小攻击面时，我改用 distroless 变体（里面连 shell 都没有）。

行为规格：

- **双变体**：`binflow:<VER>-alpine`（alpine 基底，含 shell——调试 / 排障 / `docker exec` 运维友好）与 `binflow:<VER>-distroless`（`gcr.io/distroless/static-debian12:nonroot` 基底——零 shell、零包管理器）；浮动 tag `binflow:<VER>` 指向 distroless（默认推荐最小面，K2 暂行）。
- **multi-arch**：两变体均为 `linux/amd64 + linux/arm64` manifest list（buildx）；构建沿 T-106 先例——镜像内 node 阶段自建 console 与 docs 站（不依赖构建机本地状态，资产指纹与 `make console`/`make docs` 一致）。
- **镜像契约（architecture §9 维持）**：`EXPOSE 8080`、`USER` 非 root、`HEALTHCHECK /readyz`、data 默认 `/var/lib/binflow`、`CGO_ENABLED=0`。dev 形态镜像（deploy/dev，T-17/T-106 产物）继续维护，GA 镜像构建链独立（deploy/release 或同等目录，release-engineer 定布局——K3）。
- 镜像扫描面归 FR-42-AC2；推送公共 registry 归 §7 Q1（未定案前一律本地 tag）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-35-AC1 | G06：buildx `--platform linux/amd64,linux/arm64` 构建两变体退出码 0；`docker buildx imagetools inspect` 两变体 manifest list 均含两平台 | P0 |
| FR-35-AC2 | G07：两变体（amd64 本机）起容器 `/readyz` 200 且 HEALTHCHECK 转 healthy；alpine 腿 `docker exec <c> id -u` 非 0；distroless 腿 `docker run --rm --entrypoint sh <img>` **失败**（无 shell——最小面断言） | P0 |
| FR-35-AC3 | G08：容器内 `/binflow/ui/` 200 + `/binflow/docs/` 200；console 资产指纹与宿主 `make console` 产物一致（T-106 §2.2 手法） | P0 |
| FR-35-AC4 | G07b：`docker run --platform linux/arm64`（qemu）distroless 变体 `/readyz` 200 | P1 |
| FR-35-AC5 | 两变体压缩尺寸记录进 QA 报告（记录项：alpine ≤ 60MB / distroless ≤ 40MB 参考线，超限归因不阻塞） | P1 |

### FR-36 docker-compose GA 产物（release-engineer）

**用户故事**：作为评估者，我 clone 仓库按 README 找到 GA compose，15 分钟内 `docker compose up -d` 得到一个生产姿态（持久卷 / 自启 / 健康检查 / 资源上限样例）的 BinFlow。

行为规格：

- `deploy/compose/`（GA 形态）：具名卷、`restart: unless-stopped`、healthcheck、`:?` 强制口令沿用、资源 limits 注释样例、可选 `nginx` 反代 profile（D-106-2 修复后片段：`location = /binflow` 直通 + `/v2/` 不 rewrite）；`.env.example` 完整。dev compose（`deploy/dev/`）维持不动。
- 「按文档 15 分钟」是 PRODUCT 成功标准：文档计时口径 = clean clone → `docker compose up -d` → `/readyz` 200（镜像构建时间计入——镜像自建 console/docs 是文档路径的一部分；预拉缓存不计）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-36-AC1 | G09：`docker compose -f deploy/compose/docker-compose.yml up -d --build` → healthy → 五协议烟测（dind docker login/push/pull + generic/maven/npm/pypi 各一腿）→ `compose restart` → 制品 GET 200 + 旧 session whoami 200 → `down -v` 卷 / 网络零残留（T-106 §2.2 口径复跑） | P0 |
| FR-36-AC2 | G10：clean clone + 计时从零到 `/readyz` ≤ 15 分钟（记录项；超时归因写进报告，文档问题转 tech-writer） | P1 |
| FR-36-AC3 | `docker compose -f ... config -q` 通过；`.env.example` 与 compose 引用变量一一对应（无缺注） | P0 |
| FR-36-AC4 | 反代 profile（P1）：`--profile nginx` 起栈 → `/binflow` 301 相对路径 + `/v2/` 401 挑战直通（T-106 §2.3 断言复跑） | P1 |

### FR-37 Helm Chart（release-engineer）

**用户故事**：作为 SRE，我 `helm install binflow charts/binflow -f my-values.yaml`——PVC 持久化、ingress 可选、values 校验在我犯多副本错误时直接拒绝安装，而不是给我一个坏掉的双副本集群。

行为规格：

- `charts/binflow/`：Deployment（replicaCount=1）/ PVC(RWO) / Service / Ingress（`ingress.enabled` 可选）/ ConfigMap（YAML 配置）+ Secret（口令——`BINFLOW_ADMIN_PASSWORD` 不入 ConfigMap）/ Probes（liveness=/healthz, readiness=/readyz）/ resources 样例 / `values.schema.json` / NOTES.txt（起服后取口令与端口提示）。
- **单副本拦截（architecture §9 硬约束）**：`values.schema.json` 拒绝 `replicaCount > 1`（maximum: 1，错误信息含单副本约束与 HA=M6+ 说明）。
- **HPA 模板（§7 Q2 暂行）**：`hpa.enabled` 默认 `false`；启用时 schema 强制 `maxReplicas <= 1`（多副本水平扩展依赖 M6+ 对象存储，单 PVC RWO 下多副本是数据损坏配置而非扩展）。
- **ingress 与 /v2（ADR-0010 联动）**：ingress 模板必须在同一 host rule 下直通 `/` 与 `/v2/`（**不 rewrite**）；文档注明经 HTTPS 反代后 docker 客户端无需 insecure-registries 配置。
- Chart 版本随 `<VER>`；`helm-docs` 或 README values 表齐全（P1）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-37-AC1 | G11：`helm lint charts/binflow` 0 错误；`helm template` 渲染产物含上述对象；HPA 默认不渲染；NOTES.txt 渲染非空 | P0 |
| FR-37-AC2 | G12：`helm install --set replicaCount=2` → 安装**失败**（schema 校验，错误信息含单副本说明）；`--set hpa.enabled=true,hpa.maxReplicas=2` 同样失败 | P0 |
| FR-37-AC3 | G13：kind（或 Docker Desktop K8s，Q3）`helm install` → Pod Ready → `kubectl port-forward` `/readyz` 200 + docker 经 NodePort/dind 一腿 push/pull + generic PUT/GET → `kubectl delete pod` 重建后制品 GET 200（PVC 持久化） | P0 |
| FR-37-AC4 | G13b（P1）：`ingress.enabled=true` 渲染断言——同 host 下 `/` 与 `/v2/` 路径均直通、无 rewrite 注解；可达环境（kind+hostPort traefik）curl `/v2/` 401 挑战 | P1 |
| FR-37-AC5 | `helm upgrade` 回归（P1，记录项）：同 chart 升一个 patch 版 → Pod 重建后制品存活 | P1 |

### FR-38 原生 K8s 清单（release-engineer）

**用户故事**：作为不用 Helm 的 K8s 用户，我 `kubectl apply -f deploy/k8s/` 一样能起，且清单自带安全基线（non-root、资源限额、健康探针）。

行为规格：`deploy/k8s/`（Deployment / PVC / Service / Secret 样例 / Ingress 注释样例）；Deployment：`securityContext.runAsNonRoot=true`、resources requests+limits、probes、env 口令经 Secret；镜像 tag 以文档变量锚定（不写 latest）；目录 kustomize 兼容（`kustomization.yaml`，P1）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-38-AC1 | G14：kind 上 `kubectl apply -f deploy/k8s/` → Pod Ready → `/readyz` 200 → push/pull 一腿成功 → delete pod 重建制品存活 | P0 |
| FR-38-AC2 | 清单静态断言：`kubectl get deploy -o yaml` 含 `runAsNonRoot: true`、resources requests/limits、探针三件；镜像字段无 `latest` | P0 |
| FR-38-AC3 | `kubeconform`（或 kubeval）对清单校验通过；`kustomize build` 可用（P1） | P1 |

### FR-39 systemd 单元 + 安装脚本（release-engineer）

**用户故事**：作为裸机运维，我跑一次安装脚本（或照文档三步手工），得到开机自启、崩溃自拉起、优雅停机的 binflow 系统服务。

行为规格：

- `contrib/systemd/binflow.service`：`Type=simple`（sd_notify 为 P2 可选增强，不承诺）、`User=binflow`、`ExecStart=/usr/local/bin/binflow-server serve -c /etc/binflow/binflow.yaml`、`Restart=on-failure`、`ReadWritePaths=/var/lib/binflow`（硬化）、`TimeoutStopSec` 对齐优雅停机 ≥ 30s（architecture §9 公共约定）。
- `install.sh`：system 用户创建、目录布局（/etc/binflow /var/lib/binflow /usr/local/bin）、二进制下载 + **checksums 校验失败即中止**、单元安装、`systemctl enable --now`、`--dry-run` 支持、卸载路径文档化；依赖仅 bash + curl + sha256sum + systemctl。
- 运行时真机验证为条件腿（darwin 宿主不可达 → §7 Q3 降级：容器内 `systemd-analyze verify` 静态验证 + 记录 + 用户确认；有 Linux VM/CI runner 则实测）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-39-AC1 | G15：容器内 `systemd-analyze verify binflow.service` 零 error；`shellcheck install.sh` 零告警 | P0 |
| FR-39-AC2 | G15b（条件腿，Q3）：Linux 环境 `systemctl start` → `active (running)` → `/readyz` 200 → `systemctl restart` → 制品存活；`systemctl is-enabled` = enabled；不可达按降级口径记录并经用户确认 | P1 |
| FR-39-AC3 | G16：校验和防线——沙箱内篡改二进制后跑安装脚本 → 脚本在校验步失败退出（退出码非 0、零文件落地） | P0 |
| FR-39-AC4 | 停机优雅：`systemctl stop` 后日志含优雅停机行、退出码 0（条件腿，随 AC2） | P1 |

### FR-40 离线安装包（release-engineer）

**用户故事**：作为 air-gapped 集群的运维，我拿到一个 tar：里面有双变体镜像、Chart、K8s 清单、linux 二进制、校验和与安装说明——安装全程零外网。

行为规格：

- 产物 `binflow_offline_<VER>.tar.gz`，内部布局（K3 暂行，release-engineer 细化）：
  - `images/binflow-<VER>.tar`（docker save：双变体 multi-arch 一并保存）
  - `charts/binflow-<VER>.tgz` + `k8s/`（原生清单副本）
  - `binaries/`（linux amd64 + arm64 二进制与 checksums）
  - `install-offline.sh`（docker load → 可选 kind 导入 / compose 起 / helm install 分支）+ `README-offline.md`
  - `SHA256SUMS`（包内全件）——外层 tar 自身亦附 sha256（发布清单联动 FR-34 checksums）
  - docs 不单独附带（embed 随二进制/镜像自带；若 ADR-0011 fallback 生效则附 `docs-static_<VER>.tar.gz`）
- **零外网语义**：包内自足，安装过程零外网请求（NFR-S29 联动）；文档口径「离线包 = 唯一需要带进机房的东西」。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-40-AC1 | G17：包结构断言（上述件齐全）+ 包内 `SHA256SUMS` 全过 + 外层 tar sha256 记录 | P0 |
| FR-40-AC2 | G17b：隔离网络环境（独立 docker network / 断网 VM；QA 注明手法——镜像与包预置后断外网）从包起 compose 或 kind 栈 → `/readyz` 200 → docker push/pull + generic PUT/GET 成功，全程零外网请求（代理路由断言或 `--network none` 前置 load 手法） | P0 |
| FR-40-AC3 | G18 联动：断网浏览器（Playwright 离线上下文）访问该实例 `/binflow/docs/` 可浏览可搜索 | P0 |
| FR-40-AC4 | 包体积与件数记录进 QA 报告（记录项，P2） | P2 |

### FR-41 帮助文档中心（Docusaurus 五类；devops-engineer 脚手架 + tech-writer 内容）

**用户故事**：作为新接手的同事，我打开 `http://registry:8080/binflow/docs/`——左侧五类导航（安装 / 客户端接入 / 管理 / API 参考 / FAQ）、右上角搜索框；在断网机房里它照样能搜——文档随二进制走。

行为规格：

- **五类信息架构**（docs/user 既有 11 篇收编 + M5 新增篇目）：
  1. **安装指南**（新增）：每部署形态一篇——bare 二进制 / Docker / compose / Helm / 原生 K8s / systemd / 离线包（7 篇）+ 升级与版本说明（1 篇，P1）；
  2. **客户端接入**（已有 4 篇收编 + 1 新增）：docker（含 Helm OCI）/ maven / npm / pypi + **generic(raw) 上传下载**（新增）；maven 篇补 **mvn ≥ 3.8 `maven-default-http-blocker` 注意事项**（O-106-2 收编——T-106 修正账⑤ 实证）；
  3. **管理指南**（已有 5 篇收编）：console / groups-permissions / governance（**补 `graceHours:0` 运维告警——显式 0 = 摘 mtime 宽限，busy 实例 `apply + graceHours:0` 可致在途上传偶发失败**，T-94-review N6 收编）/ backup-restore / remote-virtual；
  4. **API 参考**（新增）：Artifactory 兼容端点子集总表（E/DE/ME/NE/PE/SE/SR 域汇总，来源各里程碑 PRD 矩阵）+ `/api/v1` 自有端点 + 三凭据面（Basic / Token / session）+ 错误信封三分层（errors[] / 纯文本 / OAuth）；
  5. **FAQ**（已有 1 篇扩编）：GA 条目（15 分钟口径 / 体积预算 / 单副本约束 / 与 Artifactory 差异对照）。
- **站点形态（K1 暂行，architect ADR-0011 增补终裁）**：Docusaurus 3；**中文（zh 唯一 locale，i18n 骨架就位）**；**self-host 静态资产零 CDN**；**本地索引全文搜索**（离线可搜）；版本化 `docs/v1.x`（首个版本目录）；`docs-site/` 以 `docs/user/` 为内容源聚合构建（writer 只写 Markdown 源，frontmatter 用兼容子集——ADR-0011 工作流细则）；`make docs` 目标（build + 复制进 `internal/docs/` embed）；独立托管可选输出 `docs-static_<VER>.tar.gz`（同一 build 产物，用户自办，不承诺双轨）。
- **挂载与访问**：`GET /binflow/docs/**`（统一前缀内，ADR-0011；repo key 保留字 `docs` 已在 ADR-0008 并集）；**匿名只读**——`anonymous_access=false` 实例同样放行（与 `/healthz` 同类产品自描述面，ADR-0011 明文「匿名可读」）；无任何写面。控制台「帮助」入口链到 `/binflow/docs/`（前端一行，P1）。
- **体积预算**：docs embed 增量 ≤ 15MB（gzip 口径）；与二进制 40MB 总预算联动（FR-34-AC3）；超限触发 ADR-0011 fallback（独立 tar + 二进制重建）须经用户确认。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-41-AC1 | G19：`make docs` 退出码 0；embed 后 `GET $BASE/binflow/docs/` 200（站点 shell）；`anonymous_access=false` 实例匿名 GET 同 200 | P0 |
| FR-41-AC2 | G20：五类导航逐类点达 ≥ 1 页；篇目清单核对——安装 7 形态 + generic 接入 + API 参考三处新增为硬项（QA 按篇目清单逐篇 200） | P0 |
| FR-41-AC3 | G18：Playwright 离线上下文（route abort 一切外部 host）——docs 首页 / 中文搜索（「配额」「离线」两关键词）/ 正文页全可用，**零外网请求断言**（NFR-S29） | P0 |
| FR-41-AC4 | G21：docs embed 增量 ≤ 15MB 且六平台压缩产物 ≤ 40MB（与 G03 对账）；fallback 未触发或触发经用户确认记录 | P0 |
| FR-41-AC5 | G22：版本下拉存在且含 v1.x；搜索命中跳转正确页 | P1 |
| FR-41-AC6 | 内容验收（tech-writer DoD）：maven 篇含 http-blocker 注意（O-106-2）；governance 篇含 `graceHours:0` 运维告警（N6）；API 参考含错误信封三例与兼容端点总表；backup-restore 篇含「删文件后空目录自动清除是预期行为」口径（ADR-0016 prune 语义联动 FR-44） | P0 |
| FR-41-AC7 | `docs-static_<VER>.tar.gz` 随 release 归档 + 自托管说明（nginx 静态托管一页，P1）；控制台帮助入口可达（P1） | P1 |

### FR-42 安全审计（security-auditor）

**用户故事**：作为安全评审员，我问「GA 版本过没过安全审计」——对方拿出报告：Go 依赖零高危、双变体镜像零 Critical/High、全仓零密钥、权限模型负断言集复跑结论、发布物 checksums 齐全——每条可复跑。

行为规格（产物：安全审计报告归档 `reports/`，发现分级 + 豁免清单（每条含理由与期限）+ 修复状态）：

- **依赖面**：`govulncheck ./...`（Go 运行时面，零 High/Critical 或豁免）；`npm audit`（web/ 与 docs-site/ 构建面——High 需修或豁免记录；构建面依赖不入运行时二进制，NFR-S31）。
- **镜像面**：trivy（或等价）扫**双变体 × 2 架构**：`--severity HIGH,CRITICAL --exit-code 1` 口径零发现（distroless 面天然小；alpine apk 升级处置由 release-engineer 执行）。
- **密钥面**：gitleaks（或等价）全仓扫描零命中（M1 起零密钥承诺延续，含历史）；`go mod verify` 通过。
- **静态面**：golangci-lint（gosec 规则集）零告警维持；新增 `deploy/`+`charts/` 面纳入 lint（helm lint / kubeconform / shellcheck）。
- **模型复核**：三凭据面（Basic / Token / session）与 CSRF / SSRF / 路径穿越既有负断言集复跑（抽样 NFR-S13/S19~S26 面，引用既有序列编号不新造）；export 产物敏感面（NFR-S22：0700 + 密文同等保管）复查。
- **供应链**：全发布物 checksums（FR-34/FR-40 联动）；构建脚本与 CI 零凭据硬编码；goreleaser / docusaurus 等构建工具与运行时依赖隔离登记（ADR-0005 依赖准入记录）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-42-AC1 | G23：`govulncheck ./...` 零 High/Critical（数据库时点记录）或豁免清单齐备；`go mod verify` OK | P0 |
| FR-42-AC2 | G24：trivy 双变体 × amd64/arm64 零 HIGH/CRITICAL（`--exit-code 1` 口径）；alpine 变体 OS 包已升级处置 | P0 |
| FR-42-AC3 | G25：密钥扫描零新增命中；`npm audit`（web/docs-site）High 修复或豁免记录 | P0 |
| FR-42-AC4 | G26：负断言抽样集复跑全绿——SSRF 16 直连变体抽查、路径穿越变体抽查、CSRF 跨源 403、匿名矩阵（引用既有 W/M 序列编号，报告列明复跑项） | P0 |
| FR-42-AC5 | 安全审计报告归档：分级发现表 + 豁免清单 + 结论（「可 GA」或阻塞项——阻塞 = Critical 未修）；任何 Critical 修复后复扫闭环 | P0 |
| FR-42-AC6 | 凭据生命周期可审计复核（FR-45 联动）：token.issue/token.revoke 可查、detail 零明文（G31 同断言） | P0 |

### FR-43 性能基准与 GA 验收总矩阵（qa-engineer）

**用户故事**：作为容量规划师与评估者，我要数据不要形容词：1000 并发拉取曲线、冷启动与内存、每形态启动时间、export 与 GC 吞吐——一份基准报告 + 一张 GA 总矩阵表。

行为规格：

- **PRODUCT 成功标准全量兑现**：1000 并发拉取零错误（docker pull 池 + REST GET 混合；QA 手法：预热 ≥ 50 blob、1000 并发会话 / 连接复用口径在报告注明——SQLite WAL 面 M4 NFR-P20 先例放大）；冷启动 < 2s（三腿平台抽查）；空载 RSS < 100MB；1GB 流式 RSS 增量 < 256MB 维持；**checksum 去重生效断言**（跨五协议同 blob 单物理份，C07/D16/AC7 手法复跑）。
- **基准矩阵记录项**（记录不设门，供选型与 M6+ 决策）：REST PUT/GET 吞吐与 P95、搜索 P95（NFR-P17 维持）、export/import 吞吐、**GC dry-run/apply 吞吐（CLI 双遍 mark 基线数据——为 §6.4 #10 的 M6+ 优化决策供数，本里程碑零优化）**、docs/console 首屏。
- **形态启动收敛**：各部署形态从「命令执行」到 `/readyz` ≤ 60s（镜像拉取 / K8s 调度 / systemd 启动不计入——口径报告注明）。
- **GA 总矩阵**（qa 产出）：FR-34~FR-42 每条 AC × 部署形态 × PASS/FAIL/降级 三态汇总表——GA 验收的唯一索引。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-43-AC1 | G27：1000 并发混合拉取（REST GET 为主 + docker pull 循环）退出码全 0、零 5xx、零客户端错误；≥ 5 分钟持续负载曲线记录（P1） | P0 |
| FR-43-AC2 | G28：冷启动 < 2s（darwin / linux-amd64 / 条件 windows 三腿）；空载 RSS < 100MB；1GB 流式 < 256MB（M1 口径复跑） | P0 |
| FR-43-AC3 | G29：基准报告归档（矩阵齐全含记录项）；与 M4 基线（T-105）对比 P95 面**无 > 20% 回退**（回归判据；回退项归因） | P0 |
| FR-43-AC4 | GA 总矩阵表产出（每 FR 一行：AC 清单 × 形态 × 三态）；降级腿逐条引用 Q3 用户确认记录 | P0 |
| FR-43-AC5 | 去重断言：跨五协议同 blob 单物理份 + stats 对账（复跑 AC7/D16 手法） | P0 |

### FR-44 M4 债务收编（一）：目录实体化落地（ADR-0016；dev-go-core BE + dev-frontend FE）

**用户故事**：作为从 Artifactory 迁移的开发者，我 `GET /api/storage/<repo>/a/b/` 一个从未 mkdir 过的目录，得到 200 FolderInfo（含真实 created/lastModified）而不是 404——「目录是实体」的心智成立；删掉唯一子文件后空目录链自动消失，和原厂一致。

行为规格（ADR-0016 五要点 + T-119 草案两票，票面要点照录不重议）：

- **BE 票（dev-go-core）**：`putNode` 内材料化祖先 folder 行（祖先先于目标行落库——崩溃窗口至多残留良性空 folder 行）；祖先是派生状态——不判权、不过 governance pattern、不记审计、quota/usage delta 0；迁移 `007_folder_rows_backfill.{sqlite,postgres}.sql`：先 `INSERT OR IGNORE` 哨兵 blob 行（FK 前置，实现最大坑）再递归 CTE 推祖先集，双方言幂等。
- **FE 票（dev-frontend，硬依赖 BE 合入后——先删兜底会回归 404）**：删 `searchListing` 双兜底与 mkdir 逐段材料化循环（改单段 PUT）；TreePage 404 分支对非根目录恢复可达（T-100 review NB① 结构性关闭）。
- **行为变更面（GA 前最后一个）**：隐式目录 `GET`/`?list` **404 → 200**；`pruneEmptyParents` 激活（删文件后空祖先链清除——**含显式 mkdir 的空目录**，repo-semantics §4 参考语义；文档口径 FR-41-AC6 联动）；docker 仓 image 目录成为可浏览实体行。
- **边界维持**：remote engine 不跟随（architecture §11.20，M6+ 随 remote 浏览面评估）；搜索索引对 folder 行的显式排除维持（search 域零变更）；007 回填行 `created_by=''`。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-44-AC1 | G30a：深层单上传（≥ 4 段路径）后逐级隐式目录 `GET /api/storage/<repo>/<dir>/` 200 FolderInfo（真实 created/lastModified）；`?list` 200 | P0 |
| FR-44-AC2 | G30b：删除唯一子文件 → 空祖先链被 prune（GET 404）；有兄弟子代不剪；显式 mkdir 的空目录同样被剪（参考语义断言） | P0 |
| FR-44-AC3 | G30c：007 幂等（重跑零变化）；存量库升级路径（老库迁移后隐式目录同样 200） | P0 |
| FR-44-AC4 | G30d：export/import 往返含 folder 行与哨兵 blob（W28/W30 复跑绿）；**T-124 面回归**——mkdir 实例 export manifest 零占位仍成立（FolderMarkerSHA 排除按值不受回填影响） | P0 |
| FR-44-AC5 | G30e：Playwright——隐式目录主路径浏览**不再发 `/api/search` 请求**（request 监听断言）；typo 深链 404 态正确呈现（NB① 关闭证据） | P0 |
| FR-44-AC6 | 回归：docker 套件全绿（image 目录行为变更面全量复跑）；governance 拒绝上传**零祖先残留**；并发同新目录上传无 unique violation；存量「隐式目录 404」断言按 §5.6 反转 | P0 |

### FR-45 M4 债务收编（二）：token 签发/吊销落审计（dev-go-core）

**用户故事**：作为审计员，我在审计页按 `action=token.issue` / `token.revoke` 检索——凭据的签发与吊销有据可查，且记录里永远看不到 token 明文。

行为规格（D-104-2 修复——M4 PRD FR-29 词表已列 `token.issue/token.revoke` 而实现未落，前提由不实变实）：

- **挂点**：TokenRegistry 签发与吊销点落审计——`token.issue`（actor / token 指纹（sha256 前 8 位，非明文）/ 过期时长）；`token.revoke`（actor / 被吊销 token 指纹）；REST（`/api/security/token` 族）与 CLI 面（若有）全覆盖。
- **docker 会话 token 不落（产品口径，防审计噪声）**：`/v2/token` 短 TTL 会话 token **不产生** `token.issue`——它是运行态凭据（类同 web_sessions 不入备份的 §11.19 同族口径），每次 docker pull 都签发，全落会把审计表灌成 docker 流水；docker 域的凭据生命周期由访问日志与 `login.*` 既有词覆盖。
- 词表零变更（M4 FR-29 定案全集维持）；脱敏链（NFR-S3）对新事件生效；M4 W23 词表断言扩两词（T-104 E2 收口核验同步——§6.4 速裁 #2）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-45-AC1 | G31a：`POST /api/security/token`（client_credentials）→ 200 后 `GET /api/v1/audit?action=token.issue` ≥ 1 条；detail 无 token 明文（grep 零命中） | P0 |
| FR-45-AC2 | G31b：revoke → `token.revoke` ≥ 1 条；被吊销 token 重放 401 | P0 |
| FR-45-AC3 | G31c：docker 隔离断言——`/v2/token` 签发（docker login / Bearer 协商）**不产生** `token.issue` 事件（口径断言，防噪声回归） | P0 |
| FR-45-AC4 | 词表回归：W23 动作词循环扩 `token.issue` / `token.revoke` 两词全过 | P0 |

### FR-46 M4 债务收编（三）：控制台 docker 视图与树数据源定案（dev-frontend + 产品裁决）

**用户故事**：作为开发者，我在控制台打开 docker 仓，看到镜像与 tag 的树——数据来自 storage 面，我的登录会话就够了，不需要给浏览器发一枚 docker 专用 token。

行为规格：

- **数据源裁决（产品定案，终结 T-123 移交观察）**：控制台 docker 数据源 = **storage 面**（`GET /api/storage/{repo}/{path}` children + item info）——session 凭据（cookie `Path=/binflow`）原生可用、与权限模型同源（NFR-S26 不绕权）。三个候选出处：① 匿名 `/v2/_catalog`——被否（匿名关实例不可用且绕 ACL）；② 控制台持 Bearer 走 `/v2`——被否（E4 定案：cookie 结构性不可达根级 /v2；给浏览器发 docker 专用凭据是新的凭据面）；③ 反代改写——被否（把 UI 耦合到部署形态）。ADR-0016 落地后 image 目录为可浏览实体行，storage 面数据完整度就位。
- **特化视图最小面（P1）**：docker 仓树节点渲染 manifest digest 行 + tag 徽标（数据源 storage children；tag node M2 起落 nodes 行）；maven/npm/pypi 特化视图维持 M6+（通用树已可用，特化是增强非缺口——T-100 遗留「五协议特化视图 P1」至此收窄为 docker 一面）。
- **零新端点**：数据源全既有；若 FE 需 tag→manifest 聚合，走 children 面组合——本里程碑不扩端点（缺口登记归 M6+ 评估）。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-46-AC1 | G32a：Playwright——docker 仓树可见 manifest / tag 行（session 凭据登录）；**request 监听断言零 `/v2/*` 请求**（数据源口径证据） | P1 |
| FR-46-AC2 | G32b：数据等价抽样——UI 树中 tag 集与 `crane ls`（或 curl + Bearer `/v2/.../tags/list`）输出一致 | P1 |
| FR-46-AC3 | 文档：console 篇补「docker 视图数据源 = storage 面」一句口径（FR-41 内容矩阵联动） | P1 |

### FR-47 M4 债务收编（四）：storage uri 基址族修正（dev-go-core，P2）

**用户故事**：作为迁移用户，我的脚本解析 `?list` 响应顶层 `uri` 当 URL 用——拿到的是可直接请求的地址，而不是拼错段名的路径。

行为规格（O-106-1 + O-4 同族修复）：

- **统一规则**：storage REST 族（item info / `?list` / `?permissions`）的 uri 基址拼接统一——`uri` = `<scheme://host[:port]>/binflow/api/storage/{repo}/{path}` 形态（以 docs/reverse/rest-api.md §3 规格形为准，高置信度）；**repo key 恰出现一次**（O-106-1 现状为 `http://host/<repo>/api/storage/<path>` 双重嵌段）；item info 与 `?list`/`?permissions` 家族基址一致（O-4 现状拼接规则互异）。
- 修正在 GA 前的必要性：uri 形状是兼容承诺的一部分——GA 后再改等于对脚本用户 breaking。

| # | AC（可执行） | 优先级 |
|---|---|---|
| FR-47-AC1 | G33a：`GET $BASE/binflow/api/storage/{repo}/{dir}/?list` 顶层 uri——① 与 `files[].uri` 同基 ② repo key 恰一次 ③ `curl <uri>` 200（自洽性） | P2 |
| FR-47-AC2 | G33b：`?permissions` uri 与 item info uri 拼接规则一致（同族断言）；形态对照 rest-api.md §3 | P2 |
| FR-47-AC3 | 回归：M4 W21（?permissions）与 W12（树消费）断言零破坏；FE 树 / 搜索零改动（FE 不消费顶层 uri——T-100 核实，QA 复核注记） | P2 |

---

## 5. 兼容性矩阵（M5 核心）

### 5.1 层级定义（沿用 M1 §5.1 四层，M5 特化）

- **发布域（PB：goreleaser / 镜像 / 部署产物）对齐基准 = BinFlow 自有**——Artifactory 无对应交付物形态承诺（其 Helm/ compose 为 JFrog 自有分发），不构成兼容面；兼容性承诺不涉此域。
- **文档域（DC）对齐基准 = BinFlow 自有**（ADR-0011 已定案）；唯一行为约束是「匿名可读 + 零外网」。
- **债务修正域（DM）**：触达既有兼容端点的行为修正——逐条标注对齐来源与置信度；DM-02 是**行为缺口补齐**（原 404 偏离参考行为，ADR-0016 取证 storage-layout §3 + repo-semantics §4 双高置信）。
- 错误契约：M5 无新错误面；docs 静态面 404 走静态页（无信封）。

### 5.2 M5 端点/产物矩阵

「置信度」：高 = 逆向规格明文 / ADR 定案 / 用户定案；中 = PRD 暂行（待 §5.5 校准）。编号前缀：PB = 发布（产物面），DC = 文档中心，DM = 债务修正（触达既有端点）。

| # | 端点 / 产物 | 行为要点 | 层级 | 优先级 | 置信度 | 验收 |
|---|---|---|---|---|---|---|
| PB-01 | goreleaser 六平台产物 + `binflow_<VER>_checksums.txt` | linux/darwin/windows × amd64/arm64；CGO 零维持；单二进制含 console+docs embed | 自有 | P0 | — | G01/G03 |
| PB-02 | `--version` + `/api/v1/health` `version` 字段 | ldflags 注入 `<VER> (<git short sha>)`；裸 build 回退 dev；health 只增字段 | 自有 | P0 | — | G02/G04 |
| PB-03 | 镜像双变体 multi-arch（`<VER>-alpine` / `<VER>-distroless` / 浮动 `<VER>`） | amd64+arm64 manifest list；非 root；HEALTHCHECK；镜像内自建 console/docs（不依赖构建机） | 自有（浮动 tag 指向为 K2 暂行） | P0 | 中（K2） | G06~G08 |
| PB-04 | `deploy/compose/` GA 产物 | 持久卷 / restart / healthcheck / 反代 profile（D-106-2 修复片段）；15 分钟口径 | 自有 | P0 | — | G09/G10 |
| PB-05 | `charts/binflow/`（persistence / ingress / HPA 模板） | values.schema 拦 replicaCount>1；HPA 默认 disabled + maxReplicas≤1（Q2 暂行）；ingress 直通 `/` 与 `/v2/` 不 rewrite | 自有 | P0 | 中（Q2/K2） | G11~G13 |
| PB-06 | `deploy/k8s/` 原生清单 | runAsNonRoot / resources / probes；镜像 tag 文档锚定非 latest | 自有 | P0 | — | G14 |
| PB-07 | `contrib/systemd/binflow.service` + `install.sh` | Type=simple / Restart=on-failure / ReadWritePaths；install.sh checksums 校验失败即止 | 自有 | P1 | — | G15/G16 |
| PB-08 | `binflow_offline_<VER>.tar.gz` 离线包 | 镜像+chart+清单+linux 二进制+SHA256SUMS+安装脚本；零外网安装 | 自有 | P0 | 中（K3） | G17 |
| DC-01 | `GET /binflow/docs/**` | Docusaurus 站（zh / 零 CDN / 本地搜索索引 / v1.x 版本化）；**匿名只读**（匿名关实例同放行）；无写面 | 自有 | P0 | 高（ADR-0011）/ 中（K1 构建细节） | G18~G22 |
| DC-02 | 控制台「帮助」入口 → `/binflow/docs/` | 前端一行链接（P1） | 自有 | P1 | — | G19b |
| DM-01 | `?list` 顶层 uri / `?permissions` uri 基址修正（O-106-1/O-4 族） | uri 与 files[].uri 同基、repo key 恰一次、自 curl 200；形态对照 rest-api.md §3 | 兼容（E-10 子集行为修正） | P2 | 高（rest-api §3） | G33 |
| DM-02 | 隐式目录 `GET /api/storage/{repo}/{dir}`（含 `?list`）**404 → 200** FolderInfo（ADR-0016） | putNode 材料化祖先 + 007 回填；prune 空祖先链（含显式 mkdir 空目录）；docker image 目录可浏览 | 兼容（行为缺口补齐） | P0 | 高（ADR-0016 双源取证） | G30 |
| DM-03 | `token.issue` / `token.revoke` 落审计 | 词表兑现（M4 已列）；REST 挂点；**`/v2/token` 会话 token 不落**（防噪声口径） | 自有（审计查询面既有 GE-01） | P0 | — | G31 |

> 计数：**13 条**。兼容（子集/行为修正）**2**（DM-01/DM-02）；自有（含产物 / `/api/v1` 家族 / CLI）**11**（PB-01~08、DC-01/02、DM-03）；有意不兼容 **0**（M5 无新不兼容项——既有 Non-goal 面〔SE-09/SR-04/GE-04/GE-09 等〕维持原裁定不重计）。另：docker 树数据源裁决（FR-46）**零新端点**，不入矩阵计数。

### 5.3 客户端与部署形态分级矩阵（GA 判定标准）

「全过」定义：所列操作退出码 0 且服务端日志无 5xx；「降级」定义见 §7 Q3（T-106 §2.5 先例定案化）。

| 成员 | 必测面 | 分级 |
|---|---|---|
| curl + 发布 CLI（goreleaser / helm / kubectl / systemd-analyze / shellcheck / kubeconform / trivy / govulncheck / gitleaks） | G01~G35 主体 | **P0 必须全过** |
| docker / buildx / dind（registry 客户端） | G06~G09、G17、G27、G34 基线 | **P0 必须全过** |
| kind 或 Docker Desktop K8s（helm / kubectl 消费面） | G13 / G14 / G17b | **P0**（环境不可达 → Q3 降级 + 用户确认） |
| Playwright（docs 离线面 / FE 清理断言 / docker 视图） | G18 / G30e / G32 | docs 面与 FE 清理 **P0**；docker 视图 P1 |
| mvn / npm / pip / twine（回归基线） | G34 四序列 P0 | **P0 必须全过** |
| systemd 真机 / windows 运行时（条件腿） | G05 / G15b | **P1 条件**（Q3：不可达即降级，记录 + 用户确认） |
| WebKit / Firefox（docs 三页链） | 观察 | P2（不作门槛） |

### 5.4 M5 核心验收命令（G 序列，QA 直接引用）

> `$BASE/$ADMIN_PW/$VER` 沿用 §4 约定；`DIST=dist`；kind 上下文名 `bf-ga`；dind 手法沿 T-105/T-106（宿主 daemon 零改动）。带「条件」注记的腿按 §7 Q3 口径。

```bash
# ---- 发布矩阵：goreleaser（FR-34） ----
# G01 产物与校验和（PB-01）
make release-snapshot && echo RC=$?                          # 0
ls $DIST | grep -c 'binflow_.*_\(linux\|darwin\|windows\)_'  # 6（六平台）
(cd $DIST && shasum -a 256 -c binflow_${VER}_checksums.txt) | grep -vc OK   # 0
# G02 版本注入（PB-02）
./$DIST/binflow_${VER}_darwin_arm64/binflow-server --version # binflow-server <VER> (<sha>)
BIN=$DIST/binflow_${VER}_linux_amd64_/binflow-server         # QA 按实际解包路径
docker run --rm -v $PWD/$DIST:/d -w /d alpine ./$BIN --version | grep -v dev
strings $DIST/binflow_${VER}_windows_amd64.zip 2>/dev/null || unzip -p $DIST/binflow_${VER}_windows_amd64.zip | strings | grep -m1 $VER
# G03 check-size（PRODUCT <40MB；docs 增量记录）
find $DIST -name '*.tar.gz' -o -name '*.zip' | xargs ls -l   # 逐件 ≤ 40MB（QA 表格化记录）
# G04 可跑腿（darwin 本机 + linux/amd64 容器）
BINFLOW_DATA_DIR=/tmp/g04 BINFLOW_ADMIN_PASSWORD=$ADMIN_PW ./$BIN serve -c binflow.yaml &
sleep 1; curl -s localhost:8080/readyz                       # OK（冷启动 <2s 记时）
curl -s localhost:8080/binflow/ui/ -o /dev/null -w '%{http_code}\n'    # 200
curl -s localhost:8080/binflow/docs/ -o /dev/null -w '%{http_code}\n'  # 200
curl -su admin:$ADMIN_PW localhost:8080/binflow/api/v1/health | jq -r .version   # <VER>
# G05 条件腿（windows）：serve 起服 → export 成功 → 并发第二 export/gc 退出码非 0（LockFileEx）
# G04b P1：docker run --platform linux/arm64 alpine ... /readyz OK

# ---- 镜像双变体（FR-35） ----
# G06 multi-arch（PB-03）
docker buildx build --platform linux/amd64,linux/arm64 -t binflow:${VER}-alpine --push ./deploy/release/alpine  # 本地 registry 或 --output type=oci
docker buildx imagetools inspect binflow:${VER}-alpine | grep -c 'linux/\(amd64\|arm64\)'   # 2（distroless 同）
# G07 变体契约
docker run -d --name g07 binflow:${VER}-distroless; sleep 2
docker exec g07 id -u 2>/dev/null || echo NO_EXEC            # distroless: NO_EXEC（无 shell）
docker run --rm --entrypoint sh binflow:${VER}-distroless true 2>&1 | grep -qi 'no such\|not found' && echo NO_SHELL_OK
docker inspect g07 | jq '.[0].State.Health.Status'           # healthy（alpine 腿 id -u 非 0）
# G08 embed 就位（镜像内自建，指纹对账 T-106 §2.2 手法）
docker exec g07-none /bin/sh -c true 2>/dev/null; curl -s localhost:8080/binflow/docs/ -o /dev/null -w '%{http_code}\n'  # 200（端口映射后）

# ---- compose / Helm / K8s / systemd / 离线包（FR-36~40） ----
# G09 compose 全链（PB-04；T-106 §2.2 口径：五协议 + restart 持久 + down -v 零残留）
docker compose -f deploy/compose/docker-compose.yml up -d --build
docker compose -f deploy/compose/docker-compose.yml ps | grep healthy
#   …五协议烟测（dind docker push/pull + curl/mvn/npm/twine 各一腿）→ restart → 制品+whoami 200 → down -v
docker compose -f deploy/compose/docker-compose.yml config -q && echo CFG_OK
# G10 15 分钟口径：clean clone + 计时（make/console 由镜像内构建，宿主零前置）→ /readyz；记录分钟数
# G11 Helm lint/template（PB-05）
helm lint charts/binflow                                     # 0 错误
helm template charts/binflow | grep -c 'kind: \(Deployment\|PersistentVolumeClaim\|Service\)'
helm template charts/binflow | grep -c 'HorizontalPodAutoscaler'   # 0（默认 disabled）
# G12 单副本拦截
helm install bf charts/binflow --set replicaCount=2 --dry-run-server 2>&1 | grep -i 'replica'  # schema 拒绝
# G13 kind 安装 + PVC 持久
kind create cluster --name bf-ga && kind load docker-image binflow:${VER}-distroless --name bf-ga
helm install bf charts/binflow --set image.repository=... --set adminPassword=$ADMIN_PW
kubectl wait --for=condition=Ready pod -l app.kubernetes.io/name=binflow --timeout=180s
kubectl port-forward svc/bf 18080:8080 & curl -s localhost:18080/readyz   # OK
#   …dind→NodePort push/pull 一腿；kubectl delete pod … → 制品 GET 200（PVC 幸存）
# G13b P1 ingress 渲染：helm template --set ingress.enabled=true | grep -A3 'paths:'  # / 与 /v2/ 同 host 直通、零 rewrite 注解
# G14 原生 K8s 清单
kubectl apply -f deploy/k8s/ && kubectl wait --for=condition=Ready pod -l app=binflow
kubectl get deploy -o yaml | grep -c 'runAsNonRoot: true'    # ≥1；resources/probes 断言同法
kubeconform -strict deploy/k8s/*.yaml                        # P1
# G15 systemd 静态（容器内）
docker run --rm -v $PWD/contrib/systemd:/s systemd/ubuntu systemd-analyze verify /s/binflow.service; echo RC=$?
shellcheck contrib/systemd/install.sh                        # 零告警
# G15b 条件腿：systemctl start/stop/restart + /readyz + is-enabled（Q3）
# G16 校验和防线：篡改二进制 → install.sh 校验步失败退出（沙箱跑，退出码非 0、零落地）
# G17 离线包结构 + 校验
tar tzf binflow_offline_${VER}.tar.gz | grep -c -e images/ -e charts/ -e k8s/ -e binaries/ -e SHA256SUMS   # ≥5
mkdir /tmp/off && tar xzf binflow_offline_${VER}.tar.gz -C /tmp/off && (cd /tmp/off && shasum -a 256 -c SHA256SUMS) | grep -vc OK   # 0
# G17b 零外网安装（隔离网络起栈 → readyz → push/pull + PUT/GET；QA 注明网络隔离手法）
# G18 docs 离线可用（见下 Playwright 块）

# ---- 文档中心（FR-41；Playwright 离线上下文 + curl） ----
make docs && echo DOCS_BUILD_OK
# G19 挂载与匿名豁免（DC-01）
curl -s $BASE/binflow/docs/ -o /dev/null -w '%{http_code}\n'                    # 200
#   匿名关实例（security.anonymous_access=false）同样 GET 200
# G19b 控制台帮助入口（P1）：Playwright 点 help 图标 → URL 含 /binflow/docs/
# G18 离线断言（NFR-S29）
cat > web/e2e/g18-docs-offline.spec.ts <<'EOF'
import { test, expect } from '@playwright/test';
test('docs fully usable offline', async ({ page }) => {
  await page.route(/^(?!.*localhost).*$/, r => r.abort());     // 阻断一切非本机请求
  await page.goto('/binflow/docs/');
  await page.fill('[data-testid="docs-search"]', '配额');
  await expect(page.locator('[data-testid="docs-search-results"]')).toBeVisible();
});
EOF
npx playwright test e2e/g18-docs-offline.spec.ts --project=chromium   # exit 0
# G20 五类篇目清单逐篇 200（安装×7 + 接入×5 + 管理×5 + API 参考 + FAQ——QA 按篇目表核对）
# G21 体积对账：docs embed 增量 ≤15MB（make docs 前后二进制差）+ G03 六平台 ≤40MB
# G22 版本化：页面版本下拉含 v1.x（P1）

# ---- 安全审计（FR-42） ----
# G23 依赖
govulncheck ./... > /tmp/vuln.txt; grep -ci 'high\|critical' /tmp/vuln.txt    # 0（或豁免清单）
go mod verify && echo MODV_OK
# G24 镜像
trivy image --severity HIGH,CRITICAL --exit-code 1 binflow:${VER}-distroless; echo RC=$?   # 0（两变体两架构 ×4）
# G25 密钥
gitleaks detect --no-git -v 2>/dev/null || gitleaks detect -v; echo RC=$?      # 0 命中
# G26 负断言抽样复跑（SSRF 16 变体抽查 / 路径穿越 / CSRF W38 / 匿名矩阵——引用既有序列，报告列复跑项）

# ---- 性能基准（FR-43） ----
# G27 1000 并发（PRODUCT 成功标准；预热 ≥50 blob，报告注明会话/连接口径）
seq 1000 | xargs -P1000 -I{} curl -sf -u admin:$ADMIN_PW $BASE/binflow/generic-local/perf/{}.bin -o /dev/null; echo RC=$?
#   docker pull 循环腿（dind 并发）+ 零 5xx 断言（服务日志 grep）
# G28 冷启动/内存：三腿平台 ping 计时 <2s；空载 RSS <100MB；1GB 流式 <256MB（M1 口径）
# G29 基准报告：矩阵齐全 + 与 T-105 基线 P95 对比（>20% 回退项归因）
# G29b GC 吞吐基线记录（CLI 双遍 mark 数据采集——M6+ 决策供数，不优化）

# ---- 债务收编（FR-44~47） ----
# G30a 隐式目录（DM-02）：深层上传后逐级 200
curl -su admin:$ADMIN_PW -T t.bin $BASE/binflow/generic-local/a/b/c/d.bin -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/generic-local/a/ -o /dev/null -w '%{http_code}\n'          # 200（原 404）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/a/b/?list" | jq '.files | length'           # ≥1
# G30b prune：DELETE a/b/c/d.bin → GET a/ 404（空链清除）；有兄弟不剪；显式 mkdir 空目录同剪
# G30c 007 幂等：迁移重跑零变化（QA 报告记录 changes() 断言）；存量库升级路径腿
# G30d 备份回归：mkdir 实例 export → manifest 零占位（T-124 面）→ import → folder info 200
# G30e FE 兜底删除：Playwright 隐式目录浏览 request 监听零 /api/search 请求；typo 深链 404 态
# G31 token 审计（DM-03）
TOKEN=$(curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token -d 'grant_type=client_credentials' | jq -r .access_token)
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=token.issue&limit=5" | jq '.events | length'      # ≥1
grep -c "$TOKEN" <(curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?limit=1000")                          # 0（零明文）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/security/token/revoke -d "token=$TOKEN" -o /dev/null -w '%{http_code}\n'  # 200
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=token.revoke&limit=5" | jq '.events | length'     # ≥1
# G31c docker 隔离：docker login + pull 后 audit 无新 token.issue（口径断言）
# G32 docker 视图（FR-46）：Playwright 树含 manifest/tag 行 + request 监听零 /v2/* 请求；crane ls 对账（P1）
# G33 uri 基址族（DM-01，P2）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/acme/?list" | jq -r .uri
#   断言：repo key 恰出现一次；curl <uri> 200；与 ?permissions/item info 同基

# ---- 回归与总矩阵（FR-43-AC4/AC5） ----
# G34 四里程碑回归：M1 C / M2 D / M3 M / M4 W 全序列 P0 复跑（断言按 §5.6 反转表更新）
#   隐式目录 404 断言（M1 C17 面）→ 200；--version dev → <VER>；/binflow/docs 404 → 200
# G35 GA 总矩阵 + 发布清单：FR×AC×形态三态表归档；发布物 sha256 清单产出（推送任何公共渠道前须 §7 Q1 定案 + 用户确认）
```

### 5.5 待校准项（M5 落地后回写，流程同 M1/M3/M4 §5.5）

| # | 项 | v1.0 暂行值 | 校准来源 |
|---|---|---|---|
| K1 | Docusaurus 构建形态细节：搜索索引方案（Docusaurus 内建本地索引 vs 外挂）、self-host 资产落盘布局、fallback 触发线（超 15MB 的处置步骤）、docs-static tar 内容 | 本 PRD DC-01 行为约束（匿名 / 零 CDN / 离线搜索 / v1.x）为准，实现形态如上暂行 | architect **ADR-0011 增补票终裁**；落地后回写 DC-01 注记（+0.1） |
| K2 | 镜像供应链细节：distroless 基镜像 tag 锚定、浮动 tag `<VER>` 指向 distroless、registry 命名空间 | FR-35 暂行值 | architect/release 增量票（随 §7 Q1 渠道定案联动） |
| K3 | 离线包内部布局与 `install-offline.sh` 语义（幂等 / 失败清理 / kind 导入分支） | FR-40 暂行结构 | release-engineer 产物票细化，QA 按 G17 断言 |

### 5.6 回归基线反转表（M5 起生效，qa 更新既有断言）

| 既有断言 | 来源 | M5 起的期望 |
|---|---|---|
| 隐式目录 `GET /api/storage/{repo}/{dir}` → 404 | M1 占位语义 / C17 面（T-105 修正账③） | **200 FolderInfo**（DM-02/ADR-0016）；C17 命令载体（子目录 list）语义不变 |
| M3 M16c 勘误注释「SNAPSHOT 目录为隐式目录、无 folder node」 | M3 v1.3 E3 注释 | 注释**过时**（M5 起 folder 行存在）；断言零反改（metadata+直连 GET 载体继续可用），注释随下次 M3 勘误更新 |
| `token.issue`/`token.revoke` 词表断言缺位（词表「M1 既有」前提不实） | M4 FR-29 / T-104 D-104-2、E2 | **可查 ≥ 1**（DM-03）；W23 词表循环扩两词 |
| `binflow-server --version` = `dev (revision dev)` | T-16/T-45/T-106 注记 | `<VER> (<git short sha>)`（PB-02）；裸 build 回退 dev 维持 |
| `/binflow/docs/**` → 404（未挂载） | 现状 | 200（DC-01；repo key 保留字 `docs` 既有 ADR-0008 并集） |
| docker 仓树 image 目录不可浏览 | 现状（T-119 行为变更面） | 可浏览实体行（DM-02 连带）；docker 套件全量复跑确认零断言依赖现状 |
| M1 C / M2 D / M3 M / M4 W 序列 P0 | 四轮 QA 基线 | **全部复跑全绿**（GA 硬门槛——五协议 + 权限 + 治理 + 备份零回归） |

---

## 6. 非功能需求（NFR）与 M4 债务归置

### 6.1 性能（M5 增量）

| NFR | 指标与验收方式 | 优先级 |
|---|---|---|
| NFR-P21 1000 并发拉取 | PRODUCT 成功标准首次全量兑现：1000 并发混合拉取（REST 为主 + docker pull）零错误、零 5xx（G27）；≥ 5 分钟持续负载曲线记录（P1） | P0 |
| NFR-P22 体积 / 冷启动 / 内存 | 单二进制压缩产物 < 40MB（六平台逐一，含 console+docs embed）；空库冷启动 < 2s（三腿平台）；空载 RSS < 100MB；1GB 流式 < 256MB 维持 | P0 |
| NFR-P23 docs 站点 | embed 增量 ≤ 15MB（gzip）；docs 首屏 < 3s（本机 Chromium，P1 记录）；离线搜索可用（G18） | P0/P1 |
| NFR-P24 备份与 GC 基线 | export/import 吞吐 ≥ 100MB/s（记录维持 NFR-P19）；GC dry-run/apply 吞吐记录（CLI 双遍 mark 基线，M6+ 决策供数） | P1 |
| NFR-P25 形态启动收敛 | 各部署形态「命令执行 → /readyz」≤ 60s（镜像拉取 / K8s 调度 / systemd 启动不计，口径报告注明） | P1 |

### 6.2 安全底线（M5 增量）

| NFR | 要求 | 验收 |
|---|---|---|
| NFR-S27 供应链完整性 | 全发布物附 sha256（checksums + 离线包双层）；install.sh 校验失败即止（G16）；goreleaser 与 CI 产物哈希记录（可复现性记录项） | G01/G16/G17 |
| NFR-S28 镜像最小面 | distroless 变体零 shell / 零包管理器（`--entrypoint sh` 失败断言）；两变体非 root；trivy 双变体 × 2 架构零 HIGH/CRITICAL | G07/G24 |
| NFR-S29 docs 面离线与零外联 | docs 静态资产 self-host 零 CDN；零第三方脚本 / 统计 / 字体外链；断网浏览器可浏览可搜索（零外网请求断言）——air-gapped 不出网既是体验也是安全属性 | G18 |
| NFR-S30 凭据生命周期审计 | token.issue/token.revoke 落审计且 detail 零明文（sha256 指纹形态）；`/v2/token` 会话 token 不落（防噪声口径，FR-45）；词表与脱敏链回归 | G31 |
| NFR-S31 依赖隔离与准入 | goreleaser / docusaurus / trivy 等构建侧工具不入 Go 运行时依赖（go.mod 运行面零新增——ADR-0005 依赖准入：构建链工具由 architect 记录在案）；docs-site node 依赖与运行时二进制隔离 | G23 + 评审项 |

### 6.3 可观测性（M5 增量）

- 结构化日志字段集维持；启动首行含 `version`（与 `--version` 同值）。
- `/binflow/api/v1/health` 只增字段原则：新增 `docs` 子系统状态（embed 资产就绪）与顶层 `version`（回显注入值）。
- 部署产物探针统一 `/healthz` `/readyz`（systemd / Helm / K8s / compose / 镜像 HEALTHCHECK 五面同源）。

### 6.4 M4 债务归置（逐条定界：入 M5 / M6+ / 关闭）

**主表：票面点名的十项。**

| # | 债务项 | 来源 | 处置 | 理由 |
|---|---|---|---|---|
| 1 | ADR-0016 实现草案（BE 材料化 + 007 回填；FE 删双兜底） | T-119 两票草案（BOARD M5 首票预留） | **入 M5（P0，FR-44）** | 产品完整性：隐式目录 404 偏离 rest-api.md §3 参考行为，且 FE 双兜底有实付代价（NB① 404 语义丢失）；007 回填越晚做存量越大；**GA 是行为面冻结点，此后修即 breaking**。FE 票硬依赖 BE 合入 |
| 2 | docker 树数据源裁决（storage 兜底 vs 匿名 /v2 vs 反代） | T-123 移交（随 T-100 P1 docker 特化视图） | **入 M5（P1，FR-46 定案）** | 产品完整性：五协议旗舰在控制台的自然完型；裁决是产品口径（三候选取 storage 面，零新端点、零新凭据面）；ADR-0016 落地后数据完整度就位，是最佳时点。maven/npm/pypi 特化视图收窄 M6+（通用树已可用） |
| 3 | token 签发/吊销不落审计（词表「M1 既有」前提不实） | T-104 D-104-2（M4 收尾评估） | **入 M5（P0，FR-45）** | 产品完整性：M4 PRD 词表已承诺而实现未落——PRD 断言与产品行为不一致必须在 GA 前消除；改动小（两挂点）+ 一条防噪声口径（docker 会话 token 不落） |
| 4 | windows 锁运行时验证（LockFileEx 未实证） | T-96 遗留（M5 预留） | **入 M5（P1 条件腿，FR-34-AC5）** | M5 首次交付 windows 二进制——锁失效 = 并发写 data dir = 数据安全面；有环境则实测，无环境按 Q3 降级（记录 + 用户确认），不再无限挂起 |
| 5 | 无 manifest 引用的 docker blob node 纳 GC 候选 | T-103 D-1（M5+ 评估）/ M4 v1.3 E3 注记 | **M6+** | E3 已定案「按每写原子接受」（P2：usage 可见、内容 API 不可达、无泄漏——非缺陷是语义）；修复需 docker_refs 感知的 mark 补集 = 动 GC 安全语义，GA 前不动（与 #10 同原则）；FR-43 记录 usage 存量可观测性供 M6+ 决策 |
| 6 | architecture §7.1 两行补遗（PUT /api/security/password、/api/v1/permissions CRUD） | T-122 遗留（M5 文档清单） | **入 M5（P2，architect 增量票顺带）** | 纯文档一行级成本；GA 是文档面复核点，路由表缺行会让 GA 后的 API 参考（FR-41）与架构文档失真 |
| 7 | O-106-1：`?list` 顶层 uri 基址拼接错（repo key 双重嵌段） | T-106 观察（E-10 面） | **入 M5（P2，FR-47，与 O-4 同族）** | 真实缺陷但低影响（files[].uri 正确，消费面少）；**修在 GA 前的理由是形状冻结**——uri 形态是兼容承诺，GA 后修正即对脚本用户 breaking；与 O-4（?permissions uri 拼接互异）合并一票收口 |
| 8 | O-106-2：maven 文档 http-blocker 注意事项 | T-106 观察（M5 文档清单） | **入 M5（P1，FR-41-AC6 收编）** | 文档中心本就是 M5 主交付；mvn ≥ 3.8 `maven-default-http-blocker` 拦纯 HTTP 仓是接入第一坑（T-106 修正账⑤ 实证），文档中心五类重构时顺手收口零边际成本 |
| 9 | graceHours:0 运维文档 N6（摘宽限 = 失去在途上传保护窗） | T-94-review N6（T-114 转登记） | **入 M5（P1，FR-41-AC6 收编）** | 同上归文档中心；governance.md 已有语义表但缺运维告警句（busy 实例 `apply + graceHours:0` 可致在途上传偶发失败）——W24 剧本是静默实例配方的暗坑必须文档化 |
| 10 | CLI `gc` apply 双遍 mark 性能 | T-114 N2（M5 备查） | **M6+** | 正确性无虞、纯性能损耗；GA 前不动 GC 路径（与 #5 同原则：mark 语义与扫描策略变更属 GC 安全面）；FR-43-AC3 采集吞吐基线数据，M6+ 有数再立项 |

主表计数：**入 M5 8 / M6+ 2 / 关闭 0**。

**速裁表：BOARD done 区其余 M5 相关登记项（一并定界，供 conductor 清账）。**

| # | 登记 | 来源 | 处置 |
|---|---|---|---|
| 1 | 版本注入（`--version` = dev） | T-16/T-45/T-106 三处 | **收编**（FR-34 本体，不再是债务） |
| 2 | T-104 E1/E2 PRD 面收口状态核验 | T-121 遗留② | **随 FR-45/FR-46 关闭**（E2 = D-104-2 词表面即 FR-45-AC4；E1 = W12c 注记随 FR-46 docker 视图定案） |
| 3 | T-74 E3~E5（M3 PRD 三处 AC 措辞） | T-121 遗留① | **关闭**（QA 已按语义等效验证、零行为影响；如后续因他因触碰 M3 PRD 再顺手，不立项） |
| 4 | M1 C17 载体同步 | T-121 遗留③ | **关闭**（T-105/T-106 已按口径执行；FR-44 使隐式目录语义升级，C17 面归 §5.6 反转表统一处置） |
| 5 | remote docker FetchError 500 | T-111 遗留（M5+） | **M6+**（随 docker remote 仓本体捆绑——remote docker 本身维持 400 不支持，该缺陷在产品面不可达） |
| 6 | FE 工程债：虚拟化窗口 / dev 代理缝 / CopyButton 终态 / ErrorBoundary / vitest 框架 / 侧栏折叠 / ⌘K 让位收尾 | T-100/T-99/T-98 各票遗留 | **M6+**（工程增强非产品面缺口；随下一轮 FE 票批量收口） |
| 7 | audit/搜索 path 服务端过滤（漂移①） | T-102/T-101 | **M6+**（后端增量票；path 过滤非 FR-29 承诺面，FE 现以客户端过滤兜底） |
| 8 | 导航取消 499/WARN（漂移⑤） | T-101 | **M6+**（观测面微优化） |
| 9 | users DELETE 端点缺口（FR-28 UI 删除入口） | T-97 NB | **M6+**（级联面广：sessions/tokens/user_groups 三清；GA 前不扩认证面——替代路径改密 + 移组） |
| 10 | T-96 NB 台账（N1~N7） | T-96 复核 | **N5 随 FR-34-AC5 windows 验证收编；其余 M6+ 微票池**（均 blocker-only 纪律下的小改） |
| 11 | web_sessions / storage 会话周期清扫接线 | architecture §11.10/§11.18 | **M6+**（量前接受，重启清扫兜底维持） |
| 12 | O-MEM1 argon2id 瞬态内存 | T-105（→T-107 已文档引导） | **关闭**（已按 m1 同形非回归定界，文档引导已落） |

---

## 7. 开放问题（需用户定案；暂行假设 v1.0 起生效）

| # | 问题 | 影响面 | 暂行假设 |
|---|---|---|---|
| Q1 | **发布渠道与产物托管**：六平台二进制 / 双变体镜像 / Chart / 离线包发布到哪里（GitHub Releases？自有/内网 registry？仅本地归档交付？）；镜像与 Chart 的 registry 命名空间与 tag 策略 | FR-34（`make release` 启用）/ FR-35（推送）/ FR-40；K2 供应链细节 | **全部产物本地构建 + checksums 归档（dist/ 与 release 目录），不推送任何公共 registry / Releases**；发布动作待本项定案后执行，且**每次对外发布前逐项经用户确认**（CLAUDE.md 安全底线 + DoD 第 7 条）。浮动 tag `<VER>` 指向 distroless 为暂行（K2） |
| Q2 | **Helm HPA 的呈现口径**：ROADMAP 字面含「HPA」，但 BinFlow 单副本约束（architecture §9）下多副本挂同 PVC 是禁止配置——交付「默认关闭 + 强制 maxReplicas=1」的 HPA 模板，还是不交付 HPA？ | FR-37 / charts 产物 | **交付模板但默认 `hpa.enabled=false`，启用时 schema 强制 `maxReplicas<=1`**：字面满足 ROADMAP（HPA 对象存在且可用），同时不诱导多副本错误配置；真水平扩展随 M6+ 对象存储。若用户倾向「不交付」，FR-37-AC1/AC2 对应断言删除（其余不变） |
| Q3 | **部署烟测环境可得性**（T-106 §2.5 降级口径定案化）：windows 运行时（G05 锁验证）、systemd 真机（G15b）、真实 K8s 集群（可选，kind 替代）——用户能否提供？不可得项是否接受「静态验证 + 容器等价 + 记录归档」的降级结论？ | FR-34-AC5 / FR-39-AC2 / §5.3 分级矩阵 / DoD 第 3 条 | **kind（或 Docker Desktop K8s）承担 K8s 面全部 P0 断言**（本地可得）；windows 与 systemd 为条件腿——有环境（用户主机 / CI runner / VM）则实测，无环境按 T-106 先例降级为：`systemd-analyze verify` / `shellcheck` / 单测 + 容器等价腿 + 报告记录，**降级腿逐条经用户确认后视同通过** |
| Q4 | **GA 版本号**：`<VER>` 取值（v1.0.0？含 build 元数据？）与 tag 策略（`v1.0.0` + `m5-done` 双 tag？） | FR-34 产物命名 / 全部 G 序列 / docs v1.x 版本目录 | **v1.0.0**（semver，无 build 元数据；git tag `v1.0.0` 与里程碑 tag `m5-done` 并存）；docs 版本目录 `v1.x` 起步 |

（无其他开放问题：Docusaurus 选型 / 中文 / embed 主交付 / 零 CGO / 统一 `/binflow` 前缀均为用户既定决策或已定 ADR，本 PRD 照录不重开。）

---

## 8. M5 验收剧本（QA 总纲）

1. **回归基线**：§5.6 反转表更新断言 → M1 C / M2 D / M3 M / M4 W 四序列 P0 复跑全绿（G34；GA 硬门槛）。
2. **发布矩阵（二进制）**：G01（产物+校验和）→ G02（版本注入）→ G03（check-size）→ G04/G04b（可跑腿）→ G05（条件：windows 锁）。
3. **发布矩阵（镜像与部署）**：G06 → G07/G07b → G08（镜像）→ G09/G10（compose 含 15 分钟口径）→ G11 → G12 → G13/G13b（Helm）→ G14（K8s）→ G15/G15b/G16（systemd 条件腿 + 校验和防线）→ G17/G17b（离线包零外网）。
4. **文档中心**：G19/G19b → G20（五类篇目）→ G18（离线搜索）→ G21（体积对账）→ G22（版本化）。
5. **债务收编**：G30a~G30e（目录实体化全链含 FE 断言）→ G31a~G31c（token 审计 + docker 隔离）→ G32（docker 视图，P1）→ G33（uri 基址族，P2）。
6. **安全审计**：G23 → G24 → G25 → G26 → 报告归档（豁免清单核验）。
7. **性能基准**：G27（1000 并发）→ G28（冷启动/内存）→ G29/G29b（基准报告 + 基线对比 + GC 吞吐记录）。
8. **GA 总矩阵**：G35（FR×AC×形态三态表 + 发布物 sha256 清单；降级腿逐条挂 Q3 确认记录）。
9. **文档**：FR-41-AC6 内容验收（tech-writer DoD：http-blocker / graceHours / prune 语义 / API 参考四硬项）。
10. **发布（DoD 第 7 条）**：Q1 定案 + 用户逐项确认 → 执行推送 / Releases。

## 9. M5 DoD（GA 口径）

1. §4 全部 P0 AC 经 qa 验证全绿；P1 除「条件腿」外全绿（P2 与条件腿延后在 BOARD 记录）；
2. §8 剧本全绿，§5.3 分级矩阵 curl+CLI / docker / K8s(kind) / Playwright(docs+FE) / mvn+npm+pip 五个 P0 成员全过；
3. **不可达形态降级报告**（windows / systemd 真机等条件腿）按 §7 Q3 口径归档，且**逐条经用户确认**；
4. tech-writer 文档中心五类齐备（安装 7 形态 / 接入 5 篇 / 管理 5 篇 / API 参考 / FAQ，含四项内容硬项——FR-41-AC6）；
5. 安全审计报告 + 性能基准报告归档：零 Critical/High（或豁免清单经用户确认）；1000 并发 / <40MB / <2s / <100MB 四条 PRODUCT 成功标准全达；
6. §6.4 主表入 M5 的 8 项债务全部收口（FR-47 为 P2 可至 m5-done 前补收）；
7. 主会话完成 `m5-done` 与 `v1.0.0` 双 tag；**GA 对外发布物（镜像 / Chart / 二进制 / 离线包 / docs-static tar）清单经用户逐项确认后方可推送任何公共渠道**（§7 Q1 定案为前置）。

---

*本 PRD v1.0 由 product-manager（T-125）依据 PRODUCT.md、ROADMAP.md M5 节、M1~M4 交付基线与 T-106 烟测先例撰写；与 ADR-0011 增补（K1）/ 镜像供应链（K2）终裁冲突时按 §5.5 流程回写修订。*
