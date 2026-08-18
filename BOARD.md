# 任务看板（BOARD）

> 唯一事实来源。**只有主会话（conductor）可以写本文件**，所有 subagent 只读。
> ticket 由 tech-lead 生成、主会话录入。当前里程碑：M1 内核基座（见 ROADMAP.md）。

## 票据格式

```
- **T-<编号>** [P0|P1|P2] 标题 `role:<agent类型>` `area:<Go包/页面组/部署目标>` `dep:T-x,T-y`
  AC: ①可验证的验收标准 ②… ③…
```

- 优先级：P0 阻塞他人/当前里程碑关键路径；P1 本里程碑应完成；P2 可延后。
- `area` 示例：`internal/storage`、`internal/adapter/docker`、`internal/auth`、`web/src/pages`、`deploy/helm`。
  同一轮并行派发的 ticket，area 不得重叠。
- `dep` 列出必须先完成的 ticket；协议适配类 ticket 必须依赖对应的逆向规格票。

## 📥 待办（todo）

> 完整 AC 见 reports/agents/T-6.md（唯一全文）；此处为录入摘要。分批：1:T-7 → 2:{T-8,T-9,T-10} → 3:T-11 → 4:T-12 → 5:T-13 → 6:{T-14,T-20} → 7:T-15 → 8:T-16 → 9:T-17 → 10:{T-18,T-19 串行}。

- **T-8** [P0] config 包：YAML+env 覆盖与 fail-fast 校验 `role:dev-go-core` `area:internal/config` `dep:T-7`
  AC: ① Load 架构 §8 全量字段+env（BINFLOW_ 前缀 __ 层级）；匿名读双键名等价+冲突报错 ② fail-fast 校验；ADMIN_PASSWORD 只走 env ③ table-driven 覆盖校验与 env 矩阵
- **T-9** [P0] storage 引擎：会话/checksum 寻址/原子落盘/GC `role:dev-go-storage` `area:internal/storage` `dep:T-7`（review 双 reviewer）
  AC: ① Engine/Session + blobs/<xx>/<sha256> 布局 + write→fsync→rename→fsync(dir) + singleflight + sentinel ② 启动清扫 + GC dry-run 默认；-race 单测：去重/摘要不匹配/Abort 零残留/并发收敛/崩溃窗口 ③ 1GB RSS<256MB；三摘要与 sha256sum 一致；Open 返回 ReadSeekCloser
- **T-10** [P0] metadata：SQLite Store+迁移器+001_init `role:dev-go-core` `area:internal/metadata` `dep:T-7`（review 双 reviewer）
  AC: ① Store+六子接口+embedded 迁移器+modernc.org/sqlite+WAL+零 CGO ② 9 表 DDL+admin 种子（env 优先/缺省 password/argon2id）；permissions 按 PRD E-24 命名 target 扩展 ③ -race 单测：幂等迁移/upsert/ListByPrefix/Token 只存摘要/级联删
- **T-11** [P0] auth 与 audit：认证/Token/路径 ACL `role:dev-go-core` `area:internal/auth、internal/audit` `dep:T-8,T-10`
  AC: ① Authenticator（Basic/Token/X-JFrog-Art-Api/匿名）+argon2id+TokenRegistry（只存 sha256）② Authorizer.Can：admin 全过；命名 permission target（include/exclude 两级通配）；匿名仅内容 GET/HEAD ③ 权限矩阵/token 生命周期/改密单测
- **T-12** [P0] repo.Service：local 用例编排与仓库 CRUD `role:dev-go-core` `area:internal/repo` `dep:T-9,T-10,T-11`（review 加正确性 reviewer）
  AC: ① repo key [a-z][a-z0-9-]{1,62}+保留字 api/v2+rclass 仅 local ② 同 checksum 幂等重传免覆盖检查（T-3 规格吸收）；Delete 只删引用+幂等 404；删仓 deleteContent 语义 ③ fake 驱动全分支单测含事务回滚
- **T-13** [P0] adapter SPI 与 Generic 适配器 `role:dev-registry-adapter` `area:internal/adapter、internal/adapter/generic` `dep:T-3,T-11,T-12`
  AC: ① SPI+路径归一化拒绝逃逸（../%2e%2e→400）；PUT 201+Location+checksum 头+FileInfo（size 字符串）；GET/HEAD 三 checksum 头+ETag=sha1；DELETE 204 重复 404 ② X-Checksum 不一致→409；checksum-deploy 未命中→404；Explode→400 ③ httptest+curl 真实客户端断言 C07~C23 等价
