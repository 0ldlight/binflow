# T-219 评审报告（视角: correctness + security——token 铸造加固面）

- 结论: **APPROVE**
- 日期: 2026-08-23 ｜ role: code-reviewer ｜ 契约: ADR-0027（Accepted 修订版）决策 1~7
- 范围: internal/config（两键）、internal/auth（stepup.go + SessionHash 台账接线）、internal/httpapi（stepup.go + security/oidc/server）、测试三件；web/ 改动（T-218 在途）未触碰未评审。

## 逐项核查

### 1. mint grant 台账安全 — 通过

- **256-bit 熵**：`grantBytes = 32`（crypto/rand）hex 64 字符；`TestStepUpGrantLifecycle` 钉死长度 64。
- **只存 sha256**：`stepUpLedger.grants` 键 = `grantHash(grant)`（sha256 hex）；明文仅出现一次——callback 302 fragment。全量 grep 确认：签发 INFO 日志只带 user+ttl；审计 detail（tokenIssueDetail）无 grant；无任何持久层落明文。
- **同人/同 session 绑定不可绕**：`ConsumeStepUpGrant` 判 `g.username == username && g.sessionHash == sessionHash`；跨 session 同人 → 401（`TestT219GrantBoundToSession` e2e + `TestStepUpGrantWrongSessionBinding` 单元）；换 user → 401（lifecycle）。绕过需 sha256 原像。`Principal.SessionHash` = 存储 digest（`GetBySHA256` 同键），非 cookie 明文。
- **burn-on-presentation 原子性**：mutex 内「读+删」一个临界区完成；红绿突变①证实测试有效；临时 64 goroutine 并发消费探针（-race）恰一次成功、事后零复活。
- **TTL 域 [60,3600] 无条件拒启动**：validate.go 独立于开关；59/3601/开关 off+5/env 路径四腿全测（`TestTokenStepUpGrantTTLDomain`、`TestTokenStepUpEnvOutOfRangeRefusesBoot`）；未知 env 拼写拒收（`TestTokenStepUpUnknownEnvIsRejected`）。
- **签发清扫不复活**：sweep 只删过期（`TestStepUpGrantSweepOnIssue`）；台账泄涨有界（签发率 × TTL）。
- **进程内单例并发安全**：`New()` 初始化、`With*` 克隆共享指针（`TestStepUpLedgerSharedAcrossWithClones`）；三包 -race 全绿。

### 2. 触发条件完备性 — 通过

- 判定点唯一：`stepUpTriggered = TokenStepUp ∧ p.ViaSession ∧ !adminMinter`（httpapi/stepup.go:61）。Basic/Bearer/API-key 臂 `ViaSession=false` 零触发；`/v2/token` 在 docker adapter 自有 handler（`TestT219DockerTokenPlaneUntouched` 钉死 + Basic 同栈豁免）；匿名被路由 `required:true` 401 在前。
- **默认 off 与 HEAD 等价**（V25 四护栏）：全部差异点核过——① adminMinter（CanManage(CapSecurityWrite)）与旧 `p.Admin` 三角色逐字等价（EffectiveRole：admin→true；readonly_admin/user→false，CapSecurityWrite 仅 admin 持有），`TestT190*` 全家（自铸/三臂他人 403/TTL 帽/禁用行 401/admin 不受限）复跑全绿 + `TestT219SwitchOffKeepsQ11Posture` 钉 audit 无 step_up 字段；② step_up 审计字段 omitempty 不落；③ 新增 body 字段 off 时不要求不解析语义不变；④ tx cookie 普通登录尾段空点（内部 opaque 值，无行为面）。handler 内无裸 `p.Admin` 残留（revoke 走 T-215 路由门）。
- **门位**：形态校验后、subject/TTL 护栏前（assurance 先于 authorization）——未验 session 不泄露 403/401 护栏状态，与 ADR 决策 1 逐字对齐。

### 3. OIDC purpose=step_up 流 — 通过

- authorize 强制 `prompt=login`（OIDC Core 标准参数；测试断言 query 参数）。
- callback 不建新 session：purpose 分支先于 IssueSession，测试断言无 Set-Cookie session。
- 同人校验：`reauth.Name` 与 session principal EqualFold 不符 → 401 + auth.failed 审计，grant 不签发；purpose 经 HttpOnly tx cookie 第三段承载（不可外部伪造，state 常时比较不变）。
- **fragment 属性核实**：RFC 3986 §3.5——fragment 不随请求发给服务端（浏览器发请求前剥除），故不进 BinFlow 访问日志亦不进中间代理日志；Referer 亦不含 fragment。grant 明文只在 Location header → 浏览器地址栏这一跳出现。
- 错误腿（sessionless/错人/facet 缺失/签发失败）全部 writeError，无 redirect——不泄漏 grant。

### 4. 密码腿 — 通过

