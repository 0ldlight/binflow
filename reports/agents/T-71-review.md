# 评审报告 T-71（视角: correctness）

结论: **APPROVE**

- 评审对象: commit `eab4363`（internal/repo/virtual.go 新 / service.go 分派 / api.go 文档 / virtual_test.go / export_test.go）
- 契约核对: repo-semantics §8.1/§8.2（高置信度）、ADR-0013 + T-79 联动勘误①②、PRD v1.2 FR-21-AC1~AC9 / §5.5 C5 / line 92（权限叠加 M4 延后）
- 取证（实际执行，工作区含并行票未提交改动 fetcher.go 等，repo 包测试跑在当前树）:
  - `gofmt -l internal/repo/` → 空
  - `go vet ./internal/repo/` → 零输出
  - `go test -race -count=1 ./internal/repo/` → **ok 47.931s**
  - 源码级核对 internal/remote/fetcher.go（六步序/负缓存先行/HasCopy 不变式）、cachestate.go（kind="negative" 拼写与 entryFresh 语义）、metadata/substores_remote_virtual.go（SetMembers 事务整体替换 + ListMembers `ORDER BY position`）

## 逐项核实（对应派发要点）

1. **两桶序正确性 — 通过**。`virtualMemberOrder`（virtual.go:103）逐请求调 `ListMembers`（`ORDER BY position, member_repo`，SetMembers 为单事务 DELETE+INSERT——每请求看到一致成员列表，无半新半旧窗口）；优先桶=桶内声明序、其余=桶内声明序，与 PRD C3 两桶简化逐字一致。优先标记探针（virtual.go:146）坏值安全默认 false，`validateLocalConfig`（config.go:268）已建仓面钉 bool 形。矩阵测试 10 例覆盖 local/remote 交叉、无隐式 local-first、标记越位、双标记桶内序、四成员混合——断言 body+Resolved-From+Cache 三面。AC2/AC9 的「下一次请求即变」由 `TestVirtualMemberChangesImmediateEffect` 钉（零解析缓存，代码与测试互证）。
2. **stale/miss 语义（R10）— 通过**。`probeRemoteMember`（virtual.go:240）消费 `FetchResult.HasCopy`：引擎侧核对确认「成功必带副本」（fetcher.go:602/746 全部 HasCopy:true；true miss 只以 `*FetchError{Unfound}` 出现）。stale（含 assumed-offline 期、expired-but-serving）→ hit 即停，`TestVirtualRemoteStaleHitDoesNotSkip` 断言 STALE 头+Upstream-Error+不落向后位成员。负缓存/无副本/offline 无缓存 → Unfound → 继续桶序（AC7 三枚举各有子例）。**非 Unfound 故障透传的自定决策：确认**——AC7 定案文案「仅成员真正 404 …才继续」的严格读法；跳过会把 SSRF 400/hardFail 502 掩蔽成全仓 404，安全面更差（见 non-blocking ②，需 QA 钉板防日后被「顺手修复」）。stale 服务伴随写下的负缓存行保留（成员自身状态机，负缓存先行是 T-66 钉板序），下一请求真 miss 继续——组合行为已被测试固化，机械正确。
3. **pre-read guard 时序 — 通过（含一条并发窗口备注）**。顺序面完全正确：`hasFreshNegative`（virtual.go:288）与引擎 `entryFresh` 语义逐字段一致（RFC3339+Before，`""`/坏值→false）；kind 拼写 `"negative"` 与 `cacheKindNegative` 同串（virtual.go:283 注释钉 + `TestVirtualTrueMissFallsThrough` 负缓存子例的「行存活+上游计数不增」断言会抓拼写漂移）。探测 Unfound 后仅删本次探测所写（guard 真→不删），`DeleteCache` 的 NotFound 容错正确。
4. **写路由 — 通过**。未配 → `refuseVirtualWrite` 405+`Allow: GET`+C5 文案与 repo-semantics §8.2/PRD §328 **逐字节相等**（含句尾句号）；三 Put* 入口（service.go:319/414/504）均在排空 body/权限对**之前**换址；换址后走目标仓原生 `authorizeContentPut`/checksum 链/audit（AC4「按目标仓语义」的忠实实现，`TestVirtualWriteRoutedToDeploymentRepo` 覆盖权限对/覆盖/三变体/folder 哨兵/摘路由即 405）。virtual DELETE 恒 405、配路由后用 truthful 措辞（spec 未给该分支文案，自拟合理——对配了路由的仓说「未配置」确是说谎）。容忍式路由读取（三别名、`{}` 裸行→无路由→405）与 T-67 harness 兼容目标吻合，且严格形（`parseVirtualConfig` 别名分歧即拒）只在建仓面——两读者不会给出相反答案（canonical 形只写 `defaultDeploymentRepo`）。
5. **ExtraHeaders 合并 — 通过**。`withResolvedFrom`（virtual.go:316）拷贝底层 hints 后 `Set` 叠加 Resolved-From（canonical key，覆盖式，底层不可能已带该头）；`resolvedFromReader` 内嵌委托 Close 无泄漏。generic 面 wire 级双头证据在实现者日志（local→Resolved-From 单头；remote→+Cache MISS）。
6. **三型 switch 回归 — 通过**。`Get`（service.go:240）TypeLocal 空臂直落既有本地面，remote/virtual 分派前后代码零改动；List 维持 AC8 P2 拒绝；`-race` 全包绿。
7. **clean-room 抽查 — 通过**。实现的是 PRD C3 两桶简化（与 Artifactory 四桶**有意识分歧**——BinFlow 无 `-cache` 影子仓投影），常量/文案全部溯源 docs/reverse 行为规格与 PRD 定案，无反编译结构残留、无逐行对应嫌疑。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

