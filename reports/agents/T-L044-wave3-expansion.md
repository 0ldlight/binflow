# T-L044-wave3-expansion 工作日志

```
Ticket:       T-L044-wave3-expansion — 差分 wave-3 扩展：L043 五候选活体复验 + bearer 跨面矩阵补全 + tags TTL 工况依赖确认
Role:         differential-qa-engineer
Area:         tools/difftest/v2/（wave3e_* 工具链 + cases/wave3e_* 五探针）+ reports/compatibility/
Input:        reports/compatibility/L043-candidates-deep-dive.md（五项定谳，执行域 t634-count）；契约面 docker/registry-catalog-bare-root、docker/remote-schema1-conversion、docker/bearer-failure-shape、docker/remote-tags-list-freshness-and-406；模式 dual；A=http://192.168.120.36:32082（7.161.26）、B=http://192.168.120.36:32083（简报 dev-aceee772 → 活体实测 dev-fdeabbf2）
Changes:      新增 wave3e 工具链：wave3e_lib.py（凭据行首锚定/证据脱敏/suffix 证据名）、wave3e_preflight.py（健康+夹具+B 版本门）、cases/wave3e_p1_catalog_reverify.py（catalog 六臂含建删仓）、cases/wave3e_p3_bearer_matrix.py（4 面×6 臂×双端 48 格）、cases/wave3e_p2_schema1_reverify.py（热 6+冷 6 相位）、cases/wave3e_p5_tags_accept_reverify.py（九臂 406 规则）、cases/wave3e_p2b_d1_envelope_capture.py（D-1 全信封专捕）、cases/wave3e_p4_ttl_workload.py→wave3e_p4_ttl_workload.py（TTL 工况实验，后台长跑）、wave3e_run.sh（批跑入口，RUN=1/2 + gate 文件）、wave3e_rerun_gate.py（复跑门对账）、wave3e_summarize.py（五候选判定汇总）
Files:        tools/difftest/v2/{wave3e_lib.py,wave3e_preflight.py,wave3e_p4_ttl_workload.py,wave3e_run.sh,wave3e_rerun_gate.py,wave3e_summarize.py}、tools/difftest/v2/cases/wave3e_{p1_catalog_reverify,p3_bearer_matrix,p2_schema1_reverify,p5_tags_accept_reverify,p2b_d1_envelope_capture,residue_check}.py、reports/compatibility/L044-wave3-expansion-report.md、tools/difftest/v2/run/wave3-expansion/*（gitignored 证据）
Tests:        批跑 ×2（RUN=1 定谳 + RUN=2 复跑门）：p1 六臂双端 / p3 48 格 / p5 九臂 / p2 12 相位 —— 复跑门 RERUN GATE PASS（92 格逐臂结论两轮相等，零 flake）；p4 TTL 工况窗 1 个（9 步 + 竞速收口 exit 0）；p2b 信封专捕 1 臂；residue check 两轮 CLEAN。四态：PASS=定谳与复现类臂，FAIL=分歧成立（记录目的达成：D1/D2/D3 三臂 CHANGED），BLOCKED=0，NOT_RUN=0
Commands:     python3 -I tools/difftest/v2/wave3e_preflight.py；python3 -I tools/difftest/v2/wave3e_p4_ttl_workload.py（后台，T0 15:34:53→16:47 收口）；RUN=1/RUN=2 bash tools/difftest/v2/wave3e_run.sh；python3 -I tools/difftest/v2/wave3e_rerun_gate.py（→ PASS）；python3 -I tools/difftest/v2/wave3e_summarize.py；python3 -I tools/difftest/v2/cases/wave3e_p2b_d1_envelope_capture.py；全部真实 HTTP 双发（自研 client 仅作 L0/L1 密集断言，本波无真实客户端腿——五候选面均为协议语义取证，curl 等价）
Outputs:      reports/compatibility/L044-wave3-expansion-report.md；证据 tools/difftest/v2/run/wave3-expansion/{p0_preflight,p1_catalog_topology,p2_schema1_phase,p2b_d1_envelope,p3_bearer_matrix,p4_ttl_workload,p5_tags_accept_matrix,residue_check,rerun_gate,summary}.json + 各 *-run2.json；本地提交 181de5eb（工具链）+ a496fd3a（cases 路径修复）+ 本报告提交
Compatibility: 差异分类建议——候选③ catalog 六项 REPRODUCED；候选① bearer 七项 REPRODUCED（A 四面统一 Basic 24 格补全 / B F1,F4 Basic vs F2,F3 Bearer 分裂维持）；候选面② 406 规则 REPRODUCED；候选② schema1 A 侧 REPRODUCED 12/12、B 侧 PARTIAL：C-b 缓存污染在 dev-fdeabbf2 不复现（fixed-in-build，待 compatibility-engineer 改判）+ **新分歧 D-1（冷 s1 请求 404 MANIFEST_UNKNOWN stored:schema2，回归向翻转，UNKNOWN 待裁，疑似 d7bd58eb 副作用）**；候选面① TTL 模型修订（刷新=届满首请求 ∪ 冷 pull-through 即时，工况依赖实锤）。回归对照：fixed=1（C-b）/ 仍在=6（catalog 根面、匿名根面、B 跨面分裂、realm 字符串、Accept 406、TTL 盲窗）/ 新增=1（D-1）
Security:     匿名姿态三格双端复验（A 根面 401 vs B 200 枚举泄漏、A F1-F3 匿名 Bearer vs B 内容面匿名 200、A REST ping 匿名 200）——C-a 泄漏面与 B 开放面证据双轮固化；凭据零落盘（env 行首锚定、token 仅 len+sha256 前缀、一次性用户名防熔断）、A F1 empty-bearer 403 Forbidden（C-c）复现，零 recurrent 熔断污染
Performance:  批跑单轮 ~4.5 分钟（p3 面间静默纪律占大头）；p4 长跑 73 分钟收口；无超时 case；上游 zot 匿名 PUT 全部 201
Risks:        （1）执行中途外部 git clean 清空本 worktree 未跟踪文件（疑似并行会话；本会话零清理命令）——已重写+逐文件验证+提前本地提交防再发；（2）Write 落盘层曾静默丢失一行（首版 p4 缺 exec_module）——固化「写后 py_compile+特征行 grep」纪律；（3）TTL 工况实验单窗单样本，跨日相位未采样不外推；B 部署版本漂移（简报 dev-aceee772 vs 活体 dev-fdeabbf2）已按活体锚定；（4）A/B DELETE 后 GET 双端一致返回 400（L043 时 404）——联合漂移一句记录不立案
Blockers:     无（参照与 UAT 全程可用）
Next:         待 compatibility-engineer 裁定——D-1 分类（BUG vs INTENTIONAL 取舍，建议限期两程）+ C-b 改判（fixed-in-dev-fdeabbf2）+ known-divergence 五条目 evidence 追加（报告 §新分歧候选清单/§Z 建议已备）；B 侧 dev-d 后续波次建议复验 D-1 是否随修复迭代收敛
```

断点快照：无中断——批跑两轮 + TTL 长跑 + 信封专捕 + 复跑门 + 汇总全部收口，difftest 命名空间已清零（residue 双轮 CLEAN 在案），上游 GC 清单结转 6 新 tag（DELETE 405 所限）。
