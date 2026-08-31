# BinFlow 包型图标集（UX-1 交付 2）

| 项 | 值 |
|---|---|
| 文档 | `docs/design/brand/package-icons/README.md` |
| 票据 | UX-1（插空票：品牌资产 + Artifactory 交互对齐规格） |
| 状态 | v1.2（2026-08-31，K56 生产化：npm/go 字标 `<text>` 转 path、conan 双版 #669ACC 落地——30 枚零 text 依赖；此前 v1.1 同日 T-381 活体对照修正） |
| 维护者 | ux-designer |
| 上游依据 | 用户指令 2026-08-30（② 各协议包型 logo 加上）；`web/src/lib/repos.ts`（PackageType 联合）、`internal/license/manager.go`（五核心包型槽位）、`internal/repo/service.go`（helmoci 属 registry-v2 族）、`internal/webhook/doc.go`（webhook 域） |
| 下游消费者 | FE 票（建仓包型网格 `pkg-grid`、Set Me Up 网格 `smu-grid`、制品树类型列、License & Add-ons 矩阵的图标接线） |

---

## 1. 目录与消费规则

```
package-icons/
  mono/<id>.svg    单色版：全部 currentColor —— 列表「包类型」列、表单、树节点、菜单项
  brand/<id>.svg   品牌色版：官方品牌色（近似） —— 建仓包型网格、Set Me Up 网格、
                   License & Add-ons 矩阵等「包型身份」场景（Artifactory 同款用法）
  README.md        本文
```

- **两版同形不同色**：mono 版不是 brand 版的去色滤镜，而是逐枚校过视觉重量的独立骨架（见 §2）。
- 24×24 viewBox，笔宽基准 2（1.5~3.8 按光学微调），round cap/join。挂进 MUI 用 `IconButton`/`ListItemIcon` 的 20~24px 槽即可，无需再缩放。
- 文件名 = 包型 id（与 `PackageType` 联合对齐）；两处例外：`go.svg`（任务口径；wire 值就是 `go`）与 `deb.svg`（wire 值为 `debian`，目录名取任务口径，FE import 时自行映射）。

## 2. 统一视觉重量（光学平衡，非机械同尺寸）

所有图标画布 24×24，但**占位面积按「形的心量」而非外框对齐**，规则如下：

| 形族 | 占位策略 | 代表 |
|---|---|---|
| 线框立方/包裹（线密度低） | 外扩到 3~21（全幅），靠细笔维持重量 | generic、nuget、rpm |
| 粗线条带（笔画自重高） | 收进到 4~20，笔宽 3.6~3.8 | pypi、conan、deb |
| 密集小件堆叠 | 每件 3.3px 方块、留 0.4px 缝 | docker |
| 圆形全幅符号 | r=8.7（直径 17.4，圆的视觉内收） | helm、helmoci、cargo |
| 字标型 | 字高占 8~10px + 陪衬线 | npm、go |

FE 侧若发现某枚在 16px 下偏轻/偏重，按「加减 0.2px 笔宽」微调，不要整体缩放 svg。

## 3. helm × helmoci 视觉区分方案（同舵轮族，三重可辨）

两型都走舵轮（Helm 生态身份），但任何尺寸下不靠文字也能区分：

| 维度 | `helm` | `helmoci` |
|---|---|---|
| 毂 | **实心圆毂**（r 3.2 填充）——经典舵轮 | **空心六边毂**（六边形 = OCI 工件/容器形，呼应 OCI artifact） |
| 辐 | 8 根粗辐（1.9） | 8 根细辐（1.5） |
| 主色（brand 版） | 深蓝 `#0F1689`（Helm 官方色系） | 青蓝 `#0E7490`（衍生色，暗示「OCI 载具」而非 Helm 本体） |
| 语义 | Helm classic chart 仓（chartmuseum 形态） | Helm chart 以 OCI 工件形态存 registry-v2 平面 |

最小可辨尺寸：16px 下「实心圆点 vs 空心六边」依然成立；色弱用户靠毂形区分，不依赖颜色。

## 4. 逐枚清单（形态注记 / 原标记持有方 / 置信度）

