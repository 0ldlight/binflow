# 评审报告 T-98（视角: consistency + 前端正确性）

- 结论: **REQUEST_CHANGES**
- 日期: 2026-08-20
- 评审对象: commit `d51dcce`（current tree 与该 commit 的 web/ 面一致，`git log d51dcce..HEAD -- web/` 为空）
- 评审人: code-reviewer
- 验证执行: `cd web && npm run typecheck`（0 错误）、`npm run lint`（0 告警）、`go test -count=1 ./internal/console/`（ok 0.392s）、dist 产物外链 grep、internal/httpapi 路由与错误体逐一比对（只读）

## 必须修改（blocking）

### B1. 会话过期 401 风暴可产生重复「登录已过期」常驻 toast（AuthContext.tsx:65-76）

失败场景：`statusRef.current` 只在渲染期被回写（AuthContext.tsx:42-43），而 `setStatus('anonymous')` 在 promise 回调里是异步调度（React 18 DefaultLane，渲染在调度器宏任务中提交）。TTL 过期后用户点回「仪表盘」，DashboardPage 一次挂载并发 4 个非 silent401 请求（health/stats/repos/audit），4 个 401 在 React 重渲染提交前先后进入监听器时，`statusRef.current` 仍为 `'authenticated'` → 重复执行「toast + navigate」。错误 toast 是常驻手动关（ToastContext 设计），用户会看到 N 条相同通知。工作日志的 TTL 探针只停在 /settings（单请求面），未覆盖并发风暴路径，故自测未暴露。

建议改法（一行）：监听器内同步改写哨兵，消除窗口：

```ts
setUnauthorizedListener(() => {
  if (statusRef.current !== 'authenticated') return
  statusRef.current = 'anonymous' // 同步挡住同一轮里并发到达的 401
  setSession(null); setStatus('anonymous')
  toast.error('登录已过期，请重新登录')
  ...
})
```

### B2. useAsync 共享 alive ref 存在陈旧响应竞态（useAsync.ts:29,32-52）——基座 hook，T-99~T-102 将带 deps 消费

失败场景：`alive` 是单个 ref，新一次 effect 运行把它重置为 `true`（line 32）。deps 变化时旧请求仍在飞：cleanup 置 false → 新运行立刻置回 true → 旧响应晚到时 `alive.current === true` 通过检查，**用旧数据覆盖新数据**（T-99 仓库详情快速切换 A→B，若 A 的响应晚于 B 到达，页面显示 A 的数据）。本批调用点全部 `deps: []` 未触发，但该 hook 正是 T-88 R6 指定沉淀给 T-99~T-102 的共享基座，下游必然出现 deps 变化调用点；届时修复需回审全部消费者。

建议改法：把旗标改为**每次 effect 运行的闭包变量**（语义等价、零成本）：

```ts
useEffect(() => {
  let alive = true // 每次运行私有，而非跨运行共享的 ref
  setState({ status: 'loading', data: null, error: null })
  fn().then(d => { if (alive) setState(...) }).catch(...)
  return () => { alive = false }
}, [...deps, tick])
```

（StrictMode 双挂载下行为也更正确：第一次运行的响应被正确丢弃。）

### B3. 设置页「改密」是 AC③ 明确面，却零自动化覆盖、零记录的手工验证（web/e2e/auth-shell.spec.ts 全文；reports/agents/T-98.md 自测段）

AC③ 要求「设置页实例信息 + 改密（复用 /api/security/password）」。committed e2e 覆盖了登录/守卫/壳/仪表盘/主题/404/登出，但设置页仅出现在登出后的守卫拦截断言里；改密的两个关键分支——错误旧口令的行内纯文本呈现、成功 toast——没有任何已执行证据（工作日志自测清单亦未记录）。违反「验证优先」（声称完成必须附实际执行过的自测）。

建议改法：e2e 追加一例（不动 ADMIN_PW，错误分支即可起步；要覆盖成功分支可用探针用户 jane 改后改回）：

```ts
test('settings password change surfaces server plain-text wording inline', async ({ page }) => {
  await page.goto('/binflow/ui/settings') // → login → 壳
  await page.fill('[data-testid="password-old"]', 'definitely-wrong')
  await page.fill('[data-testid="password-new"]', 'new-password-1')
  await page.fill('[data-testid="password-confirm"]', 'new-password-1')
  await page.click('[data-testid="password-submit"]')
  await expect(page.locator('[data-testid="password-error"]')).toContainText('Incorrect username/password')
})
```

## 建议改进（non-blocking）