1. **virtual.go:248-262 pre-read guard 的并发 TOCTOU 窗口**：V 预读无行 → 并发直接 GET 写下 fresh 负缓存行 → V 的 Fetch 第 3 步以该行应答 Unfound（V 未写任何行）→ V 仍删除该行。后果上限=损失一个负缓存窗（下一次 miss 多打一次上游 404，自愈，无正确性影响），但实现者「直接 GET 的合法负缓存必须在 virtual 扫描后存活」的不变式在并发交错下不成立。建议：Unfound 后重读行，仅当 `entry.FetchedAt > 本次探测开始时刻` 才 `DeleteCache`（`RemoteCacheEntry.FetchedAt` 已有，~5 行）。
2. **非 Unfound 透传的 QA 钉板请求**：hardFail:true 成员 offline 时整个 virtual GET 502——**即使后位成员持有该制品**（`TestVirtualMemberFaultsPropagate` 已固化）。这是 AC7 严格读法的必然后果，本评审确认；请 T-74/T-75 把它作为**预期行为**钉进矩阵，防止日后被当缺陷「修复」成跳成员（那会违反定案文案）。建议 architect 下次回写 ADR-0013 时补一句该分支。
3. **读写权限面不对称（规格正确，需文档化）**：读门在 virtual key（成员 ACL 不参与——PRD line 92 明确 M4），写面权限按目标成员仓（AC4 字面）。管理员只授 virtual write 不授成员 write 时 deploy 403，易被误报为 bug。建议 tech-writer 在 M3 接入文档写明，QA 场景加一条。
4. **virtual.go:103 逐请求 N+1 成员查询 + 每成员 JSON 探针**：M3 规模（成员数个位）无虞；M4 若 virtual 数量/成员数上量，这里是第一个加缓存点（缓存失效钩子=UpdateRepo）。记档即可。
5. **成员 remote_configs 行丢失（create 崩溃窗）时该成员令整个 virtual 读 500**（fetcher.go:862 拒绝）——与直接 GET 该成员行为一致（诚实暴露、UpdateRepo 可愈），非 virtual 特有缺陷；与「成员行漂移 WARN 跳过」的容忍哲学不完全对称，接受现状。
6. **virtual.go:283 `negativeEntryKind` 跨包字符串重复**：仅注释维系同步；行为测试已实际钉住（拼写漂移→guard 恒 false→「行存活」断言翻车）。可选：在 export_test 暴露一个与引擎侧常量的编译期/测试期等值断言更稳。

## 范围外发现（→ conductor，转 T-82）

实现者遗留①②的严重度核实为**属实且略低估**，~10 行/adapter 的修法（照 generic/handler.go:276 ExtraHeaders 探测 + :390 StatusError 优先渲染）**充分**。本评审补充三点具体证据：

- **maven** `internal/adapter/maven/handler.go:286-306`：`method != PUT/POST → 400`（DELETE 面 405 缺失，日志已记）；**另发现**：自拟 405 臂发 `Allow: GET, HEAD`，而 PRD FR-21-AC3/repo-semantics §8.2 钉的是 **`Allow: GET`**，service 的 StatusError.Header 携带正是 `GET`——即使 PUT 面，maven 路由今天也在 Allow 值上违反钉板（M52 文案恰好逐字所以 curl 过了）。T-82 修法必须**逐字渲染 StatusError.Header**，不能保留本地臂的 Allow 拼写。
- **npm** `internal/adapter/npm/errors.go:65-82`：`writeServiceError` **两缝皆缺**（无 StatusError 优先分支、无 ErrRepoTypeNotSupported 臂）→ 未配路由的 virtual publish 面落 `default` → **500**（比日志「npm 待查同缝」的预期更差）。T-82 需一并覆盖。
- **pypi** `internal/adapter/pypi/handler.go:225`：`ErrRepoTypeNotSupported → 400` 恒定，PUT/DELETE 双面皆错（PUT 面 400+裸 err 文案，应 405+C5）。

时序提醒：T-82 必须在 T-74/T-75 QA 前收口（ExtraHeaders 缺失会令 T-75 的 M50/M51/M55 Resolved-From 头断言在 maven/npm/pypi 路由上翻车——日志判断正确）。

## 验证覆盖评估

17 个测试群均为真栈（真 storage+sqlite+httptest 计数上游+负断言），表驱动，断言到头值/文案/上游计数/行存活四层；真二进制 curl 闭环（M50/M52/M53+generic wire 头）与单测互证。覆盖对 P0 AC（AC1/AC2/AC3/AC7/AC9）+ AC4 P1 全命中。mvn 真机腿归 T-74/T-76 合理（服务面路径同缝）。
