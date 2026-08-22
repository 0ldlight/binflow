# T-215 复审报告（视角：架构一致性 + 测试覆盖）

- 评审人：code-reviewer（a 视角，与并行正确性视角互补）
- 日期：2026-08-23 ｜ 对象：工作树未提交改动（16 文件：M 10 + 新测试 3 + 本日志）
- 结论：**APPROVE**（0 blocking / 7 non-blocking / 2 范围外交 conductor）

## 一、架构一致性

### 1.1 routeAuth 形态 vs §3.4a 求值链 — 通过

- `middleware.go:437-469`：`manage auth.ManagementCapability` + `repoManage *repoManageGate{repo, write}` 双字段。
  §7.1 草图写的是 `routeAuth{manage ManagementCapability | repoManage bool}`——实现的 struct 指针形态是
  必要精化（bool 载不动 (repoKey, write) 对；struct 承载防拆散错配），门位→能力映射与清点表逐行一致，
  形态差异属实现细节（见 N-1 建议 architect 回填一行）。
- **单决策点**成立：路由级判定全在 `authorize()` 一处（manage/repoManage 两分支同走
  `managementAllowed` → `auth.ManagementAuthorizer` facet → auth.Service.CanManage/CanManageRepo，
  `internal/auth/rbac.go:157/185`）；handler 侧分臂**仅**族 6 建仓臂一处
  （`repositories.go:386-393` `s.canManage(CapRepoWrite)`，§7.1 族 6 明文指定 handler 分臂）——最小。
  usage 保留既有用例层判定（required-only + allow(Can r)，§7.1 族 7 注记允许）。
- **fail-closed**：无 facet / nil authorizer / 能力拼写出闭集 → deny（`rbac.go:135 validCapability` +
  `middleware.go managementAllowed` 类型断言失败即 false），钉死在 `t215_guard_test.go`
  TestRepoManageGateFailsClosedWithoutFacet。facet 模式与既有 session/permission-view facet 同构。

### 1.2 30 门 vs §7.1 清点表终版 — 逐条核对通过

- 实测 `grep -c`：`manage: auth.` = **26**、`repoManage: &repoManageGate{` = **4**、残留 `admin: true` = **0**。
- 逐族比对（§7.1 表 1321-1330 行 vs router.go 迁移后）：族 1 六路由全 CapSystemRead（含 T-214 勘误①
  replications 补入）；族 2 四路由 CapSystemWrite（GC 含 dry-run，T-214① 否决生效——测试 W06
  `{"apply":false}` 钉死）；族 3 五路由 CapSecurityRead（含勘误② permissions 列表、token 列表不存在）；
  族 4 十臂 CapSecurityWrite + T-217 m-holder 例外只挂注释未提前实现（正确——FR-65 归 T-217）；
  族 5 列表 CapRepoRead；族 6 建仓臂 handler 分臂 + DELETE CapRepoWrite；族 7 四 repoManage 门
  （GET 详读/write:false、PUT·POST/write:true、?permissions/write:false）+ usage 用例域。与表零出入。

### 1.3 M1~M6 路由零漂移 — 抽查三组通过

- **M2 docker**（2 处）：`/v2/**` 路由零改动（router diff 无 /v2 行）；`token.go:253` form 腿仅增量
  `Role: Role(u.Role)`。admin/user 两角色判定前后同值（原无 Role 字段时 EffectiveRole 回落 Admin 位，
  折叠结果同为 admin/user；改动只修复 roa 折叠为 user 的欠授权——T-212 移交①语义正确）。
  scope 收窄是 ADR-0010 既有行为（response-level only，enforcement per-request），本票未动。
- **M4 治理**（2 处）：`?permissions` admin→repoManage{repo}。admin/user 判定不变（user 走 Can(m)，
  而 m 今日无 REST 授予缝——permissions.go 无 manage 字母，观察面零漂移；roa 新增读放行是 FR-64 目标行为）。
  403 文案 `administrator privileges required` 原样保留（t190 断言零改动）。
- **M6 replication**（4 处）：admin→system:read/write，admin 恒过 / user 恒拒，与旧布尔门逐角色同值。

### 1.4 internal/audit 越区 2 行 — 最小且必要，报备成立

