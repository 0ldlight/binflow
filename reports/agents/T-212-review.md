# T-212 复审报告（视角: correctness + consistency，conductor 指定双重视角）

- 日期：2026-08-23 ｜ 结论：**APPROVE**
- 范围：internal/auth（全部改动 + rbac.go/rbac_test.go/rbac_idp_test.go 新文件）、internal/metadata（api/substores_auth/store + 两份 011 sql + 三份测试）、adapter/docker/token_test.go 6 行 stub。同树 T-229/T-216 改动未触碰。
- 权威契约：DECISIONS.md ADR-0026（Accepted，T-214 终裁版）+ architecture §3.4a 四不变量。

## 一、ADR-0026 四不变量逐条判定

### ① 无提权链 — PASS
- readonly_admin：CanManage 只放 {system:read, security:read, repo:read}（rbac.go:165，闭集枚举比较，无通配）；CanManageRepo(write) 恒 false（rbac.go:193）；Can 面 w/d/m 恒拒且 **target 短路在 PrincipalsFor 之前**（authorizer.go:41-45），组行/`trap` 授权行无效——`TestCanManageAction/readonly_admin_short-circuits_the_target_plane` 钉死（带 w 授权组行对其无效 + r 不依赖任何 target）。
- user + m：CanManage 对 user 恒 false（default 分支），m 无法换取任何 security:*/system:* 能力；httpapi 的 permission-target CRUD 目前仍在 `!p.Admin` 门后（security.go:330，T-215/217 迁移面），m-holder 今天不可自授 w——T-212 交付层无洞。
- idp 映射：阶梯封顶 admin_group（合法 admin 源），readonly_group 只产 readonly_admin；auto-create 落库与 principal 同 role。
- UpdateProfile 布尔臂：isAdmin=true → role='admin' 是既有 admin-only 门（security.go:330）的合法 promote 路径，非 readonly 自助可及。

