# M1 QA 总报告 T-19 — 存储完整性 / 性能 / 持久化 + README 复跑（PRD §7 场景 2/5/8/9/10）

- role: qa-engineer
- 日期: 2026-08-18
- 被测对象: `make build` 重建产物 `./bin/binflow-server`（17,110,160 B ≈ 16.32 MB，CGO_ENABLED=0，`go build ./...` + `go vet ./...` 通过后重建，与 HEAD `b0faf41` 源码一致）
- 剧本口径: docs/prd/milestone-1.md **v1.3.1** §7 场景 2/5/8/9/10 + §6.1 NFR-P1~P4；DoD 结论合并 T-18（reports/agents/T-18-qa.md，round 1 + 回归轮最终 ALL GREEN）
- 环境: darwin/amd64（16 核 / 32GB），Go 1.26.5，Docker 29.7.2 + Compose v5.3.1；宿主 8080 被既有容器（arca）占用 → 全程随机/高位端口（31828 裸实例、18080 compose、18082 README 复跑），README 默认端口路径的偏离已在场景 9 注明
- 结论预告: **ALL GREEN**（0 个缺陷；3 条观察项 O1~O3，均不构成缺陷）

## 场景总览

| §7 场景 | 范围 | 结果 |
|---|---|---|
| 2 冷启动 | NFR-P1 双路径（裸二进制 3 轮 + compose 含构建/缓存两轮） | ✅ PASS |
| 5 存储完整性 | C12 去重 / 慢上传中断 / kill -9 重启（双轮）/ 覆盖写幂等 / 1GB 流式 RSS | ✅ PASS |
| 8 压力 | NFR-P2 100 并发 GET 10MB（两轮）；NFR-P4 空载 60s 采样 | ✅ PASS |
| 9 文档 | README 快速开始干净 worktree 复跑 + 安全节抽查 + smoke.sh | ✅ PASS（1 条环境注记） |
| 10 持久化 | C29 compose restart + down&up 两轮；gc dry-run/--apply（裸机 + 容器内） | ✅ PASS |
| NFR-S1/S2/S3 | 本票实例口令/token 不入库不入日志复查 | ✅ PASS |

---

## 场景 2：冷启动（NFR-P1，门 < 2s）

| # | 路径 | 实测 | 结果 |
|---|---|---|---|
| 2.1 | 裸二进制（空库，0.01s 间隔轮询计时） | round1 **0.065s** / round2 **0.092s** / round3 **0.096s**（exec→ping OK） | ✅（余量 ~20-30 倍） |
| 2.2 | compose `up -d --build`（首次，含镜像构建） | **4.5s**（build + 起 + ping OK）；镜像缓存后 `up -d` → ping OK **0.87s** | ✅（PRD FR-6-AC1 的 30s 门为 compose 路径口径，NFR-P1 的 2s 门以裸二进制为准，两口径均远超） |

计时方法：`python3 time.time()` 包裹「进程启动 → 0.01s（裸）/0.1s（compose）间隔轮询 `GET /binflow/api/system/ping` 首次返回 `OK`」。

## 场景 5：存储完整性（FR-2）

