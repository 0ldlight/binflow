# 路线图（ROADMAP）

> 由 product-manager 维护；tech-lead 据此把当前里程碑分解为 ticket。

## 当前里程碑：M16（控制台 full-parity 收口大程——PRD **v1.1 裁定回填版**〔2026-09-02：v1.0 立项稿 + 七项终裁落章——用户四项 **Q1 引入 cron 调度域〔推翻 M15 Q5 终裁，BOARD 留痕〕/ Q3 双语可切换〔E6 翻案〕/ Q4 页码控件 / Q6 产品域进〔两程结构——M17 预立项段承载〕** + conductor 三项 Q2 E1 收紧不倒退 / Q5 路由化 / Q7 Annotate 加；两程结构：M16 = 交互 parity 主轴四批次〔树栈 P0〕+ i18n〔FR-149〕+ cron 调度域〔FR-150，ADR-0044 占位〕+ Annotate + 远端浏览 + AQL 副线，M17 预立项段 = 产品域扩张〕；**M15 收尾中**——T-423 busy 在途 + T-429 release → T-430 终验 → `m15-done` 收口窗待开〔Q1 AQL 分阶段终裁 + 未纳入项启用 + tag〕；立项稿与收尾并行纯文档零冲突）

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

### M8 — 控制台对齐 Artifactory（已完成，`m8-done`，2026-08-24；PRD v1.0）
需求基线：docs/prd/milestone-8.md（PRD v1.1 = v1.0 + 2026-08-24 §0 勘误〔U20 curl 预编码姿势 + spec 目录取现役 web/e2e/m8/〕；ADR-0029 Accepted 转正〔Q1~Q6 终裁：基线 7.84.10 实例 / 默认亮色 / redirect 全量映射 M9 移除 / Governance 保留 / 前端栈维持现役 / UI 打磨并入域票〕；UI 对齐矩阵 24 条〔对齐 8 / 形态不同 3 / 子集 8 / 有意差异 5〕；QA 终验 T-246 PASS——QA-1/QA-2 经 T-244 §8 fix-forward 收口，axe 52 扫描全零、回归 150/0、契约终审 31 文件 100% 归属三豁免票）
- [x] conductor 审定 PRD v1.0 并定案基线版本（Q1 = T-228 保留的 Artifactory OSS 7.84.10；ADR-0029 Accepted）
- [x] 前置产物：docs/reverse/console-ui.md 冻结 + ADR-0029 + console-ux v1.7（实际版本号，v2.0 口径分歧已备案收口）
- [x] FR-71 双模式壳与导航树重排 + 20 条旧路由 redirect（T-235）
- [x] FR-72 制品浏览器左树右详情（T-236：10,291 节点实测首屏 p95 734ms / 层展开 144ms）
- [x] FR-73 管理面统一表格与编辑器 + Set-Me-Up 对话框（T-237/T-240/T-241/T-242/T-243）
- [x] FR-74/75/76 导航搜索/键盘可达/自有皮肤与 token（T-239/T-244；gzip 192.6KB〔预算 54%〕、axe 双主题全零、零复制扫描三件套）
- [x] FR-77 M8 债券收编（T-231/T-233/T-249 三豁免票 + 六项复核全绿：percent-encode 5×2 / --skip-users / CI -timeout 20m / V28 附录 / dialer 样板 / UI 打磨）
- [x] QA：U01~U15 + 零学习成本剧本 8/8 + W 锚迁移回归 + M1~M7 P0 复跑全绿（T-246）；文档 T-245（console.md 改版 + artifactory-path-map 24 任务对照）；CI/CD 双线 T-247/T-248（Jenkins 三级 + dogfood 闭环）

### M9 — 服务端缺口收口与运营硬化（**完结 2026-08-25**，PRD v1.0 + ADR-0030/0031 Accepted；26 票全 done，DoD 八条全绿〔终验 PASS 含修复窗 T-274/T-275〕；`m9-done` tag 已推）
- [x] conductor 审定 PRD v1.0（含 Q1~Q6 暂行口径、Q5 replica 延后 M10 建议与 Q1 git force-push 授权门）
- [x] 前置产物：ADR-0030（SE 域端点群 wire 定案：DELETE users 语义 / enabled 回显落点 / groups includeUsers 与列表扩宽 / usage 批量 / permissions 过滤参数，含 K18~K21 校准）+ ADR-0031 候选（GC 引用原子化方案，architect）
- [x] FR-78 用户与组域端点补全：enabled 回显 / DELETE users（护栏+审计+即时失效）/ groups ?includeUsers + 列表扩宽——users 页 N+1 根治（P0，种子 A + T-237 漂移①②③）
- [x] FR-79 管理面扇出与 m-holder 可达性：/api/v1/storage/usage 批量（repos 页 ~171 请求 → ≤3）+ /api/v1/permissions?filter=manage 覆盖集过滤（m-holder 控制台编辑器可达，L2 边界卡退役）（P0，种子 A + T-241 §3.1 + T-246 QA-4）
- [x] FR-80 GC graceHours=0 并行竞态根治：apply 引用原子化零误删；e2e 解除 --workers=1 兜底，默认并发 3 连绿（P0，种子 B + T-232）
- [x] FR-81 OIDC step-up 控制台腿：mint grant + prompt=login 回跳续铸（消费 ADR-0027 既有契约，服务端零改动；mock IdP 全链 + 单次性）（P1，种子 C + T-242 §7）
- [x] FR-82 控制台与工程债包：旧路由 redirect 移除〔Q3 终裁〕/ QA-3 过滤复位 / QA-5 e2e 缺省 / assert-tokens 扩面 / packument 转义收敛 / push_npm 自查 / 锚册口径统一+死锚退役 / 顶栏搜索框升级 / 文档措辞与 deprecate 口径〔PM 裁定维持严格〕/ pass-gate 登记 / U20 勘误（P1）
- [x] FR-83 运营与发布 chores：git 历史瘦身 dry-run+手册〔**force-push 须用户单独授权**〕/ CI 多架构镜像（amd64+arm64 manifest）/ 用户实例 18080 刷新提议〔dep:用户环境，非硬 DoD〕（P1）
- [x] QA：N 序列 + 扇出/竞态量化门槛实测 + M1~M8 P0 回归（契约变更面 100% 归属 M9 豁免票审计）；tech-writer（SSO 铸 Token 路径 / npm 权限口径 / 用户管理闭环 / 旧书签失效公告）
- [x] F 池对账：28 条处置落地核对；延后 3 项（E-04 扩列 / R2 搜索契约 / R6 Tokens 页）登记入 M10+ 候选池

### M10 — Artifactory 对齐第一程：license 门控基座 + addon 注册表 + 试点包型 + 属性系统（**完结 2026-08-26**，PRD v1.2 正式版；21/21 票全 done，DoD 八条全绿〔T-297 终验 PASS：L01~L30 = 28✅+2⚠️+0❌，真实客户端八面全绿，抓获 P0×1 修复 `7b84a71`〕；`m10-done` tag 已推）
需求基线：docs/prd/milestone-10.md（PRD v1.2 正式版；FR-84~FR-91 八条需求；契约矩阵 12 条〔A 6 / C 5 / D 1〕+ 档位 × addon 解锁矩阵〔11 槽 × 3 档〕；L01~L30；Q1~Q7 随 §5.6.1 as-built 校准收口；ADR-0032/0033/0034 Accepted）
来源链：用户三指令（2026-08-25：全功能对齐 / license 分级门控 / 包型 addon 化补协议）→ docs/reverse/artifactory-full-feature-matrix.md（213 条目主矩阵）+ inv-1~4 分区目录；M9 §4.7 候选池处置（滚入 M11+）
- [x] conductor 审定 PRD（v1.0→v1.1 as-built→v1.2 终验转正；ADR-0032/0033 Accepted `e56dd8d` + ADR-0034 随 T-293 落）
- [x] 前置产物：ADR-0032（license）+ ADR-0033（addon 注册表与包型 addon 化——属性系统契约承载 = architecture §15.3）+ ADR-0034（五协议管理面 dispatchAPI + server.base_url 统一）
- [x] FR-84 license 核心包（T-279：ed25519 文档 v1/三端点/无撕裂/fail-safe）
- [x] FR-85 门控织入 + addons.disabled 熔断（T-283：D1~D7 逐行验证 + 228 并发无撕裂）
- [x] FR-86 addon 注册表 11 槽 + GET /api/v1/addons + 控制台页（T-282/T-288）
- [x] FR-87 Go 试点（T-285：3915 行，go1.26 真实全链）+ FR-88 NuGet 试点（T-287：5103 行，dotnet 8 全链；T-280 规格缺失按官方文档路径合规处置）+ Cargo 条件票实做（T-294，含 auth 裸 token 臂）
- [x] FR-89 属性系统（T-286 BE：矩阵参数单点 + node_props + 三动词；T-291 FE：首个 MUI 面 Properties Tab）
- [x] FR-90 快赢包：MPU REST 六端点（T-289；AC2 S3 kill -9 续传 descope M11 债）+ smart remote 字段子集（T-290；unused-cleanup 引擎收窄 M11）
- [x] FR-91 规格五份（T-284/T-292：conan/cargo/debian/rpm/helm 1158 行零低置信）+ tech-lead 就绪度确认（tl-fr91-ac3：5/5 可拆、23 裁决点、缺项 0 阻塞）
- [x] QA：T-297 终验 PASS（四闸门 0 deviations + 真实客户端八面 + DoD 1~7 全 PASS）；T-295 部署接线 + T-296 文档五项
- [x] M9 候选池对账：滚入 M11+（见下「M10 未纳入项」；M11 PRD §2.2 收编 C 组点名项）

