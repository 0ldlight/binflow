---
title: 属性系统用法（矩阵参数与 ?properties）
sidebar_position: 60
---

# 属性系统用法（矩阵参数与 `?properties`）

> 适用版本：M10（属性系统随 M10 交付，**community 地板恒解锁**——无需 license；`addons.disabled` 含 `properties` 时除外，见 [License 与 Add-ons 管理](admin/license.md#addonsdisabled-熔断配置)）。REST 行见 [API 参考 · E 域](api-reference.md#e-通用制品域)；规格锚：`docs/design/architecture.md` §15.3。

给制品节点打键值标签的两个入口：

| 入口 | 时机 | 形态 | 典型用途 |
|---|---|---|---|
| **矩阵参数** | 部署时随 PUT 顺手打标 | 路径尾 `;k=v;k2=v2` | CI 流水线部署即打标 |
| **`?properties` REST 族** | 事后读/写/删 | `GET/PUT/DELETE /api/storage/{repo}/{path}?properties=...` | 发布脚本核对、QA 回写、清理策略数据面 |

两者写进同一存储（节点属性表），控制台制品详情的 **Properties 页签**可查看与编辑。

## 值域闭集（两入口同一套规则）

| 项 | 限制 |
|---|---|
| 键 | `[A-Za-z][A-Za-z0-9_.-]{0,63}`——字母开头，≤64 字符 |
| 值 | 非空、≤1024 字节、无控制字符 |
| 单键 | ≤32 个值；**值集合语义——同键内不允许重复值** |
| 单节点 | ≤64 个不同键 |

违反任何一条 → 400（矩阵参数臂与 REST 臂同文案同码）。

## 矩阵参数：部署时打标

内容 PUT 的路径尾随**成对**的 `;k=v` 序列会被整体剥离为部署属性，文件按剥离后的路径落盘：

```bash
export BASE=http://localhost:8080
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/app/app.bin;build=77;env=prod" \
  --data-binary @app.bin -o /dev/null -w '%{http_code}\n'
# 201 —— 存储路径是 generic-local/app/app.bin，节点带 build=77、env=prod
```

- **成对才剥离**：从第一个 `;` 起的整段，**每个** `;` 分段都含 `=` 才视为矩阵区域并剥离；任何一分段无 `=`（如 `file;name.jar`、`x;y;z.txt`）→ 整段保留为文件名字面（M1~M9 既有语义，含 `;` 的历史路径永久可达）。
- 同键重复出现则累积多值：`;env=prod;env=canary` → `env=[prod,canary]`。
- 剥离对**所有动词**做地址归一（GET 该文件也用剥离后的路径），但只有 PUT 族把属性落库。
- 目标路径也可以是目录（尾斜杠保留：`dir/;k=v` → `dir/`，folder 节点可携带属性）。

矩阵参数走**部署权限链**（与裸 PUT 相同），不额外要权限。

## `?properties` 三动词

挂在既有 `/api/storage/{repo}/{path}` 路由上；**读**走制品读门（匿名与否随全局开关），**写**要求该路径的 **`annotate`** 动词（属性是元数据不是内容——`deploy-cache` 不再覆盖属性写，也不要求 `delete`；动词语义与迁移注意见[用户组与权限管理](admin/groups-permissions.md#动作动词read--deploy-cache--annotate--delete--manage)）。

### GET：读取与过滤

```bash
# 全量键
curl -su admin:$ADMIN_PW "$BASE/binflow/api/storage/generic-local/app/app.bin?properties" | jq
# {"properties":{"build":["77"],"env":["prod"]}}

# 键过滤 + 尾 * 前缀通配（env* 命中 env/environment/...）
curl -su admin:$ADMIN_PW \
  "$BASE/binflow/api/storage/generic-local/app/app.bin?properties=build,env*" | jq
```

- **无命中 = 200 `{"properties":{}}`**（BinFlow 自有裁定，非 Artifactory 的 404）；节点不存在才是 404。
- `properties=*` 列全部键（等价无过滤）。
- `atomic=true`：任一**字面**过滤键缺失 → 404 `Property '<key>' was not found on '<repo>/<path>'.`（通配键永不触发——通配只能匹配不能 miss）。适合「核对齐了再放行」的发布门。

### PUT：合并写入

```bash
# 同名键值集整体替换、异名键保留（合并律）——成功 204 无 body
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/api/storage/generic-local/app/app.bin?properties=env=canary" \
  -o /dev/null -w '%{http_code}\n'
# 204 —— build=77 原样保留，env 变为 [canary]（原 prod 被替换）
```

- **node 须存在**（404）；不能用它创建文件。
- 多键/多值共用一套**逗号文法**（对原始 query 值先切分后解码）：`qa=passed,owner=team-a` = 两键；`env=prod,canary` = 一键两值；值里要放字面逗号用 `%2C`（切分发生在解码前，`%2C` 作为值内容存活）。
- `recursive=1`（folder 目标）：folder 行自身 + 其下**每个节点**应用合并——目录级批量打标。
- 空写集（无可解析对）→ 400 `Unspecified properties to set.`。

### DELETE：删键

```bash
# 点名删（幂等：键本就不在也 204）
curl -su admin:$ADMIN_PW -X DELETE \
  "$BASE/binflow/api/storage/generic-local/app/app.bin?properties=env" \
  -o /dev/null -w '%{http_code}\n'
# 204

# 前缀通配删 / 全删
curl -su admin:$ADMIN_PW -X DELETE \
  "$BASE/binflow/api/storage/generic-local/app/app.bin?properties=env*,qa"    # 删 env 前缀族 + qa
curl -su admin:$ADMIN_PW -X DELETE \
  "$BASE/binflow/api/storage/generic-local/app/app.bin?properties=*"          # 全删
```

- 与 PUT 同门（路径 `annotate`）、同 `recursive=1`、node 须存在。
- 空参数 → 400 `Unspecified properties to delete.`。

## 控制台 Properties 页签

制品详情（制品树点开目录/文件节点）→ **Properties** 页签（页签进 URL 段——`/artifacts/properties/<repo>/<path>` 深链直达）：

- **常显表单**：Property / Value 两个输入框 + `Add Property` 钮常驻（空态也在场）。**同名键 Add = 该键值集整体替换**（PUT 合并律的 UI 形态——「改值」就是同键重 Add），兄弟键保留；空态有引导文案。
- **网格搜索**：键/值子串过滤（大小写不敏感）+ 无匹配提示块 + 清除复位；计数行「属性 · N 个键（匹配 M）」。
- **删除走危险确认**：行内删除钮弹出红色确认对话框（可拒绝）。
- 校验与服务端同口径（键闭集/值限制/上限），非法即时反馈、Add 钮禁用——零坏请求出浏览器。
- 写门 = `annotate`（与 REST 同门）：readonly_admin 预收敛禁用（输入 + Add + 删除全禁）；普通用户保留写入口，无权限时服务端 403 行内呈现。

## CI 打标场景（PRD 场景 E）

流水线部署打标 → 发布脚本读标核对 → QA 回写结论——为后续按属性的搜索/清理策略备好数据面：

```bash
# （CI 账号用 Bearer token 认证——高频流水线请用 token 而非 Basic，见 FAQ「高 QPS 请用 Access Token」）
# ① CI 部署即打标（矩阵参数，一次请求完成部署+标记）
curl -s -H "Authorization: Bearer $CI_TOKEN" -X PUT \
  "$BASE/binflow/generic-local/app/app-1.4.0.bin;build=77;env=prod;commit=$GIT_SHA" \
  --data-binary @app-1.4.0.bin -o /dev/null -w '%{http_code}\n'        # 201

# ② 发布脚本核对（atomic：缺任一键即 404，门禁形态）
curl -s -H "Authorization: Bearer $REL_TOKEN" -o /dev/null -w '%{http_code}\n' \
  "$BASE/binflow/api/storage/generic-local/app/app-1.4.0.bin?properties=build,env&atomic=true"
# 200 —— build/env 齐备

# ③ QA 回写结论（REST 合并写入，不动其它键）
curl -s -H "Authorization: Bearer $QA_TOKEN" -X PUT \
  "$BASE/binflow/api/storage/generic-local/app/app-1.4.0.bin?properties=qa=passed,qa=signed-off" \
  -o /dev/null -w '%{http_code}\n'                                     # 204

# ④ 目录级批量打标：给整个版本目录补 release 标（folder + recursive）
curl -s -H "Authorization: Bearer $REL_TOKEN" -X PUT \
  "$BASE/binflow/api/storage/generic-local/app/?properties=release=v1.4&recursive=1" \
  -o /dev/null -w '%{http_code}\n'                                     # 204
```

属性写操作落审计 `props.write` / `props.delete`（detail 含键摘要与 recursive 标记），可在[治理指南](admin/governance.md)的审计查询里追溯。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| PUT 400 `Properties value cannot be empty. (key ...)` | 值为空（如 `;env=`） | 给值或去掉该对 |
| PUT 400 `... property key "..." must match [A-Za-z][A-Za-z0-9_.-]{0,63}` | 键字符集/长度越界 | 键字母开头，只用字母数字 `_.-` |
| PUT/DELETE 404 | node 不存在（属性族不能建文件） | 先部署制品 |
| PUT 400 `Unspecified properties to set.` / DELETE 400 `Unspecified properties to delete.` | 空/无可解析参数 | 检查 query 拼写 |
| 写 403 `permission denied: writing properties requires annotate access on the item`（实测） | 有读、或只有 `deploy-cache` 而无 `annotate` | 给 principal 授该路径 `annotate`（不需要 `delete`） |
| GET 404 `Property '...' was not found ...` | `atomic=true` 且字面键缺失 | 预期行为（门禁语义）；去掉 atomic 或补键 |
| 矩阵参数没生效（文件名带 `;k=v` 落盘） | 序列里有非成对分段（如 `;v1.2`） | 每段都必须 `k=v` 形态，否则整段按字面处理 |
| 上传接口拒绝矩阵参数（MPU 面） | `/api/v1/uploads` 平面不收矩阵参数 | 属性走内容 PUT 或 `?properties` 族 |

## 下一步

- 许可/槽位（properties 恒为 community 地板）：[License 与 Add-ons 管理](admin/license.md)
- 端点契约：[API 参考](api-reference.md)
- 审计查询：[治理指南](admin/governance.md)
