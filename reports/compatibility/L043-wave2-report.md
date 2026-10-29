# L043 — 差分 wave-2：wave-1 unknown 臂收编 + A 侧熔断阈值精确计数（T-632 反哺）

- 日期：2026-10-28；模式 **dual**（A/B 双发，v2 框架批跑；熔断计数实验为 A 侧受控单边 + B 侧活体对照）
- **执行域**：worktree `wave-docker-remote`（未动 git 分支；新增用例与报告本地提交）
- **A 面**：JFrog Artifactory **7.161.26** @ `http://192.168.120.36:32082`（docker v2 面根路径，REST `/artifactory/api/*`；凭据 gitignored `.env.ref` 行首锚定注入，零落盘零打印）
- **B 面**：BinFlow **dev-cfa5da05** @ `http://192.168.120.36:32083`（含 T-632 实现与 T-633 httpapi 接线——本波首次对 B 熔断面做活体测量）
- 夹具活体：双端 remote 仓 `diff-docker-remote` + 上游中继 `:32100`（zot 系）镜像 `difftest-docker/smoke`（t1/t2），HEAD 200 双端复验在案。

---

## 任务一：A 侧熔断阈值精确计数（三事实 + 范围事实）

**方法**：一次性用户名（`threshold-count-20261028`、`w2-silence-*`、`w2-sticky-*`、`w2-escal-*` 各自独立），坏 Basic 凭据打只读端点（REST `GET /artifactory/api/system/ping` 为主、docker `GET /v2/` 交叉验证），发间 ≥1.05-1.2s；不创建用户、不改参照配置、不动实例既有数据。三轮探针：`wave2_threshold_probe{,2}.py`，证据 `run/wave2/threshold_experiment{,2}.json`（9 次独立触发全部落盘：状态/倒计时/延迟/体片段，无凭据）。

### 三事实

**事实 1 — 精确阈值（线口径）：第 3 发拒绝。** 9/9 次独立触发（1 无名用户 + admin + 5 静默剖面用户 + 2 语义腿用户）逐一复现 `401, 401, 403`——坏凭据第 1、2 发得 401，**第 3 发起返回 403** 熔断文案。wave-1 的「约第 2-3 发」收敛为定数：线口径 N=3。

**事实 2 — 窗口行为：4s，倒计时随时钟衰减，持续探测不续期。**
- 触发响应渲染「please try again in **3** second(s)」（9/9 一致）——注意是 3 不是 4：与发间 ≥1.1s 间隔联读，武断点在第 2 次失败、封锁自第 3 发起绑定（bind-next 形态，与 B 的 recurrent.go 同构），第 3 发到达时窗口已耗 ~1.1s，`ceil(剩余)` = 3。wave-1 观测的「4 second(s)」与此相容（当时发距更短）。
- 静默剖面（触发后静默 D 秒再探一次，D=1..5，5 个独立用户）：D=1 → 403 cd=2；D=2 → 403 cd=1；**D=3 → 401**；D=4/5 → 401。窗口 ≈ 触发+3s 内解除，倒计时严格随时钟衰减（9→7→5→3→1，2s 步进精确 -2）→ 与 wave-1「4 second(s)」文案与「发距更短时渲染 4」联立，**窗口 = 4s**（静默剖面把 W 夹在 (3.15, 4.15) 开区间，取 4s 与全部观测相容）。
- **封锁期内继续打坏凭据不续期**（连续 5 发 403 倒计时只减不增）——封锁只按首次武装时刻计时。
- **快速再犯窗口升级**：窗口届满后 1 发坏凭据即再武装（见事实 3 的 sticky 计数），第二窗口 ~9-10s（cd=8/9），第三窗口 ~15-16s（cd=15）——升级序列 4→9-10→15-16，三次独立观测（`R2/R3`、`L2`、`L3`）。

**事实 3 — 封锁内正确凭据行为：放行（200）。** admin 用户名自身被封锁期间，**正确 admin 凭据 → 200**（封锁武装后 ~0.3s 内即验）；恢复轮询第 1 发即 200。A 的熔断**只在认证失败的回答上显形**（403 替代 401），不拦成功认证——即失败率限流器形态，非 pre-auth 闸门。

### 范围事实（附带测得）

- **键控 = 用户名**：U1 被封锁期间，另一无名用户坏凭据 → 401（不受牵连）；admin 正确凭据 → 200。
- **计数跨面共享**：REST 面触发后，同一用户名打 docker 根面 `/v2/` → **403 同倒计时**（封锁跨 REST/docker 两面共享）。
- **届满后计数粘滞**：窗口届满后的第 1 发坏凭据仍得 401（武装绑定次发起），第 2 发即 403——**不是新计数**（若清零需再攒 2 发）。计数器跨封锁存活。
- **scope 不校验（同ActionCode）**：bearer t5 臂双端均不校验 token action scope（push-only token 拉 manifest 双端 200）。

### B 侧活体对照（dev-cfa5da05，T-632/T-633 已接线）

