# L044 — 差分 wave-3 扩展：L043 五候选活体复验 + bearer 跨面 A 侧 24 格补全 + tags TTL 工况依赖实验

- 日期：2026-10-30；模式 **dual**（双端同发 ×2 全量批跑 + 复跑门 PASS；任务三为 A 侧受控长跑实验 + B 侧活体对照）
- **执行域**：worktree `wave-docker-remote`（未动 git 分支；新增用例与报告本地提交 `claude/lzw-followups`，不推远端）
- **A 面**：JFrog Artifactory **7.161.26** @ `http://192.168.120.36:32082`（docker v2 面根路径，REST `/artifactory/api/*`；凭据 gitignored `.env.ref` 行首锚定注入，零落盘零打印）
- **B 面**：BinFlow **dev-fdeabbf2** @ `http://192.168.120.36:32083`（`/binflow` 前缀 REST、docker v2 面根路径）
  - **部署版本注记**：任务简报标注 dev-aceee772；preflight 活体读取 `/binflow/api/system/version` 实测 `{"version":"dev-fdeabbf2","revision":"fdeabbf2"}`——该构建 = `claude/lzw-followups` HEAD 合并点 fdeabbf2，**含 T-632 认证熔断合入与 d7bd58eb（T-L043-C-b pull-through 修复）**。本报告全部 B 侧判定以 dev-fdeabbf2 锚定；L043 定谳（dev-cfa5da05）的复现性按此新基线解读。
- 夹具活体：双端 remote 仓 `diff-docker-remote` + 上游中继 `:32100`（zot 系）`difftest-docker/smoke`；preflight manifest t1 双端 200（schema2）、上游 t2 200 复验在案。
- 批跑：`wave3e_run.sh` RUN=1 → `run/wave3-expansion/`（定谳）；RUN=2 → `*-run2.json`（复跑门）；TTL 长跑 `wave3e_p4_ttl_workload.py` 后台先行（quiet-window 纪律：前 75 分钟内不对 A 的 docker remote 发起认证流量）。
- **复跑门：RERUN GATE PASS**——两轮 4 证据文件逐臂结论（status/challenge family/classification/body-kind/manifest 表示）92 格逐一相等，零 flake。

---

## 任务一：L043 五候选活体复验（逐项判定表）

### 逐项判定总表

| 候选 | L043 定谳（dev-cfa5da05，10-29） | 本波实测（dev-fdeabbf2，10-30） | 判定 |
|---|---|---|---|
| 候选③ catalog 根面 | A 根面恒空 body 零 CT（四拓扑不变）；B=remote 成员聚合；匿名根面 A 401 / B 200 | 六项 check 全 True（见 §1.1），双轮一致 | **REPRODUCED（6/6）** |
| 候选② schema1 协商 | A 逐请求协商缓存透明 12 臂无例外；B 冷取协商一次入库；C-b=TRUE（冷 s1 污染缓存行） | A 侧 12/12 臂复现；B 侧热相位 6/6 复现、**冷相位 D1/D2/D3 三臂 CHANGED**——C-b 污染不复现，替代形态为新分歧 D-1（见 §1.2） | **A: REPRODUCED；B: PARTIAL（C-b 已被修复消除，新分歧 D-1 出现）** |
| 候选① bearer 失败形态 | 4 面×6 臂：A 四面统一 Basic+分类文案；B F1 Basic / F2 F3 Bearer+泛化信封 / F4 realm 字符串分歧 | 7 项 check 全 True，A 侧 24 格 + B 侧 24 格全部补齐（见任务二），realm 字符串逐字复现 | **REPRODUCED（7/7）** |
| 候选面② tags/list Accept 406 | A 406 ⇔ Accept 存在且不含 application/json 与 `*/*`；B 九臂全 200 | 九臂逐臂同形，A 规则 holds=True misses=[]，B 全 200 | **REPRODUCED（规则级）** |
| 候选面① tags TTL | TTL≈2.5h 惰性刷新（窗内探测不刷新；届满后首请求触发）——**刷新触发器未分工况** | **工况依赖实锤**：窗内纯 tags 读 ×3 不刷新、热 manifest 拉（缓存命中）不刷新、**冷 pull-through 立即刷新**（见任务三） | **新证据（模型更新：刷新是工况依赖的）** |

