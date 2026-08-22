# T-216 评审报告（视角：correctness）— APPROVE

- 日期: 2026-08-23
- 复核人: code-reviewer（按其运行约束，报告原文由 conductor 落盘）
- 结论: **APPROVE**（blocking 0 / non-blocking 5）

## 分项判定

**1. lazy 重建正确性 — PASS**
- `sessionRegistry.resolve`（internal/adapter/docker/uploads.go:239）：快路径 lookup → `r.mu` 内 double-check `byID` → per-id `resuming` 等待 → `eng==nil` fail-closed → 成为 flyer 在 **r.mu 外**调 `ResumeSession` → 结果在 `r.mu` 下发布 + `close(done)`。锁内唯一嵌套是 `sess.Offset()`（互斥内存读，非 IO），O(size) 重哈希不串行化无关会话，符合 §5.3.1 伪代码。
- 内存可见性：`att.up/att.err` 锁内写、`close(done)` 先于 waiter `<-done`（happens-before 成立），-race 全绿佐证。
- 失败不缓存不毒化：err 路径不注册 `byID`、`delete(r.resuming)` 后新请求全新 attempt；`TestUploadResumeStoreFailureNotPoisoned` 钉死 500→恢复后重建成功。
- **变异实证**：备份后临时将 funnel 等待分支改 `ok && false` → `TestUploadResumeSingleFlight` 红（`engine ResumeSession calls = 8, want exactly 1`）——8 并发断言真实咬合单飞；还原后绿、uploads.go sha256 `f7cdc171…` 与原状字节级一致。

**2. offset 事实源迁移 — PASS**
- Content-Range 起点校验（uploads.go:498）与全部三个 416 分支、GET 状态腿（:422）均读 `authoritativeOffset()`（= `sess.Offset()`）。`up.received` 残余 4 处全部合法（nil-session 防御回退 :191 / 失败日志字段 :519 / 成功 Append 后 202 渲染 :538/:543）——received 确为降级缓存，无协议判定残留。
- 错位 416 形态：`Location` + 权威 `Range` + `Content-Length: 0` + 无 body，三分支一致；`alignContentRange` 只比对起点——对齐 docs/reverse/docker-registry.md §2.5#2/#3。

**3. 五动词统一经 resolve — PASS**
- PATCH/PUT（:393）、GET/HEAD（:417）、DELETE（:435）全走 `resolveUpload`，无 lookup 旁路。PUT finalize / DELETE cancel 重建后立即可执行（TestUploadResumePutAndDeleteAfterRestart：201 / 204 + 行与目录清 + 后续 404）。错误分层：ErrSessionNotFound（未知 id/过期 fail-closed/S3/nil-store/裸装配）→ 404 BLOB_UPLOAD_UNKNOWN；其余 → writeStoreFailure（500，ErrEngineClosed/busy → 503）。契约 4（id 即 capability）与契约 6（adapter 零 metadata 触点）保持。

**4. S3 契约 — PASS**：storage 侧 TestS3ResumeSessionNotSupported 按名复跑 PASS；适配器侧 TestUploadResumeS3BackendStays404 四动词恒 404。

**5. 并发与锁 — PASS**：锁序全序 `up.mu → r.mu → sess.mu` 单向无环；evictIdle 快照后释放锁再取 up.mu 无嵌套；-race 全包绿。

## 证据摘要（复核人亲跑）

| 命令 | 结果 |
|---|---|
| `go build ./... && go vet ./...` | ALL-BUILD-VET-OK |
| `go test -race -count=1 ./internal/adapter/docker/...` | ok 21.940s（还原后复跑 20.078s） |
| `golangci-lint run ./internal/adapter/docker/...` | 0 issues |
| storage 按名 4 测试 | 全 PASS |
| `m7-resume-probe.sh --stop kill`（**新构建二进制**） | **GREEN EXIT=0**（POST 202 → PATCH 202 → kill -9 → 重启 GET 204+Range → 续 PATCH 202 → PUT 201 → 逐位相等） |
| 单飞变异红绿闭环 | 红（calls=8）→ 还原绿 |

**探针重要插曲**：首跑 `--stop kill` 得 RED EXIT=1——根因非代码：`bin/binflow-server` 是 05:45 陈旧 pre-T-216 制品（二进制内无 `upload session re-materialized` 串），而脚本「binary 存在即复用」。用工作树新构建二进制复跑 → GREEN。agent 的 GREEN 声明成立，本次已用当前树重新证实。

## clean-room 抽查 — 无嫌疑

reverse-src 的 DockerBlobUploadHandler.java 是 JAX-RS「文件即会话」架构（无会话注册表/重建/单飞概念）；T-216 的 resolve/resumeAttempt/authoritativeOffset 在反编译源中无结构与命名对应；行为对齐点全部出自 docs/reverse/docker-registry.md §2.5 规格文本——规格驱动，非代码搬运。

## non-blocking（5 条）

1. `scripts/m7-resume-probe.sh:88-95`「binary 存在即复用」陷阱：陈旧制品造成假红（本次亲历）。建议校验制品新鲜度或默认强制重建——**防误导后续 QA 轮（T-222）**。
2. `uploads.go:261-279` flyer 路径 `delete(r.resuming)`/`close(att.done)` 未用 defer：ResumeSession 若 panic，该 id 永久阻塞后续请求。建议 defer 收尾加固。
3. 工作日志「9 顶级测试」实为 8 个顶级测试函数（12 个 run）——计数小误差。
4. `uploads.go:519` Append 失败日志记缓存 received 而非权威 offset——观测字段小瑕疵。
5. 归属确认：token_gate_test.go min→floor 为本票顺手清零；token_test.go SetRole 桩为 T-212 涟漪——与 agent 声明一致。