- **T-14** [P0] httpapi 核心：Server/middleware/错误信封/路由 `role:dev-go-core` `area:internal/httpapi（核心）、internal/console（占位）` `dep:T-8,T-11,T-13`
  AC: ① 路由表：/healthz /readyz 无前缀；/binflow 剥离分发；ping/version/v1-health/v1-stats ② middleware 链固定顺序；结构化日志不记认证头；错误信封 errors[]（含内容路径）；E-26 全矩阵 404 ③ 认证分层（匿名内容 GET/HEAD）；SIGTERM 优雅停机
- **T-15** [P0] Artifactory 兼容 REST `role:dev-registry-adapter` `area:internal/httpapi（兼容 handlers，与 T-14 串行）` `dep:T-3,T-14`
  AC: ① 仓库 CRUD（PUT 建仓 200 纯文本；remote/virtual 400）② /api/storage FileInfo/FolderInfo 字段全集（rest-api §3）；?list 匿名 403/根 400 ③ security：password/token（自有语义，auth-model.md 缺位）/users；/api/v1/permissions CRUD；C 序列 curl 断言
- **T-16** [P0] cmd 装配与生命周期 `role:dev-go-core` `area:cmd/binflow-server` `dep:T-14,T-15`
  AC: ① 构造注入装配链；serve/gc subcommand ② 缺省口令 WARN；postgres 报错退出；启动清扫；冷启动<2s ③ scripts/smoke.sh 冒烟（C01/C03/C07/C08）
- **T-17** [P0] 开发环境：Dockerfile/compose+README `role:devops-engineer` `area:deploy/dev、README.md` `dep:T-16`
  AC: ① compose up 30s ping OK；restart/down+up 持久化（C29）② README 五步快速开始（URL /binflow）；匿名读/缺省口令/明文 HTTP 提示 ③ 健康检查+stop_grace_period≥30s；全新环境复跑全 0
- **T-18** [P0] QA：M1 功能矩阵验收（§7 场景 1/3/4/6/7） `role:qa-engineer` `area:验收` `dep:T-16,T-17,T-21`（v1.2 先行，R9b）
  AC: ① 工程基线+仓库生命周期（C03 期望 200）+roundtrip（C14 期望 409）+边界拒绝 ② 认证 ACL 双模式（C02~C23/C27）；校准项复核（DELETE 204/幂等重传/mkdir 201/size 字符串）③ NFR-S1/S2/S3 抽查（无明文凭据）；5xx 打回附票号
- **T-19** [P0] QA：存储完整性/性能/持久化+README 复跑（§7 场景 2/5/8/9/10） `role:qa-engineer` `area:验收` `dep:T-18`
  AC: ① C12 去重/慢上传中断/kill -9 重启/覆盖幂等/1GB RSS ② 冷启动<2s 双路径；100 并发 0 错误；C29 两轮；gc dry-run/--apply ③ README 全新复跑；产出 T-19-qa.md DoD 结论
- **T-20** [P2] Range 与条件请求 `role:dev-registry-adapter` `area:internal/adapter/generic` `dep:T-13`
  AC: ① 单区间 206/非法 416 ② If-None-Match/If-Modified-Since→304 ③ curl -r/-z 断言；M2 前必须 done
- **T-23** [P1] 补逆向规格 auth-model.md（R7） `role:reverse-engineer` `area:docs/reverse` `dep:T-3`
  AC: ① auth-model.md：用户/组/权限模型+token 行为（签发/验证/吊销/过期字段与错误码）② 置信度标注 ③ clean-room 铁律；供 T-15 token 端点校准

## 🔨 进行中（doing）

- **T-18** [P0] QA：M1 功能矩阵验收 `role:qa-engineer` `area:验收` `dep:T-16,T-17`
  状态：08:1x 派发，在途（v1.3.1 口径：C03 200/C14 409/C28a sfu/幂等重传/Range 面）。

## 👀 评审中（review）

（空）

## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

