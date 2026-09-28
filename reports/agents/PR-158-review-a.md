# PR-158 Review A（correctness 形态）

- Ticket: PR #158（0ldlight/binflow，base=develop）R1 closeout: L029 re-verify closes D-1/D-2; matrix D12-R18; iteration-1540
- Role: code-reviewer (reviewer-a)
- Area: difftest harness / compatibility 文档（无产品码）
- 结论: **REQUEST_CHANGES**（blocking 2、non-blocking 3）

## 结论

REQUEST_CHANGES。矩阵计数、四问口径、台账核算、上游 sha256、r5/r6/r7 证据链全部独立复核通过；但两条必须修改：① iteration-1540.md 出现凭据对字面量写法；② L029 一处「r5 已翻绿」与自家引用的 r5 wire 证据矛盾。

## 检查项与结果

### 1. tools/difftest/v2/cases/maven_resolve_remote_cache.py — 通过
- 豁免仅 B 腿：`if side == "b"` 守卫（:66-73），A 腿建仓 payload 不变——复核通过。
- 注释如实：clash TUN fake-ip / NFR-S13 / 一等旋钮表述与产品码吻合——`allowPrivateUpstream` 确为真实旋钮（internal/repo/config.go:83,232、internal/httpapi/repositories.go:113、internal/remote/ssrfguard_test.go），REST PUT 可携带该字段，case 写法正确。
- 非放宽产品行为：本 PR 零产品码；产品默认 SSRF 姿态不变；断言面未削弱（仍要求 200 + 上游逐字节 sha）。独立验证：本机直拉 repo1.maven.org 的 javax.annotation-api-1.3.2.pom，sha256 = `46a4a251ca406e78e4853d7a2bae83282844a4992851439ee9a1f23716f06b97`，与 case 常量及 r6/r7 双腿证据一致（豁免后 B 腿拿到的确是真实上游字节）。

### 2. reports/compatibility/L029-maven-v2-batch1-reverify.md — 一处证据链矛盾（blocking B2）
- r5/r6/r7：run/maven-batch1-r5 results = 4 PASS + maven-resolve-remote-cache FAIL（b_spec_violations: cache_projection 404 / first+second_resolve 400 sha d05e3e0a67c4c5d7…）；r6=r7 全 5 case PASS 且逐项一致——与 L029 表格及「稳定性判据 r6=r7」吻合。
- sha256 链：400 体 sha d05e3e0a… 在 r1 与 r5 证据同值（「L028 时即是此因」成立）；46a4a251… 已独立实测验证。400 体归因修正（SSRF × fake-ip ULA，非解析缺失）逻辑自洽。
- 无凭据值落盘（该文件用「凭据经环境变量注入，未落盘」合规表述）。
- **矛盾点 :25**：「F1 缓存投影缺失…T-529/T-530（PR #156）实现后 **r5 已翻绿**」——r5 results.json 里 `cache_projection_artifact` b=status=**404**（FAIL），投影翻绿发生在 r6；与本文表格注「r6 起翻绿」自相矛盾。归因修正章节自身引入误述，证据文档不可带病入册。

### 3. docs/compatibility/matrix.yaml — 通过（计数逐项重算全对）
python3 + yaml 重算 rows 实际计数 vs summary：total 201；state 95/17/57/20/12；confidence high 141/medium 60/low 0；priority 58/63/80；by_state_x_priority compatible P0 56/P1 37/P2 2、partial 2/10/5、absent P1 16/P2 41、not_applicable P2 20、superset P2 12；D12 18/9/1/7/1；contract_ref 37、golden_ref 1、last_difftest 51——**全部与 summary 块一致**；无重复 row_id。diff 仅增量：header 注记 + summary 计数 + 单行 D12-R18 插入，冻结行集零改动。「195 冻结行集」为 L005 起沿用口径。D12-R18 字段完整（state/confidence/last_difftest/priority_class P1=缺省策略/layer A=有效枚举）。

