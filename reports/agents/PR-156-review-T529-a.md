# 评审报告 T-529（形态: reviewer-a / correctness）

## 15 字段模板

Ticket:        T-529 [P0] `<K>-cache` 投影派生注册表（virtual 四桶解析 remote 侧底座）
Role:          code-reviewer (reviewer-a)
Area:          internal/remote（storage 域关键面；改动=新增 projection.go / projection_test.go）
Input:         conductor 派发（diff 文件清单 + 设计权威 virtual-four-bucket.md §4/§2.1）；通读 docs/reverse/remote-cache-projection.md §1 全表、internal/repo/config.go（remoteConfig canonical 拼写）、internal/metadata/substores.go（repoStore.Get/Update/Delete）、internal/repo/virtual.go:147-161（memberPriorityResolution 先例）、internal/remote/fetcher.go（Engine/NewEngine 定义）
Changes:       projection.go 175 行逐行 + projection_test.go 283 行逐行；上下游追读：metadata 读取面（Get 原子性/错误包装）、repo 写面（Type 常量、canonical 字段拼写、defaultRepoLayoutRef）、httpapi render 默认值（经 T-529.md ⑥ 引证，未逐行走读 render 文件）
Files:         internal/remote/projection.go — PASS（正确性无缺陷，见结论区）；internal/remote/projection_test.go — PASS（覆盖充分，2 条 non-blocking 增强建议）；internal/remote/fetcher.go — 零改动证实；reports/agents/T-529.md — 声称的命令全部复跑复现，无虚假证据
Tests:         `go test ./internal/remote/ -run TestCacheProjection -v` = 6/6 函数 PASS（15 个叶子用例：Derivation 5 + KeySuffix 1 + Gate 3 + Unknown/NonRemote 4 + Reload 1 + Concurrent 1）；`go test -race -run TestCacheProjection ./internal/remote/` = ok 51.485s；`go vet ./internal/remote/...` = 干净；`golangci-lint run ./internal/remote/...` = `0 issues.`；`gofmt -l internal/remote` = 空
Commands:      上述五条原文；`git diff --stat internal/remote/fetcher.go`（空输出，exit 0）；`git status --porcelain internal/remote/`（仅 ?? projection.go / ?? projection_test.go）；`grep -rn '"-cache"' --include='*.go'`（全仓字面量清点）；`git log --oneline -1 -- internal/replication/probe.go`（aae43683，T-422 遗留）
Outputs:       reports/agents/PR-156-review-T529-a.md（本文件）
Compatibility: 本视角核对了 remote-cache-projection.md §1.1/§1.2/§1.3 逐行：派生字段表、固定值（client-checksums/unique 落注释不落字段）、`<K>-cache` 恒定拼接、storeArtifactsLocally gate、无持久实体——全部一致；无 clean-room 嫌疑（实现是 BinFlow 惯用的 store 现读派生，非 Java 逐行翻译）
Security:      攻击面=只读 + 日志。remoteKey 直拼日志/拼接 Key 无注入面（非 SQL/非 HTML 语境）；store 错误文本进 WARN 无凭据外泄（metadata 层错误不含 secret）；无路径穿越面（不触文件系统）
Performance:   每调用一次 `Repos().Get`（单行主键 SELECT）+ 一次 `json.Unmarshal`（config blob KB 级）——与既有 fetcher `loadRepo`/repo 侧探针同量级，四桶序每 remote 成员一次，无锁竞争点（不触 Engine.mu/clients）；O(1) 无分配热点
Risks:         ① handleReleases/handleSnapshots 当前恒读 true（remote canonical 结构不持久化这两键——已核实 config.go:72-154 无此字段）；若后续票落这两键的存储，投影自动跟随，无需改本文件。② repoLayoutRef 缺省 literal 双源（defaultProjectionLayoutRef ↔ repo.defaultRepoLayoutRef，import 环），同步注释已落两处，漂移窗口小。③ store 故障静默降级为无投影（签名无 error 席位，设计定死）——四桶序该 remote 的 cache 步缺席属既登记权衡
Blockers:      无（全部取证命令可跑且跑通）
Next:          ① 范围外发现（见下）建议开 D-票：internal/replication/probe.go:113 的 `strings.HasSuffix(targetRepo, "-cache")` 是 CacheSuffix 之外全仓唯一非注释硬编码，且 internal/replication 已 import internal/remote（engine.go:18），替换为 `remote.CacheSuffix` 一行改动、无环。② 本票属 storage/remote cache 关键域——按双审强制域规则应已有/补 reviewer-b 实例（架构/契约面）；我未读取也不受任何 B 形态报告影响。③ T-530（internal/repo 消费面）落地时验证 `remote.CacheSuffix` 对接与本注册表注入

