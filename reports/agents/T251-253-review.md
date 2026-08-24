# T-251 / T-253 复审报告（合并提交 a87250d）

- 评审人: code-reviewer（视角: correctness + security——用户删除护栏与可见集过滤）
- 日期: 2026-08-24 ｜ 对象: `a87250d`（feat: users-domain endpoints + batch usage）+ reports/agents/T-251.md / T-253.md
- 结论: **APPROVE**（0 blocking / 6 non-blocking；三条裁定建议见 §3）

## 1. 复审证据（全部实跑）

```
go test -race -run 'T251|T253|UserDelete|UsageBatch|DeleteUser|TestUserListWidening|TestUserDetailEnabledEcho|TestGroupMembershipViaUsers|TestRouteGateCapabilitySpellings' \
  ./internal/auth/... ./internal/httpapi/... ./internal/repo/... ./internal/metadata/... -count=1
  → ok auth 2.7s / ok httpapi 14.7s / ok repo 4.6s / ok metadata 3.7s
EXPECT=1 make test-m7-rbac-matrix → 0 deviations — PRD table clean, contract baseline clean (0 whitelisted)
golangci-lint（GOPATH/bin）auth/httpapi/metadata/audit/repo → 0 issues；go vet 同五包无输出；gofmt -l 空
node --check web/scripts/seed-m9.mjs → ok
```

**独立红绿（测后还原，internal/ 工作树已确认零残留）**：

1. 去掉自删护栏（`user_delete.go` self 分支加 `&& false`）→
   `TestDeleteUserGuards`（auth）、`TestUserDeleteGuards` + `TestUserDeleteLastAdminGuard`（httpapi）
   三处红（自删腿真被护栏承载）。还原后绿。
2. E1 改逐仓循环（`Usage().List` → `Repos().List` + 逐仓 `Usage().Get`）→
   `TestT253UsageBatchSingleQuery` 四臂全红（full/counts/named/filtered 各报
   "0 List calls, want 1" + 12/24/36 次 Get, want 0）——N+1 闸真实且对四种形态都设防。
   还原后绿。

## 2. 逐项复核

### 2.1 E4 三护栏完备性与检查序 —— 成立

- **序**：存在（404）→ 内置 → last-admin → 自删。存在性先行由 `users.Get` 承担；
  `DeleteCascade` 内部 rowcount=0 → 回滚 + `ErrUserNotFound`，竞态下（Get 与级联间被他人先删）
  仍映射 404 而非 500（sentinel 经 `errors.Is` 贯通）。
- **wire 可达性论证核实**：路由门 `CapSecurityWrite` 仅 `RoleAdmin` 持有
  （authorizer.go `CanManage`；readonly_admin 只得三 read 能力，httpapi 测试钉了 403）。
  若目标为最后一个 admin 且调用者过门，调用者必为目标自身 ⇒ 自删先查会使 last-admin 400
  在 HTTP 面不可达——先查 last-admin 的序正确且非死码（TestUserDeleteLastAdminGuard 两臂活体证明）。
- **绕序路径排查（组 admin 合并语义）**：BinFlow 无组级 admin——ADR-0030 E8 显式否决
  `adminPrivileges`；`Principal.EffectiveRole` 只读 `users.role`（组只进 repo 面 ACE 的
  `Can`），故 census（role 列）与 wire 门（同列）同源闭合，无第二提权路径。
  Artifactory REST 的「组员提权合并回显 admin:true」（gap-endpoints §1.2）在 BinFlow 无对应面。
  percent-escape 绕序不可能：名字先经 `Get` 探测，`%61dmin` 之流 404。
- **census 判据**：`role='admin'`（011 列）与鉴权面 `ParseRole` 同源；
  计数只可能偏少（fail-closed 拒删方向），不可能放行真·last-admin 的删除。
  readonly_admin 不计入 census（auth 测试钉死）——正确。
- **404 零副作用**：metadata `TestUserDeleteCascadeMissingZeroSideEffects` 钉了
  「孤儿 user-typed ACE 行在 404 路径不剥」（strip 同事务随回滚）——§2.2 步骤 1 的逐字兑现。
- **重复删除语义 / 竞窗**：见 §3 裁定 2、3。

### 2.2 级联完整性 —— 无遗漏引用面

DDL 逐表核对（migrations/sqlite）：

