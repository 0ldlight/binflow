# 路线图（ROADMAP）

> 由 product-manager 维护；tech-lead 据此把当前里程碑分解为 ticket。

## 当前里程碑：M11（Artifactory 对齐第二程——配置域指令兑现 + 第一梯队包型批量 + 行为逐项对齐制度化；PRD v1.1 已审定（Q1/Q2/Q6/Q8 用户裁定），2026-08-26）

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
需求基线：docs/prd/milestone-11.md（FR-92~FR-102 十一条需求；契约矩阵 18 条〔A 14 / C 2 / D 1 / 待裁 1〕+ 档位 × addon 矩阵扩展 5 槽；L01~L45；开放问题 Q1~Q8 带暂行）
来源链：用户指令日志最近四条（2026-08-26 11:22 MUI 迁移 T-299/T-300、11:35 认证配置前端化、11:45 存储配置独立文件化、**19:05 行为逐项对齐**——全程工作方式条款）→ conductor 种子 A~D；FR-91 五份规格 + tl-fr91-ac3 23 裁决点；M10 §2.2 滚入项 + M9 Q5 复制硬化
- [ ] conductor 审定 PRD v1.0（Q1~Q8 暂行终裁；ADR-0035/0036 立项）
- [ ] 前置产物：ADR-0035（认证配置面 REST/持久化/变更即生效/双源优先级）+ ADR-0036（存储配置独立文件与链式 schema）；规格复核票两份（auth-integration.md / config-formats.md §1——逐条附 Artifactory 行为出处）+ R-1/R-2 规格修订
- [ ] FR-92 认证配置前端化：OAuth2/LDAP/SAML 管理面 REST + 测试连接 + 变更即生效 + 控制台页组（MUI）（P0；SAML 运行时深度 Q3）
- [ ] FR-93 存储配置独立文件化：链式 provider 表达（filestore/S3/dual-write）+ 兼容窗 + 部署矩阵演进 + CD 链验证（P0）
- [ ] FR-94 存量控制台 MUI 化两批：T-299 批次一（P0）/ T-300 批次二（P1）——交互逻辑零变化四闸门
- [ ] FR-95 M10 自有裁定回头看：基线 23 项 + tl 裁决差异点（RP-2/TL-5/TL-4/HL-2 等→Q8）逐条复核——维持附出处/改回验证/分歧上 BOARD（P0，规划期完成）
- [ ] FR-96 conan 包型：v2 local 全量 + v1 握手三端点（P0）+ remote/virtual（P1）
- [ ] FR-97 debian 包型：automatic local 主票（P0）+ virtual/remote（P1）+ trivial P2；GPG K-1 条件票（Q6）
- [ ] FR-98 rpm 包型：local 管线含 header 解析器（P0）+ reindex 七分支 + remote/virtual（P1）+ modules P2
- [ ] FR-99 helm 经典仓 local（P0）+ virtual/remote（P1）；HelmOCI 条件票（Q2，HL-3）
- [ ] FR-100 cargo remote pull-through + virtual（P1，dep T-294——cargo 家族三态齐装）
- [ ] FR-101 复制硬化：enableTokenAuthentication/contentSynchronisation 生效 + 属性复制同步 + replica 隔离终裁执行（P1）
- [ ] FR-102 工程债打包：S3 MPU kill -9 续传复活 / unused-cleanup 引擎 / D-8 footprint ≤100MB / D-9 测试基建 / 文档尾巴三处（P1/P2）
- [ ] QA：L01~L45 + 四包型真实客户端矩阵（conan/apt/dnf/helm）+ M1~M10 P0 回归双形态 + 两处断言反转审计；tech-writer 五类文档；Trash can 余量条件票（Q7）
- [ ] 「M10 未纳入项」对账：D 组（Cleanup-Retention 策略引擎/制品操作族/Webhook/AQL）建议 M12+，滚入「M11 未纳入项」登记

### M10 未纳入项（滚入 M11+ 候选池；2026-08-25 M9 终验归档后由 M10 PRD §2.2/§4.7 处置）
- 延后 3 项（F 池）：E-04 repos 列表扩列 / R2 搜索契约 / R6 Tokens 页
- Q5 replica 隔离（ADR-0025 决策 1 遗留）→ **建议并入 M11「复制硬化」**（与 smart remote contentSynchronisation/属性同步同域，消费 M10 属性系统成果）/ E7 repos 侧过滤列表（ADR-0030）
- 票级遗留 17 条：remote JoinURL 转义（D-1 同类候选）/ -rev 回显塌缩 / scenario-3 观测面 / 复制管理专篇 / console-m8 §4.1/§6.9 回写 / SearchPage q-sync 微票 / recents 双实现收敛 / a11y 预算观测 / matrix 层探针 / .status-pill 收敛 / counts 实体列 / docs-site/build 体积 / legacy Engine.GC 物理删除 / Playwright 压力腿形态 / t104 matrix workers / T-251.md 遗留 5 措辞
- M11+ 主轴候选（主矩阵十大缺口分期）：AQL + 13 老搜索 / Trash can / Cleanup-Retention / 制品操作族（copy/move/zip/archive!/）/ Webhook 事件总线 / 第一梯队包型批量实现（消费 FR-91 规格）/ **HA 高可用本体**（M10 仅占位槽位，未实现）/ **NuGet symbol server**（.pdb/GUID 路径）/ **制品 license 识别**（licences.xml 91 模式，inv-4 J3）/ **冷存储分层**（Cleanup-Retention 内单列）/ Build-info 域 / Go 深化（sumdb 代理 + external 重定向）/ HuggingFace 等 AI/ML 13 型（T-297 终验 DoD-7 补词：原四处弱登记显式化，2026-08-26）

## 里程碑完成定义（DoD）

每个里程碑视为完成，当且仅当：
1. 该里程碑所有 ticket 处于 done（通过 review + qa）
2. qa-engineer 的兼容/验收报告全绿（含真实客户端证据）
3. release-engineer 对已交付的部署方式完成烟测（M2 起）
4. tech-writer 已产出该里程碑新增能力的用户文档
5. 主会话完成 git tag（`m<N>-done`）；对外发布任何制品先经用户确认
