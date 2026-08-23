# 路线图（ROADMAP）

> 由 product-manager 维护；tech-lead 据此把当前里程碑分解为 ticket。

## 当前里程碑：M8（PRD v1.0 草案，待 conductor 审）

### M0 — 团队启动（已完成）
- [x] 产品愿景 PRODUCT.md（BinFlow）
- [x] 团队工作流与看板建立

### M1 — 内核基座（已完成，`m1-done`；QA 全绿见 T-18/T-19）
目标：存储引擎 + 仓库模型 + Generic 本地仓库的最小闭环，Go 脚手架与工程化就绪。
- [x] 逆向规格：REST 表面 / 存储布局 / 配置格式 / 仓库语义（reverse-engineer → docs/reverse/）
- [x] ADR 与架构设计：模块划分、存储设计、适配器接口、部署架构（architect）
- [x] 脚手架：go module、cmd/internal 布局、Makefile、lint/test/CI（devops-engineer）
- [x] 存储引擎：checksum 寻址、去重、上传会话、原子落盘（dev-go-storage）
- [x] 元数据与仓库模型：repo 配置、node 元数据、SQLite 嵌入（dev-go-core）
- [x] Generic 本地仓库：上传/下载/删除/校验（dev-registry-adapter）
- [x] 基础认证与权限骨架：admin 用户、API Token、路径 ACL（dev-go-core）
- [x] 开发环境：docker-compose 起本地实例（devops-engineer）
- [x] QA：generic roundtrip + 存储完整性；文档：README 快速开始

### M2 — 云原生旗舰：Docker Registry v2（已完成，`m2-done`；QA 全绿见 T-43/T-44，烟测 T-45，文档 T-46）
需求基线：docs/prd/milestone-2.md（PRD v1.1；`/v2` 路由已定案（ADR-0010 根级例外））
- [x] 逆向规格：docker-registry.md（补 spec 外空白，reverse-engineer）
- [x] ADR-0010：`/v2` 挂载形态（根级例外 vs `/binflow/v2`+反代 rewrite，architect，依赖 PRD Q1 用户定案）
- [x] blob upload 协议（POST/PATCH/PUT，monolithic + chunked）
- [x] manifest schema2 / OCI 存取（by-digest / by-tag）
- [x] `/v2/_catalog`、tags/list；docker login 的 token 认证流
- [x] Helm OCI 承载（oras 客户端可用）
- [x] conformance：docker / podman / crane / skopeo / oras 全过（分级见 PRD §5.3）
- [x] M1 遗留收编：O1 断开日志定界、O3 gc 旗标、Content-Type 映射（PRD §6.4）
- [x] 部署烟测：Docker 镜像 + compose（release-engineer）

### M3 — 多生态与代理（已完成，`m3-done`；QA 全绿见 T-74/T-75/T-76，文档 T-77）
需求基线：docs/prd/milestone-3.md（PRD v1.0，T-57；docker remote pull-through 经裁决定为 M4+ 评估，见 PRD §2.2/Q4）
- [x] Maven 2：layout 解析、deploy/resolve、maven-metadata.xml、checksum 策略（FR-16/FR-17，mvn 3.9 真实客户端）
- [x] npm：publish / tarball / metadata（FR-18）；PyPI：simple index / upload（FR-19）；npm 10 + pip/twine 真实客户端
- [x] remote 仓库代理缓存（pull-through，含 SSRF 防护与上游故障降级）（FR-20 + NFR-S13）
- [x] virtual 仓库聚合与解析顺序（成员顺序优先 + 可选写路由）（FR-21）
- [x] （支撑）rclass remote/virtual 与 packageType maven/npm/pypi 启用（FR-15，M1 E-07 断言反转见 PRD §5.6）

### M4 — 控制台与治理（已完成，`m4-done`）
需求基线：docs/prd/milestone-4.md（PRD v1.0，T-85）
- [x] Web 控制台：登录（session）、仓库管理 CRUD 页、制品树浏览/上传/下载、搜索（FR-23~FR-26）
- [x] 权限模型完整实现：groups 实体与成员、授权继承（FR-27/FR-28）
- [x] 治理：审计日志查询面、GC 管理化、repo 级配额 quotaBytes（FR-29~FR-31）
- [x] 备份/恢复：export/import CLI（FR-32）
- [x] （验收面）Playwright + curl W 序列与 M1~M3 回归基线反转（FR-33）

### M5 — 发布矩阵与文档中心（GA）（已完成，`m5-done`，2026-08-21）
需求基线：docs/prd/milestone-5.md（PRD v1.1，T-125；FR-34~FR-47）
- [x] goreleaser 多平台二进制（linux/darwin/windows × amd64/arm64）+ 校验和
- [x] Docker multi-arch 镜像（distroless / alpine 双变体）
- [x] docker-compose 产物、Helm Chart（persistence/ingress/HPA）、原生 K8s 清单、systemd + 安装脚本、离线安装包
- [x] 帮助文档中心：每种部署方式的安装指南、每协议客户端接入指南、管理指南、API 参考、FAQ
- [x] 安全审计（security-auditor）+ 性能基准
- [x] M4 债务收编：ADR-0016 目录实体化（BE+FE）、token 签发/吊销审计、docker 树数据源定案与特化视图、windows 锁运行时验证（条件腿）、storage uri 基址族修正（P2）
- [x] QA 全量验收矩阵 + 发布清单就绪

