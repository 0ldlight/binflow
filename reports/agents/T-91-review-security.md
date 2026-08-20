# 评审报告 T-91（视角: security — session/CSRF/凭据面）

结论: **REQUEST_CHANGES**
日期: 2026-08-20 · reviewer: code-reviewer(security) · 输入: reports/agents/T-91.md、internal/auth/session.go、internal/httpapi/session.go、middleware.go、router.go、internal/config console 段、PRD M4 v1.1 FR-23/CE-01~07/NFR-S19/S20、ADR-0014（勘误后）

## 总评

第三臂的认证内核质量高：256-bit crypto/rand、sha256 存储（与 tokens 同规）、失效即拒不降级、每请求重查属主、绝对封顶 TTL、HttpOnly/Path=/binflow/SameSite=Lax 属性齐全、日志零 session 值（真机 grep 复核）。csrfGuard 链位正确（authenticate 之后、dispatch 之前，覆盖内容/管理/adapter 全写面），Origin 判定对子域/null/畸形/大小写/端口不匹配全部 fail-closed（真机矩阵 12 变体核实）。会话固定结构性免疫（每次登录新铸随机 id，预置值登录前 401）。

但登录端点本身有一个**正确性+可用性缺陷**（blocking 1），以及待裁决的 **login-CSRF**（blocking 2，裁定应补防线）。二者改动面都收敛在 handleSessionCreate / dispatch 单点。

## 必须修改（blocking）

### B1. 陈旧/被投掷的 session cookie 使「正确凭据登录」401 —— 登录端点被全局拒绝凭据逻辑劫持

- 位置: `internal/httpapi/router.go:126-133`（dispatch 的 authRejected 硬 401 分支）× `internal/httpapi/session.go:90`（handleSessionCreate）
- 复现（真机，/tmp/t91-review，端口 18195）:
  - `POST /binflow/api/v1/session`（正确 admin 凭据）+ `Cookie: binflow_session=attackerchosenvalue123` → **401 "invalid credentials"**；同一请求去掉 cookie → 200 + Set-Cookie。
- 机理: authenticate 中间件对「呈交但失效的 cookie」返回拒绝 → dispatch 在**路由之前**统一渲染 401，POST /api/v1/session 无豁免。而登录端点恰恰是陈旧 cookie 的预期归宿（expired/revoked/swept/被投掷），浏览器的 cookie（Path=/binflow）会自动附上。
- 攻击/事故面:
  1. **cookie 抖掷 DoS**（远程、持久）: 控制任一子域的攻击者向受害者浏览器投 `binflow_session=garbage; Domain=.example.com; Path=/binflow; Max-Age=远未来`；无会话的受害者此后在控制台输入**正确**口令永远得到 401「invalid credentials」——口令错误的误导性提示 + 无限期登录拒绝。
  2. **TTL 调小 / 换库 / 清扫后**: 浏览器侧 cookie Max-Age 按旧 TTL（最长 24h）仍存活、行已过期/删除 → 正确凭据 401 持续整个残窗。遗留②把清扫接线到 cmd 后此面**放大**（行被删除 → not-found → 拒绝）。
  3. 时钟偏差下的过期重登窗口（秒级，用户重试可自愈——次要）。
- 与契约冲突: CE-03/W04 的契约是「正确凭据 → 200 + Set-Cookie」；「到期待遇同未认证（401）」针对的是 API 请求，不是凭据呈交端点本身。
- 建议改法: dispatch 的拒绝凭据分支对 `POST /binflow/api/v1/session` 精确豁免（该路由本就是 routeAuth{} 不设门——凭据核验由 handler 自持，成功判据是 body 的 username/password，与 cookie 无关；「presented-but-rejected 绝不静默成功」的姿态对其余全部路由保持不变）。补测试: (a) 垃圾 cookie + 正确凭据 → 200 + 新 Set-Cookie；(b) 过期行 + 存活 cookie 登录 → 200；(c) 垃圾 cookie + 错误凭据 → 仍 401 且文案统一。

### B2. login-CSRF（遗留④裁决: **应修**）——登录 POST 无 Origin 校验，跨站可把受害者登进攻击者账户

- 位置: `internal/httpapi/session.go:90-148`（handleSessionCreate 无 Origin 判定；csrfGuard 因匿名 principal 天然不覆盖此路由）
- 复现（真机）: `POST /binflow/api/v1/session`（form 形态）+ `Origin: http://evil.example` → **200 + Set-Cookie**。
- 评估: SameSite=Lax 对此**无效**——login-CSRF 不需要携带受害者 cookie，攻击请求自带攻击者账户凭据，响应的 Set-Cookie 照常写入受害者浏览器（SameSite 管发送不管写入）。后果: 受害者以攻击者身份操作控制台（上传的制品、audit 归因全部落攻击者可见域）；对以「审计可归因」为卖点的 M4 是直接打击。PRD「CSRF 防线（暂行）」字面只覆盖 cookie 认证写操作，但 ADR-0014 的内核目标是「Cookie 面与 Bearer 面安全等价」，登录本身可被跨站驱动即破坏该目标；且勘误④已把 Origin 校验立为主防线，把同一判定复用到登录是顺手收口。
- 建议改法: handleSessionCreate 入口处 `if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !sameOrigin(r, origin) → 403`（复用 sameOrigin 与 csrfGuard 同款日志/文案）。无 Origin 放行 → curl/CI 零感知。补测试: 跨源 form/JSON 登录 → 403 且无 Set-Cookie；同源/无 Origin → 200。
- 与 B1 的顺序: 先 B1 后 B2——修复 B1 后，陈旧 cookie + 跨源 Origin 的登录由 B2 拦 403，两条防线咬合。