`wave2_bside_breaker_probe.py` + `wave2_bside_admin_block_probe.py`，证据 `run/wave2/threshold_experiment_bside{,_admin}.json`：

| 维度 | A（7.161.26 活体） | B（dev-cfa5da05 活体） | 判读 |
|---|---|---|---|
| 线口径拒绝点 | 第 **3** 发坏凭据（401,401,403） | 第 **6** 发（401×5,403） | **DIVERGENT**（常数差） |
| 窗口 | 4s | 4s（cd 3→2→1 衰减） | SAME |
| 封锁内**自身**正确凭据 | **200 放行** | **403 拒绝**（pre-auth 闸门，先于凭据验证） | **DIVERGENT** |
| 恢复 | 窗口届满即自愈 | 窗口届满自愈（轮询第 4 发 200） | SAME |
| 线文案 | verbatim 熔断句 + Jackson pretty dialect | 逐字同（含 `(s)` 拼写与 ` : ` 方言） | SAME |
| 届满后计数 | 粘滞（1 发再武装）+ 窗口升级 4→9-10→15-16 | fresh 计数 + 固定 4s（recurrent.go `blockedRemaining` 删项、无升级逻辑） | **DIVERGENT**（A 实测；B 为码面陈述未活体复测） |

### T-632 收敛建议（本票不改码，出数即可）

1. **阈值常数**：B 现值 5（bind-next 语义，线口径第 6 发拒绝）；A 线口径第 3 发拒绝。**收敛值 = 2**（5→2，bind-next 不变即得 401,401,403）。若改用 arm-on-trip 语义则常数 = 3，但需同时改武装形态——**取 2 最小 diff**。
2. **正确凭据封锁内放行**（A=200 vs B=403）：B 现实现比 A 更保守（防爆破锁死真实用户是安全增益），但与参照活体相悖。对齐与否是**安全语义裁定**，归 compatibility-engineer/安全侧；若裁对齐，改法是封锁检查挪到认证失败路径。
3. **粘滞计数 + 窗口升级**：A 实测存在（4→9-10→15-16），B 未实现。登记为分歧候选，是否对齐（含升级公式 6k-3 假设的验证）待裁定。
4. B 现线文案/窗口/衰减/pretty 方言已与 A 对齐，**无需改动**。

---

## 任务二：wave-1 unknown 臂收编（6 用例，dual 四态）

批跑 `wave2_run.sh` → `run/wave2/`（定谳）+ `run/wave2-2/`（复跑门）；**复跑门：连续两轮 3P/3F 同构、首失败原因逐字一致，零 flake**。score：`{"pass":3,"fail":3,"blocked":0,"not_run":41,"total":47,"coverage":0.5}`（47 = 41 存量 + 6 新增；本波筛选 6 例全执行）。

| case（cases/ 新文件） | 收编的 L042 unknown | verdict | 判定依据（evidence `run/wave2/evidence/<case>/checks.json`） |
|---|---|---|---|
| `docker_negative_cache_ttl.py`（dcr-negative-cache-ttl，L9） | negative-cache TTL 窗口臂 + 过期窗重验证 | **PASS** | 上游可见迁移法：未知 tag 双端 404 → 上游补推该 tag（201）→ 立即重取**双端仍 404 逐字同体**（B 侧仅尾换行方言 N-1）；+6min 复探双端仍 404——B 侧负缓存窗口与 A 同级（≥分钟粒度；精确 TTL 等值未证，见 Risks） |
| `docker_catalog_empty_degrade.py`（dcr-catalog-empty-degrade，L5） | catalog 空列表降级臂 | **FAIL** | 三个臂：空本地仓 tags/list 未知镜像**双端同 404 errors 形**（SAME）；**registry 级 `/v2/_catalog` 分歧**（A 200 空 body 无 CT vs B 200 JSON 带条目，见候选 ③）；**remote 仓未知镜像 tags/list 分歧**（A 404 NAME_UNKNOWN vs B 200 `{"name","tags":null}`，见候选 ④）。建仓 200×2、清理 DELETE 200×2 零残留 |
| `docker_star_inm.py`（dcr-star-inm，L3） | star INM 腿 | **PASS** | `If-None-Match: *` 四臂（manifest GET/HEAD 存在、manifest 未知、blob 存在）双端状态逐臂相等、304 头面同姿；绝对值仅记录（契约尚无 expect 块钉值） |
| `binflow_v2_prefix_face.py`（binflow-v2-prefix-face，L1） | `/binflow/v2/` 前缀面确认 | **PASS** | B-only 断言（A 侧 record-only）：`/binflow/v2/` 认证/匿名均 404 无 docker 挑战、前缀下真 manifest 404 不服务——B docker 面确不挂 `/binflow/v2/` 前缀，wave-1 拓扑测绘维持 |
| `docker_schema1_pull_block.py`（dcr-schema1-pull-block，L5） | schema1 拉取阻断 | **FAIL** | **A 做 schema2→schema1 在线降转换**（Accept 仅 schema1 → 200 CT=prettyjws），B 不转换（200 CT=schema2 原样）；混合 Accept 双端同取 schema2（SAME）。见候选 ② |
| `docker_bearer_tamper_arms.py`（dcr-bearer-tamper-arms，L4） | expired/revoked bearer 臂（REST 可化子集） | **FAIL** | 五臂（篡改尾/截半/alg:none 伪 JWT/空 bearer/push-scope 拉）状态**全部双端同 401**（t5 同 200），但**错误形态系统性分歧**（A 分类文案 + Basic 挑战 vs B 泛化文案 + Bearer 挑战），见候选 ①。真过期/吊销仍需参照侧 token 状态操控，维持未覆盖 |

