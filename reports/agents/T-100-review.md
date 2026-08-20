# 评审报告 T-100（视角: consistency + 前端正确性）

- 结论: **REQUEST_CHANGES**
- 评审对象: commit `c00130a` 的 T-100 部分（`web/src/pages/repositories/tree/{lib,sha256,TreePage,UploadDialog,NodeDetail,tree.css}`、`web/src/pages/search/SearchPage.tsx`、`web/e2e/artifacts.spec.ts`、`main.tsx` 树/搜索路由、AppShell 搜索导航、auth-shell 占位断言收口）
- 评审人验证（全部实际执行）：
  - `cd web && npx tsc --noEmit` → exit 0（全树）
  - `go test -count=1 ./internal/console/` → ok 0.695s
  - sha256.ts 零依赖实现独立对账：编译后与 node:crypto 在 25 个边界尺寸（0/55/56/63/64/65/127/128/4MB±1 等）× 三入口（不规则增量 update / blobSha256 分片 / streamSha256 流）全一致
  - 斜杠契约逐项比对 `internal/httpapi/storage.go`、`internal/adapter/generic/handler.go`、`internal/repo/service.go`（Delete 的 isFolderNode 分支、Put 的 folder-deploy 分支、validateRelPath 与 FE validateNameSegment 同口径）

## 契约核对结论（重点审查区 1/4）

| FE 行为 | 后端事实 | 结论 |
|---|---|---|
| mkdir/目录 DELETE 带尾斜杠（`contentURL` 显式保留） | `service.Delete` 的 `isFolderNode(path)` 分支键在尾斜杠（service.go:866 起，无斜杠走文件臂 `nodes.Get` → 404）；`handlePut` 尾斜杠走 folder-deploy（body 必须为空——FE fetch 无 body ✓） | 一致 |
| 元数据面 children/item-info/list 不带斜杠 | `storageNode` 先试原拼写、再试补斜杠（storage.go:249-261）——显式 folder 行两种写法均可；隐式目录双写法 404 → FE 走搜索面回退 | 一致 |
| 隐式目录回退：`name=<dir>/` 子串 + startsWith 收窄 + 首段投影 | 索引排除 folder 行（空 mkdir 目录不可见）→ mkdir 逐段材料化祖先使主路径可达；回退对子串误命中（`ba/` 对 `a/`）有 startsWith 防线 | 逻辑成立 |
| mkdir 祖先段失败不致命 | folder PUT 逐段独立授权（`allow(write)` 每段）+ governance pattern 门在 Put 前置——窄授权 403 可忍的取舍与登记一致 | 一致 |
| E-14 404 幂等呈现（toast「此前已不存在」） | service 两个臂均 wrap ErrNodeNotFound | 一致 |
| `?permissions` 仅 local 且 admin 门 | handleStoragePermissions：admin 门 + 非 local 400；FE 非 admin 收敛（§3.6.3）、virtual/remote 隐藏 | 一致 |

build 产物/deps：c00130a 未触碰 package.json/lockfile（deps 零变更属实）；Tree/Search 均为独立懒加载 chunk；§10.3 预定锚全落（增补锚已在工作日志登记为 ux v1.2 素材）。clean-room：FE 为原创 React/TS 实现，与 reverse-src（Java 反编译服务端）无逐行对应嫌疑。

## 必须修改（blocking）

1. **`web/src/pages/repositories/tree/UploadDialog.tsx:107-157 / 191-195` — 关闭对话框不会停止上传队列，剩余文件在无 UI 反馈下继续落库**。
   `close()` 只 abort 当下在飞的 XHR；泵循环 `for(;;)` 在 abort 异常被 catch 标记该行 error 后**继续 find 下一行 hashing/queued 并发起新 PUT**。复现：拖入 3 个文件 → 第 1 个上传中点「关闭」→ 文件 2/3 仍会被完整上传（`onUploaded` 还会触发父页 refresh）。这既违背同文件头注释「关闭对话框 abort 全部在飞 XHR」（UploadDialog.tsx:19），也违背用户取消心智——对配额仓（413 语义已实现）会真实烧配额，且失败行无人可见（组件已卸载）。
   建议改法：加 `const closedRef = useRef(false)`；`close()` 置 `closedRef.current = true` 后再 abort；泵循环每轮开头 `if (closedRef.current) break`，行可标记 `error: ApiError(0,'已取消')` 或新增 cancelled 相位。顺带在 PUT settle 后 `xhrs.current.delete(xhr)`（当前集合只增不减）。

