# T-PENPOT-A — Penpot Phase A：Artifactory UI 证据爬取（套件交付 + 执行移交）

```
Ticket:        T-PENPOT-A [P0] Penpot 落图 Phase A — :8082 UI 全量证据爬取（loop-state user_directive_penpot, in_flight）
Role:          ux-designer
Area:          tools/penpot-sync/capture/（新建）+ docs/reverse/frontend/parity-capture/（输出目录，票面显式授权）
Input:         派发指令 + routes.yaml（119 路由 E2 提取集）+ screens.yaml（41 屏四态账）+ api-map.yaml（SSR/REST 双面认证证据）+ console-ui.md §2/§3/§4（登录选择器/页面骨架/交互流走查锚）
Changes:       交付可串行执行的爬取套件（非执行产物）：① crawl-manifest.json——67 屏爬取清单（每条 path 派生自 routes.yaml 提取集，范围外 projects/pipelines/xray 显式 skip 留理由）+ 13 弹窗清单；② capture.mjs——十阶段严格串行（preauth/login/nav/setup/screens/dialogs/tokens/states/catalog/cleanup），登录走 UI 表单（/ui/api/v1/* 拒 basic auth 的 E4 证据），REST 仅兜底/清理且只打 /artifactory/api/*；③ README.md 运行手册；④ 输出目录 .gitignore 防百张 PNG 误提交
Files:         tools/penpot-sync/capture/crawl-manifest.json（新建）；tools/penpot-sync/capture/capture.mjs（新建，827 行）；tools/penpot-sync/capture/README.md（新建）；docs/reverse/frontend/parity-capture/.gitignore（新建）；本日志。锚册/母册零改动（本票非锚册票）
Tests:         本角色无 Bash——执行类自测不可行，以静态四门替代：①凭据门：Grep 'JFrog@|password[:=]' 于 tools/penpot-sync 零命中，凭据仅 process.env（capture.mjs:49-54）；②载体真实性门：web/node_modules/playwright/package.json 实存（Glob 过，1.62.1），createRequire 解析路径 '…/../../web/package.json' 与目录层级核对一致；③结构门：全文通读一轮，抓出并修复 1 处运行时必炸 bug（sidebar.html 落盘括号分组致 .slice 落在 Promise 上）+ 4 处死码/重复请求；④范围门：落盘仅 tools/penpot-sync/** + 授权输出目录 + 本日志，未触 BOARD/PRD/web-src/matrix/reverse-src
Commands:      Read×4（routes/screens/api-map/README 种子）+ Read×3（loop-state/playwright pkg/console-ui grep）+ Glob×4 + Grep×3（penpot 全仓定位/凭据红线/登录选择器锚）+ 通读 capture.mjs 全文
Outputs:       母册章节=无；锚册版本=零改动；套件 4 文件。**执行产物（nav-tree.json/截图/tokens.json/screens-catalog.json/states-gaps.md）= 0——未执行**
Compatibility: 不涉及 matrix.yaml 互链（本票为证据工程票非锚册票）。与 screens.yaml 对账逻辑内置于 catalog 阶段（seed 映射 + newScreens/missingSeeds 标注），待执行后生效
Security:      凭据红线=env 注入零落盘（grep 自查过）；登录错误态仅 1 次错误尝试（防锁账）；写腿全部 audit-probe- 前缀且 cleanup 阶段 REST 删净+复核零残留；参照实例不注入 5xx（states-gaps 静态登记）
Performance:   截图预算 MAX_SHOTS=140 硬闸 + 同形态屏指纹去重（标题+Tab+列头+heading）压向 60-100 张目标；树深链/虚拟滚动大量级场景列为 gap 不硬造
Risks:         ①套件未经活体执行——选择器多为证据锚+兜底链（console-ui 7.84.10 走查锚 vs 实例 7.161.20 存在版本偏斜，wizard/树节点/行删除按钮可能形态漂移），首轮执行预计需按 run-log/gaps 迭代 1-2 轮；②UI 触达失败自动 REST 兜底造数——弹窗形态未取证时会如实登记 gap 不静默；③登录若现 SSO 形态则落 login-form.html 证据后退出码 1，不硬闯
Blockers:      **本角色工具面无 Bash——无法执行 Playwright**。套件已就绪，执行需移交 conductor 主会话或 dev-frontend（命令在 README：ARTIFACTORY_USER=… ARTIFACTORY_PASSWORD=… node tools/penpot-sync/capture/capture.mjs）
Next:          ①Bash 会话执行套件（预计 10-20 分钟）→ 检 run-log.json 迭代选择器 → Phase A 收口（屏覆盖/截图数/弹窗数/tokens 摘要/states-gaps/screens.yaml 对账差五项回报补齐）；②执行后若 nav-tree 发现清单外活体路由 → screens-catalog newScreens 转 reverse-engineer 增补 routes.yaml；③Phase B spec 生成以 parity-capture/ 为输入
```

## 断点快照（如执行会话中断）

- 已完成：爬取套件全套（manifest + capture.mjs + README + 输出目录闸）+ 本日志。
- 未完成：**执行**（十阶段一次未跑）；五项收尾回报数字（屏覆盖数/截图数/弹窗数/token 摘要/对账差）全空。
- 断点位置：capture.mjs main() 第一行之前——代码就绪零运行。
- 恢复建议：Bash 会话按 README 命令起跑；首跑重点看 run-log.json 的 phases[].error 与 states-gaps.md，按 gap id 回补选择器（wizard 弹窗、树节点、行删除按钮三个最可能漂移）；PHASES env 支持单阶段重跑（如 PHASES=nav 只补导航树）。