| 引用面 | 处置 | 核实 |
|---|---|---|
| tokens.username | FK `ON DELETE CASCADE`（001:67） | ✓ auth 级联测试 + wire 腿（token 删后即 401，Verify 逐请求重查 owner） |
| user_groups.username | FK CASCADE（004:39） | ✓ 组行本身保留（group_id FK 只对 groups） |
| web_sessions.username | FK CASCADE（004:52） | ✓ 会话即死（ADR 措辞「revoke」以行删实现，功能等价，记 non-blocking 注记） |
| permission_principals（无 users FK） | 显式剥 `principal_type='user' AND principal=?` | ✓ 同名 group-typed 行保留（auth 级联测试钉了同名双行） |
| audit_events.actor / nodes.created_by / docker created_by | 无 FK，保留 | ✓ 追责/归属面，正确不删 |
| remote_configs.username / replications.target_username | 上游/远端凭据，非本地账号 | ✓ 正确不级联 |
| upload_sessions | 无 username 列（state 为引擎不透明 JSON） | ✓ 无遗漏 |

postgres 迁移为占位（README 钉 sqlite-only），无方言分叉风险。级联在单事务
（strip → users 删 → commit；defer rollback），与 §14.1「同事务」一致。

### 2.3 E1 可见集零泄露 —— 成立

- **同源 seam**：`UsageBatch` 的判定表达式与单仓 `Usage` 逐字节相同
  （`allow(read) ∨ allow(m)`，service.go 2271 vs 2325）；§14.1 的「CanManageRepo(read) ∨ Can(r)」
  对普通 user 展开即 `Can(m) ∨ Can(r)`，两臂一致，无重实现漂移。测试钉了
  carol（纯 m）batch 行与 `Usage(r03)` 放行 / `Usage(r00)` 403 的双向一致。
- **同形性**：点名不可见名与点名未知名产出逐字节同形响应
  （httpapi `invisible key is indistinguishable from unknown` 臂 + service 臂 + 真实栈 curl 双面）。
  残余信道仅剩微秒级计时差（存在名多 1~2 条授权查询）——与 Artifactory 同形，量级不可用，记观察。
- **`?repos=`**：present-but-empty = `[]`（字面交集读法）；重复/空白折叠；多值参数累积；均在约。
  ADR-0030 E1 行明文含 `?repos=a,b,c`——契约内。
- **N+1 闸真实性**：装饰器计数器断言任一形态恰 1 次 `List`、0 次逐仓 `Get`；
  红绿突变证实闸会抓回归（§1）。
- **?include 未知值 400**（errors[] 信封）镜像 E6 治理族惯例，契约内。

### 2.4 E2 additive 加宽 —— 零破坏（抽 3 处消费者）

1. 控制台 `web/src/pages/security/api.ts` `UserListItem` 仍瘦形态 `{name,uri,realm}`——
   TS 结构类型 + 运行时忽略未知字段，加宽零破坏（消费改造归 T-257/T-259，已登记）。
2. `web/e2e/m8/users-groups.spec.ts:242` 对列表仅断言 `status 200`——不受加宽影响。
3. `web/e2e/security.spec.ts:153-157` detail 断言为子字段级
   （`expect(uj.groups).toEqual([group])`）——E3 增 `enabled` 不触碰。
   另 `t97_groups_test.go` W40 断言按 ADR-0030 E2 姿态取代（仍禁 `admin` 布尔与 password 形字段，
   jane 空组钉 `[]` 非 null），属契约演进要求而非破坏。
- **空组恒 `[]` vs 条件省略**：gap-endpoints §1.2 自己把「仅非空才设置」标为
  **中置信序列化形态**（字段本身高置信）；§14.1 E2 明文钉「空 = `[]`」。
  有意差异、已登记、且有消费者理由（`groups[]` 被前端 range）——无需动作。

### 2.5 clean-room

文案逐字来自 docs/reverse 行为规格（gap-endpoints/auth-model/rbac-model 引文），
属 ADR-0001 许可路径；实现为 BinFlow 自有架构（sentinel 错误 + seam + adapter），
无逐行翻译形态。无嫌疑。

## 3. 三裁定建议（conductor 待裁项）

### 裁定 1：DELETE 404 带 `User not found` 纯文本 vs gap-endpoints 高置信「无 body」

**建议：维持现状（带文本），对齐家族而非对齐 Artifactory。** 理由：
① §14.1 E4（Accepted ADR-0030 展开）已钉「404 `User not found`（与组面同族）」，是 BinFlow 权威契约；
② BinFlow 既有 404 家族全带文本——GET users/{name} 404 = `User not found`（security.go 两处）、
groups 面 = `Group not found`（groups.go 四处）；DELETE 独做无 body 反而制造家族内异形；
③ 兼容性代价为零：客户端按状态码分流不受影响，读 404 body 的客户端罕见且只会多得分隔文本。
动作：T-271 api-reference 登记该 wire 偏差（Artifactory 无 body）即可。

