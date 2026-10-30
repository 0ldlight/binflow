# L044 — 差分 wave-3：L043 候选深挖（catalog 拓扑三态 / schema1 缓存相位 / bearer 跨面矩阵）+ A 侧熔断账本作用域验证（T-632/T-635 反哺）

- 日期：2026-10-29；模式 **dual**（A/B 双发，v2 框架批跑；任务一为 A 侧受控单边 + B 侧既有实证对照）
- **执行域**：worktree `wave-docker-remote`（未动 git 分支；新增用例与报告本地提交 `claude/lzw-followups`，不推远端）
- **A 面**：JFrog Artifactory **7.161.26** @ `http://192.168.120.36:32082`（docker v2 面根路径，REST `/artifactory/api/*`；凭据 gitignored `.env.ref` 行首锚定注入，零落盘零打印）
- **B 面**：BinFlow **dev-aceee772** @ `http://192.168.120.36:32083`（`/binflow` 前缀 REST、docker v2 面根路径）
- 夹具活体：双端 remote 仓 `diff-docker-remote` + 上游中继 `:32100`（zot 系）`difftest-docker/smoke`（t1/t2），preflight HEAD 200 双端复验在案（`wave3_preflight.sh`）。

---

## 任务一：A 侧熔断账本作用域语义验证（反哺 T-632/T-635 设计定性）

**方法**：一次性不存在用户名（`semantics-a-<epoch>` ×2 次独立运行），坏 Basic 凭据在两路由间轮替——`GET /artifactory/api/system/ping` ↔ `GET /artifactory/api/repositories`，发间 ≥1.15s，4 轮上限；触发后封锁面确认三发（ping / repositories / docker 根 `/v2/`，无间隔连发）+ 窗口自愈两腿（sleep 4.6s 单发、再紧接一发）。探针 `wave3_a_scope_probe.py`，证据 `run/wave3/scope_experiment_a.json`（2 份，无凭据）。

### A 侧作用域三事实

**事实 1 — 跨路由共享 = 是（与 BinFlow 同构）。** 轮替序列 `401(ping) → 401(repositories) → 403(ping)`：**合计第 3 发即封**，即 2 次跨路由失败累计触发——账本按用户名全局共享、不按路由分账。2/2 次独立运行（不同一次性用户名）同构复现。**反哺结论：BinFlow「按用户名跨路由共享账本」的设计语义与参照一致，T-635 钉死测试的语义无需重审**；「防主体爆破预算按路由数翻倍」是参照自身的形态而非 BinFlow 分歧。

**事实 2 — 阈值形态：线口径 N=3，bind-next，与 L043 单路由测量完全一致。** trip 文案逐字：`This request is blocked due to recurrent request failures, please try again in 3 second(s)`（Jackson pretty 方言，`errors[0]{status,message}`）——跨路由触发与单路由触发同文案同形态，进一步佐证同一账本。

**事实 3 — 窗口自愈 + 封锁面共享。** 封锁期内 ping / repositories / docker 根 `/v2/` 三面连发全部 403 且共享倒计时（运行 1：cd=3,3,3；运行 2 随时钟衰减 cd=3,3,2——与 L043「倒计时随时钟衰减」相容，运行 1 三发落在同一秒故同值）；~4.6s 后单发坏凭据 → **401 自愈**；紧接一发 → **403 cd=8 再武装**（粘滞计数 + 第二窗口 ~9s，与 L043 的 4→9-10 升级序列相容）。

**范围附注**：一次性用户名不存在于 A，零残留零清理；全程未触碰 admin（L043 已证封锁按用户名键控、admin 自身正确凭据 200 放行）。

---

## 任务二：L043 候选深挖（3 case，dual 四态）