用例侧 bug 防假发现（L042 纪律）：首轮 negative-cache FAIL 系上游 GET 未带 Accept 头（zot 严格协商致 CT 错配 400）——修用例后 PASS；首轮 catalog A 侧 body 未留存，补采后定谳。无假发现入账。

## 新分歧候选清单（不定性，待裁）

1. **bearer 失败形态**（dcr-bearer-tamper-arms，状态层 SAME）：无效/篡改 bearer 双端均 401，但 A = `errors[0]{status:401, message:分类文案}`（"Token failed verification: signature" / "Props Authentication Token not found" / "Authentication is required"）+ `WWW-Authenticate: Basic realm="Artifactory Realm"`；B = `{code,detail,message:"authentication required"}`（无 status 键）+ `WWW-Authenticate: Bearer realm=…/v2/token`。挑战机制与文案粒度双分歧。注：B 在 `/v2/` ping 面答 Props 文案（wave-1 P2 PASS），manifest 面答泛化文案——B 自身跨面不一。
2. **schema1 单独协商**（dcr-schema1-pull-block）：A 在线降转换出 prettyjws，B 无视 schema1 Accept 原样回 schema2。依赖 schema1 的旧客户端拉取在 B 上会破。
3. **registry 级 `/v2/_catalog`**（dcr-catalog-empty-degrade arm2，新开面——wave-1 只比过 repo 限定路径）：A = 200 **空 body 零 CT**；B = 200 JSON `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}`（remote 仓上游镜像入目录）。
4. **remote 仓未知镜像 tags/list**（同 case arm3）：A 404 `NAME_UNKNOWN`；B 200 `{"name":…,"tags":null}`（本地仓面双端同 404——仅 remote 面分歧）。
5. **熔断正确凭据封锁内行为**（任务一）：A 放行 200 vs B pre-auth 拒 403（安全语义反向——B 更严但偏离参照）。
6. **熔断阈值线口径**（任务一）：A 第 3 发 vs B 第 6 发拒绝（常数 5→2 收敛建议在案）。
7. **熔断届满粘滞 + 窗口升级**（任务一）：A 粘滞计数 + 4→9-10→15-16 升级 vs B fresh + 固定 4s（B 侧为码面陈述）。

## Z 口径更新建议（归 compatibility-engineer）

1. 新条目提案：`docker/remote-schema1-conversion`（候选 ②）、`docker/registry-catalog-bare-root`（候选 ③）、`docker/remote-unknown-image-tags-list`（候选 ④）、`docker/bearer-failure-shape`（候选 ①）、`rest/login-failure-rate-limit` 参数面（R-22c 后续：阈值/正确凭据行为/粘滞升级三小项）。
2. `docker/remote-*` 基准标注：本波全部 dual 判定基于 7.161.26 + dev-cfa5da05。
3. normalize：docker-remote 域维持 **raw**（6 用例全部 raw 判定；B 尾换行沿用 N-1 方言注记，未动 normalize.yaml）。
4. 工装纪律自留：负缓存探针 tag 按轮唯一（`nc<NONCE>`）；上游 DELETE 405 残留结转手动 GC 清单（本波新增 `nc1a181cca3cd`、`nc1a181cea8bb`）。

## 四问口径（本波）

- **surface**：docker v2 面 6 条 unknown 臂（负缓存窗口/catalog 空降级/star INM/前缀拓扑/schema1/bearer 篡改族）+ 熔断面阈值-窗口-凭据三维参数空间（A 侧 9 次独立触发 + B 侧 2 轮活体测量）。
- **matched**：3/6 case PASS（负缓存窗口双端同姿、star INM 四臂同姿、B 前缀拓扑断言成立）+ 熔断线文案/窗口/衰减/自愈四维双端同。
- **divergence**：3/6 case FAIL 展开为 7 项候选（上列）；任务一贡献 3 项（5/6/7）。
- **unknown（未覆盖，不立断言）**：负缓存精确 TTL 等值（分钟粒度同级已证）；真实 docker daemon 端到端拉穿时序（wave-1 结转）；token 真过期/吊销态（需参照侧状态操控）；B 熔断跨面共享与粘滞升级的活体复测（码面推断）；`/binflow/v2/` 在 A 侧的偶发形态（record-only 未断言）。

