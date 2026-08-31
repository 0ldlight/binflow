# BinFlow 候选 1「容器·双箭流」生产件（K56 交付）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/brand/logo/candidate-1/README.md` |
| 票据 | K56（M14 前置资产小票：品牌资产生产化 + T-381 L02 conan 换色） |
| 状态 | v1.2（2026-08-31，候选 1 转正为工作 logo——圈定窗 B2 前截止、用户未推翻） |
| 维护者 | ux-designer |
| 上游依据 | `logo/README.md` §2（候选 1 规格）；`web/src/styles/tokens.css`（色板） |
| 下游消费者 | T-389 / FE 接线票（favicon、登录页、侧栏顶、docs-site 首页）；本票不动 `web/` |

---

## 1. 产物清单

| 文件 | 内容 | 消费位（对应 logo/README §4 用例） |
|---|---|---|
| `mark.svg` | mark 单形，48 网格，亮底色板（容器 `#1d232c` / 双箭 `#0b6bcb`），透明底 | favicon、PWA 浅底变体、docs-site |
| `mark-dark.svg` | 同几何，暗底色板（`#e9edf3` / `#4aa3ff`） | 侧栏顶 24px、登录页品牌区、深底物料 |
| `mark-mono.svg` | 同几何，三形全 `currentColor` | 按钮内、禁用态、打印、单色场景 |
| `lockup-horizontal.svg` | mark 108px + wordmark path，viewBox `674×128`，亮底色板，透明底 | docs-site 首页 navbar logo（浅底） |
| `lockup-dark.svg` | 同几何，暗底色板 | 登录页品牌区（约 48px 高）、侧栏横条 |

五件共同点：**零 `<text>`、零字体族属性、零外部依赖**——SVG 单文件分发不携带字体环境。
旧规格表 `../candidate-1-bin-chevron.svg` 保留为 UX-1 原稿（内含 `<text>`，仅规格用途，不接线）。

## 2. wordmark 字形网格（手工勾画的取径与可复核坐标）

**取径声明：手工勾画 monoline 几何构字，非 Inter 字形转曲。** 理由：

1. K56 执行环境无字体渲染工具链，盲转 Inter 轮廓不可验证；
2. 手工勾画是 `logo/README.md` §3/§5 预留的退路，且**规避 Inter OFL 随附 license 文本的分发义务**（§5：接线票可不再放 `INTER-LICENSE`）；
3. monoline 圆帽与 mark 的 round-cap 折线箭同语言，系统一致性优于混入第二套字形气质。

若日后要换 Inter Bold 轮廓版：用 Inkscape（Text → Path）或 fontforge 重导出后**原位替换两个 `<g>`，接口不变**（viewBox、双色分组、坐标原点均保持）。

**网格规格**（wordmark 本体 `534×100`，lockup 内置于 `translate(140 14)`）：

- cap 高 100（y 0~100），笔宽 16（cap 的 16%，等宽 monoline），圆帽圆角；
- x 字高外缘 72（圆件 o/w 上下各 2 光学溢出，实际外缘到 -2/102）；
- 字距 26（外缘到外缘，机械等距）；i 点 r9（略大于半笔宽 8，光学补偿）；
- 双色分组：`Bin` = 结构色，`Flow` = 品牌主色（§1 双色 wordmark 规则）。

| 字形 | 路径（stroke 16, round） | 说明 |
|---|---|---|
| B | `M8 8V92 M8 8H34A22 22 0 0 1 34 52H8 M8 52H38A20 20 0 0 1 38 92H8` | 竖笔 + 上下双碗（下碗略宽 58>56），碗为半圆弧 |
| i | `M100 36V92` + `<circle cx="100" cy="13" r="9">`（填充） | x 字高竖笔 + 浮点（点顶 y4，略低于 cap 线） |
| n | `M142 36V92 M142 58C142 44 150 36 164 36C178 36 186 46 186 58V92` | 竖笔 + 双立方曲线肩 |
| F | `M228 8V92 M228 8H276 M228 50H266` | 竖笔 + 顶臂 276 / 中臂 266（中臂短 10） |
| l | `M318 8V92` | 全高竖笔 |
| o | `M360 64A30 30 0 0 1 420 64A30 30 0 0 1 360 64Z` | 整圆（心 390,64，r30，正负 2 溢出） |
| w | `M462 36V78A16 16 0 0 0 494 78V36 M494 36V78A16 16 0 0 0 526 78V36` | 双 U 谷（double-u），中竖共用 x494 |