### M11 — Artifactory 对齐第二程：配置域指令兑现 + 第一梯队包型批量实现 + 行为逐项对齐制度化（PRD v1.0 草案待 conductor 审，2026-08-26）
需求基线：docs/prd/milestone-11.md（PRD v1.2.2 收口回写版：FR-92~FR-102 十一条需求；契约矩阵 18 条〔A 15 / C 2 / D 1 / 待裁 0——LC-24 归位〕+ 档位 × addon 矩阵扩展 5 槽；L01~L45；开放问题 Q1~Q8 带暂行〔Q8 已终裁〕；§5.6.1 回头看 30 项全量填实）
来源链：用户指令日志最近四条（2026-08-26 11:22 MUI 迁移 T-299/T-300、11:35 认证配置前端化、11:45 存储配置独立文件化、**19:05 行为逐项对齐**——全程工作方式条款）→ conductor 种子 A~D；FR-91 五份规格 + tl-fr91-ac3 23 裁决点；M10 §2.2 滚入项 + M9 Q5 复制硬化
- [ ] conductor 审定 PRD v1.0（Q1~Q8 暂行终裁；ADR-0035/0036 立项）
- [ ] 前置产物：ADR-0035（认证配置面 REST/持久化/变更即生效/双源优先级）+ ADR-0036（存储配置独立文件与链式 schema）；规格复核票两份（auth-integration.md / config-formats.md §1——逐条附 Artifactory 行为出处）+ R-1/R-2 规格修订
- [ ] FR-92 认证配置前端化：OAuth2/LDAP/SAML 管理面 REST + 测试连接 + 变更即生效 + 控制台页组（MUI）（P0；SAML 运行时深度 Q3）
- [ ] FR-93 存储配置独立文件化：链式 provider 表达（filestore/S3/dual-write）+ 兼容窗 + 部署矩阵演进 + CD 链验证（P0）
- [ ] FR-94 存量控制台 MUI 化两批：T-299 批次一（P0）/ T-300 批次二（P1）——交互逻辑零变化四闸门
- [ ] FR-95 M10 自有裁定回头看：基线 23 项 + tl 裁决差异点（RP-2/TL-5/TL-4/HL-2 等→Q8）逐条复核——维持附出处/改回验证/分歧上 BOARD（P0，规划期完成）
- [ ] FR-96 conan 包型：v2 local 全量 + v1 全量数据面（十七端点——CN-1 终裁推翻收窄，T-308 承载，conan 1.66/2.31 双客户端活体验证；P0）+ remote/virtual（P1，T-312）
- [ ] FR-97 debian 包型：automatic local 主票（P0）+ virtual/remote（P1）+ trivial P2；GPG K-1 条件票（Q6）
- [ ] FR-98 rpm 包型：local 管线含 header 解析器（P0）+ reindex 七分支 + remote/virtual（P1）+ modules P2
- [ ] FR-99 helm 经典仓 local（P0）+ virtual/remote（P1）；HelmOCI 条件票（Q2，HL-3）
- [ ] FR-100 cargo remote pull-through + virtual（P1，dep T-294——cargo 家族三态齐装）
- [ ] FR-101 复制硬化：enableTokenAuthentication/contentSynchronisation 生效 + 属性复制同步 + replica 隔离终裁执行（P1）
- [ ] FR-102 工程债打包：S3 MPU kill -9 续传复活 / unused-cleanup 引擎 / D-8 footprint ≤100MB / D-9 测试基建 / 文档尾巴三处（P1/P2）
- [ ] QA：L01~L45 + 四包型真实客户端矩阵（conan/apt/dnf/helm）+ M1~M10 P0 回归双形态 + 两处断言反转审计；tech-writer 五类文档；Trash can 余量条件票（Q7）
- [ ] 「M10 未纳入项」对账：D 组（Cleanup-Retention 策略引擎/制品操作族/Webhook/AQL）建议 M12+，滚入「M11 未纳入项」登记

### M12 — Artifactory 对齐第三程：NuGet 面补全 + 制品生命周期域（操作族/回收站）+ 行为债收口（PRD v1.0 草案待 conductor 审，2026-08-28）
需求基线：docs/prd/milestone-12.md（PRD v1.0：FR-103~FR-113 十一条需求；契约矩阵 15 条〔A 13 / C 1 / D 1〕+ 档位矩阵增量 2 行〔trashcan 新槽 Q3 / helmoci 转正〕；L01~L35；开放问题 Q1~Q7 带暂行）
来源链：用户三项裁决 2026-08-28 07:5x（① NuGet 对齐 bundle 立项 + ③ D-A dual-write fail-open 补实现）+ 收口裁定⑤（D-8R 瘦身票 M12 承载）+ T-329 登记（D-F）+ ROADMAP「M11 未纳入项」PM 聚类（Trash can/HelmOCI/MUI 批三/回写批/遗留小票收编）+ 主轴增量选题（制品操作族——PM 定，§2.2 滚程留痕）
- [ ] conductor 审定 PRD v1.0（Q1~Q7 暂行终裁；ADR-0040〔dual-write fail-open〕立项，视 Q3 终裁 ADR-0041）
- [ ] 前置产物：nuget.md 规格票（**新建**——as-built + 增量出处双段，M10 T-293「立项随票补」口径兑现）+ repo-operations.md mini 规格 + Trash can mini 规格随票（T-330 模式）
- [ ] FR-103/104 NuGet 对齐 bundle（裁决①）：v2 路由全集（L2）+ publish 重复臂（409+canDelete 覆盖）+ v3 search 上游代理（L3-remote/L7）+ service index 动态解析（L4）——T-304 §1.1/§4.1 规格出处直取（P0）
- [ ] FR-105 制品操作族：copy/move（树级/dryRun/flat/属性与索引随行——主矩阵缺口 5；P0）+ `archive!/`/目录 zip/exploded 解包（M10 X-Explode 400 拒绝反转；P1）
- [ ] FR-106 Trash can 回收站（Q7 兑现，T-330 转正：auto-trashcan 内置仓 + trash 四元组打标 + 保留期 14 天 + restore/empty/clean + 控制台最小面；P1；与 Cleanup-Retention 策略引擎分界——策略引擎 M13+）
- [ ] FR-107 dual-write S3 停机 fail-open（裁决③：本地优先写 + 异步重试队列 + 排空对账；M6 FR-50 文面维持；dep ADR-0040；P1）
- [ ] FR-108 空载 RSS 瘦身（D-8R 收口裁定⑤：懒加载 embed，`make footprint` 门红→绿 138.7MB→≤100MB；P0）
- [ ] FR-109 HelmOCI 分发（Q2 承接 T-320 未派 + oci:// 透传 D-5 + chartsBaseUrl D-2 P2 + `_external` 落盘缓存 D-3 architect 评估 Q6；P1）
- [ ] FR-110 包型收尾小票包：conan（D-F `_/_` 坐标 delete 200 + forceConanAuthentication + Artifactory 活体互证条件腿 Q5）+ cargo（死上游 search 409 R-3/4 + `.cargo/**` DELETE 收敛）（P1/P2）
- [ ] FR-111 MUI 批次三（T-300 候选清单：RepoDetailPage/Dashboard/Profile/Placeholder/NotFound + 共享组件六件套 + combobox 统一化——交互零变化四闸门；P1）
- [ ] FR-112 架构/规格回写批（architecture §15.4/§23〔T-332 转交〕+ cargo.md §8〔T-318〕+ conan.md D1/D5/D7/D8 升置信〔T-312〕+ D-G/D-H 落位校验；P1）
- [ ] FR-113 配置与治理域遗留小票（T-290-2 socketTimeoutMillis / byHash 枚举 + web 策略键表单〔T-327R〕/ checksum-deploy token 窄域化〔T-332〕/ auth 尾巴〔T-305〕/ env 缺键拒启序〔T-325〕/ CI 专用 runner de-flake〔T-327 §7〕；P1/P2）
- [ ] QA：L01~L35 + M1~M11 P0 双形态全量回归 + 契约归属审计（m11-done..HEAD）+ **三处断言反转**（nuget v2 404→路由全集 / virtual search 缓存→上游代理 / X-Explode 400→接受）+ footprint 红→绿；tech-writer 五类增量；release 部署烟测 + UAT 链
- [ ] 条件票：NuGet symbol server（Q2 余量——mini as-built 规格随票）/ conan 活体互证（Q5 dep:用户环境）/ `_external` 落盘缓存（Q6 architect 评估）——未触发 BOARD 留痕非 DoD 缺口

