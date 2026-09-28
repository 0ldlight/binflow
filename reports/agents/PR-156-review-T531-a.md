# PR-156 Review — T-531（Reviewer A · correctness 形态）

```
Ticket:        T-531 [P0] Maven virtual 聚合的 Facet 消费——四桶序下 maven-metadata.xml 合并跳过 cache 投影步
Role:          code-reviewer (reviewer-a)
Area:          internal/adapter/maven（virtual metadata 聚合面）
Input:         conductor 派发（T-531，形态 reviewer-a）；通读 virtual_metadata.go 全量 diff 与现行全文、新增 virtual_metadata_cache_skip_test.go、T-531 15 字段日志、docs/design/virtual-four-bucket.md §2.1/§2.3/§3/§5.3/§6/§8、docs/reverse/virtual-resolution.md §2/§3.4/§3.6/§5.1、docs/reverse/remote-cache-projection.md（handle* 继承拼写核对）；下游追读：internal/repo/virtual.go（virtualMemberOrder/expandVirtualMembers/VirtualMemberOrder/ReadVirtualMember/readRemoteMemberDoc）、internal/repo/api.go（Service 契约注释、ClassReader）、internal/adapter/maven/{config.go,layout.go,calc_test.go,virtual_metadata_test.go}、internal/metadata/substores.go（repoStore.Get）、cmd/binflow-server/main.go:429（class 注入点）、internal/remote/projection.go:97-113（remote 行 handle* 拼写，只读核对，不评审）
Changes:       diff 全量逐行（virtual_metadata.go 140+/14-：memberFacet 镜像 + facetOfStep seam 读点 + metadataLevel/metadataLevelOf + metadataWalkStep + filterMetadataSteps + virtualMetadataSteps + collectVirtualMetadata 改造）；上下游追读至 repo 序装配与 FR-20 读链；存量 T-72 测试未改动经 git status 核实（包内仅 virtual_metadata.go 改 + 新测试文件）
Files:         internal/adapter/maven/virtual_metadata.go — 逻辑主体见下方逐条；internal/adapter/maven/virtual_metadata_cache_skip_test.go — 覆盖面合格（详 Tests）；reports/agents/T-531.md — Blockers 字段对 HEAD 准确、对现行工作区已失真（详结论 B1）
Tests:         go test ./internal/adapter/maven/ -count=1 → ok 29.238s（全包，含存量 T-72 回归）；go vet ./internal/adapter/maven/... → 通过；golangci-lint run ./internal/adapter/maven/... → 0 issues；gofmt -l internal/adapter/maven/ → 空；新腿 -run 定向 -v → TestMetadataWalkCacheFacetSkip / TestMetadataWalkLevelPolicySkip / TestMetadataLevelOfClassification / TestVirtualMetadataSnapshotPolicySkip / TestVirtualMetadataModulePolicySkip 全 --- PASS。无 skip 当 PASS。注意：上述全包跑是在「T-531 + T-530 未提交改动共存」的工作区上执行的（见 Risks/B1）
Commands:      git -C <worktree> diff internal/adapter/maven/virtual_metadata.go；git -C <worktree> status --porcelain internal/repo/（→ " M internal/repo/service.go" " M internal/repo/virtual.go"，virtual.go +451/-行 未提交）；git show HEAD:internal/repo/virtual.go | grep -c FacetCache（→ 0：HEAD 确无 cache 步发射）；go test ./internal/adapter/maven/ -count=1；go vet ./internal/adapter/maven/...；golangci-lint run ./internal/adapter/maven/...；gofmt -l internal/adapter/maven/；go test ./internal/adapter/maven/ -run 'TestVirtualMetadataSnapshotPolicySkip|TestVirtualMetadataModulePolicySkip|TestMetadataWalk|TestMetadataLevelOf' -count=1 -v
Outputs:       reports/agents/PR-156-review-T531-a.md（本文件）
Compatibility: A 视角核对：与 docs/reverse/virtual-resolution.md §5.1 L88 一致（快照级跳 handleSnapshots=false、cache 步恒滤）；§3.4 模块级镜像为已登记漂移点（见下方 non-blocking N2）；I11「合并遍历不含 cache 步」在纯函数层有测、在真实序层**当前不成立**（B1）——这正是兼容性缺口本体
Security:      无新增输入面：path 仅过 splitDirFile 做后缀分类，validateNodePath 门在 ReadVirtualMember 内不变；ParseRepoConfig 只提取布尔、不落日志，warn 日志仅 virtual/member/error 三键，无 config 泄漏；无凭据接触；无注入/穿越新面
Performance:   每请求新增 len(order) 次 class.Get——实现日志称「内存行缓存/小表」**不准确**：metadata/substores.go:43 是 QueryRowContext DB 点查，main.go:429 注入的即原始 store，无缓存层（N1）；量级受既有 per-member 走查成本约束（同阶），非阻塞。B1 双读问题会再放大 per-remote 成本一倍——修 B1 即消
Risks:         ① 工作区现状 = T-531 与 T-530 的 internal/repo 改动（virtual.go 四桶装配 + VirtualMember.Facet + cache 步发射，均未提交）共存：我的全包测试跑在该组合态上且全绿——**绿恰是危险信号**：merge 幂等（versions `seen` 去重、快照块 bn==bn 且时间戳相等不替换）使双份成员文档合并后 body 不变，第二次 FR-20 读命中刚落地的 standing copy 使 upstream hits 不翻倍，故无任何现有断言能暴露 B1；② swap 后 handle* 读 remote 行的拼写一致性已核对（projection.go:112 `json:"handleReleases"/"handleSnapshots"` 与 ParseRepoConfig 同 key）；③ memberFacet(m.Facet) 数值转换依赖两枚举 iota 序对齐——设计 §2.1 已钉死（Plain=0/Cache=1 两侧同序），swap 时应顺手在该行注释留锚
Blockers:      无取证障碍（测试/vet/lint 全部可跑且已跑）
Next:          ① conductor：T-531 收编时当场执行 B1 一行 swap + 补一条真实序 cache 步跳过断言（fixture 已有 f.hits 计数器，virtual_metadata_test.go:27）；② 范围外交 conductor：repo/api.go ClassReader 契约注释「exactly one consumer additionally reads the row's canonical CONFIG JSON」（T-367 措辞）已因本票成为两个消费者（helm + maven）——属 internal/repo 文件，随 T-530 收编时更新；③ 范围外交 conductor：repo/api.go VirtualMemberOrder 契约注释仍写「two-bucket resolution order」，与工作区 virtual.go 四段实现不符——T-530 收编项；④ 差分票候选已在 T-531 日志登记（模块级 handleReleases 跳过），维持
```

