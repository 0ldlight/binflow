# 呈批材料——conductor 待用户裁定，非执行记录

| 项 | 值 |
|---|---|
| 文件 | `reports/ruling-pack-2026-10.md` |
| 日期 | 2026-10-26 |
| 性质 | **呈批材料**——conductor 整理待用户逐项拍板，不含任何已执行动作；本文不翻 matrix/台账状态，不 commit |
| 素材 | BOARD.md 尾部 intake ㉕（2026-09-24）、`docs/ai-engineering/loop-state.yaml`、`docs/compatibility/known-divergence.yaml`（156 条全量解析）、`docs/compatibility/matrix.yaml` summary、`docs/prd/pending-rulings.md` v2.7、`reports/iteration-1553.md`（R14）/ `reports/iteration-1554.md`（R15，origin/develop）、`reports/agents/T-515.md` + `docs/reverse/config-formats.md` |
| 批复方式 | 逐项批，或整包「按建议执行」（例外项点名即可）；裁定后回写路径见 §5 |

---

## 一、总览：两块积压

1. **D1–D5 五项基建/流程决策**（2026-09-24 intake ㉕ 立项时挂起，至今一个月未裁）。未阻塞子项一直按已公布默认推进；其中 D2 经 2026-10-26 基建清理后**事实已消解，建议直接销项**。
2. **兼容台账开放面**：Z=156 = 93 resolved + **63 open（UNKNOWN 44 / INTENTIONAL 10 / BUG 7 / UNSUPPORTED 2）**（对账见 §四）。其中真正的用户决策面比「63」小得多：INTENTIONAL 10 条中 9 条 gate 已载事实裁定（存证簿记），UNSUPPORTED 2 条均已定谳维持，BUG 7 条均有工程票在排队——**真正等待用户/authority 裁定的是 UNKNOWN 中的约 30 余席**，其中 34 行已在 `docs/prd/pending-rulings.md` v2.7 在册待批（11 行涉用户 authority），另有 v2.7 之后新增的未入席位。

---

## 二、D1–D5 逐项呈批

### D1 配置格式策略

- **背景**：Phase A 票 T5 已交付 `docs/reverse/config-formats.md` 全量行为规格（T-515，2026-09-24）。T-515 handoff 明确：**§3 优先级双轴（env > 服务段 > shared > 默认 + JVM -D 轴）、§8/§9 导入语义（config.xml POST 全量=替换、带旧 revision 三方合并、PATCH YAML 合并 `~`=删键）、§13 etc→DB 文件同步器**是 BinFlow 设计必须「对齐或显式分歧」三选一的三个面。规格置信度高（~62 高），但 sysconf 内核未反编译，env 映射/插值等仅官方文档单源（中置信）。
- **选项**：
  - A 全面对齐：三面全实现（含启动文件族加载时序、同步器 daemon）——成本最高，且 sysconf 边界面只能按官方文档实现、差分验证受 D3 阻塞。
  - B 子集对齐（**建议**）：REST 暴露面优先——§14 端点表（GET/POST/PATCH/PUT /api/system/configuration 等 12 端点）+ §9.3 replace/merge REST 语义先契约化并实现；启动文件族（system.yaml/ha-node.properties/config.xml 加载时序、§13 同步器）降级为文档兼容+按需实现。
  - C 显式分歧：只做自有配置面，§14 REST 面登记 INTENTIONAL——偏离「对标 Artifactory」目标最远，迁移场景受损。
- **已公布默认（现状）**：规格先行已完成，实现票未开——事实上处于 B 轨迹的起点。
- **影响面**：配置域实现票群（compatibility-engineer 已收到 §14 端点表 + §9.3 语义的契约化 handoff）；`rest/repo-config-*` 族台账条目的最终归依。
- **建议**：批 B，并授权 compatibility-engineer 先行契约化 §14+§9.3。

### D2 skill 栈去重 —— **建议直接销项**

