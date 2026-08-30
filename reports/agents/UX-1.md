# UX-1 工作日志 — 品牌资产 + Artifactory 交互对齐规格

| 项 | 值 |
|---|---|
| 票据 | UX-1（插空票，2026-08-30） |
| 角色 | ux-designer |
| 输入 | 用户指令 2026-08-30：① 交互体验对齐 Artifactory（含弹窗/抽屉）② 包型 logo ③ 自设计产品 logo |
| 产出 | 三交付全落盘（见下）；**零现有代码改动、零 git 操作、未碰 BOARD.md** |

## 交付清单

**交付 1 品牌 logo（`docs/design/brand/logo/`）**
- `candidate-1-bin-chevron.svg`（容器·双箭流，**推荐稿**）
- `candidate-2-bracket-stream.svg`（括号·数组流）
- `candidate-3-hex-flow.svg`（六边容器·折线流）
- `README.md`：候选对照表（国际性/技术感/16px 可辨/单色/深浅底五判据）、推荐理由、48 网格几何规格、色板锚定 tokens.css、落地清单 6 用例（favicon.ico/PWA/登录页/侧栏顶/文档站/GitHub）
- 每稿规格表含：mark 96px + 横版 lockup + 暗底版 + 单色 currentColor 版 + 16/20/24/32px 缩放样张

**交付 2 包型图标集（`docs/design/brand/package-icons/`）**
- `mono/` 15 枚（currentColor：列表/表单/树/菜单）+ `brand/` 15 枚（品牌色：包型网格/Addon 矩阵）
- 13 包型：generic/docker/maven/npm/pypi/go/nuget/cargo/conan/helm/helmoci/rpm/deb（文件名 `deb.svg` ↔ wire 值 `debian`，`go.svg` ↔ `go`，已注记）+ addon 2 枚：trashcan（回收站槽）、webhook（统一事件槽）
- `README.md`：光学平衡规则（形族分档占位）、**helm×helmoci 三重区分方案**（实心圆毂粗辐深蓝 vs 空心六边毂细辐青蓝）、逐枚持有方+置信度表、许可姿态（几何极简重绘仅作包型识别，业界同例）、FE 接线注意（npm/go 含 `<text>` 须转 path）

**交付 3 Artifactory 交互对齐规格（`docs/design/console-artifactory-parity.md`）**
- 模式 catalog 15+ 项：N1~N3 导航分域 / M1~M4 弹窗族 / D1~D3 抽屉族 / L1~L4 列表 / F1~F3 反馈
- 每项：Artifactory 行为（置信度高/中/低）→ MUI 组件 → BinFlow 载体（web/src 文件级）→ 差距三档（✅/△/✗）
- §7 页面×模式差距矩阵（17 行 × 8 列）供 PM 起 M14 UI-parity PRD 直接引用
- §8 活体核验清单 V1~V8（本票无浏览器，低置信项不进 PRD 断言）
- §9 不做清单（Non-goal 防线）+ 豁免登记 E1~E7（BinFlow 优于 Artifactory 的安全设计不倒退）
- §10 落地批次建议（批 1：Set Me Up 抽屉化/建仓单 modal/用户组创建 modal/行内 ⋯）

## 现状核对记录（写规格前逐一读过）

- 主题色板：`web/src/app/MuiProvider.tsx`（LIGHT/DARK 字面量）+ `web/src/styles/tokens.css`（--bf-* 权威）——logo/图标色全部锚定既有值，未另起炉灶
- 壳层：`AppShell.tsx` 双模式（APP_NAV/ADMIN_NAV 15 条目）、顶栏搜索真输入框、用户菜单 Quick 动作
- 弹窗基座：`ConfirmDialog.tsx`（danger/confirmDisabled/Esc 双通道）、`SetMeUpDialog.tsx`（720px 居中 Dialog + step-up 双腿）、`DeployDialog.tsx`、`RepositoryFormPage.tsx`（pkg-grid Dialog → 路由页六节表单）
- 包型事实：`lib/repos.ts` PackageType 12 型联合（helmoci 未入前端联合）、`lib/addons.ts` 槽位驱动、`internal/repo/service.go` helmoci 属 v2 族、`internal/license/manager.go` 五核心
- 反馈：`ToastContext.tsx` 右下堆叠、`EmptyState`/`Skeleton` 组件在位

## 设计决策（三条最重要）

1. **品牌主色不换**：logo 与图标全部锚定 `--bf-accent`（#0b6bcb/#4aa3ff）——控制台是第一门面，换色意味着全站链接/按钮/焦点环联动，收益为负；推荐稿 mark 用「容器+双箭」双隐喻（bin/flow + 终端提示符 »），16px 免简化直接可用。
2. **对齐=形态不对齐=功能**：parity 规格只约束弹窗/抽屉/菜单的手势形态，最大差距定为 D1（Set Me Up 居中 Dialog → 右抽屉）与 M1（建仓收单 modal）；同时立豁免登记 E1~E7，把 BinFlow 优于 Artifactory 的安全设计（删除收危险区、keyset 加载更多、token 一次性明文）从「差距」里显式摘出来，防止对齐评审误伤。
3. **低置信不进断言**：Artifactory 行为描述全部标置信度，8 项活体核验点（V1~V8）单列——本票无浏览器，凡凭记忆的细节（toast 锚位、向导宽度等）只进核验清单不进 PRD 验收标准，防 clean-room 纪律下的凭空契约。

## 遗留

1. **推荐稿待圈定**：三候选齐备并推荐候选 1，最终由用户定夺（README §2 留了判据表）；wordmark 生产件须转 path（Inter OFL 授权或手工勾画，README §3）。
2. **FE 接线票未派**（本票零代码改动）：logo 落地 6 用例（brand/logo/README §4）、图标 30 枚进 `web/src/assets/pkg-icons/`（package-icons/README §6，npm/go 转路径前置）、parity 批 1 四项——全部待 PM 起 M14 PRD 后拆票。
3. **活体核验 V1~V8 未执行**（无浏览器）；conan/nuget/docker 鲸腹三枚图标重绘置信度中低，核验后修正。
