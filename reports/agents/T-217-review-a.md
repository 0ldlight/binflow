# T-217 复审报告（视角：架构一致性 + 测试覆盖）

结论：**REQUEST_CHANGES**（1 blocking）
日期：2026-08-23 ｜ 评审人：code-reviewer（review-a）｜ 对象：工作树未提交改动（9 文件 + 新测试 + 日志）

## 逐项核验结论

### 1. §7.1 符合性 — 通过（含裁可偏离的精确性证实）

- **族 4 例外**：§7.1 行原文「例外：POST·DELETE `/api/v1/permissions` 追加 m-holder 覆盖集臂（handler：CapSecurityWrite ∨ target.repos ⊆ 调用者 m 覆盖集，越界 403——FR-65）」与 ADR-0026 决策 3 逐字口径一致；日志 §3 的引述是忠实转述。实现在 permissions.go（`canManageAllRepos` + `errPermissionEditDenied`，create+delete 两臂），门式 = CapSecurityWrite ∨ 覆盖集，与清点表「handler：」前缀吻合——路由字面量维持 `manage:CapSecurityWrite` 反而使 m-holder 结构性到不了 handler，裁可偏离成立。
- **族 5 不开放**：`GET /api/repositories` 路由字面量仍 `CapRepoRead`（本票零触碰）；TestT217ManageHolderBoundaryLegs 钉 carol 列表 403 / 详情 200，与 §11.30「过滤列表 M8+」一致。
- **族 7 ∨ 式**：service.go `Usage` = `allow(r) || allow(m)`，role 短路使 admin/readonly_admin 经 r 臂通过——与「CanManageRepo(read) ∨ Can(r)」语义等价（user 臂下 CanManageRepo(user)≡Can(repo,"",m)，§3.4a 链③）；W26b r 臂回归由 t217（carol 200）+ t95 既有测试双钉。执行位置在 **service 用例**（路由 required:true），与族 7 行「现门 admin（usage 为 required + 用例判定）」注记一致——若路由挂 repoManage 反而会误伤 r-only 用户，实现选择正确。
- **守卫 26→24 精确性**：`git diff router.go` 中 enforce 行变更**恰两处**（POST `v1/permissions`、DELETE `v1/permissions/{name}`，均为 `manage:CapSecurityWrite` → `required:true`）；实文件 `manage: auth.Cap*` 计数 = 24、`repoManage: &repoManageGate` 计数 = 4，与守卫常量一致；全仓无残留「26 gates」陈旧引文；Makefile 仅注释（117-120），目标未动。**无第三处暗改**。
- **独立复跑**：`EXPECT=1 sh scripts/m7-rbac-matrix.sh` → 0 deviations（W04 POST permissions：roa 403 / user 403 / admin 201；R01~R12/W01~W07 全行符合，zero-side-effect guard OK）——handler 门保住矩阵判定，实证无旁路。

### 2. 分层与分层漂移 — 通过

- **service 门放宽形态**：CreateRepo/UpdateRepo → `requireAuthenticated`，授权门唯一归 httpapi（族 6 建仓臂分支 / 族 7 repoManage write）——无双门不一致；DeleteRepo 保留 `requireAdmin`，注释与 dispatchAPI 头注均写明「destructive extreme 纯纵深防御」，与「无 httpapi 门的内部调用路径保留原判定」稳妥侧一致。**非测试调用点核实**：`cmd/bf/main.go:371` 与 `internal/migrate/writer.go:56` 均走 HTTP client，不触 service——零提权证明的 grep 主张属实。
- **usage ∨-臂归属**：service 判（两次 `s.allow` = auth.Service.Can 同一决策点，不变量 2 无违）；路由 required-only，无双门漂移。
- **api.go 契约注释**：CreateRepo/UpdateRepo「Direct callers must gate themselves」、DeleteRepo「KEEPS the service-level admin door」、Usage ∨ 式——三处均与新行为一致；`ActionManage` 别名单点消费（service.Usage），内容面未泄漏 m。

### 3. 测试覆盖 vs V07~V10 — 覆盖到位，缺口一处（见 blocking）

- V07：TestT217ManageWireRoundTrip（wire 往返 + carol 编辑 + dave 读 200/写 403）；V08：BoundaryLegs（建仓臂/越界/半交集/users/revoke/族 7 越界/删仓/列表 403 + 字节级零副作用）；V09：Orthogonality（carol2 仅 manage：详情 200/建+换 target/quota 写/PUT 换臂 + 制品读写 403 + usage ∨ 三腿）；V10：既有套件（C22/C27/W19/W21/W26 族 + t215 三件）在本次 `go test -count=1` 全绿。readonly_admin 对新 handler 门的 403 由矩阵 W04 覆盖；readonly_admin usage 200 由 t215 既有测试覆盖。
- 覆盖集三例表驱动：TestT217CoverageMatrix 6 例（空覆盖/子集/恰好/半交集/不相交/空集拒绝）+ DELETE 三腿 + 存在性隐藏两腿。
- 击落-恢复 diff 一致性：9 文件通读，风格/结构无断裂；t217 测试复用 t215/t95 helper（t215Admin/t215As/t215Code/t95Upload），无重复实现、无半成品残留；build/vet/双包测试独立复跑全绿。

### 4. PRD 骨架勘误复核（PM 回写清单用）

