# 裁决素材 T-631：remote 上游私网限制（allowPrivateUpstream）配置面分歧

- 来源：L042 wave-1 差分报告 F-1 节（reports/compatibility/L042-wave1-docker-remote-replay.md）+ T-631 裁定材料票（conductor 派发）；
  证据=本票活体探针（probes/rest/t631-allow-private-upstream.sh，2026-10-28 对参照 7.161.26 实跑两轮）+ 官方文档核实 + BinFlow 代码/PRD/ADR 溯源。
- 性质：**配置面行为分歧的裁定素材**（非规格冲突）——参照 A（JFrog 7.161.26 Enterprise+ @ 192.168.120.36:32082）remote 配置面
  **无 allowPrivateUpstream 概念**（未知字段静默忽略 + 私网上游默认放行）；BinFlow B **有此字段默认 false**（PRD NFR-S13 明文「BinFlow 扩展」），
  建仓 200 但运行时拒拉 400。本文件只出两案对比素材与建议立场，不构成裁定；既有台账条目 `docker/ssrf-private-upstream-stricter`
  （UNKNOWN，authority pending）为本分歧唯一在账记录，本素材为其定谳输入。
- 席位建议：pending-rulings 席位编号归 PM 登记（INTENTIONAL 裁定需 authority：用户裁决 / ADR / 规格票 三者之一，缺一不可）。

## 1. 分歧面（F-1 全维度）

| 维度 | A 参照（7.161.26 活体） | B BinFlow（uat 实例） |
|---|---|---|
| 配置面回显（GET /api/repositories/<key>） | **无 allowPrivateUpstream 键**（全字段集 117 键亦无任何 private/loopback/internal 概念键） | 回显该字段，默认 `false` |
| 建仓/更新载荷带该字段 | **200 接受但静默丢弃**（创建与 POST 更新两臂均不回显；与假字段对照臂行为一致） | 200 接受并持久化 |
| 私网上游（RFC1918）拉穿 | **默认放行**：建仓即拉穿（schema2 manifest 200） | 默认拒拉：`400 Cannot fetch '<repo>/<path>': upstream target refused — private or suppressed upstream (…)`；置 `true` 后拉穿正常 |
| 错误形态 | 无此臂（恒放行） | RejectionError → 400（fetcher 统一映射，不留 offline 窗口） |

## 2. 参照语义核查（活体探针 A）

探针：`docs/compatibility/probes/rest/t631-allow-private-upstream.sh`（2026-10-28 实跑；凭据 env 注入零落盘；
只写 diff- 前缀测试仓，三仓 P5 清理 200×3、复验零残留）。上游=集群内中继 registry `http://192.168.120.36:32100`（RFC1918 私网，即 F-1 场景上游）。

| 臂 | 命令要点 | 结果（关键输出） |
|---|---|---|
| P0 版本 | GET /artifactory/api/system/version | `7.161.26 / revision 86126900` 200 |
| P1 基线建仓（无字段） | PUT /api/repositories/diff-privup-t631（url=私网上游） | 200 `Successfully created` |
| P1b 全字段集回读 | GET config 全文 grep private/loopback/internal | 仅命中无关键 `cargoInternalIndex`；**零私网概念键**（117 键全集） |
| P2 建仓带 `allowPrivateUpstream:true` | PUT /api/repositories/diff-privup-t631-b | 200 `Successfully created`（**接受**） |
| P2b 回读 | GET config grep private | **零回显 → 字段被静默丢弃** |
| P2c 更新带字段 | POST /api/repositories/diff-privup-t631 `{"allowPrivateUpstream":true}` | 200 `update successfully` |
| P2d 回读 | 同上 | **零回显 → 更新路径同样静默丢弃** |
| P3 假字段对照 | PUT 建仓带 `t631DiffProbeField:true` | 200 且不回显——**参照未知字段策略=静默忽略**，故 allowPrivateUpstream 的沉默与「未知字段被丢弃」一致 |
| P4a v2 ping | GET /v2/（凭据） | 200 |
| P4b 私网上游拉穿 | GET /v2/diff-privup-t631/difftest-docker/smoke/manifests/t1 | **200 schema2 manifest 全文**（默认放行、建仓即拉穿，与 F-1 一致） |

探针方法论注记（round-1 教训）：参照的 `PUT /api/repositories/<key>` 仅建仓——已存在键在**字段校验前**即 400
`Repository key already exists`，字段面必须以独立键名建仓臂 + POST 更新臂探测（已修正入脚本注释）。

## 3. BinFlow 侧语义（代码与文档溯源）

**实现位置**（本票只读核实，未改产品码）：

