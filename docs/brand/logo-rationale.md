# Binflow 品牌方向族谱与选定论证（B-1）

| 项 | 值 |
|---|---|
| 文档 | `docs/brand/logo-rationale.md` |
| 轨道 | B-1 Binflow 品牌系统设计（UI/UX 现代化阶段宪章） |
| 状态 | v1.0（2026-09-14：12 方向探索 → 选定 D1「校验和点阵」深化；5 件生产 SVG + favicon 落 `brand/`） |
| 上游依据 | 宪章 §16（禁云朵/文件夹/抄 JFrog；关键词 Binary/Artifact/Flow/Pipeline/Distribution/Storage/Infrastructure；避 JFrog 橙）、§10（accent 蓝紫向）；产品定位 Modern Artifact Repository Platform（Artifactory 兼容 + 云原生 UX + 开源身份） |
| 下游消费者 | B-3 设计系统票（token 落地）、FE 接线票（favicon/侧栏/登录页）、docs-site / Fern 文档站 |
| 血统 | 承 UX-1（2026-08-30 三候选 + candidate-1 转正）——本册**取代**其 mark，**继承**其 wordmark 字形几何（§8） |

---

## 0. 设计约束（先于美学，全部来自宪章与产品事实）

1. **禁令**：无云朵（SaaS 泛词）、无文件夹（存储泛词）、不抄 JFrog 任何形；品牌色避开 JFrog 橙的色相域（≈35°–45°）。
2. **关键词域**：Binary / Artifact / Flow / Pipeline / Distribution / Storage / Infrastructure——隐喻只能从这七个产品自身概念里取。
3. **开源身份 + 现代感**：几何可网格复画、无文化特定符号、无拟物；形态语言与 Penpot/Linear 式的「克制的几何 + 单色结构 + 一抹品牌色」同代。
4. **工程硬约束**：48 网格可重画；16px favicon 可辨；亮暗双主题双色值；零字体依赖（零 `<text>`）；单色（currentColor）可用。

## 1. 设计判据（六条，后文矩阵引用）

- **J1 产品真值**：隐喻必须映射 BinFlow 的原子事实（sha256 校验和、二进制制品、进仓-出仓的流），不做「科技感装饰」。
- **J2 16px 可辨**：浏览器标签页是 logo 最高频曝光位。
- **J3 撞形安全**：与 JFrog / Docker / GitHub / Helm / Kubernetes 生态既有视觉无碰撞，无「泛科技」既视感（区块链六边形、SaaS 云、齿轮）。
- **J4 双色系统**：ink（结构）+ accent（品牌）双色即成立；去色后轮廓完整。
- **J5 可延展**：mark 能否派生品牌纹理（空态/hero/插画）与头像/PWA 等周边，不只是一次性字形。
- **J6 工程可维护**：path 数少、几何可公式化（改 token 换色零重工）。

## 2. 方向族谱（12 方向，全部原创几何，SVG 见 `docs/brand/explorations/`）

> 探索期统一用提案色（ink `#1d232c` / accent `#3d63f2`；暗底 `#e9edf3` / `#7b9eff`）。每个方向均经无头光栅化验证（48px 走查 + 16px 双色像素存活断言，`explorations/verify.mjs`），16px 实测结论附在各 rationale 尾。