### 4. reports/iteration-1540.md — 四问口径全对；一处红线（blocking B1）
- Y = 95 + 0.5×17 + 0.5×12 = 109.5 ✓；coverage = 109.5/(201−20) = 109.5/181 = 60.50% ✓；上轮 108.5/180 = 60.28% ✓；与 L029、matrix 三方数字一致。
- Z=33 台账实算闭合：known-divergence.yaml 68 条 = 31 resolved（有 resolved dict）+ 4 gated-resolved（逐条指认：docker/remote-url-v2-suffix-silent-404、npm/login-idrev-npm-blind、goproxy/list-timestamp-column、conan/v2-ping-superset，gate 均含 resolved 裁定）+ 33 open（BUG 4/INTENTIONAL 1/UNSUPPORTED 1/UNKNOWN 27）✓；N=27=UNKNOWN ✓。
- 诚实失败清单在场（§四：adapter 爆炸半径、replication 同族、D-1 误诊一轮、文件名返工）✓。
- **红线 :5**：「B1 双端 <凭据对字面量>」——凭据对写法落仓文件。红线口径：落仓文件只允许「凭据经环境变量注入」类描述（L029 自己就是这么写的；runner 实为 A/B_PASSWORD 环境注入）。此处描述的是含 7.161.26 活体参照实例在内的双端凭据，即便按最宽读法（BinFlow scratch 文档默认口令，agent 日志有先例）也不符合红线明文。一字级修复：改「B1 双端凭据到位（环境变量注入）」。（conductor 落盘本报告时已将引文中的凭据对掩码——红线对评审报告同样生效。）

## 必须修改（blocking）

1. `reports/iteration-1540.md:5` 凭据对字面量（admin/口令明文）→ 改为「B1 双端凭据到位（经环境变量注入）」（或等价描述，不留凭据形态）。
2. `reports/compatibility/L029-maven-v2-batch1-reverify.md:25` 「…实现后 r5 已翻绿」与 r5 wire 证据（cache_projection 404 FAIL）矛盾 → 改「r6 起翻绿」或「r5 手工豁免验证后确认、自动腿 r6 翻绿」，与 :19 表注对齐。

## 建议改进（non-blocking）

1. run/ 证据树整体 gitignore（宿主本地）——L028 同款既有实践，但 L029 引用的 r5/r6/r7 无法从仓库单独复现；建议关键 results.json/证据快照择要入册或在报告注明宿主。
2. 400 错误体（private_ula fdfe:dcba:9876::22）仅存于手工复现转述，run/ 无原始体；sha d05e3e0a 链接已可接受，建议手工复现输出落 run/…/evidence 加固。
3. matrix header 注记行较长，可读性欠佳（不影响正确性）。

## 命令与输出（取证原文摘录）

- `gh pr diff 158`：4 文件、3 commits，与派发一致（本地 worktree diff 73 文件系本地 develop ref 落后，已改用 origin/develop...HEAD 复核 = 4 文件）。
- 矩阵重算（python3 yaml）：`total rows: 201 summary total_rows: 201 / state: {compatible 95, partial 17, absent 57, not_applicable 20, superset 12} / conf: {high 141, medium 60} / prio: {P0 58, P1 63, P2 80} / compatible {P0 56, P1 37, P2 2} / D12 rows 18 {compatible 9, partial 1, absent 7, not_applicable 1} / contract_ref 37 / golden_ref 1 / last_difftest 51 / dup row_ids: []`——与 summary 块逐项相等。
- `run/maven-batch1-r5/results.json`：`maven-resolve-remote-cache FAIL … cache_projection_artifact: status=404, first_resolve: status=400 sha=d05e3e0a67c4c5d7, second_resolve: status=400 sha=d05e3e0a…`；r6/r7：5×PASS 且 r6==r7 逐项一致。
- 上游独立验证：`curl https://repo1.maven.org/.../javax.annotation-api-1.3.2.pom | shasum -a 256` = `46a4a251…f06b97`（与 case UPSTREAM_SHA256 逐字节一致）。
- 凭据扫描：`grep -nE 'password|JFrog@|secret|token'` 四文件 → 唯一命中 iteration-1540.md:5；L029/case/matrix 干净（matrix 命中均为能力描述词 token/credential 面）。
- 台账：`python3` 解析 known-divergence.yaml → 68 entries；resolved(dict)=31；unresolved=37 {INTENTIONAL 5, BUG 4, UNKNOWN 27, UNSUPPORTED 1}；其中 4 条 gate 含 resolved 裁定 → 33 open 与战报吻合。
- 旋钮：`grep -rn allowPrivateUpstream internal/` → repo/config.go:83,232、httpapi/repositories.go:113、remote/ssrfguard_test.go 等（真实一等旋钮）。

## 残留 / Risks

- clean-room：不适用（本 PR 零产品码，纯 difftest case + 文档）。
- 双审：本票不落入六关键域（无 storage/security/protocol 产品码变更），单 A 实例充分；B 形态（契约一致性）无强制需求。
- 范围外发现：无（run/ 证据不入库为既有实践，记 non-blocking 不上报转票）。
- security：SSRF 豁免仅测试仓配置、产品默认姿态不变、豁免后字节仍强校验——攻击面无增量。
- 两条 blocking 均为一行级文字修复，修复后可直接重审放行（其余全部验证项已 PASS）。