`api.go` 仅 +`ActionUserRoleChange` 常量与 `Actions()` 词条各 1 行。词表是 GE-02 选择器/断言源与
web 前端镜像（`web/src/lib/governance.ts`）的数据面，影子常量会造词表缺项；T-97 增 ActionGroupMember
同先例。已核实 governance.ts:49 有 group.member 而无 user.role.change——遗留③的 T-218 挂钩真实存在。

## 二、分层纪律 — 通过

- httpapi 全部经 facet / 公共 API 消费：`auth.ManagementAuthorizer`、`auth.ParseRole`、
  `p.EffectiveRole()`、`metadata.Users().SetRole`、`metadata.User.Role` 公共字段。无跨包摸内部结构。
- config readonly_group 完整沿 T-179/T-210 模式：raw 指针字段（load.go）+ build 装配 + env 双拼写
  （`BINFLOW_AUTH_OIDC_READONLY_GROUP` 单下划线 + `BINFLOW_AUTH__OIDC__READONLY_GROUP` 通用形，
  config.go splitEnvKey 两处）+ 严格 schema（t215_readonly_group_test.go 第 4 测钉 typo 拒绝）。
- cmd 接线最小：main.go wireAuthProviders 两处各 +1 字段透传。
- wire 映射单点：adminRole↔users.role 映射只在 security.go 一处（quotaBytes 先例），符合 ADR-0026 决策 6。

## 三、测试覆盖充分性 — 通过（缺口均 non-blocking）

8 行为测试 vs V01~V06：V01（分配/回显/落库/布尔等价，含 6 组 create 冲突 400 + 3 组 partial 冲突 400，
kebab 拒绝在内）、V02（13 行读面矩阵，501 过门口径 + plain 403 对照）、V03（9 写臂 403 + 三实体逐字
零副作用 + 建仓臂 404 探针）、V04（同 Token 三段 403→200→403）、V05（越权/冲突/自建 403）、V06
（审计 4 迁移含布尔臂 + 默认角色建号不记事件）。守卫 3 测 + config 4 测。抽查确认：

- **kebab 拒绝**：`t215_rbac_routes_test.go:229` + 矩阵脚本注释双覆盖。
- **布尔/adminRole 混合写**：create 臂 4 组 + partial 臂 2 组冲突、unknown 值 1 组——充分。
- **守卫 vs T-217 空间**：T-217 三挂钩（m-holder 覆盖集臂、principals wire `manage` 字母、repo.Service
  第三道门）均不触 routeAuth 字面量 → 26/4 计数断言不误伤；计数变更须连同清点表改，注释已言明是
  刻意摩擦。闭集拼写断言只认 `auth.Cap*` 字面量——留了「改计数+改表」的合法扩展缝，无误伤。
- **矩阵 verdict 自洽**：实测 `bash -n` OK；`EXPECT=1 make test-m7-rbac-matrix` → **0 deviations，
  exit 0**（readonly_admin 11×200 + R12 501；user 全 403；admin 写全成；三实体 guard OK + 建仓臂
  repo absent）。行感知 verdict（R12 501 例外、guard VIOLATION 计入偏差、SKIP 随行数参数化）语义自洽。

覆盖缺口（non-blocking，N-2~N-4）：
- 未钉「roa 自铸 token 200」（族 8 不变式）——恰是 PRD 出入②待 PM 裁决处，建议补 1 行 pin 防
  未来「修正」静默翻转。
- 未直测「roa docker push 逐请求 403」（不变量 5 字面）——现有覆盖是组合式（auth rbac_test 钉
  Can(roa,w)=false + docker 包逐请求 Can 机制），建议 T-217/T-220 顺手补集成钉。
- 集合 POST（POST /api/security/users 带 name）的 adminRole 臂未直测——三臂共用 userCreate +
  resolveCreateRole，路径已覆盖，风险低。
- 守卫正则两处小盲区：`routeAuth\{[^}]*` 在嵌套 `repoManageGate{...}` 处截断（其后若再混入 admin:
  键逃逸扫描）；`manage:\s*auth\.(Cap\w+)` 不认变量传参。主防线是 admin 字段已物理删除（编译期），
  守卫属纵深，不阻塞。

## 四、遗留 5 项定性与去向 — 全部支持原建议，无升格项

