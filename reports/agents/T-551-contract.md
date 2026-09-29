# T-551 stage 1 / BIN-33 — sidecar validator 族契约修订 + L032 台账登记批（R5 / iteration-1544）

```
Ticket:       T-551 第一段 / Linear BIN-33「sidecar validator 族契约修订 + L032 台账登记批」（R5 / iteration-1544）
Role:         compatibility-engineer（maven/rest/conan/npm 域台账批实例）
Area:         docs/compatibility/（本段唯一写入域；stage 2 落码归 dev-registry-adapter 并行票，不归本段）
Input:        reports/compatibility/L032-t550-followup-arms.md（R4 六臂差分，r1≡r2≡r3——Arm 1 D-3 / Arm 2 kcache / Arm 4 conan v1 / Arm 6 member sidecar 17 维矩阵）；reports/agents/T-548.md（L031 修复 15 字段日志——resolved 回填锚）；docs/compatibility/contracts/maven-virtual.yaml（T-542 派生 sidecar 契约宿主）；internal/adapter/maven/（只读核对 writeDerivedSidecar 调用点：virtual_metadata.go:116 + handler.go:159 两面共用单 helper）
Changes:      ① 契约修订：maven-virtual.yaml 新增第 4 条目 maven/derived-sidecar-validator-family（DIVERGENT/high，divergence_ref 挂台账新账）——A 面 live 实形行为句式：旁车 200 响应携带自身 Last-Modified（派生时刻，晚 body 数秒）、不发 ETag；If-Modified-Since 命中→304、旧值→200；body 面（ETag+LM+双条件）双端一致在 rider 条目原样保留并加 L032 复验证据。rider 条目（maven/virtual-snapshot-versions-m3-rider）修订：旁车臂 .sha1/.md5 扩为 .sha1/.md5/.sha256（L032 match×3 A 面 live 取证收编，旧「.sha256 不在契约面」注记废止留痕）、unobserved 第三项改指分立条目、binflow_state 加修订注记（body/digest 面 L032 复验 14/17 维一致无回归，status 维持 VERIFIED）、evidence 增 L032 Arm 6 条目；文件头加 T-551 修订 changelog（保留 T-542 原契约对照口——3b58b082 版旁车臂仅断言内容维正是实现自由猜测根因）。② 台账五件：新账 maven/derived-sidecar-validator-family（BUG，fix in flight T-551 stage 2——B 面修复并行派发中，工作树已有未提交落码）；rest/kcache-deploy-404-wording 扩展（Arm 2 live 确认 + remote 本体 405 腿按 Arm 2 建议并入同族不立新 D-id，fix in flight T-553）；npm/virtual-packument-merge-latest-tag 回填 resolved（T-548 双端三 case 连续两轮 PASS）；conan/v1-packages-delete-virtual-plain-404 更新（Arm 4 A 实形=400+errors envelope「Unsupported Conan v1 repository request for '<K>'」——原 plain→envelope-404 提案仍三重分歧 status/carrier/措辞，B stock 许可门不可达系环境事实，licensed 复测待许可）；rest/nonempty-repo-delete-cascade-guard 追加 ruling-ready 注记（级联计数/文案/善后态证据齐，终裁=用户/ADR，维持 UNKNOWN）。③ matrix.yaml：头部 changelog 两块（T-551 主批 + 卫生款）+ D12-R18 last_difftest 补 L032 链 + note 加 stage 1 注记（4 条目）。④ 卫生款：三处存量 plain scalar「 #」YAML 截断加引号修复（内容零变更）——D12-R18 last_difftest（「PR #156」起 L030 链自 L029-1 落账即整体落注释区）、known-divergence#docker/remote-ping-credential-class-message-granularity review_gate、#rest/security-read-family-readonly-admin-gate evidence（后两处「#2」截断）。
Files:        修改：docs/compatibility/contracts/maven-virtual.yaml（4 条目，+1 DIVERGENT）、docs/compatibility/known-divergence.yaml（81 条目：+1 新账、1 回填 resolved、3 条 rationale/evidence 扩证、2 处卫生款）、docs/compatibility/matrix.yaml（changelog + D12-R18 行级 + 1 处卫生款）。新增：本日志。未动：产品码、tools/difftest/、reports/compatibility/。
Tests:        无新 probe（本段为契约/台账批，证据源=L032 既有差分三轮；新契约 evidence 挂 L032 Arm 6，无零证据新立条目）。surface 存在性核对：grep -rn writeDerivedSidecar internal/adapter/maven/ → virtual_metadata.go:116（virtual 面）+ handler.go:159（成员面）两调用点命中——B 面修复面存在且单点，DIVERGENT 定级有落码面对照。
Commands:     YAML 门（三文件逐个 safe_load 全过）；Z 复算脚本（python3 yaml：resolved=有 resolved 键 / gated=review_gate 以 resolved 开头 / 余为 open）——先验基线：改动前 total=80 resolved=33 gated=4 open=43 四分类 BUG8/UNKNOWN33/INTENTIONAL1/UNSUPPORTED1，与 R4 iteration-1543 报告逐项吻合（口径验证通过）；改后见 Outputs。矩阵口径门：rows=201、五态 95/17/57/20/12、contract_ref 实计 38=summary、last_difftest 实计 51=summary（D12-R18 加引号后全文 len=804 恢复）。引用完整性门：divergence_ref known-divergence.yaml#maven/derived-sidecar-validator-family 台账 grep 命中 3 处；L032-t550-followup-arms.md / T-548.md / L028/L031 报告文件 ls 存在。
Outputs:      契约 4 条目（maven/virtual-snapshot-versions-m3-rider〔修订，VERIFIED 维持〕、maven/metadata-version3-system-switch〔未动〕、maven/deploy-pom-path-consistency-gate〔未动〕、maven/derived-sidecar-validator-family〔新立，DIVERGENT〕）；matrix 行更新 1 行（D12-R18 note+last_difftest，行态 compatible 不变、零新行）；known-divergence 新增 1 条（sidecar validator BUG）+ 扩展 1 条（kcache）+ 回填 1 条（L031 resolved）+ 更新 2 条（conan v1 / D-3）。
Compatibility: 状态机变动：新契约 1 条 DIVERGENT（maven/derived-sidecar-validator-family）；rider 契约 VERIFIED 维持（断言面无回归，L032 复证）；无 IMPLEMENTED→VERIFIED 翻态（本批全部为差异确认/取证/回填）。置信度分布（maven-virtual.yaml 4 条目）：high 3（rider / GAV 门 / validator family〔新立〕）/ medium 1（version3 开关，未动）。四问 Z 复算：总 81 = 34 resolved + 4 gated + 43 open（vs R4 基线 80=33+4+43：+1 新账 sidecar 进 open、+1 L031 出 open 进 resolved——两动作 open 净零变动、total +1、resolved +1；与票面预期 81=34+4+43 一致；票面「+2 open 登记」按动作计数——kcache remote 腿按 L032 Arm 2 建议并入既有条目不增账，如实说明）；open 四分类 BUG 8 / UNKNOWN 33 / INTENTIONAL 1 / UNSUPPORTED 1（构成与 R4 相同：L031〔BUG〕出、sidecar〔BUG〕进相抵）。X=201 不变；Y=109.5、coverage 60.50%（无行态变动，未重算变动）。
Security:     契约 authentication 断言：新条目 authentication={type: basic, role: user}（沿 rider 条目既有面）；本段零凭据触碰、零双端访问（纯台账批）。
Performance:  契约无 timing/p95 面断言（validator 族为语义面；「晚 body 数秒」为时序观测非预算断言）。
Risks:        ① 新契约 validator 维直接取证腿=.sha1×java-agent（L032 矩阵）；.md5/.sha256 validator 维、virtual 面旁车 validator、INM-on-旁车 A 面应答、LM 跨请求稳定性四项按纪律入 unobserved（禁猜），T-551 stage 2 修后差分复验臂可顺带钉。② stage 2 落码已在工作树未提交未验收——台账 fix-in-flight 措辞按「并行派发中」如实标注，resolved 回填须待双端复验（本段不抢跑）。③ conan v1 对齐目标已按 A 实形更新为三重（400+envelope+Unsupported 文案），licensed B 复测前不得标 VERIFIED。④ 卫生款修复后建议后续批加「plain scalar 含 # 的 lint 门」（本轮手工扫描代替，全账现存命中已清零——引号内/行尾注释除外）。
Blockers:     无。（D-3 终裁待用户拍板系排程事非阻塞；conan licensed 复测待许可同。）
Next:         ① 转 differential-qa-engineer：T-551 stage 2 修后复跑 maven-member-snapshot-sidecar-304（含 unobserved 四臂顺带取证）→ validator 契约 DIVERGENT→VERIFIED + 台账 resolved；T-553 修后复跑 rest-kcache-deploy-404-wording 三腿。② 转 conductor：D-3 ruling-ready 呈批（pending-rulings 增席）；conan v1 裁定票（A 实形已具备）。③ 提金候选（L032 报告四项：sidecar 17 维 A 腿/conan v1 400 envelope/deploy 拒绝族三腿文案/非空仓级联 body 形）——入金前置条件=时间戳 normalize（maven 域 golden 空白，沿 T-549 注记）。
```