笔画全部为 path（`fill="none"` + `stroke`），i 点为唯一填充件。FE 复核工具建议：把路径逐段投到 534×100 网格（每 10 单位一格）目检。

## 3. favicon / PNG 派生管线（16-32-48 及扩展档）

**本票未生成 PNG**（执行会话无 shell/渲染工具），SVG path 化为硬交付；PNG 生成留给 T-389 或 FE 接线票，命令如下（任选其一，输出等价）：

```bash
# 方案 A：librsvg（brew install librsvg）
cd docs/design/brand/logo/candidate-1
rsvg-convert -w 16  -h 16  mark.svg -o /tmp/favicon-16.png
rsvg-convert -w 32  -h 32  mark.svg -o /tmp/favicon-32.png
rsvg-convert -w 48  -h 48  mark.svg -o /tmp/favicon-48.png
rsvg-convert -w 180 -h 180 mark.svg -o /tmp/apple-touch-icon.png   # PWA/apple（§4 用例 2）
rsvg-convert -w 512 -h 512 mark.svg -o /tmp/pwa-512.png

# 方案 B：无本地依赖，npx headless chromium 渲染
npx -y svgexport mark.svg /tmp/favicon-16.png 16:16
npx -y svgexport mark.svg /tmp/favicon-32.png 32:32
npx -y svgexport mark.svg /tmp/favicon-48.png 48:48

# .ico 合成（16/32/48 三档打进一个 ico）
magick /tmp/favicon-16.png /tmp/favicon-32.png /tmp/favicon-48.png web/public/favicon.ico
```

派生纪律：

- **favicon 用 `mark.svg` 单形**（不用 lockup）——两箭疏密对比在 16px 可辨（logo/README §1 判据，样张已核）；
- 16px 档若 FE 实测发糊，允许**仅 favicon 派生**时把描边箭 stroke 4 提到 4.5，母版不动；
- PWA 512/apple 180 建议在 mark 外加 8% 边距的画布居中（直接 rsvg 渲染会顶格）；
- 暗底 icon 场景（macOS 深色 Safari 标签）可用 `mark-dark.svg` 同管线派生。

## 4. 验证

结构验证命令（有 shell 的执行者跑）：

```bash
xmllint --noout docs/design/brand/logo/candidate-1/*.svg \
        docs/design/brand/package-icons/{mono,brand}/{npm,go,conan}.svg && echo OK
grep -l '<text' docs/design/brand/logo/candidate-1/ docs/design/brand/package-icons/{mono,brand}/ -r \
  || echo "no <text> anywhere"   # 预期：candidate-1/ 与 package-icons/ 全部无 text
rsvg-convert -w 512 docs/design/brand/logo/candidate-1/lockup-horizontal.svg -o /tmp/lockup.png
```

K56 交付时点的静态自查（无渲染器，逐文件人工审计）结论：

1. 五个生产件 + 六枚改动图标：标签闭合、属性引号、path 命令序列（M/V/H/A/C/Z 与参数个数）逐段核对合法；
2. 全部 XML 注释避开 `--` 字符序列（严格解析器对注释内双连字符会拒——**旧稿 `../candidate-1-bin-chevron.svg` 的注释含 `--bf-*` token 名，xmllint 会报错**，属规格表遗留，接线不要直接拷它的注释，见遗留节）；
3. 弧参数可行性：所有 A 段弦长 ≤ 2r（o/w/B 碗为精确半圆，go 的 G 大弧弦 2.446 < 2×2.45）。