### M6 — 企业就绪与生态扩展（PRD 已完成，待分票）
需求基线：docs/prd/milestone-6.md（PRD v1.0，T-148；FR-48~FR-63 六域 16 条需求，28 端点/产物，45 条 AC，九项开放问题 Q1~Q9）
- [x] PRD v1.0：S3 存储后端（FR-48~FR-53）、OIDC+LDAP 企业认证（FR-54~FR-56）、复制/联邦（FR-57~FR-60）、Prometheus 指标（FR-61）、`bf` CLI（FR-62）、Artifactory 迁移工具（FR-63）
- [ ] ADR-0018~0023（architect）待定案
- [ ] 待用户定案九项开放问题 Q1~Q9
- [ ] 待 tech-lead 分票

### M7 — 权限细化与运营硬化（已完成，`m7-done`，2026-08-23；PRD v1.2）
需求基线：docs/prd/milestone-7.md（PRD v1.2——T-214 裁决回写〔ADR-0026/0027/0028 Accepted〕+ 执行期勘误〔T-215/217/221/222/224/226〕；FR-64~FR-70 七条需求，13 条端点/产物矩阵，V01~V35 验收命令；Q1/Q2/Q3/Q5 已定案、Q6 进入执行态〔T-228 已解除并归档、T-227 dep:用户环境〕、Q4/Q7 维持暂行）
来源链：M6 §7 Q4（细粒度角色归 M7+ RBAC）+ T-209 遗留债 N6/O-2/O-1/N3/N2 + M6 §7 Q11 留的「SSO session 铸 Token 二次认证」可选加固 + ADR-0025 决策 3 条件腿（Q8/Q9）
- [x] PRD v1.1（v1.0 草案 + T-214 裁决回写）：细粒度 RBAC（FR-64~FR-66：角色闭集 + readonly_admin 全域只读 + manage 派生 + 控制台）、docker blob 上传跨重启续传 REST 化（FR-67，N6/O-2 收口）、Token 铸造 step-up 可选增强（FR-68，P2）、Q8/Q9 条件腿执行（FR-69，dep:用户环境）、技术债打包（FR-70：O-1/N3/N2/internal-auth 53 条 lint/008-009 sql 行尾）
- [x] ADR-0026~0028 已定案（Accepted，2026-08-23，T-214：角色模型与 manage 派生 / step-up 契约 / 会话 Close 语义修订牵 ADR-0006 勘误④）
- [x] Q1~Q7 已按 ADR-0026~0028 收敛，推翻出口保留（Q1 角色闭集 / Q2 read-only 边界 / Q3 干净停机语义 / Q5 step-up 形态与默认值已定案；Q4 S3 续传 / Q7 replica 隔离归属维持暂行待用户终裁；Q6 条件腿执行收口——T-228 真实 OSS 7.84.10 实腿完成并归档、V28 证据移植 docs 为 M8 债券〔FR-77〕，T-227 真实 AWS 仍 dep:用户环境，插队制不阻塞 DoD）
- [x] tech-lead 分票完成（T-211~T-228 已录板）

### M8 — 控制台对齐 Artifactory（PRD v1.0 草案，2026-08-23，待 conductor 审）
需求基线：docs/prd/milestone-8.md（PRD v1.0 草案；用户指令「前端 UI 和交互逻辑要求和 JFrog 一样」——对齐 = IA + 交互逻辑 + 操作流，自有皮肤，clean-room 行为规格制，服务端契约零改动；FR-71~FR-77 七条需求，UI 对齐矩阵 24 条〔对齐 8 / 形态不同 3 / 子集 8 / 有意差异 5〕，U01~U24 验收命令，开放问题 Q1~Q6 带暂行）
- [ ] conductor 审定 PRD v1.0（含 Q1~Q6 暂行口径）并定案基线版本（Q1 暂行 = T-228 保留的 Artifactory OSS 7.84.10）
- [ ] 前置产物：docs/reverse/ui-console.md（reverse-engineer，UI 行为规格：布局/交互流/组件清单/状态矩阵，JFrog 资产零复制）+ 前端重排 ADR（architect，ADR-0029+ 候选）+ console-ux v2.0（ux-designer）
- [ ] FR-71 双模式壳与导航树重排（Application/Administration、URL 深链、旧路由 redirect 映射，P0）
- [ ] FR-72 制品浏览器左树右详情（rclass 分组/树内过滤/checksum 拷贝/packageType 特化视图迁入，P0）
- [ ] FR-73 管理面统一表格与编辑器 + Set-Me-Up 式对话框（P0；字段集 = 既有面零增减）
- [ ] FR-74 面包屑/全局搜索/深链状态保持；FR-75 键盘可达与批量动作；FR-76 自有皮肤与设计 token（零复制合规 + 非像素判定口径）（P1）
- [ ] FR-77 M8 债券收编（T-231 percent-encode 5×2 矩阵 / B-1 bf-migrate --skip-users / UI 打磨 4 条 / CI -timeout 20m / V28 附录移植 docs / dialer 样板 13 处；「ROADMAP M7 勾账同步」已随 M8 PRD 发稿完成）
- [ ] QA：U 序列 + 8 个零学习成本剧本 + W 序列锚迁移回归 + M1~M7 P0 序列复跑（服务端零改动硬门槛）；tech-writer 控制台指南改版 + 操作路径对照表

## 里程碑完成定义（DoD）

每个里程碑视为完成，当且仅当：
1. 该里程碑所有 ticket 处于 done（通过 review + qa）
2. qa-engineer 的兼容/验收报告全绿（含真实客户端证据）
3. release-engineer 对已交付的部署方式完成烟测（M2 起）
4. tech-writer 已产出该里程碑新增能力的用户文档
5. 主会话完成 git tag（`m<N>-done`）；对外发布任何制品先经用户确认