- **背景（2026-09-24 立项时）**：Claude 工具链存在 claude-flow 死配置、三个死插件条目（ponytail / agent-skills / obsidian）、superpowers 需重装、`~/.agents/skills` 17 个 skill 未上架 Claude Code。
- **2026-10-26 现状（已全部完成，见 loop-state.yaml `infra_fixes_applied`）**：settings.json 清除 claude-flow 死配置与三个死插件条目；superpowers 7.0.1 重装（obra marketplace）+ gopls-lsp 安装；17 个 skill 全部软链上架 `~/.claude/skills/`（fern-docs 例外，用户级本有实体）；新增 `/design-review` 命令（intake ㉔ 审查门解冻）。
- **建议**：**销项，无需裁定**。剩余两件用户侧小事不在 D2 范围内、另列 §五：linear-server MCP OAuth 首连（交互式授权）、jfrog-fly 本地代理（127.0.0.1:33555）来源确认/修复方式。
- **影响面**：无排队票。

### D3 REF 凭据通道 —— **当前最大单点阻塞**

- **背景**：兼容基准 2026-09-24 切至 **7.161.26 Enterprise+（http://192.168.120.38:8082）**；UI 200 实测但 `system/version` API **401 未认证 → 活体复验 BLOCKED**。matrix 全部既有行（✅95/◐17/超集 12）按旧参照 7.161.15 认证，**翻绿与新结论均须按 7.161.26 复验**——凭据不到位，复验持续挂起，且未知基准版本差异是否推翻既有结论。
- **选项**：
  - A 只读测试账号 + env 注入（**建议**）：用户提供只读账号 → `BINFLOW_REF_USER`/`BINFLOW_REF_TOKEN`（iteration-1539 B1 原案；差分六 env A_BASE/B_BASE/A_USER/A_PASSWORD/B_USER/B_PASSWORD 同步注入）。权限最小化，符合「凭据不落盘」红线。
  - B admin 账号 env 注入（现状通道）：`ARTIFACTORY_REF_PASSWORD` 已在 env 通道但对该实例 401——疑新实例未配置该密码或密码未交付。admin 仅在需要配置态翻转取证（如匿名双态锚契约，R-2/R-22b 同款）时必要。
  - C 凭据管理器/密钥服务：引入新基建，短期成本高。
- **已公布默认（现状）**：env 注入 + 禁落盘红线（intake ㉕ ②）；但**凭据本身未到位**。
- **影响面**：首轮真实差分、7.161.26 活体复验、T-524 裁定、全部「翻绿须复验」行——差分 QA 整条链的硬前置。
- **建议**：批 A（只读账号交付 conductor 注入 env）；如差分需要配置态翻转取证，另授一次性 admin 凭据窗口。

### D4 .131 docker 豁免

- **背景**：Jenkins 岛 VM（172.16.58.131）不可达、待开机（多轮战报 pending 项）。docker 纪律现状=一律远程执行（ssh lzw@172.16.58.130），本地 docker 禁用；Jenkins 侧验证（dind 类腿 t104、真实 docker 客户端腿在 Jenkins 执行）持续 BLOCKED。
- **选项**：
  - A 开机 + docker 豁免：Jenkins 岛获 docker 使用权（dind/客户端腿本岛执行）。
  - B 开机不豁免（**建议**）：.131 只做调度/轻验证，docker 统一走 .130 远程通道——与现行纪律一致，面最小。
  - C 维持关机：Jenkins 相关验证持续 BLOCKED（CircleCI nightly 是另一通道，不受阻）。
- **已公布默认（现状）**：.131 关机，相关验证 BLOCKED 维持。
- **影响面**：Jenkins 相关验证票；不影响 CircleCI 五腿协议矩阵与差分主链。
- **建议**：批 B；若 Jenkins 岛定位含 docker 客户端腿执行，再升级 A。

### D5 DB 实例资源（多 DB 矩阵）

- **背景**：dba-engineer（域 T，intake ㉕ 扩编）多 DB 验证硬要求；`BIN-13`（MySQL 票）因无 MySQL 镜像 **blocked**（多轮战报用户侧待办项）；PG 路径已在 dev 栈（ADR-0007 双方言义务：SQLite/PostgreSQL）。
- **选项**：
  - A 全矩阵实例：按 DB 类型/版本矩阵逐型供真实实例——资源投入最大。
  - B 最小集（**建议**）：补 MySQL 镜像解 BIN-13（满足双方言硬门），其余 DB 按需追加。
  - C 维持现状：BIN-13 持续 blocked，DB 矩阵验证面收窄。
