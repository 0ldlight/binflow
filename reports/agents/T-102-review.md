# T-102 评审报告（视角: consistency + 前端正确性）

- ticket: T-102 [P1] FE 治理组：审计页 + GC 页 + 配额页
- reviewer: code-reviewer（单 reviewer 票）
- 日期: 2026-08-21
- 范围: 工作树未提交改动中的 `web/src/lib/governance.ts`、`web/src/pages/audit/AuditPage.tsx`、`web/src/pages/governance/{GCPage,QuotasPage,BackupPage}.tsx`、`web/src/styles/governance.css`、`web/e2e/governance.spec.ts`，以及 main.tsx / AppShell.tsx / auth-shell.spec.ts 中的治理相关行（T-100/T-101 混线部分不评）

## 结论: REQUEST_CHANGES（blocking 1 条，non-blocking 8 条）

---

## 一、契约逐项核对（重点审查区 1~5 的取证结果）

### 1. governance.ts ↔ 后端契约

- **漂移①核实为真**：`internal/audit/api.go` `Filter` 只有 `Repo/Actor/Action/Since/Until/Limit/Cursor`，**无 Path**。UI 按 §6.3「前端过滤只作用于已加载集并显式提示」兜底（count 文案「路径过滤仅作用于已加载集」），合规。
- 查询参数面与 `handleAuditQuery` 读取面逐项一致：`repo/actor/action`（trim 后精确）、`since/until`（RFC3339、闭开区间）、`cursor` 原样透传（URLSearchParams 编码）、`limit=100`（§6.1 页大小，在 1..1000 内）。空值不进参数——正确。
- `localInputToRFC3339`：datetime-local 按本地时区解析 → UTC 秒精度，与 `audit.NormalizeTimestamp`（offset 折算、亚秒 floored）语义对齐。
- GC 请求体 `{apply: bool, graceHours?: number}` 与 `gcRequestBody{Apply bool, GraceHours *int}` 对齐：null 时**省略字段**（保持 absent ≠ 0 的指针语义），`GC_MAX_GRACE_HOURS = 876000` 与 `maxGCHours` 同值，`/^\d+$/` 拒绝小数/负数（避开 encoding/json 对 `*int` 的 400 decode 路径）。400 边界实际由前端预检挡住，服务端 400 仍会落 `gc-error` 面板。
- **AUDIT_ACTIONS 22 项与 `internal/audit Actions()` 逐项一致（含顺序）**——今日核对无漂移；漂移风险见 non-blocking N5。

### 2. GC 危险流

- **type-to-confirm 取 `YES`**：console-ux §4.11 原文「输入仓库实例名或 **YES** 确认」明示双分支；派单「输入仓库名」对实例级 GC 无指称对象。漂移④的规范依据成立，工作日志已留痕交 conductor 回写。e2e 覆盖「输错禁用 / YES 放行」。
- 409：专用红面板 + `runError.raw`（含 holder pid/op 诊断）原样 mono 呈现，与 `gcLockRefusedMessage` 的自描述信封匹配。浏览器面未构造真 409——已登记遗留②归 T-103，前端分支在 `gc-error`，可接受。
- dry-run→apply 状态机：无 dry-run 时 apply 禁用；grace 变更 → `dryStale` → apply 关回并提示重跑；`running` 旗防重复点击；中途失败 `runError` 呈现且 dryRun 保留可重试。TS 别名条件收窄（`canApply` 含 `grace !== 'invalid'`）经干净 typecheck 证实成立。

### 3. 审计页

- keyset 终止条件 `nextCursor === ''` 与后端「末页游标为空 + 越界探一行」契约一致；事件 newest-first（time DESC, id DESC）+ append-only 日志使 loadMore 追加无重复窗口。
- detail JSON 折叠：`<pre>{json}</pre>` React 文本子节点转义，全文件无 `dangerouslySetInnerHTML`——XSS 面干净（`remote_addr`/JSON 均为转义文本）。
- **发现一个正确性缺陷，见 blocking B1（loadMore 晚到响应竞态）。**

### 4. 配额页

