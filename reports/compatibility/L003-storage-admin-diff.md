# L003-1 storage-admin 对拍报告（/api/system/storage 端点族 E1-E10）

- 差分腿：BinFlow 工作树实例 `http://127.0.0.1:8084`（develop 工作树 @HEAD 952e556d + 本票未提交改动，二进制 `go build ./cmd/binflow-server`，数据目录 /tmp/l0031-data）
- 参照腿：Artifactory-pro 7.161.20 `http://localhost:8082`（admin basic，2026-09-11 复测）
- 行为规格：`docs/reverse/storage/prune-gc-admin.md`（E1-E10 + §2/§3）+ `docs/reverse/storage/binary-provider-chain.md` G 组
- 方法：BinFlow 侧全端点真实 curl（含真实删除验证）；参照侧仅只读等价面（prune/status、size、info、匿名 401）——**写腿不在参照实例上触发**（保护参照状态；写腿形态以规格 §1 表 + 规格已录活体证据为对照基线）
- 验证实录（BinFlow 侧逐案关键输出）见文末附录

## 1. 逐端点对拍矩阵

| # | 端点 | Artifactory 形态（规格/活体） | BinFlow 形态（实测 :8084） | 判定 |
|---|---|---|---|---|
| E1 | POST gc | 200 同步阻塞 + `text/plain;charset=utf-8` chunked 点流（活体空载体恰 1 个 `.`）；并发 409+status 消息体（反编译单源 V-7） | 200 同步 + `text/plain;charset=utf-8`，**256 dots + 3 newlines（259B，每目录一点、80 点一行）**；并发 409（errors JSON 信封 + 持锁者诊断）——单测 `TestStorageGCStreamFormAndConcurrentRefusal` 钉死 | **SAME（形态）**；点数粒度=BinFlow 分片目录数（规格钉的是「调试级进度逐条出点」，非点数）；409 信封形态分歧（V-7 无活体，BinFlow 用统一 errors JSON）→ 待契约化 |
| E2 | POST prune/start | 202 + `{"info":"Pruning Unreferenced Data task has been submitted"}`（CT application/json）；带 startFromDirectory → `resumes from directory <dir>`；并发 412 `cannot be started`；AoL 403 | 202 + 同文案 + CT ✓；resume 文案 ✓（`resumes from directory 80` 实测）；并发 412 ✓（单测钉）；非 admin 403 ✓（单测钉） | **SAME** |
| E3 | GET prune/status | 200 + 26 字段/4 对象 schema（活体三态全捕获）；从未跑 → 412 `{"info":"No Prune task found"}` | 200 + 同 schema（逐键对照：顶层 6 键/timing 6/report 3/lastHandledDirectory 5+timing 6；字段名、顺序、类型、`progress:"N of 256"` 字符串、`yyyy-MM-dd'T'HH:mm:ss` 无时区、`HH:mm:ss.SSS` duration 全对齐）；从未跑 412 ✓ | **SAME**（JSON 缩进风格差异：Go Encoder vs Jackson 空格冒号——归一化后一致） |
| E4 | POST prune/stop | 202 + `{"info":"Prune task stop request submitted"}`；空闲有历史报告也 202；无历史报告 412 `No running Prune task found` | 三臂全对齐（空闲 202 实测 ✓；两 412 实测+单测 ✓） | **SAME** |
| E5 | POST compress | Derby 200 流式；PostgreSQL 流内错误行（V-1 未获实证）；码钉 200 | 200 + `text/plain;charset=utf-8` 流（`compressing the internal database` + before→after 行；sqlite VACUUM=BinFlow 的 compress 载体）；无 VACUUM 面 → 流内错误行、码恒 200 | **FORM-SAME**（后端差异：Derby/sqlite——BinFlow 的 internal DB 是 sqlite，compress=VACUUM 重建，与 maintenance/compress 载体同核） |
| E6 | POST optimize | 202 **空体无 CT**（活体 0.09s）；失败 409/412（反编译） | 202 空体无 CT（实测 `[HTTP 202 CT=[] bytes=0]`）；单 provider filestore=诚实 no-op | **SAME**（no-op 语义差异登记：BinFlow 无 sharding，触发即无操作） |
| E7 | POST backup?key= | key 缺失 → 500 `{"errors":[{status:500,message:"No backup identified with key 'X'"}]}`（活体）；disabled → 500（反编译）；成功 200 流式立即调度 | 缺失 key → 500 errors 信封逐字对齐（实测）；disabled → 500 ✓；成功臂 = Deps.BackupRunner seam 触发 + 200 text/plain——**cmd 接线未落**（本票禁改 cmd）：装配实例上该臂 503 honest | **PARTIAL**（错误臂 SAME；成功臂待 3 行 cmd 接线——转 conductor） |
| E8 | GET size | 200 text/plain 裸数字（活体 35341882） | 200 text/plain 裸数字（实测 0 → 24 随真实删除变化） | **SAME** |
| E9 | GET info | 200 **text/plain** chunked + `{data:{baseDataDir,binariesDir,usageSpace…},subBinaryTreeElements:[]}` 全字符串字段（2026-09-10/11 两轮活体实测） | 200 text/plain + 同构（字段集/顺序/字符串化/type="file-system"/百分比；usageSpace+freeSpace==totalSpace 的 statfs 口径对齐——参照实例 usageSpace=分区占用而非 filestore 占用，BinFlow 同口径） | **SAME**（映射值差异：tempDir="uploads"（BinFlow 实况）vs "_pre"；fileStoreDir="blobs" vs "filestore"——诚实值非翻译） |
| E10 | POST exportds | 恒抛 → 500（"Export data is no longer supported"，@Deprecated） | 恒 500 同文案（errors 信封；重复调用恒定） | **SAME（形态）**（信封形状无活体——反编译未捕获异常路径，规格标中置信；BinFlow 用统一信封） |