- **已公布默认（现状）**：C 在生效（BIN-13 挂起，PG 主线推进）。
- **影响面**：BIN-13、DB 矩阵迁移面、schema 版本化与升级恢复验证票（域 T 整域排程）。
- **建议**：批 B（MySQL 镜像来源由用户指定——本机/.130 compose 或内网 registry）。

---

## 三、兼容裁定开放面：对账与结构

### 对账（三源一致）

| 口径 | 数值 |
|---|---|
| 台账总条目 Z | **156** |
| resolved | **93** |
| open 合计 | **63** |
| open = UNKNOWN | **44** |
| open = INTENTIONAL | **10** |
| open = BUG | **7** |
| open = UNSUPPORTED | **2** |

对账式 **63 = 44 + 10 + 7 + 2**，与 iteration-1554（R15 出口 seal「156 = 93 resolved + 63 open」）及 yaml 全量解析实测一致。matrix 主账另计：201 行 = compatible 95 / partial 17 / absent 57 / not_applicable 20 / superset 12。

### 结构分析（把 63 缩小到真决策面）

- **INTENTIONAL 10 open**：9 条 gate 已载事实裁定（`resolved（parity…）`/`resolved-by-design`/`resolved-by-decision`/`已兑现闭合`/`存证`）——属**簿记翻面**（补 resolved 键即可），非用户决策；唯 `rest/permissions-v1-managedxraymeta-seat`（mxm 席位）真正未决，但已明确「xray 域进场时回补」，无排程压力。
- **UNSUPPORTED 2 open**：`goproxy/sumdb`（维持不做已定谳）+ `api/docker-direct-face`（gate 已写 resolved 注册即收口）——均无需裁定。
- **BUG 7 open**：均有工程票在排队（docker/remote-cache-layout 设计票、search 两微票、maven/bare-directory-get 差分腿、storage/deploy-201 envelope 族票、generic/error-face-ct-charset〔BIN-102 已修待簿记收口〕）；唯 `rest/properties-root-posture` 需裁定（= R-16 席位，见下表 #10）。
- **UNKNOWN 44 open = 真裁定面**：authority=pending 31 条；其中 34 行已在 pending-rulings v2.7 在册（含 R-15a/b 等 P0 席），**用户自 LOOP 002/003 起未批复，最早席位限期已逾期一个月**；其余为 v2.7 后（LOOP 026–028 / R13–R15）新增未入席。

---

## 四、Top 20 呈批排序（每条：差异 / 两向 / 建议）

前 16 条为 v2.7 在册席（完整素材以 `docs/prd/pending-rulings.md` v2.7 表为权威）；#17–#20 为 v2.7 后新增未入席。