- **N1（契约漂移①收敛的一致性）**：仪表盘四张管理面卡已按 403 驱动（`Card` forbidden → null，后端将来放宽会自动跟随，✓）。但两处仍是 admin 位硬编码：SettingsPage.tsx:50（`{admin && ...}` 健康行——不会随后端放宽自动出现，且该行未处理 forbidden 态）与 AppShell.tsx:32-49（NAV 安全/治理组 `adminOnly`）。console-ux §3.3 明文「API 放宽时 UI 自动多显示，无需改版」。建议：设置页健康行改用与仪表盘相同的 403 驱动（forbidden → 整行隐藏）；导航占位组的 admin 位可保留（whoami 是 CE-04 主信号），但把「放宽自动跟随」要求写进 T-99~T-102 派单，漂移①维持逼 ux/architect 裁决。
- **N2**：AppShell.tsx:80-94 的 ⌘K/`/` 快捷键未在 modal 打开时让位。复现：打开登出确认框，按 `/` → 背景路由跳 /search 占位页，modal 仍悬在其上。本批消费者仅登出（影响小），但 T-99 删仓/T-102 GC apply 复用 ConfirmDialog 后，确认框开着时背景页被换走会让确认结果落在无关页面上。建议在 handler 里检测 `document.querySelector('[role="dialog"]')` 或暴露 ConfirmProvider 的打开态，随 B 批顺手修。
- **N3**：api.ts:45-61 注释与实现不符——宣称「三种错误体（errors[] / 纯文本 / OAuth）统一解析」，但 OAuth 形 `{"error":"...","error_description":"..."}` 走 JSON.parse 成功且无 `errors` 字段后，回落为**原始 JSON 字符串**当 message 展示。本批前端不消费 OAuth 错误端点（token 面归 T-101）无实害；建议在 T-101 前补 `error_description`/`error` 字段提取，或先修正注释。
- **N4**：useVersion.ts:10-23 首次失败后 `inflight` 永久缓存失败结果（resolve null），整页生命周期内不再重试，版本恒显 `—`。建议失败时清 `inflight` 允许下次挂载重试。
- **N5**：AuthContext.tsx:84-88 登出时若会话已死（用户停驻超过 TTL 后直接点登出），DELETE 返回 401（silent401）→ AppShell doLogout 弹「登出失败：authentication required」且停留壳内。语义上服务端已无此会话，应视为幂等成功：登出的 ApiError 401 分支直接清本地态走 /login。
- **N6**：data-testid 清单未回写 console-ux（工作日志已自登记）。本批 e2e 实际依赖的锚（`login-*`、`session-user`、`nav-version`、`app-nav`、`dashboard-*-card`、`confirm-*`、`not-found`、`placeholder-page`）在 T-99~T-102 改版时保持稳定即可；建议 ux v1.1 回写后再让 T-104 依赖非 e2e 已用的新锚。
- **N7（cosmetic）**：内联样式绕过 token（DashboardPage.tsx:242 `maxWidth:640`、:165 `float:'right'`、SettingsPage 同款；ErrorCard `marginTop:8`）；ConfirmDialog 关闭后焦点未归还触发元素（§8 Tab 序连续性）；InstanceCard 版本未到时显 `—` 而非骨架（与其余卡密度不一致）。均可留待 T-99/T-104。

## 契约核对（证据，全部通过）

| 项 | 前端 | 后端（internal/httpapi） | 结论 |
|---|---|---|---|
| E-01 错误信封 | `errors[0].message`（api.ts:51-53） | envelope.go `{errors:[{status,message}]}`（缩进 JSON） | 逐字段一致 |
| 纯文本层 | `text.trim()` 回落 | security.go `writePlainError` 400（改密错旧口令 = 400 非 401，**不触发**全局过期处理） | 一致，无 401 语义冲突 |
| whoami/登录 | `{username,admin}` | session.go sessionReply | 一致 |
| 端点路径 | `/system/version`、`/v1/health`、`/v1/storage/stats`、`/v1/audit?limit=`、`/repositories`、`/security/password`、`/v1/session` 三动词 | router.go dispatchAPI 各 case（version 开放；health/stats/audit/repositories admin-only，D2） | 一致 |
| 字段名 | stats `{blobs,logical_bytes,physical_bytes}`、audit `{events:[{id,time,actor,action,repo,path,detail}],nextCursor}`、repos `{key,description,type,packageType,url}`、version `{version,revision,product}` | system.go/audit.go/repositories.go json tag | 一致 |
| TTL 塌缩 | 无任何 keep-alive/ping/轮询（src 全量 grep 仅 toast/skeleton/copy 三处 setTimeout） | — | 符合 T-110 塌缩句 |
| silent401 豁免面 | session 三动词 + version（开放端点，豁免无实害）；数据面端点全部非豁免 | — | 面≈whoami 族，合理 |
| return 开放跳转 | `startsWith('/') && !startsWith('//')`（LoginPage:26、AuthContext:72、AppShell:132），且 react-router basename `/binflow/ui` 前缀包裹，`/\` 归一化后仍同源 | — | 无 open-redirect |
| 403 自动跟随 | 仪表盘卡 403 驱动 ✓（N1 所列两处除外） | — | 基本符合 |
| 四态矩阵 | loading=150ms 延迟骨架 / error=ErrorCard+重试 / empty=CTA / forbidden=隐藏；403 不进 ErrorCard | — | 覆盖本批全部数据面 |
| token 双主题 | base.css 零硬编码色值（hex/rgb 仅存在于 tokens.css 两主题块）；`[data-theme]` 覆盖；reduced-motion 关脉动 | — | 零分叉 ✓ |
| 构建纪律 | package.json/lock 零变更（diff 为空）；dist/index.html 零 http 引用；JS 内外链串仅为 SVG/MathML namespace 与 react.dev 错误文案常量；主 chunk 241KB raw / 77.6KB gzip，+14KB = react-router-dom(~13KB gzip)+页面代码，预算 1.7% | — | 合理 |
| e2e 证明力 | AC①全覆盖、AC②除 ⌘K 与 Tab 序断言外覆盖（含 Esc 取消/服务端吊销/404 保壳）、AC③仪表盘五卡覆盖；缺口=改密（B3）与会话过期未入 committed 套件（TTL 探针为手工脚本） | — | 除 B3 外达 AC 面 |

## 范围外发现

- 无（并行票 WIP 面 internal/auth、httpapi、adapter/docker 未触碰、未评审；漂移①~④已由工作日志登记，归 conductor 逼回写，其中漂移②（config 面无 GET 端点）建议随 N1 一并交 architect 裁决）。

## 结论复述

REQUEST_CHANGES：B1（过期 401 风暴重复 toast，一行修）、B2（useAsync 陈旧响应竞态，基座 hook 三行修）、B3（改密面零验证证据，补一例 e2e）。三者修复成本合计 < 30 行 + 1 个测试用例；修完即可approve，无需二轮全量评审（仅复核三处 diff + e2e 绿）。
