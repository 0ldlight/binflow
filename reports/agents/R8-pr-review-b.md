# R8 载荷评审 — Reviewer B（architecture 面）

```
Ticket:        R8 载荷区间 49cb5cae..HEAD（BIN-49 五族 Location / BIN-50 pypi 响应头 /
               BIN-52 mime 表对齐 / T-569 proposal+规格 / 台账批），P1
Role:          code-reviewer (reviewer-b, architecture)
Area:          internal/adapter/{deb,rpm,helm,nuget,cargo,pypi,generic,maven} +
               docs/compatibility/{known-divergence.yaml,proposals/} + docs/reverse/
Input:         conductor 派发（评审形态 reviewer-b）；通读区间 6 commit 全 diff、
               docs/compatibility/proposals/mime-ownership-ruling.md、
               docs/reverse/mime-ownership.md、reports/agents/T-56{7,8,9,70}.md、
               docs/design/architecture.md 分层约定（adapter 域隔离/无跨族 import）
Changes:       评审 26 文件 +1386/−97；上下游追读：五族 writeCreated/serveDerivedWrite/
               serveBareContent/servePush 全调用点、generic handler CT 消费点（PUT 100-104/
               checksum-deploy 217-220）、maven put.go CT 消费点（217-220、282-285）、
               httpapi/repositories.go:612（既有第 9 份 requestBase）、pypi simple.go escapePath
Files:         逐文件结论见下方评审结论区
Tests:         go build ./... → exit 0；go vet ./internal/adapter/... → exit 0；
               五族 Location 新测全绿（deb 3 腿/rpm 3/helm 3/cargo 3/nuget 2+2，-count=1 -v）；
               generic+maven mime 测试与 pypi TestUploadResponseHeaders 全绿；
               gofmt -l internal/adapter/ → 空；known-divergence.yaml yaml.safe_load 通过
Commands:      git log/diff --stat 49cb5cae..HEAD；awk+md5 逐字比对 9 份 requestBase 与
               8 份 escapePath；python3 解析两表键值交集/差集；python3 统计台账
               open/resolved 分布；上述 go build/vet/test/gofmt
Outputs:       reports/agents/R8-pr-review-b.md（本文件）
Compatibility: 见「焦点逐项」——台账批与 L036 六点裁定/两票制 proposal 自洽，
               open 计数 46（BUG 5）与 commit 777049cf 声称一致（脚本复核）
Security:      本批无新攻击面：Location 各段均经 url.PathEscape 逐段编码
               （escapePath 拷贝逐字一致）；pypi X-Checksum-Sha256 取会话摘要无重算窗口；
               无凭据落盘（T-568 报告凭据走 /tmp env 注入，日志零凭据值）
Performance:   Location 渲染为 O(path 段数) 字符串拼接，非热 path；mime 表为纯 map
               值替换，查表路径零结构变化（T-570 报告同结论，复核属实）
Risks:         1) requestBase/escapePath 9+8 份拷贝的长期漂移面（见焦点一，收敛触发条件）；
               2) maven 表缺 5 个出厂表条目，BIN-53 删 stdlib 回退后将成 maven 域新 DIFF 面
               （见焦点二/必须注意项）；3) 本批 resolved 的活体差分腿全系 license 门欠账
               （台账逐条标注，门解锁后需补拍——欠账在案非语义存疑）
Blockers:      无（取证环境完整：build/vet/test/lint 全部可跑且已跑）
Next:          1) BIN-53 立票时并入两条 rider：maven 表补 .md/.html/.htm/.yaml/.yml 5 键；
               proposal §3-3a「无扩表」表述需修正——两表现仍是出厂表 v17 的真子集，
               删回退后表外扩展名从「宿主漂移值」变「恒 octet-stream」，对 A 表内条目
               （css/java/py/mf/…）是可预期新 DIFF，BIN-53 需显式裁「补全表 or 登记残差」；
               2) generic/handler.go:99-100 陈旧注释「Artifactory honors the header too」
               与本批已提交的 docs/reverse/mime-ownership.md 第 1 条（高置信活体）相反，
               按 proposal 计划随 BIN-53 勘误（proposal §1 原文指向 maven put.go，实际
               残留在 generic/handler.go——BIN-53 勘误时按此定位）；
               3) 双审确认：本批覆盖 storage 域关键面（adapter 存储渲染），reviewer-a
               独立报告应在案（本次为 B 实例，A 由另实例出具）
```

## 焦点逐项（architecture 证据）

### 一、Location 三件套本地拷贝模式（BIN-49/T-567 + T-568）