- **T-1** [P0] M1 里程碑 PRD `role:product-manager` `area:docs/prd` — done 2026-08-17
  产出 docs/prd/milestone-1.md（486 行）：FR-1~FR-6 全 AC、26 端点兼容矩阵、C01~C30 验收命令。核验通过。
  8 项开放问题已定案（Q1 `/binflow` 前缀；Q2 匿名读默认开；Q3 缺省 password；Q4 如实版本；Q5 纯 Go SQLite；Q6 Range P2；Q7 github.com/lzwzzy/binflow；Q8 devops 起草）→ 回写为 T-4。
- **T-2** [P0] M1 架构设计与元数据 schema 定稿 `role:architect` `area:docs/design` — done 2026-08-17
  产出 docs/design/architecture.md（12 节）+ ADR-0005/0006/0007（零 CGO 依赖基线 modernc.org/sqlite、blob 布局与落盘协议、迁移机制）。核验通过。
  决策对齐修订（/binflow 前缀、匿名读、Q3 口令、Q7 路径）→ T-5。
- **T-4** [P0] PRD v1.1 回写 8 项已定案决策 `role:product-manager` `area:docs/prd` `dep:T-1` — done 2026-08-17
  docs/prd/milestone-1.md 升 v1.1（525 行）：§0 修订记录；Q1 全文 URL 改 /binflow（104 处）+ /artifactory/** 404 断言；Q2 匿名读默认开（FR-5-AC12/13、C23/C27、NFR-S8）；Q5 零 CGO（FR-1-AC7）；Q7 落定；§9 已决决策表。核验通过（残留 /artifactory 均为有意保留）。
- **T-5** [P0] 架构文档对齐用户决策（含 Q3 修订） `role:architect` `area:docs/design` `dep:T-2` — done 2026-08-17
  ADR-0008（/binflow 统一前缀 + module 路径 + docker /v2 例外风险）、ADR-0009（匿名读默认开 + admin env 口令引导）；§7.1 路由全前缀化、§6 种子数据改 env 优先/缺省 password、Derby/H2 排除记录入 ADR-0005。核验通过。
- **T-3** [P0] Artifactory M1 行为规格（clean-room） `role:reverse-engineer` `area:docs/reverse` — done 2026-08-17
  四份规格共 471 行：rest-api.md（~34 端点，高28/中10/低3）、storage-layout.md（sha1 分片/binaries+nodes 表行为/_pre 暂存 24h 清理）、config-formats.md（三 XML → YAML 映射）、repo-semantics.md（local 8 路径规则/上传 5 步/删除 9 场景）。核验通过（置信度标注齐全、clean-room 抽查无代码翻译）。
  高价值发现：同 checksum 幂等重传免覆盖权限检查（官方未记载）、统一错误体 errors[] 形态、回收站 14 天。PRD §5.5 六项校准项可回写（PM 增量修订，随下轮或 T-6 一并处理）。
- **T-6** [P0] M1 工程 ticket 拆解 `role:tech-lead` — done 2026-08-17
  14 张票（T-7~T-20）+ 分批表（最大波 3 张）+ R1~R9 风险清单，全文 reports/agents/T-6.md。核验通过（area 无重叠、宽度 ≤4、校准项固化进 AC、低置信度不作 AC）。R1/R2→T-21；R3~R6→T-22；R7→T-23；R8 处置合理照准。
- **T-21** [P0] PRD v1.2 校准回写 `role:product-manager` `area:docs/prd` `dep:T-3,T-6` — done 2026-08-17
  R1（checksum 不一致 409）/R2（建仓 200 纯文本）/§5.5 六项全部定案（改「校准记录」表）；增补两条规格（ETag/304/416、幂等重传注记）；token 字段标待 T-23。核验通过（旧口径无残留，对照表左列旧值为有意保留）。T-18 QA 依赖已解除。
- **T-22** [P0] architecture.md 回写 R3~R6 `role:architect` `area:docs/design` `dep:T-6` — done 2026-08-17
  R3 匿名读主键名 security.anonymous_access（别名双键等价）；R4 permission_targets+permission_principals 两表替换扁平表；R5 repo key {1,62}；R6 错误信封 errors[] 数组形。核验通过（grep 四处落点 + 旧形态零残留）。ADR-0008/0009 仅追加回写注记。遗留：M4 可在 breaking 窗口移除旧键别名。
- **T-7** [P0] 工程脚手架 `role:devops-engineer` `area:仓库根` — done 2026-08-17
  go.mod（github.com/lzwzzy/binflow, go 1.26）/ Makefile（build/test/lint/dev/clean，GOPROXY 镜像）/ .golangci.yml v2 / CI 三步 / cmd 骨架 / internal 九包 doc.go。conductor 亲测复现：build 2.58MB、test PASS、lint 0 issues、gofmt 空、CGO_ENABLED=0 通过。遗留：CI 首跑绿待推送后确认；go.sum 待首依赖生成。
- **T-23** [P1] 补逆向规格 auth-model.md `role:reverse-engineer` `area:docs/reverse` `dep:T-3` — done 2026-08-17
  273 行六节：用户模型/改密/Token 生命周期/权限概览/M1 校准建议/待验证清单；置信度高 41/中 16/低 1。核验通过（clean-room 零违规）。关键校准：token 创建响应真实字段集（无 token_id）+form 编码；revoke 幂等 200；改密现行路径与 400 语义；建用户真实为 PUT {name}。→ 触发 T-24 PRD v1.3。
- **T-24** [P1] PRD v1.3 auth 校准回写 `role:product-manager` `area:docs/prd` `dep:T-23` — done 2026-08-17
  E-17 form 编码+真实字段集（token_id 超集扩展）；E-18 revoke XOR+幂等 200；E-16 双路由+旧口令 400；E-19 PUT {name} 兼容路由；§5.1 错误体三分层（制品 errors[]/用户管理纯文本/token OAuth）；§5.5 六项校准全部收口；顺手修 C20 剧本 ADMIN_PW 连锁 401 缺陷。核验通过（旧口径零残留）。T-15 派发时附 v1.3 口径。
- **T-10** [P0] metadata：SQLite Store+迁移器+001_init `role:dev-go-core` `area:internal/metadata` `dep:T-7` — done 2026-08-17（经一轮修复）
  14 文件：api/store/migrate/password/substores + 001_init.sql（9 表，permission 两表形态）+ 35 测试。双 review：正确性 APPROVE；架构 REQUEST_CHANGES 两 blocker 均已修复并复审通过——B1 LIKE 大小写误删（case_sensitive_like 入 DSN + 6 子用例破坏性断言）、B2 池 NumCPU + 三 PRAGMA 挪 DSN（四连接并发断言 + fail-fast）。conductor 复现：race 12.8s 绿 / lint 0。
  遗留（minor 不阻塞）：FilterUnreferenced TOCTOU 契约（T-13 派单附注）、tokenStore.Touch 上下文（T-11 顺车）、双进程首启竞态（M4 技术债）。
- **T-8** [P0] config 包 `role:dev-go-core` `area:internal/config` `dep:T-7` — done 2026-08-18（经一轮修复）
  7 文件：api/load/validate/config + 30 测试（覆盖率 91.9%）。review 3 blocker + 1 major 全修复并复审通过：B1 多文档 YAML（decode 后断言 io.EOF）、B2 sqlite DSN 白名单（拒绝 URI 形态）、B3 ADMIN_PASSWORD 大小写归一赋值、M1 redactDSN 脱敏。conductor 独立探针复验多文档秘密拦截。race 2.4s 绿 / lint 0。
  顺手：m2 TOCTOU 注释、m1 doc.go 例外清单补 DATA_DIR、m4/m5 测试补齐；n1 记录保留理由。
- **T-25** [P0] 架构文档回写 `role:architect` `area:docs/design、DECISIONS.md` `dep:T-8,T-9,T-10` — done 2026-08-18
  14 处回写（architecture.md 12 + ADR-0007 勘误 2）：GC 集合形签名/mark-sweep/mtime 硬约束（备份保留 mtime）、Engine.Close、sentinel 六全集、state.json 契约定稿与 version 演进规则、DSN per-connection PRAGMA 机制、case_sensitive_like 语义、事务边界 a 案裁定、DATA_DIR 四例外名。核验通过（旧措辞零残留，来源标注 18 处）。
- **T-9** [P0] storage 引擎 `role:dev-go-storage` `area:internal/storage` `dep:T-7` — done 2026-08-18（经三轮收敛）
  11 文件 ~1200 行实现 + 40 测试。三轮评审收敛：架构 B1/M1/M2（Close 契约、会话毒化、state 形状）→ 正确性探针 singleflight panic 死锁（defer teardown 修复 + waiter 释放测试）→ rename 失败/GC 保护场景固化。conductor 复验：race 104s 全绿、lint 0、零 CGO、包边界红线（binflow 依赖=1）、512MB RSS 增量为负。
  契约偏离（经 T-25 回写架构）：GC 集合形回调、Close() 入接口、state.json version 字段。已知边界：单 data dir 单 Engine 实例（doc.go 约束）。
- **T-11** [P0] auth 与 audit `role:dev-go-core` `area:internal/auth、internal/audit` `dep:T-8,T-10` — done 2026-08-18（经一轮修复）
  auth 13 文件 + audit 2 文件，29 测试/94 子用例。review 3 blocker 全修复并复审通过：B-1 哨兵导出别名（errors.Is 双拼法钉死）；B-2 pathmatch folder 语义（尾斜杠=folder，matchStart 仅 folder 生效，over-grant 探针三行钉死）；B-3 fail-closed 三测试 + 日志卫生。conductor 复现：race 23s 绿 / lint 0。
  新契约待 architect 回写（§3.4）：Can 的 path 尾斜杠=folder；文件路径与 pattern 全段匹配。顺手：Redact camelCase/header、ChangePassword 顺序对齐规格、Touch 每分钟节流。
- **T-12** [P0] repo.Service `role:dev-go-core` `area:internal/repo` `dep:T-9,T-10,T-11` — done 2026-08-18（经一轮修复）
  api/service/validate + 28 测试（真引擎基座 + hookStore 注入/journal 写序）。review 2 blocker 修复并复审通过（TrimSuffix 三处归一化：prune 保活目录行回归 + List 双形态一致）；事务边界被证实过硬（blob-first/FK 兜底/24 并发探针）。conductor 复现：race 19.5s 绿 / lint 0。修复 agent 被 429 击落于日志收尾，产出 100% 落盘。
- **T-26** [P1] architect 回写：Can folder 契约 + gosec 豁免复核 `role:architect` `area:docs/design、.golangci.yml` `dep:T-11` — done 2026-08-18
  §3.4 补 Can 尾斜杠=folder 三句契约（对齐 AuthorizationServiceBase）；gosec 五规则收窄：G301/G306 移除豁免、G204 限测试、G401 限 digest.go+测试、G115 限 password.go、G304 限两文件；隔离探针反向验证（新违规四条全中）；nolintlint require-explanation 启用。核验通过（lint 0）。
- **T-20** [P2] Range 与条件请求 `role:dev-registry-adapter` `area:internal/adapter/generic` `dep:T-13` — done 2026-08-18
  conditional.go：单区间解析（闭/开/后缀/钳制）+ 条件求值（INM 弱比较三形态/IMS/优先级）；206 走 Seek+CopyN；416 + bytes */total；HEAD 同分支。32 区间矩阵 + ETag 12 形态 + 条件 15 形态单测 + curl 9 步（-r 四形态/999999999- 416/INM 三拼写/-z 双侧）。T-13 零回归。轻量核验（P2+黑盒覆盖）替代 review；conductor 复现 race 绿 lint 0。curl -z 对 ISO 日期静默不发头的客户端怪癖已实证并绕开。M2 If-Range 扩展点已留（seek 数据面就绪）。