裸实例 `serve -c cfg.yaml`（listen 127.0.0.1:31828，data_dir 隔离目录，`BINFLOW_ADMIN_PASSWORD` 显式设置——启动日志 0 条缺省口令 WARN）。

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 5.1 | C12/FR-2-AC1 去重 | 同内容 `a/x.bin`、`b/y.bin` 两次 201；stats blob 计数不变 | `201`/`201`；stats 1→上传 b/y 前后 **1→1 不变**；`logical_bytes` 10485760→20971520（翻倍）、`physical_bytes` 10485760 不变；两路径下载 sha256 三行一致（uniq -c=3）；落盘 `data/blobs/a4/<sha256>` 恰 1 个文件 | ✅ |
| 5.2 | FR-2-AC3 慢上传中断 | 3s kill 后 GET 404、blob 计数不变 | `--limit-rate 64k` 上传 10MB，3s 后 kill -9 curl（exit 137）；GET `acme/big.bin` **404**；stats 仍 `blobs:1`；`data/sessions/` **零残留**（断连后即时清理）、`data/blobs` 计数 1 | ✅ |
| 5.3 | FR-2-AC4 kill -9 重启（第 1 轮） | 历史 200 / 中断 404 / health 200 | 1GB 限速上传 4s 后 `kill -9` 服务进程；重启后 `a/x.bin` **200**（sha256 与源一致）、`b/y.bin` **200**、中断路径 `acme/slow1g.bin` **404**、health **200** `{status:ok,storage:ok,metadata:ok}`、stats 不变；崩溃 session 残渣仅存于 `sessions/<uuid>/`（不入 blobs、不计数）——符合 T-9 契约「kill -9 窗口只允许 session 残渣」 | ✅ |
| 5.4 | FR-2-AC4 kill -9 重启（第 2 轮，5 个已提交制品 + 慢传中断） | 全部历史 200 / 中断 404 / health 200 | 预置 p1~p5（全 201）→ 1GB 限速 4s → kill -9 → 重启：p1~p5 全 **200**、`slowkill` **404**、health **200**、stats `{blobs:3,logical:62920360,physical:1084233384}`（被引用 blob 全在） | ✅ |
| 5.5 | FR-2-AC5 覆盖写幂等 | 同内容 2xx 计数不变；异内容 2xx 新内容可见 | 同路径同内容重传 **201**、stats `blobs:1` 不变；异内容 PUT **201** → `blobs:2`、下载 sha256 == 新内容；旧 blob（10MB）此时仍被 `b/y.bin` 引用 → GET 200 幸存；随后 DELETE `b/y.bin`（204）释放最后引用 → stats `blobs:2, logical:5800`（node 立即不可见），物理字节待 grace 后回收（见 10.3） | ✅ |
| 5.6 | FR-2-AC6/NFR-P3 1GB 流式 | PUT 201、GET sha256 一致、RSS 增量 < 256MB | PUT **201**（5.20s，~206MB/s）+ GET **200**（1.36s）；`ps -o rss` 峰值采样（200 样本 @0.05s）**146428 KB，较上传前 146372 KB 增量 56 KB（0.05MB）**；PUT/GET/本地三方 sha256 一致 `f42168b3…89b7` | ✅（余量三个数量级） |

附：进程 RSS 全程稳定在 ~143MB（空载 146248 KB → 并发后 148444 KB，见 8.2），1GB 上传零尖峰，流式实现确证。

## 场景 8：性能（NFR-P2/P4）

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 8.1 | NFR-P2 100 并发 GET 10MB | `seq 100 \| xargs -P100 curl -sf` 退出码全 0；日志零 5xx | 两轮均 xargs **exit=0**（100 个 curl 全 -sf 成功）；并发期间峰值 RSS 148444 KB；裸实例三份日志 5xx 合计 **1 条**且为 5.2 慢中断用例的预期产物（PUT `acme/big.bin` 500，连接断开时写路径以 500 收卷，node 未落、blob 未计），压力窗口本身 **0 条 5xx、0 ERROR、0 panic** | ✅ |
| 8.2 | NFR-P4 空载内存（记录不设门） | 记录 | 启动完成 + 无请求 60s（13 个样本 @5s）：**RSS 恒 146248 KB（≈142.8 MB）**，零漂移。PRD 参考线 100MB（M5 GA 才设门）：M1 实测高出参考线约 43MB——主要构成为 argon2id 参数校验路径（m=64MB）触达后的堆驻留与 SQLite 池（MaxOpenConns=16），M1 记录在案，不判失败 | ✅（记录） |

## 场景 9：README 快速开始复跑（FR-6-AC4）

干净环境：`git worktree add /tmp/readme-t19 HEAD`（无 bin/、无 data/，README 与 dev-center HEAD 逐字节一致）。五步逐行执行：

| 步 | 命令 | 结果 |
|---|---|---|
| 1 build | `make build` | exit 0，`bin size: 16.32 MB` ✅ |
| 2 start | `./bin/binflow-server serve` + `curl -s $BASE/binflow/api/system/ping` | **环境注记**：本机 8080 被第三方容器（arca）占用，README 默认命令在无冲突机器上原样可用；此处按 README「Configuration」节明示的 env 覆写机制 `BINFLOW_SERVER__LISTEN=127.0.0.1:18082` 启动 → ping `OK` exit 0 ✅（服务端对端口冲突行为正确：启动日志明确报 `bind: address already in use` 并退出，无静默错绑） |
| 3 create repo | `curl -su admin:$ADMIN_PW -X PUT .../repositories/generic-local ...` | `200` `Successfully created repository 'generic-local'` exit 0 ✅ |
| 4 upload | `echo "hello binflow" > hello.txt` + `-T` + `jq .` | `checksums.sha256` == 本地 sha256（`268ccb75…319`），size "14" ✅ |
| 5 download+verify | 匿名 `curl -s -o` + `diff` + `sha256sum` | `content identical`，sha256 一致，diff exit 0 ✅ |

