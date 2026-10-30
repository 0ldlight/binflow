Ticket:       T-L043-probe — L043 五候选活体探针（wave-5，双系统差分）
Role:         differential-qa-engineer
Area:         tools/difftest/v2/ + reports/compatibility/L043-wave2-report.md（rerun 章节）
Input:        conductor 派发五候选（catalog 空降级 / schema1 缓存相位 + C-b / bearer 跨面矩阵 / tags/list 非活体 / Accept 宽严差）；A=JFrog 7.161.26 @192.168.120.36:32082，B=BinFlow @192.168.120.36:32083（version 端点 dev-aceee772，阈值-2 行为活体在装——401,401,403 实测）；契约域 docker-remote（wave-1~3 既有判定为对照基线）
Changes:      新增 batch case ×6（cases/docker_catalog_membership_matrix.py、docker_schema1_phase_boundary.py、docker_bearer_face_full_matrix.py、docker_tags_list_liveness.py、docker_tags_list_accept_strictness.py、docker_schema1_tag_mutation_heal.py）；独立探针 ×3（wave5_tags_ttl_window_probe.py、wave5_schema1_recovery_probe.py、wave5_bside_threshold_context.py）；批跑/门/摘要（wave5_run.sh、wave5_rerun_gate.py、wave5_summarize.py）。不改产品码、不改契约/fixtures/normalize/known-divergence（evidence 补充以提案形式落报告）
Files:        上列 12 个新脚本 + reports/compatibility/L043-wave2-report.md（追加 rerun 章节）；证据 run/l043-probe{,-2,-3}/（gitignored，本地留存）
Tests:        批 1（run/l043-probe）：5 FAIL + 1 BLOCKED（catalog case 用例 bug：blob Location 处理 NameError）；修 case 后批 2/批 3：6 FAIL ×2 全执行（FAIL=分歧成立记录型，与 wave-3 口径一致）；复跑门 wave5_rerun_gate.py(2,3)=PASS（6 id 状态逐一相等、首失败原因 nonce/count 归一后逐字一致、检查账本同构、零 ok_mismatch）；批 1 与批 2 五共同 case 状态同构。恢复采样器 30/30 采样完成；TTL 窗口采样器 30/30 完成；阈值上下文探针 3 发完成。全部 case 无 skipped
Commands:     bash wave3_preflight.sh（双端+上游 5×200）；python3 -I wave5_bside_threshold_context.py（401,401,403）；bash wave5_run.sh run/l043-probe{,-2,-3}（score: {"pass":0,"fail":6,"blocked":0,"not_run":51,"total":57,"coverage":0.0}）；python3 -I wave5_rerun_gate.py run/l043-probe-2 run/l043-probe-3（PASS）；python3 -I wave5_summarize.py run/l043-probe-2；两个长窗采样器后台运行（各 30×60s）；curl 活体（变异 tag 分钟级复查：上游 v2 / A v1 schema2 / B prettyjws）；python3 -I -c json.load ×3 results.json 解析通过；证据凭据扫描（grep 密码变量/Basic 值零命中）
Outputs:      reports/compatibility/L043-wave2-report.md rerun 章节（五探针逐项判定 + 变异附加 case + 双采样器 + known-divergence.yaml 候选 evidence 补充提案 ×10 + 四问口径）。核心定谳：①A catalog 不变量四拓扑闭环（populated-local 入证）+ B 成员规则镜像路径粒度双向可逆；②schema1 A 服务时转换 vs B 缓存键不含 Accept（c3/c4 持久性 + n1/n2 对照双向钉死）；③bearer 24 格完成——A 片内 6 面统一机制、B 内容面退化 + token 面挑战漂移 + ping 面同构；④tags/list 非活体窗口改判（非常数、工况依赖：manifest 拉穿 ≤60s 刷新 vs 纯 tags 读 ≥30min 未刷新，双工况对照）；⑤Accept 严格度规则钉死（A：Accept 全为 manifest 族即 406）；⑥新分歧：已缓存 tag 上游变异双端分钟级盲；⑦B 阈值-2 活体在装（L043 收敛建议 1 可结）
Compatibility: 差异分类建议：UNKNOWN/待裁 8（候选③②①④ evidence 补充 + C-a 全域化 + C-c 双面扩展 + 新候选 tags-list-accept-strictness + 新候选 manifest-cache-mutation-blindness）；INTENTIONAL 恒 0（无 authority 不裁定）；回归对照：B 熔断阈值线口径 fixed（401,401,403 与 A 对齐，L043 建议落地验证）；候选③④①② divergence 仍在（形态深化）；新增 2 项（变异盲 / Accept 严格度独立条目提案）
Security:     bearer/breaker 面 48 发矩阵 + 3 发阈值探针零熔断触发（recurrent_wording 全 false）；basic-bad 每格一次性不存在用户名（12/轮）admin 永不触碰；C-a（B 内容面+token 面匿名放行，blob 回真字节、token 真发票；A 六面全闸）维持安全语义反向，报 conductor + 安全侧裁定；凭据零落盘零打印（.env 行首锚定注入，证据凭据扫描零命中）
Performance:  批跑单轮 ~6.5min（含 case 内 90s/180s 受控 sleep）；两长窗采样器各 ~31min 后台完成零超时；无异常耗时 case
Risks:        ①批 1 catalog case 崩溃遗留孤儿仓 diff-cat5-1a187f8ca10（已双端 DELETE 200+GET 400 复验清净，批 2 拓扑事实曾含它——报告已注 93/94 双轮数字）；②C-b 绝对 TTL 未定（60s 读节奏下 ≥30min 未愈，读续期可能性未排除——采样结论为下限非定数）；③tags 视图刷新的机制归因（manifest 拉穿假设为双工况对照强支持，未解剖内部机制）；④B version 端点字符串（dev-aceee772）与行为面（阈值-2）不一致——按活体行为口径理解，建议部署侧核对版本标注链路；⑤上游残留 tag 结转 GC 清单（w5w*/w5c1*/w5n* ×3 轮、w5t* ×3、w5rec*、w5win*、w5m2* ×3 + 变异 config blob ×2；上游 DELETE 405 沿 L043 口径）；⑥门归一化含 count=<N> 通配（上游残留增长量非结论量，已注明）
Blockers:     无（A/B 双端 + 上游中继全程可达）
Next:         待 compatibility-engineer：①收编报告内 10 项 evidence 补充/提案进 known-divergence.yaml（候选④的「~54min」改判为工况依赖表述）；②新契约提案裁定（tags-list-accept-strictness、manifest-cache-mutation-blindness）；③C-a 匿名放行报安全侧。待 conductor：B version 端点与部署行为不一致核实。可选后续实验：manifest 内容缓存（非 tags 视图）是否同样受拉穿刷新；C-b 恢复窗的静默对照（零读采样）

