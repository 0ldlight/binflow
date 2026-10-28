Ticket:       T-L043-wave2（wave-2：wave-1 unknown 臂收编 + A 侧熔断阈值精确计数，T-632 反哺）
Role:         differential-qa-engineer
Area:         tools/difftest/v2/ + reports/compatibility/（执行域 worktree wave-docker-remote，分支 claude/lzw-followups 未动）
Input:        conductor 派发三任务（A 侧熔断受控计数 / L042 unknown 清单可 REST 化臂 / L043 报告）；A=JFrog 7.161.26 :32082、B=BinFlow dev-cfa5da05 :32083、上游中继 :32100；L042 报告 unknown 清单 + _dockerlib/wave-1 工装纪律
Changes:      新增 6 用例文件（cases/docker_negative_cache_ttl.py、docker_catalog_empty_degrade.py、docker_star_inm.py、binflow_v2_prefix_face.py、docker_schema1_pull_block.py、docker_bearer_tamper_arms.py）+ 批跑入口 wave2_run.sh + 4 份熔断探针（wave2_threshold_probe{,2}.py、wave2_bside_breaker_probe.py、wave2_bside_admin_block_probe.py）及其 .sh 包装；用例轮内修复两处（上游 GET Accept 协商、catalog body 留存）
Files:        上列 tools/difftest/v2/**；reports/compatibility/L043-wave2-report.md；run/wave2/（定谳）+ run/wave2-2/（复跑门）；本日志
Tests:        批跑 6/6 执行：PASS 3（dcr-negative-cache-ttl、dcr-star-inm、binflow-v2-prefix-face）/ FAIL 3（dcr-catalog-empty-degrade、dcr-schema1-pull-block、dcr-bearer-tamper-arms，均为真分歧非用例 bug）/ BLOCKED 0；复跑门 run/wave2 vs run/wave2-2 同构零 flake；熔断实验 A 侧 9 次独立触发 + 5 用户静默剖面 + B 侧 2 轮活体
Commands:     bash tools/difftest/v2/wave2_run.sh run/wave2（×2 轮，第二轮落 run/wave2-2）；bash tools/difftest/v2/wave2_threshold_probe.sh；bash tools/difftest/v2/wave2_threshold_probe2.sh；bash tools/difftest/v2/wave2_bside_breaker_probe.sh；bash tools/difftest/v2/wave2_bside_admin_block_probe.sh；python3 runner.py --list；python3 -m py_compile cases/<6 新文件>；curl 夹具/残留复核（/tmp/wave2_preflight.sh、/tmp/wave2_catalog_inspect.sh、/tmp/wave2_residue_check.sh）
Outputs:      报告 reports/compatibility/L043-wave2-report.md；results.json run/wave2/；提金候选：dcr-star-inm 四臂绝对值（契约尚无 expect 钉值，A/B 已同姿可入金）、dcr-negative-cache-ttl 双端 404 逐字同体（含 +6min 复探）
Compatibility: 候选 7 项（bearer 失败形态 / schema1 降转换 / registry 级 catalog / remote 未知镜像 tags:list / 熔断正确凭据封锁内 200 vs 403 / 阈值线口径 3 vs 6 / 粘滞+窗口升级）；回归对照（vs wave-1）：fixed 0 / 仍在 0（C01b、C03 未重触）/ 新增 7；T-632 收敛建议：阈值 5→2（bind-next 语义不变即线口径 401,401,403），窗口 4s 维持，正确凭据放行与粘滞升级待裁定
Security:     auth 面 case 双端对照：坏凭据族（ping-reject 臂沿 wave-1）本波新增 bearer 篡改族五臂（状态层双端同 401、形态层分歧候选 ①）+ alg:none 伪 JWT 双端拒 401；熔断面参数空间全测（阈值/窗口/正确凭据/粘滞/升级/键控/跨面）；凭据全程 env 注入零落盘零打印，探针只读端点 + 一次性用户名，未动参照配置与实例既有数据
Performance:  批轮耗时 ~25s/轮（6 用例）；熔断探针 ~45-90s/轮；无超时 case（runner 默认 30s 未触顶）；负缓存 +6min 复探为唯一长间隔采样
Risks:        负缓存精确 TTL 等值未证（仅分钟粒度同级）；B 熔断粘滞/升级/跨面为码面推断未活体复测；A 阈值 M1（arm-on-trip, N=3）与 M2（arm-after-2 bind-next, W=4s）两模型对单发序列不可分辨，采 M2（与静默剖面及 wave-1「4 second(s)」相容）；上游匿名 PUSH 仍开放（wave-1 环境观察结转）；升级序列 3 样本，公式 6k-3 为假设非定谳
Blockers:     无（参照可用，金样未用）；真过期/吊销 bearer 臂维持环境性不可达（需参照侧 token 状态操控），非本波阻塞
Next:         compatibility-engineer 裁定 7 项候选（尤其熔断正确凭据行为的安全语义反向与阈值收敛值）；提金候选评审入 golden/；conductor 计量四问见报告口径节