安全节抽查（README「Security notes」逐条）：

- 匿名读默认开：匿名内容 GET **200** / 匿名 PUT **401** / 匿名管理 API **401** ✅
- 改密轮换：`PUT /api/security/password` → `200` `Password has been successfully changed`；新口令 **200**、旧口令随即 **401** ✅
- 关闭匿名读（README 明示的 env）：`BINFLOW_SECURITY_ANONYMOUS_ACCESS=false` 重启 → 匿名 GET **401** + `Www-Authenticate: Basic realm="BinFlow Realm"`，认证 GET **200** ✅
- 优雅退出（`Ctrl-C` 等效 SIGINT）：日志 `"msg":"binflow stopped"`，进程干净退出 ✅
- `scripts/smoke.sh`（README 提及的自测链，自选随机端口 + trap 清理）：**exit 0**，`smoke: PASS (C01/C03/C07/C08 + shutdown)`，冷启动 0.129s ✅

**结论：README 五步 + 安全节全部可复跑，退出码全 0**（唯一偏离为宿主端口冲突，属环境而非文档缺陷；README 已内置 `BINFLOW_BIND_PORT`/env 覆写两套应对说明）。

## 场景 10：持久化（FR-6-AC3 / C29）与 GC

compose 实例：干净源码拷贝（tar 排除 .git/data/bin）为 build context，`deploy/dev/.env` 设测试口令与 `BINFLOW_BIND_PORT=18080`。

| # | 检查 | 期望 | 实际 | 结果 |
|---|---|---|---|---|
| 10.1 | C29 restart 轮 | 同路径 GET 200 且 sha256 不变 | 建仓 200 + 上传 201 → `docker compose restart` → ping OK → GET **200**，sha256 与上传源**逐位一致** | ✅ |
| 10.2 | C29 down+up 轮（不 `-v`） | 同上；仓库配置也在 | `down`（卷 `binflow_binflow-data` 存活）→ `up -d` → GET **200** sha256 一致；`GET /api/repositories` 仍列出 `generic-local` | ✅ |
| 10.3 | gc dry-run → --apply（裸机） | 默认 dry-run；grace 内不回收；过期后物理回收；被引用幸存 | ①释放引用后 24h 默认 grace：dry-run `candidates=0`（保护期内）✅；②`--grace-days 1` 覆盖仍 0 ✅；③config `gc_grace_hours:1` + blob mtime 回拨 2 天模拟过期 → dry-run 列出该 sha256（**默认仍是 dry-run，未删**）✅；④`gc --apply` → `data/blobs/a4/<sha256>` **物理消失**，stats 从 `blobs:2/physical:10491560` 收敛为 `blobs:1/physical:5800`，被引用的 `a/x.bin` **仍 200** ✅；⑤二次 dry-run `candidates=0`（幂等收敛）✅ | ✅ |
| 10.4 | gc（容器内，挂载卷数据） | 同上语义 | `docker exec binflow-dev binflow-server gc`（dry-run）→ 制造**异内容**孤儿（上传→删除→mtime 回拨）→ dry-run `candidates=1` 列出 → `--apply` → `find /var/lib/binflow/blobs -type f` 计数回落，被引用的 `c29/persist.bin` **仍 200** ✅。插曲：第一次误用与 persist.bin 同内容的文件做候选，dry-run 正确报 `candidates=0`——同 blob 被引用不可回收，反向佐证引用保护 ✅ | ✅ |
| 10.5 | kill -9 崩溃一致性（容器路径，场景 2/5 补强） | 中断 404 / 历史 200 / 自动拉起 | 1GB 限速上传中 `docker restart`（SIGKILL 等效进程死亡）→ 容器回来后 ping OK、`persist.bin` **200**、`slow2.bin` **404**、health ok；容器日志 5xx 仅 1 条 = 该中断 PUT（500，预期） | ✅ |

## NFR-S1/S2/S3（本票实例复查，T-18 已全量验证）

- SQLite（binflow.db/-wal/-shm，裸 + 容器卷）：本票全部测试口令（`T19-QA-pw-1`、`T19-Docker-pw`、`a-real-secret`、`smoke-admin-pw`）逐文件 grep **0 命中** ✅
- 服务日志（main/main2/main3/main4 + 容器日志 + README 复跑日志）：全部口令 **0 命中**，无 Authorization 头值 ✅