| 点 | 位置 | 语义 |
|---|---|---|
| 字段定义与默认值 | `internal/repo/config.go:84`（`AllowPrivateUpstream bool`，Go 零值=false）；`internal/repo/config.go:576-577`（更新合并） | 默认 `false`，无任何置 true 的缺省路径 |
| REST 面 | `internal/httpapi/repositories.go:114`（请求 DTO `*bool, omitempty`）、`:350`（GET 回显）；`internal/repo/api.go:1159` | 建仓/改仓载荷可设；配置回显恒含此键 |
| 守卫本体 | `internal/remote/ssrfguard.go`（拒绝类别常量：loopback/RFC1918/ULA/link-local+云 metadata/组播/广播/保留/NAT64/6to4/Teredo…，NFR-S13 第 1-2 点）；`internal/remote/client.go:80-83,164-166`（Guard 按 `AllowPrivateUpstream` 豁免构造） | 请求时全链校验（含重定向逐跳、DNS rebinding pinning） |
| 错误映射 | `internal/remote/fetcher.go:1121`（RejectionError → 400 `upstream target refused — private or suppressed upstream`，**不留 assumed-offline 窗口**——筛查拒绝不静默其它路径）；docker adapter 镜像点 `internal/adapter/docker/remote.go:1679`、`virtual.go:520` | B 侧 400 文案出处 |
| 权限门 | `internal/httpapi/repositories.go:800-804`（建仓/改仓 admin-only 不变量 `administrator privileges required`） | 字段仅 admin 可设（NFR-S13「仅 admin 建仓/改仓可设，写审计日志」） |
| 守卫复用面 | webhook（`internal/webhook/bus.go:174`）、replication（`internal/replication/engine.go:308`，复制目标默认放行=ADR-0025 决策 4 另案） | 同一 Guard 链的其它消费方 |

**文档来源链（防守性设计，非逆向规格）**：

- `docs/prd/milestone-3.md:129`：字段明文标注 **「BinFlow 扩展」`allowPrivateUpstream`（默认 `false`）**——「是否允许上游解析到私网/环回地址（NFR-S13；场景 E 的内网上游放行开关）」。
- `docs/prd/milestone-3.md:289`：「pull-through 语义对齐 Artifactory remote；**SSRF 为 BinFlow 安全底线，NFR-S13**」——PRD 即已声明该面有意不对齐。
- `docs/prd/milestone-3.md:829`（NFR-S13 全文）：校验链七点 + 「除非该仓显式 `allowPrivateUpstream:true`（仅 admin 可设，写审计日志）」+ M42 全变体验收（127/8、10.x、192.168.x、169.254.169.254、::1、0.0.0.0、file://、gopher://）。
- `DECISIONS.md` ADR-0012（remote 代理基线）+ 勘误二（2026-08-19，T-79）：「`allowPrivateUpstream` 豁免语义不变，仅 admin 可设 + 审计」。
- **docs/reverse/ 零记载**：全目录 grep `allowPrivateUpstream` 零命中（仅 helm.md:196 引用 NFR-S13 的 400 拒绝形态作错误面规格）——逆向规格从未把该字段当作参照行为记录，与本票活体结论互证。
- git 溯源：字段与守卫随 ADR-0012/M3 安全底线落地（T-66 错误文案、T-79 勘误），后续 T-597（2098f642）明文「NFR-S13 private-upstream guard retained」——**无任何「对齐参照」意图的实现痕迹**。
- 台账在案：`known-divergence.yaml#docker/ssrf-private-upstream-stricter`（UNKNOWN，pending authority；review_gate LOOP 003 限期升级）；
  首次观察=L000 D19（「DIVERGENT / INTENTIONAL 候选（BinFlow 安全姿态更强）」）。

## 4. 官方语义佐证

- **JFrog 官方文档（docs.jfrog.com「Configuration JSON Files」，`RemoteRepositoryConfiguration+json` 全字段 schema，2026-07-26 更新版）**：
  **无 allowPrivateUpstream 字段**。schema 中最接近的安全类字段为 `allowAnyHostAuth`、`bypassHeadRequests`、`clientTlsCertificate`——
  全 schema 无私网/环回/SSRF 概念字段。活体 117 键全字段集与官方 schema 互证。
- **公网检索**（Bing `"allowPrivateUpstream" jfrog artifactory`，2026-10-28）：**零结果**引用该字段名。
- **反编译源**：`reverse-src/` 不在本机（gitignored 目录未放置）——该字段的 JFrog 侧版本痕迹无法核实，如实标注。但「官方 schema 无 + 活体静默丢弃 + 默认放行」三证已足以下「参照无此概念」的结论（7.161.26 档内）。

## 5. 两案对比（裁定选项）

### 案①：对齐参照——移除守卫或默认放行