- **勘误 1（PUT/200→POST/201）：成立**。PRD v1.1 AC1（`docs/prd/milestone-7.md:154`）与 §V07 骨架（:347-349）均为 `PUT …/permissions/t-app → 200`；router 仅注册 POST 集合路由（create-or-replace，恒 201，permissions.go:218），PUT 无路由 → E-26 404（routes_compat_test:183 钉）。architecture §7.1 族 4 行即 POST。
- **勘误 1 补充（建议一并回写）**：§V07 骨架 body 字段拼写与实际 wire 不符——骨架 `"repositories"`/`"includesPattern"` vs wire `repos`/`includePatterns`（permissions.go:40-41 json tag；t217PutTarget 同）。QA 脚本照骨架会 400。
- **勘误 2（列表 vs 详情）：PRD 原文本身无错**——AC3（:156）与 V09 骨架（:351）均写 `GET /api/repositories/app-local`（单仓详情路径已拼出）；歧义在 BOARD 票面措辞而非 PRD。可选在 V09 行追加「（全局列表 `GET /api/repositories` 仍 403，§11.30）」提示 QA，非必须。

### 5. 实跑取证

- `go test -count=1 ./internal/httpapi/... ./internal/repo/...` → ok（68.9s / 20.1s）
- `go vet ./internal/httpapi/... ./internal/repo/...` → 通过
- `bash -n scripts/m7-rbac-matrix.sh` → OK
- `EXPECT=1 sh scripts/m7-rbac-matrix.sh`（本机独立跑，throwaway 实例）→ exit 0，0 deviations
- 日志 §3 引文 vs §7.1/ADR-0026 原文：逐条核对，忠实。

## 必须修改（blocking）

1. **internal/httpapi/permissions.go:95-103（create 臂覆盖门）— 替换语义下覆盖集只验 body、不验存量 target，越界销毁可行**。场景：carol 的 m 覆盖集 = {app-local}；admin 建有 `t-other`（repos=[other-local]，授予他组 r/w）。carol `POST /api/v1/permissions`，body `{"name":"t-other","repos":["app-local"],"principals":{…}}` —— `canManageAllRepos` 只看 body.Repos → 通过；:193 `GetTarget(body.Name)` 仅取 CreatedAt；`PutTarget`（metadata/substores_auth.go:303-317）按名 UPSERT 并 **DELETE 该 target 全部 principal 行** → other-local 上的既有授权被整体销毁/改写。效果越出受权覆盖集（族 4 例外的「越界 403」承诺被替换路径绕过），且与 DELETE 臂形成内部不对称——DELETE 臂正确取存量 repo 集判定（日志 §1 自述），证明 create 臂的 replace 情形是未分析遗漏而非设计取舍；无测试腿、日志 §5 置信度清单亦未列此边。**建议改法**：非 security-writer 且 `GetTarget(body.Name)` 命中存量时，追加 `canManageAllRepos(存量 target 的 repos)`（等价于验 body ∪ 存量的子集关系），失败 403；并在 TestT217CoverageMatrix 增腿「in-coverage body 替换 out-of-coverage 存量 target 名 → 403 + 清点不变」。该收紧与 V07~V09 全部合法腿兼容（carol 换 t-app：存量 repos=[app-local] ⊆ 覆盖集，不受影响）。若 architect 裁定 body-only 即目标语义，须在 ADR-0026/§7.1 补记该边界并同样补测试——两者取一，不能维持现状静默。

## 建议改进（non-blocking）

1. **principal 存在性 oracle**：覆盖集内编辑者可经校验 400（"Unable to find user by name 'x'"）探测用户/组名存在性——族 3（security:read）本对 m-holder 隐藏用户清单，此为新开缝（pre-T-217 校验仅 security-writer 可达）。低危（仅存在性、无角色/邮箱），与 Artifactory target 编辑者可得校验反馈同形；建议 architect/PM 表态并在规格注记。
2. PM 回写清单补 §V07 骨架字段拼写（见上 §4 勘误 1 补充）。
3. readonly_admin 对 POST/DELETE permissions 的 403 文案平面变化（envelope → plain text）：日志 §6-5 已自报，矩阵/测试仅断言码；T-218 按服务端原文呈现即可，维持在案。
4. handlePermissionCreate 的门/decode 嵌套（decodeErr 双分支异状态码，:95-103）正确但致密，后续可提取小 helper 降漂移风险；非本票义务。
5. T-221（QA）：同意「usage ∨-臂翻转腿进 V 序列」，并建议随 blocking 修复补 replace-臂收紧腿。

## 遗留 4 项定性

1. **§3 偏离（路由字面量 ×2 + 守卫 26→24）**：conductor 已裁可；本复审证实变更精确收敛于两处字面量、无第三处暗改、守卫实质三断言保持——**裁可成立，无需回退**。
2. **PM 回写**：勘误 1 成立（含字段拼写补充）；勘误 2 PRD 原文无错、降级为可选提示。
3. **T-218 消费**：GET/POST 已接受并回显 manage（r/w/d/m 序，测试钉序），principals 面板复选与动词表可直接接线；`user.role.change` 词表归 T-218 不变。
4. **V 序列补 usage 腿**：同意（T-221），carol2 单臂 403 → ∨-臂 200 的翻转值得固定。

## 范围外发现

- 无（internal/auth 52 条 lint 为 T-211 基线债务线归 T-220，本票 delta=0，日志陈述属实）。