- `buildLocalQuotaBody` 与 `RepositoryFormPage.buildBody` local 分支**逐字段比对一致**（rclass/packageType/description/priorityResolution/patterns/quotaBytes + maven 四策略字段），POST 全量替换语义下不会「改配额丢其它字段」；`updateRepo` 走 T-95 透传链。
- 水位三态：`used >= quota → full（红+满）`、`clamped pct >= 80 → warn（黄+高）`，颜色非唯一信号（文字「高/满」），quota≤0 显式「不限」。与 AC③ 及 §8 一致。
- 空串语义差异与 remote 行呈现小瑕疵见 N6；usage 逐仓 N+1 见 N7。413 语义在页头文案 + e2e REST 腿覆盖（写入面 413 属上传路径，非本页呈现职责）。

### 5. 非 admin 收敛（§3.6）

- L1：治理分组 adminOnly（AppShell），含 §3.1 v1.1 补列的「审计日志」条目——与规范一致。
- L2：三页主数据面 403 → 单张无权限卡；路由门已逐一核实（router.go：`v1/audit` admin、`v1/system/gc` admin、`repositories` 列表 admin——§3.6.1 漂移 B 定案维持），e2e 断言通过。
- L4：GC 危险区 whoami admin 预收敛——符合 §3.6.3「whoami 位仅用于 L1/L4」纪律（AuditPage/QuotasPage 均未引 useAuth 做数据面显隐）。
- 例外：BackupPage 无数据面无法 403 驱动，见 N2。

### 6. e2e 证明力

- 4 例覆盖 AC①②③ + L1/L2/L4：审计 REST 同过滤全量对账（总数一致 + 无跨仓行反断言）、keyset 到末页按钮消失、gc.run detail.deletedCount/graceHours 对账、配额编辑后 usage REST 对账 + 水位回落、非 admin 三直链。对账强度合格（真后端 + 服务端 5xx 监听 + serial 模式与 PRD QA 串行口径一致）。
- `--workers=1` 全套件 23 passed 为工作日志自报，本次未复跑（需起真实例）；已复跑：本域 eslint 0 告警、`tsc --noEmit` 0 错、`go test -count=1 ./internal/console/` ok。

### 7. clean-room 抽查

新文件零 `artifactory/jfrog/reverse-src` 痕迹；全部消费 BinFlow 自有端点与 docs/design 规范，无逐行对应嫌疑。**通过**。

---

## 二、必须修改（blocking）

- **B1** `web/src/pages/audit/AuditPage.tsx:93-107`（`useAuditPages.loadMore`）——**晚到响应竞态，脏数据注入新过滤结果集**。`loadMore` 在 `await` 后无条件 `setEvents(prev => [...prev, ...p.events])` 与 `setNextCursor(p.nextCursor)`，没有校验过滤键是否已变。交错序列：点击「加载更多」→ 请求在飞时用户改过滤 → 首 effect 同步清空 events/nextCursor 并发新过滤首页 → 旧 loadMore 响应晚到 → 把**旧过滤的第 2 页追加进新过滤列表**，且 nextCursor 被旧游标覆写，后续「加载更多」在新过滤下延续**旧游标链**。这正是 useAsync.ts:20-27（T-99 review B2）修过的同类缺陷；本 hook 头注释（AuditPage.tsx:51-53）声称「晚到旧响应按运行闭包旗标丢弃（useAsync 同款语义）」但 loadMore 并未实现——代码与自述契约不符。
  **建议改法**（最小）：`loadMore` 起点已取 `const { key: k, nextCursor: cursor } = stateRef.current`，在 `await` 返回后加守卫再落地：
  ```ts
  const p = await getAuditEventsPage(JSON.parse(k) as AuditFilters, cursor, AUDIT_PAGE_SIZE)
  if (stateRef.current.key !== k) return // 过滤已变：丢弃晚到页（finally 仍复位 loadingMore）
  setEvents((prev) => [...prev, ...p.events])
  setNextCursor(p.nextCursor)
  ```
  （更稳妥可用 effect 内自增的 epoch ref 比对，语义同 useAsync 的 alive 闭包。）

## 三、建议改进（non-blocking）

