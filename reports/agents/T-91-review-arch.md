# T-91 评审报告（视角: consistency / 架构一致性）

- ticket: T-91 session 三臂 + console 挂载 + CSRF（FR-23, CE-01~07）
- reviewer: code-reviewer（架构一致性）
- 日期: 2026-08-20
- 基线: PRD milestone-4 v1.1 + ADR-0014 勘误后（ad418c5）+ architecture §3.4/§5.1/§6 004/§7.1/§7.2/§7.5/§8 + ADR-0008 T-108 增补
- 结论: **APPROVE**（blocking 0；non-blocking 6 + 范围外 1；两项上游裁决/回写建议见 §遗留裁决）

## 验证执行（实际命令）

```
go test ./internal/auth/ ./internal/config/ ./internal/console/ ./internal/repo/ -count=1   # 全 ok
go test ./internal/httpapi/ -count=1 -skip TestSearchPerfSpotCheck                        # ok 37.1s
go test ./internal/httpapi/ -run 'TestCSRFOriginMatrix|TestSessionTTLExpiryHTTP|TestConsoleSegment|TestReservedRepoKeysHTTP|TestSessionLogin|TestSessionLogout|TestSessionCookie|TestSessionRestart|TestSessionConcurrentWhoami' -count=1 -v   # 全 PASS（W01~W08/W37a/W38 逐锚点过）
go vet ./internal/auth/ ./internal/httpapi/ ./internal/config/ ./internal/repo/ ./internal/console/   # PASS
gofmt -l <同上五包>                                                                          # 空输出
```

## 逐项核对

### 1. ADR-0014 勘误对照 —— 全部符合

| 契约点 | 实现 | 裁定 |
|---|---|---|
| /binflow/ui 挂载 + 根 301 | router.go L145-158（`/binflow`、`/binflow/`、`/ui/**`、`/assets/**` → Deps.Console）；console.go serve() 301 → `/binflow/ui/` | 符，TestConsoleSegmentMount + middleware_test 双断言 |
| /api/v1/session 三动词 | router.go L273-278：POST 无路由门（登录即凭据呈交）、GET/DELETE required；§7.1 路由表 L800 已含 | 符 |
| cookie `binflow_session` + 三属性 | auth/api.go L174；HttpOnly/Path=/binflow/SameSite=Lax（TestSessionLogin W04b 逐属性断言）；Secure 依 base_url scheme 优先、否则请求 scheme（XFP 感知） | 符（勘误②） |
| TTL 双键 hours 主 + seconds 覆盖 | load.go build()：hours 先写、seconds 后写覆盖（同给 seconds 胜，**非冲突报错**，与 anonymous 双键语义差异已注释）；env 走 sort.Strings 遍历（load.go L325-328），`...HOURS` < `...SECONDS` 字典序 → seconds 后写胜，**确定性成立**；console_test.go 8 用例含双键/env 覆盖/零值拒 | 符（R4 一致） |
| CSRF Origin 主防线 + SPA 头习惯层 | csrfGuard：`ViaSession && 非 GET/HEAD && Origin 存在 && 非同源 → 403`；无 Origin/同源放行；Origin `null`/不可解析 fail-closed；`X-BinFlow-Console` 服务端不校验（勘误④，测试固化带头/无头均放行） | 符（勘误④） |
| 机制内核 | web_sessions sha256 行存储/吊销幂等（ErrWebSessionNotFound 冒泡）/滑动受绝对封顶 | 符（塌缩语义见 §遗留裁决 2） |

### 2. 挂载边界 —— 符合

- **不吞内容路径**：dispatch 只吃 `/ui`、`/ui/`、`/ui/**`、`/assets`、`/assets/`、`/assets/**` 精确前缀，其余 `/binflow/<repo>/<path>` 走 dispatchContent；TestConsoleSegmentMount 断言 generic-local PUT/GET + 嵌保留字 `ui-local` 仓不受影响。
- **dot-segment 穿越**：`/binflow/ui/../<repo>/x` 死于 console handler 的 path.Clean 背板（出段 404，assembled handler 直测）；`%2e%2e` 变体留在段内吃 shell，无穿越回内容面。
- **缓存契约**：assets `public, max-age=31536000, immutable`、shell `no-cache`（console.go L52-56 + 测试断言）。
- **保留字并集 {api,v2,docs,console,ui}**：与 DECISIONS.md L112（ADR-0008 T-108 增补）、architecture §5.1 L429、§6 L555 三处定稿一致；repo_test + TestReservedRepoKeysHTTP 五键全拒 + 合法 key 仍建（W01b 双腿）。