置信度针对「重绘形与官方标的相似度」；低置信项列入活体核验（有浏览器时对照官方标修正）。
**2026-08-31 活体核验（T-381）**：对照源 = t226-artifactory（OSS 7.84.10）建仓向导「Select Package Type」磁贴内联 SVG（JFrog 渲染版官方标，33 包型；分析记录 `reports/agents/T-381.md` 图标腿）。核验方式 = fill 色/path 数/几何包围盒的结构化比对（clean-room：只析不拷，官方矢量零入库）。三枚低置信全部修正：docker 单色 `#0091E2`/conan 灰蓝 `#669ACC` 系（主色已改）/nuget 官方已换曲线双形（BinFlow 经典形保留注记）；另六枚获旁证色（见各行）。

| 文件 | 包型（wire） | brand 主色 | 形态 | 原标记持有方 | 置信度 |
|---|---|---|---|---|---|
| generic | generic | `#0b6bcb`（BinFlow 主题 accent） | 等距立方体（顶面 30% 蓝） | 无（BinFlow 自有） | — |
| docker | docker | `#0db7ed` + `#0f5fa8` | 集装箱堆（2+3）+ 鲸腹 | Docker, Inc.（Moby/Docker 标） | **中高**（2026-08-31 活体：7.84 网格 tile 为**单色 `#0091E2`** 鲸+集装箱堆（10 path）；BinFlow 双蓝描摹可保留，若求同款观感可收敛为 `#0091E2` 单色；方块群位于左上（背）、体型大件右下延伸，与官方 Moby 标「集装箱背左上、头右」一致） |
| maven | maven | `#D22128` | 斜置羽毛 quill | Apache 软件基金会（Apache Feather/Maven） | 中高（2026-08-31 活体旁证：7.84 tile 用渐变羽毛 `#BE202E` 系；BinFlow 色为合理近似） |
| npm | npm | `#CB3837` | 红方 + npm 字 | npm, Inc. | 高（活体旁证：7.84 tile 单 path `#FF553E` 略亮于官方 `#CB3837`——BinFlow 取官方色更准） |
| pypi | pypi | `#306998` + `#FFD43B` | 双蛇互扣（Python 双色） | Python 软件基金会（Python/PyPI） | 中高（活体旁证：7.84 tile 双蛇 `#4B7196`/`#F4BF52`，与 BinFlow 取色同族） |
| go | go | `#00ADD8` | 斜体 GO + 速度线 | Go 团队（Google） | 高（活体旁证：7.84 tile `#00ACD7`，与 BinFlow 几乎同色） |
| nuget | nuget | `#004880` | 倾斜圆角方 + 中心球 | .NET 基金会（NuGet） | **中**（2026-08-31 活体：7.84 tile 已用**现代曲线双形**——8 path，`#016FD2` 蓝 + `#5FFFE6`/`#B3DADD` 青绿点缀，非经典斜方+球；BinFlow 的经典斜方+球识别度仍高、可保留，接线后并排无混淆风险；若要贴新标再开重绘票） |
| cargo | cargo | `#CE422B` | 齿轮（dasharray 齿圈） | Rust 基金会（Cargo/Rust 齿轮） | 中 |
| conan | conan | ~~`#0095D5`~~ → **`#669ACC`** | 棱角 C | JFrog / conan.io 社区 | **中高**（2026-08-31 活体：7.84 tile 为 5 path **多段灰蓝** `#669ACC`/`#7CA8D4`/`#316699` + 线性渐变——即 conan 官方灰蓝色系；BinFlow 原 `#0095D5` 过亮偏 azure，**brand 主色改为 `#669ACC`**；「棱角 C」骨架方向与官方一致；v1.2/K56 已同步 `brand/conan.svg` 换色落地，mono 版依 §1 纪律保持 currentColor、仅注记参照色） |
| helm | helm | `#0F1689` | 舵轮（实心圆毂） | CNCF（Helm） | 中高 |
| helmoci | helmoci | `#0E7490` | 舵轮变体（六边毂，§3） | 同上（衍生形，非官方标） | —（自有变体） |
| rpm | rpm | `#EE2526` | 包裹盒 + 标签带 + 落箱箭头 | Red Hat（RPM 生态） | 中高（活体旁证：7.84 tile `#D72123` + black，同族红） |
| deb | debian | `#D70A53` | 双段内旋螺线（swirl） | SPI / Debian（swirl 标） | 高（swirl 轮廓特征强；活体旁证：7.84 tile 单色 `#A80030` 深绛——BinFlow 取 Debian 品牌粉 `#D70A53` 亦常见，保留） |
| trashcan | —（addon 槽） | `#55606e`（中性 slate） | 回收桶 + 上行恢复箭头 | 无（BinFlow 自有） | — |
| webhook | —（addon 槽） | `#0b6bcb`（BinFlow accent） | 双链环 + 闪电 | 无（BinFlow 自有） | — |