### M13 — Artifactory 对齐第四程：Webhook 统一事件总线 + HelmOCI 三态齐装 + 配置旋钮与文面债收口（**`m13-done` 2026-08-31**；PRD v1.1 终版——23 票全落：21 done + 条件票 T-379/T-380 未触发留痕；T-377 终验 PASS：DoD 八条达标，L01~L24 全绿）
需求基线：docs/prd/milestone-13.md（PRD v1.0：FR-114~FR-122 九条需求；契约矩阵 11 条〔A 10 / 待裁 1——LC-56 D-10〕+ 档位矩阵增量 1 行〔webhook 第 19 槽 Q4〕；L01~L24；开放问题 Q1~Q7 带暂行）
来源链：ROADMAP「M12 未纳入项」（票级遗留聚类 / P2 登记维持 / 运维尾巴）+ **PM 主轴选题**（Webhook 事件总线——主矩阵缺口 6，inv-4 判定「可整体平移」、官方文档为唯一行为基准〔反编译集合无该 addon〕；§2.2 滚程留痕：AQL/Build-info 滚 M14+，HA 前置 = PRODUCT.md 修订解禁未发生）+ T-356 终验维持登记四项（D-10/flat/L31/flake）+ T-348/T-340/T-313/T-342 取证链 + Sprint 942 收官笔头批 `346485e`
- [x] conductor 审定 PRD v1.0（Q1~Q7 暂行终裁；ADR-0041〔webhook 事件总线：outbox/投递/SSRF〕立项，视 Q 裁定 ADR-0042〔D-F2 存量迁移〕）
- [x] 前置产物：webhook.md 规格票（**新建**——官方文档逐端点出处 + 36 事件清单 + BinFlow 触发源覆盖界 + inv-4 §I/§K 锚点补白）+ ADR-0041 + helm.md 增量段（chartsBaseUrl/_external as-built 细化，随票）
- [x] FR-114/115 Webhook 主轴：订阅 CRUD+test REST + 36 事件注册与事件源织入（artifact/artifactProperty/docker 域 P0）+ outbox 投递引擎（重试/死信/签名/SSRF）+ 控制台最小面 + 真实消费者验收（dogfood Jenkins 条件腿）（P0；FE 面 P1）
- [x] FR-116 HelmOCI remote pull-through + virtual（D-5 翻转点——docker /v2 面首个 remote 数据链，M3 Q4 缓议翻转；remote P0 / virtual P1）
- [x] FR-117 chartsBaseUrl 分体基址（T-313 D-2）+ `_external` 落盘缓存（D-3——T-342 评估结论兑现，引擎 absolute-URL 缝票）（P1）
- [x] FR-118 旋钮两枚：folderDownloadConfig 六字段 + trashcan.retention_days（缝已备——T-356 L14/L17 断言开关化）（P1）
- [x] FR-119 conan D8 整树删翻转（T-348 双证）+ D-F2 files 通道布局迁移（T-340 §4，dep ADR-0042）（P1）
- [x] FR-120 文面裁定包：D-10 同字节幂等上 BOARD 终裁（P0 裁定动作）+ flat 措辞回写 + fail-open AC2 加注（T-356 观察⑨；ADR-0040 零修改）（P2 落笔）
- [x] FR-121 de-flake：raceEnabled escape（TestBigTreeCopyNo5xx 预算臂 + deb 满载族）+ CI e2e job 三连权威化——race 全树一次绿，不再接受隔离复跑辩护（P1）
- [x] FR-122 运维尾巴：trash 树常驻节点（console-m8 推翻条款——先改册后实现）+ 侧栏清单 15 对齐 + npm 尾斜杠接入注记（P1/P2）
- [x] QA：L01~L24 + M1~M12 P0 双形态全量回归 + 契约归属审计（m12-done..HEAD）+ **断言反转两处**（conan D8 latest 链→整树删 / folderDownload 恒关→旋钮化）+ 布局对齐一处（D-F2）；tech-writer 增量（webhook 指南/HelmOCI remote/旋钮/收官清扫）；release 烟测 + **UAT 随里程碑 PR 首跑**（M12 T-355 未执行教训）
- [x] 条件票：NuGet symbol server（Q2 余量承接）/ docker remote 顺车（Q5，K54 判定）/ D-10 翻转（Q3）/ deb bz2 推翻（Q7）——未触发 BOARD 留痕非 DoD 缺口
- [x] 「M12 未纳入项」对账：收口时建「M13 未纳入项」段（DoD#7 字面；M14+ 主轴候选第一顺位 = AQL + 老搜索专程）

### M13 未纳入项（滚入 M14+ 候选池；2026-08-31 T-377 终验归档 + 各票登记汇总；DoD#7 对账）

- **主轴候选（第一顺位 = UI-parity 里程碑——用户指令 2026-08-30「交互体验与 JFrog Artifactory 完全一致（弹窗/抽屉）+ 协议 logo + 自设计品牌 logo」）**：docs/design/console-artifactory-parity.md 差距矩阵 + UX-1 品牌资产（logo 三候选/图标 30 枚）已备；活体核验 V1~V8 与 FE 接线票归 M14。
- **实现类候选**：docker remote 首航（T-380——K54 判定条件已满足，收口波未派发转 M14）/ NuGet symbol server（T-379 余量承接）/ npm legacy login 服务端小票（T-374 L1——T-77 O-4 实证）/ 仓库表 hover 对比度 FE 小票（T-374 L2，4.41:1）/ Replay + outbox 行级 REST 面（T-364 §5-③ + T-366 §4-2）/ remote 缓存树高并发 busy 重试预算（T-377 D1——24 路 0.27% 边角）/ helm uninstall PVC keep（T-376）/ virtual 成员同型全包型推广（T-367）/ v3-flat 直推面 403-vs-409 规格补锚（T-378）/ 启动日志措辞一行（T-376）/ 3xx 终态 V4 活体验证（T-364）/ disable 快照契约翻转若需（T-364）。
- **编排注记**：playwright 全量纯净 community 实例前提 README 一行（T-374 L3——T-377 再次实证：pro 宿主 132 红）；dind containerd snapshotter 调试建议 `--feature containerd-snapshotter=false`（T-377 D2 环境注记）。
- **沿 M11/M12 候选池续滚**：HA 本体（Q1——PRODUCT.md 修订解禁未发生）/ AQL + 老搜索 / Build-info 域 / Go 深化 / Terraform / GitLFS / 制品 license 识别 / 冷存储分层 / AI/ML 包型扩展 / license 公钥 config 覆盖。