### §1.1 候选③ catalog 根面（case `wave3e_p1_catalog_reverify`，两轮各 1 次建仓删仓）

| 臂 | A（7.161.26） | B（dev-fdeabbf2） | 判定 |
|---|---|---|---|
| T1a 根面 `/v2/_catalog`（auth，fixture remote 在场） | 200 **空 body 零 CT**（empty-body） | 200 JSON `{"repositories":["diff-docker-remote/difftest-docker/smoke"]}`（n=1） | body-kind DIVERGENT（定谳维持） |
| T1b + `?n=1000` | 200 同上 | 200 同上 | 分页假设再排除 |
| T2 仓限定 `/v2/diff-docker-remote/_catalog` | 200，6 成员 | 200，**逐字同 6 成员**（members identical=True） | 活体聚合一致复证 |
| T3 建临时空 local 仓（`diff-w3e-<nonce>` 双端 200）后根面 | 200 空 body；`has_new=False` | 200 JSON；`has_new=False`（**空 local 仓不入目录**） | membership 行为双端一致 |
| T4 匿名根面 | **401** `{"errors":[{"code":"UNAUTHORIZED",…}]}`（CT `application/json;charset=ISO-8859-1`，87B） | **200** 同上 JSON（62B） | **C-a 匿名分叉复现**（B 枚举泄漏面维持） |
| T5 匿名 local 仓 tags/list | 401 | 404 | B 匿名 404（与自身根面 200 不一）复现 |
| 清理 | DELETE 200，仓库列表复核零残留 | DELETE 200，同左 | 双端零残留（注意：DELETE 后 GET 返回 **400/400 双端一致**——L043 深挖时为 404/404；双端彼此 SAME，行为相对深挖的联合漂移记录在案，p1 exit=1 仅因该 verify 断言口径，删除正确性由仓库列表复核独立证明） |

**定谳**：候选③ 六项不变量在 dev-fdeabbf2 上全数复现；A 根面 = 成员解耦不变面、B 根面 = remote 成员聚合、C-a 匿名分叉，三行定谳升级为**跨两个 B 版本的稳定事实**。

### §1.2 候选② schema1 协商（case `wave3e_p2_schema1_reverify`，每轮自推 2 个轮次唯一上游 tag）

热相位（t1，P0 schema2 prime 后）——**双端 6/6 臂逐字复现深挖形态**：

| 臂 | Accept | A | B | 判定 |
|---|---|---|---|---|
| P0 | s2 | 200 schema2 | 200 schema2 | prime |
| H1 | s1 | 200 **prettyjws**（服务时转换） | 200 schema2 原样（热行无视 Accept） | 复现 |
| H2/H2b | s2 ×2 | 200 schema2（转换后缓存行无污染） | 200 schema2 | A 缓存透明复现 |
| H3 | （无） | 200 prettyjws（默认服务形） | 200 schema2 | 复现 |
| H4 | s1+s2 混合 | 200 schema2 | 200 schema2 | 复现 |

冷相位——**B 侧三臂 CHANGED（新分歧 D-1，见 §4）**：

| 臂 | tag | Accept | A | B（dev-fdeabbf2） | B（dev-cfa5da05 深挖） | 判定 |
|---|---|---|---|---|---|---|
| D1 | 冷 d1w3e* | s1 | 200 prettyjws | **404** `MANIFEST_UNKNOWN` | 200 prettyjws（上游转换入库） | **CHANGED** |
| D2 | 热 d1w3e* | s2 | 200 schema2 | 200 schema2 | 200 **prettyjws**（C-b 污染） | **CHANGED（C-b 不复现）** |
| D3 | 热 d1w3e* | （无） | 200 prettyjws | 200 schema2 | 200 prettyjws | **CHANGED（同上）** |
| E1/E2/E3 | 冷 d2w3e* | s2 / s1 / 无 | schema2 / prettyjws / prettyjws | schema2 / schema2 / schema2 | 同左 | 复现（热行无视 Accept 维持） |

**D-1 全信封**（probe 2b 专捕，`run/wave3-expansion/p2b_d1_envelope.json`）：

```json
{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest d1bw3e1a18bfdc433 is not available in an accepted media type (stored: application/vnd.docker.distribution.manifest.v2+json)","detail":{"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}}]}
```

