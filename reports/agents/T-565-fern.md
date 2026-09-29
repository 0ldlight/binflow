# T-565 · fern 文档站与 OpenAPI 规范 DELETE 仓语义清尾（第 ③ 块）

```
Ticket:       T-565 / Linear BIN-47 第 ③ 块——fern 文档站 + OpenAPI 规范中 DELETE 仓语义陈旧面清尾（T-555 语义变更的文档消费面）
Role:         tech-writer
Area:         fern/、tools/openapi-spec/（本票 area 纪律：不碰 web/、Go 代码、docs/design、docs/user）
Input:        conductor 派发（语义事实=commit 6812f271 已合 main）；权威源=internal/httpapi/repositories.go handleRepoDelete（825-864 行，实测措辞/计数/旗忽略）、
              reports/agents/T-555.md（活体探针矩阵 7.161.26：count=文件+folder 全量、根不计；virtual=plain 措辞；旗=噪声）、
              docs/compatibility/matrix.yaml D02-R05（state=compatible/confidence=high，L034 Arm 1 双轮 PASS×2，契约 rest/repo-delete-nonempty-cascade VERIFIED）
Changes:      ①d_search.py repoDelete op 重写——description 由「canonical path 带 ?deleteContent=」改为静默级联口径（恒 200 + JSON 报告体 {repoKey,statusMsg,deletedArtifactsCount,success}，
              计数口径、rclass 措辞二分、旗冗余同形、404 仍在）；deleteContent 参数保留、描述改「兼容保留/冗余」；200 响应补 RepoDeleteReport schema+example；补 404 envelope 响应；
              ②helpers.py 新增 RepoDeleteReport 组件 schema（四字段 required，additionalProperties=false，字段级 description 带措辞与计数规则）；
              ③fern/pages/admin/repositories.mdx:110「整仓清空：删仓带 ?deleteContent=true」→ 级联口径（删仓即级联、旗冗余、保留仓配置只清内容=逐路径 DELETE+重建）；
              ④fern/pages/admin/operations.mdx:104 不捕获面去「?deleteContent」拼写，明示「级联移除的全部内容一律不进回收站」（trashcan 不捕获仓拆除仍真，见 Evidence）；
              ⑤fern/pages/admin/console.mdx:74 控制台删除确认描述——按同票 UI 块在制变更（RepoDeleteConfirm.tsx 未提交 diff：撤「同时删除内容」勾选与 400 重试臂，改输入 key 确认+成功反馈携带计数）同步改写
Files:        修改：tools/openapi-spec/d_search.py、tools/openapi-spec/helpers.py、fern/openapi/binflow.json（make spec 再生，非手改，已 git add 待 conductor 收编）、
              fern/pages/admin/repositories.mdx、fern/pages/admin/operations.mdx、fern/pages/admin/console.mdx；新增：reports/agents/T-565-fern.md（本文件）
Tests:        门禁（全跑，真实输出）——make spec：binflow.json 112 path items/158 operations/20 tags/55 schemas/326968 bytes，self-check: clean；
              make spec-check：PASS「fern/openapi/binflow.json in sync with tools/openapi-spec/」（首轮按门禁提示 git add 生成产物后过——门禁比对 index，artifact 暂存属其文档化流程，未 commit）；
              make fern-en-ratchet：PASS「en pages: current=10 baseline=10」（en 树仅 install/getting-started 十页，无 admin en 页，本改动零波及）；
              残余面 grep（deleteContent/非空仓 400/同时删除内容 全 fern/）=仅剩新口径行自指，无陈旧残留；
              端点行为未在本地实例复跑（curl 面）——语义证据链=T-555 自测（httpapi 全量 ok 181.7s + TestRepositoryDeleteCascade 5 腿全 PASS）+ L034 差分双轮 PASS×2，按契约引用核对而非重跑
Commands:     make -C <worktree> spec；make -C <worktree> spec-check；make -C <worktree> fern-en-ratchet；
              git add fern/openapi/binflow.json；grep -rn "deleteContent|同时删除内容|非空" fern/ tools/openapi-spec/（清尾核查）；
              git diff web/src/pages/repositories/RepoDeleteConfirm.tsx（只读核对 UI 块在制方向，未改 web/）
Outputs:      fern/openapi/binflow.json（repoDelete 描述/参数/200+404 响应 + RepoDeleteReport schema）、
              fern/pages/admin/repositories.mdx、fern/pages/admin/operations.mdx、fern/pages/admin/console.mdx
Compatibility: 口径源逐条核对 matrix.yaml D02-R05（capability=DELETE /api/repositories/{key}，state=compatible，evidence=L034 Arm 1）：级联✓、报告体四字段✓、
              计数=文件+folder 全量根不计✓、旗冗余同形✓、virtual plain 措辞✓、404 保留✓——spec 与三页 mdx 均按此口径，无美化无隐瞒；
              相邻账 d08/repo-delete-response-envelope 成功体半边已随 T-555 resolved（matrix note 佐证），spec 的 200 JSON body 描述与之一致
Security:     新增文案零密钥/零内网地址/零凭据；spec example 用占位仓 key "libs-release" 与既有页惯例一致；报告体只含计数与措辞，不含主体敏感面
Evidence:     internal/httpapi/repositories.go:825-864（handleRepoDelete + repoDeleteStatusMsg：virtual/local-remote 措辞原文）；internal/httpapi/repo_batch_write.go:391-397（JSON tag 逐字段核对 repoKey/statusMsg/deletedArtifactsCount/success）；
              reports/agents/T-555.md 探针矩阵（s0-s5/w-virt/w-rem）；docs/compatibility/matrix.yaml:938-953（D02-R05 行）；reports/compatibility/L034-r6.md Arm 1（经 matrix last_difftest 引用）；
              console.mdx 同步依据=同票 UI 块未提交 diff（RepoDeleteConfirm.tsx：删 deleteContent 勾选与 400 重试，成功反馈携带 deletedArtifactsCount）
Status:       done（③ 块范围内）
Followups:    ①docs/user 镜像面未在本块（area 排他）——api-reference.md:347（"optional ?deleteContent=true"）、admin/remote-virtual.md:165-167、admin/trash-can.md:37 仍陈旧，待 T-565 ①②块或后续票收编；
              ②helpers.py 头注「contract source #1 = docs/user/api-reference.md verbatim」在 ①块落齐前暂与该页 347 行失谐（spec 已按服务端实际行为+matrix 主账先行，属主账优先）；
              ③console.mdx:74 系按在制 UI diff 同步——UI 块落地后 UAT 复核一次最终文案（若对话框措辞再变则联动）；
              ④en 树无 admin 页，若后续 en 扩面到 admin，本三页需双语同步
Lessons:      spec-check 门禁比对的是 git index 而非 HEAD——「不 commit」约束下的过闸姿势=make spec 后 git add 生成产物（门禁自身 fix 提示即此流程），收编者随票一并提交；
              文档消费面清尾除派发点名的三处外，console.mdx 的 UI 确认描述也是同语义面——全树 grep「同时删除内容/非空仓」比只搜 deleteContent 多抓到一处
```