### M14 — UI-parity 里程碑：前端交互体验与 Artifactory 完全对齐 + 协议 logo + 自设计品牌 logo + docker remote 首航（**`m14-done` 2026-09-01**；PRD v1.2 终版——24 票全落：22 done〔含 T-406/T-406b P0 热修〕+ 条件票 T-401 已触发执行 + T-403 未触发留痕 + T-402 两段/T-404/T-405 增补；T-400 终验 PASS：五 AC 全绿，L16 终评落档 parity v1.3）
需求基线：docs/prd/milestone-14.md（PRD v1.0 草案：FR-123~FR-130 八条需求；契约矩阵 10 条〔A 7 / C 2 / 待裁 1——LC-57~LC-66 续接〕+ 档位矩阵增量 0 行〔19 槽维持〕；L01~L18；开放问题 Q1~Q7 带暂行；E1~E7 豁免常设条款）
来源链：用户指令 2026-08-30 三指令（① 交互体验与 Artifactory 完全一致〔弹窗/抽屉〕② 协议 logo ③ 自设计品牌 logo——M14 主轴定音，**AQL 专程让位滚 M15 第一顺位**）+ UX-1 三交付（console-artifactory-parity.md §7 差距矩阵/§8 V1~V8/§9 E1~E7/§10 批次 + brand/logo 三候选 + brand/package-icons 30 枚）+ ROADMAP「M13 未纳入项」候选池 PM 收编（FE 类入主轴；docker remote〔T-380 K54 条件已满足〕/npm legacy login/PVC keep/v3-flat 补锚入波；Replay REST/D1 busy/成员同型滚 M15+——理由 PRD §2.2 留痕）
- [ ] conductor 审定 PRD v1.0（Q1~Q7 暂行终裁——活体核验源/logo 圈定窗/决策项 A·B·C 前置；无新 ADR——服务端两面均既有域增量）
- [ ] 前置产物：活体核验票（V1~V8 回写 + parity 置信度升级 + 差距矩阵复核基线）B1 首波（降级路径 Q1）；wordmark/npm/go 转 path 定案随票（K56）
- [ ] FR-124 批 1 形态对齐 P0：D1 Set Me Up 抽屉化（smu-* 锚族冻结）+ M1 建仓单 Dialog 化（深链兼容，决策项 A）+ M3 用户/组创建 modal（决策项 B）+ L2 行内 ⋮（删除不进菜单 E1）
- [ ] FR-125 批 2 补缺 P1/P2：Tokens 页真身（创建 modal + 一次性明文 + 吊销确认，零新端点）+ L1 列选器/刷新 + F2 空态插画槽 + N2 侧栏图标槽（V5 条件）
- [ ] FR-126 品牌 logo 转正 P0：候选 1 六用例接线（favicon.ico/PWA/登录页/侧栏顶/文档站〔GitHub 远期 P2〕）+ wordmark 转 path（Q2 圈定窗 B2 前）
- [ ] FR-127 包型图标接线 P1：30 枚四消费点（pkg-grid/smu-grid/类型列/addon 矩阵）+ npm/go 转 path 前置 + 门控/暗底纪律
- [ ] FR-128 FE 债 P1/P2：仓库表 hover 对比度 ≥4.5:1 + playwright 纯净实例前提/dind snapshotter README 注记
- [ ] FR-129 docker remote 首航 P1（T-380 条件已满足——K54 判定成立；helmoci 缝复用；docker 三态齐装收口）
- [ ] FR-130 服务端小票包 P1/P2：npm legacy login（T-77 O-4 实证）+ helm uninstall PVC keep + 启动日志措辞 + v3-flat 403-vs-409 规格补锚（Q6 条件翻转）
- [ ] QA：L01~L18 + 差距矩阵逐格终评（17×8 覆盖率 100%）+ E1~E7 豁免复核 + axe 双主题维持 + M1~M13 P0 双形态全量回归 + FE 票服务端 diff=0；tech-writer 增量（console parity 化 + 品牌注记 + docker remote 接入 + npm login + helm keep）；release 烟测 + UAT 随里程碑 PR
- [ ] 条件票：NuGet symbol server（余量四承）/ v3-flat 翻转（Q6）/ N2 图标槽（V5）/ D3 立项（Q7）——未触发 BOARD 留痕非 DoD 缺口
- [x] 「M13 未纳入项」对账：收口时建「M14 未纳入项」段（DoD#7 字面；M15+ 主轴候选第一顺位 = AQL + 老搜索专程——UI-parity 让位留痕 + Replay/D1 busy/成员同型滚程登记；**备稿已就绪——T-395 v1.1，见下段，收口窗启用**）→ **已启用**（m14-done 收口笔：下段去「备稿」帽，T-406 遗留〔virtual 聚合 / remote 远端浏览〕并入）

### M14 未纳入项（滚入 M15+ 候选池；T-395 2026-08-31 起草，**m14-done 收口笔启用**并按 as-built 修订勾稽——T-406 遗留两项并入；DoD#7 对账）

> **状态：已启用（m14-done 收口笔 2026-09-01）**——PM 预备文本（沿 M13 R5 先例）经 conductor 终验后启用；「票级遗留」以下以 2026-08-31 23:3x 票据状态为基线起草，收口笔逐条复核：滚程三项/三出口候选维持成立，主轴 AQL 第一顺位维持；**T-406 收口笔并入两项**（见下）。

- **T-406 遗留（m14-done 并入）**：virtual 仓聚合浏览（FR-21-AC8 P2——成员并集 children / Artifactory 同形态；M14 以成员感知空态收口，读取面经成员解析已可用）+ remote 仓远端浏览（Artifactory remote browsing——M14 落缓存浏览即停，不回源列举；回源列举系上游目录枚举语义决策，需 per-协议探测上游能力）。

- **主轴候选（M15 第一顺位 = AQL + 老搜索专程）**：M13 PRD 原列 M14 第一顺位，被 2026-08-30 UI-parity 主轴指令让位（用户动作，非 PM 裁量）；M14 PRD §2.2 留痕在案；体量专程级判断不变（查询语言/执行引擎/分页）。
- **滚程项（M14 PRD §2.2 判定维持）**：Replay + outbox 行级 REST 面（M15+ webhook 域二程——运营增强非协议兼容面，死信重放机制已备翻转面小不返工）；remote 缓存树高并发 busy 重试预算（M15+ 后端硬化——门内 8 路零 5xx 已达承诺，24 路 0.27% SQLITE_BUSY 可重试边角，busy_timeout 牵 SQLite 写路径需专项回归）；virtual 成员同型全包型推广（M15+ 后端对齐程——需 13 包型 × 三 rclass 全量回归矩阵）。
- **执行期改判与出口登记（收口对账必读）**：
  - **L2 行内 ⋮ 菜单化撤销**（V4 实测 Artifactory 7.84 行尾无 ⋮——icon-trash 直删；T-385 票面撤销不派发）：BinFlow 现行行交互按 E1 家族有意偏离登记（V8 实证加码——BinFlow 输入 key 确认更严）；「复制 key / Set Me Up 行内快捷」降为**可选自有增强候选票**（非 parity 面——T-381 §V4 建议）。
  - **E7 toast 锚位再议出口**（V3 实证 Artifactory 顶部居中单条 ~2-3s vs BinFlow 右下堆叠）：默认不改（既有有意设计）；用户要一致才开一行级微调票——候选登记。
  - **Q4 Tokens 字段集对照残留**（V6c 降级——OSS 无 admin 集中 token 面 + profile 密码门）：T-386 以暂行字段集收口；商业版/云实例活体源可得时补核验——候选登记。
- **票级遗留（各票报告在档，入 M15+ 候选池登记）**：
  - FE/测试基建：`.member-pop` hover 对比度同配方一行（T-391 L-a——virtual Tab 浮层入口，亮暗双修配方现成）；列选器推广 users/groups/search（T-387——共享层 columnPrefs.ts 已就绪）；m9 N01 请求预算 flake spec 级竞态（T-384——一行测试基建票）；m9 seed 并行互撞（T-391 L-b——CI workers=2 理论可复现）；assert-tokens 属性选择器豁免规则单独立票（T-390）；e2e 破坏性动作默认禁点 + 共享 fixture 快照前置成文（T-381 L06——INC-1 教训）；pkill 按端口精确杀纪律成文（T-382/T-384/T-389 三起误伤留验实例教训）。
  - 规格/册回写：parity 册 M1 行「定案 440px」升级 + M3 行「MUI Paper」代差描述 + ux 共笔签认路由（T-383/T-384/T-381 L05——归 ux-designer）；package-icons helm/nuget 暗底提亮超 +10% 量级拍板（T-390——纯蓝通道物理下限）；`docs/reverse/README.md` 补 npm.md 行（T-393——conductor 顺手或顺车票）。
  - 服务端/运维：Prometheus remote 族双计数源归并 RE-11（T-392——P2 占位）；httpapi 无 WriteTimeout 既有姿态（T-392——归 architect 裁）；whoami 403 读面 ACL 语义（npm E403 误导文案——T-394，conductor 裁维持或移出租路径 ACL 小票）；npm≥11 legacy 漂移（T-394——随升级窗）；npm login 缺字段 400-vs-401 逐字（T-394——DE 文案另裁）；DE whoami 匿名错误体形态（T-393——t226 活体顺带）；dind PMTU 环境注记（T-392）；docker virtual 开矩阵（T-397——conductor 裁）；by-digest 强刷（T-397——产品决策候选）；migrate-artifactory.md 措辞陈旧（T-397）；t381 事故残留清理时机（VM `~/t381-incident-recovery/` 15MB 取证快照 + `t381-ui-probe` 空仓——conductor 决定；REST 删被 OSS license 门挡）。