## 结论区

结论: **APPROVE**

### 必须修改（blocking）

无。逐项核验结果：

1. **并发读安全（派发重点 a）**——`CacheProjection` 不触任何 Engine 共享态（mu/clients/extClients 均不进入），探针 `pc` 是方法局部变量，`json.Unmarshal` 无半解析外泄窗口；`repoStore.Get`（substores.go:43-55）每调用 `r := &Repo{}` 新构返回，无共享可变指针，sqlite 单语句快照保证 Update 整串原子可见。`go test -race -run TestCacheProjection` 独立复跑 ok（8 读者 × 500 次 × 1 写者 200 次配置翻转）。
2. **错误路径（重点 b）**——未知 key：Get 返回 `fmt.Errorf("... %w", ErrRepoNotFound)`（substores.go:53），`errors.Is` 判定成立（metadata/store_test.go:62 契约钉死），静默 `(零值,false)`；非 remote 行：`row.Type != "remote"` 与 `repo.TypeRemote = "remote"`（api.go:140）一致；config 损坏：先预置默认再 Unmarshal，失败后**整体重置**为默认（projection.go:148——正确处理了 Unmarshal 部分解码残留），WARN 后不 panic 不外泄；store 故障：WARN（repo key + err.Error()，store 错误文本无凭据）+ `(零值,false)`，`e.log` 经 NewEngine 兜底 `slog.Default()`（fetcher.go:347-349）无 nil panic。
3. **CacheSuffix 单源（重点 c）**——全仓 grep：非测试 Go 码中唯一第二处是 internal/replication/probe.go:113（T-422 遗留、非本票引入，见 Next）；adapter/rpm/virtual.go:32 为注释。测试钉死拼写 `"-cache"`。
4. **八字段派生（重点 d）**——与设计 §4 逐字段一致：Key=remoteKey+CacheSuffix、PackageType 镜像行、PriorityResolution 继承 JSON 键（拼写与 repo.remoteConfig tag 逐一核对：priorityResolution/repoLayoutRef/blackedOut/archiveBrowsingEnabled）、handle\* 用 `*bool` 缺省 true（remote canonical 不持久化此二键已核实）、gate `!*StoreArtifactsLocally` → 无投影（缝位，与 httpapi 恒 true wire 一致）；固定值 client-checksums/unique 按设计落注释。
5. **零持久化（重点 e）**——projection.go 全文仅 `Repos().Get` + `log.WarnContext` 两个副作用点，结构上不可能写任何存储面。
6. **fetcher.go 零改动**——`git diff --stat` 空；worktree 内 internal/remote/ 仅两个新增未跟踪文件。
7. **reload 语义**——每次调用现读现派，严格强于「reload 时重建」；测试钉死改配置下一调用可见 + 删行即无投影。

### 建议改进（non-blocking）

1. projection_test.go:264-277——并发读者仅断言 `ok` 与 `Key`；flip 两态 config 的 RepoLayout 恒为 "maven-2-default"（flipped 无 repoLayoutRef 落默认），可加一行 `proj.RepoLayout` 恒等断言，把「内部一致性」从注释变成检查。
2. projection_test.go:99-106——损坏 blob 用例仅 `"{not json"`；可补一行类型错配的合法 JSON（如 `{"priorityResolution":"yes"}`），钉死「部分解码后重置默认」路径（代码已正确处理，测试未直接覆盖）。
3. 记录性：派发输入称「16 子用例」，实测 15 个叶子用例（12 t.Run 子用例 + 3 独立函数）；T-529.md 自身口径（5/3/4 子用例 + 3 独立）准确，非实现方偏差。