1. **V01 POST-as-create 骨架**：核对 PRD:328-331 原文确为 `POST /api/security/users/alice … 201`；
   §7.1/T-97 定案 POST=部分更新（缺失 404）、PUT=create-or-replace（201）。**architecture 对、PRD 错**，
   验收已按 PUT/POST 两臂真实语义执行。PM 回写建议成立（PRD v1.1 该骨架行应改 PUT 或集合 POST）。
2. **「Token 签发」措辞**：PRD:118 变更面全拒清单含「Token 签发与吊销」；§7.1 族 8 + Q11 + ADR-0027
   裁定自铸不属管理面（为他人铸仍被 handler admin 检查 403，security.go:309/330 实存）。PRD 页脚
   自带「文本冲突以 ADR 为准」。**architecture 对**；若 PM 意图连自铸也要拒须新 ADR。回写建议成立。
3. **web 词表镜像 → T-218**：正确（见 §一.4 取证）。
4. **T-217 三挂钩**：均为 ADR-0026 决策 3 的 FR-65 面（m-holder 覆盖集臂属 handler 判定、repo.Service
   requireAdmin 是建/换仓第三道门、usage ∨-臂需 internal/repo 扩）——非本票 area，挂 T-217 正确。
   注意：usage 今日实现 = Can(r) 单臂，与清点表「CanManageRepo(read) ∨ Can(r)」的差仅对
   「持 m 无 r」者可见，而 m 无 REST 授予缝 → 今日不可观察。**T-217 必须落 ∨-臂**，请 conductor
   在 T-217 验收时点名。
5. **token handler p.Admin 未迁**：等价性核实成立（两处均为「非 admin」护栏判定，Admin ⇔ role=admin
   镜像由单语句写维护；生产 Principal 两字段恒一致）。Q11 护栏非路由门，T-219 顺势统一定位合理。

## 五、实跑记录

```
go test -count=1 ./internal/httpapi/... ./internal/config/...
  → ok internal/httpapi 76.082s；ok internal/config 0.765s
bash -n scripts/m7-rbac-matrix.sh            → OK
make build                                   → ok
EXPECT=1 make test-m7-rbac-matrix            → 12 读行（roa 11×200+R12 501 / user 全 403 /
                                               admin 200|501）、7 写行（roa/user 全 403），
                                               guard 四段 OK，"0 deviations"，exit 0
```

## 六、结论

**APPROVE**。30 门迁移与 §7.1 终版逐行一致、单决策点/fail-closed 成立、M1~M6 零漂移（三组抽查）、
分层与 config/cmd 模式合规、V01~V06 覆盖充分且矩阵 EXPECT 复跑绿、遗留 5 项定性全部合理。

### 必须修改（blocking）

（无）

### 建议改进（non-blocking）

- N-1 architecture §7.1:1310 草图 `repoManage bool` 与实现的 `*repoManageGate` 形态差一行回填
  （architect 顺手，或随 T-217 文档面一并）。
- N-2 补 pin：roa `POST /api/security/token` → 200（族 8 维持；PRD 出入②裁决前防静默翻转）。
- N-3 补集成钉：roa docker push 逐请求 403（不变量 5 字面；可挂 T-217/T-220）。
- N-4 集合 POST 臂 adminRole 直测 1 例（低风险，共用路径已覆盖）。
- N-5 守卫正则嵌套括号截断盲区（主防线是字段已删，纵深层可留）。
- N-6 usage ∨-臂缺口在 T-217 票描述中显式点名（本日志 §四.4 已留证据）。
- N-7 token handler p.Admin 双点在 T-219 票描述挂「迁 CanManage(security:write)」备注。

### 范围外发现（交 conductor）

- O-1 `Makefile:117-120` test-m7-rbac-matrix 帮助注释仍写「read-only-admin SKIPs until T-215 …
  flip on after T-215」——T-215 落地后已过时（列已不 SKIP、EXPECT 已开）；Makefile 不在本票
  文件清单，建议 conductor 以 chore 顺手更新。
- O-2 PRD v1.1 两处回写（V01 骨架改 PUT / 集合 POST；§4.1 变更面清单「Token 签发」措辞限定为
  「为他人签发」或删除）——PM 面，依据本日志 §四.1/§四.2。