## 工件与清理

- 用例：`tools/difftest/v2/cases/` 新增 6 文件（上表）；探针 `wave2_threshold_probe{,2}.py`、`wave2_bside_breaker_probe.py`、`wave2_bside_admin_block_probe.py` + 4 个 `.sh` 包装；批跑入口 `wave2_run.sh`（六变量 + `DIFFTEST_UPSTREAM_BASE` 注入，产物落 `run/wave2/`）。
- 证据：`run/wave2/`（results.json + evidence/ 逐检查项 + 4 份阈值实验 JSON，无凭据）；复跑门 `run/wave2-2/`。
- 清理：双端 `diff-catalog-<n>` 临时仓每轮 DELETE 200、复验零 `catalog` 残留；探针一次性用户名零持久化（B 侧 in-process 自愈，A 侧 4s 窗自愈）；上游 `nc-*` tag ×2 残留（DELETE 405，手动 GC 清单结转）；`difftest-docker/smoke` t1/t2/t3 维持声明留存。

## 复跑门与机器可读门

- 复跑：`run/wave2` 与 `run/wave2-2` 结论同构（6 id 状态逐一相等，首失败原因逐字一致）。
- results.json 解析：`python3 -I -c "import json; json.load(open('run/wave2/results.json'))"` 通过；case id ↔ 用例文件对账见上表。

---

# L043 五候选活体探针 rerun（wave-5，2026-10-29）

- 模式 **dual**（A/B 双发；两个长窗采样器为受控双端平行单边记录）；批跑入口 `wave5_run.sh` → `run/l043-probe`（首轮：catalog case 用例 bug BLOCKED）/ `run/l043-probe-2`（修后定谳轮）/ `run/l043-probe-3`（复跑门轮）。
- **A 面**：JFrog Artifactory **7.161.26** @ `http://192.168.120.36:32082`（凭据 gitignored `.env.ref` 行首锚定注入，零落盘零打印）。
- **B 面**：BinFlow @ `http://192.168.120.36:32083`。**版本活体事实**：`/binflow/api/system/version` 报 **dev-aceee772**（与简报「dev-cfa5da05+阈值2」字符串不一致），但 **阈值-2 行为活体在装**：一次性不存在用户名 3 发坏 Basic → `401, 401, 403`（`run/l043-probe/bside_threshold_context.json`；wave2 方法论，发间 ≥1.15s）——即熔断线口径已收敛为与 A 相同的第 3 发拒绝；版本端点字符串与行为面不一致一事按活体行为口径理解，报 Risks。
- 夹具活体：preflight 双端 REST ping 200、remote manifest t2 200、上游中继 :32100 200（`wave3_preflight.sh` 复用）。
- score（定谳轮）：`{"pass":0,"fail":6,"blocked":0,"not_run":51,"total":57,"coverage":0.0}`——本波 6 例全为分歧记录型深挖，FAIL=分歧成立并完成取证，与 wave-3 口径一致。
- **复跑门：PASS**（`wave5_rerun_gate.py run/l043-probe-2 run/l043-probe-3`：6 id 状态逐一相等、首失败原因 nonce 归一后逐字一致、检查账本逐项同构；归一覆盖 w5* nonce 家族与 `count=N` 上游残留数字）。首轮 5 个共同 case 与定谳轮状态逐一相同（catalog 首轮 BLOCKED 系用例 bug：blob Location 处理引用未定义变量，修复后两轮 FAIL 稳定；崩溃遗留的临时仓 `diff-cat5-1a187f8ca10` 双端 DELETE 200 清净并复验）。

## 探针 1 — registry 级 `/v2/_catalog` 成员拓扑矩阵（case `dcr-catalog-membership-matrix`）→ FAIL（分歧收敛 + A 不变量再加固）

拓扑背景（REST 活体）：A 93 仓（Docker LOCAL×2 `difftest-docker`/`docker-local`、REMOTE×2 `diff-docker-remote`/`docker-remote`、VIRTUAL×1 `docker`——local/remote/virtual 三拓扑齐备）；B 1 仓（fixture remote）。

| 相位 | A（7.161.26） | B（活体） | 判定 |
|---|---|---|---|
| P0 基线（authed） | 200 **空 body 零 CT** | 200 JSON `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}` | body-kind DIVERGENT（候选 ③ 持续） |
| T 匿名 | 401 `UNAUTHORIZED` JSON（CT `application/json;charset=ISO-8859-1`） | 200 同 JSON | **DIVERGENT（C-a 在 catalog 面复现）** |
| T 坏 Basic（一次性用户） | 401 Jackson pretty `"Bad Credentials"` | 401 泛化 `authentication required` | 状态 SAME，形态 DIVERGENT（候选 ① 族已知面） |
| T 有效 Basic | 200 空 body 零 CT | 200 JSON | 同 P0 |
| M 临时 local 仓**灌入镜像**（blob×2 + manifest PUT + HEAD 复验 200 双端） | 200 **仍空 body 零 CT**（93 仓在册，含已灌镜像的 local；批 2 轮因孤儿未清为 94——两轮目录恒空不变） | 200 JSON 新增 `diff-cat5-<n>/difftest-docker/smoke` 条目 | **body-kind DIVERGENT + 成员规则分叉坐实：B 按「仓/镜像」路径聚合（populated local 入目录），A 成员解耦不变面在 populated-local 拓扑下依旧成立**（wave-3 empty-local 观测升级） |
| C 清理后（DELETE 200×2） | 200 空 body 零 CT | 200 JSON，条目消失（count 2→1） | body-kind DIVERGENT；**B 成员增删随仓生命周期精确可逆（双向取证）** |

