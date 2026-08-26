# Sprint 678 迭代报告 — T-294 关账（`6069845`）B8 全清；B9 双票派发；两条用户指令落板

**日期**: 2026-08-26 11:37
**上轮**: Sprint 677

## T-294 关账链

- 修复轮全落地：B1 引用计数 per-(repo,crate) 互斥集（acquire-before-block 保单实例、release-after-unlock 保无同键双活——conductor 代码抽查过）+ **并发回归两腿**（8 goroutine×3 轮断言行数==版本数；yank 混合确定性终态）；M1 畸形形态拒绝不降匿名（表加两行）；M2 注释修正
- conductor 门：build/lint 0/并发腿 PASS/双包 -race 0/双矩阵 0 偏差/HEAD-build 验证（build+vet+双包）
- 提交 `6069845`（21+ 文件点名）→ 双远端推送 → **B8 全清**
- **M10 第三个包型 addon 全链贯通**：go / nuget / cargo

## 用户指令两条（本轮落板）

1. **MUI 指令重申**（11:22）：存量页面迁移升格显式票组 T-299 [P1] 批次一 / T-300 [P2] 批次二，T-297 后立即开、M11 首批；硬验收 = 交互零变化 + ledger PASS + 全量 playwright 绿
2. **新指令**（11:35）：「oauth2和ldap等认证配置要放到前端页面可配置，诸如此类的配置要和artifactory的行为严格保持一致」——M11 立项：BE（auth 配置面 REST + 变更即生效）+ FE（admin 认证配置页组，MUI + Artifactory 交互）；行为基准 docs/reverse/auth-integration.md（拆票前复核）；收敛进既有认证多臂链

## B9 双票派发（11:36）

- **T-295**（release-engineer）：charts configmap 显式枚举 M10 配置面（addons.disabled 等）+ compose/systemd 同步 + helm 渲染与启动烟测——T-183 教训防线
- **T-296**（tech-writer）：文档五项（license 指南/属性用法/Go/NuGet/Cargo 接入）+ FAQ 三目 + cargo.md R-1 修订（D-1 落册）+ docs-site 构建验证

## 状态

**M10：17/21 done**（B0–B8 全清）。在途 ×2（B9）。剩余：T-295/T-296 → T-297 终验（L01~L30 + DoD 八条 + PRD v1.1 转正）→ tag m10-done。

## M11 管道（累积）

S3 MPU kill -9 续传；unused-cleanup 引擎；enableTokenAuthentication/contentSynchronisation；超时上界 + legacy secs 退役；cargo remote/virtual 单票（dep T-294）；T-294 M3~M9；**认证配置前端化（新指令）**；**存量页面 MUI 化 T-299/T-300（新指令）**；Terraform/GitLFS 规格余量；huggingface 等剩余包型。
