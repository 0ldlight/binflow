# T-L044-wave3 工作日志（differential-qa-engineer）

```
Ticket:       T-L044-wave3 — L043 候选深挖 + A 侧跨路由账本语义验证（wave-3）
Role:         differential-qa-engineer
Area:         tools/difftest/v2/ + reports/compatibility/（docker v2 remote 域）
Input:        L043 候选 ①③⑤（bearer 篡改族/catalog 空降级/schema1 缓存相位）+ T-632/T-635 反哺问题；
              模式 dual；A=JFrog 7.161.26 @192.168.120.36:32082、B=BinFlow dev-aceee772 @192.168.120.36:32083、
              上游中继 :32100；夹具 diff-docker-remote + smoke t1/t2 双端在位（preflight 4×200）
Changes:      新增 case 3（catalog 裸根拓扑三态 / schema1×缓存相位四向 / bearer 跨面矩阵 8 格）+
              A 侧作用域探针 + wave3 批跑入口 + preflight + evidence 摘要器
Files:        tools/difftest/v2/cases/docker_catalog_bare_root_topology.py
              tools/difftest/v2/cases/docker_schema1_cache_phase.py
              tools/difftest/v2/cases/docker_bearer_crossface_challenge.py
              tools/difftest/v2/wave3_run.sh / wave3_preflight.sh / wave3_summarize.py
              tools/difftest/v2/wave3_a_scope_probe.py / wave3_a_scope_probe.sh
              reports/compatibility/L044-wave3-report.md
              tools/difftest/v2/run/wave3/ + run/wave3-2/（gitignored 证据产物）
Tests:        批跑 3 case 全执行（0P/3F/0B/0N——FAIL=分歧记录型用例达成记录目的）；
              case 内检查项 22+27+16=65 项，失败项即分歧取证（3+6+7=16）；
              任务一探针 2 次独立运行 × 13 发全部落盘
Commands:     bash wave3_preflight.sh（双端健康 + 夹具 4×200）
              bash wave3_a_scope_probe.sh（×2，第二次自动换一次性用户名）
              bash wave3_run.sh run/wave3；bash wave3_run.sh run/wave3-2
              python3 -I wave3_summarize.py run/wave3{,-2}
              复跑门机器对照：nonce 无关化 checks+facts 逐字同构 → RERUN GATE: PASS
              机读门：python3 -I -c "import json; json.load(open('run/wave3/results.json'))" 两轮通过
Outputs:      reports/compatibility/L044-wave3-report.md（逐臂判定表 + 新候选 C-a/C-b/C-c + 四问）
              提金候选：无（本轮 3 case 全为分歧面，无双端一致的 golden-capture 对象；
              bearer ping 面 3 格逐字同但属 401 错误形态，非 golden 语义）
Compatibility: 差异分类建议统计——BUG 候选 0 / UNSUPPORTED 0 / UNKNOWN+待裁 6
              （C-a 匿名放行、C-b schema1 表示污染、C-c 空 bearer 403、候选②定谳改写、
              候选③不变面定性、候选①分歧面收敛——INTENTIONAL 恒 0，裁定归 compatibility-engineer）；
              回归对照：L043 候选 ①②③ 全部「仍在」（深化取证），无 fixed/恶化
Security:     auth 面 case 双端对照：bearer 矩阵 8 格（候选①完成）+ catalog 匿名态（C-a）+
              熔断作用域三事实（跨路由共享双端同构）；账本安全——bad-basic 格全部一次性
              不存在用户名（每格 1 发低于阈值）、bearer 格单发、admin 零误触（全矩阵 16 发
              零 recurrent 字样）；evidence 零凭据落盘
Performance:  批跑时长首轮 ~40s / 复跑 ~35s（含每轮 2 次上游补推）；无超时 case；
              探针运行 ~17s/次（1.15s 发距纪律）
Risks:        C-b 污染恢复窗口未测（TTL 届满是否自愈——下波候选）；
              A c1 转换来源 wire 层不可分辨（上游回包 vs 本地转换，单边上游不可观测）；
              上游残留 tag ×4 结转手动 GC（w3w/w3c ×2 轮，DELETE 405）；
              normalize 维持 raw（docker-remote 域未注册），B 尾换行方言按 N-1 注记不放宽
Blockers:     无
Next:         待 compatibility-engineer 裁定：C-a（安全语义反向——建议同时上会安全侧）、
              C-b（含 TTL 自愈实验立项建议）、C-c；契约修正案 2 项（remote-schema1-conversion
              expect 语义改写、registry-catalog-anonymous 新条目）；T-635 语义无需重审（任务一结论）
```

断点快照：无中断——任务一（2 次运行）、任务二（首轮+复跑门）、任务三（报告+提交）全部完成；difftest 命名空间已清理（双端临时仓 DELETE 200 ×2 轮 + p3 复验零残留），仅上游 4 个轮次唯一 tag 结转 GC 清单（上游 DELETE 405 既有口径）。
