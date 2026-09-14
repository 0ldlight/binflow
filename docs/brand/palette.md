# Binflow 品牌色板提案（B-1 视觉规范起始）

| 项 | 值 |
|---|---|
| 文档 | `docs/brand/palette.md`（swatch 图 `palette.svg` / 渲染件 `palette.png`） |
| 轨道 | B-1 Binflow 品牌系统设计 |
| 状态 | v1.0 提案（2026-09-14）——**落地归 B-3 设计系统票统一改 `web/src/styles/tw/tokens.css`，本票不动 web/src** |
| 上游依据 | 宪章 §10（accent 蓝/紫向）、§16（避 JFrog 橙）；现役 token 基线 `web/src/styles/tw/tokens.css` + shadcn 桥接 `tailwind.css` |
| 对比度 | 全部为 WCAG 2.1 实测值（脚本复算，非估算） |

---

## 0. 设计立场

1. **蓝紫向、但克制**：主 accent 取靛蓝（hue ≈ 228°），紫作次色/渐变端点——全站一紫会淹没「制品仓库的工程感」。品牌色相域整体压在 220°–260°，与 JFrog 橙（≈35°–45°）彻底解耦。
2. **中性阶零新值**：现役蓝灰中性阶（三阶纵深体系）质量足够，全部收编为品牌中性谱，不做第二套灰。
3. **ink 即 --bf-text**：logo 结构色直接用正文色 token，品牌与 UI 同源（UX-1 既有纪律，延续）。

## 1. 品牌双色

### 1.1 Flow Indigo（主 accent）

| 主题 | 值 | 用途 | 实测对比度 |
|---|---|---|---|
| 亮 | `#3D63F2` | 主操作/链接/焦点环/logo accent | on white **4.92** · on bg `#f3f5f7` **4.50**（AA）· white-on **4.92**（AA） |
| 暗 | `#7B9EFF` | 同上（暗色档） | on bg `#12161d` **7.06** · on surface `#1a202a` **6.37** · ink `#0b0e13`-on **7.53** |

- 替换现役 `--bf-accent`（亮 `#0b6bcb` / 暗 `#4aa3ff`）。亮色档对比度略降（4.83→4.50 on bg）仍在 AA 线上；暗色档反而升（`#4aa3ff` on bg ≈ 6.6 → 7.06）。
- 暗色 accent-fg 维持 `#0b0e13`（深字浅底，7.53:1，现役同策略）。
- **info 状态色不动**（亮 `#0b6bcb` / 暗 `#6cb2ff`）：accent 迁往靛蓝后，info 保留原工程蓝，形成「信息蓝 vs 品牌靛」的双蓝分工，避免语义色与品牌色混用。

### 1.2 Vault Violet（次品牌色）

| 主题 | 值 | 用途 | 实测对比度 |
|---|---|---|---|
| 亮 | `#7C5CFF` | 品牌渐变端点、插画、hover 强调、营销素材 | on white 4.35——**仅装饰档，不作正文/链接色** |
| 暗 | `#A78BFF` | 同上（暗色档） | on bg `#12161d` 6.72 |

- 新增 token 建议：`--bf-violet`（亮/暗两值）→ shadcn 桥接 `--color-brand-violet`。**注意**：shadcn 语义里 `secondary`/`accent` 已被 surface 系占用，品牌次色必须用独立命名，不挤语义槽。

### 1.3 Flow Gradient（品牌渐变）

- 亮：`linear-gradient(135deg, #3D63F2, #7C5CFF)`；暗：`linear-gradient(135deg, #7B9EFF, #A78BFF)`。
- 新增 token 建议：`--bf-gradient-brand`（双主题两值）。
- **纪律延续**：渐变只进品牌时刻（登录页 hero、文档站头图、启动屏、空态插画），组件默认态禁渐变（MUI 时代「不使用渐变」的纪律放宽到「品牌层独占」，与宪章现代化方向一致）。

