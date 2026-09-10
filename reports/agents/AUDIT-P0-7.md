# AUDIT-P0-7 — 既有规格目录化（frontend/security/storage/jobs 四域）

Ticket:       ARTIFACTORY FULL REIMPLEMENTATION PROGRAM · Phase 0 任务 #7（既有规格目录化）
Role:         reverse-engineer（目录化索引实例）
Area:         docs/reverse/{frontend,security,storage,jobs}/ 四新目录
Input:        派发=Phase 0 §3 对账表的四个 ◐ 行（#10 Frontend/#13 Security/#12 Storage/#14 Async 目录化）；
              既有规格 10 份（console-ui/auth-model/rbac-model/auth-integration/storage-layout/s3-storage-layout/cron-scheduling/rest-api/inv-2/inv-3）；
              docs/design/ parity 锚册 6 份；FE-Rewrite 审计三件套（frontend-rewrite-audit 133KB 等）；
              反编译前端源（artifactory-ui 34 文件 + frontend-server 627 文件）
Changes:      按序——①盘点 docs/reverse 全量 41 文件与 design 锚册；②反编译前端结构定位（宿主 router routes:[] 运行时注册、SSR 挂载拓扑 NODE_SERVER_PATH/ExpressLoaders/ApiRoute）；③运行时路由表提取（imports-map + artifactory/access 两 MFE app.umd.js grep）；④四域 README + 三 yaml 成稿；⑤YAML 语法校验三轮通过
Files:        新建 7——docs/reverse/frontend/{README.md,routes.yaml,screens.yaml,api-map.yaml}、docs/reverse/security/README.md、docs/reverse/storage/README.md、docs/reverse/jobs/README.md（未改任何既有文件/代码，未 commit，未写 BOARD）
Tests:        活体抽验 4 条（:8082 只读）——①GET /ui/login/ =200、/ui/packages =200（SPA 壳可达）；②GET /ui/api/v1/imports-map =200（MFE 资产图实证：artifactory 7.161.12 / access 7.191.14 等 13+ MFE）；③GET /ui/api/v1/system/version -u admin → "1.652.42"（前端服务版本，SSR 面证据）；④GET /ui/api/v1/ui/auth/current -u admin → {"name":"anonymous"...} + /ui/auth/screen/footer → 401（**SSR 面 basic auth 不被接受**——行为发现，已录 api-map.yaml）
Commands:     反编译定位——`find frontend/{artifactory-ui,frontend-server}`（661 文件实测）；`grep -rn API_VERSION Constants/`；`sed ApiRoute.ts`（服务前缀挂载表）；`head router.ts`（base:/ui/ + routes:[]）；`grep -rl kagkarlsson backend-maven`（db-scheduler 12 文件）；`grep -rln AsyncWorkQueueService`（20 消费模块）；`ls artifactory-core/schedule`（HaQuartzTask 等原语）。运行时提取——`curl imports-map`；`curl app.umd.js?v=7.161.12 | grep -o 'path:"[^"]*"'`（98 raw→89 条目）；同法 access MFE（26 条目）；`grep -o 'path:"...name:"..."'` 配对（16 对）。yaml 校验——ruby YAML.load_file ×3 全 valid
Outputs:      规格文件 7（索引条目：路由 119 / 屏 41〔走查 26+路由级 15〕/ API 映射 29 屏 + SSR 面 50 端点 / 安全缺口 11 条 / 存储缺口 10 条 / 调度四轴 + 6 条缺口）；无重写——4 域既有 7 份规格原样引用
Compatibility: 全部索引条目带证据源版本（A=7.161.24 反编译、B=:8082=7.161.20 运行时、console-ui 走查=7.84.10 偏斜显式标注）；三 yaml 头注 evidence-index-only 声明（对齐 module-catalog 先例与 §4-3 裁定）；版本差发现 1 处——7.161 access MFE 含 scim/vaults/global_roles 等 7.84.10 走查后新增页，登记为对拍缺口未改旧稿
Security:     安全相关索引条目——api-map.yaml SSR 开放路由白名单（免认证面 8 族 + open-or-authenticated 6 条）+ admin-only 中间件标注（joinKey/mfa-config/resetMfaStatus）；SSR 面拒绝 basic auth 的行为发现（Tests ④）；security/README §2 negative 缺口 11 条含 MFA/SCIM/Vault 零规格区
Performance:  目录化票无性能条目；既有引用中 s3-storage-layout §2 重定向阈值 200KB 等已在索引行注明出处
Risks:        ①MFE bundle grep 提取的 path 字面量可能含非路由字符串残留（已剔除 SVG 类噪声 5 条，`:repoType(`/`:label(` 尾括号为压缩工件——推断性修剪，低风险）；②7.84.10 走查与 7.161 路由表的版本偏斜贯穿 frontend 三文件，未对拍前 old 走查屏结构不应直接当 7.161 现值；③jobs/storage 的类路径锚是存在性证据非行为断言，防下游误当规格消费
Blockers:     无（reverse-src 就位、:8082 可达）
Next:         建议——①转 unknown 队列（任务 #8 收割本票 UNKNOWN：frontend §5 七条 + security 11 + storage 10 + jobs 6 ≈ 34 条）；②frontend 走查对拍票（:8082 Playwright 走 7.161 管理树 + retention/Pro 屏 + 四态补证）；③jobs work-queue/event/db-scheduler 三专项逆向票（§2 轴 1-3）；④storage corruption/recovery 专项票（Sha256MigrationJob/PersistentQueueErrorService 已定位）

## 断点快照
（不适用——本票已收口）
