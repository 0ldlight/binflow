# L045 — wave-1 六条 unknown 臂补齐批（wave1-arms 重放 + 匿名 catalog 补腿 + 负缓存 +6min 判别探针）

- **日期 / 模式**：2026-10-29 / **dual**（A=Artifactory 参照 192.168.120.36:32082 可用，全批双发；无降级）
- **任务源**：L042 报告 unknown 清单中可 REST 化的 6 臂补齐（conductor 派发）；产物目录 `tools/difftest/v2/run/wave1-arms{,-2}/`
- **上游替代（环境约束，沿 wave-1 口径）**：remote 仓统一指向集群内中继 registry `http://192.168.120.36:32100`（zot 系，docker schema2+OCI 严格白名单）；夹具 `difftest-docker/smoke`（t1/t2，确定性字节，manifest digest `sha256:9530196490…`）
- **用例资产**：6 臂用例已存在于 `tools/difftest/v2/cases/`（wave-2 提交 c0b7edf4 引入，本批**逐字未改**，保证与 wave-2 结果可回归对照）；本批新增 1 个补腿用例 + 2 个工具脚本（见工件节）。批跑入口 `wave1_arms_run.sh`（两次运行分别落 `run/wave1-arms/`、`run/wave1-arms-2/`）。

## 执行概况

- 批次两轮：`run/wave1-arms/` 与 `run/wave1-arms-2/`，逐 case 结论 **7/7 逐字同构，零 flake**（复跑门通过）。
- score：`{"pass":3,"fail":4,"blocked":0,"not_run":44,"total":51,"coverage":0.4286}`（两轮同值；分母=本批选取的 7 例）。
- 夹具自愈：`docker_wave_setup.sh` 幂等预检——remote 仓 `diff-docker-remote` 双端 GET 200（先前 wave 遗存，skip-create 未触碰），上游 t1/t2 HEAD 200，A 本地面 `difftest-docker` GET 200。
- 临时仓清理：catalog 用例 `diff-catalog-1a1872c04a3` / `diff-catalog-1a1872ea0aa` 双端 DELETE 200×2×2 轮，复验零残留。

## 逐臂判定表（case id ↔ 派发臂对账）

| 派发臂 | case id（layer） | run-1 | run-2 | wave-2 基线（L043） | 结论 |
|---|---|---|---|---|---|
| ① 负缓存 TTL 窗口 | dcr-negative-cache-ttl（L9） | **PASS** | **PASS** | PASS | 双端同姿（含即时重发 404 逐字同体，B 仅尾换行方言 N-1）；**+6min 判别探针发现新分歧，见候选 ⑥** |
| ② catalog 空列表降级 | dcr-catalog-empty-degrade（L5） | FAIL | FAIL | FAIL | 仍在：候选 ③④ 同检查级复现（arm2 shape / arm3 status） |
| ② 补腿（对匿名） | dcr-catalog-anonymous（L5，**本批新增**） | FAIL | FAIL | —（未覆盖） | **新分歧候选 ⑤**：A 401+挑战 vs B 200 开放 |
| ③ star INM 腿 | dcr-star-inm（L3） | PASS | PASS | PASS | 四臂状态逐臂相等（REST 化成功，非 BLOCKED） |
| ④ /binflow/v2/ 前缀面 | binflow-v2-prefix-face（L1） | PASS | PASS | PASS | B-only 三断言全绿（认证/匿名 404 无挑战、前缀下真 manifest 不服务）；A 同路径 404 仅记录 |
| ⑤ schema1 拉取阻断 | dcr-schema1-pull-block（L5） | FAIL | FAIL | FAIL | 仍在：候选 ② 同检查级复现（s1-only CT/media/config-digest） |
| ⑥ expired bearer 臂 | dcr-bearer-tamper-arms（L4） | FAIL | FAIL | FAIL | 仍在：候选 ① 同检查级复现（t1–t4 shape；t5 双端同 200）；真过期/吊销仍需参照侧 token 状态操控，维持环境性未覆盖 |

## 逐臂证据要点（全部可反向对到 `run/wave1-arms*/evidence/<case-id>/checks.json`）

