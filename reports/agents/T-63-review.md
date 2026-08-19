# 评审报告 T-63（视角: 架构一致性 consistency）

结论: **APPROVE**
评审人: code-reviewer（单 reviewer，契约面从严）
日期: 2026-08-19
输入: reports/agents/T-63.md、commit 5b79a52、architecture.md §5.1/§5.4、PRD milestone-3.md、BOARD.md（只读）

## 评审范围与方法

- 通读 internal/adapter/metadata.go（新）、registry.go、internal/httpapi/router.go、server.go、internal/repo/api.go 全部改动及上下游调用点（splitFirstSegment/authorize/withStrippedPrefix/metadata.RepoStore）。
- git diff 逐行核对 commit 5b79a52（10 文件，+753/-50）。
- 实跑取证：`go test -race -count=1 ./internal/adapter/`（ok 1.772s）、`go test -race -count=1 ./internal/httpapi/ ./internal/repo/`（ok 63.7s / 32.7s）、`go vet` 三包零告警、`go test -race -run 'TestT63|TestE26' ./internal/httpapi/`（ok）。
- **独立探针**（reviewer 自建 6 行矩阵打真栈后已删除，工作树还原核实）：挂载前缀内转义（`npm%2F`）不误挂维持 404；repo 键带转义（`npm%2Dlocal`）splitFirstSegment 解码寻址 200 且 escaped 拼写逐字到 handler；query string 过重写保留；裸 `/binflow/api/npm/` 与双斜杠 `npm//` 均 404 不 panic、与裸内容面同答；dot segment 逐字穿透到 adapter（无 cleanPath 重定向）。6/6 PASS。
- clean-room 抽查：metadata.go 仅 import fmt/sort，接口形态为 Go 原生设计；规格来源 oss-structure.md §4（行为/结构描述）与 §6 启示 1/2，无逐行对应嫌疑。reverse-src 未触碰。

## 逐项结论

### 1. MetadataProvider 接口形态 — 通过

- 四方法全部是 repo 相对路径纯函数，无请求上下文无存储访问，无过早设计：T-66 只消费 `Classify`（ForProtocol miss → 默认 KindContent 已在 ForProtocol godoc 写死）；T-69/70 是注册者；T-72 消费 `PackageName+Versions`；T-67 maven 经 ClassReader 取 class（不经此注册表）。未发现缺方法。
- `VersionComparator` 确认「留缝不实现」：全仓无任何实现（仅测试 stub 返 nil），§11.15 M4 缝维持。
- panic 规则与 Register 同构（nil/空 Protocol/重复 protocol），锁同一把 reg.mu（race 绿）。

### 2. 分发缝正确性 — 通过

- escaped 拼写逐字保留：实现者测试（矩阵 10 行 + %2f/%2F 等价 + 两入口等价）+ 本评审 6 项独立探针双重确认；RawPath 承载原始字节，EscapedPath 不折叠不重编码。
- 前缀匹配在 escaped 拼写上做（`npm%2F` 不匹配 `npm/`）——与内容面 raw-spelling 分发纪律一致，正确。
- 未注册 404：`dispatchAPIProtocolMount` 返 false 落 E-26 信封；`TestE26FullMatrix` 的 `/binflow/api/npm/xx`、`/binflow/api/pypi/simple` 两行原断言通过（默认 harness），R5 未提前反转，`TestT63UnmountedProtocolStaysE01` 钉死 OFF 态永久契约。
- maven/pypi-ui 不进闭合清单核对无误：PRD M3 §2（npm/PyPI 挂 `/binflow/api/npm|pypi/**`，Maven 内容直走内容路径）与端点矩阵（`/binflow/api/pypi-ui/**` 不排期、维持 404 + E-01）。
- ACL 取键核实：authorize → splitFirstSegment 作用于**重写后** EscapedPath（/binflow/<repo>/...），与路由 repo 行解析同一字符串，两门同名。

### 3. class 键清理 — 通过

- registry.go `Register`：删 byType class 写入与重复 class panic；保留 nil/空 Protocol/空 RepoTypes/重复 package type panic——与 §5.1 勘误文本逐条对齐（「byType 键仅 package type、空 RepoTypes panic 保留」）。
- `TestRegisterSharedRepoClassIsLegal` 钉死五协议全 class=local 合法注册（generic/docker/maven/npm/pypi），`ForRepoType("local")` miss 断言在 TestRegisterAndAll 补齐。
- server.go `New()` adapters map class 死键写入删除，注释含「不得 reintroduce」防线。勘误落地完整。