## 评审报告 T-531（形态: reviewer-a）

结论: REQUEST_CHANGES

### 必须修改（blocking）

- **B1 · internal/adapter/maven/virtual_metadata.go:141-143 — seam 交换点必须在本变更集内执行：工作区前提已失效，照此提交即发布「每个 remote 成员双读」。**
  证据链（全部现行工作区直读）：
  1. `git status --porcelain internal/repo/` → ` M internal/repo/virtual.go`（+451 行）、` M internal/repo/service.go`（+92 行）——T-530 的四桶序装配**未提交但已在本 worktree 工作区落地**；
  2. 该 virtual.go 的 `virtualMemberOrder`（约 :192-199）对每个 remote 成员**发射两步**：`cacheStep.facet = FacetCache` 先入序、本体步后入序；导出 seam `VirtualMemberOrder`（:634-645）**原样透传全部步**（`out = append(out, VirtualMember{Key:…, Facet: m.facet, …})`，无 plainSteps 过滤）；
  3. 而 `facetOfStep`（virtual_metadata.go:141-143）body 是 `return facetPlain // T-530: swap to memberFacet(m.Facet)`——**不读 m.Facet**。函数注释的前置断言「Until then the two-bucket order emits no cache projections at all, so plain is exact」对 HEAD（`git show HEAD:internal/repo/virtual.go | grep -c FacetCache` = 0）成立，对**当前工作区为假**；
  4. 后果：`virtualMetadataSteps` 给每个 remote 造出两个 plain 步 → `filterMetadataSteps` 滤不掉任何东西 → `collectVirtualMetadata` 对同一 remote key 调 `ReadVirtualMember` 两次 → `readRemoteMemberDoc` 走两遍 FR-20（当前 ReadVirtualMember 的 remote 分支不区分 facet，恒 FR-20）。这正是 I11 要防的形态：一个 remote 被读两次、下载统计双计、缓存语义双跑——**且全包测试全绿掩盖它**（merge 幂等 + 第二读命中刚落盘的 standing copy，详 Risks ①）。T-531 新测试也测不到：cache 腿全部跑合成步表，真实序层零覆盖——票面把真实序断言推迟到「T-530 落地后」，而 T-530 已在工作区里。
  5. T-531 报告 Blockers 字段声称「本 worktree HEAD 72484f51 均无 Facet 字段」——对 HEAD 准确、对本工作区现状失真（实现者写作时大概率为真，非虚假证据，但收编依据它做「plain 恒真」判断会出错）。

  改法（最小 diff，字段已存在，今天即可编译）：
  - virtual_metadata.go:141-143 body 换为 `return memberFacet(m.Facet)`，删 `//nolint:revive` 与失效前置注释（可留一行「数值转换依赖设计 §2.1 的 iota 序对齐」锚）；
  - 补一条真实序断言收掉推迟项：在现有 fixture 上（`virtualFixture.hits`，virtual_metadata_test.go:27 已有 upstream 计数器）断言「virtual 含 remote 成员的模块级 metadata GET 后 hits 计数不因 cache 步发射而翻倍」——把 I11 从注释级升到测试级；
  - 重跑 `go test ./internal/adapter/maven/ -count=1` + lint。
  若 conductor 选择替代路径（T-531 先于 T-530 单独提交、swap 归 T-530 收编），必须显式登记该顺序约束——但两套改动此刻同栖一个工作区，一行 swap 即可消除全部歧义，没有理由保留硬线。

