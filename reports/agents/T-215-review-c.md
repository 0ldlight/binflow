# T-215 review（视角：correctness — 并发/错误处理/越权）

- 评审人：code-reviewer（correctness）｜ 日期：2026-08-23｜ 结论：**REQUEST_CHANGES**（blocking 1，non-blocking 4）
- 对象：工作树未提交改动（14 个源文件 + 3 个新测试 + 矩阵脚本）；施工图 architecture §7.1 [M7] 终版 + ADR-0026 + PRD FR-64 V01~V06。
- 方法：全量 diff 通读 + 上下游（internal/auth/rbac.go、internal/metadata/substores_auth.go、internal/repo/service.go requireAdmin）核对 + 只读命令取证 + 独立红绿探针（测后 md5 校验精确还原，grep 无残留）。

## 一、复审分项判定

### 1. 迁移完备性（30 门）— PASS
- 独立 grep `admin\s*:\s*true` 于 internal/ 与 cmd/（含测试）：仅注释命中（security.go:972 注释、t215_guard_test.go:22 注释），**门位零残留**；`req.admin` 在 middleware.go 零残留。
- 逐路由对照 §7.1（30/30，非抽查）：族 1 = 6×CapSystemRead（health/stats/audit/migration/replications 列表/replication status）；族 2 = 4×CapSystemWrite（GC **含 dry-run**、migration/start、replications POST/DELETE）；族 3 = 5×CapSecurityRead；族 4 = 9×CapSecurityWrite（token/revoke 带 oauth 位；m-holder ∨-臂按表挂 T-217）；族 5 = 1×CapRepoRead；族 6 = DELETE 1×CapRepoWrite + PUT 建仓臂 handler 内 CapRepoWrite 分支；族 7 = 4×repoManage（GET 详情 read / PUT·POST write / ?permissions read；usage 维持 required-only + 用例层 Can∨CanManageRepo(read)）。计数 26 manage + 4 repoManage 与守卫常量、§7.1 表一致。
- migration 过门 501：Go 矩阵与脚本双证（readonly_admin 501 / user 403 —— 门先判、装配后答）。

### 2. fail-closed 面 — PASS
- `managementAllowed`：nil authorizer、无 facet 的 authorizer 均 deny（TestRepoManageGateFailsClosedWithoutFacet + rbac.go `p==nil`/未知能力双 fail-closed，T-212 全表钉死）。
- readonly_admin × 六能力：模型面 3×6 全表（auth/rbac_test.go TestCanManageCapabilityMatrix，含匿名/未知能力退化行）；路由面 13 行表（11×200 + migration 501 + ?permissions 200；user 同行全 403 佐证门在判）。
- repoManage 语义（admin true / readonly !write / user 走 m）：CanManageRepo 实现与 ADR 决策 3 逐字一致；TestCanManageRepoMatrix + 路由行为双钉。

### 3. adminRole wire — PASS
- `resolveCreateRole` + 部分更新臂：冲突 400 六例（含 admin 缺省=false 配 adminRole=admin、kebab、未知值）纯文本层；非 admin 写 403（self-promotion 用例）；GET 回显 `adminRole` + whoami/session 回显 `p.EffectiveRole()`；DB 落值 + 镜像不变量（readonly_admin↔admin 往返）经真实 store 断言。
- **即时生效真实**：TestT215RoleImmediateEffectOnLiveToken = 真 httptest 栈、真 mint、同一 Bearer 三段 403→200→403，无重启无换发。
- 布尔臂审计：auditee `{"admin":true}` 升降同样留 user.role.change，事件形态与显式臂一致；UpdateProfile 的 CASE（substores_auth.go:113）同语句维护 role↔is_admin 镜像，布尔升降不可能产生分叉行（我逐条核过 promote/demote/readonly 三分支与 `replaceLandedRole` 等价）。

### 4. authenticateForm 修复（T-212 移交①）— PASS
- token.go:260 principal 带 `Role(u.Role)`；readonly_admin form 腿 pull 存活 / push 剥除 + plain 负控；scope 词表未触（scope.go 无 diff，m 永不出 docker 面）。

### 5. 零副作用 — PASS
- 写面测试对 user/repo(含 quota)/group/permissions 实体做 **GET body 逐字比对**（非仅状态码）；建仓臂后 repo 确认 404。脚本守卫同口径（三实体 cmp + create-arm repo absent）在真实二进制上 OK。

