---
title: 治理：审计、GC 与配额
sidebar_position: 42
---

# 治理：审计、GC 与配额

> 适用版本：M4（治理面；PRD milestone-4 v1.2 FR-29/30/31/24、ADR-0015 勘误后）。
> 本文全部命令在本机 scratch 实例（commit `7593d8e`）上复跑：审计过滤/词表、GC dry-run→apply 与互斥 409、pattern 409 双态、配额 413 与幂等重传豁免均按预期（蓝本 T-103 W22~W27/W12a，报告见 `reports/agents/T-103-qa.md` §2.2/§2.5~§2.7）。

治理四件事：**审计**（谁在何时动了什么）、**GC**（回收无引用 blob）、**配额**（仓库容量上限）、**路径模式**（仓库接纳哪些路径）。前三个都有控制台页面；本文以 REST/CLI 为主面（脚本可完全等效），页面走查见[控制台指南](../console.md)。

## 审计

### 查询

`GET /api/v1/audit`（admin only；未认证 401、非 admin 403）：

```bash
curl -su admin:$ADMIN_PW "$BASE/binflow/api/v1/audit?action=gc.run&limit=2"
# {"events":[{id,time,actor,action,repo,path,detail},...],"nextCursor":"..."}
```

| 参数 | 说明 |
|---|---|
| `repo` / `actor` / `action` | 等值过滤 |
| `since` / `until` | 时间窗，RFC3339；**闭开区间** `[since, until)` |
| `limit` | 默认 100、上限 1000（超限 400） |
| `cursor` | keyset 游标（不透明，取上一页 `nextCursor`）；结果**倒序**（最新在前） |

`detail` 为对象（如 `quota.exceeded` 的 `{actor, repo, path, used, quota}`）；`time` 为 RFC3339 UTC。

### 词表与脱敏

M4 审计动作全集（可作 `action=` 过滤值）：

| 族 | 动作 |
|---|---|
| 制品 | `deploy`（上传/发布）、`download`、`delete` |
| 仓库 | `repo.create`、`repo.update`、`repo.delete` |
| 安全 | `group.create`、`group.update`、`group.delete`、`group.member`（成员集变更）、`permission.create`、`permission.update`、`permission.delete`、`password.change` |
| 治理 | `gc.run`、`quota.exceeded`、`export.run`、`import.run` |
| 会话 | `login.success`、`login.failed` |

**已知缺口**：`token.issue` / `token.revoke` 在词表中定义，但 token 签发/吊销当前**不落审计**（M4 登记缺口，签发无留痕——排障时以服务端日志为准）。

脱敏红线（QA 全量导出 grep 验证）：口令字面量、token 明文、`Authorization` 头**零命中**——审计里永远看不到这些值。

### 只追加

审计没有写面：对 `/api/v1/audit*` 的 PUT/DELETE/POST 一律 **404**（路由层即无此面）；元数据层无 UPDATE/DELETE 代码路径。归档留存请走数据库备份（见[备份手册](backup-restore.md)），不要试图「清理」审计。

## 搜索

跨仓统一入口（结果**按调用者权限过滤**——无 read 的仓库不出现；admin 全见）：

```bash
# 名称子串（SQL LIKE；缺 name → 400）
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/artifact?name=w.bin" 
# {"results":[{repo,path,size,...}]}；repos= 逗号分隔限定仓

# checksum 精确反查（sha256/sha1/md5 至少一个；跨仓去重可见——同 blob 多路径全返回）
SHA=$(shasum -a 256 w.bin | cut -d' ' -f1)
curl -su admin:$ADMIN_PW "$BASE/binflow/api/search/checksum?sha256=$SHA"
```

M4 边界（**404，有意不做**）：`/api/search/props|users|artifactory|pattern|badge`、AQL、gavc 结构化检索、`*` 通配。匿名实例关闭时未认证搜索当前返回 403（与 /api 家族 401 姿态尚不统一，已登记勘误）。

## GC（垃圾回收）

GC 回收**无引用**的 blob（删除制品后，同内容仍被其它路径引用的 blob **幸存**——去重语义）。两步操作：**先 dry-run 看报告，再显式 apply**。

```bash
# dry-run（apply 缺省 false；只报告不动数据）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc \
  -H 'Content-Type: application/json' -d '{"apply":false,"graceHours":0}'
# {"candidateCount":1,"candidateBytes":24,"deletedCount":0}

# apply（真正删除；控制台侧对应「输入实例名」二次确认）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/system/gc \
  -H 'Content-Type: application/json' -d '{"apply":true,"graceHours":0}'
# {"candidateCount":1,"candidateBytes":24,"deletedCount":1}
```

| 面 | 行为 |
|---|---|
| admin only | 非 admin 403、未认证 401 |
| `graceHours` 缺省 | 用配置 `storage.gc_grace_hours`（默认 **24h**）——刚删的孤儿在宽限期内**不算候选**（基准 = blob mtime，防误删正在写的会话） |
| `graceHours: 0` | **无宽限**（REST 显式 0 = 立即可回收）。注意与 CLI 的差异：CLI `--grace-hours 0` 仍是「用配置值」（flag 哨兵语义），两面有意不同 |
| `graceHours` 越界 | 负值或 >876000 → 400 |
| dry-run | `deletedCount` 恒 0、stats 不变 |
| apply | `deletedCount` 为实际删除数；被引用 blob 幂等幸存；再 dry-run 应为 0 候选 |
| 审计 | 每次成功运行（含 dry-run）落 `gc.run`，detail 含 apply/graceHours/候选数/释放字节 |
| 同步执行 | M4 REST 触发为**同步**（请求返回即完成；大库请用 CLI 兜底，见下） |
| 状态查询端点 | **无**（`GET /api/v1/system/gc` 不做）——上次运行经审计查：`GET /api/v1/audit?action=gc.run` |

