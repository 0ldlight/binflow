---
title: Conan（C/C++）接入
sidebar_position: 27
---

# Conan（C/C++）接入

> 适用版本：M11（conan 包型为 **pro 档**能力——建仓/上传需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有制品仍可下载/构建）。
> 验证客户端：**conan 2.31.2** 与 **conan 1.66.0** 双版本实测（T-308/T-312：create / upload / install / list / remove / remote login 全链 + latest 解析对账）。行为基准 `docs/reverse/conan.md`。

BinFlow 实现 conan 修订链协议：**local / remote（代理缓存）/ virtual（聚合）三类仓型齐备**。conan 2 客户端走 v2 端点族；conan 1.x 走 v1 数据面（仅 local 仓）。

- 仓 URL：`$BASE/binflow/<repoKey>`（v2/v1 前缀由客户端自动拼接）。
- 能力头每个响应都带：`X-Conan-Server-Version: 0.20.0`；`X-Conan-Server-Capabilities` local = `complex_search,checksum_deploy,revisions,matrix_params`，remote/virtual 追加 `only_v2`。
- 修订链：上传产生不可变 revision（RREV/PREV）；`index.json` 记录修订史，latest = 首项。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（community 建 conan 仓得 400 `package type 'conan' is not available ...`）。
- conan 2.x（推荐）或 conan 1.66+。

## local 仓：发布与消费（conan 2）

### 1. 建仓

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/conan-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"conan"}' -o /dev/null -w '%{http_code}\n'   # 200
```

### 2. 注册 remote 并登录

```bash
conan remote add binflow $BASE/binflow/conan-local
conan remote login binflow admin -p <口令>
# Authenticated 'admin' in the 'binflow' remote
```

（登录走 `users/authenticate`，口令换 BinFlow access token 亦可。）

### 3. 创建并上传

```bash
conan new hello/1.0 --template=cmake
conan create .                              # 本地构建验证
conan upload 'hello/*' -r binflow --confirm
# 服务端日志：PUT 先 404（checksum-deploy 零 body 试探 miss）后 201（真实 body）——两跳是机制本体
```

- 同一 `ref` 再次 upload（recipe 或 binary 变更后）产生**新修订**——旧修订不可变、保持可下载。
- 校验链：PUT 带 `X-Checksum-*` 头，不符 409；零 body 的 checksum-deploy 部署命中已有 blob 时 201。

### 4. 消费（install 解析 latest）

```bash
conan remove 'hello/*'                      # 清本地 cache（演示用）
conan install --requires=hello/1.0 -r binflow --build=missing
# Downloaded recipe revision fcbe9506...      ← latest RREV
# Downloaded package revision 01cc0a11...     ← latest PREV

conan list 'hello/1.0#*' -r binflow --format=json   # 修订史（JSON）
```

### 5. 删除

```bash
conan remove 'hello/1.0' -r binflow         # 删 latest 修订链（v2 latest 404 为止）
```

## conan 1.x 客户端（仅 local 仓）

```bash
conan remote add binflow $BASE/binflow/conan-local
conan user admin -p <口令> -r binflow
# Changed user ... to 'admin'

conan create . && conan upload 'hello/1.0@myuser/stable' --all -r binflow
conan search -r binflow
# Existing package recipes:
#   hello/1.0@myuser/stable

conan install hello/1.0@myuser/stable --build=missing -r binflow
```

v1 上传的修订在 v2 视角下可见（修订 `0` 注册进 index）；两平面同树同真相。

> **v1 数据面仅 local**：remote/virtual 上的 v1 族请求 → 400 `Unsupported Conan v1 repository request for '<repoKey>'`（能力头 `only_v2` 即此意；握手三端点例外，三类仓都服务）。

## remote 仓（代理上游）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/conan-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"conan","url":"https://center2.conan.io"}' \
  -o /dev/null -w '%{http_code}\n'           # 200

conan remote add binflow-remote $BASE/binflow/conan-remote
conan remote login binflow-remote admin -p <口令>
conan install --requires=hello/1.0 -r binflow-remote --build=missing
```

- 读路径全走 pull-through 引擎：首次回源、此后命中缓存（负缓存 / TTL 双类 / 守卫回源 / stale-while-error 语义与其它包型 remote 一致，见 [remote/virtual 管理](../admin/remote-virtual.md)）。
- **search 不代理**（引擎上游跳是路径拼接，带 query 的端点不可达）：`conan search -r <remote>` 对应的服务端端点回 404 诚实文案——已知版本直接 `install --requires=<精确ref>` 拉取。
- 写拒绝：PUT → 405（只读代理）。

## virtual 仓（聚合）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/conan-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"conan",
       "repositories":["conan-local","conan-remote"],
       "defaultDeploymentRepo":"conan-local"}' -o /dev/null -w '%{http_code}\n'  # 200

conan remote add binflow-virt $BASE/binflow/conan-virt
conan remote login binflow-virt admin -p <口令>
conan install --requires=hello/2.0 -r binflow-virt --build=missing    # 聚合读
conan upload 'hello/3.0' -r binflow-virt --confirm                    # 路由写（进 conan-local）
conan list 'hello/3.0#*' -r binflow-virt --format=json                # 归并视图立即含新修订
```

- `revisions` / `latest` 按**成员序去重合并**（首见成员行保留，time 降序）；文件下载两桶序首命中（`X-BinFlow-Resolved-From` 标来源）。
- 写：PUT 路由到 `defaultDeploymentRepo` 指定的 local 成员；未配置 → 405。DELETE 不跨成员传播。
- search 并集仅 local 成员（remote 成员目录不可枚举）。

## reindex（管理面）

从 `.timestamp` 事实重建修订索引（误操作/外部导入后修复用）：

```bash
# 全实例（query 或 JSON body 两种入参）
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/conan/reindex?repoKey=conan-local"
# 单仓
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/conan/conan-local/reindex
# Calculated Conan index for repository 'conan-local' (path ''): N revisions reindexed.
```

门 = 认证 + CanManageRepo(write)；仅 local 仓（其它类 400）；同步执行。

## 边界与有意不做（M11）

| 项 | 行为 |
|---|---|
| remote search 代理 | 不做（引擎约束）——404 诚实文案；`install --requires` 按精确 ref 可用 |
| `forceConanAuthentication` 仓配置 | 已实现（默认 false 维持普通 ACL；true 时匿名全端点 401+Basic 挑战——T-355A） |
| 缓存落点 | remote 缓存落在仓自身命名空间（无 `-cache` 独立缓存仓） |
| v1 DELETE 语义 | 删 latest 修订链（旧修订存活） |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'conan' is not available ...` | community 档 | 装 pro/enterprise license |
| upload 403 + `X-Binflow-License-Required: conan` | license 过期/卸载后的写门；**读不受影响** | 重装 license |
| conan 1.x 对 remote/virtual 操作 400 `Unsupported Conan v1 repository request` | v1 数据面仅 local（握手除外） | 客户端升 conan 2，或直连 local 仓 |
| `conan search -r <remote>` 失败 | remote search 不代理 | 用 `conan list '<ref>#*'` / 精确 `install --requires` |
| virtual 上传 405 | 未配 `defaultDeploymentRepo` | 建仓时指定 local 成员为写路由 |
| 上传 revision 409 | checksum 头与内容不符 | 重传一致内容 |

## 下一步

- 三类仓型通用语义（缓存/负缓存/写路由）：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 其它包型接入：[Helm](helm-charts.md) · [RPM](rpm.md) · [Debian](debian.md) · [Maven](maven.md) · [npm](npm.md)
