# 评审报告 T-97（视角: correctness + security）

结论: **REQUEST_CHANGES**

- 评审对象：commit 86f879b 中 T-97 面（internal/auth/{groups,api,authenticator,authorizer,token,deps}.go、internal/metadata/{api,substores_auth}.go、internal/audit/api.go、internal/httpapi/{groups,security,permissions,storage,router,server}.go + 三份新测试）。T-94 的 GC 面（system_gc.go/t94_gc_test.go/harness GC 改动）按派单不评。
- 验证（实际执行）：`go build ./...` ✓；`go vet ./internal/auth/... ./internal/httpapi/... ./internal/metadata/... ./internal/audit/...` ✓；`go test -count=1 ./internal/auth/ ./internal/metadata/` → ok 7.0s / 4.6s；httpapi 定向（TestGroup*/TestStoragePermissionsView/TestCompatPlaneE26 等）→ ok 3.6s；`go test -count=1 ./internal/httpapi/` 全量 → ok 67.0s。
- clean-room 抽查：改动为消费者侧惯用 Go（consumer 接口、table-driven、注释锚 AC），与 reverse-src 无逐行/逐结构翻译嫌疑；逆向产物仅经由 docs/reverse 的行为规格面进入（wire shape/文案），合规。

## 两条遗留的裁定

- **遗留①（?permissions 映射方向）：裁定翻转** → 见 B1。reverse-src 实证 key=主体名、value=权限字母集合；rest-api.md §3 括注（及 PRD FR-27 行「r/w/d 位映射」同源转述）为误读，需勘误（范围外）。
- **遗留②（匿名调用）：裁定收紧** → 见 B2。真实端点门是 `canManage`，不是读门；匿名（乃至普通读者）在真机上拿不到该视图。至少 `required: true`，推荐 admin 门。

## 必须修改（blocking）

### B1 — `?permissions` 视图键值方向反了（与参考实现及官方 REST shape 不兼容）

- 位置：`internal/httpapi/storage.go:214-229`（`bitsToNames`）、`internal/httpapi/storage.go:126-136`（`permissionsView` 形态与注释）、`internal/httpapi/t97_groups_test.go:539-563`（W21 断言钉死了错误方向）
- 证据（reverse-src，只读取证）：
  - `reverse-src/.../addon/rest/RestAddonImpl.java:2240-2286` `getItemPermissions`：构造 `Map<String, Set<String>>`，逐主体调 `appendPrincipalsAndPermissions(usersMap, userInfo.getUsername(), permissionsAsString)`（:2265 users / :2276 groups）——**map key = 主体名，value = 权限字母集合**；
  - `SecurityModelPopulator.java:235-256` `getPermissionsAsString(canRead, canAnnotate, canDeploy, canDelete, canManage)` 逐布尔拼字母；
  - `RestAddonImpl.java:2637-2641` `appendPrincipalsAndPermissions`：**空集合跳过**（主体无任何权限则不出现在视图）。
  - 官方公开 REST 文档（Get Item Permissions）同形：`"principals":{"users":{"<name>":["r","w",...]}}`。按项目规则「有公开规范的协议以官方文档为准」。
- 失败场景：现实现输出 `{"users":{"r":["jane"]},"groups":{"w":["devs"]}}`；任何按官方 shape 解析的真实客户端把 `"r"`/`"w"` 当主体名、把数组元素当权限位，全量错位——SE-08 的兼容目标（P1）直接落空。
- 修复建议：`bitsToNames` 反转为 namesToBits：输出 `map[主体名][]字母`（"r"/"w"/"d"，固定序即可），**空位集合的主体不出现**；同批改 `permissionsView` 注释、`TestStoragePermissionsView` W21 断言（`users["jane"]==["r"]`、`groups["devs"]` 含 "r"+"w"）与工作日志遗留③口径。
- 范围外：`docs/reverse/rest-api.md` §3 括注与 `docs/prd/milestone-4.md` FR-27「r/w/d 位映射」句的勘误回写（conductor → reverse-engineer / PM）；T-103 W21 脚本若断言方向需同批。

### B2 — `?permissions` 门宽于参考实现：匿名可枚举主体与权限（安全）

- 位置：`internal/httpapi/router.go:382-389`（该分支 `routeAuth{}` 空门 = 内容面读门 + 匿名策略）
- 证据：`RestAddonImpl.java:2242-2244` 端点第一道门是 `if (!this.authorizationService.canManage(repoPath)) throw new AuthorizationRestException();` —— **manage 位持有者专属**，非读门；匿名与普通读者在真机上均被拒。
- 失败场景：`anonymous_access=true` 实例上，任意未认证访客可对每个可读路径枚举**全部用户名/组名及其 r/w/d 有效权限**——(a) 账号枚举：本系统登录面刻意以均一 401 文案避免存在性泄露，此端点整体洞穿该防线；(b) 侦察：写权限分布图是定向撞库/投毒的前置情报。普通读者同样可枚举（真机不可）。
- 修复建议：BinFlow 动作集无 manage（M1 read/write/delete），最近映射 = 管理面 admin 门：该分支改 `routeAuth{required: true, admin: true}`（一行）；handler 内保留 item 解析（404/400 形态不变，读门退为纵深冗余）；补「匿名 401 / 非 admin 403」断言，并把 `TestStoragePermissionsView` 的 unprivileged 子测改为管理面门语义（handler 注释「与 item GET 完全同源」同步改）。附带收益：repo 存在性/类型（404/400）不再先于认证对匿名泄露。
- 若 conductor 坚持 K1 暂行宽松，底线是 `required: true`（认证专属）——匿名绝不可见。