**定谳**：A bare-root catalog 的「成员解耦不变面」从三拓扑（wave-3：remote 在位/空 local/清理后）扩展到 **populated-local 第四拓扑**，且对 93→94 仓的在册规模不敏感——A 侧不变量证据闭环；B 是真实聚合面且成员规则钉死为「镜像路径粒度、随推送/删除实时进出」。匿名策略分叉维持 C-a（record-only 域）。

## 探针 2 — schema1 缓存相位四向边界（case `dcr-schema1-phase-boundary`）→ FAIL（语义定谳复现 + 污染持久性 + 对照方向 + 短窗未愈）

新推 3 个轮次唯一上游 tag（F1 `w5w*` 暖缓存环 / F2 `w5c1*` 冷取环 / F3 `w5n*` 对照环）。

| 相位 × Accept | A | B | 判定 |
|---|---|---|---|
| w1 冷+negotiate（F1） | 200 schema2 | 200 schema2 | SAME |
| w2 热+s1-only（F1） | 200 **prettyjws**（服务时转换） | 200 **schema2 原样** | **DIVERGENT（候选 ② 复现，dev-aceee772 活体再证）** |
| w3 热+s1-mixed（F1） | 200 schema2 | 200 schema2 | SAME |
| w4 热+negotiate（F1） | 200 schema2 | 200 schema2 | SAME |
| c1 冷+s1-only（F2） | 200 prettyjws | 200 prettyjws | SAME |
| c2 冷后跟+s2（F2） | 200 **schema2**（缓存保持规范） | 200 **prettyjws**（污染） | **DIVERGENT（C-b 复现）** |
| **c3 再跟+s2（F2，新）** | 200 schema2 | 200 **prettyjws** | **DIVERGENT（污染持久：第 3 读仍污染）** |
| **c4 跟+negotiate（F2，新）** | 200 schema2 | 200 **prettyjws** | **DIVERGENT（污染宽度：negotiate 读也被污染）** |
| **n1 冷+s2-only（F3，新对照）** | 200 schema2 | 200 schema2 | SAME |
| **n2 热+s1-only（F3，新对照）** | 200 **prettyjws** | 200 **schema2** | **DIVERGENT（与 w2 同构——B 的热读无视 Accept 与冷取 Accept 无关，缓存键不含 Accept 双向钉死）** |
| rec90（+90s，record-only） | 200 schema2 | 200 **prettyjws** | 短窗未愈（+90s 仍污染；不立 TTL 断言） |

**定谳**（wave-3 语义定谳在 dev-aceee772 + 阈值-2 B 上原样复现并补齐边界）：A = 服务时按 Accept 转换、缓存永保持规范 schema2；B = 缓存键不含 Accept，冷取协商回包原样入库、此后一切 Accept 热读都吃缓存（c3/c4 持久性 + n1/n2 对照方向双向钉死）。**C-b 污染在 60s 读节奏下 ≥30min 不自愈**（独立采样器 `wave5_schema1_recovery_probe.py`，见下）。

## 探针 3 — bearer 篡改族全面矩阵（case `dcr-bearer-face-full-matrix`）→ FAIL（2 面×4 形 → 6 面×4 形，24 格全取证）

| face \ shape | anon | basic-bad | bearer-garbage | bearer-empty |
|---|---|---|---|---|
| **ping** `/v2/` | 双端同 401 Bearer+泛化 | 双端同 401 Basic+Bad Credentials | 双端同 401 Basic+Props Token not found | **A 403/none/Forbidden vs B 401/Basic/Bad Credentials（C-c 复现）** |
| **manifest** | **A 401 Bearer vs B 200 匿名放行（C-a）** | A Basic+BadCred vs B Bearer+泛化 | A Basic+Props vs B Bearer+泛化 | A Basic+AuthReq vs B Bearer+泛化 |
| **blob** | **A 401 vs B 200（真 blob 字节）** | 同 manifest 行形态 | 同 | 同 |
| **tags** | **A 401 vs B 200** | 同 | 同 | 同 |
| **catalog** | **A 401 vs B 200** | 同 | 同 | **A 403/none/Forbidden vs B 401**（A 侧 403 格=ping+catalog 双面） |
| **token** | **A 401 Basic+AuthReq vs B 200 匿名发票** | A Basic 挑战 vs **B Bearer 挑战**（body 同文 BadCred） | 消息双端同 Props，挑战 A Basic vs B Bearer | 消息 A AuthReq vs B BadCred，挑战同左 |