## D-3 裁定落地（2026-09-29）

用户已对 D-3 拍板（会话内裁定，conductor 转达）：**BUG，对齐 A 面**——非空仓 `DELETE /api/repositories/<key>` 须改为 A 面 7.161.26 实测形：200 静默级联删除，body 含 deletedArtifactsCount（计数口径差 A=6 计数 vs B 节点数 5 系证据细节，修复票活体探针定准）。

- 台账翻态：known-divergence#rest/nonempty-repo-delete-cascade-guard **UNKNOWN → BUG**；authority 由 pending 改为 `user_ruling`（用户裁定 2026-09-29 会话内、conductor 转达——三类 authority 之「用户裁决」）；review_gate 改为 fix ticket **T-555/BIN-37（conductor 已立，实施 R6）**：200 静默级联 + deletedArtifactsCount body + B 现 ?deleteContent=true 放行臂语义随票定去留，修后差分复验（L028 D-3 / L032 Arm 1 臂）翻绿 → resolved；rationale 追加裁定落地段（ruling-ready 素材与 L032 Arm 1 证据指针维持）。
- 四问 Z 复算更新（python3 yaml，口径同前）：

```
Z: total=81 = 34 resolved + 4 gated + 43 open
open four-class: {'BUG': 9, 'UNKNOWN': 32, 'INTENTIONAL_DIFFERENCE': 1, 'UNSUPPORTED_FEATURE': 1}
asserts OK: D-3 BUG/user_ruling/T-555; open BUG 8->9, UNKNOWN 33->32, totals unchanged
```

  对账：open 总数不变 43（D-3 条目留在 open、仅四分类间移动）；BUG 8→9 / UNKNOWN 33→32；总账 81=34+4+43 不变。
