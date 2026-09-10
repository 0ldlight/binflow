# FE-P5 工作日志 — 前端重写 Phase 5（AI 基座）

Ticket:       FE-P5 前端重写 Phase 5 — AI Foundation（P1~P4 已落库 dd5a5e9d/f031b25b；本票 = 末程实现段，其后即终验）
Role:         dev-frontend（area = web/src/components/ai + lib/ai + app/shell 接线 + i18n ai 域 + web/e2e/p5）
Area:         web/src/{components/ai,lib/ai,stores/ai-store,app/shell,app/router 消费面}；web/e2e/p5；i18n ai 域
Input:        docs/design/frontend-rewrite-architecture.md §1 AI 基座行（assistant-ui+@ai-sdk/react，Phase 5 只搭壳）+ §10 P5 门（AI drawer 可开合+上下文注入正确+零后端虚构）+ §11 R6（mock provider 边界写入组件契约，禁真请求）；总令 §十五 Context-aware Copilot（设计意图唯一源——上下文徽章「当前在 docker-prod/nginx/1.27」形态/ToolCallCard name+args 折叠/ConfirmCard [取消][创建仓库] 双钮形态）；dispatch 硬边界（零 Go 改动、零虚构后端、四门绿、锚账 ledger exit 0、i18n 双语全量、axe 双主题）
Changes:
  1. 依赖（按需最小集）：+@assistant-ui/react@0.15.18（+react-markdown@10.1.0、remark-gfm@4.0.1 承载消息渲染分层的 markdown/表格/代码面）；**@ai-sdk/react 未引入**——mock provider 是纯本地状态机，无 streaming protocol 消费面（dispatch「如需」条款不触发，体积纪律）
  2. AI Shell（components/ai/AiAssistant.tsx 懒分片入口）：右滑 Drawer = Radix Dialog 侧板（ai-drawer 锚，520px 档）——Esc/焦点回归/滚动锁交 Radix FocusScope；**runtime 挂顶层设计**：useLocalRuntime 在 Dialog Root 外（Content 卸载不随葬——开合往返会话态存活，SPA 刷新才重置）；首开后常驻挂载（ai-store.mounted 闩，关闭仅隐藏）
  3. ChatPanel（components/ai/ChatPanel.tsx）：头部（标题 + 本地演示徽章 + ai-context-badge 上下文徽章 + ai-close）/ 消息流（ThreadPrimitive.Viewport+Messages children 形——MessagePrimitive.Parts 分层渲染）/ 输入框（Enter 发送、Shift+Enter 换行、ai-send 运行中禁发）；四态齐活：空态（ai-empty 欢迎卡+两条建议 chip）/ 加载（ai-typing 三点指示 + 12ms 帧流式）/ 缺省（消息流）/ 错误（ai-error 卡 + 服务端式错误详情 + ai-error-retry 原位重生成——MessageRuntime.reload，兜底 startRun）
  4. 上下文模型（lib/ai/context.ts）：deriveAiContext 纯函数——路由表只读消费（Explorer 页签段 general|properties|permissions 优先于 repoKey；/admin/repositories 的 new/rclass 段优先于 :key；builds/bundles name/number 链；search ?q=）；aiContextCoord 坐标链（repoKey/path、build 链、query）
  5. 消息渲染分层（components/ai/MarkdownText.tsx + ToolCards.tsx）：markdown（react-markdown+remark-gfm，strong/列表/行内码）/ GFM 表格（ai-md-table）/ 围栏代码块（ai-code-block mono + ai-copy-code 拷贝钮——CopyButton 复用，值不截断纪律）/ ToolCallCard（name 徽标 + args 参数表 <details> 折叠）/ ToolResultCard（columns/rows 假表格 ai-tool-result-table，未知形态回落 JSON 折叠）/ ConfirmCard（ai-confirm 参数表 + [取消][创建仓库] 双钮——**确认动作回调抽象 = assistant-ui ToolCallMessagePartProps.addResult**，渲染层是结果的事实源，真后端接入换 execute 回调同形）
  6. Mock provider（lib/ai/provider.ts）：本地确定性假响应引擎——**组件契约：禁真请求**（函数体零 I/O，仅 12ms setTimeout 帧间隔；audit 盲区② 裁定 + §11 R6 边界写入头注释）。行为四臂：echo（上下文感知样板「当前在 `坐标`（域）」+ 演示声明）/ 存储占用演示（query_storage_usage tool-call **携带内联 result** 假表格——直落 complete）/ 创建仓库演示（create_repository tool-call 无 result + requires-action/tool-calls 终态 → runtime 暂停〔unstable_humanToolNames〕→ ConfirmCard → 确认/取消 addResult → **runtime 重入 adapter**〔unstable_getMessage() 读进行中消息的 resolved 结果——options.messages 只含 [user]，LocalRuntime performRoundtrip 以 parentId 取链，node 实证〕→ 成功/取消文案追加）/ 模拟错误臂（抛错 → incomplete/error → 错误卡 + 重试闭环）；toolCallId 按消息序号确定性派生
  7. ai-store 接线（stores/ai-store.ts 重塑）：drawerOpen/mounted（首开常驻闩）/context（活上下文——ChatPanel 内 effect 随路由同步）/contextSnapshot（发送时快照）；**上下文注入消息首帧**：发送时把活上下文以哨兵行 [bf-ctx] {json} 注入用户消息首帧——用户气泡渲染 ai-msg-context-badge 坐标徽章（解析剥离），provider 解析同源
  8. 三入口接线：Topbar ai-open 钮（architecture §4 Topbar AI 槽位兑现）/ ⌘/Ctrl+J 全局快捷键（AppShell 监听——与 ⌘K palette、`/` 聚焦搜索三键并存）/ palette-ai 条目转正（P4 占位 disabled → onSelect 开 drawer，条目尾注 ⌘J kbd 提示）
  9. i18n ai 域：I18nDomain +'ai'（index.ts）+ en/ai.ts（50 键全填）+ zh/ai.json 清单 + catalogs.ts 登记 + assert-i18n/regen-catalogs DOMAINS +'ai'（T-514 builds 域先例同款——新域落地的既定工具面）；**键路由迁移**：AI 面复用的 6 键（安全/确认/仪表盘/治理/制品浏览器/AI 助手）按 ≥2 域 → common 规则迁 common.ts（值随迁手填），console.ts 增 AI 助手（⌘J）/清孤儿 2 键（AI 助手（Phase 5 上线）/占位）；regen-catalogs 保值重算（3,118 调用点 / 2,345 键）
  10. e2e（e2e/p5/ai-foundation.spec.ts 六腿）+ p4 palette spec 契约翻新（AI 条目 disabled→enabled+开 drawer；两处竞态断言修——见 Compatibility）