批跑 `wave3_run.sh` → `run/wave3/`（定谳）+ `run/wave3-2/`（复跑门）。**复跑门：两轮 3 id 状态逐一相等、首失败原因逐字一致；nonce 无关化后 checks 与 facts 逐字同构（`RERUN GATE: PASS`），零 flake。** score：`{"pass":0,"fail":3,"blocked":0,"not_run":47,"total":50,"coverage":0.0}`（本波 3 例全为分歧记录型深挖，FAIL=分歧成立即用例达成记录目的）。

### 逐臂判定表

**case 1 — `dcr-catalog-bare-root-topology`（候选 ③ 深挖）→ FAIL（分歧成立并完成取证）**

| 臂 | A（7.161.26） | B（dev-aceee772） | 判定 |
|---|---|---|---|
| P0 基线（fixture remote 在位，authed） | 200 **空 body 零 CT** | 200 JSON `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}` | body-kind **DIVERGENT** |
| P1 +临时空 local 仓（authed） | 200 空 body 零 CT；`repo_count=0`（**local 仓不入目录**） | 200 JSON；`has_temp_repo=false`（**empty local 仓同样不入目录**） | body-kind DIVERGENT；membership 行为 SAME |
| P2 匿名（record-only） | **401** `{"errors":[{"code":"UNAUTHORIZED",…}]}` | **200 同 JSON** | **DIVERGENT（新，并入 C-a）** |
| P3 清理后（authed） | 200 空 body 零 CT | 200 JSON | body-kind DIVERGENT（p3 复验 `has_temp_repo=false` 双端零残留） |

**定谳**：A 的 bare-root `/v2/_catalog` 是**成员解耦的不变面**——remote 仓在位、local 仓创建、清理后三拓扑恒空 body 零 CT（L043 单点观测升级为三态不变量）；B 是真实聚合面（remote 上游条目入目录）但 **empty local 仓不入目录**（成员构成规则在该格双端一致）。新分叉：匿名策略（A 认证闸门 vs B 匿名放行）。

**case 2 — `dcr-schema1-cache-phase`（候选 ② 深挖，四向定向）→ FAIL（语义边界钉死 + 新污染方向）**

每轮上游补推 2 个轮次唯一 tag（F1=`w3w<n>` 暖缓存环、F2=`w3c<n>` 冷取环，匿名 PUT 复制 fixture manifest）：

| 相位 × Accept | A | B | 判定 |
|---|---|---|---|
| w1 冷取+negotiate（F1 首拉） | 200 schema2 | 200 schema2 | SAME |
| w2 热+s1-only | 200 **prettyjws**（无 mediaType/config 键） | 200 **schema2 原样**（mediaType/config 同 w1） | **DIVERGENT** |
| w3 热+s1-mixed | 200 schema2 | 200 schema2 | SAME |
| w4 热+negotiate（F1 稳定性） | 200 schema2 | 200 schema2 | SAME |
| c1 冷取+s1-only（F2 首拉） | 200 prettyjws | 200 prettyjws | SAME（状态/CT 面） |
| c2 冷后跟+s2（F2 二拉） | 200 **schema2** | 200 **prettyjws** | **DIVERGENT（新方向）** |

**语义边界定谳**（修正任务假设）：
- **A = 服务时转换、相位无关、缓存规范**：热缓存+s1-only 照样出 prettyjws（`hot_s1_equals_cached=false`），冷取 s1 后缓存仍是规范 schema2（c2 A=200 schema2）——「热缓存无视 Accept」对 A **不成立**；A 在每次读按 Accept 转换且缓存永保持 schema2 规范表示。
- **B = 冷取协商一次入库、热读缓存优先**：w2（F1 冷取 negotiate 后热读 s1-only）回 schema2 原样——「热缓存无视 Accept」对 B 成立；c1→c2 链条证明冷取把**协商回包表示原样入缓存**——F2 冷拉 s1 后 schema2 客户端收到 prettyjws。**「冷取透传上游并缓存回包」对 B 成立且产生了负向后果：schema1 表示污染缓存**（新候选 C-b）。
- 混合 Accept 双端同取 schema2（w3 双端 SAME，与 L043 相容）。