## 2. 语义对拍（§3 流程）

| 语义 | Artifactory | BinFlow | 判定 |
|---|---|---|---|
| 202 异步 job 化 | Quartz 单例手动任务，后台遍历 00..ff | pruneManager 单飞 + goroutine 遍历 00..ff（256 目录枚举含不存在目录，实测 running 态 `progress:"233 of 256"` 递增） | SAME |
| 报告持久化 | configs 存储 key=STORAGE_PRUNE_REPORT，重启仍可读 | `<data>/prune_report.json` 原子写（temp+rename；终态带 fsync），**重启实测保持**（finished/256/dryRun 三值一致） | SAME（载体差异：configs 表 vs 数据目录 JSON——BinFlow 无 configs KV 面） |
| 停止语义 | stop 置标记，任务 ~1 目录内消费，落 stopped 终态 | 引擎目录边界消费标记；实测/单测 stopped 终态 + lastHandledDirectory.status="stopped" | SAME |
| dry-run 极性（D6） | dryRun 缺省 false=真删 | 缺省真删（实测：无 body start → cleaned=1/24B，-aged 孤儿删除、grace 内新 blob 保留）+ dryRun:true 回显/估算（cleaned=0） | SAME（新增面按规格极性；旧 `/api/v1/system/gc` 的 apply 极性不动——两族并存） |
| startFromDirectory | resumes 文案 + 分母恒 256（V-5 计数口径未定） | 文案 ✓ + 分母恒 256 + 跳过目录零计数 | SAME（V-5 口径 BinFlow 取「位置计数」，与活体观察一致） |
| GC 引擎复用 | FilestorePruner 扫描 + 策略回收（20 轮 minor 夹 1 FULL，D8） | storage.Prune 复用 GCSweep 同门（mark/hold/grace/Live 四门逐 blob，ADR-0031）；无 minor/FULL 之分（D8 维持 known-divergence） | 部分对齐（D8 已裁定可 known-divergence） |

## 3. BinFlow 侧已知映射决策（低置信度条目不擅定）

1. **prune grace**：取 `storage.gc_grace_hours`（默认 24h）。Artifactory 回收资格门槛 UNKNOWN（§3.4/G5/G7 未收口）——不猜测。
2. **running 报告重启恢复**：装载时发现 running 态报告 → 改写 stopped（拥有者进程已亡）。规格未覆盖（活体不可复现清零态）；BinFlow 侧恢复纪律，登记待验证。
3. **PruneRequestModel 第 3/4 参数**（续跑位置/binaryOlderThanDays，V-3 键名未实证）：不实现。
4. **错误态报告（V-4）**：status="error" 已实现（引擎错误→error 态+report 计数保留），完整字段形态待故障注入取证。
5. **gc 点流粒度**：一分片目录一点（256 点）。规格钉流式形态非点数；Artifactory 空载体 1 点的观测与 BinFlow 满枚举点数不同——归一化形态一致。

## 4. 残留

- E7 成功臂的 cmd 接线（3 行，dev-go-core 域）→ conductor 转派。
- V-1/V-2/V-5/V-7 维持规格挂账（本票未推进）。
- 差异清单 D1-D10 清偿：D1 ✓（10 端点族落地）、D2 ✓（流式）、D3 ✓、D4 ✓（26 字段）、D5 ✓（412/409）、D6 ✓（新面 Artifactory 极性）、D7 不动（maintenance 面维持映射）、D8 维持 known-divergence、D9 部分（info 信封族对齐；gc 409/backup errors 对齐；exportds 信封待活体）、D10 ✓（optimize/compress/backup/size/info/exportds 全落地，backup 成功臂待接线）。

## 附录：BinFlow 侧验证实录（关键输出摘录）

```
GET  prune/status（从未跑）        → 412 {"info":"No Prune task found"}
POST prune/stop（从未跑）          → 412 {"info":"No running Prune task found"}
POST prune/start {"dryRun":true}   → 202 {"info":"Pruning Unreferenced Data task has been submitted"}
GET  prune/status（运行中采样）    → 200 …"status":"running","progress":"233 of 256",lastHandledDirectory.name="e8"
GET  prune/status（终态）          → 200 …"finished" | "256 of 256" | lastDir "ff" timing.duration="00:00:00.000"
POST prune/start {startFromDirectory:"80"} → 202 {"info":"Prune Unreferenced Data task resumes from directory 80"}
POST prune/stop（空闲）            → 202 {"info":"Prune task stop request submitted"}
POST gc                            → 200 CT=text/plain;charset=utf-8，256 dots/3 newlines/259B
POST optimize                      → 202 CT=[] bytes=0
POST compress                      → 200 …"internal database: 483328 -> 483328 bytes (reclaimed 0)"
GET  size（播种后/删除后）         → 200 text/plain "0" → "24"
GET  info                          → 200 text/plain {"data":{…"type":"file-system"},"subBinaryTreeElements":[]}
POST backup?key=nope               → 500 {"errors":[{"status":500,"message":"No backup identified with key 'nope'"}]}
POST exportds?to=/tmp/x            → 500 {"errors":[{"status":500,"message":"Export data is no longer supported"}]}
POST prune/start（无 body=真删）   → finished | dryRun False | processed 2 | cleaned 1 | bytes 24（aged 孤儿删、grace 内 blob 留）
重启后 GET prune/status            → finished | 256 of 256 | dryRun True（持久化保持）
匿名 GET prune/status              → 401
```

参照腿（:8082，只读）：prune/status 200 application/json；size 200 text/plain；info 200 text/plain；匿名 401。（写腿不触发；其形态基线=规格 §1 表已录活体证据。）
