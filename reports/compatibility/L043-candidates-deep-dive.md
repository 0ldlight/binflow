# L043 — 五候选活体深挖：定向探针、双端对照与语义定性（wave-2 分歧候选收口材料）

- 日期：2026-10-29；模式 **dual 定向探针**（每候选独立脚本、双端同发、证据逐臂落盘 `run/l043d/`）
- **执行域**：worktree `t634-count`（分支 `feat/t634-count`，本地提交不推远端）
- **A 面**：JFrog Artifactory **7.161.26** @ `http://192.168.120.36:32082`（docker v2 面根路径 `/v2/*`，REST `/artifactory/api/*`；凭据 gitignored `.env.ref` 行首锚定注入，零落盘零打印）
- **B 面**：BinFlow **dev-cfa5da05** @ `http://192.168.120.36:32083`（docker v2 面根路径 `/v2/*`，REST `/binflow/api/*`；凭据 `.env.lzw`）
- 上游夹具：中继 `:32100`（zot 系）镜像 `difftest-docker/smoke`；remote 仓 `diff-docker-remote`（**本波重建**，见 §0）
- 对照基准锚定：A=7.161.26（2026-09-24 总令基准，本波活体复验在案）；B=dev-cfa5da05（T-633 接线合入点，早于 T-634/635/636 分支提交——熔断相关判定沿用 wave-2 的 B 活体口径，本波未复测熔断参数面）

---

## §0 环境事件：wave-2 夹具拆除与重建（p0，先行事实）

会话开始复跑 wave-2 候选取证时发现：**wave-2 夹具 remote 仓 `diff-docker-remote` 双端均已不存在**（A=404 `"Repository 'diff-docker-remote' not found"`；B=404 `NAME_UNKNOWN`）。wave-2（10-28）报告时双端 HEAD 200 在案——夹具在两波之间被拆除（谁拆未考，环境事实记录）。

处置：`l043d_refixture.py`（p0）幂等 PUT 双端 remote 仓（B 侧带 `allowPrivateUpstream:true`）→ 双端 200；t1 manifest GET 双端 200（CT=schema2）；tags/list 双端 200 活体（15 tags，含上游历波残留 nc-\*/w3-\* 全集）。**本报告全部判定基于重建后的活体拓扑。**

（证据：`run/l043d/p0_refixture.json`）

---

## §1 候选③ catalog 根面：多拓扑 + 匿名三态对照（docker/registry-catalog-bare-root）

探针 `l043d_catalog_topology.py`（p1）；证据 `run/l043d/p1_catalog_topology.json`。

review_gate 要求的「A 侧多拓扑补证」三臂全部落定：

| 臂 | A（7.161.26） | B（dev-cfa5da05） | 判读 |
|---|---|---|---|
| 拓扑基线 | REST 列 **93 仓** | REST 列 1 仓（重建的 remote） | 拓扑非空 |
| 根面 `/v2/_catalog`（认证） | 200 **空 body 零 CT** | 200 JSON `["diff-docker-remote/difftest-docker/smoke"]`（仅 remote 成员，repo 限定形） | 分歧维持 |
| 根面 + `?n=1000` | 200 空 body 零 CT（同形） | 200 同上 | 分页假设排除 |
| **93 仓在场时根面** | **仍恒空**（空 body 零 CT） | 列 remote 成员 | **A 恒空 = 参照固有姿态，非拓扑/索引/权限偶发** |
| 新建空 local 仓后根面 | 仍空（新仓不入） | **仍只列 remote 成员（空 local 不入，has_new=False）** | B 的根面=remote 成员聚合，非全仓枚举 |
| 仓限定 `/v2/{repo}/_catalog` | 200，6 成员 | 200，**逐字同 6 成员** | L003-2 resolved「活体聚合一致」复证（成员：`binflow`、`difftest-docker/smoke`、`library/alpine`、`mirror/library/{alpine,golang,node}`） |
| 匿名根面 | **401** `{"errors":[{"code":"UNAUTHORIZED",...}]}` | **200** 成员 JSON | **新分歧臂：B 匿名泄漏仓存在性** |
| 匿名仓面 tags/list | 401 | 404 | B 匿名 404（不泄漏），与自身根面 200 不一 |