addon 两枚的用色纪律：治理面/能力开关**不占用** success/warning/danger 语义色（那是状态色预算，console-ux §7.1），trashcan 用 `--bf-text-2` 中性、webhook 用品牌 accent。

## 5. 来源与许可姿态（重要）

- **姿态**：对官方标记做**几何极简重绘，仅用于「包型识别」**（在制品仓库 UI 中指示「这是哪类包的仓库」）。这是业界通行形态——JFrog Artifactory（包型选择网格用各家技术标）、Harbor（docker whale）、Verdaccio（npm 标）同例。BinFlow 不复刻官方矢量源文件，全部图形为本票在 24px 网格上的重画。
- **逐枚持有方**见 §4 表。技术商标权利归各自持有方；在自家产品 UI 内以识别为目的的简化使用是行业惯例，但：
  1. **不得**将这些图形用于 BinFlow 自身的品牌位（logo/宣传物料）；
  2. **不得**修改后暗示官方背书；
  3. 若未来商业化分发离线物料（白皮书/展会），低置信三枚（nuget/conan/docker 鲸腹）先做商标复查（**2026-08-31 已活体对照修正**，见 §4；商业化前仍需正式商标复查，活体对照不替代法务核查）。
- Debian swirl 本身有单独的商标政策（Debian Open Use 标许可开放使用）——当前重绘为其几何近似，风险低，登记备查。
- `generic` / `trashcan` / `webhook` / `helmoci` 为 BinFlow 自有原创图形，无第三方权利。

## 6. FE 接线注意（本票不改任何 web/src 代码）

1. 接线票把这 30 枚搬进 `web/src/assets/pkg-icons/`（或做 React 组件封装），消费点：
   - `RepositoryFormPage` 的 `pkg-grid`（brand 版 + 档位门控态用 mono + disabled）；
   - `SetMeUpDialog` 的 `smu-grid`（brand 版，替换 `CLIENT_PKG_META` 里的 `▫ ⬢ ⌬ ⬒ ⬓` 字符图标）;
   - 仓库列表/树/搜索的类型列（mono 版，currentColor 随文字色）；
   - `LicenseAddonsPage` addon 矩阵（trashcan/webhook，brand 版）。
2. ~~**`npm` 与 `go` 含 `<text>`**~~ **已清除（K56/v1.2）**——两枚字标改为 monoline path 勾画：npm 双版同骨架（stroke 1.2 圆帽，小写 n/p/m，p 下延 18.0，包络 4.0~19.4 在方块内净区内）；go 双版同骨架（stroke 2.3 圆帽，G=开口右上的圆 + 3 点位内伸横杠，O=整圆，`skewX(-8)` 给斜体），字形网格已写进各 SVG 注释。30 枚现全部零 text、零字体环境依赖。
3. 门控包型（license 未解锁）用 mono 版 + `opacity: 0.4` + 现有 `pkg-tier-*` 档位徽章组合，不要用 brand 版置灰（品牌色置灰会臟色）。
4. 深色主题：mono 版天然适配（currentColor）；brand 版的官方色在暗底（`#12161d`）下对比度抽查过 docker/npm/pypi 三枚均 ≥3:1（图形件标准），其余枚如发现暗底发闷，允许 +10% 亮度微调并在本 README 登记。
