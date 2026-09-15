# T-UIB3 — UI Phase 1 批 3：Inter 上线

Ticket:       T-UIB3 UI Phase 1 批 3（Inter 上线）P1
Role:         dev-frontend（area = web/src/design-system 字体面 + 表格数字特性点）
Input:        docs/design/design-system-plan.md §5（字体接入方案定案）、§3.2（九级字阶/tnum 规则）、§6 批 3 行、§7 门、§8 R6；批 0-2 产出（design-system/ 六族 + fonts.css 骨架 + 壳 token 化，golden 在 dev.2534b634）；console-size 基线 1,015,795B（票面）
Changes:
  - 依赖：`npm i @fontsource-variable/inter`（实装 5.3.0，与 §5 实查版本一致）。
  - fonts.css 骨架落地：自持 latin 单轴 @font-face（家族名 'Inter'，font-weight 100 900，font-display: swap，unicode-range 逐字取自包内 index.css 的 inter-latin-wght-normal 块），url() 裸包名引 node_modules 内 48,256B woff2——不引包内 wght.css（其含全 7 子集含 latin-ext 85,068B，且家族名注册为 'Inter Variable' 与 §3.2 栈头不符）。vite 解析裸 url() 成功落 dist/assets/<hash>.woff2，relink-assets face 2 既有覆盖面零改动。
  - fonts.css 补文档基线 `body { font-family: var(--bf-font-sans) }`：**批 3 发现的洞**——FE-P4 MUI 清场（dd5a5e9d）带走 CssBaseline 后，全 web/src 再无非 mono 的 font-family 声明（grep 实证），body 字体悬空在 UA 默认（衬线）；不补此基线，前置 'Inter' 无生效点。
  - tokens/typography.css：--bf-font-sans 头部前置 'Inter'（栈序 §3.2「CJK 回退栈原样保留」为准——'Segoe UI' 为既有栈成员原样保留，§5 栈行系示意）。
  - main.tsx：fonts.css 在全部皮肤（base/pages/governance/tailwind）之前引入——@font-face 是文档全局注册不随级联序，放首位使字体请求在样式表头部即被发现（swap 窗口最短）；body 基线规则居级联最弱位，共存期旧全局层仍可覆盖（与 tailwind.css 末位引入的让位纪律同构）。注释已写进两文件。
  - tabular-nums 按仓库现行模式补两处：styles/pages.css `.filter-bar .count`（旧栈 CSS font-variant-numeric，security/repositories 两域 .table-foot 同款——审计页计数「本页 100 条」即此类）；components/layout/pager.tsx `.pager-range`（新栈 Tailwind `tabular-nums` 工具类——全站表格分页区间）。审计表时间/动作/路径列本身已是 font-mono（monospace 数字不抖，无需 tnum）。
Files:
  - 修改：web/package.json、web/package-lock.json（+@fontsource-variable/inter@5.3.0）
  - 修改：web/src/design-system/fonts.css（骨架→落地 + body 基线）
  - 修改：web/src/design-system/tokens/typography.css（栈前置 'Inter'）
  - 修改：web/src/main.tsx（fonts.css 引入 + 级联注释）
  - 修改：web/src/styles/pages.css（.filter-bar .count + tabular-nums）
  - 修改：web/src/components/layout/pager.tsx（pager-range + tabular-nums）
  - 新增：reports/agents/T-UIB3.md（本文件）；删除：无