| # | 条目（席位） | 差异是什么 | 两个方向 | 建议 |
|---|---|---|---|---|
| 1 | R-22b 全域匿名读默认策略 | B 默认匿名读**开**（ADR-0009）vs 参照实例全域 deny | 甲：翻 ADR-0009 严化（breaking：现有匿名消费方断）；乙：维持+INTENTIONAL，差分以配置态收敛 | **乙**（PM 倾向；与 R-2 同构可并批；ADR 翻转属用户级） |
| 2 | R-3 SSRF 私网上游闸 | B 默认拒私网上游（显式键放行）vs A 无此闸 | 甲：维持更严立场 INTENTIONAL+ADR；乙：对齐放开 | **甲**（安全优先；显式键已服务内网级联场景） |
| 3 | R-1 docker token TTL/形态（联裁 revoked 第四臂） | B `{token(不透明),…,expires_in:2592000}` vs A `{token(JWT),expires_in:9000}`；另 B revoke=删行致「revoked」第四臂不可达 | 甲：对齐 TTL=9000 灭 DIVERGENT 主因+安全收紧（×288 泄露窗口）；乙：维持 30 天 | **甲**（token 内形与超集键保留；第四臂随删行模型按模型级 INTENTIONAL 联裁） |
| 4 | R-22c 登录失败限流 | A 连续坏凭据第 3 次起 403 倒计时封锁 vs B 无机制（4 次全裸 401） | 甲：补安全能力（自有形态，security 票定阈值/文案/冷却窗）；乙：缺位登记 | **甲**（变体；「不做限流」无产品立场支撑） |
| 5 | R-15a `?propertiesXml`（P0） | api 面 404+信封、**file 面静默吞参回原始字节** vs A 双面 200 XML（带/无声明） | 甲：双面对齐（探针已钉 wire，成本≈两个序列化分支）；乙：维持 | **甲**（file 面吞参是未登记的真实 wire 差异，宜早收） |
| 6 | R-15b `?lastModified`（P0） | B 404 not-implemented vs A 200 子树 max(lastModified)（官方端点，CI 增量轮询真实消费） | 甲：实现（窄投影，`?list&deep` 已 32/32 SAME 底座在）；乙：维持 | **甲**（缺位理由已被成本估计推翻） |
| 7 | R-12 security 读族门 | B readonly_admin 可读（CapSecurityRead）vs 参照无 read-only admin 主体 | 甲：维持超集 INTENTIONAL（补引 ADR-0026 转正）；乙：收窄 admin-only | **甲**（收窄=翻转既有组合且伤 console 只读视图） |
| 8 | R-13 建用户默认组 readers | A 建用户自动入 readers 组 vs B `groups:[]` | 甲：引入默认组语义（建模票）；乙：缺位 INTENTIONAL | **甲**（迁移用户静默丢默认权限心智是真实依赖） |
| 9 | R-14 anonymous 用户行 | A 有 anonymous 行（200）vs B 404 | 甲：只读虚拟行最小面（不进 DB、不可编辑）；乙：维持 404 | **甲**（写面行为未取证，实现票内先补取证） |
| 10 | R-16 properties 根姿态（BUG） | A 三动词打仓库根开放（PUT 204 递归 fan-out）vs B 一律 400 | 甲：面扩张对齐（解锁 includeRootPath enrich 臂）；乙：维持 | **甲**（双端活体取证在案） |
| 11 | R-24c version 串策略 | B 回 `'dev'` → jf CLI 数值解析断裂 exit=1（服务端主链已全绿）vs A 数值串 | 甲：数值回落正式化（jf 全链收口最后一格）；乙：维持 dev 姿态+债登记 | **甲**（发布实例另有正式版本号通道，不冲突） |
| 12 | R-23a build media-type label | A text 面**错标** application/json + json 面 vendor 型 vs B 标注更正确但无 vendor 型 | 甲：照抄错标+补 vendor 型（parity 单口径）；乙：维持正确标签 INTENTIONAL | **甲**（除非用户裁「正确性优先于 parity」；联动 matrix D07-R04/R08/R10 翻绿） |
| 13 | R-24b maven 非 GAV 上传容忍 | A 201 自由接受 vs B 400 布局强制 | 甲：对齐放开；乙：维持 INTENTIONAL（布局卫生，需 ADR） | **乙**（真实客户端恒发 GAV 不踩该面；「更严」是产品增益） |
| 14 | R-24a listFiles 行形参照漂移 | **A 跨日自矛盾两形**（两键+尾逗号 vs 四键严格 JSON，od 级双语料） | 甲：采四键严格形（较新实况）；乙：采怪癖形（B 现实现零改动） | **取证先行**（第二参照窗 od 取证后再裁，不预设——ADR-0001） |
| 15 | R-10 仓配置非法值三臂 | mistyped 数字 A=500 vs B=400；宽容布尔 A=接受转换 vs B=400；未知布局名 A=400 vs B=静默存读 | a/b 维持 400 INTENTIONAL；c 对齐 400 白名单校验（26 内置布局名冻结名单） | **a/b 维持、c 对齐**（500 系参照异常泄漏，复刻诱发规范客户端重试错误） |
| 16 | R-22a auth401 措辞族 | 同码异文跨域族（A `Bad Credentials`/`Authentication is required` vs B 统一 `invalid credentials`——单渲染点） | 甲：对齐 A 全文案族（一处渲染点收三域）；乙：维持统一 INTENTIONAL（client-blind） | **甲**（与 R-25b w08 同构口径，可并批） |
| 17 | pypi/simple-index-href-target-layout（未入席） | A href 指存储布局路径 vs B 指 `/packages/` 协议面（doc.go 有意为之；下载可用性双端 PASS） | 甲：doc.go 声明升格 INTENTIONAL authority；乙：href 对齐 A 存储布局 | **规格票带 pip 客户端取证后裁**（涉及 pip 解析语义，不盲裁） |
| 18 | nuget/v2-push-root-storage-layout（未入席） | v2 ROOT 落地 A 平铺 `<pkg>.nupkg` vs B `<id>/<ver>/` 嵌套 | 甲：对齐翻正（**存量数据破坏性变更面**，需迁移方案）；乙：维持 INTENTIONAL | **先定存量迁移成本再裁方向**（charset 微差面同拍补证） |
| 19 | npm/virtual-member-ledger-desync-after-delete（未入席） | B DELETE 成员仓后 virtual 配置回显仍显旧成员清单（ledger 失同步，聚合静默退化） | 甲：差分腿对拍→定 BUG 开单源化/联动清理票；乙：按参照语义登记 | **甲先探**（疑 BUG 面优先于裁定面） |
| 20 | helm/populated-index-body-form（未入席） | A index `urls: local://` 形态**自身令 helm pull 报 scheme not supported**（方向存疑非单向对齐）vs B 相对文件名（pull 亦败、根因未隔离） | 甲：对齐 A 体形；乙：维持 B（含反向裁定可能） | **先隔离 B 侧 pull 根因再裁**（R16 探腿票在排） |