**语义定性**：dev-fdeabbf2 的 pull-through（d7bd58eb，T-L043-C-b 修复）把「冷取协商一次入库」改为「**冷取即按规范 Accept 集拉取并以 schema2 入库，随后按存储表示应答**」：客户端 s1-only 冷请求先触发上游拉取+入库，然后被以 `MANIFEST_UNKNOWN (stored: schema2)` 拒绝。**C-b（冷 s1 污染缓存，schema2 客户端拿到 prettyjws）在该构建上不复现——修复达成其目的**；代价是 **legacy schema1 客户端经 B 的冷取全数 404**（A 逐请求服务时转换 200 prettyjws），属客户端可见负向新面。

### §1.3 候选① bearer 失败形态（case `wave3e_p3_bearer_matrix`，任务二矩阵同源）

7 项派生 check 全 True（两轮一致）：`A_credentialed_basic_all_faces` / `B_F1_credentialed_basic` / `B_F2F3_credentialed_bearer` / `B_F4_realm_BinFlow` / `A_Artifactory_Realm` / `anon_A_bearer_F123` / `anon_B_F1_bearer_F2F3_200`。逐字引证：

- A 四面凭据失败挑战统一 `Basic realm="Artifactory Realm"`；B F1 同款 Basic、**B F2/F3 = `Bearer realm="http://192.168.120.36:32083/v2/token",service="192.168.120.36:32083"`**、B F4 = `Basic realm="BinFlow Realm"`（realm 字符串分歧行维持）。
- 文案分类：bad-basic → 双端 `Bad Credentials`；bearer 三臂（garbage/algnone/truncated）→ ping 面 `Props Authentication Token not found`（B 与 A 逐字同）；B 内容面全部塌缩为泛化 `{"code":"UNAUTHORIZED","message":"authentication required"}` 信封（与匿名臂同体）——凭据类信息零保留。
- 匿名姿态：A F1-F3 = Bearer 挑战（docker 标准首腿）；A F4 REST ping = **200 OK**（匿名放行，深挖新侧写复现）；B F2/F3 匿名 = **200**（C-a 内容面放行复现）；B F4 匿名 = 200。
- A F1 empty-bearer = **403** `{"status":403,"message":"Forbidden"}`（无挑战，非熔断文案——C-c 形态复现；breaker-suspect 计数 1，系 403 状态标记而非 recurrent 文案）；A F2/F3/F4 empty-bearer = 401 Basic + `Authentication is required`；B F1 empty-bearer = 401 Basic `Bad Credentials`（与深挖同）。
- token 形态侧写复现：A 签发 806B JWT / B 64B 不透明串（仅 len+sha256 前缀落证据）；截 40% 双端均正确 401。
- 账本纪律：48 发全矩阵仅上列 1 处 403（C-c 固有形态），零 `recurrent` 熔断文案——一次性用户名 + 面间静默纪律有效。

### §1.4 候选面② tags/list Accept 406（case `wave3e_p5_tags_accept_reverify`）

九臂逐臂：`406 ⇔ Accept 存在且不含 application/json 与 */*` 规则在 A 上 **holds=True misses=[]**（s2/s2list/s1/oci-index/texthtml 五臂 406，none/appjson/star/mixed 四臂 200）；B 九臂全 200。406 信封 `{"errors":[{"status":406,"message":"Not Acceptable"}]}`（Jackson pretty）。**规则级定谳跨版本复现。**

---

## 任务二：bearer 跨面矩阵 A 侧补全（完整 24 格 ×双端）

深挖 p3 的 4 面 × 6 臂网格在本波全量重打（48 格，含 A 侧完整 24 格），RUN=1/RUN=2 两轮 48 格结论逐一相等。**每侧派生面指纹（凭据失败臂 401 的挑战族）**：

| 面 | A（24 格派生） | B（24 格派生） |
|---|---|---|
| F1-ping `/v2/` | **basic**（统一） | basic |
| F2-manifest | **basic** | **bearer**（分歧行） |
| F3-tags | **basic** | **bearer**（分歧行） |
| F4-rest-ping | **basic**（realm=Artifactory） | basic（realm=**BinFlow**，字符串分歧） |

