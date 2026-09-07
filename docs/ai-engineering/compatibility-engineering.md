# BinFlow 兼容工程体系设计（Compatibility Engineering）

> 重组总令 §三/§五/§七/§十五/§十六/§十七 落地设计。现状基线见 current-state.md §11/§14。
> 核心命题：把「兼容」从**书面三源对账 + 单票内散断言**升级为**可执行契约 + 差分执行 + 金样回归 + 评分驱动**。

## 0. 职责分工（总令 §四）

```
reverse-src/（只读） → reverse-engineer → docs/reverse/ 行为规格（保持既有 clean-room 铁律）
                                              ↓（只输出 Behavior Specification，禁止类名/私有结构）
                              compatibility-engineer → docs/compatibility/contracts/ 可执行契约
                                              ↓
                              differential-qa-engineer → 双实例差分执行 → reports/compatibility/
```
- reverse-engineer 输出示例形：「GET /xxx 带 Range 头 → 206 + Content-Range: bytes 0-99/256 …」
- 禁止：反编译码直转 BinFlow 码、copy/paste、逐行翻译、以私有实现结构作内部架构依据（ADR-0001 延续）

## 1. 目录体系（docs/compatibility/）

```
docs/compatibility/
├── README.md                 # 体系入口与状态机说明
├── matrix.yaml               # 兼容矩阵主账（自 rest-compat-matrix.md 迁移并机读化）
├── contracts/                # 可执行契约（每能力一个 YAML）
│   └── <domain>/<feature>.yaml
├── probes/                   # 可复跑实验（每契约配套，curl/脚本形态，登记于契约 evidence）
├── fixtures/                 # 差分夹具（请求体/制品样例/期望规范化样本）
├── diffs/                    # 差分运行产物登记（引用 reports/compatibility/ 全文）
├── known-divergence.yaml     # 已知差异台账（四分类裁定）
├── golden/                   # Golden Behavior Dataset（行为标准）
│   └── <domain>/<feature>/{request.http,response.{headers,body},artifact,metadata}.yaml
└── protocol/                 # 每协议的兼容细则（从 docs/reverse/<proto>.md 提炼的可执行面）
```

## 2. Compatibility Contract 契约模式（§三）

```yaml
# contracts/<domain>/<feature>.yaml
id: storage/artifact-range-get
feature: Range 下载
surface: GET /binflow/<repo>/<path>          # 或 REST/协议端点
request:
  method: GET
  headers: { Range: "bytes=0-99" }
  query: {}
authentication: { type: basic, role: read }   # anonymous/user/admin + 权限位
expect:
  status: 206
  headers: { Content-Range: "bytes 0-99/<total>", Accept-Ranges: bytes }
  body: { encoding: binary, sha256: "<fixed-fixture>" }
artifact: { bytes: "fixtures/range-100.bin", mode: exact }
metadata: { node.updated: unchanged }          # 副作用面
side_effects: [download_count+1]               # 显式列举；K69 单源口径
timing: { p95_lt_ms: 150 }                     # 仅性能敏感面
error_behavior:
  - { when: "Range 越界", status: 416, body_literal: "..." }
version: { artifactory_ref: 7.161.20, binflow_since: uat.<sha> }
evidence:
  - { kind: probe, path: probes/storage/range-get.sh, date: 2026-09-08 }
  - { kind: difftest, report: reports/compatibility/2026-09-08-storage.yaml }
confidence: high | medium | low
status: DISCOVERED | SPECIFIED | IMPLEMENTED | VERIFIED | DIVERGENT | BLOCKED | INTENTIONAL
divergence_ref: known-divergence.yaml#<id>     # status=DIVERGENT/INTENTIONAL 时必填
```

**状态机**：DISCOVERED（逆向规格在案）→ SPECIFIED（契约成文）→ IMPLEMENTED（BinFlow 有面）→ VERIFIED（探针+差分双绿）；异常态 DIVERGENT（差分不一致待裁）/ BLOCKED（参照断供等）/ INTENTIONAL（裁定为有意差异）。**无 Differential Test 的协议兼容票不得 DONE**（§八）。

## 3. matrix.yaml 主账（自 rest-compat-matrix 迁移）

- 行集与四态（✅/◐/❌/⛔ + 超集）**原样迁移**（v1 冻结行集 195 行为初始基线，保留置信度标注）
- 每行新增：`contract_ref`（指向 contracts/）、`golden_ref`、`last_difftest`、`priority_class`（P0/P1/P2）
- 迁移后 rest-compat-matrix.md 降级为只读快照（加头注指向 matrix.yaml），console-parity 册保留 FE 专账但行级互链

## 4. known-divergence.yaml（§三-8 四分类）