Files:
  新增：web/src/components/ai/{AiAssistant,ChatPanel,MarkdownText,ToolCards}.tsx；web/src/lib/ai/{context,provider}.ts；web/src/i18n/locales/en/ai.ts；web/src/i18n/manifests/zh/ai.json；web/e2e/p5/ai-foundation.spec.ts
  修改（src）：web/src/app/shell/{AppShell,CommandPalette,Topbar}.tsx；web/src/stores/ai-store.ts；web/src/i18n/index.ts；web/src/i18n/locales/en/catalogs.ts；regen 波及 en/{console,repositories,artifacts,search,common}.ts + manifests/zh 同域（键路由迁移保值重算——净变化 = 6 键迁 common/console 增 1 清 2/ai 域 50 新键）
  修改（配置）：web/package.json + package-lock.json（+3 依赖）；web/scripts/assert-i18n.mjs + web/scripts/t463/regen-catalogs.mjs（DOMAINS +'ai'——新域落地的既定工具面改动，T-514 先例）
  修改（锚册）：docs/design/console-ux.md §10.10 P5 批（28 名 ai-* 锚入册——按 P2/P3/P4「FE 批入册、conductor 收编」先例；行级诉求即该节）
  修改（e2e 存量）：web/e2e/p4/command-palette.spec.ts（AI 条目契约翻新 + 两处竞态断言确定性化）
Tests:
  新 spec 六腿（e2e/p5/ai-foundation.spec.ts）：
  ① 三入口开合（顶栏钮/⌘J toggle 往返/palette-ai 条目）+ Esc 关闭 + 焦点圈进（activeElement 落 drawer 内断言）+ ai-close 锚
  ② 上下文徽章正确性：Explorer 路由（m8-perf-local repoKey 形 + 域标签）/ builds name/number 链 / 仓库列表域名形（无坐标链负断言）+ 发送后 ai-msg-context-badge 首帧注入 + 回显坐标走 markdown 行内码（code 元素断言）
  ③ 消息渲染分层：空态+建议 chip（fill 语义）/ ToolCallCard 参数折叠展开 / ToolResultCard 假表格（docker-prod 行）/ ai-code-block（items.find AQL）+ ai-copy-code / ai-md-table / echo markdown strong
  ④ confirm 闭环：ConfirmCard 参数表（repoKey demo-local）→ 确认（ai-confirm-result 已确认 + 成功文案重入追加）→ 取消臂（已取消 + 取消文案）；scope 断言到消息级（多轮 ai-confirm-result 并存）
  ⑤ 零网络断言：page.route 计数 /binflow/(api|event)/ 面——载入收口 1s 后开窗，echo+tool+confirm 全交互 hits=0（mock provider 组件契约的 e2e 面）
  ⑥ axe 双主题（drawer 开合态 + 消息流有内容后扫描；暗色 reload 重开重发复扫）serious+critical=0
  四态覆盖：空（①空态 chip）/ 加载（流式帧 + ai-typing 指示器实现；12ms 帧时窗不 e2e 断言——登记 Risks）/ 缺省（②③④消息流）/ 错误（③模拟错误臂：错误卡+详情+重试原位重生成再抛——确定性闭环）
  回归：p4+p5 = 16 passed + 1 skipped（m8 setmeup OIDC 环境腿，既有 skip）；m8 全族+m9/console-fr82+repositories+rbac+login+console-smoke = 87 passed + 1 skipped 0 failed