**语义定性**：
1. A 根面恒空在多拓扑（93 仓、含 docker local/remote/virtual 全型）+ 分页参数 + 空仓新建四象限下成立——**参照特有姿态定谳**（wave-2 的「订阅/权限过滤空集或根面不列 remote」两假设中，「恒空、与拓扑无关」成立，具体过滤机制在参照黑盒内不可再分）。
2. **B 根面=remote 成员聚合（而非 distribution 惯例的全仓枚举）**——修正 wave-2 案①论证里「B 聚合 remote 成员是 distribution spec 惯例形态」的表述：B 并不枚举 local 仓（空 local 不入），行为是「remote 上游镜像以 repo 限定形入目录」的特定聚合，非全量 catalog。
3. 新发现 **B 自身匿名姿态不一**：根面匿名 200（枚举泄漏）vs 仓面匿名 404（隐藏）——若案①（B 维持现状）获裁，建议并裁匿名根面的枚举泄漏面（安全相关，向 compat/安全侧提示）。

---

## §2 候选② schema1 协商：四向矩阵 + C-b 缓存污染定谳（docker/remote-schema1-conversion）

探针 `l043d_schema1_phase.py`（p2）；证据 `run/l043d/p2_schema1_phase.json`。方法：t1 以 schema2 Accept prime 规一化热相位 → 热矩阵四臂；上游新推 d1/d2 两 nonce tag（t2 manifest 字节拷贝，201）→ 冷取相位双向矩阵。

### 热相位（t1，缓存行=schema2）

| 臂 | Accept | A | B | 判读 |
|---|---|---|---|---|
| H1 | schema1 | 200 **prettyjws**（服务时转换） | 200 schema2 原样 | wave-2 相位复现 |
| H2/H2b | schema2 ×2 | 200 schema2（**转换后缓存行无污染**） | 200 schema2 | A serve-time 转换证明 |
| H3 | （无） | 200 **prettyjws（默认服务形）** | 200 schema2 | A 无 Accept 默认=prettyjws（三向一致，新钉事实） |
| H4 | schema1+schema2 混合 | 200 schema2 | 200 schema2 | wave-2 SAME 复现 |

### 冷取相位（d1：先 s1；d2：先 s2）

| 臂 | tag | Accept | A | B | 判读 |
|---|---|---|---|---|---|
| D1 | d1（冷） | schema1 | 200 prettyjws | 200 **prettyjws**（上游 zot 转换形随 Accept 拉回并入库） | 冷取参与协商复现 |
| **D2** | d1（热） | **schema2** | 200 **schema2**（相位无关） | 200 **prettyjws** | **C-b 定谳：B 缓存行被冷 s1 拉取覆写——schema2 客户端拿到 prettyjws** |
| D3 | d1（热） | （无） | 200 prettyjws | 200 prettyjws | 污染行持续服务 |
| E1 | d2（冷） | schema2 | 200 schema2 | 200 schema2 | 对照冷取 |
| E2 | d2（热） | schema1 | 200 prettyjws（照转） | 200 **schema2**（热行无视 Accept） | 反向不转换 |
| E3 | d2（热） | （无） | 200 prettyjws | 200 schema2 | 同上 |

**定谳（upgrade：UNKNOWN → 事实齐备，待裁）**：
- **A = 逐请求确定性协商**：`{schema1, 无 Accept} → prettyjws（服务时在线降转换）`；`{schema2, 混合} → schema2`；缓存行全程透明（H2b、D2 三重证明）。12 臂无一例外。
- **B = 冷取协商一次入库（活体定谳）**：缓存行=首次冷取的协商结果，此后无视 Accept 原样服务；**C-b=TRUE**——冷 schema1 拉取把 prettyjws 写入缓存行，后续 schema2/无 Accept 客户端全部拿到 prettyjws（D2/D3），且跨请求持续。
- 客户端可见后果（比 wave-2 更重的一档）：B 上**先到者定形**——若 CI 冷启动先以 legacy/探测请求（s1 或无 Accept）触达，后续全量 schema2 客户端在同一缓存窗内全部破面（digest 不匹配/解析失败），且随缓存过期相位翻转。这把「非确定性」从「跨相位漂移」具体化为「**同窗内先到请求决定全体客户端所见**」。

