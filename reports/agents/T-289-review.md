# T-289 评审报告（视角：correctness）

> 评审员内联交付，conductor 归档于 2026-08-26 09:12。原文照录。

**结论：REQUEST_CHANGES**（blocking 5 条，均已定位到行并给出改法；其中 1 条根因在既有引擎代码，但本票契约依赖它）

## 必须修改（blocking）

- **B1 并发：锁序倒置可致全平面死锁 + sweep 持注册表锁等会话锁**
  `internal/httpapi/uploads.go:532`（config 失败臂在持有 `ms.mu` 时调 `s.uploads.remove` → 取 registry.mu），与 `uploads.go:142-154`（evictIdle 持 registry.mu 逐个取 `s.mu`）构成 AB-BA。死锁一旦发生，sweep 永久持有 registry.mu，`lookup/add/remove` 全部阻塞 → 整个 MPU 面永久挂死。另外 evictIdle 在 registry.mu 内等会话锁，若某会话正处长 Append（`uploads.go:619-698` Append 全程持 mu），每 30 分钟 sweep tick 都会让全注册表停摆一个 Append 的时长。改法：config 臂改为解锁后再 `remove`（置 flag、显式 unlock、锁外 remove——complete `:740-741` 与 abort `:803-804` 已是正确姿势）；evictIdle 改为 registry.mu 内只快照指针、释放后在会话锁内复查 idle 再删。
- **B2 411 响应吞掉自身错误体，且该臂零测试覆盖**
  `uploads.go:655-657`：先 `w.Header().Set("Content-Length","0")` 再 `writeError`，Go server 按声明 CL:0 丢掉 JSON 信封——httptest 实证：客户端收到 411 + 空体（信封被吞），违反 §7.3 统一 errors[] 契约。且 `uploads_test.go` 全文无 411/LengthRequired 用例，日志 §7.4「单测守卫梯已含 411 分支」不属实。改法：删掉该行 CL 头（对照 abort 的 204 处 `:805-806`，那里无 body 才是正确用法）；补一条 chunked/TE 无 CL 的测试臂。
- **B3 partSizeMB 移位溢出绕过 5GiB 400 门**
  `uploads.go:316` `bytes := partSizeMB << 20`：partSizeMB ≥ 2^43 时 int64 溢出为负/零 → 被当「低于下限」clamp 到 5MiB → 201 + 错误回显（实测 `8796093022208` → 201/5242880，`1<<44` → 201/5242880），日志声称的「>5GiB 400 fail-fast」被击穿。改法：移位前先判 `partSizeMB > mpuMaxPartSizeMB → 400`。
- **B4 裸 status 列表无授权过滤（信息泄露）**
  `uploads.go:583-592`：路由门仅 `required:true`，任何已认证主体（零授权用户）可枚举全实例所有在途会话的 repoKey/path/createdBy/receivedBytes——含其无权访问的仓。同端点的单会话臂却走 `resolveMPUSession` 的 `w` 门（`:380-384`），自家面内不一致；M7 矩阵纪律下新面不应低于既有 RBAC 地板。改法：列表臂逐会话过同一 `uploadsWriteGate`（或至少 read 检查）过滤，或整臂挂 manage 能力；补测试。
- **B5 S3 侧 multipart 状态并未回收——本票 abort/sweep/错 sha 的清理主张被证伪（根因：既有引擎缺陷）**
  `internal/storage/s3.go:943-957`：`s3Session.Abort` 只置 done+forgetSession，从不调 AbortMultipartUpload；对照 disk 引擎 `session.go:296-306` 的 `finishLocked` 是物理删除，且 `s3.go:838-845`（Commit 去重臂）显式补调 AbortMultipartUpload——代码库自身证据表明「终结会话须回收 S3 MPU」。受影响：REST abort（`uploads.go:801`）、空闲 sweep（其存在理由 `uploads.go:61-66` 明言释放 S3 MPU——实际没释放）、错 sha256 complete（Commit failLocked 路径）全部把未完成 MPU + 已传分片留在 bucket（计费存储），仅靠下次重启 + TTL 扫回。日志 §5.6「无 sessions/ 残留对象（abort 清理干净）」用 `mc` 对象列表核验——S3 ListObjects 根本看不见 in-progress MPU，属空洞验证。改法（二选一，conductor 定）：在 `s3Session.Abort`（及 `failLocked` 路径）补 best-effort `AbortMultipartUpload`（模式照 `:841`），或修正本票端点表/日志主张并登记引擎债。AC1「abort 后零残留」当前不成立。

