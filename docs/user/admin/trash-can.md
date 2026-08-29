---
title: Trash can（回收站）
sidebar_position: 51
---

# Trash can（回收站）

> 适用版本：M12（T-345；T-330 条件票转正承载；行为锚点 `docs/reverse/storage-layout.md` §5 / `inv-1-core.md` §B）。**档位暂行 pro**（feature 槽 `trashcan`，Q3 终裁建议降为 community——若终裁翻转，本文档位表述随之刷新）；community 实例**不捕获删除**（维持 M11 硬删语义）。
> 本文命令与响应取 T-345 真二进制 curl 全链（port 18345，真签名 pro license 文档）。

## 用途

local 仓的删除不再一去不返：**删除前先把节点捕获进内置仓 `auto-trashcan`**（布局 `auto-trashcan/<原仓key>/<原节点path>`，路径本身结构性编码出处），可浏览、可恢复、可永久清除；保留期（默认 14 天）到点由小时级 cron 自动清。

## 前置条件

- **pro 及以上 license**（槽 `trashcan`）：community 实例 REST 面答 403 + `X-Binflow-License-Required: trashcan`，且删除侧不捕获（删除仍为硬删，行为与 M11 逐字节一致）。
- trash REST 族的门是 `system:write`（**仅全量 admin**；readonly_admin 403、普通 user 403、匿名 401——gc/cleanup 破坏性管理面同款姿态）。
- 服务端默认装配 `enabled=true / retention=14d`（无配置旋钮，见「已知边界」）。

## 语义

### 捕获（删除时自动发生）

- 捕获点：local 仓删除的**权限门之后、任何源行删除之前**——捕获失败即中止删除（fail-closed：宁可留活、不可丢失）。逐项捕获出 error 同样中止。
- 每个节点打**属性五元组**（`?properties` 可见，T-345 真机实测）：

| 属性 | 值 |
|---|---|
| `trash.time` | 捕获时刻 epoch 毫秒 |
| `trash.deletedBy` | 执行删除的主体 |
| `trash.originalRepository` | 原仓 key |
| `trash.originalRepositoryType` | 恒 `local` |
| `trash.originalPath` | **本节点**的原路径（子树任一节点可独立恢复） |

- **跳过集**（本地生成物不入站，BinFlow 闭集）：`.jfrog/**`、`dists/**`（deb 索引）、任一段 `repodata` 或首段 `_tmp_*`（rpm 索引/暂存）、`maven-metadata.xml` 及其 checksum 族。协议制品路径全部入站。
- **不捕获面**（如实登记）：remote 缓存失效（逐缓存非制品删除）、`DELETE /api/repositories/{key}?deleteContent` 仓拆除、docker manifest/tag 删除（索引随行恢复语义归后续票）。

### 内置仓守卫

`auto-trashcan` 是装配数据：建仓/改仓/删仓拒 `auto-trashcan`（400）；内容面写入拒绝（trash 族是唯一写者）；virtual 成员校验拒绝；**没有任何 permission target 能点名它**——只有 admin / readonly_admin 可见，匿名读构造性不可能（NFR-S61）。

## 浏览（骑既有 storage 面）

没有第四个路由——浏览走既有存储面，控制台树里的 Trash Can 节点同源：

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>

# can 内树/条目信息
curl -su admin:$ADMIN_PW $BASE/binflow/api/storage/auto-trashcan | jq .

# 五元组（AC1 断言面）
curl -su admin:$ADMIN_PW \
  "$BASE/binflow/api/storage/auto-trashcan/vlibs/com/acme/v.jar?properties" | jq .
# {"properties":{"trash.time":["1787952557679"],"trash.deletedBy":["admin"],
#   "trash.originalRepository":["vlibs"],"trash.originalRepositoryType":["local"],
#   "trash.originalPath":["com/acme/v.jar"],"license":["apache-2.0"]}}
#   ↑ 原属性随行保留

# 目录清单
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/auto-trashcan/vlibs?list" | jq .
```

## 恢复

```bash
POST /binflow/api/trash/restore/{path}?to=&transaction-size=
```

```bash
# 按原位恢复（目的地解析：to 覆盖 > 属性五元组 > 路径结构首段）
curl -su admin:$ADMIN_PW -X POST \
  "$BASE/binflow/api/trash/restore/vlibs/com/acme/v.jar?transaction-size=100" | jq .