---

## §3 候选① bearer 失败形态：跨面挑战矩阵（docker/bearer-failure-shape）

探针 `l043d_bearer_matrix.py`（p3）；证据 `run/l043d/p3_bearer_matrix.json`。矩阵=4 面 × 6 臂 × 双端（anon / bad-basic（一次性用户名）/ garbage-bearer / alg:none 伪 JWT / 截 40% 真 token / 空 bearer）。防熔断污染设计：anon 臂先打、bad-basic 每发独立一次性用户名、面间 4.5s 静默（>4s 窗）、403 显式标记 breaker-suspect。

### 挑战机制矩阵（401 响应的 WWW-Authenticate）

| 面 | A | B | 判读 |
|---|---|---|---|
| F1 `/v2/` ping（凭据在场臂） | Basic `realm="Artifactory Realm"` | Basic `realm="Artifactory Realm"` | **同指纹**（wave-1 P2 清偿态复验） |
| F2 manifest GET | Basic `realm="Artifactory Realm"` | **Bearer** `realm="…/v2/token",service=…` | **分歧行**（wave-2 已录） |
| F3 tags/list | Basic | **Bearer** | **分歧行扩展至 tags 面**（wave-2 未测） |
| F4 REST `/api/system/ping` | Basic `realm="Artifactory Realm"` | Basic `realm="BinFlow Realm"` | **realm 字符串分歧行（新）**——B 与参照同机制但品牌名泄漏，且与自身 F1 面的 Artifactory Realm 不一 |
| anon（无凭据）F1-F3 | Bearer 挑战（docker 标准首腿） | Bearer 挑战（F1）；F2/F3 匿名直接 200（见下） | anon 首腿双端同构（F1） |

### 文案分类矩阵（401/403 body）

| 面×臂 | A | B | 判读 |
|---|---|---|---|
| F1 bad-basic | `"Bad Credentials"`（Jackson pretty） | 逐字同（方言空格差异） | 同分类 |
| F1 bearer 三臂 | `"Props Authentication Token not found"` | 逐字同 | 同分类——**B 在 ping 面对 bearer 臂也答 Props 文案+Basic 挑战（凭据类无感）** |
| F2 bad-basic | `"Bad Credentials"` | 泛化 `{"code":"UNAUTHORIZED","message":"authentication required"}` | 分歧行（wave-2 已录） |
| F2 bearer 三臂 | `"Props Authentication Token not found"` | 同上泛化（与 anon 臂同体） | 分歧行——**B 的 manifest 面把一切失败塌缩为匿名信封，凭据类信息零保留** |
| F4 全臂 | 分类同 F1 | 第三形态 `"invalid credentials"` | REST 域方言（已知域，记录不改判） |

### 新侧写事实（本波首证）

1. **A 的 REST `/artifactory/api/system/ping` 匿名 200**（无凭据放行，坏凭据才 401）——熔断面基线补充（wave-2 阈值实验全走坏凭据，未测无凭据臂）。
2. **A bearer 通道失败计入熔断（n=1）**：F1 面 garbage→algnone→truncated→empty 四连发（~1s 间隔），第 4 发得 **403 熔断文案**，面间静默 4.5s 后自愈 401——与 bad-creds「第 3 发拒绝」线口径相容（bearer 失败与 Basic 失败是否同 keyed 池未能区分，n=1 不立断言，供熔断参数票参考）。
3. **B 匿名开放 repo 面**：F2/F3 匿名 200（manifest/tags 无凭据可读）。与 A（匿名 401 挑战）构成匿名姿态分歧。部署面 `allowAnonymous` 配置差异与代码语义在活体上不可分——**环境面记录，不作代码断言**；若 B 目标是参照同构，匿名默认值应并裁。
4. token 形态：A 签发 806B JWT；B 签发 64B 不透明串。截 40% 篡改双端均正确 401（token 值零落盘，仅记 len+sha256 前 16 hex）。

---

## §4 候选面① remote tags/list 新鲜度：缓存 TTL 窗口（docker/remote-tags-list-freshness-and-406 面①）