### 3. auth 包演进 —— 符合

- **接口形态**：SessionRegistry（AuthenticateCredentials/IssueSession/RevokeSession）+ IssuedSession 消费侧定义在 auth/api.go；httpapi 侧 sessionRegistry 消费者接口形状一致、经 Deps.Auth 类型断言装配（cmd Deps 清单零改动，fake authenticator → 诚实 503）。webSessionSource 为 metadata.WebSessionStore 消费侧子集且导出别名（WebSessionSource），符合「接口定义在消费者」惯例。
- **NewFromStore 零改动获臂**：核实通过——cmd/binflow-server/main.go:408 为唯一生产调用点，本 commit 未触碰 cmd/；004 表随 metadata.Open 恒在，无条件接线成立。`New()` 直建服务保持 cookie 臂惰性（TestSessionUnwiredArmIgnored），既有测试字节兼容。
- **三臂文档 vs 实际臂序**：代码 = Basic → X-JFrog-Art-Api → Bearer → Cookie（最后）；「头部凭据优先于 cookie」「呈交但失效 cookie = 拒绝凭据不降级匿名」均有文档句 + 测试。一致。（一个 pre-existing 文档 nit 见范围外。）
- **Principal.ViaSession**：实现正确（session 无 TokenID、等权授权），但为 §3.4 结构体形状之外的字段 → 回写项 N3。

### 4. config 面 —— 符合

console 段结构（raw 双 `*int` + build 顺序 + Validate 正数/零值拒 + Defaults 24h + env 双键 envIntPos + 未知键严格拒）与既有 logging/audit 段模式一致；「seconds 胜出」与 anonymous 双键「同给不一致报错」的差异已注释说明理由且方向正确（两键是同一值两种精度 vs 两键是两个特性）。架构 §8 YAML 全量清单尚无 console 段 → 回写项 N4。

### 5. middleware 链位 —— 位置正确，§7.2 待回写

实际链（grep 链序核实）：

```
requestID → accessLog → recoverPanic → cors → authenticate → csrfGuard → dispatch
                                                            （per-route authorize 经 enforce 附着）
```

- 需 principal → 必须在 authenticator 之后 ✓；W39 语义（跨源 DELETE 先见 403 不见 204、且会话幸存）→ 必须先于一切路由决策 ✓（TestCSRFOriginMatrix「guard precedes routing」子例断言）；置于 per-route authorizer 之前还避免了向跨源请求方泄漏授权判定。**位置无异议**。
- 但 §7.2「顺序固定」清单未列 csrfGuard（T-91 新增中间件）→ 回写项 N2，否则「顺序固定」清单与代码漂移。

### 6. clean-room 抽查 —— 通过

reverse-src 探针：session/CSRF/console 挂载相关命中仅为 Artifactory Java 核心与前端 source map，无 Go 实现对应体；server-side session + Origin guard + sha256 行存储为 ADR-0014 自有设计（且 ADR-0014 候选分析明确**拒绝** OSS web/rest-ui 专属后端模式）。无逐行对应嫌疑。

### 7. area 越界例外（internal/repo，2 文件 ~10 行）—— 接受

保留字校验唯一实现在 internal/repo/api.go；ADR-0008 T-108 增补为已定稿契约，改动为纯数据表 + 既有测试表扩行、零行为裁量，报告已明示并沿 T-90 先例。接受，conductor 知悉即可。

## 遗留裁决（上游意见，非阻断）

**1. 遗留① `assets` 不在保留字并集 —— 建议增补（或显式记录接受）**