Tests:
  - 未新增 spec（批 3 票面无 spec 交付项；字体面证据走计算样式探针 + 全量 e2e 对照，见 Outputs）。design-baseline.spec.ts 按票面指示未跑（golden 翻新归 conductor）。
  - 计算样式探针（一次性脚本 /tmp/binflow-t-uib3/font-probe.mjs，不进仓）：zh/en × login/repos 四态——body/nav/dense computed font-family 全部 = `Inter, system-ui, -apple-system, "Segoe UI", "PingFang SC", ...`；document.fonts.check('16px Inter') / 500 / 600 = true；网络面恰好 1 个字体请求（/binflow/assets/inter-latin-wght-normal-Dx4kXJAl.woff2）。
  - zh 回退像素证据（canvas toDataURL 逐字节比对）：应用栈 zh 串 ≡ 强制 'PingFang SC'（true）、≠ serif（false）——zh 段落命中 PingFang 无衬线回退，无衬线/缺字。
  - tabular-nums computed 验证：pager-range / .table-foot / audit-count 三面均 `font-variant-numeric: tabular-nums`。
  - zh/en 目检截图（登录页+仓表各双语）：/tmp/binflow-t-uib3/shots/{login,repos}-{zh,en}.png（会话期证据，不进仓）。
Commands:
  - `cd web && npm i @fontsource-variable/inter`
  - `cd web && npm run build`（接线前=基线 1,015,795B；接线后 1,016,035B）
  - `cd web && npm run typecheck` → tsc --noEmit 零输出（通过）
  - `cd web && npm run lint` → eslint 0 errors / 45 warnings（全部 react-hooks 系存量，均不在本票改动文件）+ assert-i18n OK（3118 调用点 / 2345 键）
  - `make console` → `console SPA payload (gzip, js+css): 1016035 bytes`
  - `make docs && make build` → 二进制 47,922,192B
  - `BINFLOW_HOME=/tmp/binflow-t-uib3/homeN BINFLOW_ADMIN_PASSWORD=password BINFLOW_SERVER__LISTEN=127.0.0.1:<8163-8166> ./bin/binflow-server serve`（隔离实例 ×4，禁 docker、未触 172.16.58.130；用后按端口精确击落）
  - `BASE=http://127.0.0.1:<port> node scripts/seed-m8.mjs`（10291 节点树验证）
  - `BASE=... npx playwright test --grep-invert baseline` ×3（见 Outputs 对照矩阵）
  - `BASE=... npx playwright test e2e/m9/console-fr82.spec.ts --workers=1` ×2
  - `BASE=... npx playwright test e2e/shell-layout.spec.ts e2e/m16/t464-i18n-bilingual.spec.ts --workers=1`
  - `curl -I http://127.0.0.1:8166/binflow/assets/inter-latin-wght-normal-Dx4kXJAl.woff2` → 200 / font/woff2 / 48256B / `Cache-Control: public, max-age=31536000, immutable`
