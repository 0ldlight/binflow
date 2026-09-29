# T-565-design · 删仓级联语义的设计文档消费面翻新（BIN-47 第②块）

```
Ticket:       T-565 / Linear BIN-47（第②块）——docs/design/ 删仓交互规范三处陈旧 400 拒绝语义陈述翻新
Role:         ux-designer
Area:         docs/design/（console-ux.md / console-m8.md / frontend-rewrite-audit.md / console-artifactory-parity.md）
Input:        派发指令（conductor，2026-09-29）+ 行级追加指令（parity L119，2026-09-29）；
              语义事实源 = reports/agents/T-555.md（D-3 裁定实施：DELETE 恒 200 静默级联 +
              JSON 报告体 {repoKey, statusMsg, deletedArtifactsCount, success}；count=文件+folder
              全量、根不计；statusMsg 按 rclass content/plain 两套；?deleteContent=true 接受即忽略；
              未知仓 404 / 非 admin 403 仍在）+ internal/httpapi/repositories.go handleRepoDelete
              （L825-859 只读核对）；web 最终形态 = web/src/pages/repositories/RepoDeleteConfirm.tsx
              + web/src/lib/repos.ts deleteRepo（T-565① 并行票工作树 as-built，只读核对）
Changes:      ① 四处 400 陈述翻新（详见下方逐处清单）：console-ux §4.5 危险区口径、console-m8
              §4.6 确认文案表「仓库」行、frontend-rewrite-audit §2.4 API 表 DELETE 行 + §2.6 辅助行
              RepoDeleteConfirm 描述——全部改写为级联语义（恒 200、旗冗余、破坏性警示保留、
              deletedArtifactsCount 作为成功反馈的规模信息）；② 同语义面顺带翻新：frontend-rewrite-audit
              §3 lib 消费表 delete+deleteContent 措辞；③ 锚册同步（锚名权威源纪律：web 侧两段流
              已拆、旧锚 src 零引用，册不得滞留）——console-ux §10.2 仓库详情块摘除
              repo-delete-content/repo-delete-reason 并留退役注记 + §10.6 退役总表新增行（105→107）
              + 叙事计数链 T-565 段；④ parity 锚册 M2 删仓确认行（conductor 行级追加指令）：
              BinFlow 载体行改新形态、差距行「影响面摘要」措辞修正（规模反馈在删除后 toast），
              豁免判定不变（§9 E1 维持）；⑤ 两册版本纪律：console-ux v1.47→v1.48（票据/状态/§0
              修订行）、parity v1.14→v1.15（状态行 as-built 痕 + §0 修订行）
Files:        docs/design/console-ux.md（v1.47→v1.48：§0 修订表 +v1.48 行；票据/状态行；§4.5 L473
              危险区口径；§10.2 L941-943 锚块；§10.6 叙事链 L2643-2646 + 退役总表 +1 行 L2686）
              docs/design/console-m8.md（§4.6 L324「仓库」确认文案行，无版本表——册头仅 v1.0 状态，
              不另立版本行，票号已内嵌行文）；docs/design/frontend-rewrite-audit.md（L390 辅助行、
              L629 API 表 DELETE 行、L785 lib 消费表——审计快照文档，行内翻新不重构）；
              docs/design/console-artifactory-parity.md（v1.14→v1.15：状态行 prepend + §0 修订表
              +v1.15 行 + M2 行 L120 载体 + L121 差距）；reports/agents/T-565-design.md（本文件）
Tests:        文档变更，无测试面——以服务端 handler 语义为准绳核对（handleRepoDelete 恒 200 +
              报告体字段逐一比对 T-555 活体探针矩阵）
Commands:     Grep docs/design「deleteContent|删除仓库|非空|清空|删仓|repo-delete-*|400」（全量扫描
              定位 + 翻新后复扫零残留）；Grep internal/httpapi/repositories.go「deleteContent|
              deletedArtifactsCount|statusMsg」（handler 语义核对）；Read reports/agents/T-555.md
              （语义事实 + 探针矩阵）；Read web/src/pages/repositories/RepoDeleteConfirm.tsx +
              Grep web/src/lib/repos.ts deleteRepo（as-built 终形态核对：复选/reason 面已拆、旗已
              不再发送、成功 toast 携带计数）；Grep web/src「repo-delete-content|repo-delete-reason」
              （src 零活引用，仅退役注释 1 处——退役登记成立）；Grep docs/design 两锚名（册内仅
              退役语境引用）；翻新后回读四册编辑区（表结构/版本行完整）
Outputs:      console-ux.md v1.48（400 陈述翻新 1 处 + 锚退役 2 枚登记）；console-m8.md（400
              陈述翻新 1 处）；frontend-rewrite-audit.md（400 陈述翻新 2 处 + 旗消费措辞 1 处）；
              console-artifactory-parity.md v1.15（M2 行级改写 2 格——载体/差距；conductor 行级
              追加指令兑现）；新增锚 0 / 退役锚 2（repo-delete-content、repo-delete-reason）
Compatibility: 本票 = T-555 语义变更（known-divergence rest/nonempty-repo-delete-cascade-guard，
              D-3 用户裁定对齐 A）的设计文档消费面；设计规范不再描述「400 拒绝非空仓 +
              deleteContent 旗确认」双步门，与 handler 恒 200 级联语义一致；parity M2 行登记
              新形态「更贴近参照 V8（参照本无 checkbox）」，type-the-key 维持 §9 E1 有意偏离豁免；
              matrix.yaml 互链：无新增（本票无 L11 锚册新行，M2 既有行内改写，不代写矩阵行）
Security:     删仓权限口径未动：DELETE = CapRepoWrite（admin-only 双门）+ L4 预收敛 + 403 兜底
              措辞随新文案保留于 web 注释与 §4.5 口径（未知仓 404 通用错误 toast）；破坏性警示
              保留并强化明示「全部制品、没有撤销」——静默级联下警示与输入 key 强确认是唯一
              误删防线，设计口径明示不得随旗门退役而弱化
Performance:  不涉及（纯文档票；级联删除的计数口径 = 删除前 ListByPrefix 全量 len，见 T-555）
Evidence:     逐处清单（文件:行号 + 原文摘句 → 新文摘句，翻新后行号）：
              1) docs/design/console-ux.md:472→473（§4.5 危险区）
                 原：「非空仓显示「将删除 N 个制品」+ □ 同时删除内容（deleteContent）+ 输入 key 确认；
                 对齐 API：不带 deleteContent 的非空删除是 400，UI 必须把这条路走通而不是让用户撞 400。」
                 新：「级联警示文案「将永久删除 <key> 仓库及其全部制品（删除没有撤销）」+ 输入 key 确认；
                 对齐 API（T-555 级联语义）：DELETE 恒 200 静默级联（…deleteContent 旗被接受但冗余——
                 UI 不设复选、不发该参数），删除规模由响应报告体 deletedArtifactsCount 在成功 toast
                 呈现（「已删除 N 项内容」）；未知仓仍 404。」
              2) docs/design/console-m8.md:324（§4.6 确认文案表·仓库行）
                 原：「+ □ 同时删除内容（非空必勾，对齐 deleteContent 400 契约）+ 输入 key 确认」
                 新：「（T-555 级联语义：服务端恒 200 静默级联，无 400 拒绝门、deleteContent 旗
                 冗余——复选不设）+ 输入 key 确认；成功反馈携带报告体计数（「已删除 N 项内容」）」
              3) docs/design/frontend-rewrite-audit.md:629（§2.4 API 表 DELETE 行）
                 原：「DELETE api/repositories/{key}?deleteContent=true | 删仓（非空需参数否则 400）」
                 新：「DELETE api/repositories/{key} | 删仓（T-555 级联：恒 200 静默级联 + JSON 报告体
                 deletedArtifactsCount；?deleteContent=true 接受但冗余；未知仓 404）」
              4) docs/design/frontend-rewrite-audit.md:390（§2.6 辅助行）
                 原：「useRepoDelete（输入 key+deleteContent 复选；非空仓 400 原因带回+预勾选两段流）」
                 新：「useRepoDelete（输入 key 确认 + 级联警示文案；T-555 恒 200——成功 toast 携带
                 报告体 deletedArtifactsCount「已删除 N 项内容」）」
              5) docs/design/frontend-rewrite-audit.md:785（§3 lib 消费表，顺带面）
                 原：「delete+`deleteContent`」→ 新：「delete〔T-555 级联报告体，旗退役〕」
              6) docs/design/console-artifactory-parity.md:119→120（M2 BinFlow 载体行——conductor
                 行级追加指令）原：「RepoDeleteConfirm.tsx（非空仓 deleteContent 复选 + 影响 N 制品
                 摘要 + 输入 key）」新：「RepoDeleteConfirm.tsx（T-555 级联语义，v1.15：警示文案…
                 + 输入 key 确认 + 成功 toast 携带报告体 deletedArtifactsCount…——非空仓复选/两段流已拆）」
              7) 同文件 :120→121（M2 差距行，同指令联动）原：「输入 key 确认 + 红色 danger + 影响面
                 摘要是更安全的有意偏离」新：「…级联警示文案（规模反馈 = 删除后 toast 的
                 deletedArtifactsCount——T-555 级联语义，v1.15 修正「影响面摘要」措辞）…」
              8) docs/design/console-ux.md:940→941-943（§10.2 锚块摘除两锚 + 退役注记）+
                 §10.6 L2686 退役总表新行 + 计数链 L2643-2646（105→107）
              已核对·不动（无 400/旗门语义，按指令④登记）：
              a) console-ux.md:111（P5 危险操作显式化原则——「输入 key 确认 + 影响面摘要」为危险
                 动作族原则层陈述，GC apply/导入仍有量化摘要，删仓腿由级联文案承载，语义仍成立）
              b) console-m8.md:250（C9 危险确认族）/ :326 / :573（同上，族级原则陈述）
              c) console-ux.md:402 / :450 / :1867 / :1911 / :2657 / :2692（删仓入口/锚引用，无 400/旗门）
              d) parity:117-118（M2 Artifactory 行为列——A 侧官方形态记录，本就无 checkbox，不变）
                 / :339（仓库详情行「删仓危险区…BinFlow 更严，豁免」）/ :388（V8 核验行）
              e) parity §9 E1（L408，危险动作分治豁免登记——族级，删仓腿无 400/旗门断言，维持）
              f) docs/design/gpg-keypair.md:134（GPG keypair DELETE 引用护栏 400——不同端点，无关）
              NOT_RUN：web/e2e 删仓腿断言翻新面（web 域，T-565① 并行票承载——工作树见
              repositories.spec.ts / m8/repositories-admin.spec.ts / t104-supplements.spec.ts 已改，
              但断言内容未逐条核对，非本票域）；anchor-audit.mjs --ledger 对账器运行面（本角色无
              Bash，退役登记的机器自证由 conductor 收编后跑）
Status:       done（8 处改写 + 6 类已核对不动面 + 2 锚退役登记；docs/design 内 deleteContent/非空 400
              陈述复扫零残留——现存引用全部为级联语义新文或退役语境）
Followups:    ① conductor 收编后跑 web/scripts/anchor-audit.mjs --ledger（A3/A4 门自证：退役两锚
              册↔src↔spec 三方一致）；② web/e2e/README.md:48 仍述「?deleteContent=true（m8/
              repositories-admin §3 形态）」——web 域文档，归 T-565① 或后续 web 票（已越界不代改，
              上报 conductor）；③ 非阻塞观察（非本票面）：console-ux §4.2 L402「删除仓库不在列表
              行内」与 §10.5 L1867「repos-delete-<repoKey> 行尾删除入口」as-built 并存矛盾，
              建议后续锚册票对齐口径
Lessons:      服务端语义变更的设计文档消费面不止「规范句」——锚名权威源（console-ux §10.2/§10.6）
              与 parity 载体列是同一变更的第二、第三消费面，漏登即 ledger 断链（册有 src 无 = A4
              FAIL）；审计快照类文档（frontend-rewrite-audit）的 API 契约行被重写消费，语义过期
              同样误导，行内翻新 + 票号内嵌优于重构
```