## 补强用例（DoD 清点发现的 P1/P2 缺口，本票顺手补测）

| AC | 检查 | 实际 | 结果 |
|---|---|---|---|
| FR-3-AC7（目录递归删） | mkdir 201 → 目录下 2 文件 → 目录 DELETE | `204`；`sub/f1.bin`、`f2.txt` 均 **404** | ✅ |
| FR-4-AC13（Content-Type） | GET 头 | `Content-Type: application/octet-stream`（默认映射，P2 扩展名映射未做，见 DoD P2 清点） | ✅（M1 口径） |
| FR-3-AC10（Postgres fail-fast） | `metadata.driver: postgres` 启动 | exit **1** + ERROR 日志 `Postgres support is not enabled` | ✅ |

---

## 观察项（不构成缺陷，供 conductor/PM 裁决）

- **O1 [行为记录]** 慢上传被客户端中断时，服务端访问日志以 **500** 收卷该 PUT（裸机与容器路径各观测 1 条，均在本票刻意构造的中断用例中）。客户端可见行为完全正确（路径 404、blob 不计、session 即清），无任何可见脏数据；仅「客户端断开」映射为 500 而非 499/400 类语义。PRD 未对该场景的服务端状态码作规定，不构成 AC 违反；建议 M2 在日志规范里定界（nginx 风格 499 或保持 500 但加 disconnect 标注），便于未来区分「真 5xx 故障」与「客户端主动断开」。定位：internal/adapter/generic 上传 handler 的错误收卷分支。
- **O2 [环境注记，非缺陷]** README 默认端口 8080 在本机被第三方容器占用；README 已提供 `BINFLOW_BIND_PORT`（compose）与 `BINFLOW_SERVER__LISTEN`（裸二进制）两套明示应对，且服务端冲突时报错退出行为正确。全新 clone + 空 Docker 环境的完整默认端口复跑仍建议在 CI 或下一台干净机器上留档一次（本票 worktree 复跑已覆盖全部命令语义，仅端口换用 README 明示机制）。
- **O3 [运维便利性，非缺陷]** `gc` 子命令无 `-c` 旗标（serve 才有），gc 的配置来源固定为「cwd 的 ./binflow.yaml 或 $BINFLOW_HOME/binflow.yaml 或 env 覆写」。T-16 已在票内记录该遗留；容器内因 `BINFLOW_DATA_DIR` env 生效不受影响。建议 M2 顺手补 `-c` 的一致性体验。另 `--grace-days` 语义为「覆盖为 N×24h」，无法表达「小于一天的窗口」（本票用 config `gc_grace_hours` + mtime 回拨完成过期模拟）——与 T-16 记录一致，不新增缺陷。

---

# M1 DoD 判定（PRD §8 五条）

## 第 1 条：§4 全部 P0/P1 AC 通过验证并附证据（P2 延后已在 BOARD 记录）

**结论：满足。**

逐 FR 清点（T = T-18 报告，§ = 本票）：

| FR | P0/P1 AC | 证据来源 | 判定 |
|---|---|---|---|
| FR-1 脚手架 | AC1/2/3/5/7（P0）+ AC4/AC6（P1/P2） | T-18 场景 1（build/test/lint/gofmt/tidy/零 CGO/CI 文件全绿）；本票开工前 `go build ./... && go vet ./...` 通过并重建二进制 | 全过 |
| FR-2 存储引擎 | AC1/2/3/4（P0）+ AC5/6（P1） | 本票场景 5（5.1~5.6 逐条）；AC2 的 checksum 对账在 T-18 场景 4.1/4.4 | 全过 |
| FR-3 元数据 | AC1/2/3/4/6/9（P0）+ AC5/7（P1） | T-18 场景 3（AC1~AC5）；AC6 场景 4.4；AC9 本票 10.1/10.2 + 裸机 kill -9 双轮（5.3/5.4）；AC7 本票补强 | 全过 |
| FR-4 Generic | AC1~AC5/AC8/AC9/AC10/AC12（P0）+ AC7/AC11（P1） | T-18 场景 4（4.1~4.13 含校准项）+ 场景 7（AC9/10/11/12）；AC13 本票补强（M1 口径 octet-stream） | 全过 |
| FR-5 认证 ACL | AC1/2/4/5/6/7/8/11/12（P0）+ AC3/9/10/13（P1） | T-18 场景 6（6.1~6.31）+ 回归轮 R1~R3（D2/D3 修复后 23/23）；本票 README 复跑安全节复核 AC12/AC13 面 | 全过 |
| FR-6 开发环境 | AC1/2/3/5（P0）+ AC4（P1） | T-17 票内 Docker 真机 + 本票场景 2/9/10 独立复验（AC1 4.5s/AC2 health ok/AC3 两轮 sha256 不变/AC5 裸二进制+优雅退出/AC4 worktree 复跑全 0） | 全过 |