**未入 Top 的开放条目去向**：其余 UNKNOWN 多为「探腿先行」在途工程票（R16 排队的 pypi 仓根 DELETE 204 语义、maven folder 拼写 DELETE、virtual DELETE 三态、generic/virtual-metadata-synthesis、helm chart-put-201-face / index-200-header-set、maven/checksum-oc 写穿、generic/repo-cache-deploy-face、d01/d02/d08 normalize 族、docker remote token 匿名策略〔并入 R-2/R-22b 批〕等）——裁定的证据前置未齐，不进本次呈批窗；conductor 按 R16 探腿结果滚动补席。

---

## 五、裁定后的回写路径（供批复时一并确认）

- pending-rulings 席位 → 回写 `docs/prd/pending-rulings.md` 表 + 关联账目（known-divergence 分类翻面 / matrix 行态 / 契约条目 / gap 总账），不另建副本（维护者=product-manager 唯写）。
- 用户级 INTENTIONAL（R-3/R-22b 乙案等）→ ADR 登记（DECISIONS.md，architect 只追加+Errata）。
- 簿记翻面批次（§三 所列 INTENTIONAL 9 条 + UNSUPPORTED 2 条 + generic/error-face 等已修待收口条）→ compatibility-engineer 一次台账批补 resolved 键（不含本材料执行，须另派票）。

---

## 六、用户最少需要回答的 7 个问题

1. **D1**：配置格式策略批 B（REST 暴露面先行、启动文件族文档兼容）？
2. **D2**：skill 栈去重确认销项（2026-10-26 已清理完毕）？
3. **D3**：参照实例（192.168.120.38:8082）凭据按只读测试账号 env 注入交付（`BINFLOW_REF_USER`/`BINFLOW_REF_TOKEN`）？
4. **D4**：.131 Jenkins 岛开不开机、docker 豁免给不给（建议：开机不豁免，docker 走 .130）？
5. **D5**：DB 最小集批不批（补 MySQL 镜像解 BIN-13；镜像来源请指定）？
6. **pending-rulings v2.7**：34 个待批行整包「按建议执行」，还是逐项批？（整包时请点名例外；R-18〔npm login 缺字段 400 vs 401〕为真两可席，PM 未给建议，需单独裁）
7. **Top #17–#20 四条新增未入席**：批「按建议路径推进」（规格票/差分腿先行），还是留 R16 探腿结果滚动补席？

另两件用户侧小事（非裁定，随批复顺手处理即可）：linear-server MCP OAuth 首连（交互式授权）；jfrog-fly 本地代理（127.0.0.1:33555）来源确认与重启方式。

---

*本材料由 conductor 侧会话依据 2026-10-26 仓库现状整理；素材版本：main@1901da8e（#200 后）+ origin/develop iteration-1554。只读分析，未改任何台账/matrix 状态。*