- 其余不动：matrix 零触碰（D-3 仍台账级跟踪不入行态——修复未实施，D12-R18 note 的遗留注记继续成立）；依旧零 commit。

## R5 终批第一半（2026-09-29，L033 差分定谳收编）

- Input: reports/compatibility/L033-r5-fix-verification.md（含 T-556 追加节 r5≡r6——第二半指令未到，本半不动 matrix/Z 口径）。
- Changes: 台账新增 4 条 UNKNOWN（L033 Arm C 提案①②③ + Arm A 跨秒 LM 残差）；金样评审 5 候选 1 入 4 弃；G1 落盘 golden/maven/derived-sidecar-validators/（首个 maven 域金样）。
- Files: docs/compatibility/known-divergence.yaml（Edit 追加，+4 条）；docs/compatibility/golden/maven/derived-sidecar-validators/{request,metadata,response}.yaml（新建 ×3）。
- Tests: 台账 YAML 门 OK；四分类复算 total 85 = resolved 34 + gated 4 + open 47（BUG 9 / UNKNOWN 36 / INTENTIONAL 1 / UNSUPPORTED 1——D-3 落地基数 +4 新 UNKNOWN，精确吻合零意外）；金样三门 YAML 门 OK；新块 ` #` 注释雷区扫 0 命中。
- 登记明细:
  - `maven/handle-policy-reject-409-wording-family`（提案①）——status 双面同 409、文案族异（A resolution/deployment 语义句 vs B ME-08 参数式）；verbatim 双文案入 rationale。
  - `maven/handle-policy-member-get-class-gate`（提案②）——A 读路径 409 门 vs B 404；先决发现（hr=false release PUT 双面拒→栖身子面 NOT_RUN by construction）一并入档。
  - `maven/plain-snapshot-path-resolve-404`（提案③）——B 对 plain-SNAPSHOT 拼写恒 404（含 ctl 默认成员对照腿——独立既有面孔，与 handle* 解耦；对照腿同时证模块清单维不受混杂）。
  - `maven/derived-sidecar-lm-per-request-stamp`（跨秒 LM 残差）——**单立 UNKNOWN 落位**（不并入翻绿条目）：capable 面 304 稳定 / java-agent 剥离面 200+新 LM（per-request 派生戳）；A 侧 304 为机制推导值（跨秒直接腿不可达，如实标注）；review_gate 含 stored-stamp 升级微票路径。