- **条件票出口（收口时留痕）**：T-401 v3-flat 翻转（Q6 终裁=对齐才触发；否则 D 层差异行留痕——K59 锚定 409 vs as-built 403 材料在案，**Q6 为收口窗必裁项**）；NuGet symbol server 余量四承（**原 T-402 号**——与 replication 增补票号冲突，让号/改号归 conductor 裁定）。
- **范围增补留痕（非 PM 裁量——用户指令）**：**T-402 replication 交互对齐**（用户指令 2026-08-31 23:2x「replication 的交互要与 Artifactory 一致」——BOARD intake ② 在档；两段票：a 锚定段 t226 差集法只读探测〔在途〕/ b 实现段候 FE lane 空位）：已入 M14 P0 插空（PRD v1.1 §4.9 FR-131 / LC-67 / L19）；**若实现段未随收口窗完成 → 滚 M15 首票（conductor 裁）**。
- **沿 M13 候选池续滚**：HA 本体（PRODUCT.md「明确不做」修订解禁前置未发生）/ Xray 集成面 / Build-info 域 / Go 深化（sumdb 代理 + external 重定向）/ Terraform / GitLFS / 制品 license 识别 / 冷存储分层 / AI-ML 包型扩展 / license 公钥 config 覆盖（T-293 终裁③——走新 ADR）。



### M15 — 搜索基建专程（AQL 首程）：AQL 查询语言与执行引擎 + 老搜索首批端点 + virtual 聚合浏览收口 + 复制包 B 首批（PRD v1.1 收口笔——2026-09-02 T-427；v1.0 已转正 2026-09-01 09:1x〔Q5 不引入 cron / Q6 docker virtual 开禁即裁〕）
需求基线：docs/prd/milestone-15.md（PRD v1.1 收口笔：FR-132~FR-140 九条需求；契约矩阵 12 条终版〔**A 10 / C 2 / 待裁 0**——LC-76 归 A（Q4 终裁出口 C 批 1：helm+deb+rpm 可选档）〕+ 档位矩阵增量 0 行〔19 槽维持——搜索域按 Artifactory oss 档映射为核心能力无槽〕+ **搜索域端点全景归属表**〔14 端点族 + 2 外挂 + AQL 全量对账，as-built 逐行〕；L20~L34；开放问题 Q1~Q7 v1.1 归位——终裁落章 3〔Q4/Q5/Q6〕+ 规格回写 2〔Q2 K63 定案 1000/4/10s/429+Retry-After/408、Q3 400 维持〕+ 维持暂行 2〔**Q1 AQL 分阶段边界——收口窗必裁，材料已齐** / Q7 by-digest 登记〕；K62~K66 全量回填实装值）
来源链：主矩阵十大缺口 2（查询面——「企业日常操作入口」）+ **AQL 两度让位留痕**（M13 §2.2 主轴让位 webhook → M14 §2.2 主轴让位 UI-parity〔用户指令〕——两次均非 PM 裁量，本程兑现不再旁移）+ ROADMAP「M14 未纳入项」候选池 PM 收编（T-406 遗留两项 / 复制包 B 首批〔T-402a ②段登记〕/ mint 500→400〔T-386 契约漂移〕/ busy 预算改判收编〔BE 里程碑回归窗一次覆盖〕/ L2 自有增强 / 测试基建与文面债包；**Replay REST 与成员同型滚 M16 维持——理由 PRD §2.2 留痕**）+ inv-1 §E / inv-2 §1.C 反编译锚点（AQL 端点/九域/QRL + 14 老搜索端点枚举）+ AddonType `oss` 档（t226 活体核验腿可用——无 replication 式 entitlement 锁）+ M4 两笔欠账（SR-03 gavc「still closed」/ §5.5 K2 匹配语义待校准）+ M10 node_props 预留索引兑现（architecture §15.3.2）+ M14 as-built（T-405 replication PUT 最小面——包 B 在其上叠加零返工）
- [ ] conductor 审定 PRD v1.0（Q1~Q7 暂行终裁——Q1 分阶段边界 / Q4 远端浏览三出口为收口窗必裁；ADR-0043〔AQL 引擎：语言子集文法 / AST→参数化 SQL / ACL 织入 / 资源治理门 / WriteTimeout 交互〕立项）→ **已审定**（2026-09-01 09:1x：v1.0 转正 + Q5/Q6 即裁 + Q1/Q3 暂行确认；ADR-0043 Accepted 同日 T-408——含三笔勘误；Q4 已随 T-425 终裁 2026-09-02，余 Q1 收口窗）
- [ ] 前置产物：aql.md 规格票（**新建**——官方文档为唯一行为基准〔webhook.md 先例〕+ inv 补白 + t226 活体核验腿 + 口径归一〔14-vs-13 勘误 + 子集边界表 + M4 K2 校准 + 基座映射表〕）+ ADR-0043 + replication.md 增量段（包 B 双源材料——T-402a R 系在案）
- [ ] FR-132/133/134 AQL 主线（P0）：规格锚 → 语言与执行引擎（item+property 域 + include/sort/offset/limit + ACL 同源过滤〔T-92 血统——越权零泄漏探针硬 AC〕+ 资源治理门简化版 K63 + `POST /api/search/aql?compact`）→ 老搜索首批 gavc/prop/pattern（同引擎——SR-03/SR-04 断言反转①；dates/creation 余量条件 K65；K64 匹配语义校准落笔）
- [ ] FR-135 搜索面 FE（P1）：搜索页 AQL 模式（编辑器 + 语法错内联，零新端点）+ 列选器三页推广（users/groups/search）+ member-pop hover
- [ ] FR-136 virtual 聚合浏览（P1）：FR-21-AC8 兑现——children 成员并集 + t226 形态对照 + tree-empty-virtual 断言反转②（锚册留痕）
- [ ] FR-137 remote 远端浏览**评估票**（P2）：per-协议上游枚举能力矩阵（13 包型逐行）+ Q4 三出口材料——不设实现断言
- [ ] FR-138 复制包 B 首批（P1/P2）：Replicate Now（executereplicationnow 对位 + outbox 模式复用零重构）/ Test 连接 / blockPush·blockPull 全局封锁（UI-API 不受门）；cron 双轨 Q5 不裁不建
- [ ] FR-139 契约与硬化小包（P1/P2）：mint unknown username 500→400（auth-model 3.1 归位——断言反转③）+ remote 缓存树 busy 重试预算（24 路 0.27% 边角清零 + SQLite 写路径专项回归）
- [ ] FR-140 债包（P2）：L2 行内快捷（复制 key/Set Me Up——LC-79 C 层自有增强，E1 不倒退）+ 测试基建纪律成文（INC-1 教训/pkill/assert-tokens）+ 文面回写簇（reverse README/parity 册两行/package-icons 暗底拍板/migrate 措辞）
- [ ] QA：L20~L34 + 断言反转三处归属审计 + t226 AQL 活体对拍 + §5.7 全景表逐行核对 + M1~M14 P0 双形态全量回归；tech-writer（AQL 用户指南 + 搜索 API 参考 + 浏览/复制增量 + FAQ 子集边界）；release 烟测 + UAT 随里程碑 PR
- [ ] 条件票：dates/creation 顺车（K65）/ 远端浏览实现段（Q4）/ docker virtual 矩阵开禁（Q6）/ by-digest 强刷（Q7）/ NuGet symbol server 余量五承（T-403 延续）/ Tokens 字段集补核验（候商业版/云活体源）——未触发 BOARD 留痕非 DoD 缺口
- [ ] 「M14 未纳入项」对账：收口时建「M15 未纳入项」段（DoD#7 字面；备稿沿 T-395 先例收口窗启用——M16+ 候选第一顺位 = AQL 高级面〔statistics/usage 域 + QRL 全量 + UI 搜索族，dep Q1 终裁〕）→ **备稿已就绪——T-427 v1.1（见下段，收口窗启用并按届时 as-built 修订勾稽；intake ⑤ M16 全翻案语境已逐条衔接标注——启用时与全量审计产出双源对账）**

### M15 未纳入项（滚入 M16+ 候选池；T-427 2026-09-02 起草〔备稿〕，m15-done 收口窗启用并按届时 as-built 修订勾稽；DoD#7 对账）

