# R10 双审报告 B（architecture）— 载荷四提交 d9bae6b4..fc6fc0f5

- 形态：reviewer-b（architecture：架构/兼容/测试质量）；评审对象 `3254c887` (BIN-60/T-578) / `b248be9d` (BIN-61/T-579) / `4fa01dda` (ADR-0052) / `fc6fc0f5` (L039/T-580, docs only)
- 复核自跑：build/vet/gofmt 0 告警；`go test -count=1` repo/nuget/httpapi/metadata/generic/maven 全 ok（221s httpapi 在案）
- 以下为评审全文（conductor 代落盘，内联原文）

---

所有验证已完成。以下是评审结果。

## 评审报告 R10 载荷（形态：reviewer-b / architecture）

**结论：批准** — 0 阻塞性问题，4 个非阻塞性问题，3 项升级事项移交 conductor 处理

范围：`git log d9bae6b4..fc6fc0f5` = 3254c887 (BIN-60/T-578 SPI 缝+翻正), b248be9d (BIN-61/T-579 nuget 渲染族), 4fa01dda (ADR-0052), fc6fc0f5 (L039/T-580 探针批，docs only)。复核跑通：build/vet/gofmt 0 告警；`go test -count=1` repo/nuget/httpapi/metadata/generic/maven 全部 ok（221s httpapi 在案）——两票报告的验证声明全部复现。

### 逐项结论（architecture 重点）

**1. §5.1 依赖方向 — 通过 (PASS)**。adapters 仅经 seam 调 `svc.SetClientChecksums`（generic handler.go:260 / maven put.go:475）；`Nodes().SetClientChecksums` 唯一调用点 = repo/service.go:1290，adapter 层零 NodeStore 写面 import。nuget/created.go 的 repo import 不是新依赖边（nuget handler.go/v3virtual.go/flat.go 既有）；payload 对 handler.go/put.go 零新增 import 行。范围外注记：§5.1 字面「adapter 禁 import storage/metadata」vs 全 adapter 群既有类型级 import 是存量张力（签名同源口径），非本 payload 引入。

**2. ADR-0052 符合度 — 通过 (PASS)，两处自报偏差均裁定接受 (ACCEPT)**。六点逐一对照：签名逐字（repo/api.go:869）；单语句 CASE WHEN + 0 行→ErrNodeNotFound + folder SQL 边排除（substores.go:289-307，无 read-modify-write）；服务缝零仓型判定、w 门在缝内（匿名→ErrUnauthorized）；「SET 先行于渲染」两 adapter 均遵守（generic SET→比对→201/409；maven refuse 判定→SET→渲染，判定次序=ADR 6②）；策略门留 adapter 现位；sha512 三处排除（表、SET 条件、overlay 条件）。偏差①（可选接口 `ClientChecksumWriter` vs ADR 字面「Service 增设」）：**接受 + 登记**——理由核实为真：docker/blob_test.go:59 `fakeService` 逐方法实现 repo.Service 无嵌入，扩大会接口必击穿域外文件；RemoteV2Plane 可选段先例在案（api.go:839/954/983）；编译钉 `var _ ClientChecksumWriter = (*service)(nil)` + 语义逐条一致。归 T-578 Next ③，conductor 应派 architect 裁定票。偏差②（internal/metadata 一枚方法）：ADR 决策 3① 明文强制（专用方法禁走 PutNodeWithUsage），非偏差，披露合规。

**3. 渲染单源 — 通过 (PASS)**。`repo.OriginalChecksums` 四消费面全数确认：httpapi storage.go:530（FileInfo overlay）、generic handler.go:203（GET 回显，空 triple 传入=404 姿态不并入 helper）、maven put.go:527 + walk.go:243-250（direct 面 overlay、walk 腿 false 保 t5/t6 计算值）、nuget created.go:122（conductor rewire 后）。上传上下文渲染（maven put.go:737-747、generic handler.go:408+）正确**不**走 helper——ADR 边界条款（declared-only 维持 uploadContext 形）被遵守。nuget rewire 符合裁定 4「两臂同一 digest 组装」：值选取经 helper（A raw 证 overlay 臂成立），成员过滤（sha256 恒在、sha1/md5 仅声明时）留本面。