- 金样评审（入 1 / 弃 4，弃项理由留痕）:
  - **入 G1** `maven/derived-sidecar-validators`——契约锚在（contracts/maven-virtual.yaml#maven/derived-sidecar-validator-family + matrix D12-R18），双报告 live 对账，L033 17/17 翻绿完成态；跨秒残差与 .md5/.sha256/virtual 面推定入 known_gaps。matrix 行 golden_ref 回填归第二半（本半不动 matrix）。
  - 弃 G2（conan v1 400 envelope）——无契约锚：conan.yaml 16 条目中无 v1 POST packages/delete virtual 面条目（error-envelope-family 明示 virtual 面外；v1-delete-tree/v1-ghost-delete 为 DELETE 族异面）；A 实形已 verbatim 存台账，契约化随裁定票后随票入金。
  - 弃 G3（deploy 拒绝族三腿文案）——无契约锚：面在 ledger+spec（remote-cache-projection §2.1/§8.2）无 contract_ref 可指；B 已逐字对齐（L033 Arm B PASS×3），契约条目立时以双端一致完成态入金。
  - 弃 G4（非空仓级联 200 body）——无契约锚（storage-admin.yaml 10 条目全 GC 族，无 DELETE /api/repositories/{key} 条目）；且 deletedArtifactsCount 计数口径（6 vs 5）用户裁定明示需活体探针定准——语义未定先行锚金样过早。修复票 T-555/BIN-37 契约化时入。
  - 弃 G5（Arm C handle* 22 维 A 矩阵）——无契约锚：handle* local 面尚无契约条目（提案①②③均 UNKNOWN 待裁，模块级面契约化随 T-556 翻回票）；22 维实形已 verbatim 存 L033+本批台账，无丢失。
- Compatibility: 台账侧 open 43→47（UNKNOWN 32→36，BUG/INTENTIONAL/UNSUPPORTED 不变）；matrix 四态与 Z 口径本半未动（第二半统一复算——L033 追加节 r5≡r6 已出，等 conductor 指令）。
- Risks: 4 新 UNKNOWN 中①②有同族耦合（共享 A 面 409 语义），裁定时建议合并考量；plain-SNAPSHOT 时间戳拼写构造未测。
- Next: 第二半（等指令）= Z 复算（预期 total 88 = resolved 37 + gated 4 + open 47）+ modulereleases-skip 翻 resolved（r5≡r6 已达 gate 预写条件）+ Arm A/B resolved 回填 + D12 行 golden_ref 回填。

## R5 终批第二半（2026-09-29，L033 三 resolved 落账 + Z 终算）

- Input: reports/compatibility/L033-r5-fix-verification.md（含追加节「T-556 修复验证」r5≡r6——三项 gate 预写条件全达，conductor 第二半指令）。
- Changes:
  1. `maven/virtual-metadata-modulereleases-skip` → resolved（fix_ref=T-556/BIN-38；锚「T-556 修复验证」节 r5≡r6 面级 b==a 双轮，hwm 两维 404→200+versions 双列；review_gate 兑现注记=差分偏参照并入→双站点同翻〔filterMetadataSteps + virtual.go walk 镜像〕，walk 层 release 族面 vacuous 零行为损失如实保留）。
  2. `maven/derived-sidecar-validator-family` → resolved（fix_ref=T-551 stage 2 / BIN-33；锚 Arm A 17/17 PASS×3 三维翻绿+维持面全绿）。跨秒 LM 残差不随翻——单立 UNKNOWN 两态并存如实。**契约条目随翻 VERIFIED**（contracts/maven-virtual.yaml——evidence 补 L033 Arm A difftest + golden-capture 两锚，divergence_ref 转历史指针注记；binflow_tested 更新为翻绿轮版本）。
  3. `rest/kcache-deploy-404-wording` → resolved（fix_ref=T-553/BIN-35；锚 Arm B 三腿逐字 PASS×3——remote 本体 405→404+同族文案双翻、virt 405 腿无回归）。
  4. matrix.yaml 终触：D12-R18 golden_ref 回填 `golden/maven/derived-sidecar-validators`（首个 maven 域金样挂链）；summary golden_ref_non_null 1→2；header changelog 新块。行态零变更，X=201 / Y=109.5 / 60.50% 不动。