- **N1** `AuditPage.tsx:130/260-265`：`timeInvalid` 提示文案「修正后才会发起查询」与实际不符——防抖仍以 `since:''/until:''` 提交并发请求（不设界）。仅 DOM 篡改可达（datetime-local 浏览器侧产不出非法值），建议要么真门控（invalid 时不提交新 key）要么改文案。
- **N2** `BackupPage.tsx:35-66`：任何已登录用户直链 `/governance/backup` 可见完整 CLI 引导页；§3.6.4 矩阵将「治理全部页面（含备份）」列为非 admin L2 无权限卡。该页无数据面无法 403 驱动。两个出路二选一交 conductor：v1.2 回写时把备份页标注为例外（内容即公开文档），或按 whoami admin 收敛内容（静态信息页，不违反 §3.6.3 的数据面禁令）。e2e L2 腿也未覆盖此页，若选收敛应补断言。
- **N3** `GCPage.tsx:135-136, 283-285`：apply 成功 toast/结果面板的「释放 X」用的是 `candidateBytes`（预扫聚合），写入方竞态下 deleted<candidate 时高估；响应无 deletedBytes 字段（契约限制）。建议 apply 面板措辞改「预计释放」或在分歧提示里一并说明字节口径。
- **N4** `GCPage.tsx:66-67, 128-146`：apply 成功后 `dryRun` 快照不失效（grace 未变则 `dryStale` 为 false），gc-apply 重新可用，可不经新 dry-run 再执行一次真实 GC（服务端幂等重扫、大概率 0 候选，但与「先审后执行」姿态不完全一致）。建议 apply 消费快照后置 `dryRun=null`（或标记 consumed）。
- **N5** `governance.ts:34-57`：`AUDIT_ACTIONS` 是 `internal/audit Actions()` 的手维护镜像，今日 22/22 一致但无锚定测试——后端将来加动作会静默缺席选择器。建议从 Go 侧导出 fixture 做 parity 测试（同 T-101 的 pathmatch fixtures 思路），或至少在两侧注释互指同步点。
- **N6** `QuotasPage.tsx:100-104, 165-167` vs `RepositoryFormPage.tsx:187-189`：同一 quotaBytes 字段两个编辑器对空串语义不一致——表单「空输入视为清除（0）」，行内编辑「空串报错、必须显式 0」。行内编辑的拒绝更安全，建议在日志登记差异（或将表单对齐）。另：remote 行配额列「—（仅 local 仓支持）」而水位列显示「不限（quotaBytes 0）」，两列口径打架，建议水位列对非 local 同样显示「—」。
- **N7** `QuotasPage.tsx:81-84`：每仓一个 usage GET（N+1，无并发上限）。当前仓数规模可接受（后端注释同判断），仓数增长后考虑批量端点（后端增量票）。
- **N8** `AuditPage.tsx:182-266`：非 admin 403 时过滤器栏仍可交互地渲染在无权限卡上方（配额页等兄弟页面不渲染过滤面）；每次改动只会再 403，无破坏，纯收敛姿态不一致。

## 四、AC 覆盖结论

| AC | 结论 |
|---|---|
| ① W34 审计页 | 满足（过滤/表格/cursor 加载更多/REST 抽样对账/动作原样 mono/CSV 隐藏）；B1 修复后无保留 |
| ② W35 GC 页 | 满足（stats/dry-run 面板/YES 二次确认/deleted 与 stats 对账/0 候选绿空态）；409 浏览器腿归 T-103 已登记 |
| ③ 配额页 | 满足（水位 80 黄/100 红/行内编辑/备份 R5 兜底页/非 admin 403 卡）；N2/N6 建议项 |

## 五、范围外发现（交 conductor）

1. §3.6.3 指出的存量偏离「设置页健康行 `{admin && …}`」未随本批收口（规范原话「随 T-100~T-102 任一批次或 T-104 前顺手收口」+ `settings-health` 锚）——三票均未做，T-104 前需安排归属。
2. console-ux §10.3 v1.2 回写（增补锚 + audit-export 不渲染注记）与 GE-01「精确匹配」注记均待 conductor 统一落笔（工作日志漂移①②④已备好素材）。