- **T-13** [P0] adapter SPI 与 Generic 适配器 `role:dev-registry-adapter` `area:internal/adapter、internal/adapter/generic` `dep:T-3,T-11,T-12` — done 2026-08-18（经一轮修复）
  SPI（Layout 解码→校验链/Register panic 条件/ForRepoType 并发安全）+ generic 四动词 + 校验头语义 + errors[] 信封 + FileInfo（size 字符串）。review 修复：repo.PutFromBlob（判权先于 blob 打开、双维校验、ErrOrphanBlob 堵死孤儿实体化与 sha256-only 降级）、BlobOpener 缝删除、404 文案分动词、控制字节拒绝、originalChecksums 上下文区分。路径安全 30+ 变体真机实测全 400（reviewer 取证）。curl 黑盒 12 场景全 PASS。
  遗留裁决归档：sha1-only deploy 维持 404（M3）；originalChecksums 持久化 M1 不需要；TOCTOU 零调用确认。PutFromBlob 契约待 architect 记入 §3.3（一句话）。
- **T-14** [P0] httpapi 核心 `role:dev-go-core` `area:internal/httpapi、internal/console` `dep:T-8,T-11,T-13` — done 2026-08-18（经一轮修复）
  7 文件 + 真栈 harness 测试（覆盖率 84%）。EscapedPath 手写分发树（绕开 mux cleanPath 归一化）；middleware 固定链 + logFields；errors[] 信封；系统端点四件；SIGTERM 优雅停机。review 3 blocker + 2 major 全修复复审通过：readyz storage 探测（只读目录 503）、死 case 删除、statusRecorder 双标志、mid-stream panic 不嫁接信封、首段 unescape 三源同源（encoded key 可路由且无 ACL 旁路）。curl 冒烟 + 19 探针矩阵全绿。
  遗留归档：minor 1-4/6-8 转 T-15/T-16/PM；RepoLookup 缝判定可保留（PackageTypeOf 列非阻断重构）；M2 架构措辞（路由解析位于授权门后 + ADR-0009 补句）交 architect。
