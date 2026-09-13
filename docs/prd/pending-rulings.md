# PRD — 用户/产品待裁清单（pending rulings）

| 项 | 值 |
|---|---|
| 文档 | `docs/prd/pending-rulings.md` |
| 版本 | v2.2（L014-1 重呈裁：R-15a 探针翻案三新案 / R-15b 两案重呈+成本估计 + R-15c/d 落格收注 + as-built 漂移纠偏入路径；v2.1=L011-2 增补：票 E+F 四臂入册 R-15〔§2 R-15 + §3/§4 联动〕；v2.0=L008-4a 整编：新增 R-10 三臂 / R-11 / R-12 / R-13 / R-14 + 既有 9 项复核〔§2.0〕+ §4 批量批复式；v1.0=L002-4 首建 9 项） |
| 维护者 | product-manager（唯写）；裁定结果回写本表 + 各关联账目，不另建副本 |
| 裁定通道 | 「用户」类经 conductor 转用户终裁；「产品」类 PM 域内可裁、报 conductor 备案；「ADR」类走 DECISIONS.md 流程 |

## 0. 纪律与图例

- **建议立场 = product-manager 的专业建议，不构成裁定**。每项待对应 authority 终裁后方可回写关联账目（known-divergence / matrix / gap 总账 / 契约）。
- 证据等级：E1=反编译走读（B=7.161.20 partial 源）/ E4=运行时实测（参照实例 Artifactory-pro 7.161.20 rev 86120900，:8082）/ E5=双系统差分（BinFlow UAT :8083；L001-1 复验基线 uat-l0011-f80c46aa，源 rev 59f33ab5；L007-1 独立验证实例 binflow-l0071-verify / uat-l0071-c1193f5f）。
- authority 类型：**用户**（范围/安全/排程终裁）/ **产品**（PM 域内）/ **ADR**（架构记录，需先入 DECISIONS.md）。
- 来源账目：`docs/compatibility/known-divergence.yaml`、`docs/ai-engineering/loop-state.yaml`、`docs/ai-engineering/artifactory-binflow-gap.yaml`、`docs/design/storage-v2.md` §1/§5、`reports/compatibility/L000-docker-remote-*.md`、`reports/agents/T-L001-4.md`；v2 新增素材：`docs/prd/ruling-invalid-value-family.md`（R-10/R-11① 素材，architect 两案对比）、`docs/design/repo-update-merge.md`（ADR-0050 候选稿）、`reports/compatibility/L007-residuals-users-diff.md`（R-12 邻面 / R-13 / R-14）、`reports/compatibility/L003-s3-chain-evidence.md`（R-6 现状）；v2.1 素材（R-15，票 E+F）：`docs/compatibility/matrix.yaml` D01-R03/R04/D02-R01 行注记、`docs/reverse/rest-api.md` §1.1 行 30 / §2 行 85 / §3 行 117、`docs/reverse/api-inventory.yaml` 属性/lastModified 行注记、`docs/reverse/rest-compat-matrix.md` §2 行 3/行 4 + §3 行 1 + 带② 2-1/2-2、`docs/ai-engineering/loop-state.yaml` L011-2 派单；v2.2 素材（R-15a/b 重呈裁）：`reports/compatibility/L013-r15-packument-probes.md` §1（R-15c/d 落格）/§2（R-15a 五臂翻案）+ 双端 wire `reports/compatibility/l0134-wire/{a,b}/r15/`、`internal/httpapi/storage.go` E-26 分支与 `compat_test.go` not-implemented 断言（as-built 404 实态——冻结件 501 记载漂移的源证据）、`docs/ai-engineering/loop-state.yaml` L013-r15c / L013-4+5（落格回执）/ L014-1（重呈裁派单）。
- 批量批复式（v2）：见 §4——用户可逐项批，或整包「按建议执行」。

## 1. 总览表