- Files: docs/compatibility/known-divergence.yaml（3 处 Edit 翻态）、docs/compatibility/contracts/maven-virtual.yaml（1 处 Edit 翻 VERIFIED）、docs/compatibility/matrix.yaml（3 处 Edit：golden_ref 行 + 计数器 + changelog）。
- Tests / Z 终算复核（沿四问口径：resolved=有 resolved 键；gated=review_gate startswith 'resolved'；open=其余）:

```
$ python3 - <<'EOF'
import yaml
from collections import Counter
e = yaml.safe_load(open('docs/compatibility/known-divergence.yaml'))['entries']
res = [x for x in e if 'resolved' in x]
gate = [x for x in e if str(x.get('review_gate','')).startswith('resolved')]
op = [x for x in e if x not in res and x not in gate]
print('Z 终算: total %d = resolved %d + gated %d + open %d' % (len(e), len(res), len(gate), len(op)))
print('open 四类:', dict(Counter(x['classification'] for x in op)))
EOF
Z 终算: total 85 = resolved 37 + gated 4 + open 44
open 四类: {'BUG': 7, 'UNKNOWN': 35, 'INTENTIONAL_DIFFERENCE': 1, 'UNSUPPORTED_FEATURE': 1}
```

- **与预期的出入说明（按指令以实算为准）**：第一半报告 Next 行曾写「预期 total 88 = 37+4+47」——**系我方口径笔误**：翻态只把 open 移入 resolved，不改总条目数（85=81+4 第一半新账，第二半零新账）。正确终值即上：total 85 = 37 + 4 + 44；四类 BUG 7（9−sidecar−kcache）/ UNKNOWN 35（36−modulereleases-skip）/ INTENTIONAL 1 / UNSUPPORTED 1，与指令引述的「BUG 9/UNKNOWN 36」差值正是三条翻 resolved 条目的原分类（BUG×2 + UNKNOWN×1）。
- 矩阵口径门：rows 201、四态 compatible 95 / partial 17 / absent 57 / not_applicable 20 / superset 12（零变更）；contract_ref_non_null 38 / golden_ref_non_null 2 / last_difftest_non_null 51；maven 金样行=D12-R18。契约门：validator-family status VERIFIED / confidence high / evidence kinds [difftest, difftest, golden-capture]。YAML 门：known-divergence + maven-virtual + matrix 三文件 safe_load 全过。
- Compatibility: 台账 resolved 34→37、open 47→44（BUG 9→7、UNKNOWN 36→35）；状态机 DIVERGENT→VERIFIED ×1（契约）；X/Y/coverage 不动。
- Security / Performance: 无新增断言面（沿用条目既有）。
- Risks: 4 新 UNKNOWN（第一半）中提案①②共享 A 面 409 语义建议合并考量；modulereleases-skip 的 remote 面前置（席位探针 a=false/b=true）未变——remote canonical handle* 落地日需复查本条 resolved 前提（面级已 local 定谳）。
- Blockers: 无。
- Next: 无（R5 终批两半全部落地；战报 Z 定值=85=37+4+44）。

## 断点快照（如被中断）

- 已完成：契约修订（4 条目）+ 台账五件 + matrix 行级/changelog + 三处 YAML 截断卫生款 + 全部门（YAML/Z/矩阵口径/引用完整性）+ D-3 裁定落地翻态（BUG/user_ruling/T-555，Z 复算更新）+ R5 终批第一半（4 新 UNKNOWN + 金样 1 入 4 弃 + G1 落盘）+ R5 终批第二半（3 resolved 翻态 + 契约翻 VERIFIED + D12-R18 golden_ref 挂链 + Z 终算 85=37+4+44）。
- 未完成：无（两半票面动作全部落地）。
- 断点位置：n/a。
- 恢复入口：conductor 收编本批（git 由 conductor 统管，全程零 commit）；后续 resolved 回填点=D-3（T-555/BIN-37，R6）与 L033 三新面孔裁定票。