- local：`VerifyPassword` 走 T-192 hashGate（argon2 全部在门内）；门放弃（ctx 取消）→ 拒绝并保留原因；空 hash 行快速拒（仅自身行，无跨用户 timing oracle）。
- ldap：与登录 fallback 臂同一 `Bind` 缝 + `Resolve(providerID)` 反查 + `EqualFold(username)==session 本人` + Enabled 双查、无 auto-create。bind 用户名恒为 session 属主 → **结构上不能探活他人密码**（探他人密码必名不符）。
- **错腿凭据 → step_up_required（agent 自决①）**：认可。错腿响应与「所欠凭据缺失」同形，不泄露腿期望、不构成密码 oracle（OIDC 行交任何密码都得到 required，与密码值无关）；ADR 只定义缺/败两分，此读法取其安全侧。测试已固化，无需改。
- 备注（范围外）：mint 面无尝试限流——与登录面现状一致（全代码库无 lockout），step-up 未引入新暴露（登录端点本就可对任意用户试密码且不需 session）。登记交 conductor。

### 5. 审计 — 通过（遗留①维持 non-blocking）

- `token.issue` detail 增 `step_up: true`（omitempty）+ `step_up_method ∈ {password, oidc_reauth}`；仅 step-up 路径出现（`stepUpMethod != ""` 只在门通过后置位）；豁免臂/开关 off 均不写（测试钉死 admin 与 off 两形态）。method 词表 = ADR 决策 7 终版拼写（local+ldap 同 password 腿）。
- **grant 签发无独立审计事件（agent 遗留①）评估**：不升格 blocking。ADR-0027 决策 7 只规定 token.issue 维度；internal/audit 动作词表为闭集、无贴切词（扩词跨区）；签发已有 INFO（user+ttl）+ 失败腿有 auth.failed。残余盲区 = 「签发后未消费即焚」与「grant 过门后铸败（如 TTL 帽 401）」无审计痕——建议 conductor 以小票评估（可与 T-224 QA 矩阵同批），非本票缺陷。

### 6. 实跑记录（全部实际执行）

```
go build ./... && go vet ./...                                        → 0 输出
go test -race -count=1 -run 'StepUp|MintGrant|TokenStepUp'
       ./internal/config/... ./internal/auth/...                      → ok (1.574s / 4.283s)
go test -race -count=1 -run 'StepUp|MintGrant|TokenStepUp|T219'
       ./internal/httpapi/...                                         → ok (7.837s)
golangci-lint run ./internal/httpapi/... ./internal/config/...        → 0 issues
golangci-lint run ./internal/auth/...                                 → 52 issues（= 基线，stepup* 零命中）
go test -count=1 -run 'TestT190|TestTokenCreate' ./internal/httpapi/  → 全 PASS（V25 四护栏）
```

### 7. 独立红绿（三突变全被捕获；测后 diff -q 逐字节还原，还原后全套复跑全绿）

| 突变 | 预期红 | 实际 |
|---|---|---|
| ① 去 burn-on-presentation（仅成功才删） | 复活/探测定烧用例红 | `TestStepUpGrantExpiredRefusedAndBurned` + `TestStepUpGrantLifecycle`（binding-probe-burn 腿）FAIL ✓ |
| ② 去 session 同人绑定（只判 username） | 跨 session 用例红 | `TestStepUpGrantWrongSessionBinding` + `TestT219GrantBoundToSession` FAIL ✓ |
| ③ TTL 域检查禁用（TTL=5 接受） | config 拒启动用例红 | `TestTokenStepUpGrantTTLDomain`（59/3601/off+5 三腿）+ `TestTokenStepUpEnvOutOfRangeRefusesBoot` FAIL ✓ |

附加探针（临时测试文件，已删）：64 goroutine 并发消费单 grant（-race）→ 恰一次成功。

### 8. clean-room — 通过

auth-model §3.7 明证 Artifactory REST 层 token 创建**无**二次认证（其邻近面 = UI 侧 `PasswordProtectedResource` + `X-JFrog-Reauthentication` 头 + `validateUserPassword`，且 SSO 用户走「内部口令」路径）。BinFlow 实现（端点 handler 门 + body `step_up_password`/`step_up_grant` + OAuth 形错误码 + prompt=login 换发单次 grant 台账）与上述任何机制无结构对应、无逐行翻译嫌疑——grant 方案恰是 Artifactory 不存在的自有设计（其 SSO 腿要求内部口令，BinFlow 以 IdP 重认证替代）。判定：自有设计，非搬运。

## non-blocking 建议

1. callback「重认证为他人身份」路径无 e2e 用例（mock IdP 单身份限制；单元级错绑定已覆盖）——建议 T-224 QA 脚本补一腿（第二 IdP 身份回跳 → 401 + 无 grant）。
2. 台账无专属并发烧毁用例（顺序复放已测、原子性由 mutex 结构保证，本次探针已独立验证）——建议随 T-220 补一廉价并发用例。
3. `purpose=step_up` init 不校验 session 属主 provider=oidc、不校验开关 on（local session 可跑流换得死 grant，无实害）——可加早拒，纯打磨。
4. 范围外交 conductor：① 登录/step-up 面无 lockout（存量姿态，非本票引入）；② grant 签发/无消费焚毁的审计词表小票（见 §5）；③ tx cookie 值形变更（尾段空点）无消费方影响登记即可。

## 结论

ADR-0027 决策 1~7 逐条落地且逐字对齐（错误体文案、审计词表、TTL 域、豁免臂清单）；两处 ADR 未规定面自决（fragment 承载、错腿→required）均取安全侧，本评审认可并登记。安全属性（熵、单储摘要、绑定、单次性、原子性、fail-closed、无 oracle）逐项实证。**APPROVE**，建议进 qa。