| 编号 | 事项 | 当前 BinFlow 行为 | Artifactory 参照行为 | 建议立场（PM） | authority | 状态/限期 |
|---|---|---|---|---|---|---|
| R-1 | docker `/v2/token` 签发 TTL 与形态 | `{token(不透明 hex), access_token, expires_in:2592000, issued_at, scope}`（30 天超集） | `{token(JWT), expires_in:9000}`（2.5h，无 issued_at/access_token/scope 键） | **对齐 expires_in=9000**（灭 DIVERGENT 主因 + 安全收紧）；token 内形保留不透明（客户端不解码，吊销链不动），超集键保留 | 产品（TTL/键集）+ 用户（30 天→2.5h 的安全口径确认） | known-divergence UNKNOWN，LOOP 002 限期**已过未升级**——随 v2 呈批；契约 `docker/remote-token-issue` DIVERGENT 待翻 |
| R-2 | remote 面匿名默认姿态（`/v2/token` 无凭据） | 匿名开：200 签发匿名 pull scope token | 参照实例匿名关：401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（pretty） | **维持默认开 + 全局 `anonymous_access` 键管辖 remote 面**（与 local 面 v1.3/C6 同键同语义）；对拍以配置态收敛（参照实例切 anonymous 双态取证后锚契约），wire 默认差异登记 INTENTIONAL（部署策略差异非协议语义） | 产品 | known-divergence UNKNOWN，LOOP 002 限期**已过未升级**——随 v2 呈批；契约 `docker/remote-token-anonymous` DIVERGENT 待翻 |
| R-3 | SSRF 私网上游闸（allowPrivateUpstream） | 默认拒私网上游（per-repo 显式键放行，对拍环境即用此键） | 无此闸：remote 仓 url 可指私网 | **维持更严立场 → INTENTIONAL_DIFFERENCE**：安全（SSRF/横向移动面）优先于行为兼容；保留显式配置键（默认 false）服务内网级联场景；登记 ADR 为 authority | 用户（安全 vs 兼容终裁；建议已明确） | known-divergence UNKNOWN，LOOP 003 限期**已过未升级**——随 v2 呈批 |
| R-4 | HA 链范围 | 无（单节点；servelock，ADR-0002/0004） | HA 心跳/主选举/分布式锁/集群拓扑/任务单节点守卫（wire 协议 H1 UNKNOWN） | **两步走**：① HA-ready 无状态化审查近期做（不锁死、低成本）；② HA 本体最小集（锁/心跳/任务守卫）**进目标但自有形态**（NEW-BUILD 绿地、无 wire 对拍义务——HA 是部署形态非客户端协议），维持 PRODUCT M18+ 候排程，请用户确认排程与最小集边界 | 用户（范围+排程；PRODUCT 2026-09-06 翻案已把 HA 本体放进路线） | gap 总账 `ha-cluster` NEW-BUILD 候选；storage-v2 §1 #17 |
| R-5 | GCS/Azure 云 provider 是否进目标 | 无（S3 已立；binstore.yaml 保留名 `azure`/`gs` 拒启 + Backend 缝已埋，ADR-0036/0019） | GCS/Azure 模板族（google-storage-v2 / azure-blob-storage{,-v2,-archive} 等 8 模板） | **不进目标（维持 UNSUPPORTED 留缝）**：S3 协议是事实标准对象存储兼容面（GCS/Azure 均有 S3 兼容层）；单二进制 <40MB 成功标准与双 SDK 维护成本不支持；Backend 缝保留，强需求再启 | 用户（终裁；建议已明确） | storage-v2 §1 #15（UNSUPPORTED 留缝）；binary-provider-chain §3.2 |
| R-6 | 云重定向/预签名行为 | S3 读全代理（无重定向） | ≥200KB（`cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）且 provider 支持且 `enableSignedUrlRedirect=true` 时下载 302 预签名 URL；URL 有效期 UNKNOWN | **证据依赖，暂不可裁**（维持 evidence-blocked）：L003 三次换链取证均 BLOCKED（TLS 开关真形态未定位，三候选在案，见 §2-R6 增补）；静态面已收（E1 字节码：redirect 默认关 + signedUrlExpirySeconds 键族 + 阈值 204800）；取证前盲裁 = 违反 ADR-0001 | 产品（取证后裁）；当前 evidence-blocked | storage-v2 §1 #18 UNKNOWN 挂账；unknown.yaml U-STG-15/U-STG-09；L003-3b-final 终态 BLOCKED |
| R-7 | C05 remote 缓存树布局 | digest 寻址（`manifests/<hex>` + `blobs/<hex>`），无 tag 目录/marker/sha256__ 命名 | tag 目录 + `sha256__<digest>` 命名 + marker 文件驱动缓存 + `library/` 归一 | **INTENTIONAL 定谳**：客户端协议面零差异（缓存树是服务端内部布局）；BinFlow digest 寻址为存储契约（去重/数据完整性语义更优）；ADR-0047 已拒 marker 逐行翻译（docker_refs 账本等价能力）；唯一可见通道 = REST storage 浏览面（ListFolder），该面若有对拍需求另立票 | 产品（登记）+ 用户确认（仅当有 REST 浏览缓存树的使用场景） | known-divergence `docker/remote-cache-layout` 仍 BUG 待按裁定拆分转 INTENTIONAL；library/ 归一行为臂随 L001-1 已收敛（C12），docker.io 上游前缀行为 LOOP 002 契约重放补证 |
| R-8 | xray-curation REMOVE 候选 | 无（matrix D14 xray 行 ⛔ 登记口径） | Xray curation/apptrust 策略族（Xray addon 联动下游） | **REMOVE 定谳**：Xray 式扫描是 PRODUCT 2026-09-06 用户终裁唯一排除项；curation/apptrust 无 Xray 引擎即无语义，同判归 D14 ⛔ 族；正式登 known-divergence INTENTIONAL_DIFFERENCE（authority=PRODUCT 终裁 + 本裁定票），对应 REST 端点按 DE-16 式边界处理 | 用户（确认 curation/apptrust 归入 Xray 排除族——PRODUCT 终裁字面只点名「Xray 式扫描」，族边界需确认） | gap 总账 `xray-curation` REMOVE 候选；first_loop=开 INTENTIONAL 裁定票（即本项） |
| R-9 | plugins/worker SPI 是否入范围 | 无（matrix D10 ❌1；webhook outbox M13 已交付） | Groovy 用户插件（execution/steps/webhook 类型，进程内执行）+ worker TS serverless 扩展（116 java 模块）+ REST plugin/workers 资源 | **DEPRECATE（不进目标）**：进程内执行用户 Groovy = 供应链攻击面，与 BinFlow 单二进制/最小面立场冲突；等价扩展能力走 webhook + REST API 组合（M13 已交付）；登记 UNSUPPORTED_FEATURE；未来强迁移需求再评估受限插件沙箱另行立项 | 用户（终裁；建议已明确） | gap 总账 `plugins-worker-spi` DEPRECATE 候选；first_loop=开产品裁定票（即本项） |
| R-10a | 仓配置非法值·mistyped 数字（`maxUniqueSnapshots:"seven"`） | 400（decode 报文带字段名——全域统一 decode 姿态，非本域特例） | 500 `Error converting from 'String' to 'Integer' For input string: "seven"`（Java 转换器逐字） | **维持 400，登记 INTENTIONAL**：500 系参照异常泄漏非契约设计；复刻 500 诱发规范客户端重试确定性错误（RFC 9110 §15.5/15.6），逐字文案耦合 JDK 实现细节（clean-room 边缘） | 产品（+ADR 登记） | known-divergence `rest/repo-config-invalid-value-family` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-10b | 仓配置非法值·宽容布尔（`blackedOut:"yes"`） | 400（严格 bool） | 接受并转换（commons-lang 真假值词表 {true,on,yes,y,t,1}/{false,off,no,n,f,0} 落库回显；不可转 "maybe" 400） | **维持 400，登记 INTENTIONAL**：宽容转换静默吞 typo 反噬可调试性；受害脚本形态（字符串填 bool 席位）罕见；折衷备选臂（仅收 "true"/"false" 字符串）列备选不预裁 | 产品（+ADR 登记） | 同 R-10a |
| R-10c | 仓配置非法值·未知布局名（`repoLayoutRef:"no-such-layout"`） | 接受并存读（无布局注册表；K73 已定谳布局 presentation-only） | 400 `Unable to find repository layout by the name: <n>`（create/update 双面逐字） | **对齐 400**：按官方 xsd/文档 26 内置布局名冻结名单做存在性校验（名单源 clean-room，实现票内冻结）；灭一个真实客户端可见分歧（typo 布局名在 BinFlow 静默存成无效配置）；自定义布局名迁移臂二选一归 R-11① | 产品（裁对齐则开实现票） | 同 R-10a；matrix D02 行 3/4 note 联动 |
| R-11 | update-merge 邻臂两件：① 自定义布局名迁移臂 ② PUT=更新超集回撤确认 | ① 未定（随 R-10c 联裁） ② PUT-on-existing=更新 200（BinFlow 自有超集行为） | ① 自定义布局名有效（Artifactory 支持自定义布局，迁来 configJSON 可引用） ② PUT-on-existing=400 create-only（参照更新拼写只有 POST） | ① **名单外一律 400**（BinFlow 无自定义布局能力，白名单放行即造无效配置；「白名单放行+WARN」为备选臂） ② **确认回撤**（ADR-0050 案 A 定谳方向：POST=merge 三列矩阵+PUT=create-only；BinFlow UAT 前夜无外部存量承诺，breaking 窗口现在最便宜；release note breaking changes 首条+无双轨期） | ① 产品 ② 用户（breaking 窗口确认）+ ADR-0050（随 L008-1 入册） | known-divergence `rest/repo-config-update-merge-semantics` BUG；ADR 候选稿 docs/design/repo-update-merge.md |
| R-12 | 经典 security 读族 GET 管理面门（users/groups/permissions 列表与详情） | readonly_admin 可读（CapSecurityRead：admin ∨ readonly_admin） | 参照实例级无 read-only admin（经典读族 admin-only；最接近物=project 域 Viewer） | **维持 CapSecurityRead 超集 → INTENTIONAL**：角色本身系 ADR-0026 决策 1 既有架构位（readonly_admin 管理面含 security:read），终裁时补引 ADR-0026 即转正；参照无此主体，参照存在主体面（admin 200/user 403/匿名 401）零差；收窄 = 翻转既有 readonly_admin 200→403 组合，冲 M9「新增不破坏」基调并伤 console 只读视图 | 产品（补引 ADR-0026 决策 1）+ 用户确认 | known-divergence `rest/security-read-family-readonly-admin-gate` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-13 | 建用户自动入默认组 readers（建模级） | 无此语义（groups:[]） | 建用户自动入组（回显 groups:["readers"]；组权限绑定随实例预置） | **对齐（引入默认组语义）**：参照生态默认权限面是真实迁移依赖（缺位 = 迁移用户静默丢默认权限心智）；实施留建模票（预置组行+建用户入组+回显；组的权限绑定默认零授权，与参照预置绑定的差异随建模票取证定） | 产品（裁对齐则开建模+实现票） | known-divergence `rest/user-create-default-group-readers` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-14 | anonymous 用户行（GET /api/security/users/anonymous，建模级） | 无该行（404） | 有（200；profileUpdatable=false、internalPasswordDisabled=true，字段集同 L007-1 §2 三形态活体取证） | **对齐（只读虚拟行最小面）**：渲染层固定行（不进 DB、不可编辑、不可作为认证身份）；写面（PUT/DELETE 该行）参照行为未取证——实现票内先补取证或按只读拒写登记未取证臂 | 产品（裁对齐则开建模+实现票） | known-divergence `rest/anonymous-user-row` UNKNOWN，LOOP 008 限期（本票呈批） |
| R-15a | 票 E①：`?propertiesXml` 臂双面（matrix D01-R03，P0） | **两面异态（L013 实测）**：api 面 404 + not-implemented 信封（E-26，storage.go 显式分支——冻结件所记 501 已漂移）；**file 面吞参静默回原始文件字节**（200，无 404 臂——未登记的真实 wire 差异） | **两面皆服务（L013 探针 E4/E5 五臂钉死）**：api 面 200 `application/xml` 带 XML 声明（94B）；file 面 200 同体无声明（54B，异头）；无属性双面 404 `No properties could be found.`（file 面为 ISO-8859-1 变体）；JSON 孪生臂 `?properties` 双端 ✅ | **案① 双面对齐实现**（api 面 XML 带声明 + file 面同体无声明 + 双 404 臂）：L013 探针已钉 wire 形态——原案 B 两大支柱（「501 显式」「诚实缺位」）双双失据（as-built 实为 404 且 file 面在静默吞参）；探针即取证，clean-room 障碍消除；实现成本≈序列化分支+file 面一路由分支（见 §2 成本估计） | 产品（裁定+实现票）；呈批随包 | matrix D01-R03 partial（P0/中置信）；v2.1「维持 501」案已被 L013 探针翻案作废——v2.2 重呈（LOOP 014-1） |
| R-15b | 票 E②：`?lastModified` 臂（matrix D01-R04，P0） | 404 + not-implemented 信封（E-26，与 propertiesXml 同分支同测钉死；冻结件所记 501 已漂移） | 200 `{"uri":…,"lastModified":"yyyy-MM-dd'T'HH:mm:ss.SSSZ"}` + `Last-Modified` 头（**子树 max(lastModified)**）；非 local/cached 仓 400（E1+官方 Get Item Last Modified 端点页双证，高置信；头逐字格式未活体钉死） | **案 A 实现**：官方文档端点（真实消费=CI 增量轮询/缓存失效）；既有能力窄投影——`?list&deep` 全树遍历与逐条 lastModified 已 32/32 SAME；400 臂有 `?permissions` 同款已对齐先例；缺位理由（成本）不成立（见 §2 成本估计） | 产品（裁对齐则开实现票）；呈批随包 | matrix D01-R04 partial（P0/中置信）；v2.2 重呈（LOOP 014-1，as-built 注记纠偏入 §3 路径） |
| R-15c | 票 F①：`GET /api/repositories?project=` 容忍语义（matrix D02-R01，P0） | 参数未实现过滤（被忽略，回全列表）〔历史态，已裁〕 | **参照实测=过滤（空集）**：`?project=<未知>` → `[]`（Projects addon 未激活仍按过滤语义回答空集）；空串=无参语义；组合臂随主臂（L013 探针 c0–c4 五臂） | ~~探针先行~~→**已落格案乙（truthful-empty）**：非空 `project` → `[]`，空串维持无参语义；「零代码翻 ✅」假说出局（参照未忽略） | 产品（§4-4 预授权式，已执行） | **已收口**：L013-r15c 实现落地（commits 3453d8bb/9322359c，五臂双端重放全同）——**D02-R01 已翻 ✅**，P0 partial 降至 2 行（余 D01-R03/R04=本表 R-15a/b） |
| R-15d | 票 F②：D02-R01 行收口路径（随 R-15c×探针联动） | —（随裁定分叉）〔历史态，已落格〕 | —（随裁定分叉） | 四象限落格=〔参照过滤/空集 × BinFlow 空集（案乙）〕小实现翻 ✅（推荐格命中，非「PM 反对」格） | 产品（预授权式，已执行） | **已收口**（同 R-15c）；四象限表留档 §2 R-15d 作先例 |

## 2. 逐项详述

### 2.0 既有 9 项复核记录（v2，L008-4a）

- **台账态核对（known-divergence 现值）**：R-1/R-2/R-3 对应条目仍 UNKNOWN（LOOP 002/002/003 限期均过未升级）；R-7 `docker/remote-cache-layout` 仍 BUG（待终裁后拆分转 INTENTIONAL）；R-4/R-5/R-8/R-9 关联 gap 总账候选行不变。逾期事实不改变建议立场，随 v2 一并呈批。
- **D2（L004 分歧号 2，known-divergence `docker/remote-v2-ping-revoked-arm-unreachable`）：仍未裁**——UNKNOWN，authority=pending（token 吊销模型：删行 vs 吊销态可验）。P1~P3 三臂消息已逐字节一致（L004-1 分型落地+差分复验），分歧仅 revoked 第四臂：BinFlow revoke=删行模型下该臂不可达。PM 建议：与 R-1 同属 token 模型面，**随 R-1 终裁联裁**（R-1 建议立场「吊销链不动」即 D2 的 INTENTIONAL 候选形态——删行模型维持，第四臂按模型级 INTENTIONAL 登记）；呈批编排归 conductor。
- **D3（L004 分歧号 3，known-divergence `docker/remote-blob-expired-revalidation`）：已终裁**——conductor LOOP 007 终裁（2026-09-12）**BUG 对齐收**（已验证 blob 豁免检索窗重取；参照语义=manifest 可变恒重验/blob 内容寻址豁免），L008-2 小票执行中。非用户裁定项，本表不收编，仅留状态注记。
- **R-6 取证现状更新**：见 §2 R-6 末「v2 增补」段（L003 三次换链 BLOCKED + TLS 开关三候选）。

### R-1 docker token TTL/形态（C01b）

- **现状对比**：BinFlow `/v2/token` 200 body `{token, access_token, expires_in:2592000, issued_at, scope}`，token 为不透明 hex（M1 token 表 + 吊销链，M2 Q3 暂行 30 天）；Artifactory `{token, expires_in:9000}`，token 为 JWT（前缀 `eyJ2ZXIiOiIy` 即 access token JWT 形态），无 `issued_at` 键。
- **影响面**：客户端兼容低风险——docker CLI 实测登录/拉取均兼容（超集字段不破坏客户端，E5 C01b）；差分 DIVERGENT 的断言驱动 = `expires_in` 值与 `issued_at` 键缺席；TTL 30 天 vs 2.5h 是安全暴露面差异（bearer token 泄露窗口 ×288）与协商频率差异（9000s → daemon 周期性重协商，Artifactory 实况如此）。
- **建议分臂**：TTL 对齐 9000（消灭契约 DIVERGENT 主因，同时安全收紧）；token 内形保留不透明（客户端不解码 token 内容，BinFlow 吊销链 FR-11-AC6 是自有安全增益，无对照面）；`issued_at`/`access_token`/`scope` 超集键保留（无 Artifactory 断言排除它们，CLI 兼容实测）。
- **邻臂联动**：D2（revoked 第四臂）建议随本项联裁，见 §2.0。
- **证据**：E4（L000-docker-remote-evidence.md E1-3）+ E5（L000-docker-remote-diff.md C01b，DIVERGENT/UNKNOWN）；known-divergence `docker/remote-token-response-shape`。

### R-2 remote 面匿名默认姿态（C03）

- **现状对比**：BinFlow remote 仓 `/v2/token` 无凭据请求 → 200 签发匿名 pull scope token（匿名开）；Artifactory 参照实例 → 401 `{"errors":[{"status":401,"message":"Authentication is required"}]}`（匿名关）。
- **定性**：**部署策略默认值差异，非协议语义差异**——Artifactory 支持匿名（全局配置项），本地面 BinFlow 已有定案（v1.3/C6：ping 恒挑战，`anonymous_access` 管匿名 token 是否发 pull scope）；待裁的是 remote 面是否同键管辖 + 默认值。
- **影响面**：匿名开 = remote 仓任何人可借上行带宽代理公网镜像（拉取放大面）；匿名关 = 匿名 pull 体验受损、偏离 Docker Hub/GHCR/Harbor「匿名可 pull」真实世界心智（M2 DE-01 同构论证）。
- **证据**：E4（E1-4）/ E5（C03）；known-divergence `docker/remote-anonymous-token-policy`（LOOP 002 限期）。

### R-3 SSRF 私网上游（allowPrivateUpstream）

- **现状对比**：BinFlow remote 仓上游 URL 指私网默认拒（per-repo `allowPrivateUpstream:true` 显式放行——L000-F/L001-1 对拍拓扑即用此键，键已存在且默认严）；Artifactory 无此闸（私网上游直接放行）。
- **影响面**：安全（SSRF：remote 仓 url 是服务端发起请求的输入，指向内网元数据/存储端点 = 横向移动面）vs 兼容（内网 Artifactory/MinIO 级联是正当场景——已由显式键覆盖，成本=配置一行）。
- **建议**：维持严立场，登记 INTENTIONAL_DIFFERENCE（authority=ADR）；对拍矩阵在 private-upstream case 标注配置前置，不视作 wire 差异。
- **证据**：E5（L000-docker-remote-diff.md 复验拓扑 `allowPrivateUpstream:true` 实证）；known-divergence `docker/ssrf-private-upstream-stricter`（LOOP 003 限期）。

### R-4 HA 链范围

- **现状对比**：BinFlow 单节点（servelock 单实例，ADR-0002/0004；元数据 SQLite/Postgres 双栈，Postgres 路径是 HA 前提）；Artifactory HA = 心跳/主选举/分布式锁（db/locks）/集群拓扑/混合许可降级/任务单节点执行守卫，wire 协议 H1 UNKNOWN（需双节点实验环境）。
- **范围基线**：PRODUCT.md 2026-09-06 终裁翻案已把「HA 本体」放进产品路线（M18+ 候排程）——本项**不是范围翻案票，是排程与最小集边界确认票**。
- **影响面**：架构级——文件存储层单写假设、GC/prune 任务 singleton、token/session 状态；storage-v2 §1 #17（GC cluster singleton 调度）随本项联裁。
- **建议**：HA 本体进目标、**自有形态**（HA 是部署形态非客户端协议，无 wire 对拍义务；管理 REST 面兼容义务已由矩阵覆盖）；最小集=分布式锁/心跳/任务单点守卫；前置=HA-ready 无状态化审查（架构票先行）；排程维持 M18+ 由用户确认。
- **证据**：gap 总账 `ha-cluster`（NEW-BUILD 候选）；storage-v2 §1 #17/§5；feature-catalog n134。

### R-5 GCS/Azure 云 provider

- **现状对比**：BinFlow S3 Backend 已立（含 MPU 新 wire ADR-0039），GCS/Azure 零实现但缝已埋（ADR-0019 Backend 接口 + ADR-0036 保留名 `azure`/`gs` 出现即拒启、文案点名保留位）；Artifactory 模板族含 google-storage-v2、azure-blob-storage{,-v2,-archive}、cluster 变体等 8 模板（binary-provider-chain §3.2，置信中）。
- **影响面**：部署矩阵广度（云原生目标用户的心智覆盖）vs 单二进制 <40MB（PRODUCT 成功标准；双 SDK 依赖链显著增重）vs 维护成本（两套 provider 行为面取证+契约）；S3 兼容层（GCS/Azure 均提供）覆盖大部分实际部署。
- **建议**：不进目标，维持 UNSUPPORTED 留缝；用户强需求信号出现再启（Backend 缝保证引擎零改）。
- **证据**：E1 静态（binary-provider-chain §3.2，jar 模板 + 官方 docs 互证，中置信）；storage-v2 §1 #15/§5。

### R-6 云重定向/预签名（证据依赖——暂不可裁）

- **现状对比**：BinFlow S3 读全代理（无重定向）；Artifactory 二进制 ≥200KB 且云 provider 支持且 `enableSignedUrlRedirect=true` 时下载可 302 重定向至预签名 URL（常量 `cloud.binary.provider.redirect.threshold.in.bytes` 默认 204800）——**URL 有效期与降级直读形态 UNKNOWN**（运行时未取证，参照实例未配云后端）。
- **硬依赖**：取证票 = 参照实例 binarystore.xml 换 `s3-storage-v3` 链指向本机 MinIO，PUT >200KB 对象后 `curl -v` GET 抓 302 Location 形态/签名参数/有效期 + 阈值上下行为（unknown.yaml **U-STG-15**（云链运行时 redirect/预签名，P1）/ **U-STG-09**（预签名 URL 生成规则，P2）、binary-provider-chain §8-4）。**取证前不裁**——盲实现违反 ADR-0001 clean-room 铁律（禁止猜测补齐兼容行为）。
- **建议**（取证后执行）：若取证证实 302/预签名形态，实现面 = S3 Backend presign GET + 200KB 阈值键 + `enableSignedUrlRedirect` 语义对齐；若取证显示形态不可稳定复刻，按 INTENTIONAL 登记（全代理是等价能力）。
- **v2 增补（R-6 取证现状，L003-3/L003-3b-final，E1+E4）**：三次换链尝试均 init 期确定性失败并三度完整恢复（sha256 字节级一致 ×3，conductor 复验）；根因链收敛 = path-style（property 形态无效 → 子元素 `<enablePathStyleAccess>true</enablePathStyleAccess>` 有效，尝试 2/3 过 DNS 证明）→ **TLS 强制**（`<httpsOnly>false</httpsOnly>` 子元素形态**不生效**——仍握手层被断，真开关未定位）。**TLS 开关三候选**（evidence §5，未验证假说按可能性排序）：① JVM 系统属性 `-Dbinary.provider.s3.https.only=false`（BinaryStoreProperties$Key 键表收录 dotted/camel 对键；注入面=JAVA_OPTIONS/setenv）；② `<property name="httpsOnly" value="false"/>` 属性形态（未单独验证）；③ `BinaryStoreConstValues` 层面出厂默认（可能仅系统属性可覆盖）。替代路径（零换链风险）= MinIO 侧配 TLS（自签证书 + artifactory truststore 注入）。静态面已收（E1 字节码）：redirect 默认关（`enableSignedUrlRedirect` 未设 true 即不重定向——字符串常量实证）、`signedUrlExpirySeconds`/`s3SignedUrlExpirySeconds` 键族、阈值常量 204800。**维持 evidence-blocked 不裁**；若三候选配方的新窗口仍失败，备选=按字节码已证静态面实现（默认直读+redirect 显式开启+有效期可配），redirect 运行时面留 UNKNOWN——该备选属产品裁定权，届时单独立票呈批（evidence §6）。
- **证据**：E1 常量+docs（binary-provider-chain §4.4-C2，中置信，有效期 UNKNOWN）；storage-v2 §1 #18；reports/compatibility/L003-s3-chain-evidence.md（§2 三次失败-恢复 / §5 三候选 / §6 回填建议）。

### R-7 C05 remote 缓存树布局

- **现状对比**：BinFlow remote 缓存 = digest 寻址（`<image>/manifests/<hex>` + `<image>/blobs/<hex>`，行带 sha1/sha2）；Artifactory = tag 目录（`library/hello-world/latest/list.manifest.json`）+ `sha256__<digest>` 文件命名 + marker 文件驱动缓存（manifest 下载预写 marker → blob 首取替换）。
- **已裁/已收敛的邻臂**：marker **门控语义**已由 ADR-0047 DigestChainGate 等价实现（docker_refs 账本 + node 短路，L002-1 落地）——本项只剩**树布局形态**；`library/` 归一**行为臂**已随 L001-1 错误形态票收敛（C12 detail 键/静态文案 SAME），docker.io 上游的 library/ 前缀行为待 LOOP 002 契约重放补证（L002-2）。
- **影响面**：客户端协议面**零差异**（缓存树是服务端内部布局，docker/oci 客户端不感知）；唯一可见通道 = Artifactory 兼容 REST 的 storage 浏览面（ListFolder/item 路径形态）——若用户有「用 REST 浏览 remote 缓存树」的使用场景则该面有对拍需求，否则无。
- **建议**：INTENTIONAL 定谳（BinFlow digest 寻址为存储契约——去重/数据完整性语义更优，ADR-0047 拒逐行翻译的立场延伸）；known-divergence `docker/remote-cache-layout` 按「布局=INTENTIONAL（本裁定）/门控=ADR-0047/归一=行为票已收敛」拆分收口。
- **证据**：E5（L000-docker-remote-diff.md C05 DIVERGENT——布局异构，非阻断）+ E4/E1（evidence E2-2/E4-3）；L001-4 Compatibility 拆解留痕。

### R-8 xray-curation REMOVE 候选

- **现状对比**：BinFlow 无 Xray/curation/apptrust 任何面（matrix D14 xray 行 ⛔ 登记口径）；Artifactory curation 策略端点族与 apptrust 属 Xray addon 联动下游能力。
- **范围基线**：PRODUCT.md 2026-09-06 用户终裁「不做 Xray 式漏洞扫描/许可证合规平台（唯一维持排除项）」——**字面只点名 Xray 式扫描**；curation/apptrust 是否归入该排除族是本票要确认的族边界（gap 总账 first_loop 即「开 INTENTIONAL 裁定票」）。
- **影响面**：matrix D14 ⛔ 族行从缺口账移除/维持非目标标注；对应 REST 端点（curation 策略 CRUD 等）按 DE-16 式边界（404 + 域内错误体）处理，不实现。
- **建议**：REMOVE 定谳——curation/apptrust 无 Xray 引擎即无语义（策略执行依赖扫描结果），单独实现是空壳端点；登记 known-divergence INTENTIONAL_DIFFERENCE，authority=PRODUCT 终裁 + 本裁定。
- **证据**：gap 总账 `xray-curation`（REMOVE 候选，待产品终裁）；matrix D14 ⛔ 登记口径。

### R-9 plugins/worker SPI 范围

- **现状对比**：BinFlow 无用户插件/worker 面（matrix D10 ❌1）；扩展等价能力 = webhook outbox/事件总线（M13 FR-114/115 已交付）+ REST API。Artifactory = Groovy 用户插件（execution/steps/webhook 三类型，进程内执行）+ worker TS serverless 扩展（116 java 模块）+ REST plugin/workers 资源。
- **影响面**：迁移故事（重度定制 Artifactory 用户带 Groovy 插件资产迁移）vs 安全（仓库进程内执行用户代码 = 供应链攻击面，安全模型需先行）vs 成本（116 模块 worker 面整域绿地 + Groovy 运行时维护）。
- **建议**：DEPRECATE——不进目标；登记 UNSUPPORTED_FEATURE；等价扩展路径（webhook + REST）已交付；未来出现强迁移需求时评估受限 DSL/插件沙箱另行立项（届时新裁，不预留半成品缝）。
- **证据**：gap 总账 `plugins-worker-spi`（DEPRECATE 候选——安全与维护成本）；feature-catalog n139。

### R-10 仓配置标量非法值三臂（R-10a/R-10b/R-10c）

素材：`docs/prd/ruling-invalid-value-family.md`（L007-3 architect 整理的两案对比+建议立场，本节折入并复核）；证据 E4 活体逐字（reports/compatibility/L007-3-update-merge-evidence.md §6，2026-09-12 :8082）+ E5（L006-a-b-diff.md §1.3）。共同背景：三臂同属仓配置写面（PUT/POST `/api/repositories/{key}`）的输入合法性问题；**参照自身姿态不均**（String→Integer 撞 500、String→Boolean 不可转拒 400、未知布局名拒 400、宽容布尔可落库）——「对齐参照」≠ 统一错误模型，而是复刻其不一致。该面是**管理面**（admin 凭据），真实消费方是 curl/CI 脚本与 terraform provider 类工具；400 vs 500 的差异主要在**重试语义**（规范 HTTP 客户端对 5xx 重试、对 4xx 不重试——RFC 9110 §15.5/15.6，协议语义推理，无 live 工具对拍记录）。

**R-10a mistyped 数字（`maxUniqueSnapshots:"seven"`）**

- **两案对比**：对齐案 = 复刻 500 + Java 转换器逐字报文（差分断言逐字同；但仓配置族获得与全域其余面相反的错误码姿态、500 诱发规范客户端重试确定性错误、逐字文案耦合 JDK `NumberFormatException` 实现细节——脆弱且 clean-room 边缘〔库癖性泄漏〕）；维持案 = 400 + 字段名报文，登记 INTENTIONAL_DIFFERENCE（错误模型一致、可调试性好、无重试误导；差分断言需 normalize 层——tools/difftest 已有 400/500 归一先例可挂）。
- **影响面**：差分矩阵 D02 行 3/4 note；contracts 仓配置契约错误臂冻结。
- **PM 建议立场**：**维持 400，INTENTIONAL**（采纳 architect 建议立场：500 是参照的异常泄漏而非契约设计，两案客户端实际后果等价〔均失败〕，唯重试语义 400 更正确）。

**R-10b 宽容布尔（`blackedOut:"yes"`）**

- **两案对比**：对齐案 = bool 席位接受 commons-lang 真假值词表、静默转换（照抄坏数据的脚本不炸；但静默改写输入〔typo 被吞——数据完整性气味〕、词表逐词维护〔含 'y'/'t' 单字母〕、全域 bool 席位要么跟改要么双姿态）；维持案 = 400，INTENTIONAL（Java 库癖性非有意契约）。
- **影响面**：同 R-10a。
- **PM 建议立场**：**维持 400，INTENTIONAL**（受害脚本形态在 JSON API 消费方中属罕见病；宽容转换的静默性反噬可调试性）。备选折衷臂（仅接受字符串 "true"/"false"——JSON 惯用双拼写，比词表窄比严格宽）：若用户判断「照抄迁移脚本」场景真实存在可选，不预裁。

**R-10c 未知布局名（`repoLayoutRef:"no-such-layout"`）**

- **两案对比**：对齐案 = 按**内置布局名冻结清单**（官方 xsd/文档 26 内置布局名，matrix D02 行 12 已载）做存在性校验，未知名 400 逐字（灭一个真实客户端可见分歧——typo 布局名在 BinFlow 静默存成无效配置、在参照被即刻拦截；成本=维护一份闭集冻结名单〔低频变更〕+ 自定义布局名迁移臂需随裁明确→R-11①）；维持案 = 接受并存读（超集姿态；零实现零维护，但静默无效配置长期在账）。
- **影响面**：实现票一枚（校验+400 逐字+差分臂）；matrix D02 行 3/4；K73 presentation-only 定谳不受影响（本臂只裁**名字存在性**，不实现布局引擎）。
- **PM 建议立场**：**对齐 400（按冻结名单校验）**（采纳 architect 建议立场：名单来源必须是官方 xsd/文档〔clean-room〕，实现票内冻结进规格票；自定义布局名迁移臂二选一归 R-11① 定）。

### R-11 update-merge 邻臂两件

素材：`docs/prd/ruling-invalid-value-family.md` §3（①）+ `docs/design/repo-update-merge.md`（ADR-0050 候选稿）+ `docs/ai-engineering/loop-state.yaml` L008-1（案 A 定谳方向）；证据 E4 活体（reports/compatibility/L007-3-update-merge-evidence.md + L006-a-b-diff.md §1.4，双仓四臂实测）。

**① 自定义布局名迁移臂（随 R-10c 联裁）**

- **两案对比**：**拒案**（名单外一律 400）= 与参照对未知名的拦截一致；BinFlow 无自定义布局能力，放行即造无效配置（错名配置静默存库长期在账）。**放行案**（白名单/登记放行 + WARN）= 迁来 configJSON 引用自定义布局名的仓不拒建（迁移顺滑）；但被放行的名字在 BinFlow 无语义（presentation-only 存读），WARN 易被 CI 脚本忽略——实质是「有条件接受无效配置」。
- **影响面**：R-10c 实现票的校验分支；迁移工具（bf-migrate 类）行为；known-divergence `rest/repo-config-invalid-value-family` 臂三分类。
- **PM 建议立场**：**拒案（名单外一律 400）**——采纳 architect 建议立场；迁移含自定义布局名的仓属显式失败（用户可感知、可改名单内布局名重迁），优于静默无效配置。

**② PUT=更新超集回撤确认（ADR-0050 案 A 呈批件）**

- **现状对比**：BinFlow PUT-on-existing=更新 200（自有超集行为）；参照=400 `error when validating repository name: <key> : Repository key already exists`（errors envelope 逐字，create-only——参照更新拼写只有 POST）。
- **已定谳方向**：LOOP 008 L008-1「update-merge **ADR-0050** 流程（**案 A 定谳**：POST=merge 三列矩阵+PUT=create-only——PUT=更新超集回撤随 ADR）」——ADR-0050 随 L008-1 入 DECISIONS.md（现册顶=ADR-0049）。**本臂待用户确认的仅剩 breaking 窗口可接受性**：案 A 下 BinFlow 自有「PUT 改仓」脚本断（改用 POST 或接受 400）、依赖「全量替换」清配置的脚本须改显式 null；release note breaking changes 首条、无双轨期（参照无 escape hatch，双轨=自造差异）。
- **影响面**：BinFlow 尚在 UAT 前夜、无外部存量承诺（回撤窗口现在最便宜）；console 前端已按 PUT=create/POST=update 分工（零破坏）；已知差分面=matrix D02 行注记复核（L007-3 发现「BinFlow PUT=更新系自有超集行为」）。
- **PM 建议立场**：**确认回撤（案 A）**——超集行为制造永久登记差与双更新入口漂移风险；对齐参照单更新入口语义最简。

### R-12 经典 security 读族 readonly_admin 姿态

素材：known-divergence `rest/security-read-family-readonly-admin-gate`（L006-1 Review B 范围外上报→LOOP 008 立账）；证据 E1（internal/httpapi/router.go 实测：security/permissions GET 族、security/users、security/groups 列表族 routeAuth 挂 CapSecurityRead）+ E1 规格（docs/reverse/rbac-model.md #2 高置信：参照实例级无 read-only admin，经典读族 admin-only）；E5 局限注记：参照无此主体，差分不可达该轴（L006-1 以 admin 凭据执行未触）。

- **两案对比**：**维持案**（CapSecurityRead 超集：admin ∨ readonly_admin 可读）= 角色是 ADR-0026 决策 1 既有架构位（readonly_admin 管理面 = {system:read, security:read, repo:read}）；对参照存在的调用者类（admin 200/普通 user 403/匿名 401）双端一致——分歧仅在 BinFlow 自有 readonly_admin 轴显现，参照存在主体面零差；console 只读视图（审计/运维角色）持续可用。**收窄案**（经典读族 admin-only）= 对齐参照；但翻转既有 readonly_admin 200→403 组合（M9「新增不破坏」基调冲突），影响面=readonly_admin 全部管理面读消费（console 只读视图首当）。
- **同款先例**：与 D2（revoked 第四臂）同为「参照主体缺位」款——参照无此角色/状态，无法差分，只能建模层裁。
- **PM 建议立场**：**维持超集，登记 INTENTIONAL**；终裁时补引 ADR-0026 决策 1 为 authority 即转正（台账 review_gate 已预留此路径）。

### R-13 建用户自动入默认组 readers（建模）

素材：known-divergence `rest/user-create-default-group-readers`；证据 E5（reports/compatibility/L007-residuals-users-diff.md §3-4，2026-09-12 双端活体：参照建用户回显 groups:["readers"]，BinFlow groups:[]）。

- **两案对比**：**对齐案**（引入默认组语义）= 预置 readers 组行 + 建用户自动入组 + 回显对齐；参照生态默认权限面（Artifactory 新用户默认可读的权限心智）对迁移用户/脚本真实存在，缺位 = 迁移后用户静默丢默认权限心智（差分可见 + 行为面损失）。**缺位案**（INTENTIONAL 建模缺位登记）= 零建模零实现；但每次建用户差分恒分歧，且迁移工具（bf-migrate users 阶段）对组面语义不对称。
- **影响面**：建模票（预置组行/建用户事务/回显）+ 权限语义：组的权限绑定建议默认零授权（BinFlow 自有权限模型下 readers 为空权限组——语义无害），与参照实例预置绑定的差异随建模票取证定，不在本票预裁。
- **PM 建议立场**：**对齐（引入默认组语义）**——静默权限心智损失劣于一张小建模票；实施细节归建模票。

### R-14 anonymous 用户行（建模）

素材：known-divergence `rest/anonymous-user-row`；证据 E5（reports/compatibility/L007-residuals-users-diff.md §2/§3-4：参照 GET /api/security/users/anonymous 200——profileUpdatable=false、internalPasswordDisabled=true，字段集同 D04-R02 全集；BinFlow 404）。

- **两案对比**：**对齐案**（只读虚拟行）= 渲染层固定行（不进 DB、不可编辑、不作为可认证身份），GET 命中 `anonymous` 返回取证形态；迁移/盘点类脚本对用户清单的差分即消。**缺位案**（INTENTIONAL 登记）= 维持 404；但「参照有此行」是迁移工具与 UI 对齐的可见面（参照 UI 用户列表含 anonymous）。
- **影响面**：建模票（handler 特判固定行）；**写面未取证**——PUT/DELETE 对该行的参照行为无活体证据（clean-room：不猜），实现票内先补取证或按只读拒写（405/400 形态届时随取证定）并登记未取证臂。
- **PM 建议立场**：**对齐（只读虚拟行最小面）**——最小实现换一个建模级差分清零；写面留取证。

### R-15 票 E+F：storage 读臂两件 + repositories project 参数（L005-3 攻坚排程裁定先行票，四臂）

素材：L005-3「E+F=裁定先行」原案（loop-state L005-3）+ L011-2 派单 + L014-1 重呈裁派单；matrix D01-R03（partial/P0/中置信）/ D01-R04（partial/P0/中置信）/ D02-R01（已翻 ✅）行注记；docs/reverse/rest-api.md §1.1 行 30、§2 行 85、§3 行 117；docs/reverse/api-inventory.yaml 属性/lastModified 行注记；rest-compat-matrix.md §2 行 3/行 4、§3 行 1、带② 2-1/2-2；**L013-4+5 探针**（reports/compatibility/L013-r15-packument-probes.md §1/§2 + 双端 wire `l0134-wire/{a,b}/r15/`）。共同背景：R-15c/d 已落格收口（L013），本节余两臂 R-15a/b 是 **P0 partial 最后两行**（D01-R03/R04）的钥匙。证据现状（v2.2）：R-15a 五臂已活体取证（E4 参照 + E5 双端对拍）——**原「无活体差分记录」前提不再成立**；R-15b 仍依赖 E1 静态规格 + 官方端点页（高置信），`Last-Modified` 头逐字格式未钉（随实现票差分腿取证）。

**R-15a `?propertiesXml` 臂双面（票 E①，D01-R03）——v2.2 三新案重呈（原两案已被 L013 探针翻案）**

- **现状对比（L013 五臂实测，E4 参照 :8082 / E5 BinFlow :8083）**：
  - **参照两面皆服务且形态不同**：api 面 `GET /api/storage/{r}/{p}?propertiesXml` → 200 `application/xml`，`<?xml version='1.0' encoding='UTF-8'?><properties>…`（94B，**带声明**）；file 面 `GET /{r}/{p}?propertiesXml` → 200，`<properties>…`（54B，**同体无声明、异头**）。无属性臂：api 面 404 `"No properties could be found."`（JSON）、file 面 404 同文案（ISO-8859-1 变体）。原「归属面记载分叉」以「**两面都对、形态不同**」定案。
  - **BinFlow 两面异态**：api 面 = **404 + not-implemented 信封**（E-26，`storage.go` 显式分支、`compat_test.go` 断言钉死——**冻结件（rest-compat-matrix §2 行 3 / matrix D01-R03 行文）所记 501 已漂移**）；**file 面 = 吞参静默回原始文件字节**（200，无 404 臂）——与参照（file 面出 XML/404）构成**未登记的真实 wire 差异**。
  - JSON 孪生臂 `?properties` 双端 ✅（p5 对照）；属性写面在 `/api/storage` 面（file 面 PUT 带参=普通重部署吞参，201 零属性——顺带差异项，见影响面末条）。
- **原案作废理由（为什么重呈）**：原案 B「维持 501 + INTENTIONAL」两大支柱均失据——①「501 显式拒绝」与 as-built 不符（实为 404 信封，账实漂移）；②「诚实缺位」不成立（file 面不是拒绝而是**静默吞参回文件字节**——恰是案 B 当初用来反对的「静默错面」形态，且未登记）。维持路线已非零成本：要达标必须先修 file 面。
- **三案对比**：
  - **案① 双面对齐实现**（api 面 XML 带声明 + file 面同体无声明 + 双 404 臂）＝把五臂 wire 形态（已由 L013 钉死并留双端 wire 目录）逐臂落进 BinFlow。成本=序列化分支（复用 properties.go 既有属性读——第二序列化输出）+ file 面下载路径一路由分支（现态全参忽略）+ 双 404 臂文案/charset 变体 + 差分重放（l0134-wire 五臂现成）+ 转义臂补证（探针值 l0134k=l0134v 无 XML 特殊字符——`&<>` 转义形态未取证，实现票首腿补）；约一票两腿（实现+差分），中偏小。受益=D01-R03 整行翻 ✅、无登记尾巴（known-divergence/ADR/契约冻结三件套全免）、file 面吞参差异随路由自然消解。风险=双形态+charset 变体是长期维护面（低频变更）；官方 REST 文档无此端点页 ⇒ 真实消费者=遗留 XML/XSLT 管道（罕见但迁移场景真实存在）。
  - **案② 诚实缺位（原案 B 修正版）**＝file 面从吞参改为**显式姿态**（复用 E-26 not-implemented 信封，零新 wire 形态；具体形态归实现票）+ api 面 404 现态维持，**两面一并登记 INTENTIONAL**（known-divergence 新条目 + ADR 登记 + contracts 404 姿态双面冻结 + 差分 INTENTIONAL normalize 标注）。成本=一微票（file 面一路由分支）+ 登记三件套；JSON 孪生臂继续承载等价能力。风险=XML 面永久缺位（未来出现真实 XML 消费者需重开票——探针证据届时仍有效）；D01-R03 行按「有意不兼容」收口而非翻 ✅。
  - **案③ 双面维持现态登记**＝零代码：api 面 404 信封 + file 面吞参回字节，照实登记。**PM 注**：file 面吞参是**静默错面**（客户端要 XML 得到 200 文件字节——成功假象下的内容错配），不满足 INTENTIONAL 登记的「诚实缺位」标准；若裁此案，该臂只能以真实分歧形态入账（BUG/INTENTIONAL 二选一由终裁定，PM 均不荐）且差分恒分歧。
- **影响面**：D01-R03 行收口路径三岔（§3）；contracts storage 属性契约臂；**as-built 纠偏（无论裁何案）**——matrix D01-R03 capability 行文与 rest-compat-matrix §2 行 3 冻结件所记 501 → 实态 404 E-26 信封（经 compatibility-engineer；冻结件按 D01-R05 先例不回改、留痕即注）；file 面吞参差异立账与否随案分岔；**顺带差异项**（探针 §2 第 3 条）：api 面 PUT 属性回 201 FileInfo vs BinFlow 204——与 matrix D01-R06 ✅（T-493 wire 断言）语境待核（疑非存量 item 臂），转 compatibility-engineer 复核登记，不随本案预裁。
- **PM 建议立场**：**案① 双面对齐实现**——取证已把 clean-room 成本清零、吞参差异逼出隐性成本（修 file 面已不可免），①与②的成本差只剩序列化器本身；换来最后一行 P0 partial 整行翻 ✅ 且零登记尾巴。案③在任何口径下都不成立（静默错面不可登记 INTENTIONAL）。

**R-15b `?lastModified` 臂（票 E②，D01-R04）——v2.2 两案重呈 + 成本估计**

- **现状对比**：BinFlow = **404 + not-implemented 信封**（E-26，与 propertiesXml 同分支、同测钉死——冻结件 rest-compat-matrix §2 行 4 / matrix D01-R04 行文所记 501 已漂移，as-built 纠偏随 §3 路径走）；Artifactory = 目录内最新修改项——200 `{"uri":…, "lastModified":"yyyy-MM-dd'T'HH:mm:ss.SSSZ"}` + `Last-Modified` 响应头；非 local/cached 仓 → 400（rest-api.md §3 行 117，E1 + 官方 Get Item Last Modified 端点页双证，高置信；语义=**子树 max(lastModified)**）。
- **两案对比**：**案 A 实现官方语义** = 子树 max(lastModified) 条目回显 + 头 + 400 臂；BinFlow 已有 `?list&deep` 全树遍历与逐条 lastModified 字段（D01-R05 经 L008→L010 三轮 32/32 归一 SAME）⇒ 本臂是既有能力的窄投影；非 local/cached 400 臂有 `?permissions` 同款已对齐先例（400 文案 `This method can only be invoked on local/cached repositories.` 亦有先例可复用）；消费者=官方文档端点、真实用途（CI 增量轮询目录变化、缓存失效判断）——「客户端真实可用」准绳直接命中。**案 B 维持缺席 + INTENTIONAL** = 零代码；但官方文档端点缺位是真实客户端可见缺口，登记三件套（known-divergence + ADR + contracts 姿态冻结）本身也是成本——而案 A 实现面小（见下），缺位理由不成立。
- **成本估计（据 rest-api.md §3 行 117 规格 × as-built）**：案 A ≈ 一票两腿（实现+差分），小——①聚合：复用 `?list&deep` 遍历取子树 max（或 storage 层一次 walk），无新数据面；②响应形态：两键 JSON（`uri`/`lastModified`，时间戳格式与 FileInfo 族同款 `yyyy-MM-dd'T'HH:mm:ss.SSSZ`——storage.go 既有 formatter）；③`Last-Modified` 头：随差分腿取证逐字格式后落（RFC 1123 vs 自定，未活体钉死——不预猜，ADR-0001）；④400 臂：非 local/cached 仓守卫（?permissions 先例同构）。案 B = 零代码 + 登记三件套 + 差分恒 INTENTIONAL 标注。
- **影响面**：D01-R04 行翻 ✅ 路径；contracts storage 契约新臂；差分报告新臂。**性能注记（实现票口径）**：大目录 O(n) 子树扫描与 `?list&deep` 同阶；元数据索引化（mtime 聚合列）留性能票口径，不在本裁定预裁。
- **取证小项（随实现票差分腿）**：`Last-Modified` 头逐字形态（RFC 1123 vs 参照自定格式）未活体钉死。注意与 L010-3 新待取证项「root lastModified 形态」区分——那是 `?list` 臂根条目字段形态（includeRootPath 角），非本臂。
- **PM 建议立场**：**案 A——实现**（官方端点 + 小语义 + 既有遍历能力复用 + 真实消费场景 + 成本小于登记三件套的长期维护）。

**R-15c `?project=` 参数容忍语义（票 F①，D02-R01）**

- **现状对比**：BinFlow `GET /api/repositories` 对 `?project=` 未实现过滤（参数被忽略，回全列表——rest-compat-matrix §3 行 1 ②「project 参数缺位」；行内① url 前缀已落 2026-09-07、③ type/packageType 过滤已实现，②是本行 partial 的最后一臂）；Artifactory 规格记「过滤」（rest-api.md §2 行 85，高置信），**但参照实例（:8082）对该参数的实际行为未取证**——Projects addon 开关态、过滤/忽略/空集三态未知；同端点家族先例：非法 type/packageType → 空数组（不报错，同规格行）。
- **两案对比**：**案甲 忽略（as-built 维持）** = 零代码；若参照实测也忽略（如 addon 缺位时 filter no-op）→ BinFlow 现行为即 SAME，**D02-R01 零代码翻 ✅**（补契约断言钉死即可）——「可能零代码翻 ✅」假说即此格；风险=若参照过滤而 BinFlow 忽略，带 project 的脚本（遍历/清理类首当）拿到超集列表——静默超集，误操作面。**案乙 空集** = 凡 `?project=<非空 v>` 一律回 `[]`；语义自洽（BinFlow 无 projects 域 ⇒「属于 project v 的仓」恒为空 = truthful answer），与「非法过滤值→空数组」家族姿态一致；小实现（列表过滤分支）；风险=若参照忽略，空集反向制造分歧（无操作变全滤空），且伤「迁移脚本想列全部却误带 project 参数」的宽容性。
- **影响面**：D02-R01 行收口（P0 partial 清尾）；contracts repositories 列表臂断言；console 自有面不受影响（不消费该参数）。
- **取证（需探针，E4 三臂）**：参照 `?project=<known>` / `?project=<unknown>` / `?project=`（空串）+ 与 type 组合一臂，对拍 BinFlow 同命令。
- **PM 建议立场**：**探针先行、对齐参照实测**——不预设甲/乙（ADR-0001：取证前不裁实然行为）；产品原则只排除第三态「明知参照过滤仍维持忽略」（静默超集不可接受）。用户可走 §4-4 预授权式一次裁「对齐参照」。
- **落格结果（L013-4+5，§4-4 预授权执行，2026-09-12）**：探针五臂实测参照=**过滤（空集）**——`?project=<未知>` → `[]`（Projects addon 未激活仍按过滤语义回答），空串=无参，组合臂随主臂；「零代码翻 ✅」假说出局 → **案乙落格（truthful-empty）**：非空 `project` → `[]`、空串维持无参。L013-r15c 实现落地（commits 3453d8bb/9322359c，五臂双端重放全同），**D02-R01 已翻 ✅**。known-project 臂本环境不可测（参照无 /api/projects 端点）——空集语义在无项目域下自洽，不构成阻塞。

**R-15d D02-R01 行收口路径（票 F②，随 R-15c×探针联动）**

- **四象限分叉表**（探针结果 × 裁定案的执行路径）：

| 参照实测 \ BinFlow 姿态 | 忽略（案甲） | 空集（案乙） |
|---|---|---|
| 忽略（filter no-op） | **零代码翻 ✅** + 契约断言钉死（推荐格） | 反向制造分歧——把无操作变全滤空（PM 反对） |
| 过滤/空集 | 静默超集——带 project 脚本拿全列表（PM 反对） | **小实现翻 ✅** + 差分臂（推荐格） |

- **PM 建议立场**：**对齐参照（甲/乙随实测落格）**；探针若出第三态（如 400 / 已知 project 过滤+未知空集的复合形态），按同族「对齐参照」原则逐臂处理并留痕，超出家族形态时回报 conductor 再呈批。
- **落格结果（随 R-15c）**：四象限命中〔参照过滤/空集 × BinFlow 空集（案乙）〕推荐格——小实现翻 ✅（差分臂随票）；非「PM 反对」格，无再呈批项。四象限表留档作后续同族裁定先例。

**R-15 探针清单（E4——执行状态注记 v2.2：①③ 已执行收证，② 随 R-15b 实现票差分腿）**

```bash
# ① propertiesXml（参照两面 + 404 无属性臂；预置属性：PUT …?properties=k=v 先行）【已执行——L013-4+5 §2 五臂（p1–p5），双端 wire 落 l0134-wire/{a,b}/r15/】
curl -su admin:'***' -i ":8082/artifactory/api/storage/<repo>/<file>?propertiesXml"   # api 面
curl -su admin:'***' -i ":8082/artifactory/<repo>/<file>?propertiesXml"               # file 面
curl -su admin:'***' -i ":8082/artifactory/api/storage/<repo>/<file-no-props>?propertiesXml"  # 404 臂
# ① 增补臂（未取证，随 R-15a 案①实现票首腿）：属性值含 XML 特殊字符（& < > " '）的转义形态
# ② lastModified（body + Last-Modified 头 + 非 local 400 臂）【未执行——R-15b 维持 E1+官方页证据；若裁实现，随差分腿】
curl -su admin:'***' -i ":8082/artifactory/api/storage/<repo>/<dir>?lastModified"
curl -su admin:'***' -i ":8082/artifactory/api/storage/<virtual-repo>/<dir>?lastModified"      # 400 臂
# ③ project 参数（三态 + 组合臂；对拍 BinFlow :8083 同命令）【已执行——L013-4+5 §1 五臂（c0–c4），落格案乙并收口】
curl -su admin:'***' ":8082/artifactory/api/repositories?project=<known>"
curl -su admin:'***' ":8082/artifactory/api/repositories?project=<unknown>"
curl -su admin:'***' ":8082/artifactory/api/repositories?project=&type=local"
```

（命令载体归 differential-qa / devops 探针票执行；本清单是裁定素材的取证规格，密码占位不落明文。）

## 3. 裁定后回写路径（约定）

| 裁定项 | 回写目标 |
|---|---|
| R-1/R-2/R-3/R-7 | known-divergence.yaml 对应条目分类/authority + contracts/docker-remote.yaml 条目状态 + 本表状态列 |
| R-4/R-5/R-6 | gap 总账对应域 migration_verdict + storage-v2 §1 行（经 architect）+ 本表 |
| R-8/R-9 | known-divergence 新 INTENTIONAL/UNSUPPORTED 条目 + matrix D14/D10 族标注（经 compatibility-engineer）+ PRODUCT.md 范围演进记录（如需字面扩界）+ 本表 |
| R-10a/R-10b | known-divergence `rest/repo-config-invalid-value-family` 拆臂定 INTENTIONAL（authority=本裁定 + ADR 登记）+ contracts 仓配置契约错误臂冻结 400 + matrix D02 行 3/4 note + 本表 |
| R-10c + R-11① | 同上拆臂 + 布局名实现票（冻结名单 + 400 逐字 + 差分臂；自定义迁移臂按裁定分支）+ 本表 |
| R-11② | ADR-0050 入册（architect/conductor，L008-1 承载）+ update-merge 实现票 PUT 臂 + release note breaking changes 首条 + 本表 |
| R-12 | known-divergence `rest/security-read-family-readonly-admin-gate` → INTENTIONAL（authority 补引 ADR-0026 决策 1）+ 本表 |
| R-13/R-14 | 裁对齐 → 建模+实现票（一票两臂或两票）+ 台账翻 BUG 修复路径；裁缺位 → INTENTIONAL 登记（authority=本裁定）+ 本表 |
| R-15a | **裁案①（双面对齐实现）** → 实现票（api 面 XML 带声明 / file 面同体无声明 / 双 404 臂含 charset 变体；形态按 L013 wire 冻结，转义臂首腿补证）+ 差分重放（l0134-wire 五臂）→ **matrix D01-R03 翻 ✅**（capability 行文同步纠偏，经 compatibility-engineer）+ contracts storage propertiesXml 双面臂 + 本表；file 面吞参差异随路由消解（无需立账）。**裁案②（诚实缺位）** → file 面显式姿态微票（复用 E-26 信封）+ known-divergence 新 INTENTIONAL 条目（propertiesXml 双面缺席）+ ADR 登记 + contracts 404 姿态双面冻结 + matrix D01-R03 按「有意不兼容」收口（经 compatibility-engineer）+ 本表。**裁案③（现态登记）** → known-divergence 立账（file 面吞参=静默错面，分类随终裁）+ 其余同案②路径 + 本表 |
| R-15b | **裁案 A（实现）** → 实现票（子树 max 聚合 + 两键 body + `Last-Modified` 头〔逐字格式随差分腿钉死〕+ 非 local/cached 400 臂〔?permissions 先例〕）→ **matrix D01-R04 翻 ✅**（行文纠偏同 R-15a）+ contracts 新臂 + 本表。**裁案 B（维持缺席）** → known-divergence INTENTIONAL 条目 + ADR + contracts 404 姿态冻结 + matrix 行按「有意不兼容」收口 + 本表 |
| R-15a/b 共同（无论裁何案） | **as-built 漂移纠偏**（经 compatibility-engineer）：matrix D01-R03/R04 capability 行文与 rest-compat-matrix §2 行 3/4 冻结件所记「501 显式拒绝」→ 实态「404 E-26 not-implemented 信封」（源证据 storage.go 分支 + compat_test 断言 + L013 探针 p1）；冻结件按 D01-R05 先例不回改、留痕即注。**顺带差异项**转 compatibility-engineer：api 面 PUT 属性 201 FileInfo vs 204（探针 §2 第 3 条，与 D01-R06 ✅ 断言语境核对后定登记形态） |
| R-15c/R-15d | **已执行收口（L013，§4-4 预授权）**：探针五臂 → 参照=空集过滤 → 案乙小实现落地（commits 3453d8bb/9322359c）→ **matrix D02-R01 已翻 ✅** + contracts repositories 列表臂断言（五臂 wire 现成）+ 本表已收注；无再呈批项 |
| D2（若随 R-1 联裁） | known-divergence `docker/remote-v2-ping-revoked-arm-unreachable` → INTENTIONAL（authority=R-1 终裁 + 模型级登记）或开吊销态可验实现票 + 本表 §2.0 注记更新 |

> 本表不替任何 authority 拍板；每项终裁后由 product-manager 在本表更新状态列并按上表路径派发回写票。

## 4. 批量批复式（v2 新增）

用户可任选其一：

1. **逐项批**：按席位给裁决（例：「R-10a 维持 / R-10c 对齐 / R-11② 确认」；R-1~R-9 同理；备选臂可直接点名，如「R-10b 折衷臂」）。
2. **整包批「按建议执行」**：= 采纳全部 PM 建议立场，展开为——
   - R-1 对齐 expires_in=9000（token 不透明 + 超集键保留）；D2 随联裁按 INTENTIONAL 登记
   - R-2 维持默认开（`anonymous_access` 键管辖 remote 面）
   - R-3 维持严立场 → INTENTIONAL（ADR 登记）
   - R-4 两步走（HA 本体进目标自有形态，M18+ 候排程）
   - R-5 不进目标（UNSUPPORTED 留缝）
   - **R-6 不在整包内**——evidence-blocked 维持不裁直至 MinIO 取证（TLS 三候选配方窗口或 MinIO 侧 TLS 替代路径）；整包批复亦不可代裁（ADR-0001 红线：禁止猜测补齐兼容行为）
   - R-7 INTENTIONAL 定谳（布局拆分收口）
   - R-8 REMOVE / R-9 DEPRECATE
   - R-10a 维持 400 / R-10b 维持 400 / R-10c 对齐 400（冻结名单）
   - R-11① 名单外一律 400（拒自定义名）/ R-11② 确认 PUT 超集回撤（ADR-0050 案 A breaking 窗口背书）
   - R-12 维持 CapSecurityRead 超集 → INTENTIONAL（补引 ADR-0026）
   - R-13 对齐（默认组语义）/ R-14 对齐（只读虚拟行）
   - R-15a 案①双面对齐实现 / R-15b 案 A 实现 / R-15c+d 已落格案乙收口（L013，无需再批）
3. **生效路径**：裁决回执经 conductor 落地——PM 回写本表状态列 + 按 §3 派发回写票；INTENTIONAL 项的 ADR 登记与契约冻结随票；实现/建模类裁定转 tech-lead 拆票。
4. **预授权式（v2.1 新增，限 R-15c/R-15d；v2.2 注：已被 L013 执行完毕留档）**：用户可一次批「对齐参照实测」——探针出数后 PM 按 §2 R-15d 四象限表自动选案执行并回报（免二次呈批）；探针出第三态（400/复合形态）或落「PM 反对」格时中止预授权、回报 conductor 再呈批。**执行留痕：L013-4+5 已按此式落格案乙并收口（D02-R01 翻 ✅）**。R-15a/R-15b 不适用预授权（均可直接裁，无需取证前置——a 的呈批素材已含 L013 探针五臂 wire，残余转义臂细节随实现票；b 的实现对齐已有高置信规格）。
