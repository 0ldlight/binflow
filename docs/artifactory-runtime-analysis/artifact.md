# 运行时分析 · 制品浏览与操作面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/tree-general.png` | 树浏览器（General 仓展开态） |
| `tree-empty-repo.png` + `states/empty-repo-tree.png` | 空仓树态 |
| `states/tree-loading.png` | 树 loading 指示（客户端节流下采得） |
| `pc/screenshots/dialogs/tree-context-menu.png` | 右键菜单（Delete Content/Native Browser/Refresh） |
| `pc/screenshots/dialogs/deploy-dialog-open.png` | Deploy 弹窗（选文件后） |
| `pc/screenshots/dialogs/set-me-up-configure.png` | Set Me Up 弹窗 Configure 页签（token 未生成） |
| `pc/screenshots/screens/builds.png` | Builds 页（制品关联面，7.161 在场） |
| `pc/dom-snapshots/walkthrough-repo-row.json` 等 | 树/行级 DOM 探针 |

## 2. 行为规格（正文）

- `docs/reverse/storage-layout.md`——filestore 目录推导、checksum 命名、元数据序列化。
- `docs/reverse/maven-npm-pypi.md` / 各包型规格——上传/解析/下载 wire。
- `docs/reverse/rest-api.md` + matrix D01（制品与存储 43 行）——FileInfo/FolderInfo 四臂、archive、checksum 部署。
- `docs/reverse/frontend/screens.yaml`——artifacts-tree 屏四态（懒加载展开/URL 同步/深链自动展开 = E4）。
- `docs/reverse/console-ui.md` §3.2——树浏览器交互（facet 工具带/右键/深链）。
- Set Me Up 弹窗三页签（Configure/Deploy/Resolve）——console-ui §4-1 + parity D1（50vw 实测）。
- Build-info 域——`docs/reverse/build-info.md`（端点族/数据模型/权限面；UI 侧 builds 屏 7.161 在场）。

## 3. 已知运行时事实

- 虚拟滚动 >2k 节点 lazy-load 未强制（ref-load 纪律——states-gaps partial 项）。
- 树右键菜单项集实测三项（门控项不显现）。

## 4. 缺口声明

制品详情页签全集（7.161 版）无整页截图（依赖逐仓下钻，walkthrough 覆盖 7.84 口径）；依赖视图/Module ID 等 Build-info 关联 UI 无活体证据。
