# K56 — 品牌资产生产化（M14 前置：候选 1 转正 + T-381 L02 conan 换色）

| 项 | 值 |
|---|---|
| 票据 | K56（M14 前置资产小票） + T-381 L02 |
| 角色 | ux-designer |
| 日期 | 2026-08-31 |
| 状态 | done（SVG path 化全交付；PNG 渲染验证留执行环境，见遗留） |
| 约束遵守 | 未动 `web/`（T-384 在途）、未动 console-* 规格册、零 git 操作 |

## 交付

1. **候选 1 生产件 5 枚**（`docs/design/brand/logo/candidate-1/`）：
   `mark.svg` / `mark-dark.svg` / `mark-mono.svg` / `lockup-horizontal.svg`（674×128）/ `lockup-dark.svg`，
   另附 `README.md`（产物清单、字形网格、favicon 派生管线、验证命令）。全部零 `<text>`、零字体依赖。
2. **wordmark 取径 = 手工勾画 monoline**（非 Inter 转曲）：cap 100 / 笔宽 16 / 圆帽 / 字距 26，
   Bin=结构色、Flow=主色分组；逐字坐标表在 candidate-1/README §2，FE 可复核可重画。
   理由：会话无字体工具链 + 触发 logo/README §5「手工勾画规避」分支（接线票免放 INTER-LICENSE）。
3. **图标转 path / 换色**：npm 双版（小写 n/p/m monoline，stroke 1.2）、go 双版（G+O monoline
   stroke 2.3 + skewX(-8) 斜体）——`<text>` 全清；conan brand 版 `#0095D5` → `#669ACC`
   （T-381 L02），mono 版按 README §1 纪律保持 currentColor、注记同步。
4. **README 升版**：logo/README v1.2（§6 生产化 + 状态行）、package-icons/README v1.2
   （§6.2 清障 + §4 conan 落地注记 + 状态行）。
5. **顺手微修（范围外、同性质隐患）**：brand/webhook、brand/trashcan、brand/generic 三枚
   注释内 `--bf-*` 写法违反 XML 注释规则（禁 `--` 序列，严格解析器拒载整文件），
   改为无连字符 token 名；语义零变动。

## 验证

- **静态复核（本会话无 shell）**：ripgrep 全目录扫描——
  `package-icons/`（30 枚）与 `candidate-1/`（6 件）`<text>` 命中 0、
  注释非法 `--` 序列命中 0；`brand/conan.svg` stroke `#669ACC` 落地确认。
  `<text>` 残留仅三张 UX-1 候选规格表（存档、不接线，已在 README §6 警示）。
- **渲染验证命令（待有 shell 者执行，命令在 candidate-1/README §3/§4）**：
  `xmllint --noout` + `rsvg-convert`（或 `npx -y svgexport`）+ `magick` 合成 .ico。
  预期：全部 noout 通过；16px 档两箭可辨（logo/README §1 已核样张）。

## 遗留

1. PNG（favicon 16/32/48、PWA 180/512）与 .ico 未生成——K56 会话无渲染工具，
   命令已写死在 candidate-1/README §3，T-389/接线票直接跑。
2. 渲染抽查（headless chromium/rsvg）未执行，同上原因；建议 T-389 接线时目检
   wordmark 字形（monoline 手工勾画首版，若某字光学不平衡，按 §2 网格微调坐标即可）。
3. 三张候选规格表（candidate-1/2/3-*.svg）仍含 `<text>` 与非法注释序列——存档件不接线，
   若要入库 xmllint 全仓检查需先清理（建议单独 chore 票）。
4. nuget 现代曲线双形重绘（README §4 已注记「若要贴新标再开重绘票」）——未列入本票。