---

## Day-2 复验附注（2026-10-30，同票重复派发 → 验证执行，非新定谳）

- **判定**：wave-5 交付已于 2026-10-29 提交（`c4c14603`，本分支 claude/lzw-followups）；本日重复派发按跨日复验执行，五探针定谳全部维持。环境不变性 preflight（A/B ping + fixture + 上游 5×200；活体抽查 A catalog 空体零 CT / B JSON、A 406 vs B 200 双签名复现；B 仍 `dev-aceee772`）。
- **命令**：`bash l044_probe_rerun_preflight.sh`（复现签名全中）；`bash l044_probe_day2_run.sh` → `run/l043-probe-4`（score 与定谳轮同构，6 FAIL=分歧记录型）；`python3 -I wave5_rerun_gate.py run/l043-probe-2 run/l043-probe-4` = PASS、`…-3 …-4` = PASS（零 ok_mismatch）——**零 flake 跨日三轮同构**；`bash l044_probe_day2_cleanup.sh`（B catalog 仅 fixture 条目、A 93 仓零残留、上游 GC 枚举）。
- **新增文件**（复验工装，非 case）：`l044_probe_rerun_preflight.py/.sh`、`l044_probe_day2_run.sh`、`l044_probe_day2_cleanup.sh`、`l044_probe_upstream_gc_ledger.py`；报告 rerun 章节尾追加 day-2 复验小节。
- **Risks 增补**：上游残留结转清单更新至 22 枚 `w5*` tag（本日新增 `1a18ab0689x` 家族 ×5）；其余 Risks 沿用上文 ①-⑥。
- **Next 不变**：known-divergence.yaml 十项 evidence 补充/提案本日 grep 复核仍未收编，维持待 compatibility-engineer；C-a 报安全侧维持。