> **状态：备稿（PM 预备文本——沿 T-395/M14 先例）**，conductor 收口窗启用；以 2026-09-02 04:3x 票据状态（M15 18/25 + T-432①）为基线起草，启用时逐条复核——在途票（T-421/T-423/T-424/T-428/T-429/T-430/T-431）的遗留以届时报告为准增删。
>
> **M16 语境总注（用户指令 intake ⑤ 2026-09-02 00:1x——全量翻案语境，本段最高位前提）**：①「制品树展示仍与 Artifactory 严重偏离」（用户第三次 UI 加码）② M16 须完全检查整个前端、对齐 Artifactory 所有内容 ③ **PRODUCT.md 不做清单全面翻案（除 Xray）**——含 E1~E7 豁免族 / parity §9 永不建 / PRD Non-goals / 滚程项；**与 conductor 先前裁定冲突处（如 Q5 cron 双轨已裁不引入）立项稿列冲突点交用户确认，而非默默翻转**。**本段各条目据此标注〔M16 吸收预期〕**：大部分滚程/候选会被 M16 全量审计 workflow（已发起——不做清单三源枚举 + t226 逐页活体对照，树为最高优先）吸收重排；本段职责 = 候选池登记**不丢项**，M16 立项时与本段 + 审计产出**双源对账、勿重复立项**。

- **M16 主轴候选第一顺位 = AQL 高级面**（dep Q1 终裁——材料已齐 PRD v1.1 §7：M15 as-built 六环全落 + M16 边界核对维持）：statistics/usage 域〔dep per-node 下载计数基建——aql.md 注：stats 字段 t226 OSS 活体可用，排 M16 系自有基建缺失非 parity 档位〕+ QRL 全量（`v1/system/query_rate_limiter` 三态 + 指标 job——M15 已落 C 层简化门 LC-73）+ UI 搜索族（artifactsearch/stashResults/packagesSearch/syntax-search）+ 剩余老搜索（dates/creation〔T-417 判 M16：404 空集族 + 瘦行 + epoch-ms〕/ badChecksum / versions / latestVersion / usage）。〔M16 吸收预期：高——UI 搜索族与 intake ② 全前端对齐合流；AQL 高级面是否独立成程归 M16 立项稿〕
- **Q 实现段（Q4 终裁 2026-09-02 04:2x——出口 C 批 1，~3 票 M16 登记）**：helm classic + deb + rpm 远端浏览**可选档**（`listRemoteFolderItems` 对位语义，默认 false 维持缓存浏览 = T-406 as-built 同形态——不欠默认 parity）；批 2（条件，批 1 验证用户真实使用后裁）= docker/helmoci tags 层（drill-down 定位，catalog 根不可达须明示）+ maven metadata 版本层；牵连面：T-412 `listVirtual`「remote 成员仅缓存行」口径扩面（repo-semantics §8.5）+ 建议规格落盘 docs/reverse/remote-browsing.md（T-425 §1/§2 可直接成稿——归 conductor 编排）+ 实现票带 t226 UI 建仓（Pro 语义对照）活体补拍腿。〔M16 吸收预期：中高——「树展示偏离」主诉即树面，remote 可选档大概率并入 M16 前端主轴的 BE 腿〕
- **明确不做面（Q4 终裁 + 协议物理边界——翻案语境下的核对位）**：generic 与 maven 目录层的 HTML 目录抓取（上游换皮即碎 + 官方未写算法——中置信无锚）；npm/pypi 根 / goproxy / cargo / conan 根树（协议无根级枚举 API——**物理不可行，非产品不做清单项，intake ⑤ 翻案不覆盖**；做成即假树）；docker tags 远端浏览首采被否（Artifactory 官方设置面未开放该型——做即超 parity L2；M16 若用户点名须如实标注超 parity）。
- **滚程项（M15 PRD §2.2 判定维持 + v1.1 终裁落章）**：Replay + outbox 行级 REST 面（webhook 域二程——机制已备翻转面小）；virtual 成员同型全包型推广（T-367——13 包型 × 三 rclass 全量回归矩阵）；**cron 双轨已终裁不引入（Q5，2026-09-01）——M16 复制域二程不列实现项；intake ⑤ 翻案语境下如重开须列冲突点交用户确认**。〔M16 吸收预期：高——intake ③ 明示滚程项在翻案面内〕
- **超 parity / 错位登记（T-425 发现）**：PM v1.0 暂行倾向「helm/docker 先行」被官方设置面证伪（docker/helmoci 系 Artifactory 未开放远端浏览设置的两型——教训：子集排序须先对官方支持面，非协议廉价度单维）；Opkg 型（Artifactory 官方设置面五型之一，BinFlow 无此包型——远期包型扩展候选池续滚）。
- **条件票出口（M15 未触发/未插空，滚 M16+）**：NuGet symbol server 余量**六承**（T-403 延续——M15 未触发）；Tokens 字段集补核验（候商业版/云活体源）；by-digest 拉取强刷（Q7 维持 as-built——TTL 统一，材料在案）；E7 toast 锚位（候用户信号维持不改）；**T-431 docker virtual 开禁已裁开（波外插空非 DoD——m15-done 前未插空则滚 M16 首票）**；dates/creation（K65 判 M16，见上）。
- **票级遗留（各票报告在档，入 M16+ 候选池登记——以 m15-done 收口笔复核为准）**：
  - 契约/规格：aql.md 待验证 V-a~V-h 八项（429 活体 / 6000 现值 / property 数据腿 / unknown 脱敏现值 / `$eqic` 族 / virtual 对拍 / pattern 空集——各有归位路径在册）；**相对时间 `"1d"` 须空格 vs 官方后缀表字面冲突**（T-426 as-built 分歧登记——翻转点 lexer.go parsePeriod）；用户 `.limit()` 也置截断标记（T-426 入册——range.total 语义附注）；compact 非空行体形态（T-415 中置信——活体实 415）；K64 ASCII 折叠局限（SQLite LOWER 无 Unicode 表——T-417）。
  - FE/浏览：`GET /api/repositories/<virtual>` echo 原始 config blob——成员级联删除后仍列已删成员（T-416 软注记——httpapi echo 改造候选小票）；httpapi 无斜杠先探 file 面在 virtual 含 remote 成员时走一次上游（T-412/T-406 as-built 同形）；AQL 排序字段须在输出集（T-419——title 提示已给）；429 Retry-After 数值不上 UI（ApiError 无响应头——锚册注记）；`t419/t404:337` e2e 真栈翻转断言腿（归 T-421 复跑）。
  - 引擎/运维：大仓 limit 化全枚举缝（T-420——T-423 同族）；TestEngineMixedLoad 慢机 429 介入测试健壮性（D-413-2——归 T-421 复验顺腿修或转 T-433）；真门并发饱和不可确定性（T-415——stub 同口径）；审计实例 18091 未复活待查 + :8099/:8174 跨票残留实例 hazard（环境项）；t381 事故残留清理（conductor 决定项维持——REST 删被 OSS license 门挡）；37 条 eslint ratchet warnings 清单（T-432① 底稿——清理候选票）；T-432 段二（vite 8 Rolldown + plugin-react 6）与 MUI v7→v9（触全页面，最后位）排队独立票面。
- **沿 M14 候选池续滚（intake ⑤ 翻案语境逐条重标）**：**HA 本体**（PRODUCT.md「明确不做」——**翻案候选**：须 PRODUCT.md 修订解禁 + 单列专程 + 用户确认）；**Xray 集成面（intake ⑤ 明示排除——唯一维持不做）**；Build-info 域（AQL build 系入口前置——**翻案候选**）；制品 license 识别（AQL license 搜索前置——**翻案候选**）；Go 深化（sumdb 代理 + external 重定向）/ Terraform / GitLFS / 冷存储分层 / AI-ML 包型扩展 / license 公钥 config 覆盖（T-293 终裁③——走新 ADR）——均候 M16 立项稿与全量审计产出对账后逐条定去留。