## 2. 中性阶（收编现役值，零改动）

### 亮色主题

| 阶 | 值 | 现役 token |
|---|---|---|
| surface-1 | `#FFFFFF` | `--bf-surface-1` |
| bg | `#F3F5F7` | `--bf-bg` |
| surface-2 | `#E9EDF1` | `--bf-surface-2` |
| surface-3 | `#DFE5EC` | `--bf-surface-3` |
| border | `#D3DAE2` | `--bf-border` |
| border-strong | `#9AA6B4` | `--bf-border-strong` |
| text-muted | `#646F7B` | `--bf-text-muted` |
| text-2 | `#55606E` | `--bf-text-2` |
| text | `#1D232C` | `--bf-text`（= 品牌 ink） |

侧栏深阶层（亮暗主题同为深底）：`#1B2430` / hover `#242F3E` / active `#2C3949`；文字 `#EEF2F7` / `#A3B1C2`。

### 暗色主题

| 阶 | 值 | 现役 token |
|---|---|---|
| bg | `#12161D` | `--bf-bg` |
| surface-1 | `#1A202A` | `--bf-surface-1` |
| surface-2 | `#202834` | `--bf-surface-2` |
| surface-3 | `#262F3D` | `--bf-surface-3` |
| border | `#2B3442` | `--bf-border` |
| border-strong | `#414C5E` | `--bf-border-strong` |
| text-muted | `#7F8A99` | `--bf-text-muted` |
| text-2 | `#9AA4B2` | `--bf-text-2` |
| text | `#E9EDF3` | `--bf-text`（= 品牌 ink 暗色档） |

暗色侧栏：`#0B0E13` / `#151B25` / `#1D2532`。favicon 底 `#10161F` 取 bg 与侧栏之间，属品牌资产专用值（不进 UI token）。

## 3. 状态色与 pkgicon 族：零改动

success / warning / danger / info 四族维持现役语义值；`--bf-pkgicon-*` 协议标族维持原生品牌色（协议 logo 与本品牌色解耦的既有决策不变）。

## 4. shadcn token 映射（B-3 落地清单）

| shadcn 桥接（tailwind.css） | 现役来源 | 提案改动 |
|---|---|---|
| `--color-primary` / `--color-ring` | `var(--bf-accent)` | **值换** `#0b6bcb`→`#3D63F2`（亮）/ `#4aa3ff`→`#7B9EFF`（暗）——桥接行本身零改动 |
| `--color-primary-foreground` | `var(--bf-accent-fg)` | 亮 `#FFFFFF` 维持 / 暗 `#0B0E13` 维持 |
| （新增）`--color-brand-violet` | — | `var(--bf-violet)`：`#7C5CFF` / `#A78BFF` |
| （新增）渐变消费 | — | `--bf-gradient-brand`（品牌层专用，组件默认态禁用） |
| `--color-background/card/border/...` | `var(--bf-bg/surface-*/...)` | 零改动（中性阶收编） |
| `--color-sidebar*` | `var(--bf-sidebar*)` | 零改动 |

## 5. 同步契约（改色必查清单）

1. `web/src/styles/tw/tokens.css`（accent 两值 + violet/gradient 新增；旧 `styles/tokens.css` 若仍在并行服役期则同改——以 MUI 退役终验状态为准）。
2. `brand/` 五件 SVG 的 `<style>`/fill 字面量（各文件 `<desc>` 已登记本契约）。
3. `docs/brand/palette.md` + `palette.svg`。
4. 亮色 on-bg 4.50:1 贴 AA 线——accent 再调深/浅必须重跑对比度复算（脚本在 B-1 会话记录：WCAG 相对亮度公式直算）。
5. accent 换值影响面 = 全站链接/主按钮/焦点环/选中态：B-3 票落地时须带双主题 axe 回归与既有 e2e 快照核对，禁止在品牌票内顺手改 `web/src`。