| # | 方向 | 文件 | 关键词 | rationale 摘要 |
|---|---|---|---|---|
| D1 | 校验和点阵 | `d01-digest-lattice.svg` | Binary·Checksum·Flow | 4×4 砖砌错位点阵 = 静止在仓中的二进制制品；奇数行右移半格让阵列产生方向势（数据在流）；三枚放大 accent 点连成右下对角 = 校验路径穿阵而过。sha256 是本产品每个制品的身份本源，「指纹」直译为视觉。16px：全阵衰减为噪点纹理（ink 74 / acc 33 px 双色存活，但图式弱）→ 选定后以 3×3 浓缩派生解（§5.2）。 |
| D2 | 双箭流 » | `d02-chevron-flow.svg` | Flow·Terminal | 描边箭 + 实心箭的加速对，终端提示符的肌肉记忆；UX-1 candidate-1 的「去壳」现代化。16px 最强（22/28 px，图式完整）。 |
| D3 | 管线拓扑 | `d03-pipeline-node.svg` | Pipeline·CI/CD | 源节点经正交管线到制品节点，CI/CD 拓扑最小形。16px 尚可（55/9）但整体读作「旗帜/提交图」，ownability 弱。 |
| D4 | 仓格流 | `d04-bin-cells.svg` | Storage·Distribution | 圆角仓内 2×2 格位、末格化为出仓箭 = 制品离仓分发。48px 读作「包裹/应用图标」，箭在 16px 丢失（126/12）——存储隐喻强、品牌独特性中。 |
| D5 | B 字标 | `d05-b-counter.svg` | Monogram·Artifact | monoline 几何 B，上字怀嵌右指三角（Flow 藏进 Bin 的首字母）。16px 优（90/10）；但字母标偏 SaaS 通用，且 B 单字丢 Flow 叙事。 |
| D6 | 层积塔 | `d06-strata-stack.svg` | Storage·Versioning | 三层地层 + 塔尖制品 = 版本沉积。16px 优（90/8）但读作「数据湖/金字塔」泛词，撞 J3。 |
| D7 | 分流器 | `d07-split-junction.svg` | Distribution·Topology | 单干入、双支出 = 一份制品分发多消费链（remote/virtual 的本质）。概念准，形读作泛「网络图」；16px 中（57/12）。 |
| D8 | 指纹环 | `d08-hash-ring.svg` | Checksum·Integrity | 断口环 + accent 弧段补全 = 校验闭环。形读作「加载中/电源钮」（57/18），与进度指示符撞形，弃。 |
| D9 | 括号仓 | `d09-bracket-depot.svg` | Binary·Storage | `[ ]` 数组括号 = 程序员的仓，内置字节条。UX-1 candidate-2 血统净化版；整体读作「菜单图标 ≡」，16px 中（100/19）。 |
| D10 | 管道弯头 | `d10-conduit-elbow.svg` | Infrastructure·Pipeline | 粗管入仓、直角弯、箭头出 = 字面 infra 输送。竖管主导后读作「1 + 箭」，16px 中（52/16）。 |
| D11 | 二进制叠印 | `d11-binary-overlap.svg` | Binary·0/1 | 圆（0）× 竖杠（1）交叠、交集染 accent = 两个比特相交成制品。概念最「Binary」，但 16px 读作放大镜/搜索（55/24），歧义致命。 |
| D12 | 字节流 | `d12-byte-stream.svg` | Binary·Flow | 三行错落字节条向右流动、末行 accent 带箭头。流势最好（46/34），但 48px 读作「播放列表/菜单」，列表图标既视感。 |

渲染证据：`explorations/contact-sheet.png`（亮暗两谱全页）+ `sheet-light.png` / `sheet-dark.png`；逐方向 ASCII 走查与像素计数见 `explorations/verify.mjs` 输出（本册 rationale 尾注的数字即来自该实测）。

## 3. 选定：D1「校验和点阵（Digest Lattice）」

### 3.1 判据矩阵

| 判据 | D1 点阵 | D2 双箭流 | D5 B 字标 | D12 字节流 |
|---|---|---|---|---|
| J1 产品真值 | **优**（sha256 指纹 = 制品身份本源；点阵 = binary at rest；对角 = flow） | 良（流有、bin 无） | 中（字母联想，需解读） | 良（binary+flow，checksum 缺） |
| J2 16px 可辨 | 全阵弱（→3×3 派生后优） | **优** | 优 | 中 |
| J3 撞形安全 | **优**（生态无点阵指纹先例；与 Databricks 砖块、Slack 四色块均不同构） | 弱（» 是通用快进/跳过符） | 中 | 弱（列表既视感） |
| J4 双色系统 | **优**（ink 点阵 + accent 路径，去色后阵形完整） | 优 | 优 | 优 |
| J5 可延展 | **优**（点阵可无限外延为品牌纹理——空态/hero/骨架屏的背景 pattern；mark 即种子） | 弱（一次性字形） | 弱 | 中 |
| J6 工程可维护 | **优**（16 个 circle、坐标公式化，零 path 手绘） | 优 | 中（字形 path） | 优 |

### 3.2 选定论证（四条）

1. **它是唯一把三个产品关键词装进一个静止图形的方向**：点阵 = Binary/Storage（制品在仓），错行 = Flow（数据在动），accent 对角 = Checksum/Distribution（校验路径穿仓而过、流向消费端）。Artifactory 兼容仓库的日常就是「字节进仓 → 指纹确权 → 按需流出」，mark 是这个叙事的俯视图。
2. **撞形安全且可注册性最好**：几何点阵 + 单对角 accent 在制品仓生态（JFrog 旋涡橙、Docker 鲸鱼、GitHub 猫、Helm 舵轮、Harbor 灯塔）无先例；D2 的 » 与 D12 的条流是通用 UI 符号，做不了品牌资产。
3. **mark 即品牌系统种子（J5）**：4×4 阵列可以向任意方向外延成 lattice 纹理，直接服务空态插画、登录页 hero、文档站头图——这是字形类方向（D2/D5）给不了的系统杠杆，正对「品牌系统设计」的轨道目标。
4. **弱点有工程解**：16px 衰减用 3×3 浓缩派生的 `favicon.svg` 解（§5.2），且派生保留同一 DNA（点阵 + 对角 accent），认知连续。

### 3.3 落选登记与翻案路径

- **D2 双箭流**：16px 冠军。若用户偏好「终端气质优先于系统感」，翻案成本 = 替换 5 件 SVG 的 mark 组，wordmark 不动。
- **D12 字节流**：动感冠军。若做 motion logo（启动帧），其形可作 D1 的动画终态参考（点阵 → 汇成条流）。
- 其余方向 rationale 内已注明致命伤（撞形/歧义/既视感），不建议翻案。