A 侧矩阵补全定谳：**A 的凭据失败面四面统一 Basic + 分类文案**（ping 面 Basic+分类 vs 内容面亦 Basic+同族分类——跨面一致）；B 的跨面不一处精确维持在其内容面（F2/F3 Bearer+泛化）与 F4 realm 字符串。**B ping 面（Basic+分类文案）与 manifest 面（Bearer 挑战+泛化信封）的跨面不一在 T-632 合入后的构建上原样保留**——该面不在 T-632 的修复范围内，与参照的差距维持。

---

## 任务三：tags TTL 工况依赖实验（`wave3e_p4_ttl_workload.py`，后台长跑 +90 分钟）

**协议**：T0 prime A 视图（38 tags，不含新 tag）→ +30s 上游推 nonce tag `tw3e1a18bbba7df`（201）→ +35s B 活体复核（**即时可见=True**，B client-blind 超集再证）→ +5/15/25min 纯 tags 读 ×3 → +40min **W1**（热 t1 manifest 拉，缓存命中）→ +41min **W2**（**该 nonce tag 自身的冷 pull-through**）→ 各臂后立即 tags 复读 → +55/70min 纯读持久性 → +75min 起 3 分钟轮询竞速（届满臂，本轮因提前翻转记 moot）。

**实测时间线（A 侧 tags 视图 tag 计数）**：

| 时刻 | 工况 | 视图 n | 含新 tag | 判读 |
|---|---|---|---|---|
| +0s | T0 prime | 38 | 否 | 视图基线 |
| +300s | 纯 tags 读 #1 | 38 | 否 | 纯读不刷新 |
| +900s | 纯 tags 读 #2 | 38 | 否 | 同上 |
| +1502s | 纯 tags 读 #3 | 38 | 否 | 同上 |
| +2400s | **W1：热 t1 manifest 拉**（200 schema2，缓存命中） | 38 | **否** | **缓存命中拉取不刷新（关键对照）** |
| +2461s | **W2：nonce tag 冷 pull-through**（200 schema2） | **39** | **是** | **冷拉取立即刷新视图（工况依赖实锤）** |
| +3300s | 纯 tags 读 #4 | 39 | 是 | 刷新闩锁（持久） |
| +4200s | 纯 tags 读 #5 | **41** | 是 | **独立佐证**：批跑 p2 在 16:36-16:40 冷拉 d1w3e/d2w3e 穿 A，视图随即含二者（38+3=41）——无 tags 读触发、无届满（16:15 刷新，2.5h TTL 未到） |
| +4381s | 竞速首轮 | 39+ | 是 | 持久复验 has=True；窗口记 `window-closed-earlier-by-workload`，exit 0 |

**定谳（对 L043 候选面① 模型的更新）**：A 的 remote tags 视图刷新**是工况依赖的**——
1. 窗内纯 tags 读不刷新（L043 惰性结论再证，3 臂）；
2. **缓存命中的 manifest 拉取不刷新**（W1 对照臂，新增事实）；
3. **未缓存 manifest 的冷 pull-through 立即刷新 tags 视图**（W2 实锤 + p2 批跑二次佐证）——刷新不必等 TTL 届满，冷拉取路径本身会更新 tags 元数据；
4. 刷新闩锁持久（后续读稳定含新 tag）；B 侧全程活体（push 后当轮可见）。

CI「push→立查」工作流在参照上的可见性因此**取决于查询前是否有该内容的冷拉取发生**：纯列表查询是盲的（~2.5h 窗），但拉取后再查是即时的。L043 深挖的「届满后首请求触发」单样本结论应修订为「**届满后首请求触发 ∪ 冷拉取即时触发**」双通道模型（本波单窗单样本，跨日相位未采样，不外推）。

---

## 新分歧候选清单（不定性，待裁）