## 建议改进（non-blocking）

1. **登录时侧信道枚举用户**（internal/auth/authenticator.go:162-185，authenticateBasic）: 未知用户 ~1.0ms（无 argon2）vs 错误口令 ~16ms（argon2id），真机 3 轮稳定 16×差；401 body 已统一（PASS）但时间侧信道可区分。**pre-existing**（Basic 臂 M1 行为，所有端点同病），非本票引入；建议后续票对未知用户做 dummy-hash 恒时验证。PRD 已把爆破锁定记 P2。
2. **X-Forwarded-Proto 无代理白名单直信**（router.go:206-214 + middleware.go:551-557 + session.go:205-212）: 直连客户端伪造 XFP:https 可 (a) 使 https Origin 被判同源（真机 201）、(b) 明文响应得到 Secure cookie（浏览器丢弃 → 该次登录无效）。浏览器跨站**无法**携带自定义头（preflight 被 CORS 默认配置挡下），故无浏览器侧 CSRF 绕过；但建议 deploy 文档明写「反代必须覆盖/清洗 XFP」，M5 再考虑 trusted-proxy 配置。
3. **sameOrigin 忽略 userinfo 分量**: `Origin: http://evil.example@127.0.0.1:18195` 判同源（url.Parse 把 evil.example 放进 User，Host 参与比较）。浏览器永不产生含 userinfo 的 Origin，不可利用；如求字面严谨可在 sameOrigin 拒绝 u.User != nil。
4. **空 Origin 头等价缺席**（`Origin:` 空值 → 放行）: 与「带 Origin 头且非同源 → 403」的字面读法略有出入；浏览器不可达此形态，维持现状可接受，报告备案。
5. **清扫未接线**（遗留②，架构 §11.18）: 过期/吊销行拒于认证时刻、仅占行数，风险低；但**接线顺序须在 B1 之后**（见 B1 事故面 2）。建议随 cmd 票带挂时回归「过期行+存活 cookie 重登」用例。
6. **API token 可作登录口令**（AuthenticateCredentials 复用 authenticateBasic 的 token 对偶）: 控制台可用 `username + API token` 换取等权 session——权限无放大（token 本就等于 Basic），属刻意设计，建议 tech-writer 在用户文档言明。
7. **越界例外**（internal/repo/api.go reservedRepoKeys +3、repo_test.go +3 行）: 纯数据表 + 测试行，AC①/T-89 交接明文要求，ADR-0008 T-108 并集定稿——**接受**，请 conductor 在 BOARD 归档时备注归属。
8. `assets` 不在保留字并集（遗留①）：与 ui 同构的遮蔽可达性问题，归 architect 裁决 ADR-0008 增补与否，本票按勘误字面实现无责。

## 验证记录（实际执行）

```
go vet ./internal/auth/ ./internal/httpapi/ ./internal/config/        # 零输出
go test -race -count=1 ./internal/auth/                               # ok 15.741s
go test -race -count=1 -run 'TestSession|TestCSRF|TestConsole|TestReserved' ./internal/httpapi/   # ok 7.85s
真机探针 /tmp/t91-review/probe.sh + probe2.sh（独立实例 18194/18195）：
  cookie 属性 HttpOnly/Path=/binflow/SameSite=Lax/Max-Age=86400/明文无 Secure —— 全 PASS
  Origin 矩阵 12 变体：跨源/子域/null/畸形/端口缺失/默认端口/https-on-http → 403（fail-closed 全对）
                   大小写 scheme+host / 尾斜杠 / userinfo → 201（同源/不可利用）
                   XFP:https 伪造 → 201（见 non-blocking 2）
  会话固定：预置攻击者值登录前 401；登录新铸 id（结构保证 + 真机）
  login-CSRF：跨源 form 登录 → 200 + Set-Cookie（blocking B2 证据）
  B1：正确凭据 + 垃圾 cookie → 401 "invalid credentials"（blocking B1 证据）
  非 admin session 于 /api/v1/storage/stats、/api/security/users → 403；admin → 200（ViaSession 授权面正确）
  登出 204 → 重放 401；serve.log 无 session id
  统一 401 body（未知用户 vs 错口令逐字节相同）PASS；timing 差 16×（non-blocking 1）
```

## clean-room 抽查

无逐行对应嫌疑：session/CSRF 实现是 BinFlow 自有 ADR（binflow_session、三动词、Origin 主防线）的原创 Go 结构，与 reverse-src 的 Java 会话/CSRF 机制无同构映射；存储形态（sha256 行 + RFC3339 时间戳）沿用本项目 tokens 既有规则。命名面（binflow_session/X-BinFlow-Console）为自有品牌面。

## 结论

内核与 CSRF 守卫本身达标；两个 blocking 均收敛在登录端点单点（dispatch 豁免 + handleSessionCreate Origin 判定），改动面小、不动第三臂内核。修复后建议补 B1/B2 各 2~3 个回归用例再过 qa。