- **逐字一致性**：`requestBase` 9 份（deb:549 / rpm:577 / helm:569 / cargo:365 /
  nuget flat:537 / pypi upload:273 / generic handler:552 / maven put:564 /
  httpapi repositories:612），`escapePath` 8 份（deb:558 / rpm:586 / helm:578 /
  cargo:374 / nuget:546 / pypi simple:336 / generic:561 / maven:573）——awk 提取函数体
  md5 全同（requestBase=d5e776e6…，escapePath=4d81a5bc…）。`productPrefix` 常量值
  9 处同为 "/binflow"，注释按族差异化（各自记录该族的 A 面证据锚），且每份新拷贝带
  互注（"Same render rule as adapter/maven's T-563 and adapter/generic's T-564 …
  no cross-package import"）——**裁定先例（本地拷贝+互注、禁跨包 import）被完整执行**。
- **无跨族 import 复核**：五族新改动对 `internal/adapter/...` 的 import 仅止于根包
  `internal/adapter`（Principal 等共享类型），无 adapter→adapter 依赖。包边界干净。
- **cargo 推断级诚实标注**：cargo/handler.go:380-397 productPrefix 注释如实记录
  A 面建仓被 Custom Base URL 实例门挡、裁定级=INFERENCE（跨族通则），与台账
  cargo 条 resolved 块「推断级标注维持」互相印证——诚实归因链完整。
- **漂移风险与收敛路径**（建议触发条件，不要求本轮做）：当前 9 份拷贝零漂移、单份
  12 行，收敛成本 > 漂移成本，维持现状合理。触发升级共享 internal 包（如 internal/render
  或落 internal/adapter 根包）的三个条件：① 渲染规则本身要变（如加 X-Forwarded-Proto
  代理链 scheme 解析、或 productPrefix 可配置化）——规则变更 × 9 处即危险；② 第 10 个
  adapter 需要三件套；③ 任一族 probe 定罪其拷贝需要不同行为（语义漂移显形，需要共享
  缝以显式分叉）。届时一并收编 httpapi/repositories.go:612 那份（它已说明「adapter
  域隔离」理由对 routing 层不成立）。
- **nuget v3 push 反向裁定**（flat.go:352-357 删 Location、249-253 注释）：与五族
  「绝对化」方向相反但证据驱动（A 面 201 无 Location），非架构不一致——模式是
  「逐面按 A 活体裁」，两方向都服从同一裁定程序。

### 二、mime 表所有权边界（BIN-52/T-570）

- **双表并存 vs 单表**：proposal §2 甲案两票制（票 1 表值先行 / 票 2 模型翻转）明确
  「generic 与 maven 两张自有表同步」——双表是裁定形态，本轮不共享是对的。脚本复核：
  27 个共享键**零值冲突**；generic 独有 5 键（.md/.html/.htm/.yaml/.yml，32 键 vs 27 键），
  maven 无独有键。同值面完全收敛，剩余差异是 maven 侧缺口（见下）。
- **排除项可追溯性**：.info/.mod 在 generic/mime.go:19-20 注释明示「goproxy protocol
  face owns their spellings（adjacent divergence, ruled separately）」；docker/helmoci
  OCI mediaType 排除在 proposal §1 表行（「排除（OCI 内容协商要求，T-32 R3 已裁）」）
  与 mime-ownership.md §1 可溯——排除决策住在裁定记录里，两张表不需要为不存在的
  docker 表写注释，可接受。maven 表注释未提 .info/.mod（maven 域 .info/.mod 不现实，
  琐碎，不计）。
- **.sha512 删除的边界处理**：maven 表删条目而 sidecar GET 面 x-checksum 协议常量
  （handler.go:36、calc.go:73）不动——T-570 报告声明与代码一致（复核 mime.go 全文，
  无 .sha512 键）。表值层与协议常量层分离正确。

### 三、BIN-53 可承接性

- **翻转面干净**：「存储值权威」语义收口在两个 resolver——generic mimeForNode /
  maven mimeForPath（均为包级单函数），GET/HEAD/FileInfo 三面同源经它们；声明 CT
  消费点全仓只有 generic handler.go:100-104+217-220、maven put.go:217-220+282-285、
  conan v1/v2、pypi upload（proposal §1 清单与代码对得上）。翻转=改这些调用点的取值
  规则，非 schema 变更（渲染时查表免数据迁移，规格 §1-6 佐证）。改动面估计与票面相符。
- **两个承接缺口**（不阻断本批、须随 BIN-53）：
  1. maven 表缺 .md/.html/.htm/.yaml/.yml 5 键（generic 有、出厂表 v17 有、generic 面
     活体已证）——本票按 proposal §3 群 1/群 2 清单逐字执行，群 1 只改「已有条目拼写」
     而 maven 原表没有这 5 键，属**清单粒度漏项非实现错**；但 BIN-53 删 stdlib 回退后
     maven 域无声明 .md 腿将从「宿主漂移值」变「恒 octet-stream」vs A= text/plain，
     制造 maven 域新 DIFF。
  2. proposal §3-3a 称删回退「无扩表」——只对两表已覆盖的扩展名成立；两表合计仍缺
     v17 的 css/java/py/mf/xz/bz2 等约 20 键，删回退后这些腿对 A 是可预期 DIFF。
     BIN-53 票面需显式裁「补全表 vs 登记残差」，否则差分矩阵会再现一批「已知但未入账」
     的腿（违反台账完备性纪律）。