`assets` 与 `ui` 同构地被挂载遮蔽：repo key `assets` 建仓成功，但 `/binflow/assets/<path>` 在 router.go L149-158 命中前缀匹配先于内容分发——**仓内容永远不可达**。ADR-0008 保留字的存在理由（「与路由段冲突即不可达」）对 assets 的成立度与 ui 完全相同，沉默状态最难辩护。二选一：
- **推荐**：ADR-0008 增补一句 + reservedRepoKeys +1 行 + repo_test/httpapi 测试各 +1 行 + PRD CE-07/W01b 补记（T-91 报告估的「一行数据 + 一行测试」准确）；
- 或在 ADR-0008 增补注记显式记录「assets 段遮蔽已知且接受」。
请 architect 裁决后开 1 行小票或并入 T-103 QA 前的任一 repo 触碰票。

**2. TTL 滑动塌缩 —— 「封顶胜出」读法正确，建议 §7.5/ADR-0014 回写一句话**

实现读法是契约文本下**唯一自洽**的读法：§6 004 DDL（architecture L760）明言 `expires_at` 列 = 「绝对 TTL」戳；ADR-0014 原文「绝对 TTL + 滑动（单次延长不超 TTL）」在 idle 窗 == 绝对封顶（双键收口共用一键）时，`last_used + TTL ≥ created + TTL = expires_at` 恒成立，滑动必然被钳到封顶——勘误③「滑动受绝对封顶的内核不变」在单键下**数学上等价于「绝对到期」**。T-91 实现正确、推导已注释、TestSessionTTLAbsoluteCapWins 固化、last_used 列留作未来 idle<absolute 拆分的缝。

回写建议：§7.5 与 ADR-0014 决策 2 补一句——「双键收口后 idle 窗与绝对封顶共用一键，滑动续期被封顶数学吞没：会话必死于 created_at+TTL，与活跃度无关；last_used 为未来拆分保留」。**不写清的风险**：QA/tech-writer/前端按 §7.5 字面把「滑动续期」写成「活跃可续命」，与实际行为（默认 24h 必掉线，whoami 401 触发重登）矛盾。改动面仅文档，无代码动作。

## non-blocking 清单

- **N1（裁决建议 1）**：assets 保留字二选一（见上）。
- **N2（裁决建议 2）**：§7.5/ADR-0014 TTL 塌缩回写一句话（见上）。
- **N3**：architecture §3.4 Principal 结构体补 `ViaSession bool` 字段位（§3.4 已写三臂注释但结构体形状未更新；Groups 字段缺属 SE 域票，非本票）。
- **N4**：architecture §7.2 固定链清单补 csrfGuard 位（authenticator 与 authorizer 之间，M4 增量）。
- **N5**：architecture §8 binflow.yaml 全量字段清单补 `console:` 段（session_ttl_hours/session_ttl_seconds + env 键）。
- **N6（健壮性 nit，下一票顺手）**：httpapi/server.go `s.audit` 在 `deps.Metadata == nil` 时为 nil interface，handleSessionCreate/Delete 直接 `Record` 会 panic；生产装配恒有 Metadata，仅理论组合，建议赋 no-op recorder 兜底。

## 范围外发现（交 conductor）

- internal/auth/api.go Authenticator 文档注释的入口列表顺序（Basic → Bearer → X-JFrog-Art-Api）与代码检查顺序（Basic → **X-JFrog-Art-Api** → Bearer）相反：两 header 同时在场时 API-key 头胜出，而注释句「The Authorization header wins when both it and X-JFrog-Art-Api are present」对 Bearer 形态不成立。**pre-existing**（T-91 diff 只追加 cookie 行与「every header arm wins over the session cookie」句，追加句准确）。建议随下一 auth 触碰票理顺措辞。

## 结论

APPROVE。AC①②③（CE-01~07）与 PRD W01~W08/W37a/W38 逐锚点有测试且本轮全过；挂载边界、臂序、双键语义、CSRF 链位与契约一致；两项上游裁决（assets 保留字、TTL 塌缩回写）与四处架构文档回写建议已列明，均不阻塞本票流转 qa。
