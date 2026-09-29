# T-562 stage 1 — plain-SNAPSHOT walk 面取证 + auto-materialize 副探（BIN-44）

```
Ticket:       T-562（Linear BIN-44）stage 1：plain-SNAPSHOT→最新时间戳 resolve 的 walk 面
              双端活体取证补全 + auto-materialize 副探（只取证、写报告；不改产品码、不 commit、不翻台账）
Role:         differential-qa-engineer
Area:         tools/difftest/v2（新增 5 case）+ reports/compatibility/L035-r7-walk-forensics.md
Input:        L034-r6 Arm 7 forensics（F1-F6 规格输入）+ known-divergence 两条
              （maven/plain-snapshot-path-resolve-404 walk 族红半边、
              maven/deploy-put-version-metadata-auto-materialize UNKNOWN）+ 契约
              maven-virtual.yaml ⑨ + fence 测试 plain_snapshot_resolve_test.go
              TestPlainSnapshotUniqueHomeBoundary；模式 dual（A=7.161.26 @192.168.120.38:8082/artifactory
              活体复验；B=worktree HEAD 0c17635a 自建 scratch @127.0.0.1:18080 /binflow）
Changes:      无产品码。新增取证 case 5：maven_walk_trigger_matrix（W1 触发矩阵 t1-t10）、
              maven_walk_selection_tiebreak（W2 选择规则 s1-s5）、maven_walk_sameface_mvn
              （W3 字节同一性/验证器回显/真实 mvn deploy 构造 + c4）、maven_auto_materialize_probe
              （副探 m0-m6）、maven_virtual_walk_selection（W4 跨成员择取键 v1-v3）；批跑入口 /tmp/l035-run.sh
Files:        reports/compatibility/L035-r7-walk-forensics.md（主报告）、reports/agents/T-562-stage1-forensics.md
              （本文件）、tools/difftest/v2/cases/{maven_walk_trigger_matrix,maven_walk_selection_tiebreak,
              maven_walk_sameface_mvn,maven_auto_materialize_probe,maven_virtual_walk_selection}.py（新）、
              tools/difftest/v2/run/l035-r7-r{1,2,3,4,5}/（证据）
Tests:        有效两轮断言值逐项相同（r2≡r3 主批 + r4≡r5 virtual 腿）：case 级 PASS 1
              （auto-materialize-probe——读路径双端全对齐）/ FAIL 4（取证腿预期分歧=登记 fence 面，
              A oracle 全维取到）/ BLOCKED 0。作废轮如实记录：r1 tiebreak（case 路径 bug 409）、
              首版 r4/r5 virtual（scenario 复用仓致目录污染）——修后重跑为有效轮
Commands:     go -C <worktree> build -o /tmp/binflow-r7-walk/binflow-server ./cmd/binflow-server（HEAD 0c17635a）；
              scratch serve -c /tmp/binflow-r7-walk/binflow.yaml（127.0.0.1:18080）；
              bash /tmp/l035-run.sh r{1,2,3}（全批）+ r{4,5}（virtual 腿）；mvn 3.9 deploy:deploy-file
              真实客户端腿（A 经内存 forward-proxy exit=0、B 直连 exit=0）；手工剖析 curl/python 探针
              8 形（virtual 择取键变量隔离）；稳定性核验脚本输出 ALL_STABLE True
Outputs:      L035-r7-walk-forensics.md（逐臂矩阵 + stage 2 实施面建议 5 条 + 台账修订建议）；
              提金候选 4 组（W1 触发矩阵/W2 规则八景/W4 三场景/auto-materialize A 腿）
Compatibility: walk 族选择规则定谳：成员内=max(文件名 ts, bn 数值)、(artifact,baseRev,classifier,ext)
              四元组分族、与 mtime/metadata 指针解耦；virtual 跨成员=存储 mtime（最后落库者胜，
              13/13 观察；全局 filename-ts 与声明序被专设场景否证）；timestamp 拼写臂与 walk 同面
              （mvn 构造复证，并票成立）；t8 新亚面（非 SNAPSHOT 拼写：A 404 vs B 400 parse 门）；
              auto-materialize：物化层级勘误 version→module 级、release version 级双端 404@30s、
              SNAPSHOT version 级双端同步物化（L033 30s 注记否证）、残余分歧=module 级物理节点/计数面
              （对齐方向维持 UNKNOWN 待裁）。回归对照：L034 walk 族红半边维持（非恶化，本批喂满规格）
Security:     auth 面无新 case（本票不触）；凭据全程 env→内存（/tmp/r3-difftest.env chmod 600，
              报告/证据/命令日志零凭据字面量）；A 面写操作严格限定 difftest-l035-* 且测毕全删（复验零残留）
Performance:  批次耗时 r2/r3 各约 6 分钟（auto-materialize case 轮询预算 30/30/30/20/60s 固定不放宽）；
              无超时 case；B scratch 请求延迟无异常
Evidence:     run/l035-r7-r{2,3,4,5}/evidence/<case>/{a,b}-leg.json（asserts+raw：Location/目录清单/
              metadata 全文/PUT 状态/ETag/逐场景服务体标识）；r1 保留作 shakedown 记录（tie 四臂无效已注明）
Status:       stage 1 done（取证完备）；stage 2 规格输入四要素齐备（触发全集/选择规则/virtual 双层键/
              parse 门亚面），修复票与翻面测试锚（TestPlainSnapshotUniqueHomeBoundary L91-93）已在报告
Followups:    ①stage 2 实施票（resolve 点位 handleGet/serveSidecar、virtual mtime 择取、t8 404 化待裁）；
              ②auto-materialize 台账 surface 修订 + 对齐方向裁定（conductor）；③提金 4 组评审；
              ④NOT_RUN 面（remote/cache 投影 walk、.sha256 旁车、plain DELETE、覆盖 PUT 重算）留后续
Lessons:      取证 case 三处自坑三修：layout-GAV 门对缺 version 目录的路径 409（A 从布局推 GAV）；
              scenario 复用仓=目录污染（forensics case 必须 fresh keyspace per scenario）；
              单 served-body 标识在多候选残留下失效——body 标识 + fresh 构造缺一不可
```

## 断点快照

无中断。双面 difftest-l035-* 残留 0；B 实例已停（18080 释放）；/tmp/binflow-r7-walk/data 与 /tmp/l035-run.sh 留存；未 commit。