### 裁定 2：重复删除 404 vs Artifactory 吞 404 幂等

**建议：维持 404（确定性 probe），按「非幂等」显式登记。** 理由：
① Artifactory 的「Access 404 吞掉视为成功」是其 REST 面不 pre-probe 的并发窗口产物
（gap-endpoints §2.3 自证：UI 面才依赖吞 404）；BinFlow 单事务 probe 使重复删**确定性** 404，
同一规则（未知名 404）无需特例。② 幂等收益方是重试客户端——应由客户端把
「重试时 404」映射为成功等价，而非服务端伪造 200。动作：T-257 控制台删除重试逻辑与
T-271 文档均按此登记（「DELETE 非幂等；重复删/并发删已成功 → 404」）。

### 裁定 3：last-admin census 竞窗

**独立评估：窗口真实存在，agent 的「单实例单写者下不可达」论证不成立；但可达路径无攻击价值，建议 P2 follow-up 而非返工。**
- 事实：census（`Users().List`）在 `DeleteCascade` 事务**之外**；连接池 `MaxOpenConns=NumCPU`
  （store.go:92），单实例内两个并发 HTTP 请求可按 census₁/census₂/cascade₁/cascade₂ 交错，
  双双通过护栏后双双级联 → 零 admin。sqlite 只序列化各写事务，管不到事务外的读。
- 可达形状：仅剩 A、B 两个 admin（内置 admin 已降级）且 A 删 B 与 B 删 A 同时在窗内。
  攻击者角度无价值（能同时支配两个 admin 账号者，制造零 admin 只会失去自己已有的完全控制）；
  纯运营误触面（两管理员互删 / 脚本化批量删竞速）。
- 后果放大项：终态下内置 `admin` 行**存在但已降级**，`BINFLOW_ADMIN_PASSWORD` 重种只在
  「admin 行不存在」时生效（store.go seedAdmin）——环境变量恢复路径死亡，只能 sqlite 手术
  `UPDATE users SET role='admin'`。这提高了 impact 评级。
- 建议动作（P2 follow-up ticket）：把 census 折进 `DeleteCascade` 事务——受戒单语句
  `DELETE FROM users WHERE username=? AND (role<>'admin' OR (SELECT COUNT(*) FROM users WHERE role='admin' AND username<>?)>1)`，
  rowcount=0 时区分映射 ErrUserNotFound/ErrDeleteLastAdmin；同时修正
  `user_delete.go:84-89` 注释与 T-251.md 遗留 5 的措辞：「窄窗、需双管理员并发互删」
  而非「不可达」。

## 4. 意见清单

### 必须修改（blocking）

- 无。

### 建议改进（non-blocking）

1. `internal/auth/user_delete.go:84-89` + `reports/agents/T-251.md` 遗留 5：竞窗论证措辞
   「单实例单写者下不可达」不准确（见 §3 裁定 3）——修正注释/报告措辞并立 P2 follow-up
   （census 入事务）。
2. 锁定终态恢复路径缺失应随 1 一并登记：内置 admin 存在但降级时
   `BINFLOW_ADMIN_PASSWORD` 不生效，运维手册（T-271/部署文档）需写明 DB 手术恢复法。
3. `internal/auth/user_delete_test.go:68` 用例名 "reserved anonymous name is refused"
   实际断言 `ErrUserNotFound`（404 兜手建行才是 400）——名实轻微不符，改名即可。
4. E4 web_sessions 以行删实现 ADR 的「全量 revoke」——功能等价（Verify 失败闭），
   建议在 ADR/§14.1 勘误一行措辞，免后来者找「revoked_at 标记」。
5. E1 计时侧信道（存在但不可见的点名仓多付 1~2 条授权查询）——量级微秒、Artifactory 同形，
   仅记录，不建议动作。
6. architecture §7.1 [M7] 门清单表需补第 25 个 manage 门（t215 计数已随动）——
   T-251 遗留 2 已登记，归 architect/conductor。

### 范围外发现

- 工作树当前无 T-253 fake 桩补丁残留（`internal/` 零 diff）；adapter/docker blob_test 的
  `UsageBatch` fake 缺口在 a87250d 已闭合（`go vet ./...` 五包 + repo 包编译干净）。
  若 conductor 仍见在途补丁，以工作树实际状态为准。