1. **负缓存窗口（候选 ⑥ 载体）**：run-unique tag（nc1a1872c04b0 / nc1a1872ea0ae）→ 双端 404（body 逐字同体，B 尾 `\n`）→ 上游补推 201 → 即时重发**双端仍 404 逐字同体**。
   **+6min 判别探针（新工具 `wave1_arms_ttl6m_probe.py`，补 case 未含的长间隔腿）**：tag `nc1a1872c04b0` 于 **plant+6.7min** 复探（tag nonce 纪元 1793269171 → probe 纪元 1793269575，实测 403.6s）→ **A=200**（body sha256=`9530196490…`，与上游夹具 manifest 逐字节同——上游唯一持有方，A 只能经 fetch-through 获得，证明 A 负缓存行已失效）、**B=404**（MANIFEST_UNKNOWN，负缓存行仍持有）。**SPLIT，判别性成立**（上游已持有该 tag，故 404 只能来自行持有而非透传）。L043 曾报「+6min 复探双端仍 404」——该采样为会话内即时观察、无留存 evidence 工件，不可复算；本批两枚探针为首批证据留存的长间隔采样。
2. **catalog 空降级**：空 local 仓（双端 PUT 200）未知镜像 tags/list **双端同 404 errors 形**（SAME）；registry 级 `/v2/_catalog`（认证）A=200 **空 body 零 CT** vs B=200 JSON 带 `repositories` 条目（候选 ③）；remote 仓未知镜像 tags/list A=404 NAME_UNKNOWN vs B=200 `{"name","tags":null}`（候选 ④）。
3. **匿名腿（候选 ⑤，本批新发现）**：匿名 `/v2/_catalog`：**A=401** + `Bearer realm=…/v2/token` 挑战 + UNAUTHORIZED errors body；**B=200** `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}` 零挑战。匿名 remote tags/list：**A=401** 挑战 vs **B=200** 完整 tag 列表（含上游全部残留 nc*/w3*/t1/t2/t3）。安全相关：B 匿名暴露 registry 目录与 tag 命名空间，A 要求认证。两轮同态。
4. **star INM**：四臂（GET/HEAD manifest 存在、manifest 未知、blob 存在）状态逐臂相等（200/200、200/200、404/404、200/200），304 头面同姿（双端均未触发 304——绝对值记录供契约钉值，L043 提金候选沿用）。附带观察见 ⑥b。
5. **前缀面**：B `/binflow/v2/` 认证/匿名均 404 且无 docker 挑战，前缀下真 manifest 路径 404 不服务——B docker 面确不挂 `/binflow/v2/` 前缀（拓扑断言维持 wave-1 测绘）。
6. **schema1**：仅 schema1 Accept → **A 在线降转换**（200 CT=`…v1+prettyjws`）vs **B 原样回 schema2**（200 CT=`…v2+json`）；混合 Accept 双端同取 schema2（SAME）。依赖 schema1 的旧客户端在 B 上会破（候选 ② 维持）。
7. **bearer 篡改族**：t1 篡改尾/t2 截半/t3 alg:none 伪 JWT/t4 空 bearer 状态**全部双端同 401**，t5 push-scope 拉取双端同 200；形态层系统性分歧（A 分类文案 `errors[0]{status,message}` + `WWW-Authenticate: Basic realm="Artifactory Realm"` vs B 泛化 `authentication required` + `Bearer realm=…/v2/token` 挑战）（候选 ① 维持）。

## 新分歧候选清单（登 UNKNOWN，待 compatibility-engineer 裁定；INTENTIONAL 恒不在本域权限）