探针 `l043d_tags_freshness_ttl.py`（p4，长跑后台）；证据 `run/l043d/p4_tags_freshness_ttl.json`（逐轮落盘，50 轮）。协议：T0 prime A 视图（19:17:12，视图 15 tags 不含新 tag）→ T0+30s 上游推 nonce tag `tf1a18760d2db`（201）→ T0+35s B 活体复核（即时可见）→ 每 180s 轮询 A 至 tag 现身。

**实测（单样本全窗）**：

| 事实 | 数值 |
|---|---|
| 窗口内轮询 | 49 轮（+216s～+8873s）全部 `a_n=15`（陈旧视图），**窗口内持续探测不提前刷新**（惰性刷新语义活体复证） |
| tag 现身 | **+9053s（21:48:06）**，`a_n=16`；上轮 +8873s 仍缺 → 刷新发生于 (21:44:46, 21:48:06] 且由届满后首个请求触发 |
| **TTL（T0 锚）** | **∈ [8873, 9053]s ≈ 147.9–150.9 min（≈2.5h）** |
| TTL（pre-window 锚修正） | T0 前最近成功 tags/list 刷新：19:10:05（p0 warm）/ 19:10:34（p5 收尾）；若 T0 为 cache hit（TTL>398s 时必然），锚=19:10:34 → TTL ∈ [9271, 9451]s ≈ **154.5–157.5 min** |
| B 对照 | 50 轮全程 `b=True`（活体聚合，上游 push 后当轮可见） |
| 现身持续性 | 现身后立即复验 has_tag=True（刷新闩锁） |

**语义定性（对 wave-2 口径的更新）**：wave-2 的「粘滞 ≥55min、18:52 追平（分钟~小时级）」由本波收敛为**实测窗口 ≈2.5h**（T0 锚 147.9–150.9min；命中修正锚 154.5–157.5min；单样本，跨日相位未采样）。刷新语义=惰性（届满后首个请求触发，窗口内任意请求不续期不刷新——与 wave-2 熔断窗口的「不续期」形态同族）。CI push→立查工作流在参照上的不可见窗口为 ~2.5h 量级；B 活体为 client-blind 超集（面①案②维持 B 的论证材料：参照的陈旧性是缺陷形态，主动仿效为客户端可见面负向）。注意 ~2.5h 与 Artifactory 常见 remote 缓存缺省 7200s（2h）不吻合——参照实例该参数可能为自定义值或锚定机制不同（黑盒内不可再分，单样本不外推）。

---

## §5 候选面② tags/list 带 manifest Accept：A 406 / B 200 的规则级钉死（同条目面②）

探针 `l043d_tags_accept_406.py`（p5）；证据 `run/l043d/p5_tags_accept_matrix.json`。9 臂（none / application/json / schema2 / manifest.list / schema1 / oci-index / \*/\* / 混合 s2+json / text/html）。

**A 的协商规则（9 臂钉死）**：`406 ⇔ Accept 头存在且既不含 application/json 也不含 */*`。

| 臂 | A | B |
|---|---|---|
| 无 Accept / application/json / \*/\* / 混合（s2+json） | **200** | 200 |
| schema2 / manifest.list / schema1 / oci-index / **text/html** | **406** `{"errors":[{"status":406,"message":"Not Acceptable"}]}`（Jackson pretty，CT application/json） | 200 |

**语义定性**：A 的 406 与 docker 媒体类型族无关（text/html 同 406）——A 把 tags/list 当 `application/json` 资源做**严格 Accept 包含检查**；混合头只要含 json/\*/\* 即放行。B 九臂全 200 无视 Accept。分歧是**存在性级**：任何携带单值 manifest Accept 的 tags/list 探测在 A 上必 406。真实客户端影响面：distribution 客户端 list 调用通常不带 Accept（不破）；cosign/notation 类工具若带 manifest Accept 探测 tags 在 A 上会 406（参照自身姿态）——对齐 A 即复制该姿态，维持 B 即宽松超集。裁定材料齐备（可与协商族 docker/remote-schema1-conversion 同批裁）。

---

## §6 残留与清理