## 建议改进（non-blocking，9 条）

- AC2（kill -9 续传）未达：裁定 C 技术上站得住（§5.3.1 契约 5 钉死 + §11.31 债登记 + 探针如实钉 404），但 PRD FR-90-AC2 白纸黑字，**必须 conductor/PM 正式 descope 并 BOARD 留痕**，不能默认放行。
- complete 的 Commit 后 5xx「safe to retry」：同 id 重试 complete 是 404（会话已消费），"retry"实为整包重传（blob 去重兜底）；建议措辞点明或引入 landed-pending 态。
- 测试缺口：torn part（声明 CL > 实到字节，`uploads.go:678-689`）、evictIdle sweep（零覆盖）、并发 part PUT 顺序竞争均无测试。
- `mpuMaxSessionPath`（`uploads.go:71`）复述 adapter.MaxRelPathLen、`resolveMPUPartSize` 复刻 resolveS3PartSize 表——两处漂移风险，宜引用同源。
- `snapshot()` 注释称 "oldest first" 实按 id 排序（`uploads.go:190-201`）。
- config 对 failed 态会话的 409 文案称 "already carries bytes"，失真（`uploads.go:514-517`）。
- `validateMPUChecksums` 拒绝大写 hex，而引擎 `normalizeHex`（engine.go:659-673）大小写不敏感并归一——面层比引擎更严且不一致。
- sweep goroutine 无停机钩子（`uploads.go:120-130`）；`writeUploadsUnavailable` 注释名与函数名不一致。
- urlPart 对 failed/awaiting-complete 会话仍发 URL（PUT 会 409 兜底），语义略宽。

## 五项裁定复核

A（BinFlow URL 非 presigned）**站得住**：§15.4 明令零存储改动，presigned 需新凭据/SSRF 面；PRD「(presigned)」字面偏差已升级 PM（日志 §7.2）。B（generic local only）**站得住**，协议仓 layout 归 adapter，400 文案点明。C：**技术成立、程序未闭环**（见上）。D（零字节重定分片、REST id 不变）**站得住**，swap 实现诚实、失败臂不留死会话（除 B1 锁序）。E（dual-write 501）**站得住**，缺 seam 即语义。

## 回归风险与验证

- s3.go `beginSession` 共体重构**无行为漂移**（`e.partSize` 引擎打开时已 resolve；`BeginMultipartSession(0)` 经 resolveS3PartSize 取默认）。router/harness/cmd 改动全为增量，8 路由臂不遮蔽既有 case。
- 矩阵零白名单合理：新端点不在 T01–T06 固定行集。
- clean-room 抽查：端点名集取自 inv-4 L3 属规格事实，语义全 BinFlow 自有（Session 面/errors[] 信封），无逐行翻译痕迹。**通过**。
- 实跑：`go test ./internal/storage -run TestBeginMultipartSession` ✅；`go vet`/`go test ./internal/httpapi`、`./cmd/binflow-server` 当轮因 T-290 中间态文件无法编译（时序性，conductor 于 T-290 修复轮完成后复验 BUILD-OK）。

## 范围外发现（交 conductor）

1. ~~T-290 config.go 中间态编译阻塞~~（已过时：conductor 复验 BUILD-OK）。
2. 引擎级：Commit 的 failLocked 各失败路径与 Abort 同样漏回收 MPU——若 B5 修 `s3Session.Abort`，建议顺带覆盖 failLocked（或开引擎票）。