| # | 候选 | 来源 case | 状态层 | 一句证据 |
|---|---|---|---|---|
| ⑤ | **匿名 catalog/tags 面开放性**：A 401+Bearer 挑战 vs B 200 开放（registry 目录 + remote 仓 tag 命名空间匿名可见；安全相关） | dcr-catalog-anonymous | SPLIT（401 vs 200） | `run/wave1-arms/evidence/dcr-catalog-anonymous/checks.json`：B body `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}` 零挑战，A 401 UNAUTHORIZED。谱系：wave-3 `dcr-catalog-bare-root-topology` 的 P2 anon 为 record-only 未设门，本批新用例升为判定性 intercompare |
| ⑥ | **负缓存 TTL 窗口分裂（证据边界已钉）**：A 行未存活至 plant+6.7min（fetch-through 200，时间线排除 repo 混淆）vs B 同刻持有（404 负缓存形）；窗口等值未证，波间不一致（L043 无工件），机制归因需实例日志 | dcr-negative-cache-ttl + ttl6m 探针 | SPLIT（200 vs 404） | `run/wave1-arms/ttl6m_probe.json`：plant+6.7min，A 200 body sha256=夹具 manifest digest（fetch-through 铁证），B 404 MANIFEST_UNKNOWN；probe-2/迟采样污染废弃（见「复核结果」节） |
| ⑥b | **HEAD manifest Etag 头面**：A HEAD 无 Etag vs B HEAD 有（状态同 200；wave-2 evidence 已含、未上报；wave-1 manifest-get PASS 系「窄集+超集 tolerated」覆盖） | dcr-star-inm（head-manifest-exists 臂） | 同态（头面 SPLIT，未设门） | 四轮（wave2/wave2-2/wave1-arms/wave1-arms-2）A etag=none / B etag=`ad690e0a…` 稳定 |

**候选 ⑥ 复核探针（判别式重放）**：`run/wave1-arms/ttl6m_probe2.json` —— 见文末「复核结果」节。

## 回归对照（vs wave-2 末轮 run/wave2-2）

- **fixed 0**；**仍在 3**（候选 ①②③④ 对应 3 个 FAIL case，失败检查名逐字同构：bearer t1–t4 shape ×4、schema1 s1-only ×3、catalog arm2 shape + arm3 status ×2）；**新增 2**（候选 ⑤⑥；⑥b 为附带观察非 case 级）。3 个 PASS case 无回退。

## 提金候选（交 compatibility-engineer 评审）

- 本批**无新增**；沿用 L043 两条待评审项：dcr-star-inm 四臂绝对值（契约尚无 expect 钉值）、dcr-negative-cache-ttl 即时重发 404 逐字同体（+6min 部分因候选 ⑥ 未决暂缓入金）。

## Z 口径 / 契约提案（归 compatibility-engineer）

1. 新契约条目提案：`docker/anonymous-catalog-face`（候选 ⑤——registry 级与 repo 级匿名访问的挑战语义）。
2. 现有提案追加参数面：`docker/remote-schema1-conversion`、`docker/registry-catalog-bare-root`、`docker/remote-unknown-image-tags-list`、`docker/bearer-failure-shape`（L043 原案维持）。
3. 负缓存窗口契约（如立）：TTL 参数面需钉 A 实例有效窗口（本批观测 ≤6.7min 即失效，与 missedTTL≈1800s 假设不符——实例配置待查证，归 compatibility-engineer 向 conductor 提请），B 侧 ≥6.7min；带内精确等值需实例日志面（本域 REST 不可达）。

## 工件

- 用例（新增 1）：`tools/difftest/v2/cases/docker_catalog_anonymous.py`（dcr-catalog-anonymous，L5）
- 工具（新增 2）：`tools/difftest/v2/wave1_arms_run.sh`（批跑入口）、`tools/difftest/v2/wave1_arms_ttl6m_probe.py`（+6min 判别探针，--plant/--push-upstream/--wait-s）
- 产物：`run/wave1-arms/`（results.json + evidence×7 + ttl6m_probe.json + ttl6m_probe2.json + ttl6m_dryrun.json 控制流自检）、`run/wave1-arms-2/`（results.json + evidence×7）
- 6 臂既有用例本批零改动（回归对照有效性前提）

## 残留台账（difftest 命名空间纪律）