**定谳**（wave-3 结论完成面级推广）：
- **A 侧片内规则**（6 面聚类）：basic-bad 与 bearer-garbage 两形在**全部 6 面**形态逐字一致（Basic 挑战 + 分类文案）——A 的 401 形态是面无关的统一机制；分叉只在 bearer-empty（403 格 = ping+catalog 两个无路径面；manifest/blob/tags/token = 401+AuthReq）与 anon（token 面 Basic 挑战 vs 5 个 v2 面 Bearer 挑战）。
- **B 侧跨面不一精确定位**：ping 面 4 格中 3 格与 A 逐字同（仅 bearer-empty 异）；**内容面（manifest/blob/tags/catalog）在 basic-bad/bearer-garbage/bearer-empty 三形全部退化为「Bearer 挑战 + 泛化 authentication required」单一形态**；token 面介中——消息与 A 逐字同（BadCred/Props）但挑战家族恒 Bearer。
- **新格（匿名面）**：B 的内容面 + token 面全部匿名放行（blob 真回字节、token 真发票），仅 ping 面有闸门——**C-a 从「manifest/catalog 两面」推广为「B 内容面全域开放、ping 面唯一闸门」**；A 六面全闸（token 面 401 Basic+AuthReq——匿名 docker 流在 A 的 token 端点即被拒，record-only 记录不外推客户端后果）。
- 账本安全：48 发全矩阵 `recurrent_wording=False` 零触发；basic-bad 每格一次性不存在用户名（12 个，各 1 发，低于任一侧阈值）；admin 永不触碰。

## 探针 4 — tags/list 非活体视图（case `dcr-tags-list-liveness` + 独立采样器）→ FAIL（t0 分歧复现 + **54min 窗口不成立** + manifest 拉取刷新假设得证）

受控节奏：warm（双端 tags/list 200，A=B=21 同构）→ 上游推新 tag（201）→ t0/+60/+120/+180 四采样，每采样双端 tags/list + 新 tag manifest GET。

| 采样 | A tags（含新 tag? / count） | A manifest 新 tag | B tags | B manifest 新 tag |
|---|---|---|---|---|
| t0 | **否 / 21**（陈旧） | **200**（活体拉穿） | 是 / 22 | 200 |
| t60 | **是 / 22**（已刷新） | 200 | 是 / 22 | 200 |
| t120 / t180 | 是 / 22 | 200 | 是 / 22 | 200 |

- **t0 分歧复现**：warm 后立即推 tag，A t0 仍答旧集（陈旧视图坐实）、B t0 即含新 tag（活体聚合坐实）——候选 ④ 的差分核心在受控条件下成立（复跑门两轮同构）。
- **但 wave1-rerun13b 的「≥54min 不刷新」本轮不成立**：A 在 ≤60s 即刷新。与 rerun13b 的关键工况差异：**本轮 t0 对新 tag 的 manifest GET 成功（200 拉穿）**，rerun13b 全程只有 tags/list 读取、从无 manifest 拉取。由此预注册假设 **(c) manifest 拉取触发该仓 tags 视图刷新**（与 L000 C10 marker 门控语义同族）。
- **假设 (c) 独立甄别**（`wave5_tags_ttl_window_probe.py`：warm 后推轮次唯一 tag，纯 tags/list 60s 节奏采样 30 次 ≈30min、零 manifest 拉取）：**A 全程 30/30 采样陈旧（~1800s 未刷新），B 恒活体**（`run/l043-probe/tags_ttl_window.json`）。双工况对照成立：
  - 工况 α（本采样器 + rerun13b）：只读 tags/list → A ≥30min 不刷新（rerun13b 曾观测 ≥54min）；
  - 工况 β（批跑 case）：t0 新 tag manifest GET 拉穿 200 → A ≤60s 刷新。
  **「manifest 拉取触发该仓 tags 视图刷新」假设由双工况对照强支持**；窗口非常数、系工况依赖——候选 ④ 的「~54min」改判为「非活体缓存视图（刷新受 manifest 拉取支配）」，定数归专项。

## 探针 5 — tags/list Accept 协商宽严差（case `dcr-tags-list-accept-strictness`）→ FAIL（A 406 规则钉死 / B 恒 200）

9 种 Accept 变体（authed，双端）：

| Accept | A | B |
|---|---|---|
| 无 / `*/*` / `application/json` / `schema2+*/*` 混合 | 200 JSON | 200 JSON |
| schema2 only / list only / OCI index only / schema1 only / negotiate 三件套 | **406** | 200 |