- 内容：`allowPrivateUpstream` 语义改为默认放行（字段保留为无害 no-op 或删除），或字段彻底移除；配置回显面同步。
- 依据：参照无此概念、未知字段静默忽略、私网上游恒放行——行为对齐主义。
- **SSRF 影响评估（必须包含）**：
  - 放开的是 NFR-S13 全链：回环/RFC1918/ULA/链路本地（含云 metadata 169.254.169.254）/NAT64/6to4 等全部筛查 + 重定向逐跳重检 + dialer DNS-rebinding pinning 只保留 scheme/重定向上限/超时/体量上限。
  - 攻击面：建仓/改仓 admin-only（repositories.go:800-804）→ 直接触发面限于 admin；但 admin 凭据失陷时失去纵深防线（内网横向、云 metadata 凭据窃取均经 remote 上游 outbound 通道）；webhook 共链面（AllowPrivateTarget 另有开关）不受本字段影响。
  - 参照自身无此防线 = 对齐后 BinFlow 安全立场弱于参照活体（与 L000 D19 记录的「姿态更强」翻转）。
  - 附带工程成本：PRD NFR-S13/M42 验收面、ADR-0012、console-ux「已放行私网上游」黄标（console-ux.md:213/426）、仓库级豁免旋钮（matrix D12-R18 L029 注记的 NFR-S13 一等旋钮）全线返工；ssrfguard 测试资产大改。
- 收益：私网上游场景开箱即用（免 admin 置 true 一步）；差分 naive 重放不再撞 F-1；配置回显面逐键对齐参照（少一个参照没有的键）。

### 案②：登记 INTENTIONAL——保留守卫，台账立定

- 内容：维持字段默认 false + admin 豁免 + 审计；在 known-divergence.yaml 将既有 `docker/ssrf-private-upstream-stricter` 条目由 UNKNOWN 翻
  INTENTIONAL（需 authority 落锚）；差分夹具/金样侧沿用既有实践（fixtures 对 B 侧统一带 `allowPrivateUpstream:true`，金样 pull-sequence/request.yaml:9 已在案）。
- 依据：字段为 PRD 明文「BinFlow 扩展」（milestone-3.md:129）+ ADR-0012/勘误二 authority 链前身；PRD 即声明「SSRF 为 BinFlow 安全底线」有意不对齐；
  拉穿正路径（公网上游）本就同构，分歧面收窄为「私网上游豁免姿态」一维。
- 成本：私网上游部署需 admin 一步显式放行（错误文案已指因、console 有黄标、文档有开关——运维路径完备）；差分 naive 重放需夹具aware（一个 fixture 字段）。
- 收益：SSRF 防线完整保留；安全审计面（WARN 结构化日志）保留；行为分歧被显式登记、可追溯、不悬空。

### 对比表

| | 案① 对齐 | 案② INTENTIONAL |
|---|---|---|
| 兼容性 | +1 面（私网上游开箱拉穿 + 回显少一键） | 维持 1 分歧（显式登记） |
| 安全 | 撤 NFR-S13 全链防线（admin 失陷纵深、云 metadata、DNS rebinding pinning 的筛查面） | 防线保留，豁免显式 + 审计 |
| 工程成本 | 高（PRD/ADR/console/测试资产全线返工） | 低（台账翻态 + 差分夹具纪律，均已就绪） |
| authority 需求 | 需新 ADR 撤销 NFR-S13/ADR-0012 相关决策（效力序上 ADR 撤 ADR） | 需 authority 确认既有 PRD 立场为最终裁定（PRD/ADR 前身在案，举证轻） |
| 先例一致性 | 与 ADR-0025（复制目标默认放行）趋同，但复制目标「内网是常态」与上游「公网是常态」场景不对称 | 与复制面区分对待（见下） |

**先例不对称说明**：ADR-0025 决策 4 对**复制目标**选了默认放行——因为复制目标几乎必然是内网 BinFlow 实例（功能刚需）。
remote **上游**场景相反：常态是公网制品源（docker.io/npm/pypi…），私网上游是例外且有显式开关。两案在「常态 vs 例外」上不对称，
案②（上游默认拒）与案①-复制先例（目标默认放）并不矛盾，反构成同一原则的两次应用。

## 6. 建议立场（compatibility-engineer，供 authority 裁）

**推荐案②（INTENTIONAL）**，理由：

1. **出身即防守性设计**：字段非逆向规格产物（docs/reverse/ 零记载）而是 PRD NFR-S13 明文「BinFlow 扩展」+ ADR-0012 安全底线——
   不存在「对齐意图被实现偏差破坏」的 BUG 叙事；INTENTIONAL 是对既有设计意图的追认而非翻案。
2. **参照权威面亦无此概念**：活体 117 键 + 官方 schema + 公网检索三证一致；连「参照侧语义」都不存在，对齐无锚点物。
3. **兼容代价已最小化**：拉穿正路径（公网上游）本就逐字节同构（L042 wave-1 十项 PASS 可证）；分歧收窄到私网豁免一维，
   且豁免开关、错误指因文案、console 黄标、差分夹具纪律四件运维件全部在案——差距是一步 admin 配置，不是能力缺口。