Outputs:
  - 四门：build（assert-tokens + assert-i18n + vite + relink + brand）绿；typecheck 绿；lint 绿（45 存量 warning 非本票面）；console-size 绿（5MB 预算）。
  - **console-size 前后对比**：前 1,015,795B → 后 1,016,035B（js+css gzip，+240B = @font-face/body 规则文本）；woff2 gzip 48,288B（raw 48,256B）。**批 3 预算腿 = +48,528B ≤ +50KB**，通过。dist 恰好 1 个 woff2（latin-ext 及其余 6 子集未引）。
  - **e2e 全量对照矩阵（--grep-invert baseline，三轮均本机隔离 community 实例 + 新种子）**：
    | 轮 | 构建 | 结果 |
    |---|---|---|
    | run1 :8163 | 字体 | 385 passed / 21 skipped / 12 did not run（55 失败证据目录；输出经 tail 截断，以 run2 为准） |
    | run2 :8164 | 字体 | 388 passed / **55 failed** / 21 skipped / 9 did not run，EXIT=1 |
    | run3 :8165 | 无字体（stash 本票改动重建二进制） | 389 passed / **54 failed** / 21 skipped / 9 did not run，EXIT=1 |
    失败集 diff：字体轮独有 1 条（m9/console-fr82.spec.ts:102 tree filter）→ `--workers=1` 串行重跑该 spec 通过（轮内另冒 1 条同族 :62，再重跑整 spec 7/7 全绿）→ 按 web/README flake 协议（串行绿=过）定谳为并行态 flake，非字体。其余 54 条两轮逐条相同 = 与字体无关的环境性失败。
  - 54 条环境性失败的根因证据：三轮 server.log 均见 run 中途 `httpapi: license installed tier=pro`（run1 04:48 / run2 05:13 / run3 05:33，POST /binflow/api/system/license 201，licensee "T-461 e2e"）——m16/remote-browse-tree.spec.ts 的 pro 租约按设计临时装 license，但其 afterAll 卸载未执行（租约文件 /tmp/t461-license-lease-*.txt 三轮均保持追加原序、实例终态仍 pro），community 姿态断言（m10 L27 系列、t441/t390 门控徽章等）在其后全红。serial 复核（community 净实例）：shell-layout 3/3 绿（含 shell axe 双主题 0 serious/critical）；t464 两条（en sampling / en date format）仍红——该两条在无字体轮同样红，属 develop 既有问题非本票引入（证据 /tmp/binflow-t-uib3/e2e-run3-nofont.log）。
  - axe 双主题 shell 页：shell-layout.spec.ts:79「shell axe: serious/critical zero, both themes」并行轮与串行复核均绿 = 零新增。其余 axe 腿（t449/t414/t443/t419/t457）的红均落在 54 条环境集内（两轮相同）。
  - R6（latin-ext 是否引入）：en 目录包 14 文件字符串字面量全量扫描——latin-ext 区（U+0100-024F / U+1E00-1EFF 等）字符为零；字面量中 latin 子集外字符仅符号类（←→✓✗⌘⚠ⓘ⊘≥≤∨∪≡②⬆⃠），走栈内 system 回退（与 CJK 同一纪律）。**结论：无引入依据，latin-ext 不引（+85KB 免付）**。
Compatibility: 锚册无涉（字体面不在 parity 锚行内）；契约无涉（纯前端静态资产）。无契约漂移。
Security:   纯静态字体资产本地打包（SIL OFL，可自由嵌入分发）；无用户内容渲染面变化、无敏感信息进前端日志。
Performance: +48,528B（gzip 口径）≤ +50KB 批 3 预算腿；woff2 走共享挂载 immutable 缓存；swap 首帧 system 栈不阻塞渲染；无大数据面变化（分页/虚拟滚动不动）。
Risks:
  - FE-P4 遗留的 body 字体悬空在本批补上（fonts.css body 基线）——这是批 2 golden 之后、批 3 golden 之前的预期视觉变更面，conductor 封票翻新 golden 时应整体承接（Inter 到位 + Times→sans 基线双层变化）。
  - latin 子集外的 latin 字符（重音字母等）未来若进 en 文案会走 system 回退（字形混排）；en 现役文案无此类字符（R6 实测）。
  - e2e 基建有三个非本票问题（见 Next）：pro 租约卸载不执行毒化全量跑、少量 "did not run" 中断（12/9/9 三轮复现）、t464 两条 develop 既有红。
Blockers: 无（批 3 交付门全过；e2e 门按对照矩阵 + flake 协议定谳——字体零责任有 run3 同失败集实证）。
Next:
  - conductor：design-baseline golden 翻新（批 3 变更面 = Inter + body 基线）。
  - 建议单独提票（e2e 基建，非本票 area）：m16/remote-browse-tree.spec.ts pro license 租约卸载失效（租约文件原序残留 + 实例终态 pro），全量跑中段毒化 community 断言——三轮日志与租约文件为证；顺带查全量跑尾部 9~12 条 "did not run" 的 worker 中断面。
  - 建议排查（develop 既有红，非本票）：t464-i18n-bilingual en sampling / en date format 两条在本机净 community 实例串行仍红。
  - §5 锚册无需更新；fonts.css 头注已留「不引 wght.css 全子集」的管道决策记录。