## 建议改进（non-blocking）

1. `tree/lib.ts:112-128` — 隐式目录回退吞掉了非根目录的 404 语义：`searchListing` 空结果（手输 URL 笔误 / 深链指向已删路径）会渲染「此目录为空 + 上传第一个制品」而非既有的「路径不存在」态（TreePage.tsx:452 的 404 分支对非根目录实际不可达）。隐式目录按定义必有后代，空结果 ≈ 不存在。建议：`searchListing` 返回空时 rethrow 原始 404 ApiError（代价：对「整子树无 read ACL」的罕见情形会显示 404 而非空态——父层列表本就隐藏该行，可接受）。
2. `tree/lib.ts:335-349` — `downloadArtifact` 先 `await streamSha256(hashLeg)` 再消费 blob 腿：tee 在哈希期间为未读腿缓冲**整个响应体**，注释「哈希内存上界 = 单块」对函数整体不成立（峰值 ≈ 全文件）。建议先 `const blobP = new Response(blobLeg).blob()` 再 `await Promise.all`（两腿并行，blob 可增量进 Chromium blob 存储）。RSS 正式口径在 T-104/T-105，不阻本票。
3. 上传 403（write 拒绝）分支无 E2E 覆盖：W12d E2E（artifacts.spec.ts:143）只盖 delete-403；AC② 的「403 权限指引」上传腿（UploadDialog.tsx:434-446）未被探针触达。建议 T-104 用 ro 用户在树页点上传补一条（渲染管线与 409/413 同构，风险低）。
4. AC① 的树表 createdBy 列契约上不可得（folderChild 与 ?list 均无——工作日志漂移 3 已登记，FE 转详情面板呈现）。需 conductor 把 AC/spec 口径回写为「详情面板呈现」，避免 T-104 按 AC 字面断言树表列时误判。
5. console-ux §10.3 预定锚 `search-filter-{repo|package|type}` vs 实现仅 `search-filter-repo`（AC③ 本就只要求 repo 过滤）——ux v1.2 回写时把 package/type 两锚标注为未实现/删除。
6. UploadDialog maven 模式：拖拽被静默忽略（onDrop 仅 generic 分支，UploadDialog.tsx:184），无「请点选」提示；小 UX 项。
7. `TreePage.tsx:160-166` — `?focus=` 未命中（ACL/已删/路径错位）时静默不选中、无反馈；可给一次性 toast 或空态提示。
8. `TreePage.tsx:64-68` — `useParams()` 调用两次取 `key` 与 `*`，风格项，可合并为一次解构。

## e2e 证明力评估（重点审查区 5）

6 例对 W12/W12b/W13/W12d/W12a(UI)/W14b/大目录覆盖良好：sha 对账为双源断言（node:crypto 直算 == 服务端 checksums.sha256，加 UI「✓ 下载落盘一致」徽标——artifacts.spec.ts:105/111），强度足够；maven 预检零写请求用 request 监听断言（:243-257，FR-24-AC5）；E-14 幂等以内容面重复 DELETE 404 为源（:135）。缺口即上述 non-blocking 3（上传 403 腿）；W12c 五协议特化 P1 与 virtual 来源列未做已按 AC P1 标注/遗留 1 登记，不阻。

## 结论

斜杠契约、隐式目录回退、流式哈希、错误原样呈现、四态矩阵、构建纪律均过关，契约漂移登记诚实且与代码一致。唯一 blocker 是上传队列取消语义缺陷（一行旗标可修），修掉即可转 APPROVE；建议 conductor 将修复并入本票或派 T-104 前置小票。
