# Sprint 659 迭代报告 — B6 全清（T-289 `4ef0972`）+ T-288 漏 add fixup（`913c5ba`）+ B7 双票派发

**日期**: 2026-08-26 09:50
**上轮**: Sprint 658

## T-289 关账（修复轮全过）

- **5 blocker 修复全落地**：B1 evictIdle 重写（registry.mu 内只快照→锁外逐会话复查→Abort→no-op delete）+ config 臂锁外 remove；B2 411 删 CL:0 头保 errors[] 信封 + chunked 测试臂；B3 移位前 `>5120 → 400`（2^43/2^44/5121 边界用例）；B4 裸 status 过 uploadsWriteGate 静默过滤 + 越权测试；B5 s3Session.Abort/failLocked 补 best-effort AbortMultipartUpload + 探针升级 `mc ls --incomplete` 三段断言（错 sha/abort 腿零残留，end-of-run 仅 kill -9 孤儿 1）
- **3 条顺手清**：hex 大写接受归一（对齐引擎）/snapshot 注释/failed 态文案
- **conductor 复验**：五处修复点代码抽查 + `go build` ✅ / `make lint` 0 / B5 storage 双测试 + TestUploads 全绿 / `make test-m10-invariant` 双腿 0 deviations / `make test-m7-rbac-matrix EXPECT=1` 0 deviations / **HEAD-build stash 验证**（提交态独立编译 + httpapi/storage/cmd 三包绿）
- 提交 `4ef0972`（13 文件点名 add）→ 双远端推送

## T-288 漏 add fixup（第三例）

- 发现：`93f2e3b` 提交了 main.tsx 路由（`import('./pages/admin/LicenseAddonsPage')`）等 12 文件，但**页面本体三新文件从未 add**（LicenseAddonsPage.tsx 394 行/addons.ts 127 行/license.css 27 行）——干净检出 vite 构建必红（之前 HEAD 验证只跑了 Go 面，go:embed 用预构建 dist 所以 go build 不红）
- Fixup `913c5ba`：三文件提交 + **干净 HEAD npm run build 亲验绿**（vite 1.55s + relink-assets 零残留）
- 流程追加（BOARD 留痕）：含新 FE 文件的提交，HEAD 侧 `npm run build` 一并验
- 副作用说明：docs-site 本地构建产物改动随 stash 流程丢弃（可再生生成物，CD 链在 VM 重建，无源码损失）

## B7 双票已派（09:50）

- **T-291 Properties Tab FE**（dev-frontend）：**首个 MUI 票**——引入 @mui/material + 主题桥接（palette 对齐 base.css token），交互按 Artifactory（key/value 表格/行内增删/PUT 替换语义/权限门控/四态）；后端 T-286 四动词已就绪；anchor-audit ledger 必跑；真实联调四态含 403 面
- **T-292 规格批次二**（reverse-engineer）：rpm.md（repomd/createrepo_c/NEVRA/zchunk）+ helm.md（经典 chart 仓 index.yaml + HelmOCI 与 docker v2 复用面）——T-284 同款结构，官方规范锚点优先

## 状态

M10：**13/21 done**（B0–B6 全清 + T-284 补跑）。在途 ×2（B7）。剩余：T-291/T-292 → B8（T-293 as-built 回写 ∥ T-294 Cargo 条件票——规格依赖已解除）→ B9（T-295 ∥ T-296）→ B10（T-297 终验）。

## 台账汇总（M11 管道持续累积）

S3 MPU kill -9 续传（AC2 descope）；unused-cleanup 引擎；enableTokenAuthentication/contentSynchronisation 真实现；socketTimeout/metadataRetrieval 溢出上界 + legacy secs 退役；T-289 评审 6 条 non-blocking；T-284 8 点 tech-lead 裁决；存量页面 MUI 化分批票。