4. **案① 安全成本不对称于兼容收益**：撤防线换来的只是「省一个字段」，且需 ADR 撤 ADR 的 authority 链工程；
   admin-only 前置虽缩小触发面，但纵深防线（凭据失陷、DNS rebinding、云 metadata）在制品仓库这一天然 outbound 高权面上有真实价值。
5. **台账卫生**：UNKNOWN 条目（LOOP 003 限期升级）悬置至今，本案以三证素材供裁，无论两案均应在本程落定，不再延期。

若 authority 选案①，前置条件：新 ADR 明文撤销 NFR-S13 筛查链（保留 scheme/重定向/超时/体量项）+ SSRF 影响评估签字 + 差分回归批。

## 7. 候选条目文案（案② 时 known-divergence.yaml 落账形态）

对既有条目 `docker/ssrf-private-upstream-stricter` **就地刷新**（id 不变保持引用稳定；不新增条目——
L042 报告「Z 口径更新建议 2」提案的 `remote/allow-private-upstream-default` 与本条目为同一分歧，避免双账，归 conductor 采认时合并）：

```yaml
- id: docker/ssrf-private-upstream-stricter
  surface: 上游私网限制（remote 仓 allowPrivateUpstream——BinFlow 扩展字段，全包型 remote 上游共面；docker remote 活体发现面）
  classification: INTENTIONAL        # ← 由 UNKNOWN 翻 INTENTIONAL（需 authority 落锚后生效）
  rationale: >-
    BinFlow SSRF 防护严于参照且为有意差异：参照 7.161.26 remote 配置面无 allowPrivateUpstream 概念（T-631 活体三仓三臂探针——
    全字段集 117 键无私网概念键；带字段 PUT 建仓/POST 更新均 200 静默丢弃，与假字段对照臂行为一致=未知字段静默忽略策略；
    私网 RFC1918 上游默认放行、建仓即拉穿 200）+ 官方 Configuration JSON schema 无此字段 + 公网检索零命中；
    BinFlow 侧为 PRD NFR-S13 明文「BinFlow 扩展」默认 false + admin 显式豁免 + WARN 审计（ADR-0012/勘误二）。
    拉穿正路径（公网上游）本就同构，分歧收窄为私网上游豁免姿态一维；守卫保留理由=admin 失陷纵深/云 metadata/DNS rebinding
    筛查面在 outbound 高权面有真实价值，豁免运维件（开关/错误指因/console 黄标/夹具纪律）四件齐备。
  authority: { type: <用户裁决|ADR|规格票>, ref: "<定谳后回填：裁决记录编号/ADR 编号/规格票号>" }
  review_gate: T-631 裁定票（LOOP 003 限期升级项经 L042 wave-1 F-1 活体发现升级触发，本案两证素材已齐）
  evidence: >-
    reports/compatibility/L042-wave1-docker-remote-replay.md F-1（7.161.26 基准活体首发现）+
    reports/compatibility/L000-docker-remote-diff.md D19（首次观察）+
    docs/compatibility/probes/rest/t631-allow-private-upstream.sh（2026-10-28 三仓三臂复跑：字段建仓/更新双路径静默丢弃 +
    默认放行拉穿 200 + 三仓清理零残留）+ docs/compatibility/rulings/T-631-allow-private-upstream.md（裁定素材）
```

classification 行仅在 authority 落锚后由本票后续（或 conductor 指令）翻 INTENTIONAL；本程不改 classification（守 UNKNOWN 纪律：
无 authority 不得裁定）。

## 8. 关联

- 契约面：contracts/ 现有 39+ 契约**零** `allowPrivateUpstream` 断言（grep 零命中）——裁定前不新增契约条目；裁定后案②路径可将
  「remote 配置回显含扩展键 + 私网默认拒 400 文案」契约化（confidence high，活体+代码双证）。
- 金样：`golden/docker-remote/pull-sequence/request.yaml:9` 已记载对拍腿 `allowPrivateUpstream=true`（BinFlow 侧）——金样无需变更。
- 差分：differential-qa 夹具纪律——B 侧 remote 仓指向私网测试上游时**必须**带 `allowPrivateUpstream:true`（wave-1 F-1 即 naive 载荷所撞）；
  建议写入 difftest playbook（归 differential-qa 域）。
- 相邻在账：`docker/remote-token-response-shape`、`docker/remote-anonymous-token-policy`（同报告 ②③ 复现项，非本票范围）；
  T-632（A 侧 recurrent-failure 熔断，另一安全面分歧，方向与本案相反——A 更严，同样待裁）。