# {"messages":[{"level":"INFO","message":"moving … completed successfully, …"}]}
#   ↑ 响应形态 = copy/move 的 CopyOrMoveResult（恢复就是一次系统身份的 move）

curl -su admin:$ADMIN_PW \
  $BASE/binflow/vlibs/com/acme/v.jar -o v.jar
shasum -a 256 v.jar        # 与删除前源文件逐位一致（T-345 真机对账）
```

- 恢复后落地树**剥除全部 `trash.*` 标记**（原属性保留）；`to` 指向已存在同名文件 → 覆盖（move 族 override 语义）。
- 拒绝臂：`to` 不允许指向 can 本身 / 目标仓非 local → 400；目标仓已删除 → 404；`transaction-size` 非正整数 → 400（批语义惰性——逐项管线）。
- 审计：`trash.restore`（actor = 执行恢复的人）。

## 清空与单条永久清除

```bash
# 清空整个 can
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/trash/empty | jq .
# {"removed":2,"files":1,"folders":1,"bytes":3}

# 单条（子树）永久清除
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/trash/clean/vlibs/com | jq .
# 同构摘要 {"removed":…,"files":…,"folders":…,"bytes":…}
```

清空/清除后 can 根 `size:0`、探针全 404（零残留，T-345 真机断言）；对应 blob 离开 GC 的引用集、由常态 GC（宽限期后）回收。

## 保留期

- 默认 **14 天**：小时级 cron（`TrashEngine`）按 `trash.time`（epoch ms）判龄；捕获后、打标前崩溃的裸行降级回退 `updated_at` 判龄（保守删除而非永久滞留）。
- 过期 FILE 行删除、无文件子树的 folder 行清除；审计 `trash.retention`（actor `system-trash`）。
- license 锁定（槽 locked）时 cron 跳过（报告为 skip，非错误）。

**GC / cleanup 免疫**（负向断言钉死）：trash 内容 = 引用内容——GC 的 mark 集**包含 can 的节点行**，不可收；cleanup 的 policy 腿只走 remote 仓（can 是 local，结构性免疫）。

## 已知边界（如实登记）

| 项 | 现状 | 去向 |
|---|---|---|
| 配置旋钮 | `trashcan.enabled` / `trashcan.retention_days` 的 binflow.yaml 字段**未落**（internal/config 域小票承载）——生产实例恒为 enabled/14d | 配置域小票；落地后本文刷新 |
| 档位 | pro 暂行；Q3 终裁建议 community（clean-room 取证：Artifactory OSS 发行即携带 trash 基座，无 addon 门证） | conductor/PM 终裁；翻转点 = slots.go 一行 + 四处测试断言 |
| docker manifest 删除 | 不入站（恢复需索引随行） | 独立票 |
| 覆盖入站（`send.overwrites.to.trashcan`） | 未实现——PUT 覆盖同名文件不进 can | 后续票候选 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| `POST /api/trash/empty` 403 + `X-Binflow-License-Required: trashcan` | community 档（槽 locked） | 装 pro license（档位若经 Q3 终裁翻转则以 `/api/v1/addons` 实时判定为准） |
| 删除后 can 里没有（community 实例） | 捕获侧同槽门控——不捕获即硬删 | 装 license；数据已在删除时物理移除则不可恢复 |
| trash 动词 403（已认证、无 license 头） | 门是 `system:write`：readonly_admin / 普通 user 拒 | 用全量 admin 执行 |
| `GET /api/storage/auto-trashcan` 404 | 尚未发生过捕获（内置仓懒落库） | 先在 pro 实例上删一次东西 |
| restore 400 | `to` 指向 can 自身 / 目标仓非 local | 换 local 目标仓 |
| restore 404 | 目标仓已删除 | 先重建目标仓 |
| 建/删名为 `auto-trashcan` 的仓 400 | 系统仓守卫（防抢注/拆除） | 不要用这个 key |

## 下一步

- 同族操作面（copy/move/zip/archive!/explode）：[制品操作族](artifact-operations.md)
- GC / cleanup 与 trash 的分界：[治理：审计、GC 与配额](governance.md)
- 档位体系：[License 与 Add-ons 管理](license.md)
