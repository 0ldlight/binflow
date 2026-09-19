# BinFlow 黑白灰 Monochrome 设计终裁

## 裁决

2026-09-19 用户裁定：**整体配色为黑白**。

Penpot Dashboard UI Kit 继续作为结构事实源：布局、密度、圆角、间距与阴影保持对齐；色彩不再复刻模板。全站使用黑白灰，图表与包型图标也不引入彩色。

## Token 映射

| 语义 | Light | Dark |
|---|---:|---:|
| canvas | `#f5f5f5` | `#0a0a0a` |
| sidebar | `#ffffff` | `#0a0a0a` |
| surface 1 | `#ffffff` | `#171717` |
| surface 2 | `#f5f5f5` | `#1f1f1f` |
| surface 3 | `#ebebeb` | `#262626` |
| border | `#0000001a` | `#ffffff26` |
| text | `#0a0a0a` | `#ffffff` |
| secondary text | `#525252` | `#d4d4d4` |
| interaction | `#171717` / white foreground | `#ffffff` / black foreground |
| focus ring | `#171717` | `#ffffff` |

图表使用 `#171717 → #d4d4d4`（暗色反向）灰阶序列。状态槽全部黑白灰，成功 / 警告 / 危险 / 信息不得只靠颜色区分。

## 包型图标

官方 brand SVG 保留资产来源，但渲染层统一：

`.pkg-svg[data-variant='brand'] { filter: grayscale(1); }`

包型身份由可见文字和图标形状共同表达。

## 硬门

`web/scripts/assert-design.mjs` 校验：

- Penpot 结构 token 未漂移；
- 关键黑白交互 token 在场；
- `color.css` 中所有 hex token 均为灰阶。

该硬门已接入 `lint`、`build`、`build:styleguide`。