**4. 兼容面边界 — 通过 (PASS)，零越界**。L039 七候选（C1 virtual/C2 按需计算/C3 大小写/C5 metadata 路由/C6 policy 枚举/C7 remote 404）本 payload 一律未碰：TypeLocal 门维持、`terminalChecksumSuffixes` 精确小写未动、`KindMetadata` 豁免 SET 与 refuse、无 policy 校验新增、remote 面未触。L039 B 基线 = origin/develop 6d10fd34 干净树（与 payload 分支隔离正确）。known-divergence.yaml 与 `storage/deploy-201-itemcreated-envelope-family` 池条目零触碰，台账翻面正确留给 conductor。

**5. mime 表第四份副本 — 收敛触发已达成，建议 R11 票**。nuget/mime.go 确为工厂表第四份副本（与 generic/maven/httpapi 三份 diff 逐字节相同，69 条目）。矛盾点：3254c887 刚在 generic/maven mime.go 写下「do not grow a fourth copy」（R9-B NB 收敛触发注记），7 秒后 b248be9d 即生出第四份。T-579 票面 area=nuget 单域，hoist 需动三个域外包——长第四份是 area 纪律下的正确局部选择，且字节忠实、.nupkg 值有测试钉；但「表族收敛」条件已从「下次改表」升级为「已破四」，**建议 conductor 立 R11 refactor 票**：表提升至共享包（adapter 公共层），四消费方切换，BIN-53/T-571 所有权文档同步。本轮 NB，不 blocking。

**6. 测试质量 — 通过 (PASS)**。nuget envelope **byte 级全等**（TS 正则归一，created_location_test.go:177-186）+ 无尾换行断言；409 message 逐字 + envelope 形 + **四条泄漏负测**（session/commit upload/storage:/Checksum error for，checksum_header_test.go:185-189）；repo 缝 gates 负测（匿名/非写者拒且不落列）、folder 拒、族阴性（.sha512/.asc/.sha1.bak）。命名全数按行为（TestBarePutCreatedEnvelope 等），票号在文件头注释。mime_test.go 删腿为意图驱动（.sha1 族脱离文件部署面），行为在 checksum_put_test.go 重新钉住，非断言弱化。

**7. 文档/台账一致性 — 通过 (PASS)，一处滞后**。ADR-0052 对账段与落地逐条相符；T-578 六项 Changes 抽查全对码。**T-579.md 滞后**：其 Changes 2)/Blockers Notes 仍写「adapter 局部实现待收编改接」，而同 commit b248be9d 的 created.go 已经 helper（仅 commit message 反映 rewire）——报告应补一行后记。干净无嫌疑：envelope 字节形态证据链=活体捕获（L037/L038/T-579 A raw），mime 表=已裁定数据表。

### 非阻塞性问题 (Non-blocking)

1. `internal/adapter/nuget/mime.go:40` — 工厂 mime 表第四份 lockstep 副本，与本批次 T-578 mime.go「不再增加第四份副本」注记自相矛盾 → R11 收敛票（见重点 5）。
2. `reports/agents/T-579.md:27,102-104` — 报告文本滞后于同 commit 内的 conductor rewire（created.go 已走 repo.OriginalChecksums）→ 补一行后记。
3. `internal/adapter/generic/handler.go:301-313`（serveClientChecksum）— LOCAL 面终缀 GET 遮蔽存量实文件节点（.sha1 实文件在场但 client 值未设→404）：A 模型一致且 T-578 Risk ④ 已自报，迁移说明归 conductor 用户文档面。
4. `internal/adapter/generic/handler.go:240`（io.ReadAll 无上限）— T-558 报告 Risk ⑤/Performance 已如实登记（maven 有 1024B 守卫、generic 无），A 面超限行为未探不猜——维持登记即可，随 L039 后继探针票收口。

### 移交 conductor 处理 (Handoff to conductor)

- **批准合入**（双审 B 侧 0 blocking；建议 A 形态并行实例按双审制度独立出报）。
- 台账翻面按 T-578/579 Next 执行（maven/checksum-put-409-write-through + generic 五条 + nuget 三条；证据=两票 Outputs 双轮活体）。
- 建 R11 票：mime 表族收敛（四副本→共享包单源）。
- 派 architect 裁定票：ClientChecksumWriter 可选段 vs 收编 Service 大接口（T-578 Next ③）。
- L039 七候选（C1-C7）+ P8 refinement 交 compatibility-engineer 落账。

**报告: 内联返回（本 subagent 无报告文件落盘权限外的产物；如需归档由 conductor 依本结论写 reports/agents/）**