- **T-15** [P0] Artifactory 兼容 REST `role:dev-registry-adapter` `area:internal/httpapi（兼容 handlers）` `dep:T-3,T-14` — done 2026-08-18
  四组 handler（repositories/storage/security/permissions）+ router/middleware/server 扩展 + 5 测试文件（38 顶级测试、覆盖率 78.2%、T-14 零回归）。v1.3 全口径落地：建仓 200 纯文本、错误体三分层、旧口令 400、token form+JSON 嗅探+OAuth 错误体、revoke XOR 幂等双文案、?list 匿名 403/根 400。curl compat 10 场景全 PASS。轻量核验（对定案矩阵编码 + 黑盒覆盖，qa 合并 T-18 全矩阵复验）。
  遗留：T-16 装配三缝（日志已列）；email 仅校验不落盘（users 表无列，回显需派生票，M2 评估）；repo 列表无按调用者过滤（M1 无数据面）。
- **T-27** [P1] architect 回写：三处累积裁决 `role:architect` `area:docs/design、DECISIONS.md、docs/prd` `dep:T-13,T-14` — done 2026-08-18
  §3.3 PutFromBlob 契约（签名逐字对齐实现 + 四点注释）；§7.1「路由解析位置」段 + §3.4 交叉引用 + ADR-0009 补句；PRD C28a 勘误（sfu admin + v1.3.1 修订行）。核验通过。
