---
title: npm 接入
sidebar_position: 21
---

# npm 接入

> 适用版本：M3（publish/packument/dist-tags/unpublish/login + remote/virtual；PRD milestone-3 v1.2）；**M8 起发布权限语义更新**（见「发布权限语义」节——npm CLI 连发多版本实测 npm 10.9.8 / node 22，T-249）；**M9 复核**：复制引擎同口径钉死（T-262——目标凭据 `read`+`write` 即可，见该节末）；**M13 补注**：registry/token 尾斜杠配对实证矩阵 + 交互式 login 现状修正（FR-122.3，T-374 实测 npm 10.9.8 / node 22）。
> 本文核心链在 M3 QA 基线（commit `0f86229`，T-74/T-76 验收产物）上复跑：`.npmrc`（registry 限定 `_auth` 形态）publish、缓存清空重装、whoami 均退出码 0（复跑记录见 `reports/agents/T-77.md`）；scoped/dist-tag/unpublish/remote 代理/virtual 聚合取自 T-74/T-76 验收记录。客户端锚定 npm 10.x（10.9.8 实测，node 22）。

把 BinFlow 当作私有 npm registry：`.npmrc` 一处配置，`npm publish` 发内部包、`npm install` 装内部与上游包——registry 协议按 npm 官方规范实现，scoped 包、dist-tag、unpublish 开箱可用。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 管理员凭据 `admin` / `$ADMIN_PW`。
- 一个 `packageType=npm` 的仓库（第 1 步创建）。

## registry URL 形态

npm 域挂在 `/binflow/api/npm/` 前缀下（与 docker 的根级 `/v2` 不同）：

```
$BASE/binflow/api/npm/<repoKey>/
```

**尾部斜杠不能省**。tarball 的内容路径 `/binflow/<repoKey>/<name>/-/<name>-<version>.tgz` 是同一文件的第二入口（curl 直取可用）。

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
export NPM_REG=$BASE/binflow/api/npm/npm-local/
```

### 尾斜杠配对：registry 行与凭据行（高频踩坑，M13 实证）

npm 客户端按「registry URL 字符串」匹配 `.npmrc` 里的凭据行（`_auth` / `_authToken`）。npm 10.9.8 / node 22 真机矩阵（T-374，2026-08-31）：

| registry 行 | 凭据行 | 结果 |
|---|---|---|
| `…/npm-local/`（带斜杠） | `//…/npm-local/:_auth=…`（带斜杠） | 全绿（publish / whoami / install）——即上文形态 |
| `…/npm-local`（**无斜杠**） | `…/:_auth`（带斜杠） | **ENEEDAUTH**：`npm error need auth This command requires you to be logged in to http://…/npm-local`（错误信息回显无斜杠 URL，可作排查线索） |
| `…/npm-local/`（带斜杠） | `//…/npm-local:_auth`（**无斜杠**） | **ENEEDAUTH**（同上） |
| `…/npm-local`（无斜杠） | `…:_auth`（无斜杠，两侧一致） | **仍然 ENEEDAUTH**——npm 的凭据键匹配不接受无斜杠形态，一致也救不了 |

要点：

- **两条都必须逐字带尾斜杠**。只改 registry 行不改凭据行（或反过来）= publish/whoami 立刻 `need auth`。
- 更隐蔽的是：**匿名拉包不受影响**（`npm view` / `npm install` 照常绿）——实例默认开匿名读时，CI 表现为「install 好的、publish 突然 401/need auth」，第一反应往往去查服务端权限，实际是 `.npmrc` 斜杠形态。
- **token 形态同坑**：`//…/npm-local/:_authToken=<token>`（带斜杠）全绿；去掉斜杠同 ENEEDAUTH。token 从管理面签发（见[管理指南](../admin/token-step-up.md)或 `POST /api/security/token`）。
- registry 换仓（如 local → virtual）时，`registry=` 行与凭据行的**主机+路径段要同步改**——两行指向不同仓时凭据照样匹配不上。

## 接入步骤

### 1. 创建 npm 仓库

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/npm-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"npm"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

### 2. 写 `.npmrc`（即拷即用）

项目根目录 `.npmrc`（或用户级 `~/.npmrc`）。凭据行必须**按 registry 限定**：

```ini
registry=http://localhost:8080/binflow/api/npm/npm-local/
//localhost:8080/binflow/api/npm/npm-local/:_auth=<base64 of admin:口令>
always-auth=true
```

`_auth` 的生成：

```bash
printf 'admin:%s' "$ADMIN_PW" | base64
```