**case 3 — `dcr-bearer-crossface-challenge`（候选 ① 完成矩阵：2 面 × 4 形）→ FAIL（2 新格 + 跨面不一处定位）**

| 格（面/形） | A | B | 判定 |
|---|---|---|---|
| ping / anon | 401 **Bearer 挑战** + 泛化 `authentication required` | 同（逐字） | SAME |
| ping / basic-bad | 401 Basic 挑战 + `"Bad Credentials"` `{message,status}` | 同（逐字） | SAME |
| ping / bearer-garbage | 401 Basic 挑战 + `"Props Authentication Token not found"` | 同（逐字） | SAME |
| ping / bearer-empty | **403 无挑战 + `"Forbidden"`** | 401 Basic 挑战 + `"Bad Credentials"` | **DIVERGENT（新，C-c）** |
| manifest / anon | 401 Bearer 挑战（token 流）+ 泛化 | **200 匿名放行** | **DIVERGENT（新，C-a）** |
| manifest / basic-bad | 401 Basic 挑战 + `"Bad Credentials"` | 401 **Bearer 挑战** + 泛化 | DIVERGENT（候选 ① 已知形态） |
| manifest / bearer-garbage | 401 Basic 挑战 + `"Props Authentication Token not found"` | 401 Bearer 挑战 + 泛化 | DIVERGENT |
| manifest / bearer-empty | 401 Basic 挑战 + `"Authentication is required"` | 401 Bearer 挑战 + 泛化 | DIVERGENT |

**定谳**（修正 wave2 候选 ① 的表述）：A 并非「恒 Basic 挑战」——匿名时 A 在两面都答 Bearer 挑战（token 流）。**B 的 ping 面与参照高度同构（4 格中 3 格逐字同）**，跨面不一处精确收敛为 **B 的 manifest/内容面**（泛化文案 + Bearer 挑战 + 匿名放行，`crossface_inconsistent_shapes`：A=`[bearer-empty]`、B=`[anon,basic-bad,bearer-garbage,bearer-empty]`）。

**账本安全**：全矩阵 16 发零 `recurrent` 字样（`rec=False` 全格）——无一发触发熔断；basic-bad 格用每格一次性不存在用户名（账本按用户名键控且跨路由共享——任务一——故 admin 永不触碰）；bearer 格每格单发不重试。

---

## 新分歧候选（不定性，待裁）

- **C-a（新）docker v2 内容面匿名放行**：B 在 manifest 面（矩阵 manifest/anon=200）与 catalog 面（P2 匿名=200）匿名可取内容；A 两面均 401（Bearer 挑战 / UNAUTHORIZED JSON）。两面双重独立取证。安全语义反向（B 更开放）——报 conductor + 安全侧裁定；若属 B 的全局匿名策略设计，需对齐参照的认证闸门或在契约钉为 B-only。
- **C-b（新）B 冷取协商表示入缓存（schema1 表示污染）**：c2 铁证——F2 冷拉 schema1 后，schema2 Accept 客户端收到 prettyjws（CT + 无 mediaType/config 键）。建议后续实验：负缓存/缓存 TTL 届满后是否自愈回 schema2（本轮未测）；以及 docker 客户端对该面行为的端到端影响面。
- **C-c（新）空 bearer 在 ping 面的状态码语义分歧**：A=403 `"Forbidden"`（无挑战，非熔断文案）vs B=401 Basic 挑战 `"Bad Credentials"`。A 把空凭据 bearer 归为 forbidden 而非认证失败——影响客户端重试/挑战流转行为。
- **候选 ③ 更新（并入本波）**：A bare-root catalog = 成员解耦不变面（三拓扑恒空 body 零 CT）；P2 匿名分叉并入 C-a。
- **候选 ① 更新（并入本波）**：分歧面收敛为 B 的 manifest/内容面；B ping 面与参照逐字同构（3/4 格）。