**互斥锁（409 的含义）**：GC、export、import 三个维护操作共用 data 目录级跨进程文件锁（`<data_dir>/.maintenance.lock`）。锁被他人持有时：

- export 运行中 POST gc → **409**，message 含 `export in progress` 与持锁进程信息；
- GC 运行中再触发 GC → **409**（`another gc run is in progress`）；
- 反向：GC/import 运行中执行 export/import CLI → **退出码非 0**。

409 不代表数据问题——等当前操作完成后重试即可。

CLI 兜底（超长库 / 排障；dry-run 缺省）：

```bash
binflow-server gc -c binflow.yaml                    # dry-run，grace 取配置（默认 24h）
binflow-server gc -c binflow.yaml --apply --grace-hours 1   # apply，grace 1 小时
```

## 仓库治理字段：路径模式与配额（仅 local 仓）

建仓/改仓时三个治理字段（remote / virtual 仓不适用——这两型不落自有内容）：

| 字段 | 默认 | 说明 |
|---|---|---|
| `includesPattern` | `**/*` | 接纳的路径模式；不匹配的上传 → **409** |
| `excludesPattern` | 空 | 排除的路径模式；**excludes 优先**——命中即拒，即使也匹配 includes |
| `quotaBytes` | `0`（不限） | 仓库逻辑字节上限（nodes.size 之和，非去重物理量）；正整数或 0；负值 → 400 |

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/pattern-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic",
       "includesPattern":"**/*.jar","excludesPattern":"secret/**","quotaBytes":1073741824}' \
  -o /dev/null -w '%{http_code}\n'    # 200
```

### 路径模式：409 / 404 双值码

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/a/t.txt --data-binary @t.txt -w '\n%{http_code}\n'
# 409: Repository 'pattern-local' rejected deployment of 'a/t.txt':
#      the path does not match includesPattern '**/*.jar' (excludesPattern 'secret/**').

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/secret/x.jar --data-binary @x.jar -o /dev/null -w '%{http_code}\n'
# 409（excludes 优先命中）

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/pattern-local/a/ok.jar --data-binary @ok.jar -o /dev/null -w '%{http_code}\n'
# 201
```

下载侧：命中 excludes 的路径与普通 miss **逐字相同**的 **404**（不泄露「存在但被拦」）。未配模式的仓库行为与历史版本逐字节一致。

### 配额：413 语义

```bash
# 建限额仓 → 上传至超限
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/tiny \
  -H 'Content-Type: application/json' -d '{"rclass":"local","packageType":"generic","quotaBytes":1024}' \
  -o /dev/null -w '%{http_code}\n'
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/tiny/a.bin --data-binary @800b.bin -o /dev/null -w '%{http_code}\n'  # 201
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/tiny/b.bin --data-binary @800b.bin -w '\n%{http_code}\n'
# 413: Repository 'tiny' quota exceeded: used 800 of 1024 bytes; the write to 'b.bin' needs 800 more bytes.
```

| 面 | 行为 |
|---|---|
| **原子拒绝** | 被拒路径 GET 404、usage 不变、无索引残留；docker 上传会话可弃零残留 |
| **幂等重传豁免** | 已在库内容的同路径同内容重传（delta=0）在**恰好顶满**时也放行 201——重试/断点续传不会因配额误伤 |
| 读/删不受限 | 超限后 GET 200、DELETE 204；删后 used 回落，再传即 201 |
| 秒传与 mount 同受限 | `X-Checksum-Deploy` 引用已有 blob 到新路径、docker cross-repo mount 同样过配额门 |
| 计量口径 | 本地仓 logical bytes；remote pull-through 落盘**不计量**（M4 暂行） |
| 审计 | 每次拒绝落 `quota.exceeded`（detail: actor/repo/path/used/quota）+ WARN 结构化日志 |
| 用量查询 | `GET /api/v1/storage/usage/{repo}` → `{repo, usedBytes, quotaBytes}`（admin 或有 read 授权用户） |

五协议客户端在 413 时的表现（真机实测）：docker push `exit 1` + `denied: Repository ... quota exceeded`；npm `E413`；mvn deploy `Failed ... status code: 413`；twine `HTTPError: 413 Error`；curl 直接呈现 message。**已知边界**（P2 观察）：docker 多层 push 中先成功落盘的小层（含 mount 的 config）、maven deploy 的 pom/metadata 可能先落——被拒的写本身原子、manifest 不落，但「先落且配额内的部分」属每写语义保留；此类残留不影响 catalog/索引可见性，需要时整仓删除回收。

## 控制台对应页面

审计页（过滤 + 游标加载更多）、GC 页（stats + dry-run 面板 + 输入实例名确认 apply）、配额页（每仓水位条 80% 黄/100% 红 + 行内编辑）均消费与本文相同的 REST 面——脚本与界面行为可互证（页面测试即 API 测试）。

## 下一步

- 备份与恢复（export/import 与 GC 的锁互斥关系）：[备份与恢复手册](backup-restore.md)
- 组与权限 target：[用户组与权限管理](groups-permissions.md)
- 各协议上传的客户端侧配置：[接入指南](../README.md#客户端接入每协议一篇)
