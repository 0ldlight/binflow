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
