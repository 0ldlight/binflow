# BinFlow 文档站（docs-site/）

Docusaurus 3 站点骨架（ADR-0011 + T-130 K1 终裁）。**本目录是构建管线，不是内容区**：
内容唯一来源是 `docs/user/*.md`（tech-writer 只写那里，frontmatter 仅用
`title` / `description` / `sidebar_position` 兼容子集——ADR-0011 工作流细则）。

## 工作方式

```
docs/user/*.md  ──(make docs: docusaurus build)──►  docs-site/build/
                                                        │  原样复制（零后处理，增补②）
                                                        ▼
                                              internal/docs/dist/  ──go:embed──►  binflow-server
                                                                                        │
                                                                        GET /binflow/docs/** （匿名只读）
```

- `make docs`：构建 → 复制进 embed 目录 → 零 CDN 检查 → 体积报告（gzip 预算
  15MB，PRD FR-41-AC4/G21）。超限只 WARN——fallback（dist 置占位、docs-static
  tar 升格唯一渠道）的触发与解除均须用户确认（ADR-0011 增补④），绝不自动。
- `make docs-static`：同一 build 产物原样打包 `docs-static_<VER>.tar.gz`（tar 根
  即站点根、无嵌套、不二次构建——增补⑤），自托管可选交付形态。
- `make build` 不依赖 node：`internal/docs/dist/placeholder.html` 已提交，占位
  策略与 console（T-89）同构。

## 关键配置事实

| 项 | 值 | 依据 |
|---|---|---|
| baseUrl | `/binflow/docs/` | 挂载段；资产由 baseUrl 原生前缀化，**无 relink 步骤**（console 的 relink-assets 在此无对应物，增补②） |
| locale | `zh` 唯一（骨架就位） | PRD FR-41；en 为 M6+ 债 |
| 搜索 | `@easyops-cn/docusaurus-search-local` + `nodejieba`（zh 分词） | 增补①：内建搜索只索引元数据、正文关键词结构性不可达 |
| 版本化 | `versioned_docs/version-v1.x/`（首个版本目录）；最新版本服务在段根（路径无版本段），docs/user 在线内容走 `/next/` | 增补③；发版时重跑 `npm run version v1.x` 刷新快照 |
| 断链策略 | `onBrokenLinks: 'warn'` | docs/user/README.md 导航指向 T-141/T-142 待补篇目；内容矩阵齐后收紧为 `'throw'` |

## nodejieba（构建期原生依赖）

zh 分词用原生 addon，**只存在于本目录的 node 构建链**——不进 Go 二进制、不进
运行时镜像（ADR-0005 构建链/运行时隔离，增补①）。无预编译二进制的环境需要
C++ 工具链（macOS：Xcode CLT；Linux：gcc/make/python3；Docker node 阶段可
`apt-get install build-essential python3`）。

## 侧栏（五类信息架构）

`sidebars.js` 手写分组（客户端接入 / 管理指南 / FAQ 已就位；安装指南 7 形态与
API 参考的插槽已注释标注，随 T-141/T-142 落地）。篇目顺序由各篇 frontmatter
`sidebar_position` 决定，与手写分组不冲突。

## 常用命令（统一走 Makefile）

```sh
make docs            # 全链：build + embed 复制 + 零 CDN 检查 + 体积报告
make docs-static     # 可选交付：docs-static_<VER>.tar.gz（含 sha256 输出）
cd docs-site && npm run start    # 本地写作预览（不动 embed，需重新 make docs 才进二进制）
```