Commands:
  cd web && npm install @assistant-ui/react react-markdown remark-gfm
  npm run typecheck && npm run lint && npm run build
  node scripts/t463/regen-catalogs.mjs && node scripts/assert-i18n.mjs
  node scripts/anchor-audit.mjs --ledger
  make console-size（repo root；dist 拷贝 internal/console/dist 后）
  BINFLOW scratch 实例：/tmp/fe-p5-e2e/binflow.yaml（listen 127.0.0.1:18180，data_dir /tmp/fe-p5-e2e/data——用户实例 ./data 零接触）+ bin/binflow-server 重铸（零 Go 源改动，纯 dist 重嵌入）+ BASE=http://127.0.0.1:18180 npx playwright test e2e/p5/ e2e/p4/（16+1s）；e2e/m8/ e2e/m9/console-fr82.spec.ts e2e/repositories.spec.ts e2e/rbac.spec.ts e2e/login.spec.ts e2e/console-smoke.spec.ts（87+1s）
  调试腿（已删）：node scripts/fe-p5-debug.mjs（drawer DOM/console dump——Duplicate key toolCallId 根因定位）+ @assistant-ui/core/internal LocalThreadRuntimeCore node 实证（addToolResult 重入 messages=[user]、unstable_getMessage 含 resolved 结果）
Outputs:
  typecheck：tsc --noEmit 零输出（过）
  lint：0 errors / 45 warnings（P4 存量 45 同基线——本票净增 0：aiMounted 闩改 store 侧（openDrawer 一并 set mounted）避掉 set-state-in-effect 新告警）
  build：assert-tokens OK（css 12 + tsx 97——零文件级豁免）+ assert-i18n OK（3,118 调用点 / 2,345 键，en 包与 zh 清单同构）+ vite ✓ built 7.20s + relink-assets verified + wire-brand-assets 7 件
  console-size：1,015,987 bytes gzip（<5MB 预算 20.3%；主壳 index 550.72KB raw/177.06 gzip 与 P4 基本持平，AI 懒分片 AiAssistant-*.js 461.44KB raw/136.52 gzip 独立 chunk——主壳零增量，首开后缓存；比 P4 的 876,407 净增 139.6KB 全部落在懒分片）
  anchor-audit --ledger：PASS（unregistered/broken 双零，retired 全在表；src 锚家族 1,025 → 1,025+28 ai 族）
  e2e：p5 六腿绿；p4+p5 16 passed + 1 skipped；宽域回归 87 passed + 1 skipped 0 failed
Compatibility:
  锚册对照：§10.10 P5 批 28 名新锚入册（ai-open/ai-drawer/ai-close/ai-context-badge/ai-msg-context-badge/ai-empty{,-suggest-storage,-suggest-create}/ai-thread/ai-typing/ai-msg-user-<i>/ai-msg-assistant-<i>/ai-input/ai-send/ai-error{,-retry}/ai-code-block/ai-copy-code/ai-md-table/ai-tool-call{,-args}/ai-tool-result{,-table}/ai-confirm{,-params,-cancel,-accept,-result}）——P4 批 §10.9 同款格式；两处 prose token 误入 ledger（ai-store/ai-filename）已改写规避（P3「文件名与机制词」同坑）
  契约漂移（无行为虚构；两条 P4 spec 修——预存竞态断言本票复跑翻出）：
  D-P5-1 palette 建仓动作断言竞态：P4 断言 /admin/repositories/new?rclass=local 终态 URL——该 URL 是 T-443 RepoCreateCompat 的入口形，确定性终态为 /admin/repositories/local/new（replace 重定向）；P4 期绿系瞬时 URL 命中竞态（冷实例窗口）。已按确定性终态断言
  D-P5-2 palette 主题/AI 条目点击竞态：35 项列表恒滚动（max-h-320），底部条目裸 click 的 Playwright 自动滚动在 cmdk 列上失灵（误报 overlay 拦截——elementsFromPoint 视口外返回 []）；修 = 过滤收窄（'主题'/'助手'——注意 'AI' 会命中全部 /admin 条目 URL 段）+ 主题腿 click / AI 腿 Enter 键激活（spec 自有键盘纪律）
  契约与后端不一致：无（零 Go 改动、零 API 消费新增——AI 面零网络经 route 计数实证）
