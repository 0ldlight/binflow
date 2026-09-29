# R7-followup-probes — differential-qa-engineer 工作日志

```
Ticket:       R7 跟进探针批（T-564 followup ①②；probe-only：不改产品码、不 commit、不翻台账、不写 Linear）
Role:         differential-qa-engineer
Area:         tools/difftest/v2/cases/ + reports/compatibility/
Input:        followup ① mimeType 归属观察（T-564 日志 Followups ①）+ ② 六族（pypi/cargo/deb/nuget/rpm/helm）自引用渲染点；
              模式 dual（A=Artifactory 7.161.26 活体 /artifactory 前缀；B=worktree HEAD 943ce899 scratch 127.0.0.1:18083 /binflow 前缀）；
              凭据 /tmp/r3-difftest.env env 注入零落盘（报告与命令日志无凭据）
Changes:      无产品码。新增 case 1 个：tools/difftest/v2/cases/generic_mime_ownership.py（18 腿 CT×扩展名×内容矩阵，
              curl 子进程精确 CT 控制 + netrc 0600 凭据、三面取证 PUT/GET/FileInfo）；探针脚本与夹具在 /tmp/binflow-r7-audit/（不入库）
Files:        新增 tools/difftest/v2/cases/generic_mime_ownership.py；新增 reports/compatibility/L036-r7-followup-probes.md；本日志。
              未 commit（probe-only；工作树产物待 conductor 处置）
Tests:        ①case 两轮均 PASS（probe posture=矩阵完整），18 腿：一致 7 / 差异 11（两轮同集合，复跑门绿）；
              扩展名表 15 腿：12 差异 / 3 同值；②六族审计腿：live 双端 PASS（pypi 四面、deb/rpm/helm Location 面、helm 真客户端、
              nuget 三面）、A-live/B-static（nuget index、五族裸 PUT 推断）、BLOCKED×6（见 Blockers）、NOT_RUN 如实在报告
Commands:     go build -o /tmp/binflow-r7-audit/binflow-server ./cmd/binflow-server（HEAD 943ce899）；
              ./binflow-server serve -c /tmp/binflow-r7-audit/binflow.yaml（18083，/binflow 前缀，数据 /tmp/binflow-r7-audit/data）；
              A_BASE=…8082/artifactory B_BASE=…18083/binflow python3 tools/difftest/v2/runner.py --out …/l036-r7fp{,-r1} --case generic-mime-ownership；
              curl 双面 wire 腿（六族建仓/PUT/GET/FileInfo/config.json/simple/index.yaml；twine 形 multipart POST；nuget multipart push）；
              helm 真客户端腿：helm repo add + repo update + helm pull（exit 失败为取证目的）
Outputs:      reports/compatibility/L036-r7-followup-probes.md（①mimeType ②渲染点审计两节，四态+raw）；
              case 证据 run/l036-r7fp{,-r1}/evidence/generic-mime-ownership/（gitignore 留存）；原始探针 /tmp/binflow-r7-audit/probes/*.txt；
              提金候选：无（本批为分歧取证，A 侧通过面均与既有已修/已裁面重叠；helm local:// 为 A 侧自 broken 形态，不入金）
Compatibility: ①归属规则定性：A=扩展名白名单表+octet-stream 兜底、声明 CT 全忽略、无 sniff；B=声明 verbatim 存储（11 腿 DIFF）
              + 表取值 12/15 DIFF（含 B stdlib 回退宿主依赖风险）→ UNKNOWN 待裁（对齐 or 登记分歧）；
              ②BUG 候选 5 组：deb/rpm/helm deploy-201 Location 裸相对（BUG）、nuget bare-PUT Location（BUG）、nuget v3 push 多渲染
              Location+锚点错叠（BUG/nuget 域裁）、pypi 上传响应缺 Location+X-Checksum-Sha256（BUG 候选或 UNKNOWN）、
              cargo 裸 PUT Location（推断级 BUG 候选）；回归对照：T-563/T-564 已修面（generic/maven Location/uri）本轮未回归
Security:     pypi/nuget/cargo 面未涉 auth 边界差异（双端 basic 同构 200/201）；凭据全程 env+netrc(0600) 注入，报告零凭据；
              A 实例配置红线未碰（cargo Custom Base URL 门未改，如实记 BLOCKED）
Performance:  全批 wall ~20min；无超时腿；B scratch 单实例常驻（健康面 /healthz 200）
Risks:        ①B stdlib mime 回退宿主漂移（darwin vs linux 的 .pdf/.svg 取值）；②五族 B 活体因 license 门仅静态证据（推断已标注）；
              ③A 无 Custom Base URL 的降级怪（localhost:8081/local://）不可在红线内升级复验；④helm index urls 的 A 绝对形
              （配 Base URL 后）不可证
Blockers:     cargo A 面活体 BLOCKED（A 实例 Custom Base URL 未配+配置红线）；cargo/deb/nuget/rpm/helm B 面活体 BLOCKED
              （scratch community<pro license 门——体系级：五族差分需 pro scratch 或 UAT 路径，上报 conductor）
Next:         待 conductor 裁：①mimeType 对齐方向（波及全 adapter mime 决策）or 登记分歧；②五组 BUG 候选立票（R8 池）；
              ③B stdlib mime 回退表扩充提案（compatibility-engineer）；④license 门解法（pro scratch / UAT）
```

## 断点快照

无中断。双面 `difftest-r7fp-*` 残留 0（A/B 各核验一次）；B 实例已停（18083 释放，graceful stop 日志在案）；
/tmp/binflow-r7-audit/{data,probes,fixtures,*.sh,*.py,binflow.yaml} 留存备复查；**未 commit**（case+报告+本日志在工作树待 conductor 处置）。