### 建议改进（non-blocking）

- **N1 · reports/agents/T-531.md Performance 字段** — 「ClassReader 行缓存/小表，内存行读」表述不准确：`metadata/substores.go:43` repoStore.Get 是 `QueryRowContext` DB 点查，`cmd/binflow-server/main.go:429` 注入的即该 store，无缓存层。量级判断（可忽略）本人认同——受既有 per-member 走查成本同阶约束、T-367 helm 先例同款——但证据措辞应改，避免后人据此做容量推演。
- **N2 · virtual_metadata.go:198-199 模块级 handleReleases 跳过（已登记漂移点独立复核）** — 票面承诺「一行可翻回」成立：单条件行 `if level == levelModule && !s.HandleReleases { continue }`，删除即回到不滤；函数注释已留规格锚。快照级 handleSnapshots 跳过（:195-196）有 reverse §5.1 L88 高置信直接依据，无漂移。维持登记不判 blocking。
- **N3 · virtual_metadata.go:220-237（swap 后）** — cache 步会被滤掉，但 `virtualMetadataSteps` 仍先对每个步做 class.Get 行读（cache 步的行读纯浪费）。可在构步时先判 facet 跳过行读，省 len(remotes) 次点查。量级极小，顺手项。
- **N4 · 测试断言强度（合格，记录两处可再拧半圈）** — TestMetadataWalkCacheFacetSkip 断言完整有序 key 序列（好）；真实栈两腿用 want+ban 双向断言（好）；TestMetadataLevelOfClassification 可补一条 sidecar 形态（`com/acme/lib/1.0-SNAPSHOT/maven-metadata.xml.sha1` 的 Target 分级）锁住「filter 分级与 merge 分级同源」——当前一致性靠 `Module = dirs[last]` ⟺ `HasSuffix(dir, "-SNAPSHOT")` 的数学等价成立（末段后缀 ⟺ 全目录后缀；sidecar 的 Target 承载目标目录；根级 maven-metadata.xml 在 Parse n<3 即拒），无缺陷，仅锁得更死。

### 复核过的正确性要点（无缺陷，留证据）

- **metadataLevelOf 三形分类**：group/artifact 级 → module；version 级（目录 -SNAPSHOT 后缀）→ snapshot；与 mergeMetadataDocs 的 `l.Module` 分级恒一致（layout.go:152-154 `Module = dirs[len(dirs)-1]`；整目录后缀 ⟺ 末段后缀）；非法路径进不了本面（Parse 的 n<3/裸 -SNAPSHOT 拒绝在前）。
- **空步集行为**：filter 后空 → collectVirtualMetadata 返回空 docs（非 nil）→ serveVirtualMetadata `len(docs)==0` → 404，与 §5.1「全 miss → 404」一致；foundByPriority 短路逐字保留（virtual_metadata.go:253-256，仅换遍历源），纯函数腿「继承优先级 cache 步被滤不扰动记账」有测。
- **memberFacet 镜像 vs 设计 §2.1**：枚举序对齐（Plain=0/Cache=1 两侧同序，设计钉死）；Key 语义（cache 步带 remote key）与 Priority 继承语义在步表结构与注释中一致；镜像省略 Type 字段——本面不需要（ReadVirtualMember 自行按行分派），非偏差。
- **handle* 读取失败路径**：ParseRepoConfig 对空/坏 JSON/缺键一律 lenient 默认双 true（config.go:56-96，`*bool` 指针判 nil），行读失败同样双 true + warn——两路一致、无静默丢成员、无类型断言无 panic；remote 行 canonical 拼写与读取 key 一致（projection.go:112-113）。
- **存量 T-72 回归未动**：git status 包内仅 virtual_metadata.go 改 + 新测试文件；对 HEAD 单独评估时（两桶序无 cache 步，facetPlain=plain 恒真）T-531 的行为增量只有两条策略跳过（票面行为），等价性主张在该前提下成立。
- **并发/资源**：无新 goroutine、无共享可变状态、步表每请求私有；rc.Close 既有 errcheck 豁免维持。

范围外标注（不评审）：internal/repo 四桶 walk 与 service.go（T-530 在途——但其与 T-531 的交互是 B1 本体，已审）；internal/remote/projection.go（T-529 已双审）；CI/Makefile（T-526 已审毕）。