## Z 口径更新建议（归 compatibility-engineer）

1. 契约提案：`docker/registry-catalog-anonymous`（C-a）、`docker/remote-schema1-cache-representation`（C-b）、`docker/bearer-empty-ping-face`（C-c）。
2. 契约修正案：`docker/remote-schema1-conversion`（L043 提案）的 expect 语义应钉为「A 服务时转换（相位无关）+ B 缓存相位语义（热读无视 Accept）」而非单一转换断言。
3. `docker/remote-*` 基准标注：本波 dual 判定基于 7.161.26 + dev-aceee772。
4. normalize：docker-remote 域维持 **raw**（3 用例全部 raw 判定，未动 normalize.yaml）。

## 四问口径（本波）

- **surface**：catalog 裸根面 4 拓扑态 × 双端；schema1×缓存相位 6 相位 × 双端（每轮 2 个轮次唯一冷 tag）；bearer 跨面矩阵 8 格 × 双端；A 侧熔断作用域参数空间（2 次独立运行 × 13 发，两路由轮替 + 三面确认 + 窗口两腿）。
- **matched**：任务一作用域语义双端同构（跨路由共享 + 线口径 + 窗口形态）；bearer 矩阵 ping 面 3/4 格双端逐字同；schema1 w1/w3/w4/c1 四相位双端同；catalog P1/P3 membership 行为同（empty local 仓不入目录双端一致）；临时仓清理双端零残留。
- **divergence**：3/3 case FAIL 展开——候选 ② 定谳（A 服务时转换 vs B 缓存相位 + C-b 污染方向）、候选 ③ 完成（A 不变面 + C-a 匿名分叉）、候选 ① 完成（跨面矩阵 + C-c 新格）。
- **unknown（未覆盖，不立断言）**：C-b 缓存污染的恢复窗口（TTL 届满是否自愈）；A c1 转换的来源（上游回包 vs 本地转换——单边上游不可观测，wire 层不可分辨）；匿名放行是 B 全局策略还是 remote 仓局部配置（未跨 package type 对照）；A 空 bearer 403 的触发面边界（仅 ping 面已证）；真实 docker daemon 端到端拉穿时序（wave-1 结转）。

## 工件与清理

- 用例：`cases/` 新增 3 文件（`docker_catalog_bare_root_topology.py`、`docker_schema1_cache_phase.py`、`docker_bearer_crossface_challenge.py`）；探针 `wave3_a_scope_probe.py|.sh` + 批跑入口 `wave3_run.sh` + `wave3_preflight.sh` + `wave3_summarize.py`。
- 证据：`run/wave3/`（results.json + evidence/ 逐检查项 + scope_experiment_a.json，无凭据）；复跑门 `run/wave3-2/`。
- 清理：双端 `diff-cat3-<n>` 临时仓每轮 DELETE 200、p3 复验零残留；探针一次性用户名（`semantics-a-*` ×2、`w3x*` ×8/轮）均不存在于 A，零持久化；admin 凭据零误用（坏凭据格全部一次性用户名）；上游残留 tag ×4 结转手动 GC 清单（`w3w1a186e578be`、`w3c1a186e578be`、`w3w1a186e810a2`、`w3c1a186e810a2`——上游 DELETE 405 同 L043 口径）。

## 复跑门与机器可读门

- 复跑：`run/wave3` vs `run/wave3-2` —— 3 id 状态逐一相等、首失败原因逐字一致；nonce 无关化（tag/用户名轮次前缀归一）后 checks 与 facts 逐字同构，`RERUN GATE: PASS`。
- results.json 解析：`python3 -I -c "import json; json.load(open('run/wave3/results.json'))"` 通过（两轮均验）；case id ↔ 用例文件对账见逐臂判定表。
- 任务一探针复跑：2 次独立运行（`semantics-a-6ae30a68`、`semantics-a-6ae30c74`）三事实逐一复现。