**P2 项清点与处置（全部合规）**：

| P2 AC | 状态 | 依据 |
|---|---|---|
| FR-1-AC6（二进制 >40MB 警告） | 已实现（非门） | Makefile check-size 打印 16.32MB，无警告 |
| FR-3-AC8（?list/deep/depth） | **已做** | T-15 实现 + T-18 4.15（匿名 403/根 400/子目录 200 相对路径） |
| FR-3-AC10（Postgres 报错退出） | **已做** | 本票补 3（exit 1 + 明确日志） |
| FR-4-AC13（Content-Type 扩展名映射） | **未做（合规延后）** | M1 实现默认 octet-stream（本票补 2 验证）；映射属 P2，BOARD 无单独票，建议 M2 顺手 |
| FR-4-AC14/AC15（Range/条件请求） | **已做（T-20）** | BOARD done 2026-08-18 + T-18 4.14 抽测（206/416 正确） |
| NFR-P4（空载内存 100MB 参考线） | 记录不设门 | 本票 8.2：实测 142.8MB，M5 GA 才设门 |

（FR-1-AC4 CI 首跑绿归主会话推送后确认，T-7 起一直沿用该口径，非本票范围。）

## 第 2 条：§7 剧本全绿

**结论：满足。** 十个场景全部执行且全绿：

| §7 场景 | 执行票 | 判定 |
|---|---|---|
| 1 工程基线 | T-18 | 绿 |
| 2 冷启动 | T-19 | 绿 |
| 3 仓库生命周期 | T-18 | 绿 |
| 4 制品 roundtrip | T-18 | 绿 |
| 5 存储完整性 | T-19 | 绿 |
| 6 认证与 ACL | T-18（含 T-28 回归轮） | 绿 |
| 7 边界拒绝 | T-18 | 绿 |
| 8 压力 | T-19 | 绿 |
| 9 文档 README 复跑 | T-19 | 绿 |
| 10 持久化 | T-19 | 绿 |

## 第 3/4/5 条（非本票判定范围，状态记录）

- 第 3 条（5 份逆向规格 + 六项校准回写）：已完成（T-3/T-21/T-23/T-24，PRD v1.3 无遗留）。
- 第 4 条（README 可复跑）：本票场景 9 独立验证 ✅。
- 第 5 条（`m1-done` tag）：归主会话，建议在本报告基础上打 tag。

## M1 里程碑总结论

**PASS（ALL GREEN）。** 两张 QA 票合并覆盖 PRD §7 全部十个场景、§4 全部 P0/P1 AC、NFR-S1~S3 抽查与 NFR-P1~P4 实测：T-18 功能面全绿（经 T-28 修复 D2/D3 后回归 ALL GREEN）；本票存储完整性（去重/原子可见/崩溃一致/覆盖幂等/1GB 流式零内存增长）、性能（冷启动 0.065s、100 并发零错误）、持久化（compose 双轮 + gc 物理回收）、README 干净复跑全绿。**零未决缺陷**，观察项 O1~O3 均不构成 AC 违反。

---

## 环境与清理（本票全程）

- 裸实例：4 个进程（主测 3 轮 + README 复跑）全部优雅停止或按剧本 kill；临时数据目录 `/tmp/binflow-t19`（含 1GB/10MB 测试文件、compose 源码拷贝）已删除。
- compose：`docker compose down -v`（binflow-dev 容器 + binflow_binflow-data 卷已删并复核 0 残留）。
- worktree：`git worktree remove --force /tmp/readme-t19` 已删，`git worktree list` 仅剩主树。
- dev-center 主仓：`git status` 干净（bin/binflow-server 为构建产物，.gitignore 内）；自带 `data/` 目录前后均为空。
- 端口：31828/18080/18082/18083 无 binflow 监听残留（18080 上的 Python 监听为本机既有第三方进程，非本票资源）。
- 测试口令全部一次性，未写入任何提交文件。
