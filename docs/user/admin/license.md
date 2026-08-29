---
title: License 与 Add-ons 管理
sidebar_position: 47
---

# License 与 Add-ons 管理

> 适用版本：M10（license/addon 体系随 M10 交付；ADR-0032/ADR-0033 为设计依据）+ **M11 增补**（conan/helm/rpm/debian 四槽位，T-308~T-311）+ **M12 增补**（`repo-operations` / `trashcan` 两功能槽，T-339/T-343/T-345——trashcan 档位**暂行 pro**，Q3 终裁建议 community）。REST 面见 [API 参考 · M10 新增端点速览](../api-reference.md#m10-新增端点速览t-296)；行为规格锚：`docs/design/architecture.md` §15。
> 本文不含任何密钥材料：签发工具的用法是文档面，钥料管理规程见下文「离线签发工具」的安全注意。

BinFlow 的功能分级由**一份 license 文档 + 一张编译期 addon 槽位矩阵**决定：

- **不装 license 也能跑**：实例按 community 地板运行，M1~M9 全部既有能力（五核心包型 + 属性系统）不受影响。
- **装 license 解锁档位**：`pro` 解锁 go/nuget/cargo（M10）+ conan/helm/rpm/debian（M11）七个包型，及制品操作族与回收站两个功能槽（M12）；`enterprise` 追加 HA / Xray 集成槽位（本体 M13+）。
- **矩阵是代码不是数据**：槽位的最低档位（MinTier）是代码常量——不存在可在运行时篡改的许可面，改档位 = 发版。

## 三档语义

| 档位 | 语义 | 解锁面 |
|---|---|---|
| `community` | 地板。**未装 license / license 过期 / 验签失败 / 档位不足时一律降级到此档**（降级闭集 D1~D7） | 五核心包型（generic/docker/maven/npm/pypi）+ 属性系统（properties）——即 M1~M9 全部能力 |
| `pro` | 中档 | 追加 go / nuget / cargo（M10 试点）+ conan / helm / rpm / debian（M11）七个包型 |
| `enterprise` | 高档 | 追加 ha / xray-integration 两个功能槽位（占位可见，本体 M13+） |

降级行为要点（排障时先想这五条）：

| 形态 | 行为 |
|---|---|
| **读不劫持（D1）** | 门控包型的内容 GET/HEAD 恒 200——卸载/过期后既有制品照常可拉；remote pull-through 的内部落盘同样豁免（读路径不问门） |
| **写动词 403（D2）** | 门控包型的 push/PUT/DELETE → 403 errors[] 信封 `license required: addon '<id>' needs tier '<t>' (current: <tier\|none>)` + 响应头 **`X-Binflow-License-Required: <addonID>`**（CI 可按此头分支，无需解析信封） |
| **建仓 400（D3）** | 建仓/改仓/删仓/virtual 成员面涉及未解锁包型 → 400 点名包型与档位（配置校验面，非许可面） |
| **到期即降级（D6）** | **无宽限期**：每日 ticker 检测到过期即降回 community，无需重启；`daysToExpiry` 负值 = 已过期 N 天 |
| **装失败不动现状** | 安装被拒（验签失败 400）后当前 license 原样保留 |

license 文档里的 **addons 白名单**可以收窄授权：`--addons "go"` 的 pro 文档只解锁 go，nuget/cargo 呈「已锁定」，拒绝文案为 `license required: addon 'nuget' is not named in the license addon allowlist (current: pro)`。

## 安装 license

### 方式一：控制台粘贴（推荐）

控制台 → 管理模式 → **License & Add-ons**（路径 `/admin/general/license`，需 admin/readonly_admin 可见）：

1. admin 登录后在 License 卡片的文本域**粘贴 license 文档全文**（`.lic` 文件内容，两段 base64url 以 `.` 相连的单行文本）。
2. 点「装载 license」。验签通过 → 201，提示「License 已装载（`<档位>` 档，即刻生效）」；装载是进程内原子切换，无撕裂窗口。
3. 验签失败 → 400，原文呈现 `LICENSE_EXPIRED` / `LICENSE_INVALID` 两类 wire 码（错误体不含内部校验细节）；当前 license 不受影响。

控制台同时提供卸载入口（见下文）。readonly_admin 只读呈现（管理面写操作仅全量 admin，服务端 403 兜底）。

### 方式二：REST

`POST /binflow/api/system/license`，**body 为 license 文档原文**（注意：装/卸是 POST/DELETE，无 PUT 动词；Artifactory 复数路径 `/api/system/licenses` 有意不做——JFrog 格式文档装不进来，404 即引导）：

```bash
export BASE=http://localhost:8080
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/system/license \
  --data-binary @pro.lic -o /dev/null -w '%{http_code}\n'
# 201（资源生效）；body 为装载后的状态 JSON（字段同下文查询）
```

权限：GET 挂 `system:read`（admin / readonly_admin 可见状态）；POST/DELETE 挂 `system:write`（**仅全量 admin**，readonly_admin 403）。

## 状态查询

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/system/license | jq
```

已装 pro license（365 天）的响应形如：

```json
{
  "licensed": true,
  "tier": "pro",
  "licenseId": "0d1f...e2",
  "licensee": "Acme Corp",
  "issuedAt": "2026-08-26T03:21:00Z",
  "notBefore": "2026-08-26T03:21:00Z",
  "expiresAt": "2027-08-26T03:21:00Z",
  "perpetual": false,
  "daysToExpiry": 364,
  "addons": null,
  "limits": null
}
```

| 字段 | 说明 |
|---|---|
| `tier` | 生效档位；未装 = `community` |
| `perpetual` | 永久 license（community 档才可永久） |
| `daysToExpiry` | 剩余整天数；**负值 = 已过期 N 天**（D6 已降级，读不劫持）；永久/未装 = `null` |
| `addons` | **文档的显式白名单**（`null` = 按档位全量解锁）；逐槽位的实时解锁视图看 `/api/v1/addons` |
| `limits` | 预留字段，当前不签发 |

GET 与 POST 成功体**永不回显文档原文**——载荷段与签名不出现在任何响应或日志里。

## 卸载 license

控制台 License 卡片「卸载 license」按钮需**输入 `UNINSTALL` 强确认**；REST 等价：

```bash
curl -su admin:$ADMIN_PW -X DELETE $BASE/binflow/api/system/license
# 200 纯文本：License removed successfully.
```

- **幂等**：未装 license 的实例 DELETE 同样 200。
- 卸载后即刻降回 community 地板：门控槽位的建仓/写入面（D3/D2）立即关闭，**既有制品读不受影响**（降级不劫持数据）。
- 只有真实发生卸载才落审计（未装时的 DELETE 是无操作的 floor-keeper，不记）。

## addon 槽位矩阵

`GET /binflow/api/v1/addons` 返回全部槽位的**实时求值**（与门控执行同源——可见性与执行不可能分叉）：

```bash
curl -su admin:$ADMIN_PW $BASE/binflow/api/v1/addons | jq
# [
#   {"id":"generic","kind":"package-type","minTier":"community","enabled":true,
#    "displayName":"Generic","description":"..."},
#   {"id":"go","kind":"package-type","minTier":"pro","enabled":false,
#    "reason":"license tier community < pro","displayName":"Go Modules","description":"..."},
#   ...
# ]
```

M10 装配 11 个槽位，M11 增至 15 个，**M12 增至 18 个**（`internal/addons/slots.go` 为单一事实源）：

| addon id | 类型 | 名称 | 最低档位 | 说明 |
|---|---|---|---|---|
| `generic` | package-type | Generic | community（地板） | 任意布局非结构化制品（M1 基座） |
| `docker` | package-type | Docker Registry | community（地板） | OCI/Docker v2 push/pull/token 三面 |
| `maven` | package-type | Maven | community（地板） | Maven 布局 + maven-metadata.xml 计算 |
| `npm` | package-type | npm | community（地板） | packument/tarball 双面 |
| `pypi` | package-type | PyPI | community（地板） | simple 索引 + 上传面 |
| `properties` | feature | Artifact Properties | community（地板） | 矩阵参数剥离 + 节点属性读写（Artifactory 归 pro，BinFlow 有意作核心能力） |
| `go` | package-type | Go Modules | **pro** | GOPROXY @v 五端点（见 [Go 接入](../integrations/golang.md)） |
| `nuget` | package-type | NuGet | **pro** | v3 主面 + v2 全路由（M12 补全；见 [NuGet 接入](../integrations/nuget.md)） |
| `cargo` | package-type | Cargo (Rust) | **pro** | sparse 索引 + crates API（见 [Cargo 接入](../integrations/cargo.md)） |
| `conan` | package-type | Conan (C/C++) | **pro**（M11） | v2 修订链 + v1 数据面，三类仓型（见 [Conan 接入](../integrations/conan.md)） |
| `helm` | package-type | Helm Charts | **pro**（M11） | 经典 chart 仓 index.yaml 引擎（见 [Helm 接入](../integrations/helm-charts.md)） |
| `rpm` | package-type | RPM (Yum) | **pro**（M11） | repodata 引擎 + repomd 签名（见 [RPM 接入](../integrations/rpm.md)） |
| `debian` | package-type | Debian | **pro**（M11） | debPUT 坐标 + dists 索引 + InRelease 签名（见 [Debian 接入](../integrations/debian.md)） |
| `repo-operations` | feature | Artifact Operations | **pro**（M12） | copy/move/zip/`archive!`/explode 整族一槽（见[制品操作族](artifact-operations.md)） |
| `trashcan` | feature | Trash Can | **pro**（M12，**暂行**——Q3 终裁建议 community） | 删除捕获/恢复/保留期（见 [Trash can 管理](trash-can.md)） |
| `ha` | feature | High Availability | **enterprise** | 槽位占位（本体 M13+） |
| `xray-integration` | feature | Xray Integration | **enterprise** | 槽位占位（本体 M13+） |

档位 × 解锁数速查：community 6 槽（五核心 + properties）→ pro 16 槽（+七包型 + repo-operations/trashcan〔暂行〕+helmoci）→ enterprise 18 槽（+ha/xray-integration）。

行内 `reason` 的三种锁定原因（呈现面字段，断言只对 `id`/`minTier`/`enabled`）：

- `license tier community < pro` —— 档位不足（装对应档位 license 即解）；
- `not named in the license addon allowlist` —— 文档白名单收窄（需换发 license）；
- `disabled by configuration` —— 命中 `addons.disabled` 熔断（见下节，装 license 救不了）。

该端点**无写面**：槽位清单是代码装配，不存在 POST/PUT/DELETE；其它动词一律 404。

## `addons.disabled` 熔断配置

运维的全局断路器（对标 Artifactory `artifactory.addons.disabled` 的行为模式，BinFlow 自有拼写）——**排障旋钮**，例如上游故障期临时关掉某包型：

```yaml
# binflow.yaml
addons:
  disabled: "go,nuget"     # CSV；addon id 大小写敏感；空 = 不熔断
```

```bash
# env 等价（容器/系统部署）
BINFLOW_ADDONS__DISABLED=go,nuget
```

语义（as-built，ADR-0032 as-built 定案段）：

- **重启生效**：装配期一次性消费；删掉条目 + 重启即全部恢复。熔断**永不删除任何数据**。
- **写平面 + 配置面的断路器，不是数据墓碑**：熔断下该包型的内容 GET/HEAD 恒 200（`npm install` 等读客户端照常工作，remote pull-through 内部落盘豁免）；拒绝面 = 写动词 403（文案点名旋钮与恢复路径：`addon 'go' is disabled by configuration (addons.disabled); remove the entry and restart to restore it`）+ 建仓/改仓/删仓/virtual 成员 400。
- 熔断态的 403 **不带** `X-Binflow-License-Required` 头——装 license 解决不了配置熔断，头名不得说谎。
- 列入五核心 id（如 `docker`）会被 WARN 但照样生效（降级旋钮，慎用）。
- 同时也熔断 `properties` 槽位——`?properties` 写族与矩阵参数部署随槽位关闭。

## 离线签发工具：`bf license`

签发是**纯离线**行为（`bf license` 不联系任何服务器、不解析 profile）：私钥永远只活在 keygen 写它的那台机器上，经 `--key`/`$BINFLOW_LICENSE_KEY` 注入，**绝不进配置文件、不进服务器**。

```bash
# 1. 生成 ed25519 密钥对（私钥 0600 落 ./binflow-license-private.pem，已存在则拒绝覆写）
bf license keygen --as-go-const --pub-out pub.hex
# kid: ...
# private key: ./binflow-license-private.pem (mode 0600) — keep offline; never commit, copy or move it onto a server
# public key (hex): ...
# // internal/license/verifykey.go — first-issuance swap:
# const EmbeddedVerifyKeyID = "..."
# const embeddedVerifyKeyHex = "..."

# 2. 签发文档（pro 必须带 --days；community 缺省 = 永久）
bf license issue --licensee "Acme Corp" --tier pro --days 365 -o pro.lic
# license document written to pro.lic
# license id: ... / licensee: Acme Corp / tier: pro / expires: ... / addons: all addons (tier-wide)

# 3. 本地验视（两条判定：--key 的公钥 + 「原厂二进制收不收」）
bf license inspect pro.lic --key pub.hex
# license id / licensee / tier / kid / typ/alg/ver / issued at / not before / expires at / addons
# verification (--key pub.hex): OK
# stock binary (embedded key "..."): accepted|REJECTED
```

要点：

- **issue 的常用旗标**：`--addons "go"`（显式白名单，缺省 = 档位全量解锁）、`--not-before`（RFC3339，支持未来生效）、`--kid`（覆盖钥文件携带的 id）。工具在交出文档前会先过真实验签链自检——签名漂移在工具侧失败，不会到客户装机时才炸。
- **inspect 的退出码**跟随主判定（给了 `--key` 用它的公钥，否则用二进制内嵌公钥），脚本可直接分支；`-` 可从 stdin 读文档。inspect 永不接触私钥。
- **首发换钥流程**（bootstrap 密钥的私钥半已在首发时销毁）：keygen `--as-go-const` 输出的两个常量替换 `internal/license/verifykey.go` → **重编译并分发全部 binflow-server 二进制** → 新钥 issue 的文档在新二进制上可验。旧钥文档继续验签失败是**有意的 fail-safe 方向**。完整规程与决策依据：`DECISIONS.md` ADR-0032。
- 私钥文件组/其他用户可读时 issue 会 WARN（建议 `chmod 600`）。

## 审计事件

`GET /api/v1/audit?action=<action>` 可过滤（admin only）：

| action | 触发 |
|---|---|
| `license.install` | 装载成功（detail 含 tier/licensee/licenseId/expiresAt） |
| `license.delete` | 卸载（真实发生时） |
| `license.invalid` | 装载被拒（detail 只含 reason 分类，不回显被拒文档字段） |
| `license.addon.denied` | 门控写拒绝（detail 含 addon/tier/need） |

## 常见问题速查

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'go' is not available (license tier 'community' < 'pro')` | D3：档位不足 | 装 pro/enterprise license，或换核心五型仓 |
| push 403 + `X-Binflow-License-Required: go` | D2：写动词门控 | 装 license；**读不受影响** |
| push 403 但**无** `X-Binflow-License-Required` 头，文案点名 `addons.disabled` | 配置熔断（非许可问题） | 从 `addons.disabled` 移除条目并重启 |
| 装 license 400 `LICENSE_INVALID` | 验签失败（格式/签名/未知 kid） | 用 `bf license inspect` 本地复核；确认文档来自与二进制内嵌公钥配对的私钥 |
| 装 license 400 `LICENSE_EXPIRED` | 文档过期或未生效 | 检查 `expiresAt`/`notBefore`；重新签发 |
| 到期后读变 403？ | 不会 | D1 读恒放行，仅写/建仓面关闭；装新 license 即恢复 |

## 下一步

- 各门控包型接入：[Go](../integrations/golang.md) · [NuGet](../integrations/nuget.md) · [Cargo](../integrations/cargo.md)
- 属性系统（community 恒解锁）：[属性系统用法](../properties.md)
- API 面与错误信封：[API 参考](../api-reference.md)