```yaml
- id: rest/build-batch-delete-date-range
  surface: POST /api/build/delete
  classification: UNSUPPORTED_FEATURE   # BUG | INTENTIONAL_DIFFERENCE | UNSUPPORTED_FEATURE | UNKNOWN
  rationale: ADR-0045 Errata 定案：批删无 dateRange（有意差异）
  authority: { type: adr, ref: DECISIONS.md#adr-0045-e3 }
  review_gate: M18+                      # 复审窗口
```
分类权限：BUG/UNSUPPORTED 由差分证据+contract 直接判；INTENTIONAL 必须 authority（用户裁决/ADR/规格票）；UNKNOWN 限期（默认两程）内升级。

## 5. Differential QA（§五 + §十五）

**层级**：L0 HTTP primitive → L1 REST → L2 Repository → L3 Artifact → L4 Auth → L5 Package protocol → L6 Remote → L7 Virtual → L8 Storage 语义 → L9 Cache 语义 → L10 Operational → L11 UI → L12 Upgrade/migration。

**执行形态**（`tools/difftest/`，differential-qa-engineer owns）：
```
fixtures → same request ─→ Artifactory ref (https://ref-uat.:8082 或恢复的 7.161 容器)
                      └──→ Binflow UAT (https://uat.binflow.org)
normalize（域无关归一：时间戳/ETag/随机 ID/绝对 URL/顺序无关数组排序，规则登记于 fixtures/normalize.yaml）
→ diff（status/headers 白名单集/body 结构或字面/artifact sha256/metadata）
→ reports/compatibility/<date>-<domain>.yaml（逐 case：一致/差异+分类建议）
```
**真实客户端优先**（curl/docker/podman/mvn/npm/pip/twine/go/helm/oras/crane/skopeo/nuget/cargo/conan——protocol-matrix 既有十腿即为 L5 差分的执行底盘，升级为双发形态）。自研 HTTP client 仅补 L0/L1 密集断言。

**参照实例修复**为体系前置（current-state §12-10）：双容器损坏必须先修（Q10 转正为体系票），否则差分退化为「BinFlow vs 金样」单边模式（金样自三书面源+一次性人工活体采集，confidence 上限 medium）。

## 6. Golden Behavior Dataset（§十六）

- `golden/` 每条 = 冻结的 request/response(headers,body)/artifact bytes/metadata/errors 全集
- 采集：差分运行中 Artifactory 侧通过的 case 自动提金（`evidence.kind=golden-capture`）；或活体手工采集（须标注实例版本）
- 地位：**项目行为标准**。重构/协议改动跑金样回归（`tools/difftest --golden`）；金样更新须走 compatibility-engineer 评审（禁止「跟实现走」的静默改金）

## 7. Compatibility Score（§十七）

```
Overall = Σ(域分 × 域权重)     域权重：P0 域 ×4 / P1 域 ×2 / P2 域 ×1（禁止简单平均）
域分   = (✅ + 0.5×◐ + 0.5×超集) / (✅ + ◐ + ❌ + 超集)      ⛔ 不计入分母
每轮输出：Overall / P0 / P1 / P2 + new regressions / fixed divergences / new divergences
```
计量由 conductor 每轮自 matrix.yaml 机读计算（脚本 tools/difftest/score.sh，performance-engineer 协同）。

## 8. Gap-Driven Planning 接口（§六/§七）

每轮 conductor 必答四问：`surface=X / matched=Y / known-divergence=Z / unknown=N` + Coverage 与 P0/P1/P2 Gap 清单。优先序：P0 兼容缺口 + P0 安全 + P0 数据完整性 → P1 兼容 + P1 客户端失败 + P1 存储正确性 → P2 增强。Priority = 业务影响 + 兼容影响 + 客户端影响 + 回归风险 + 架构依赖（tech-lead 拆票时按此打分，登记于 SPLIT）。

## 9. 与既有资产的衔接（迁移而非推翻）

| 既有 | 去向 |
|---|---|
| docs/reverse/rest-compat-matrix.md（195 冻结行） | 行集迁 matrix.yaml，原文件降只读快照 |
| docs/design/console-artifactory-parity.md | 保留为 FE 专账（L11 层），行级互链 matrix.yaml |
| docs/reverse/gap-endpoints.md + full-feature-matrix.md | 并入 matrix.yaml 后归档（头注指向） |
| ci/protocol-matrix.sh 十腿 | L5 差分执行底盘（T-479 remote/virtual 段已是拉穿对照雏形） |
| 各票「契约漂移登记」 | 收敛进 known-divergence.yaml（compatibility-engineer 主持首批收编） |
| web/e2e t422 BASE2 双实例腿 | 升格为 tools/difftest 的 e2e 级先例 |
| 本地 :8082 参照文化（t*-probe/ 取证） | 制度化为 probes/ 资产 |