### 4. repo/api.go 两段拆分 — 通过

- git diff 核实：仅两段 banner 注释 + ClassReader 声明 + `var _ ClassReader = metadata.RepoStore(nil)` 编译期断言，**方法零增删改**。
- `RepoStore.Get(ctx, repoKey) (*Repo, error)` 签名与 ClassReader 吻合（编译断言成立）；缝声明在 repo 使 maven adapter 依赖 repo 而非 metadata——比 docker 的 metadata 例外更干净，依赖方向正确。

### 5. 契约三决定 — 逐条意见

| 决定 | 意见 |
|---|---|
| 1. 分发缝=路径重写（跨协议误挂跟 repo 行拒绝留给协议票） | **确认**。单套分发语义、两入口等价有测试钉死；误挂（pypi 拼写→npm 仓）无权限面差异（ACL 同键），且本评审探针证实边界行为（双斜杠/dot segment）与裸内容面一致。注意项见 N4：严格拒绝（Artifactory 式 repo 非 npm → 404）须在 T-69/T-70 票内显式决策，不得默认遗忘。 |
| 2. ClassReader 放行整行但契约只读 Type | **确认**。与 httpapi RepoLookup（server.go:19-30）同款纪律同款形态（`*metadata.Repo`，契约「only PackageType is read」），先例一致；窄化需包装类型，超出本票 area 限制合理。见 N3。 |
| 3. Versions() nil = generic 无序、消费方回落首命中（ADR-0013） | **确认**。比「无比较器不许注册」宽容且安全方向正确（无序回落不会选错内容，只是不择优）；接口与 ForProtocol 两处 godoc 均写明。 |

### 6. 自捉 bug 修复覆盖 — 充分

prefix+TrimPrefix 组合（escaped 首拼 + decoded 次拼）经真栈端到端覆盖：实现者矩阵的 EscapedPath#RawPath 逐字断言 + 本评审补的前缀内转义不匹配、repo 键转义、query 保留、空 tail/双斜杠边界。修后写法的正确性论证（前缀字面量无可转义字节故 escaped/decoded 同减同余）已入代码注释。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

- **N1**（registry.go:14-26）byProto 与 byType 现为内容完全相同的两张 map（均以 proto 为键），byType 是纯冗余重量。可在后续清理票合并为一张（ForRepoType/Protocols 的语义区分留在文档）。本票不动是对的——两 API 名各表其义有契约价值。
- **N2**（metadata.go:52-56）`Protocol() ≡ Handler.Protocol()` 仅文档约定、无交叉校验：错拼注册会静默降级为 content 姿态（安全侧但无告警）。建议 T-67/69/70 各协议包导出**单一** Register 函数同时注册 handler+provider（同一字面量），或在 cmd 装配处加 debug 断言。
- **N3**（repo/api.go:333-351）ClassReader 只读 Type 是纪律非强制；若 M3 adapter 开始读 Config 等字段，届时以包装类型窄化（docker RepoRow 先例）。
- **N4**（下游跟踪）跨协议误挂的严格拒绝决策 + E-26 矩阵两行翻转（R5 预注）须落在 T-69/T-70；npm login（`PUT /-user/org.couchdb.user:<name>`）先过写 gate 的影响已在遗留注明，T-69 验证。
- **N5**（router.go:358-366）apiProtocolMounts 扩清单时的隐陷阱：REST 路由前缀（storage/、repositories/、security/、v1/、system/）在 switch 中先于 seam 命中，与未来协议名同名会静默路由到 REST 面。建议届时补一行注释声明保留前缀。

## 范围外发现

无。

## 验证命令存档

- `go test -race -count=1 ./internal/adapter/` → ok 1.772s
- `go test -race -count=1 ./internal/httpapi/ ./internal/repo/` → ok 63.691s / 32.729s
- `go test -race -count=1 -run 'TestT63|TestE26' ./internal/httpapi/` → ok
- `go vet ./internal/adapter/ ./internal/httpapi/ ./internal/repo/` → 零输出
- reviewer 探针（6 变体，运行后删除）：6/6 PASS；`git status internal/` 干净