> 高频卡点（npm 10 实测）：项目级 `.npmrc` 里写裸 `_auth=` 会被 npm 直接拒绝——
> `npm error Invalid auth configuration found: '_auth' must be renamed to '//<host>/<path>/:_auth'`。
> 凭据行必须带 `//<host>/<registry 路径>/:_auth` 前缀；registry 换仓时该行要同步改。
> 只读场景（匿名读默认开）可以只写 `registry=` 一行，不配 `_auth`。

### 3. 发布与验证

```bash
cd my-pkg && npm publish
# + demo-pkg@1.0.0（退出码 0；服务端 201 {"success":true}）
npm whoami          # admin
npm view demo-pkg version    # 1.0.0
```

scoped 包：

```bash
npm publish --access public          # + @acme/util@1.0.0
npm install @acme/util               # URL 编码 @acme%2Futil，BinFlow 双编码等价接受
```

`dist.tarball` 一律重写为指回 BinFlow 的 URL（上游原地址不回显），`dist.shasum`/`dist.integrity` 由服务端实测生成——`npm install` 的锁文件校验三方一致（tarball 实测 / packument / package-lock integrity）。

### 4. 安装（含缓存清空重装）

```bash
mkdir consumer && cd consumer && echo '{}' > package.json
# consumer 目录也需要 .npmrc（registry 配置不继承，缺省时 npm 走公网 registry——实测坑）
npm install demo-pkg
npm cache clean --force && rm -rf node_modules package-lock.json
npm install demo-pkg && node -e 'console.log(require("demo-pkg"))'
```

### 5. dist-tag / unpublish

```bash
npm dist-tag add demo-pkg@1.0.0 beta
npm install demo-pkg@beta
npm dist-tag ls && npm dist-tag rm demo-pkg beta
npm unpublish demo-pkg@1.0.1 --force   # 版本从 packument 移除，dist-tags 引用联动清理
```

unpublish 内部的 `PUT .../-rev/<rev>` 步骤 BinFlow 恒回 `200 {"ok":"updated package"}`（npm 客户端协议前置占位，包内容不动——npm CLI 行为依赖它）。

`npm login`（npm ≥ 9 需 legacy 形态）：**M13 实测注记（T-374）——当前版本交互式 `npm login --auth-type=legacy` 不可用**：npm 把账号口令放在登录 PUT 请求体里、不带认证头，而服务端内容面要求写动词先过认证（匿名 PUT 直接 401），请求到不了登录端点的 body 凭据臂（M3 验收时该交互流程未直跑，T-77 O-4 已留痕；修复归服务端票）。token 端点本身正常：带 Basic 头访问即 201 铸出与管理面同表的 token（可吊销）。**日常直接用 `.npmrc` 的 `_auth`（Basic）或 `_authToken`（管理面 / `POST /api/security/token` 签发）两行之一**，形态见上文「尾斜杠配对」。

## 发布权限语义（M8 起）

CI 账号的 permission target 该授什么？按操作分臂（T-249 转换感知判定，实测 npm 10.9.8）：

| 操作 | 所需权限 | 说明 |
|---|---|---|
| 发布新版本（含包的第二个及以后版本） | **仅 `write`** | 追加新版本 / dist-tags 与 time 等文档级簿记变化 = 标准发布路径，走 write 臂——**无需 delete**（与 npmjs.org 语义对齐） |
| `npm dist-tag add/rm`（版本数据不动） | **仅 `write`** | dist-tag 移动是簿记操作 |
| 改既有版本数据：`npm deprecate`、篡改 manifest 字段、删除版本（`-rev` PUT 缩减 `versions`） | **需 `delete`**（无则 403） | 已发布版本数据的重写保持「覆写 = 删除」权限对 |
| 重发同版本（tarball 已存在，内容无论改没改） | **无条件 403**（有 delete 也拒） | `Cannot modify pre-existing version '<v>'`——版本不可变 |

**给 CI 发布仓配权的最短答案**：`read` + `write` 即可满足日常连发（`npm publish` 任意多版本 + dist-tag）；仅当流程需要 `npm deprecate` 或改写已发布版本元数据时才补 `delete`。

**复制引擎同口径（M9 钉死）**：`push_npm` 复制对目标仓的写只有两种形态——「目标所缺版本的**单版本发布文档**」与「单 tag 的 dist-tag PUT」，**从不整包覆写** packument。因此上表对复制目标侧 principal 同样成立：**`read` + `write` 即可承载镜像同步，无需 `delete`**（T-262 全栈实测：write-only 凭据全新包/追加版本双绿，同版本不同数据走 first-write-wins 目标幸存，竞态对撞落 403 终态）。给复制任务配凭据时照 CI 口径配即可。

> 历史注记：M8 之前包的第二个版本发布会误命中「覆写需 DELETE」臂（T-247 在真实 Jenkins 流水线发现）——M8 的 T-249 修复后按上表语义执行。若你的实例仍是旧版本，CI 退避方案是给 principal 补 `delete`（对制品不可变面无实际风险：tarball 层的同版本重发仍无条件 403）。