### M16 — 控制台 full-parity 收口大程：制品树栈（P0）+ 仓库表单栈 + 详情/搜索栈 + 安全/shell 栈 + i18n 双语 + cron 调度域 + 远端浏览可选档 + AQL 高级面副线（PRD v1.1 裁定回填版——2026-09-02 v1.0 立项 + 同日七项终裁落章〔用户四项 Q1/Q3/Q4/Q6 + conductor 三项 Q2/Q5/Q7〕；两程结构之前程，产品域扩张落 M17 预立项段）
需求基线：docs/prd/milestone-16.md（PRD v1.1：FR-141~FR-150 十条需求〔**新增 FR-149 i18n 双语可切换〔Q3〕/ FR-150 cron 调度域〔Q1——ADR-0044 占位〕**〕；契约矩阵 LC-80~LC-98 估 19 条〔**A 17 / C 2 / 待裁 0**——v1.1 归位 LC-88 Annotate〔Q7 加〕/ LC-91 cron〔Q1 引入〕+ 新增 LC-97 i18n〔C〕/ LC-98 页码控件〔A〕〕；L35~L48；K67~K72；**§0 范围定界置于最前——v1.1 两程结构终裁落章**：M16 = 控制台交互 parity 收口〔B 偏差 47 项 + A2/A4/A5/A6/A7 部分翻案 + i18n + cron 域〕，A1 产品域扩张已裁「全部进」但落 M17 预立项段〔须 PRODUCT.md 修订 + ADR 群，候 M16 收口后 PM 正式立项〕）
来源链：用户指令 intake ⑤（2026-09-02 00:1x 三指令：① 制品树展示仍与 Artifactory 严重偏离〔第三次 UI 加码〕② 完全检查整个前端对齐 Artifactory 所有内容 ③ 除 Xray 外不做清单全面翻案——冲突处列冲突点交用户确认）+ **M16 全量审计 workflow 产出** reports/m16-parity-audit-material.md（2026-09-02 10:0x 收官——A 翻案 8 域 66 项〔xray_tied 已剔〕/ B 活体偏差 47 项〔logic 11 · visual 17 · minor 19，t226 逐页带代码行锚〕/ C 冲突 13 项 / D 骨架建议）+ ROADMAP「M15 未纳入项」（T-427 备稿〔M16 吸收预期〕标注——双源对账勿重复立项）+ M15 Q4/Q6 终裁承接（远端浏览出口 C 批 1 / docker virtual 开禁）
- [ ] conductor 审定 PRD（v1.0→**v1.1 已裁定回填**——必答七项 + Q13 全部落章，「待裁」零滞留）；**ADR-0044（cron 调度域）立项 + Annotate 迁移会签**（architect 前置）
- [ ] 前置产物 FR-141：Q 终裁回写 + parity 册 v1.2（E5 前提修正 / E1 范围修正 / stay-out 登记 / 翻案双留痕）+ reverse §3.2 facet 回填 + 规格增量段（aql.md statistics·usage·QRL·dates / remote-browsing.md〔T-425 成稿〕/ 树头工具带锚）
- [ ] FR-142 批次① 制品树栈（**P0 先行**——用户主诉）：文件叶子进树（「（空）」占位退役）/ 选择≠展开 / 页签进 URL + 文件路径段化（`?focus=` 退役 + 兼容映射）/ 树头工具带（facet/rclass 组/Sort-by/Compacted/My Favorites）+ children 表收窄〔Q2/Q9〕（B-1.1~1.4）
- [ ] FR-143 批次② 仓库表单栈：Basic|Advanced|Replications 三段 + 字段域补齐（四藏字段/Environments/描述拆分/Force Auth/Suppress POM）+ 包型 modal 880 + **8 已实现包型开禁（纯前端门——八型真实客户端 roundtrip）** + 入口分路由/列表列集/dirty-gating/remote Test（B-1.5 + B-2.5/6 + B-3.6/7/8/9/11/12）
- [ ] FR-144 批次③ 详情/搜索栈：页签序（权限在属性前）+ File URL + **Downloads/Last Downloaded 字段族〔dep 统计基建〕** + 仓库目录元数据 + 属性编辑解剖 + 下载形态 + 日期格式 + 搜索列集（三源口径归一）/行导航/快搜空历史 + **页码控件 ×9 统一〔Q4 已裁翻正——LC-98〕**（B-2.1~4/7~11/13/14 + B-3.14/15/16）
- [ ] FR-145 批次④ 安全/shell 栈：用户/组路由表单化〔**Q5 已裁路由化**〕+ 权限两步弹窗（Any Local/Any Remote 预置）+ 能力位三旗 + profile 自助 token/SSH key + 监控 System Logs/Service Status + 帮助下拉/About + 导航分组与侧栏过滤 + GC/备份 cron 消费面〔转 FR-150〕（B-1.7/8/11 + B-2.15~18）
- [ ] FR-146 后端配合小域：Annotate 动词与 write→Deploy/Cache 拆分〔**Q7 已裁加**——数据迁移零提权〕+ **per-node 下载计数基建（一鱼两吃——喂批次③字段族 + AQL usage 域）** + Last Login 派生
- [ ] FR-147 remote 远端浏览可选档（M15 Q4 终裁承接——批 1 = helm classic + deb + rpm，`listRemoteFolderItems` 对位默认 false 维持缓存浏览；牵连 repo-semantics §8.5 口径扩面）
- [ ] FR-148 AQL 高级面副线（M15 既定第一顺位，视 lane 容量裁剪）：statistics/usage 域 + `/api/search/usage`〔dep 统计基建〕+ QRL 全量三态 + UI 搜索族 + dates/creation（M15 §5.7 全景表 M16 行逐条对账）
- [ ] FR-149 i18n 双语可切换（**Q3 用户终裁——E6 翻案**）：i18n 框架 + 文案外提 100%（CI 断言）+ 中英两包（en 术语对齐 Artifactory）+ 语言切换器 + 断言双语化策略（默认 zh 全量 + en 抽样腿）；LC-97 C 层
- [ ] FR-150 cron 调度域（**Q1 用户终裁——推翻 M15 Q5「不引入」裁定，BOARD 留痕**）：调度数据模型 + cron 表达式子集 + next-run 计算 + 三消费面（GC 定时含 Cleanup 两族 / 备份定时 CRUD / 复制配置 cron 字段）+ 与事件驱动引擎并存零重复投递 + audit；**ADR-0044（占位）Accepted 前置**
- [ ] QA：L35~L48 + 逐批 V 式活体复核（t226）+ **B 47 项收口审计表（四态归属零无主项）** + 断言反转①~⑦归属审计（含 E6 双语 / cron 推翻两例 v1.1 翻案）+ M1~M15 P0 双形态全量回归；tech-writer 增量（i18n/cron 指南 + 豁免翻案用户可见变化公告）；release 烟测 + UAT 随里程碑 PR
- [ ] 条件小票池：NuGet symbol 六承转正〔Q10〕/ docker virtual 开禁（T-431 滚入首票）/ E7 toast 锚位（候用户信号）/ L1 列选器推广 + Users Last Login 列（A6 部分翻案）/ license 公钥 config 覆盖 ADR / t381 残留〔Q12 conductor〕——未触发 BOARD 留痕非 DoD 缺口
- [ ] 「M15 未纳入项」对账：双源对账（本段 vs 审计产出——勿重复立项；cron 双轨行已随 Q1 终裁推翻——本段该行历史留痕不改）；收口时建「M16 未纳入项」段（DoD#7 字面；备稿沿 T-395/T-427 先例收口窗启用——滚程项与 **M17 预立项段**对账定去向）

### M17 — 产品域扩张预立项段（Q6 终裁两程结构之后程；2026-09-02 conductor 指令立段，**候 M16 收口后 PM 正式立项**）

> **前置门槛（未发生即不启动）**：PRODUCT.md「明确不做」修订解禁（用户动作）+ ADR 群 + M16 收口（`m16-done`）。本段为预立项登记——不派票、不占 M16 容量；M17 立项稿（PM）据此起草，范围定容时与「M16 未纳入项」双源对账。

- **主目（conductor 两程结构指令列定）**：Builds / Build-info 域（PUT/append/查询/promotion/retention + docker promote）、Release Bundle 域、洞察报表 / 趋势分析图表、Federation / Lifecycles（含 Release Lifecycle 细分面）。
- **随域联动解锁面**：AQL 六域（build/module/dependency/promotion/releasebundle/sensitive）、`/api/search/license·dependency·buildArtifacts`、D3 依赖树、Module ID 字段、Any Distribution 预置、Repository Path Map 管理面。
- **追加候补（A1 同族、本轮未点名——M17 立项稿定容）**：HA 高可用本体、Artifactory 全量 REST 兼容（高频子集承诺退让边界）。
- **维持不做**：Xray 集成面（intake ⑤ 明示唯一排除——Q6 终裁不改变）。
- **容量注**：Replay+outbox 行级 REST / virtual 同型全包型推广 / Go·Terraform·GitLFS·AI-ML 包型域 / Cleanup-冷存储等 M16 让位滚程项，M17 立项时与产品域主目统一排程对账。

### M11 未纳入项（滚入 M12+ 候选池；2026-08-28 T-329 终验归档后由 M11 PRD §2.2/§4.8 + 用户三项裁决〔07:5x〕+ 票级遗留登记处置；DoD#7 对账）

- **用户裁决落定 M12 项（BOARD 2026-08-28 07:5x 三项裁决 + 收口裁定）**：
  - **NuGet 对齐 bundle → M12 立项**（裁决①）：v2 全面实装 + remote/virtual search 上游代理 + service index 动态解析——T-304 复核 L2/L3-remote/L4/L7 四项随批（出处与行为规格 T-304 §1.1/§4.1 齐备，规格随票可直取）
  - **D-A dual-write S3 停机 fail-open → M12 补实现**（裁决③，T-327 登记）：本地优先写 + 异步 S3 重试队列；M6 PRD FR-50 文面维持，实现债登记 M12
  - **D-F conan v1 delete 状态码小票**（T-329 登记）：`_/_` 坐标（conan 2.x 无 user/channel 形态）packages/delete 删树成功但回 404 而非规格 200（user/channel 形态回 200 已对照；conan 1.x 真实流量不受影响）
  - **D-8R 空载 RSS 瘦身本体**（收口裁定⑤，BOARD 留痕）：实测 138.7MB > 100MB 门槛红（check-size 六平台聚合 94.13MB 过门）——懒加载 embed 瘦身票 M12 承载，非 m11-done 阻塞