- 双端临时仓：两轮 `diff-catalog-*` 均 DELETE 200×2，零残留。
- 上游中继（DELETE 405 不可清，沿 wave 台账）：本会话新增 4 tag——`nc1a1872c04b0`（run-1）、`nc1a1872ea0ae`（run-2）、`ncp6ae32114dry`（探针 dryrun）、`ncp6ae321230002`（probe-2）；连同历史 wave-2/3 残留现共 **13 tag**（18:40 实测，`difftest-docker/smoke`），均 difftest- 命名空间内，不影响 `mirror/library/*`。
- 双端实例既有数据：remote 仓 `diff-docker-remote` 与 A 本地 `difftest-docker` 本批 skip-create 未触碰、未删除；**两仓已于 18:33–18:40 被并行会话（l042_reverify）的 wave 末清理删除**（非本批动作；后续批次跑批前须重跑 `docker_wave_setup.sh` 幂等重建）。

## 复核结果（探针 probe-2 与迟采样）

**结论先行：probe-1（+6.7min）的 SPLIT 在本批时间线内成立且无 repo 生命周期混淆；probe-2 与 +19.8min 迟采样被并行会话的夹具清理污染（repo 缺失型 404，非负缓存 404），按污染证据留存、不进判定。**

本批时间线（本地时间，epoch 对齐 run/ 目录 mtime）：

| 时刻 | 事件 | 佐证 |
|---|---|---|
| 18:19:31 | batch-1 植负缓存行 nc1a1872c04b0（双端 404）+ 上游补推 201 | run/wave1-arms evidence |
| 18:21–18:22 | batch-2 重放（7 case 同构） | run/wave1-arms-2 mtime 18:22:33 |
| 18:26:15 | **probe-1 复探（plant+6.7min）：A=200 夹具 manifest、B=404 MANIFEST_UNKNOWN** | run/wave1-arms/ttl6m_probe.json |
| 18:26:18 | 并行会话（l042_reverify，13 case 重放）开始跑批：其 setup 为 ensure_repo skip-create（GET 200 即跳过，**不删除不重建**） | run/reverify/ mtime 18:26:18 + 脚本 preflight/setup 逻辑 |
| 18:33:32 | probe-2 植行（双端 404，**repo 仍存活**）+ 上游补推 201 | ttl6m_probe2.json plant_status/push |
| 18:33:32–18:40:00 | **并行会话独立 cleanup 阶段删除双端 diff-docker-remote**（其脚本「cleanup is separate so a red batch can be re-probed before teardown」） | 本表前后两采样 shape 突变 |
| 18:40:00 | 迟采样（plant+19.8min）：A=404 `Repository 'diff-docker-remote' not found`、B=404 `NAME_UNKNOWN`——**repo 缺失型，非负缓存型** | ttl6m_probe1_late.json |
| 18:40:12 | probe-2 复探：同样 repo 缺失型 404 ×2——**判别力失效，废弃** | ttl6m_probe2.json |

**候选 ⑥ 的修订判定边界**：

- **A 侧**：负缓存行未存活至 plant+6.7min（probe-1 A=200 且 body=上游夹具 manifest 逐字节同）。probe-1 前无 repo 删除/重建事件（并行会话 18:26:18 才起跑且 setup 为 skip-create，其 cleanup 在 probe-2 窗口内才发生）——**repo 生命周期混淆已被时间线排除**；但「行过期」与「A 内部缓存逐出（堆压力等）」在 REST 面不可分辨，机制归因待实例日志。
- **B 侧**：行持有 ≥6.7min（probe-1 B=404 且 shape=MANIFEST_UNKNOWN 负缓存形——若行被冲刷应答 200，故行确实在）。
- **等值性**：A/B 窗口等值未证（本批 A≤6.7min vs B≥6.7min 已分裂）；与 L043「+6min 双端 404」（无留存工件）是否相容需静默窗口重测——A 侧行为可能随内部缓存压力时变。精确 TTL 等值归实例日志面。
- 上游中继全程持有该 tag（18:40 复验 200），排除上游侧丢失解释。

**环境协同风险（上报 conductor）**：两 agent 会话共享双端实例与 difftest 命名空间，并行会话的 wave 末清理在另一会话探针窗口内删除共享夹具仓 `diff-docker-remote`（18:33–18:40）。建议：长间隔探针/跨分钟批次互斥调度，或夹具清理前经 conductor 协调（本批产物未受损：全部 case 判定均在污染窗口前完成）。