## remote / virtual 仓的用法

- **remote 仓**（代理上游）：`.npmrc` 的 registry 指向 `$BASE/binflow/api/npm/npm-remote/`，packument 与 tarball 经 BinFlow 回源缓存；上游不回显、二次安装零上游流量。
- **virtual 仓**（聚合）：registry 指向 `$BASE/binflow/api/npm/npm-virtual/`，本地包与上游包一次 `npm install demo-pkg up-pkg` 装齐。
- **重要边界（M3）**：`registry.npmjs.org` 的 packument 不在 BinFlow/Artifactory 的 `<name>/packument.json` 布局路径上，**npmjs 真上游暂不可代理**（`npm install lodash` → 404 `Package 'lodash' not found`，T-75 真机复核）。remote 仓适用于布局兼容的上游（内网 Nexus/Artifactory 等）；npmjs 代理归 M4。Maven Central 与 pypi.org 的真上游代理均已可用，见[管理指南](../admin/remote-virtual.md#上游兼容性速查)。
- 代理仓的上游日志里会看到 npm 客户端对 `npm` 自身 packument 的自检探测（版本检查），属正常噪音，上游 miss 后进负缓存。

## 匿名与凭据

| 场景 | 行为 |
|---|---|
| 匿名 GET（默认 `anonymous_access: true`） | packument/tarball 200——安装、CI 拉包免凭据 |
| 匿名 publish（PUT） | **401** `authentication required` |
| 全局关匿名 | GET 也需 `_auth`（`npm install` 无凭据直接失败，带 `_auth` 正常） |

连通性探测：`curl $BASE/binflow/api/npm/npm-local/-/ping` → `200 {}`。

## 有意不兼容与差异（npm 域）

| 行为 | BinFlow | 依据 |
|---|---|---|
| 同版本重复 publish | **403** `Cannot modify pre-existing version '<v>', aborting upload for: '<name>'`（npm CLI 报 E403） | PRD v1.2 定案（409→403） |
| `integrity`（sha512）与 tarball 实测不一致 | **400** 拒绝（Artifactory 默认不强制；BinFlow 有意从严） | NE-01 决策 |
| `npm search`（`/-/v1/search`）、audit（`/-/npm/v1/security`） | **404**——搜索/审计端点不做 | §2.2 |
| tarball 裸 PUT（内容路径直传） | **405**——npm 域发布仅认 packument PUT 十步链 | T-76 注记 |
| packument `_attachments` | GET 面不返回 | NPM-API 惯例 |
| 包索引隐藏目录 | 不落 `.npm/` 隐藏目录（探针 404） | 规格 §4.5 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| `npm error need auth This command requires you to be logged in…`（ENEEDAUTH，install 却正常） | `.npmrc` 的 registry 行或 `_auth`/`_authToken` 凭据行缺尾斜杠（或两行指向不同仓） | 两行都逐字带尾斜杠（见「尾斜杠配对」矩阵） |
| `npm error Invalid auth configuration found: '_auth' must be renamed to ...` | 项目级 `.npmrc` 用了裸 `_auth` | 凭据行改 `//<host>/<路径>/:_auth` 限定形态（上文第 2 步） |
| publish E403 `Cannot modify pre-existing version '1.0.0'` | 同版本已发布（不允许覆盖） | `npm version` 升版本后重发；或先 `npm unpublish` |
| 连发第二个版本 E403（仅旧实例） | M8 之前的实例把 packument 追加误判为覆写（T-247 发现，T-249 修复） | 升级到 M8；过渡期给 CI principal 补 `delete` |
| publish E400 `Conflict between integrity from metadata and tarball` | packument integrity 与 tarball 内容不符（BinFlow 强制校验） | 重新 `npm pack` 生成一致的元数据 |
| install 报 404 / 装到了公网同名包 | 当前目录没有 `.npmrc`，registry 缺省走 npmjs | 每个项目目录都放 `.npmrc`（或写用户级） |
| 401 `authentication required` | 匿名 publish，或全局关匿名后未配 `_auth` | 配置 `_auth` |
| dist-tag 操作 404 `npm package not found with name:<n>, and tag:<t>` | 包名或 tag 不存在 | 核对 `npm dist-tag ls` |
| `npm whoami` 报错 | 未配凭据（whoami 需要认证） | 补 `_auth` 或 `npm login --auth-type=legacy` |

## 下一步

- remote/virtual 仓的创建与缓存管理：[remote/virtual 管理指南](../admin/remote-virtual.md)
- Maven / PyPI 接入：[maven](maven.md) · [pypi](pypi.md)
- 从 Artifactory 迁移的概念对照：[faq.md](../faq.md)