### ② 单决策点 — PASS
- CanManage/CanManageRepo/Can 三面全在 `*Service`；`ManagementAuthorizer` facet 类型断言暴露（rbac.go:203-209），Authorizer 接口零改动。
- grep 全部 `p.Admin` 消费点（非测试）：httpapi security/replication/system_gc/middleware/repo/service/search = T-215 待迁路由门（routeAuth 面，本票不动，符合分票）；httpapi/session.go:214/232 = whoami/session wire 回显；docker catalog.go:162 与 scope.go:158 非 admin 分支都终止于 `Can()`；migrate/main.go 为导入映射/特判。无第二决策点。
- scope.go 词表 pull/push/delete/*，`scopeActionAll` 展开仅 r/w/d（scope.go:163-165），`ActionManage` 在 adapter/docker 零引用——不变量 3 同源成立。

### ③ m 只判 repos[]、正交于 r/w/d — PASS
- `targetListsRepo` 只 decode repos[]（authorizer.go:138-144）；`rowAllows` m 只查 CanManage（authorizer.go:156-157）；m⇏r/w/d 与 r/w/d⇏m 双向测试钉死（rbac_test.go:270-288）；includes/excludes 不参与（exclude `**` 全拒路径面时 m 仍授予，rbac_test.go:239-268）。`ItemPrincipals` 的 Manage 位与 Can 的 m 判定逐字对齐（groups.go 同谓词复用）。匿名永无 m。

### ④ wire 兼容 / is_admin 镜像 — PASS（红绿复核过）
- 四写路径全单语句保镜像：`Create` deriveRole（空 role 按 IsAdmin 派生）/ `SetRole` `SET role=?, is_admin=(role='admin')`（substores_auth.go:131-134）/ `UpdateProfile` CASE / `seedAdmin` 显式 'admin'。grep 全仓无第五个 is_admin/role 写入者（migrate EnsureUser 走 REST→Create→deriveRole）。
- UpdateProfile CASE 全组合枚举验证无漂移（promote/demote/readonly 保持均有测试）；**变异验证**：把 CASE 改成朴素 `ELSE 'user'` → `TestSetRoleMirrorInvariant/UpdateProfile_keeps_readonly_admin` 立即红（auditor 被静默降级到 user 被抓住）——agent 声称修掉的坑确有真测试钉住。

## 二、分项复核

- **readonly_admin 短路语义**：对齐 T-214 终裁（r 恒放行 / w/d/m 恒拒 / target 不参与 / 组合无效而非非法），非 PRD v1.0「同权走 target」。rbac.go/authorizer.go 注释明确引 T-214①。**变异验证**：删掉短路 case → `TestCanManageAction/readonly_admin_short-circuits` 红。
- **即时生效缝**：Verify（token.go:47,72）与 authenticateSession（session.go:88-115）同一条 owner 行重查读 role（与 enabled 同路径）；测试真实（真 store、真 token/cookie、SetRole 后同凭证复验三段 user→readonly_admin→user，CanManage 随动）。
- **idp_sync 阶梯**：OIDC `mapGroupsRole` 与 LDAP Bind 内同阶（admin_group > readonly_group > user，双命中取 admin）；空值回退：`claimsRole` 空_ROLE→Admin 布尔→user；`Claims.Admin = (role==admin)` 保持 M6 布尔语义；零配置零行为变化有专测（TestMapGroupsRoleLadder 8 例含零配置三态）。降级方向（目录除名重写回 user）有专测。
- **migration 011**：两方言语句逐字一致（公共子集）；回填 is_admin=1→admin 正确（含 seed admin）；幂等：rewind→再升级零变化 + ledger 恰 1 行；101 用户 <1s 实测通过；与 010（upload_sessions 表）零交互；两处既有 rewind 测试补列回退正确。
- **越区 stub**：token_test.go 恰 6 行 `fakeUsers.SetRole`，注释引 T-111 同文件先例（该先例注释在文件 98-99 行实存）。机械补齐属实。
- **clean-room**：rbac.go 语义逐条溯源自 ADR-0026/§3.4a 伪代码/docs/reverse/auth-model §4 与 rbac-model.md；纯 Go 闭集 switch/facet 形态，无反编译代码结构痕迹。

## 三、实跑记录

```
go build ./...                                          # ok
go vet ./internal/auth/... ./internal/metadata/...      # ok
go test -race -count=1 ./internal/auth/...              # ok 69.1s
go test -race -count=1 ./internal/metadata/...          # ok 48.9s
golangci-lint run ./internal/auth/... ./internal/metadata/...  # 52 issues，全部既有基线
```

- lint 52 条（agent 报 53，树内微小偏差）：errcheck4/gofmt2/ineffassign1/revive35/staticcheck2/unused8，分布 ldap_test/ldap/oidc_test/oidc/ldap_login_test/identity_test/auth_test——逐条核对均为 T-211 基线欠账（oidc.go 4 条 unused 为既有死代码；ldap.go:692 unused-param 是改名前 isAdminMember 同签名同告警的位移）。「本人新建/改动文件零新增告警」核实成立。
- 红绿变异两轮均红在精确钉住测试上；恢复后 `cmp` 字节级一致、grep 无残留、两包重跑绿。

## 四、范围外发现 / 移交（non-blocking，需 conductor 记录）

1. **[移交 T-215，配置接线前必修] adapter/docker/token.go:253 `authenticateForm`** 仍构造 `&Principal{Name, Admin: u.IsAdmin}`（不含 Role）——form 凭据腿（docker login 的 POST body 臂）上 readonly_admin 会折叠为 RoleUser，其全域 r 不生效（**欠授权、fail-closed 方向，非提权**）。当前树 readonly_admin 无任何 config/wire 赋值路径（T-212 遗留 1 明示 YAML 键未接）故不可达；但 T-215 一旦接通 `oidc.readonly_group`/`ldap.readonly_group`，此臂必须同步带上 Role（该文件属 T-216 在途区，本票不可触碰）。建议并入 T-215 AC 或 T-219/T-220 波次。
2. **[T-215 校验备忘] metadata.Create 对非空 Role 逐字落库**——若未来 wire 层传 Role='user' 且 IsAdmin=true 会写出不一致镜像（现无此调用方；ADR 已定 admin=true ⇔ adminRole=admin 冲突 400，归 T-215 handler 校验）。
3. **[T-220 顺手项] ProviderUser.Role 零值是 ""（非 "user"）**——手搓 ProviderUser 不带 Role 时 refresh 每次触发一次冗余 SetRole 写（真实 adapter 经 principalRole 归一，无生产影响；测试 mock 可见）。
4. **[既有 flake，与 T-212 无关] TestSessionTTLAbsoluteCapWins**（session_test.go:185，T-91 遗留文件，本票零改动）：ttl=1200ms 的墙钟测试，机器负载下 20 跑约 2 挂（mid-life 窗口被 argon2/调度吃穿）。建议放宽 ttl 或加单调余量，归 T-220 测试 robustness。

## 五、结论

AC 四条（migration 幂等<1s / 3×6 全表 + CanManageRepo + Can-m 表驱动 / idp admin 臂回归 / export 往返保真）全数满足且测试真实；四不变量逐条成立；越区 1 处已报备且属实。**APPROVE**，范围外发现第 1 条请务必随 T-215 跟踪。