- **D-1（新）`docker/remote-schema1-cold-s1-rejection`**：B（dev-fdeabbf2）冷 s1-only manifest 请求 → 先行上游拉取+schema2 入库，随后 **404 `MANIFEST_UNKNOWN` (stored: schema2)**；A 同请求 200 prettyjws（服务时转换）。证据：run1/run2 D1 双复现 + probe 2b 全信封专捕（`p2b_d1_envelope.json`）。疑似根因 d7bd58eb（T-L043-C-b 修复）的客户端可见副作用；**相对 dev-cfa5da05（冷 s1 = 200 prettyjws）是回归向翻转**。建议分类议题：BUG（legacy 客户端破面）vs INTENTIONAL（修复取舍）——**UNKNOWN 待裁**，报 conductor + compatibility-engineer，并提示 dev 后端评估「存储表示与请求 Accept 不相交时做服务时转换或至少 4xx 语义对齐参照」。
- **C-b 状态更新（`docker/remote-schema1-conversion` 内）**：D2/D3 schema2 客户端不再收到 prettyjws——**缓存污染面在 dev-fdeabbf2 上不复现（fixed-in-build）**；known-divergence 该子项 evidence 应追加本波双轮证据并改判（裁定权在 compatibility-engineer）。
- **C-a / C-c / realm 字符串 / A REST ping 匿名 200**：四项侧写全部原样复现（§1.1/§1.3），无新证据需要改判，evidence 各追加本波双轮 JSON。
- **候选面① 模型修订**：刷新触发器 = 「届满后首请求」∪「冷 pull-through 即时」（工况依赖）；B client-blind 超集论证材料不变。

## 提金候选

无——本轮五候选全部为分歧/形态记录（A 侧通过臂均为已知 SAME 项的复验），无新增 A 侧通过且 B 未覆盖的金样捕获点。

## 残留与清理

- 双端仓库命名空间 CLEAN（A 93 仓无 `diff-w3e-*`/`diff-l043d-*` 键，B 1 仓；两轮 residue check 在案）。
- 上游中继（DELETE 405，手动 GC 清单结转）：本波新增 **6** tag：`tw3e1a18bbba7df`（p4）、`d1w3e1a18bf74832`/`d2w3e1a18bf74832`（p2 run1）、`d1w3e1a18bfebd79`/`d2w3e1a18bfebd79`（p2 run2）、`d1bw3e1a18bfdc433`（p2b）；在册历史存量 `d1l043d*`/`d2l043d*`/`tf1a*` 维持。
- 凭据纪律：全程 env 行首锚定读取、仅入 Authorization 头；token 仅 len+sha256 前缀；证据体仅 status/CT/WWW-Authenticate/body 截头。

## 环境事件（p0 先行事实）

1. **B 部署版本与简报不符（活体复验）**：简报 dev-aceee772，实测 `dev-fdeabbf2`（含 T-632 合入 + T-L043-C-b 修复）。本波全部判定以活体版本锚定；「matrix 宣称 vs 活体」账实核对通过（版本端点即证）。
2. **执行中途外部清理事故**：~16:0x 本 worktree 的全部未跟踪文件（含本波全部脚本与前一票 L042 复验产物）被外部 `git clean` 式操作清除（本会话未执行任何清理命令；疑似并行会话所为）。处置：全部脚本重写、逐文件完整性验证（py_compile + 特征行 grep——Write 落盘层曾在首版 p4 中**静默丢失一行** `_spec.loader.exec_module(lib)`，已按「写后必验证」纪律堵住）、提前本地提交（181de5eb + a496fd3a）防再发。`run/` 证据为 gitignored，未受损；p4 后台进程存活未中断。
3. Write 落盘层纪律（本波固化）：每文件写入后必须 `py_compile` + 特征行计数验证，先跑只读 smoke 再入批。

## 四问口径（本波）

- **surface**：catalog 根面 6 臂 ×双端 ×2 轮；schema1 热冷 12 相位 ×双端 ×2 轮（每轮 2 个轮次唯一冷 tag）+ 冷 s1 信封专捕 1 臂；bearer 4 面 ×6 臂 ×双端 ×2 轮（48 格/轮）；Accept 406 九臂 ×双端 ×2 轮；TTL 工况实验 1 窗（9 步 + 竞速收口，A 侧受控 + B 侧活体对照）。
- **matched**：候选③ 六项不变量双轮全绿；候选① 七项派生 check 双轮全绿（A 四面统一 Basic、B 跨面分裂维持）；候选面② 规则级钉死双轮复现；A schema1 12/12 臂跨版本复现；任务二 A 侧 24 格补全完成；复跑门 PASS（92 格零 flake）。
- **divergence**：候选② B 冷相位 3 臂 CHANGED（D-1 新分歧 + C-b 消失）；catalog 根面/匿名面、bearer 跨面分裂、Accept 406、TTL 工况依赖等分歧项全部带双轮证据固化。
- **unknown**：D-1 分类（BUG vs INTENTIONAL 取舍）待裁；A/B DELETE-后-GET 400 联合漂移（相对 L043 的 404）一句记录、不立案。