- **条件票未触发（BOARD 留痕非 DoD 缺口）**：HelmOCI（Q2，T-320 未派）→ M12+；Trash can（Q7，T-330 未派）→ M12+（与 Cleanup-Retention 同域立项候选）
- **新票候选与票级遗留（收口期登记，BOARD M11 节在档）**：
  - checksum-deploy token 窄域化候选票（T-332 登记）；conan reindex dispatchAPI 之外的 `forceConanAuthentication` 仓配置字段未落（T-308 遗留，默认 false 行为已备）；conan Artifactory 真实上游活体互证（T-312 遗留，mock+自指上游两腿留痕）+ conan 规格 D1/D5/D7/D8 升置信（交 reverse-engineer）
  - crates.io 直连双主机不支持（T-316 R-2 新票候选）+ 上游死 search 404 vs Artifactory 409（T-316 R-3/4）；`.cargo/**` DELETE 收敛（T-316 遗留，低危）
  - helm chartsBaseUrl 分体基址（T-313 D-2）+ `_external` 落盘缓存（D-3，architect 评估单列候选）；oci:// 透传深化（D-5，随 HelmOCI 域）；namespace 模式（D-10）
  - deb bz2 压缩档（T-314：dsnet 依赖不可得实证，plain+gz+xz/lzma 已落地）+ deb snapshot 族（T-310 §10 缓议）
  - T-290-2 `socketTimeoutMillis` canonical 回显键统一（T-304 判改回、路由 T-317 未承载——T-317 票面三臂不含；M11 PRD §5.6.1 v1.2.2 登记）
  - byHash 值域枚举校验归 repo.Service（T-327R 登记）+ by-hash 同秒代合并 / ALL→NONE 遗留树老化（T-327G 登记）+ web 仓表单 deb/rpm 策略键跟进（T-327R）
  - env-only 不完整链键组先于 binstore 拒启（T-325 登记，归 dev-go-storage）；mc 镜像 tag 维持版本锚定（T-325，registry 直连不可达）
  - auth 域尾巴：audit 词表两词 / userDnPattern 消费缺位（T-305 遗留）；statisticsEnabled/sourceOrigin 落库无行为（T-317 差异 2，待 stats 面立项）；属性复制仅 generic 平面（T-317 差异 3，协议面归各适配器）
  - 前端与测试：MUI 批次三候选（T-300：RepoDetailPage/Dashboard/Profile/Placeholder/NotFound + 共享组件六件套 + combobox 统一化）；e2e 负载 flake 族 CI 专用 runner（T-327 §7 协议 + T-329 观察④重申）；deb 满载并行抖动（T-318/T-329 同族，隔离绿）
  - keypair T-319 D-1~D-8 / SAML T-331 D-1~D-5 差异登记（各票报告在档，随域票消化）
- **规格/架构回写转交**：architecture.md §15.4/§23 回写转 architect（T-332 登记；§15.4.1 remote 字段落 canonical JSON as-built〔T-317 差异 1〕同批）；cargo.md（reverse）§8 virtual 行 as-built 回刷（T-318 遗留 → reverse-engineer，随 T-329 D-G 文档回刷）
- **M12+ 主轴候选（沿 M11 PRD §2.2 既定 + M10 未纳入项续滚；2026-08-28 M12 PRD §2.2 主轴重排——**已收编 M12**：制品操作族〔FR-105〕/ Trash can〔FR-106〕/ NuGet symbol server〔余量条件票 Q2〕/ HelmOCI〔FR-109〕；**余者滚 M13+**，PM 排期留痕见 M12 PRD §2.2）**：**HA 高可用本体**（Q1 终裁 M12+ 单列——M12 PRD Q1 暂行建议 **M13 专程**，须 PRODUCT.md 修订解禁）+ Xray 集成面本体（同族处置）/ AQL + 13 老搜索（搜索基建专程）/ Cleanup-Retention 策略引擎（与 FR-106 回收站分界留痕）/ Webhook 统一事件总线（36 事件）/ Build-info 域 / Go 深化（sumdb 代理 + external 重定向）/ Terraform / GitLFS / 制品 license 识别（licences.xml 91 模式）+ 冷存储分层 / HuggingFace 等 AI/ML 13 型 / license 公钥 config 覆盖（T-293 终裁③，走新 ADR）/ M10 未纳入项其余（E-04 / R2 搜索契约 / R6 Tokens 页 + 票级遗留 17 条）

### M10 未纳入项（滚入 M11+ 候选池；2026-08-25 M9 终验归档后由 M10 PRD §2.2/§4.7 处置）
- 延后 3 项（F 池）：E-04 repos 列表扩列 / R2 搜索契约 / R6 Tokens 页
- Q5 replica 隔离（ADR-0025 决策 1 遗留）→ **建议并入 M11「复制硬化」**（与 smart remote contentSynchronisation/属性同步同域，消费 M10 属性系统成果）/ E7 repos 侧过滤列表（ADR-0030）
- 票级遗留 17 条：remote JoinURL 转义（D-1 同类候选）/ -rev 回显塌缩 / scenario-3 观测面 / 复制管理专篇 / console-m8 §4.1/§6.9 回写 / SearchPage q-sync 微票 / recents 双实现收敛 / a11y 预算观测 / matrix 层探针 / .status-pill 收敛 / counts 实体列 / docs-site/build 体积 / legacy Engine.GC 物理删除 / Playwright 压力腿形态 / t104 matrix workers / T-251.md 遗留 5 措辞
- M11+ 主轴候选（主矩阵十大缺口分期）：AQL + 13 老搜索 / Trash can / Cleanup-Retention / 制品操作族（copy/move/zip/archive!/）/ Webhook 事件总线 / 第一梯队包型批量实现（消费 FR-91 规格）/ **HA 高可用本体**（M10 仅占位槽位，未实现）/ **NuGet symbol server**（.pdb/GUID 路径）/ **制品 license 识别**（licences.xml 91 模式，inv-4 J3）/ **冷存储分层**（Cleanup-Retention 内单列）/ Build-info 域 / Go 深化（sumdb 代理 + external 重定向）/ HuggingFace 等 AI/ML 13 型（T-297 终验 DoD-7 补词：原四处弱登记显式化，2026-08-26）

### M12 未纳入项（滚入 M13+ 候选池；2026-08-30 T-356 终验归档）
- **票级遗留**：chartsBaseUrl 分体基址（T-313 D-2，remote 域配置票）；`_external` 落盘缓存（D-3，建议 M13 引擎 absolute-URL 缝票）；HelmOCI remote/virtual（D-5 翻转点）；folderDownloadConfig / trashcan.retention_days 两个 YAML 旋钮（缝已备）；deb bz2 压缩档；conan D8 整树删翻转票（T-348 新取证）；D-F2 布局迁移（FR-110.1 邻域）；npm registry/token 尾斜杠接入注释。
- **P2 登记维持**：D-10 同字节幂等分歧 / flat 措辞 / L31 拒启次序解释空间（PRD AC5 与 T-349 设计冲突的文面裁定）/ 满载 flake 新成员（TestBigTreeCopyNo5xx 预算臂 raceEnabled escape）。
- **M13+ 主轴候选**：HA 本体（Q1 终裁单列，需 PRODUCT.md 修订解禁）/ AQL + 老搜索 / Webhook 事件总线 / Build-info / Go 深化 / NuGet symbol server（M12 Q2）/ 制品 license 识别 / 冷存储分层 / AI/ML 包型扩展。
- **运维尾巴**：trash 树常驻节点（console-m8 §6.3 推翻后的最小面补齐）；console-m8 侧栏清单过时（12/13 vs 15）。

## 里程碑完成定义（DoD）

每个里程碑视为完成，当且仅当：
1. 该里程碑所有 ticket 处于 done（通过 review + qa）
2. qa-engineer 的兼容/验收报告全绿（含真实客户端证据）
3. release-engineer 对已交付的部署方式完成烟测（M2 起）
4. tech-writer 已产出该里程碑新增能力的用户文档
5. 主会话完成 git tag（`m<N>-done`）；对外发布任何制品先经用户确认