### 四、测试架构

- **五族 created_location_test.go**：共同骨架（stack→seedRepo→表驱动 PUT→断言绝对
  Location 字面值→GET 回读锚）确实存在，但差异部分是**真域负载**——deb 的矩阵坐标
  不入 Location 断言（SplitN ";" 剥离）、helm 的 chart fixture、nuget 的 nupkg builder
  与「无 Location」负测、cargo 的 derived 家族三腿。抽取共享骨架需建跨包 test-support
  包，违反域隔离粒度而只省 ~70 行断言循环——**当前取舍正确**（过度复制未发生：每份
  66-95 行，其中一半是域注释与负载）。升级触发条件同焦点一（断言规则本身演化时先抽
  checklist helper）。
- **「宿主相关无稳定断言值」标注（generic/mime_test.go:21-27 .csv 段）**：正确取舍。
  强制 skip 会把腿从视野里拿掉；GOOS 门控断言则对「linux+shared-mime-info」宿主脆断。
  现方案=删断言+注释归因 stdlib 漂移+台账 generic/mime-stdlib-host-drift 跟踪，
  BIN-53 删回退后该腿自然获得稳定断言值——闭环路径在案。pypi CT 钉 text/plain 同模式
  （upload_test.go:81-83 钉现值+指向裁定），正确。

### 五、台账架构

- **臂级拆条粒度**：nuget/bare-put-201-x-checksum-sha256（UNKNOWN/authority=pending/
  gate「两程内升级」）与 pypi/upload-response-content-type-body（UNKNOWN/
  authority=conductor_ruling/T-568 票面裁定第 3 条）——两条新条目字段集完整
  （surface/classification/rationale/authority/review_gate/evidence，脚本复核），
  拆分出处（T-567/T-568 报告）与母条 resolved 块互指，粒度与既有条目一致。
- **review_gate 引用自洽**：各母条 gate 原文含「license 门解锁前单测+静态断言先行」
  条款，resolved 块据此登记「单测先行兑现、活体腿解锁后补拍（诚实活体欠账）」——
  deb/rpm/helm/nuget-bare/cargo/nuget-v3 六条逐一带欠账标注，cargo 另带 Custom Base URL
  附加门；pypi 族注明不受 license 门、live curl 已取证——**逐条核对无遗漏、无超裁**。
- **计数复核**：脚本统计 open=46（BUG 5/UNKNOWN 35/INTENTIONAL 5/UNSUPPORTED 1），
  与 commit 777049cf 声称「Z open 51→46（BUG 12→5）」一致；2026-09-29 当日 resolved
  共 20 块（含 R8 批 7 条）。yaml 可解析、无结构破损。

## 评审报告 R8 载荷（形态: reviewer-b）

结论: **APPROVE**

### 必须修改（blocking）

- 无。

### 建议改进（non-blocking）

- internal/adapter/maven/mime.go:19-47 — maven 表缺出厂表 v17 的 .md/.html/.htm/
  .yaml/.yml 5 键（generic:32 键 vs maven:27 键，共享 27 键零冲突）。本票按 proposal
  群清单逐字执行属清单粒度漏项；但 BIN-53 删 stdlib 回退后将成为 maven 域可预期新
  DIFF 面 → 建议 5 键随 BIN-53 补齐（1 个 map 字面块，与 generic 逐字同值）。
- docs/compatibility/proposals/mime-ownership-ruling.md §3-3a — 「删回退…无扩表」
  表述只对已覆盖键成立；两表仍缺 v17 约 20 键（css/java/py/mf/xz/bz2…），删回退后
  这些腿对 A 由「漂移值」变「恒 DIFF」→ BIN-53 票面需显式裁「补全表 or 登记残差」，
  防止差分矩阵出现未入账已知腿。
- internal/adapter/generic/handler.go:99-100 — 注释「(Artifactory honors the header
  too)」与本批已提交的 docs/reverse/mime-ownership.md §1 第 1 条（高置信活体：声明
  CT 全忽略）相反 → 随 BIN-53 勘误重写（proposal §1 原指向 maven put.go，实际残留在
  generic——勘误时按此定位）。
- Location 三件套第 9 份拷贝（internal/httpapi/repositories.go:612 requestBase）表明
  「adapter 域隔离」理由对 routing 层不成立 → 共享包收敛时（触发条件见焦点一）一并
  收编；本轮不动。

### 范围外交办（交 conductor，不扩本批）

- BIN-53 立票 rider 两条（maven 5 键补齐 + 3a 表述修正/残差显式裁），见 Next 1。
- license 门解锁后六条 resolved 条目的活体差分补拍批次（台账已逐条标注，无需新票，
  提醒排期）。

## 断点快照

- 已完成：全区间 diff 通读 + 五焦点逐项取证 + build/vet/test/gofmt/yaml 全绿复跑 + 本报告。
- 未完成：无。
- 断点位置：无悬挂。