### 6. 实跑记录
```
go build ./... && go vet ./...                          # ok / ok
go test -race -count=1 ./internal/httpapi/... ./internal/auth/...
                          ./internal/adapter/docker/... # ok 175.9s / 72.1s / 26.4s
golangci-lint run（触及五包含 cmd）                      # 0 issues
gofmt -l（触及目录）                                     # 0
scripts/m7-rbac-matrix.sh                               # 观察模式：roa 11读200+migration501、
                                                        # 7写全403；user 全403；admin 读200/501、
                                                        # 写200|201；guard 全 OK
EXPECT=1 make test-m7-rbac-matrix                       # 0 deviations, exit 0
```

### 7. 独立红绿（测后 md5 还原、grep PROBE 零残留、build 复验）
- ① router.go 临时裸 `admin: true` 门（连同字段临时恢复）→ TestRouteGatesCarryNoBareAdminBoolean **红**（咬合确认）→ 还原。
- ② 临时删除 repositories.go 建仓臂 CapRepoWrite 分支 → `TestT215*` 全绿，**进而全包 ./internal/httpapi + ./internal/repo 亦全绿** —— 无任何测试咬合该分支 → 见 B1。

### 8. clean-room 抽查 — PASS
- Go 侧形态溯源自 §7.1/ADR-0026（其依据 docs/reverse/rbac-model.md 行为规格）。reverse-src 检索仅见 Artifactory 自有 wire 词 "adminRole" 在反编译 JS bundle 的名字级碰撞（ADR-0026 决策 6 明示命名对齐），无 ManagementCapability/CanManageRepo/repoManageGate 等结构或逐行对应。

## 二、必须修改（blocking）

- **B1｜internal/httpapi/repositories.go:386-396（族 6 建仓臂）无判别性测试**。红绿探针实证：删除 `if !s.canManage(r.Context(), p, auth.CapRepoWrite)` 整个分臂后 `./internal/httpapi` 与 `./internal/repo` 全套仍绿——readonly_admin/普通用户的 W01 403 全部来自路由门（repoManage write → CanManageRepo），请求根本到不了该分支；全套测试中**零 m-holder 夹具**（`CanManage: true` 仅出现在 internal/auth 的 T-212 测试）。该分支是 ADR-0026 决策 3「建/删仓不下放 m」在 handler 侧的唯一执行点：今天尚有 repo.Service.CreateRepo 的 `requireAdmin(p.Admin)` 第二道门兜底（无活提权），但 T-217 计划为替换臂放宽 service 门，届时此分支即成建仓臂唯一守门——无回归钉 = 静默失守面，且 dev 日志「族 6 分臂…矩阵 7 写全 403 覆盖」的表述会误导后人以为已测。
  → 建议改法：新增一个表驱动测试，经 metadata 缝（`h.md` Permissions().PutTarget + `PermissionPrincipal{CanManage:true}`，T-212 列已备）造 m-holder，断言 ①`PUT /api/repositories/<不存在键>` → 403（本分支）；②`GET /api/repositories/{repo}` → 200（m 读面过 repoManage 门）；顺手 ③`?permissions` 视图渲染出 `m` 字母（一并闭 N1）。夹具三断言均为真实栈路径，约 40 行。

## 三、建议改进（non-blocking）

- N1｜storage.go principalLetters 的 `m` 渲染无断言（T-97 TestStoragePermissionsView 只钉 r/w；数据面 ItemPrincipals.Manage 是 T-212 已测，渲染腿裸奔）——并入 B1 夹具即可。
- N2｜Makefile:117-120 `test-m7-rbac-matrix` 注释陈旧（"read-only-admin SKIPs until T-215 wires the role field"——角色已落地且拼写改 snake；Makefile 非本票 area，交 conductor 顺手或归 T-218）。
- N3｜web/src/lib/governance.ts 审计词表镜像缺 `user.role.change`——dev 日志遗留③已登记归 T-218，复核确认仍开放（范围外，维持移交）。
- N4｜scripts/m7-rbac-matrix.sh --expect 仅把 R12 列入 501 白名单，而头注 prose 提及 replication 对的 501 姿态——若对无 replication 装配的构建跑 verdict，R09/R10 会假红（脚本自举 sqlite 实例不可达该态，仅姿态备注）。

## 四、范围外发现（交 conductor）

- 无新增（N2/N3 即日志已报备项的复核确认）。

## 五、附：探针还原证明

三份被临时改动文件 md5 与备份一致（middleware 9a040692…、router b802db5b…、repositories 9df87619…），`grep -rn PROBE-T215REVIEW internal/` = 0，`go build ./...` 复验通过，guard 测试复绿。未写 BOARD.md，未提交 git。