**定谳**：A 的 406 规则 = **Accept 非空且全部成员均为 manifest 族 media type**（含 OCI/ FAT/schema1/negotiate——不必含 schema2 才拒）；掺入 `*/*` 或 `application/json` 或无 Accept 即 200。B 恒 200 无视 Accept。与 wave1-rerun13b 的「manifest media-type Accept → A 406」单点观测收敛为完整规则（且推翻「仅 schema2 拒」的窄化猜想）。**客户端后果面**：任何把 manifest Accept 复用到 tags/list 请求的客户端（如部分 skopeo/crane 旧路径）在 A 上 406、在 B 上 200——宽严差坐实为真实互操作差异，不定性。

## 探针 2 附加 — 上游同 tag 变异 vs 缓存（case `dcr-schema1-tag-mutation-heal`）→ FAIL（**双端对已缓存 tag 的上游变异均盲**）

上游 v1 →（A/B 各冷拉 schema1 一次：A 转换入库、B 污染入库）→ 上游同 tag 变异为 v2（201）→ 三种 Accept 热读：

| 读 | A | B |
|---|---|---|
| mut-hot-s2 | 200 schema2 **config=v1** | 200 prettyjws（v1 污染） |
| mut-hot-negotiate | 200 schema2 **config=v1** | 200 prettyjws |
| mut-hot-s1 | 200 prettyjws | 200 prettyjws |

`mutation_observed` 双端全 false：**秒级窗口内双端都不感知同 tag 上游变异**。批后 ~10min 复查（curl 活体）：上游=v2、A 仍 v1 schema2、B 仍 prettyjws——**分钟级仍盲**。与探针 4 联读成重要对照：**A 对「新 tag 路径」首触即活（t0 manifest 200），对「已缓存条目的上游变异」长时间盲**——B 同构（盲）。C-b 的恢复路径由此收窄：同 tag 变异不触发失效；唯一观测到的刷新触发器是探针 4 的「新 tag manifest 拉穿 → tags 视图刷新」（对 manifest 内容缓存本身的刷新未测，归 Risks）。

## 独立采样器（C-b 恢复窗 × schema1 污染）

`wave5_schema1_recovery_probe.py`（冷 schema1 污染 B 后，schema2-Accept 读每 60s 采样，30 采样 ≈30min 窗）：**B 全程 30/30 采样仍返回污染 prettyjws 表示**（`run/l043-probe/schema1_recovery.json`，A 侧同窗恒规范 schema2）——**C-b 污染在 60s 读节奏下 ≥30min 不自愈**；结合读续期可能性未排除（Risks），绝对 TTL 下限改判为 ≥30min（原 wave-3 c2 单点证据的边界大幅外扩）。

## 新分歧候选与既有候选 evidence 补充（提案——归 compatibility-engineer 裁定收编，本票不改 known-divergence.yaml）

1. **候选 ③（registry catalog）**：evidence 补充 = 本节探针 1（populated-local 第四拓扑 + 94 仓规模不敏感 + B 成员双向可逆）。建议条目 `docker/registry-catalog-bare-root` 的 rationale 增补「A=成员解耦不变面（四拓扑闭环）；B=镜像路径粒度实时聚合」。
2. **候选 ②（schema1 转换）**：evidence 补充 = 探针 2 表（w2/c3/c4/n1/n2 五向）+ 恢复采样器。建议 `docker/remote-schema1-conversion` expect 钉为「A 服务时转换相位无关；B 缓存键不含 Accept、冷取回包原样入库」。
3. **C-b（B 缓存表示污染）**：evidence 补充 = c2/c3/c4 + rec90 + 恢复采样器（60s 节奏下 ≥30min 不自愈，30/30 采样全污染）+ 变异 case（同 tag 上游变异不触发失效）。
4. **候选 ①（bearer 失败形态）**：evidence 补充 = 24 格矩阵（探针 3 表）。B 侧跨面不一的精确定位从「manifest 面」改钉为「内容面四格退化 + token 面挑战家族漂移 + ping 面同构」。
5. **C-a（B 内容面匿名放行）**：evidence 补充 = 矩阵 anon 行（内容面 4 面 + token 面 200，blob 回真字节、token 真发票；ping 面唯一闸门）。安全语义反向，维持报 conductor + 安全侧。
6. **C-c（空 bearer ping 面 403）**：evidence 补充 = 矩阵 bearer-empty 列：A 的 403 格 = ping+catalog 双面（wave-3 单面观测扩展），B 对应格恒 401 BadCred。
7. **候选 ④（tags/list 非活体）**：**重大更新——「~54min 窗口」不可作为定数登记**。t0 差分核心成立（A 陈旧/B 活体），但窗口受「是否发生 manifest 拉取」支配（工况 β 本轮 ≤60s 刷新 vs 工况 α 采样器纯 tags 读取 ≥30min 未刷新、rerun13b ≥54min）；建议条目改钉「A tags/list 为非活体缓存视图（触发条件依赖 manifest 拉取，窗口可变）」并附两工况证据，窗口定数待专项。
8. **新候选提案 `docker/tags-list-accept-strictness`（探针 5）**：A =「Accept 全为 manifest 族 media type 即 406」规则 vs B 恒 200——建议独立条目（与候选 ④ 解耦：一个是视图活性、一个是协商宽严）。
9. **新候选提案 `docker/manifest-cache-mutation-blindness`（变异 case）**：已缓存 tag 的上游同 tag 变异，A/B 双端分钟级盲（A canonical v1 schema2、B 污染 prettyjws）——远程缓存缺失上游变异失效机制，牵 L9 缓存语义域。
10. **熔断阈值线口径闭环**：B 阈值-2 活体在装（401,401,403）——L043 任务一建议 1（常数 5→2）**已落地并可结**；「正确凭据封锁内放行」「粘滞+升级」两建议维持待裁（本票未复测）。

