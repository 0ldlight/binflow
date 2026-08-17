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

- **T-10** [P0] metadata（修复轮） `role:dev-go-core` `area:internal/metadata`
  状态：双 review 裁决 REQUEST_CHANGES → 回 doing 修两个 blocker：B1 LIKE 大小写误删（PRAGMA case_sensitive_like=ON + 测试）、B2 池开 NumCPU + PRAGMA 挪 DSN（除 WAL）。原 agent 续跑（上下文保留），在途。
  正确性视角 APPROVE 在案；修复后只需针对性复审两 blocker。

## 👀 评审中（review）

- **T-9** [P0] storage 引擎 `role:dev-go-storage` `area:internal/storage` `dep:T-7`
  状态：编码完成，conductor 复现通过（race 104s 全绿/全仓 lint 0/gofmt 净/全仓零 CGO/零内部依赖红线）。
  review 安排：双 reviewer 在途（correctness + arch）。
  reviewer 关注点：① ADR-0006 落盘顺序不可换序 ② singleflight 收敛与崩溃窗口 ③ GC 宽限期与 dry-run ④ 契约偏离两点（GC 回调集合形 vs 架构逐条查询；Close() 补入 Engine 接口）→ 需 architect 回写。
- **T-8** [P0] config 包 `role:dev-go-core` `area:internal/config` `dep:T-7`
  状态：编码完成，conductor 复现通过（race 3.1s 绿/零 CGO/gofmt 净/双键合并正确）→ 单 code-reviewer 在途。
  reviewer 关注点：① 全指针 raw schema 与 strict 解码的正确性 ② 秘密扫描路径（AdminPassword 不入 YAML/日志）③ env 名→路径映射的边界（嵌套/非法值）④ 覆盖率 90.5% 的薄弱分支。

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

## 🚫 阻塞（blocked）

（空）