## 建议改进（non-blocking）

1. **user_groups 缺 username 前缀索引（热路径全表扫描）**：`migrations/sqlite/004_console_governance.sql:34-38` PK=(group_id, username)，`GroupsOfUser` 的 `WHERE ug.username = ?`（`substores_auth.go` 同名实现于 `substores_console.go:169-173`）与 `SetUserGroups` 的 DELETE 均无法走索引；T-97 把该表变成**每次认证必读**的最热路径。建议新迁移 005 补 `CREATE INDEX idx_user_groups_username ON user_groups(username)`（sqlite 实装 + postgres 占位，照模板）。
2. **组删除 409 的 TOCTOU**：`groups.go:213-224` GroupReferences 查询与 Delete 非原子；与「POST target 引用该组」并发时留悬挂 group 行，同名组重建即复活旧授权。两侧均 admin-only、窗口极小，建议 `groupStore.Delete` 语句内 `AND NOT EXISTS (SELECT 1 FROM permission_principals p WHERE p.principal_type='group' AND p.principal=name)` 守卫（0 行受影响映射回 409/404）。
3. **group.member 审计噪声**（遗留⑤取舍，可接受）：userCreate 每次都落一条（含 groups 缺省的新建用户），PUT 重放未变值也记录。如降噪需前置读成员集比对，登记即可。
4. **permission.* 审计 detail 取证价值弱**：`permissions.go:148-156` 仅 `{"name","principals":N}` 计数。建议记 principals 名单+动作（非凭据数据，Redact 链仍兜底）。
5. **PUT replace 多步非事务**：UpdatePassword→UpdateProfile→SetUserGroups 中途失败留半改状态（如新口令+旧 email）；管理面可重试，登记即可。
6. **非 local 仓码值待勘误**：真机为 `ItemNotFoundRuntimeException("Unable to find local repository '...'")`（RestAddonImpl.java:2248-2250，疑 404），BinFlow 按内部 spec 400。随 B1 勘误一并交逆向核实，暂不动。

## 重点审查区逐项结论（正确性面）

1. **权限并集语义** ✓：`rowCoversPrincipal`（auth/groups.go:72-81）user 行 EqualFold（M1 语义不变）∪ group 行 `slices.Contains(p.Groups,...)` 精确匹配——组名入口两处强校验（建组 `^[a-z][a-z0-9._-]*$`、target 侧存在性 400）使精确匹配闭合无大小写逃逸；exclude 优先 / include 空=全含 / folder 尾斜杠经 `targetCovers` 单谓词共享（authorizer.go:83-98），Can 与 ItemPrincipals 不可能分叉；`TestCanGroupUnion`/`TestItemPrincipals`（含 view-agrees-with-Can 子测）覆盖到位。
2. **fail-closed 面** ✓ 且**认可其取舍**：`fillGroups`（auth/groups.go:55-66）组侧 fail-closed（查询失败→空组继续+Error 日志）方向正确——富集读失败升级为整凭据拒绝会让组表故障炸掉与组无关的全部账号，而现取舍下组授权绝不因故障放开、直接授权不受牵连，`TestGroupsFillFailsClosed` 钉死两腿。三臂等价：`Authenticate` 出口单点 fill（authenticator.go:131-140）覆盖 basic/token/session，`Service.Verify`（token.go:142-151，docker 交换缝）同 fill；全仓 grep 无绕行 `verifier.Verify` 的调用点。
3. **groups CRUD 安全** ✓：校验链（非空/≤64/字符集/保留字）集中在 `validateGroupName`，错误走用户管理纯文本层、admin 门在路由层；409 文案列 target 名仅 admin 可达，无越权信息泄露面。TOCTOU 见 N2。
4. **users 201 翻转 + 部分更新提权面** ✓：PUT 两态皆 201 与 auth-model §1.3-⑪ 一致（M1 409 为简化，R5 流程合规，自有测试同批注明）；`POST /users/{name}` 白名单=email/password/admin/groups 四指针字段，路由 admin 门（router.go:440-445）、changePassword 精确路由先行（:416）——admin 之外无提权缝；`groups:[]` 与缺省 groups 经指针区分（清空 vs 不动）语义正确；GET 永无口令字段（测试断言钉死）。
5. **审计** ✓：group.create/update/delete/member + permission.create/update/delete 全落（curl 矩阵复核），词表登记合规；detail 形态见 N3/N4。
6. **共享 targetCovers 重构回归** ✓：Can 重构后既有 authorizer_test 全绿（含「group 行仅经 Principal.Groups 生效」的 M1 姿势保断言），FR-27-AC10 的 C22/C27 复跑归 T-105 无碍。

## 范围外发现（交 conductor）

- `docs/reverse/rest-api.md` §3 `?permissions` 括注方向勘误（B1 证据链已备，可直接采信）。
- `docs/prd/milestone-4.md` FR-27「r/w/d 位映射」句 + §8 W21 脚本（如断言方向）同批回写；SE-08 行建议补 auth 门注记（canManage→admin 映射，随 B2）。
- user 删除端点（`DELETE /api/security/users/{name}`）M4 未做，FR-28 UI 依赖——遗留④维持登记。