## 4. 深化规格

### 4.1 mark 几何（48 网格）

- 点阵：4 行 × 4 列，行/列中心 `9/19/29/39`，点距 10；**奇数行（第 2/4 行）x 整体 +5**（`14/24/34`）——砖砌错位，赋予右向势。
- ink 点 `r=3.5`（13 枚），accent 点 `r=3.9`（3 枚：`(24,19) (29,29) (34,39)`）——accent 略大做光学等重补偿（同面积下色彩前进感弱于深 ink）。
- 视觉中心 = (24, 24)（阵列 x/y 跨度 5.5–42.5 对称）。
- 全部元素为 `circle`，无 path、无描边——改色 = 改两个 fill。

### 4.2 favicon 派生（3×3 浓缩）

完整 4×4 在 16px 糊阵（实测），浓缩为 3×3：点心 `9.5/24/38.5`，`r=5`，主对角线 `(9.5,9.5)(24,24)(38.5,38.5)` 三枚 accent——对角 DNA 与完整 mark 一致（右下向）。自含对比度：深墨蓝底 `#10161F`（rx 10）+ 浅点 `#E9EDF3` + accent `#7B9EFF`，亮暗标签栏均成立。16px 实测：ink 219 px / acc 33 px，图式完整（`docs/brand/verify-brand.mjs` ASCII 走查在案）。

### 4.3 wordmark（继承 UX-1 构字）

`BinFlow` monoline 圆帽几何构字（笔宽 16 / cap 高 100 / i 点 r9），字形 path 承 `docs/design/brand/logo/candidate-1/lockup-horizontal.svg` 手工勾画稿——**几何继承、色值换新**：`Bin` = ink，`Flow` = accent（新靛蓝）。产品名拼写维持 `BinFlow`（全仓 README/UI/docs 一致），宪章行文的 `Binflow` 是轨道名而非改名指令——若未来要做 lowercase 品牌化，属独立品牌决策，翻案时只需重勾 wordmark 组。

### 4.4 自适应机制与五件生产件

| 文件 | 内容 | 主题行为 |
|---|---|---|
| `brand/logo-mark.svg` | mark 单形 | `prefers-color-scheme` 媒体查询自适应（亮 `#1d232c`/`#3d63f2` ↔ 暗 `#e9edf3`/`#7b9eff`） |
| `brand/logo.svg` | 横版 lockup | 同上自适应 |
| `brand/logo-light.svg` | 横版 lockup | 亮底钉色（打印/浅色素材） |
| `brand/logo-dark.svg` | 横版 lockup | 暗底钉色（控制台侧栏/暗色文档站） |
| `brand/favicon.svg` | 3×3 派生 + 自含底 | 不随主题（标签栏场景自含对比度最优） |

控制台内联用法（未来接线票）：建议内联 SVG 时把类选择器换成 `fill="currentColor"`（ink）+ `var(--bf-accent)`（accent），随 `data-theme` 即时切换——media query 版留给文档站/README 这类跟随系统的场合。

## 5. 色彩与 token 衔接

- 品牌双色与中性阶的完整提案、WCAG 实测、shadcn token 映射：**`docs/brand/palette.md`**（+ `palette.svg` swatch）。
- 同步契约：改 `--bf-accent` / 暗色 accent 必须同步本册 5 件 `brand/*.svg`（各文件 `<desc>` 已写入该契约）与 palette.md。

## 6. 与 UX-1 旧资产的关系

- **取代**：`docs/design/brand/logo/candidate-1/` 的「容器·双箭流」mark 自本册起降为**历史存档**（不删除——T-389 等已消费记录需可考）；其 `mark*.svg` 五件在品牌接线票合入前仍被现役 UI 引用与否，以 `wire-brand-assets` 构建链现状为准，接线票负责切换引用到 `brand/`。
- **继承**：wordmark 字形 path、双色命名法（结构色 + 品牌色）、「几何可网格重画/零字体依赖/双主题」三纪律、favicon PNG/ico 派生命令（`candidate-1/README.md` §3 的 rsvg/magick 方案沿用）。

## 7. 交付物与验证记录

- 方向探索：`docs/brand/explorations/`（12 SVG + contact-sheet.html + 渲染 PNG ×3 + verify.mjs）。
- 生产件：`brand/` 五件 SVG。
- 验证命令（均已实际运行，输出在案）：

```
node docs/brand/explorations/render.mjs      # → contact-sheet.png / sheet-light.png / sheet-dark.png
node docs/brand/explorations/verify.mjs      # 12 方向 48px ASCII 走查 + 16px 双色断言，全过
node docs/brand/verify-brand.mjs             # 5 生产件亮/暗两档断言，ALL PASSED
```

- 自适应实证（logo.svg 像素采样）：亮 `bStem #1d232c / flowStem #3d63f2` ↔ 暗 `#e9edf3 / #7b9eff`。
