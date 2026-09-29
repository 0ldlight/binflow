# T-565（Linear BIN-47）第①块 — 控制台 DELETE 仓交互面 + apiText 消费翻新

```
Ticket:       T-565 / BIN-47 ① — web 控制台 DELETE 仓交互面文案 + apiText 消费翻新（T-555 语义变更的消费面清尾）P1
Role:         dev-frontend（area：web/src 仓库删除流 + web/e2e 对应 spec）
Area:         web/src/lib/repos.ts、web/src/pages/repositories/RepoDeleteConfirm.tsx、web/src/pages/repos/RepoDetailPage.tsx、web/src/i18n/（repositories 域）、web/src/styles/pages.css；web/e2e/{repositories,t104-supplements,m8/repositories-admin}.spec.ts
Input:        派单语义事实（T-555 commit 6812f271）：DELETE /api/repositories/{key} 恒 200 JSON 报告体 {repoKey,statusMsg,deletedArtifactsCount,success}（count=文件+folder 全量、根不计；statusMsg 按 rclass content/plain 两措辞）；?deleteContent=true 接受且冗余；非空仓 400 门不存在；ErrRepoNotFound 404 仍在。对照 internal/httpapi/repositories.go handleRepoDelete（829-853 行）+ repo_batch_delete.go repoBatchDeleteReport（386-398 行）实测 wire 形。锚册 docs/design/console-artifactory-parity.md L119/L388（V8 删仓确认形态——参照 7.x 单击确认无 checkbox，BinFlow type-the-key 为 E1 豁免的更严偏离）
Changes:      1) lib：deleteRepo 弃 apiText+deleteContent 旗 → apiJSON 消费报告体，返回类型 RepoDeleteReport（repoKey/statusMsg/deletedArtifactsCount/success）；
              2) RepoDeleteConfirm：两段流退役——删除「同时删除内容（deleteContent）」复选框（服务端级联恒删，保留复选=诱导用户以为可不删内容，误导面比多一步确认更危险）、HTTP 400 原因带回块（repo-delete-reason 锚）、400+deleteContent 重试分支（死代码）；破坏性警示保留（「将永久删除仓库…及其全部制品…没有撤销」+ 输入 key P5 强确认原样）；成功 toast = 服务端 statusMsg + count>0 时追加「（已删除 N 项内容）」；404/403 等错误仍走通用「删除失败」toast（errText 统一入口，无 400 特判残留）；
              3) RepoDetailPage 危险区：「删除仓库及其（可选）全部内容」→「删除仓库及其全部内容」（「可选」是旧旗语义残留）；
              4) i18n（repositories 域 en+zh 双侧同构）：删 2 键（同时删除内容（deleteContent）——非空仓必须勾选 / HTTP 400：）、改 1 键（危险区句）、增 1 键（（已删除 {n} 项内容））；
              5) pages.css：删除孤儿样式 .modal .server-reason（唯一消费方随两段流退役；.check-row 保留——ReplicationsSection 仍用）；
              6) e2e 三 spec 对齐（逐条理由见 Tests）
Files:        修改：web/src/lib/repos.ts、web/src/pages/repositories/RepoDeleteConfirm.tsx、web/src/pages/repos/RepoDetailPage.tsx、web/src/i18n/locales/en/repositories.ts、web/src/i18n/manifests/zh/repositories.json、web/src/styles/pages.css、web/e2e/repositories.spec.ts、web/e2e/t104-supplements.spec.ts、web/e2e/m8/repositories-admin.spec.ts。新增/删除：无（净 -15 行）
Tests:        e2e 改动逐条理由（只动确因 T-555 语义错坏的腿）：
              - repositories.spec.ts「local generic full lifecycle」：两段流腿（首确认期 400 reason「holds 2 node(s)」+ 二次确认）在新服务端恒 200 下首确认即删仓、对话框消失、fill 二次确认键必超时——改单次确认级联断言；toast 'deleted successfully' → 'removed successfully'（服务端新措辞实测）+ 增「（已删除 2 项内容）」计数断言（1 文件+1 祖先目录，口径同旧「holds 2 node(s)」）；标题「delete with content」→「cascade delete」（旓名含旗语义）
              - repositories.spec.ts「remote maven … empty delete」：仅 toast 措辞随服务端改 + 注释（count=0 不带计数后缀）；流本身（fill key→accept）未动
              - m8/repositories-admin.spec.ts「row-level delete strong-confirm」：同两段流腿错坏（期 400 reason + 预勾选断言 + 二次确认）——改单次级联 + 计数断言；标题/文件头注释 deleteContent 两段流 → 级联一次成型
              - t104-supplements.spec.ts「W12c + W11」：两段流腿（首确认期 reason「node(s)」+「仓仍在」对账 + check 复选框 + 二次确认）同因错坏——改单次确认级联；标题 two-stage → cascade delete
              - 未动：全仓 26 处 `?deleteContent=true` 清理调用（旗被接受且冗余、恒 200，腿不红——README §3 收尾形态不改）；m15/t416（只断言 status 200，仍绿）
Tests状态:    四态覆盖：默认态（弹窗渲染/强确认门）与成功态（级联 toast+计数+行消失+内容 404）有腿；错误态 404 保留（curl 实证 HTTP 404）；加载态「删除中…」文案路径未变
Commands:     npm --prefix web run typecheck；npm --prefix web run lint；npm --prefix web run build；go build -o /tmp/binflow-server-t565 ./cmd/binflow-server（console dist 先按 make console 同款接线：rm -rf internal/console/dist/assets && cp -R web/dist/. internal/console/dist/，生成物 gitignored）；scratch 起服 /tmp/binflow-server-t565 serve -c /tmp/binflow-t565.yaml（listen 127.0.0.1:18082，data_dir=/tmp，绝不触 8080 既有实例与主 checkout ./data）；curl 冒烟（PUT 仓→PUT release/app.bin→DELETE→404 臂）；BASE=http://127.0.0.1:18082 node scripts/seed-m8.mjs；BASE=… npx playwright test e2e/repositories.spec.ts；… e2e/m8/repositories-admin.spec.ts；… e2e/t104-supplements.spec.ts；… --workers=1 三 spec serial 重跑；收尾 lsof -ti :18082 | xargs kill + 端口复核释放 + /tmp 数据清理
Outputs:      typecheck 干净；lint 0 errors（45 warnings 全为存量 react-hooks 告警，无一本票文件新增）；assert-i18n OK（3263 调用点/2420 键，en 与 zh 同构）；build ✓ built in 8.59s + relink-assets/wire-brand-assets/assert-built-brand-copy 全 OK。curl 冒烟：DELETE 报告体 {"repoKey":"t565-smoke","statusMsg":"Repository 't565-smoke' and all its content have been removed successfully.","deletedArtifactsCount":2,"success":true} HTTP 200；未知仓 HTTP 404。e2e 首轮：repositories 5/5、m8/repositories-admin 5/5、t104-supplements 4 过 2 skip；serial 重跑（--workers=1）：14 passed + 2 skipped（t104 dind 播种腿 + 其依赖的 W12c/W11 腿——本机 docker remote-only 策略，dind 无法回连 127.0.0.1:18082，spec 自带 degraded skip 守卫如实记录；该腿新形态与两处已绿级联腿逐行同构：单确认→removed successfully→内容 404）
Compatibility: 契约漂移：无（实现严格对齐 T-555 实测 wire 形）。锚册对照：docs/design/console-artifactory-parity.md L119 现文「非空仓 deleteContent 复选 + 影响 N 制品摘要」已过时（本票 UI 退役复选框后更贴近参照 V8 形态：参照本就无 checkbox、单击确认；BinFlow type-the-key 严确认是 L120 已登记的 E1 豁免）——锚册行级建议见 Followups
Security:     无新增 XSS 面（toast 渲染服务端 statusMsg 走 React 文本节点；key 走 mono <b> 文本节点）；无凭据/敏感信息落前端日志；破坏性操作的防误删强度未降级（type-the-key + danger 红边保留，且移除了「不勾复选以为能保内容」的误导面）
Performance:  无影响（删一条请求路径的旗参数；无新请求/渲染面）
Evidence:     关键输出已贴 Outputs（四门命令原文见 Commands，全部实际执行）；e2e 两轮（首轮+serial）均在 /tmp scratch 实例（端口 18082）真栈跑通
Risks:        ① t104 W12c/W11 腿本机只能 skip（dind 环境缺失），其 docker 级联删除形态未在本机活体验证——需 dind 可达的环境（CI/QA VM）复跑一次；② 锚册 L119 陈旧未回写（docs 禁改，归 ux-designer/conductor）；③ 全量 e2e（~55 条已知红债）未跑——票面口径只跑改动相关 specs，未扩大
Status:       done（①块：交互面+消费面+e2e 对齐；e2e 证据 = 改动相关 specs 双轮绿）
Followups:    1) 锚册回写建议（归 ux-designer）：console-artifactory-parity.md L119 BinFlow 载体列改为「ConfirmDialog.tsx（danger 红边 + confirmDisabled 前置）+ RepoDeleteConfirm.tsx（级联删除警示 + 输入 key 强确认；T-555 起无 deleteContent 复选）」，差距列补注「T-555 起与服务端级联语义对齐，V8 单击确认对照下 BinFlow 仍严一档」；2) t104 W12c/W11 腿安排 dind 可达环境复跑（CI 或 QA VM）；3) 控制台帮助文案若在 docs/user 有删仓双段流描述，建议 tech-writer 同步检查（本票未涉 docs，未核查——非断言存在）
Lessons:      消费面清尾要扫「语义残影」不止于调用层：本票三处非调用面残留（详情页「（可选）全部内容」、孤儿 CSS、e2e 标题措辞）都在 grep「旧语义关键词」时浮出；服务端改「错误驱动的两段交互」为「一次成型」时，UI 侧最大的误导风险是保留一个已无服务端语义的复选框（不勾=以为保内容，实删）——退役控件比改文案更诚实
```