- A/B 侧：p1 建仓 `diff-l043d-1a1875b1faf` 双端 DELETE 200（residue 复核脚本 `l043d_residue_check.py` 收尾复跑）；p0 重建的 `diff-docker-remote` 为 wave-2 声明的标准夹具（声明留存，非残留）。
- 上游中继（DELETE 405，手动 GC 清单结转）：本波新增 `d1l043d1a1875f69e1`、`d2l043d1a1875f69e1`（p2）、`tf1a18760d2db`（p4）；residue 复核（`l043d_residue_check.py`，收口时点）：A 93 仓/B 1 仓均无测试残留键，上游 16 tags 中 13 项在册（含历波 nc-\*/w3-\* 存量）。
- 凭据纪律：全程 env 文件行首锚定读取、仅入 Authorization 头；证据体仅 status/CT/WWW-Authenticate/body 头部；token 仅 len+sha256 前缀。

## §7 known-divergence.yaml 候选条目 evidence 补充（待 compatibility-engineer 并回权威副本）

五条目正文（含 rationale/authority/review_gate）以兼容工程侧副本为权威（本分支 yaml 无该五条，避免双处漂移不整段搬运）。**逐条 evidence 字段追加**：

1. `docker/registry-catalog-bare-root` evidence 追加：`+ run/l043d/p1_catalog_topology.json（多拓扑定谳：A 93 仓在场根面仍恒空 body 零 CT、?n=1000 同形、空 local 不入、匿名 401 vs B 匿名 200；B 根面=remote 成员聚合非全仓枚举——案①论证修正）`
2. `docker/remote-unknown-image-tags-list` evidence 追加：`+ 本波无新证据（夹具重建后未复测该臂；wave2 arm3 维持最近证据）`（注：p0 重建后未复打该臂，如需复证可在裁定票内以 wave-2 evidence 为准）
3. `docker/remote-schema1-conversion` evidence 追加：`+ run/l043d/p2_schema1_phase.json（B=dev-cfa5da05 活体四向定谳：A 逐请求协商 12 臂无例外、缓存透明；B 冷取协商一次入库且 C-b=TRUE——冷 s1 拉取覆写缓存行，后续 schema2/无 Accept 客户端全得 prettyjws（D2/D3），反向 E2 热 s1 得 schema2；A 无 Accept 默认=prettyjws 新钉）`
4. `docker/bearer-failure-shape` evidence 追加：`+ run/l043d/p3_bearer_matrix.json（4 面×6 臂×双端：A 四面统一 Basic realm="Artifactory Realm"+分类文案；B F1 同指纹/F2 F3 Bearer 挑战+匿名同款泛化信封/F4 Basic realm="BinFlow Realm"（realm 字符串分歧行新）；B 匿名 repo 面 200、A REST ping 匿名 200、A bearer 四连发第 4 发 403（熔断 n=1）三个新侧写）`
5. `docker/remote-tags-list-freshness-and-406` evidence 追加：面② `+ run/l043d/p5_tags_accept_matrix.json（规则级钉死：A 406 ⇔ Accept 存在且不含 application/json 与 */*——与 docker 媒体类型无关（text/html 同 406）；B 九臂全 200）`；面① `+ run/l043d/p4_tags_freshness_ttl.json（50 轮全窗实测：TTL ∈ [8873,9053]s≈147.9–150.9min 自 T0 锚（pre-window 命中修正锚 19:10:34 → 154.5–157.5min）——wave-2「≥55min 粘滞」收敛为 ≈2.5h 精确窗口；窗口内 49 轮持续探测不刷新（惰性刷新活体复证）；B 50 轮全程活体）`

## §8 Z 口径建议（归 compatibility-engineer）

1. 候选②（schema1）与候选面②（406）证据已达「事实齐备、两案并陈待裁」态；候选③ 补证完成（A 恒空定谳）；候选面① TTL 已实测（≈2.5h 精确窗口）；候选① 扩展了 tags 面分歧行 + realm 字符串新分歧子项（可并入本条或拆 `docker/rest-realm-string` 微条目，裁时定）。
2. B 部署版本注记：本波 B=dev-cfa5da05（T-633 合入点）；schema1 活体定谳与 wave-2 adjudication 的 dev-5f7ebec9 复测同构（四向一致），证据跨两个 B 版本成立。
3. 新侧写（匿名姿态 3 项、realm 字符串、bearer 熔断 n=1、A REST ping 匿名 200）建议入 known-divergence 待裁清单或并入既有条目，防散逸。
