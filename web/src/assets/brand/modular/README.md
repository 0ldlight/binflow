# BinFlow 网站 Logo 套件

采用用户确认的三块几何平面「模块仓」标志，按参考图重新绘制 SVG。字标为 BinFlow，B 和 F 大写。

- horizontal：网站导航横版，SVG 与 1680px 透明 PNG。
- stacked：纵版，SVG 与 1280px 透明 PNG。
- symbol：独立标志，SVG 与 1024px 透明 PNG。
- primary：白底使用；dark：深色背景使用；black / white：单色版。
- favicon.svg / favicon.ico：浏览器图标。
- apple-touch-icon.png：180px 苹果收藏图标。
- android-chrome-*：192px / 512px 应用图标。
- icon-*：16–512px 图标。
- safari-pinned-tab.svg：Safari 单色固定标签图标。
- site.webmanifest：示例 Web App Manifest；部署时按应用路径调整。

主色 #18181B，深色背景使用 #FAFAFA，另提供纯黑和纯白版本。SVG 字标使用 Arial Bold / Helvetica / sans-serif 字体回退，未转曲；需要精确一致显示时使用 PNG。图标 SVG 无字体依赖。

将本目录资源部署到 /brand/，在 HTML head 中添加：

```html
<link rel="icon" href="/brand/favicon.ico" sizes="any">
<link rel="icon" href="/brand/favicon.svg" type="image/svg+xml">
<link rel="apple-touch-icon" href="/brand/apple-touch-icon.png">
<link rel="mask-icon" href="/brand/safari-pinned-tab.svg" color="#18181B">
<link rel="manifest" href="/brand/site.webmanifest">
<meta name="theme-color" content="#18181B">
```

导航栏示例：

```html
<img src="/brand/binflow-horizontal-primary.svg" alt="BinFlow" width="158" height="48">
```

已验证 PNG 可解码，favicon 包含 16/32/48px，已目视检查横版与深浅色版本。建议横版显示宽度不小于 132px，独立图标不小于 24px；16px favicon 使用专用导出文件。