- **T-16** [P0] cmd 装配与生命周期 `role:dev-go-core` `area:cmd/binflow-server、scripts` `dep:T-14,T-15` — done 2026-08-18（经 429 中断续跑）
  main.go 573 行装配链（无全局单例）+ 15 测试（覆盖率 77.1%）+ scripts/smoke.sh。serve/gc 双 subcommand；冷启动实测 0.15s（NFR-P1 <2s 达标 13 倍余量）；SIGTERM 排空 0.038s exit 0；postgres e2e 拒启零残留；gc dry-run/--apply/幸存断言；admin 缺省 WARN（argon2 真探测）。conductor 实跑 smoke.sh 全绿（C01/C03/C07/C08/匿名/停机）。曾被 429 击落，额度恢复后续跑完成，生产代码零损失。
  遗留：gc 无 -c 旗标（票面未要求）；二次信号强退未构造观察窗口（排空毫秒级）。
- **T-17** [P0] 开发环境 `role:devops-engineer` `area:deploy/dev、README.md` `dep:T-16` — done 2026-08-18
  多阶段 Dockerfile（非 root 10001/HEALTHCHECK /readyz/CGO 零）+ compose（命名卷持久化/35s grace/:? 强制口令）+ README（五步双路径+安全须知三件）。Docker 真机全跑：AC1 up→ping 2.35s 冷链、AC2 health ok、AC3/C29 restart+down&up 两轮 sha256 不变；容器 healthy、db 无明文、日志无凭据。conductor 复核 compose 语法（:? 触发符合预期）。遗留：版本 stamping/distroless/CI docker build 归 M5。

## 🚫 阻塞（blocked）

（空）