## 四问口径（本波探针）

- **surface**：catalog 成员拓扑 6 相位 ×3 认证态 ×双端；schema1×缓存相位 11 读 ×双端（3 个轮次唯一冷 tag）+ 变异链 5 读 + 恢复采样；bearer 矩阵 24 格 ×双端（48 发）；tags 活性 4 采样×4 读 + 独立双采样器；Accept 严格度 9 变体 ×双端。
- **matched**：A bare-root catalog 不变量四拓扑闭环（A 侧自洽）；schema1 w1/w3/w4/c1/n1 五相位双端同；bearer 矩阵 ping 面 3/4 格 + basic-bad/bearer-garbage 全面 A 片内统一且 B ping 同构；tags t60+ 双端同集；Accept 基线四变体双端同 200；B 阈值-2 行为与 A 线口径对齐（第 3 发拒绝）。
- **divergence**：6/6 case FAIL 展开——候选 ③ 四拓扑定谳、候选 ② 五向复现 + C-b 持久性/宽度、候选 ① 24 格完成 + C-a 全域化、C-c 双面扩展、候选 ④ 窗口改判、探针 5 新规则、变异盲新候选。
- **unknown（未覆盖，不立断言）**：A tags 视图刷新的精确触发机制（manifest 拉穿假设已获双工况对照支持，机制未解剖）；manifest 内容缓存（非 tags 视图）是否同样受拉穿刷新；B 污染的绝对 TTL（60s 读节奏下 ≥20min 未愈——读续期可能性未排除）；A 匿名 token 401 对真实客户端匿名拉取流的端到端后果；真实 docker daemon 端到端拉穿时序（wave-1 结转）。

## 工件与清理

- 用例：`cases/` 新增 6 文件（`docker_catalog_membership_matrix.py`、`docker_schema1_phase_boundary.py`、`docker_bearer_face_full_matrix.py`、`docker_tags_list_liveness.py`、`docker_tags_list_accept_strictness.py`、`docker_schema1_tag_mutation_heal.py`）；独立探针 `wave5_tags_ttl_window_probe.py`、`wave5_schema1_recovery_probe.py`、`wave5_bside_threshold_context.py`；批跑 `wave5_run.sh`；复跑门 `wave5_rerun_gate.py`；摘要 `wave5_summarize.py`。
- 证据：`run/l043-probe/`（首轮：5 case + catalog BLOCKED + 阈值上下文 JSON）、`run/l043-probe-2/`、`run/l043-probe-3/`（定谳 + 复跑门）、采样器 JSON 同目录；全部 payload 无凭据。
- 清理：批 1 崩溃遗留 `diff-cat5-1a187f8ca10` 双端 DELETE 200 + GET 400 复验清净；批 2/3 临时仓 `diff-cat5-*` 由用例内 DELETE 200 双向清净（c_membership 复验零残留）；一次性用户名（w5b* ×12/轮、w5c*、w5thr*）均不存在于双端，零持久化；上游残留 tag 结转手动 GC 清单（上游 DELETE 405 沿 L043 口径）：`w5w*`/`w5c1*`/`w5n*` ×3/轮 ×3 轮、`w5t*` ×3、`w5rec*` ×1、`w5win*` ×1、`w5m2*` ×3 + 变异 config blob ×2（共享 layer 无新增）。

## 复跑门与机器可读门

- 复跑：`wave5_rerun_gate.py run/l043-probe-2 run/l043-probe-3` → `RERUN GATE: PASS`（6 id 状态逐一相等、首失败原因 nonce/count 归一后逐字一致、检查账本逐项同构）；首轮与定谳轮 5 共同 case 状态同构，catalog 首轮 BLOCKED 为已释明用例 bug。
- results.json 解析：`python3 -I -c "import json; json.load(open('run/l043-probe-2/results.json'))"` 通过（三轮均验）；case id ↔ 用例文件对账见各探针小节标题。
- 采样器复跑：恢复采样器单次长窗运行（C-b 恢复窗为状态依赖量，双轮重跑成本 2×N 分钟，收益有限——以批跑 c2/c3/c4 三点同构 + 采样器单窗长观察替代，Risks 已注）。