Security:     XSS 面：mock 响应模板与用户回显全部经 react-markdown（escape-by-default，不启 rehype-raw——原始 HTML 不透传）；用户消息正文 = React 文本节点（whitespace-pre-wrap 转义渲染）；工具参数/结果表值走 JSX 文本；上下文哨兵 = 机器生成前缀 + 精确前缀解析（损坏非 JSON 时整段按裸文本诚实降级）；敏感信息不落前端日志（drawer/console 零 AI 面日志输出——调试腿已删）
Performance:  AI 面整体懒分片（assistant-ui runtime + react-markdown + 卡片族 136.52KB gzip 独立 chunk——主壳零增量、首开后缓存）；会话态组件内存自持（不入仓、不持久化——SPA 生命周期界）；mock 帧间隔 12ms×粗粒度段（无逐字符帧）；消息流无虚拟化（单会话消息量 << 阈值——长会话虚拟化留待真后端接入票评估）；console-size 1,015,987B/5MB = 20.3%
Risks:
  R1 assistant-ui 0.15.18 API 面钉版风险：消费面含 unstable_humanToolNames 与 unstable_getMessage（HITL 重入路径——0.15.x 内稳定，跨大版本迁移需复核；已在 provider.ts 头注释留实证依据：options.messages 只含 [user]，resolved 结果仅 unstable_getMessage 可读）
  R2 ai-typing 指示器为瞬态实现（12ms 帧），未做 e2e 断言（时窗竞态）；四态之加载另有流式帧本体与 ai-send 禁发态兜底
  R3 m8 keyboard 树腿在并行满载下偶发 AG Grid pinned-top overlay 拦截（FE-P4 R1 布局债同源——本票复跑一次翻出、单独串行复跑绿；非本票改动面，独立布局票建议维持）
  R4 markdown 渲染层对无限长消息无截断（单条消息超长时 drawer 内滚动承载）——真后端接入时评估消息长度钳制
  R5 react-markdown+remark-gfm 为 architect 拍板口径内的「消息渲染」承载（dispatch 任务面 4 明示 markdown/表格/代码分层；如需换自研 mini-markdown 另立票）
Blockers:     无（六腿绿 + 四门绿 + ledger PASS；R1-R5 为登记项非阻塞）
Next:
  1. 锚册收编：§10.10 P5 批 28 名 con conductor 收编入正册（行级诉求即该节本身；P4 批 §10.9 同流程）
  2. 终验（P5 后即达）：MUI=0/Emotion=0 依赖清零复核（P4 已达成——npm ls (empty) 复验一次）+ capability matrix 全行对账 + embed 冒烟（make console→serve→/binflow/ui/ 200——本票 18180 实例已实证该链）+ 全量 e2e + Compatibility 四问基线
  3. 真后端接入票（Go 侧 LLM/chat 端点立项后）：换 ChatModelAdapter 实现（消费面零改动承诺的验证点）；确认动作回调从 addResult 本地收账换 API 调用；消息长度钳制评估（R4）
  4. 布局票（FE-P4 R1 遗留 + 本票 R3 复现）：树页/主内容 flex 高度链短视口塌缩——建议优先级提升（并行 e2e 稳定性直接受害）
  5. lint 45 warnings 存量清理票（react-hooks set-state-in-effect 类——0 error）
断点快照（连接拒绝复活续跑——2026-09-10 本票内）：断点在「ChatPanel + drawer 壳」编写前（context.ts/provider.ts 初版/ai-store 重塑/MarkdownText/ToolCards 已落盘）；复活后续行完成 ChatPanel/AiAssistant/i18n 域/AppShell+palette+topbar 接线/两轮 bug 修（Radix Content 卸载随葬 runtime → runtime 挂顶层；Duplicate key toolCallId → unstable_getMessage 重入臂）/e2e 六腿/锚册 §10.10/四门+回归。全部完成，无遗留断点。
