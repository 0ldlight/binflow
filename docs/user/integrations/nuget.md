---
title: NuGet 接入（v3 / v2）
sidebar_position: 25
---

# NuGet 接入（v3 / v2）

> 适用版本：M10 起（nuget 包型为 **pro 档**能力——建仓/推送需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有包仍可还原）；**M12 补全**（T-337：v2 全路由集实装；T-341：v3 remote/virtual search 升为上游实时代理 + service index 动态解析）。协议行为基准：learn.microsoft.com/nuget/api 官方规范（clean-room 有公开规范以官方为准）。
> 验证客户端：**dotnet SDK 8**（8.0.412 实测，T-287：push / add package / restore / run 全链 + remote 缓存断言 + 公网 LIVE 腿）；M12 增量以**真 curl 逐字矩阵**验证（T-337/T-341 live 矩阵，环境无 dotnet SDK 时按 M11 conan 先例回落），并对 nuget.org 真上游活体闭合 OData 参数语义。

BinFlow 同时提供 **v3 主面**（service index / flatcontainer / registration / search / publish——`dotnet` 与现代客户端走这里）与 **v2 全路由面**（OData 全 18 端点：`Search()` / `Packages()` / `GetUpdates()` / `$batch` / `Download` / DELETE / PUT 双形——nuget.exe 2.x 与老工具链走这里）。`dotnet` 工作流只需一个 `nuget.config`。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（community 实例建 nuget 仓得 400 `package type 'nuget' is not available ...`）。
- dotnet SDK 8（或任何走 v3 协议的 NuGet 客户端）。

## URL 形态

| 平面 | 地址 |
|---|---|
| v3 service index | `$BASE/binflow/api/nuget/v3/<repoKey>/index.json` |
| v2（OData）base | `$BASE/binflow/api/nuget/v2/<repoKey>`（GET 根 = service document；PUT 根 = v2 直推） |

v3 index 自述全部资源端点（SearchQueryService / RegistrationsBaseUrl / PackageBaseAddress/3.0.0 / PackagePublish/2.0.0 / LegacyGallery，M12 起另含 `registration-semver2` 行——`semVerLevel=2.0.0` 查询的客户端被指到独立 semver2 基）——客户端只认 index 里的 `@id`，无需手拼其余 URL。

## 接入步骤

### 1. 创建 nuget 仓库

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/nuget-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"nuget"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

# remote（代理 nuget.org）与 virtual（聚合）同族
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/nuget-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"nuget","url":"https://api.nuget.org/v3/index.json"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/nuget-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"nuget","members":["nuget-local","nuget-remote"]}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

### 2. 写 `nuget.config`（源映射 + 凭据）

放在解决方案根目录（与 `.sln` 同级；MSBuild 向上查找）：

```xml
<?xml version="1.0" encoding="utf-8"?>
<configuration>
  <packageSources>
    <clear />                                    <!-- 断开 nuget.org 缺省源：解析全走 BinFlow -->
    <add key="binflow"
         value="http://localhost:8080/binflow/api/nuget/v3/nuget-virt/index.json" />
  </packageSources>
  <packageSourceCredentials>
    <binflow>
      <add key="Username" value="admin" />
      <add key="ClearTextPassword" value="<你的管理员口令>" />
    </binflow>
  </packageSourceCredentials>
</configuration>
```

- `<clear />` + 单源 = 源收口（内部包与上游包都从 virtual 解析）；只想加源不想收口就去掉 `<clear />`。
- 凭据是 **Basic**（`dotnet` 对 push/restore 自动携带）；只读场景若实例匿名读开启，可省 `packageSourceCredentials`。
- 密钥管理注意：`ClearTextPassword` 为明文，CI 上建议用环境变量扩展（`value="%NUGET_PW%"` 视 CI 而定）或用户级加密配置。

### 3. 打包与推送（dotnet 8 实测）

```bash
dotnet new classlib -n Acme.Lib && cd Acme.Lib
dotnet pack -c Release
# 输出 bin/Release/Acme.Lib.1.0.0.nupkg

dotnet nuget push bin/Release/Acme.Lib.1.0.0.nupkg --source binflow
# 已推送包。（成功——dotnet 8 自行发现 PackagePublish 并 PUT，身份取自包内 nuspec）
```

服务端行为：

- push 体为单 part 的 multipart/form-data（dotnet 8 实测形态），裸流也照收；包大小上限 **512MiB**。
- 校验链：zip/nuspec 可解析、id/version 一致 → 400 家族拒绝；落 `.nupkg` 后**服务端生成** `.nuspec` 与 `.nupkg.sha512` 两个 sidecar（可再生件）。
- 成功 **201 + Location**（包地址）。
- 同 id/version 重复 push → 409；改内容需先删（见下文 DELETE 语义）。

### 4. 还原与运行

```bash
cd .. && dotnet new console -n Consumer && cd Consumer
dotnet add package Acme.Lib --source binflow     # 无版本号：走 search 面解析最新
# 或精确版本（走 flatcontainer 版本清单）：dotnet add package Acme.Lib --version 1.0.0
dotnet restore
dotnet run          # 引用生效，程序输出正常（T-287 实测：hello from T287.Lib）
```

### 5. remote 代理与缓存断言

```bash
# 消费公共包：virtual 里未命中 local 即回源 nuget.org 并缓存
dotnet add package Newtonsoft.Json --version 13.0.2 --source binflow
dotnet restore && dotnet run
# T-287 实测：Newtonsoft.Json 13.0.2 经 BinFlow remote 拉取（含强制 gzip 的 registration
# 代理 + 镜像 302 + packageHash 客户端校验），整个客户端流程上游恰好联系 1 次——
# 第二次 restore 起零上游流量。
```

remote 拉取的 registration 文档由 BinFlow 重写为本仓地址（分页保留；M12 起改写目标按**上游 service index 动态解析**出的 `@id` 匹配，异构挂载点的源自适应，见下文）；`packageHash`（SHA-512）随包校验，`dotnet` 还原时自动核对。**上游临时故障/删除**时，已缓存的 registration 与包照常服务（T-341 live 矩阵：上游 registration 翻 404 后 BinFlow 仍 200 服务缓存副本）。

## v3 行为速查（flatcontainer / search / DELETE）

| 端点（相对 v3 仓面） | 行为 |
|---|---|
| `<id>/index.json` | 版本清单（local=事实生成 / remote=透传 / virtual=成员并集）；**id 键小写折叠**（`Acme.Lib` → `acme.lib`），展示名保留 nuspec 原大小写 |
| `<id>/<ver>/<id>.<ver>.nupkg` | 包文件下载；同名 `.nupkg.sha512`/`.nuspec` 可直取；v2 Download 面（`/api/nuget/v2/<repo>/Download/<id>/<ver>`）与 v3 缓存互通（T-341：`v2/live-remote/Download/live.v3/1.0.0` = 200 且字节一致） |
| `SearchQueryService`（`q`/`take`/`skip`/`prerelease`/`semVerLevel`） | **M12 起分仓型**：local = 存储事实；remote = **上游实时代理**（见下）；virtual = 两源合并（优先档成员压制同 id，档内按 id 分组、版本并集排序；`totalHits`=合并全量）。不再要求「先 restore 落地缓存才能被搜到」 |
| registration / `registration-semver2` | M12 新增 semver2 路由族（`registration-semver2/{index,page,<version>.json}`）；`semVerLevel=2.0.0` 的搜索结果与 registration 链接指到 `-semver2` 基；registration 文档的 `packageContent` 改写指向本仓 **v2 Download** 面（版本小写、剥 build metadata） |
| 版本归一化 | 官方规则：补零到三段、第四段保留、prerelease/build 小写（`1.0` → `1.0.0` 寻址） |
| DELETE（包或版本） | **硬删**版本目录，无 listed/unlisted 位；relist（重推同版本）不适用旧形态 → 405 |

### remote search 的上游代理（M12）

- 代理链：上游 service index 取回并缓存（`.nuGetV3/feed.json`，复用 remote 缓存到期语义）→ 定位 `SearchQueryService` @id（层阶：plain → `/3.0.0-beta` → `/3.0.0-rc`，首个命中）→ 带 `q/skip/take/prerelease/semVerLevel` 直连上游（不落盘）→ 结果 URL 全量改写为本仓 v3 registration 基。
- 上游 400 → `take=100` 重试一次；其他非 200/空体 → **贡献空**（200 JSON，`totalHits:0`——不 5xx）。
- **搜索出站不带上游凭据**（如实登记）：公开源（nuget.org / BaGet / MyGet）匿名可用；带凭据源（如 Azure Artifacts）的搜索回落空集，包的下载/还原不受影响（下载链走 remote 缓存引擎，凭据照常）。
- 异构上游自适应：feed 地址按约定 `<repo.url>` 推导 `v3/index.json`，index 内各 `@id` 逐字段自适配（custom 挂载路径的源可用）；index 不可解析时回落 nuget.org 族常量（护栏）。

## v2 全路由面（M12，老客户端）

base = `$BASE/binflow/api/nuget/v2/<repoKey>`，OData Atom feed（V2FeedPackage 属性族；`IsLatestVersion`=最新稳定 / `IsAbsoluteLatestVersion`=最新含预发布）：

| 端点（相对 base） | 行为 |
|---|---|
| `GET /`（service document） | workspace/collection 形状（nuget.org 实抓对齐） |
| `GET /$metadata` | EDMX，含 Search/GetUpdates FunctionImport；`DataServiceVersion` 头随面提供 |
| `GET /Search()`（括号可省）± `/$count` | 行管线与 Packages 共享；**空集 → 404**（OData 语义）；`semVerLevel` 过滤见下 |
| `GET /FindPackagesById()?id='<小写id>'` ± `/$count` | **缺 id 参数 → 404**（非 400）；未知 id → 200 空 feed（收集语义） |
| `GET /Packages()` | 全 feed；`(Id='',Version='')` 单条目未命中 404；`(Id=)/Id` 属性投影；`()/$count` 计数（不寻址单条目） |
| `GET /GetUpdates()/?…` ± `/$count` | **服务端过滤**（nuget.org 活体闭合：严格大于基准版本、预发布默认滤、`versionConstraints='[a,b)'` 范围、`includeAllVersions=true` 全量） |
| `POST /$batch` | 202 + `multipart/mixed; boundary=batchresponse_<guid>`，七形态分发；不支持形态整批 400 `Unsupported batch entry.` |
| `GET /Download/{id}/{version}` | 包文件下载：local 三级链 / remote canonical→`api/v2/package` 上游 hop（marker 缓存）/ virtual local-先-remote 两趟 |
| `GET /{file}.nupkg`（任意深度） | 直通存储（免 v2 门，权限归存储层） |
| `DELETE /{id}/{version}` | 200 `Successfully removed '<path>'`（T-337 live 逐字）/ 404 / 无删权限 403 `Unable to delete NuGet package '<path>'`；remote → 400 |
| `PUT /`（multipart，字段名 `package`） | 201 `Successfully published NuPkg to: <path>`（live 逐字，如 `team/live.a/1.1.0/live.a.1.1.0.nupkg`）；缺字段 → 400 精确文案。**落点差异**：Artifactory 落仓根扁平 `<id>.<version>.nupkg`，BinFlow 落 canonical flatcontainer 三件套（201 文案中的 path 随之） |
| `PUT /{前缀}` | deployPath = `<前缀>/<nuspec.id>.<nuspec.version>.nupkg`（身份取自 nuspec，按规格字面） |

**重复推送臂**（v2 直推与 v3 push 分立，语义不同——v3 push 重复恒 409）：

| 形态 | 结果 |
|---|---|
| 同 id/version 已存在，无 `delete` 权限 | **409** `Package already exist: <deployPath>` |
| 同 id/version 已存在，有 `delete` 权限 | 覆盖 → 201 |
| 同 id/version 且字节相同 | 幂等 201 |
| remote 仓 / 未路由 virtual | 400 `This operation can only be performed on local repositories.` |

**`semVerLevel` 过滤语义**（nuget.org 活体闭合，FluentAssertions/StackExchange.Redis 双源实测）：SemVer2-only = **点分预发布标识（≥2 个点分段，如 `alpha.1`）或含 build metadata**；`3.0.0-alpha`/`rc1` 等 v1 形预发布在默认档可见。

**remote 代理（v2）**：上游 URL = `<repo.url>/api/v2/<资源拼写>`（查询串 verbatim）；响应经 `.nuget-v2/<hex>.xml` marker 落缓存（引擎 TTL/负缓存/stale 全链生效），解析后重锚（下载链接 → 本仓 v2 Download 面）；上游故障时回落落地事实（离线臂——remote 仓从未缓存过且上游故障 → 404 家族）；`$count` 非数字体 → `-1` 哨兵。

**virtual 合并（v2，九步）**：local/remote 两桶顺序成员解析 → fan-out → `$select` 全程重置 → GetUpdates 无 `$orderby` 按 Version 排 → **跨桶 id 级去重**（桶内版本级）→ 链接重锚本 virtual Download → id 小写升序 + 版本升序 → IsLatest legacy 聚合（过滤后可见集上重算）。

## 符号服务器边界（后续里程碑）

BinFlow **不提供符号服务器**：`.pdb` 的 SYMSRV 路径与 `.snupkg` 符号包端点未实现（请求按未知路径 404），符号分发能力不在 M12 交付面（M12 补的是 v2 全路由与 v3 代理）。`.nupkg` 内嵌的文件（含随包打入的 pdb）随包原样存取不受影响；Visual Studio 的符号加载请继续指向独立符号源。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'nuget' is not available ...` | community 档（D3） | 装 pro/enterprise license |
| push 403 + 响应头 `X-Binflow-License-Required: nuget` | license 过期/卸载后的写门（D2）；**restore/读不受影响** | 重装 license |
| push 401 | 匿名写被拒（push 永远需要认证） | 补 `packageSourceCredentials` |
| push 400（id/version 校验） | nuspec 与寻址形态不一致 / 包损坏 / nuspec 超 4MiB | 重新 `dotnet pack` |
| push 409 | v3 push 同 id/version 已存在（v3 面恒 409；v2 直推的重复臂见上表——有 `delete` 权限可覆盖） | 升版本；或先 DELETE（硬删） |
| `dotnet add package` 在 remote 仓搜索为空（200 `totalHits:0`） | M12 起 remote search 是上游实时代理：上游不可达/非 200 → 贡献空（不 5xx）；带凭据源（Azure Artifacts）搜索回落空集 | 检查上游可达性；带 `--version` 走版本清单（flatcontainer 不依赖搜索）；公开源匿名可用 |
| 版本寻址 404 | 未做归一化（如用 `1.0` 寻址存储为 `1.0.0`） | 用三段版本 `1.0.0` |
| v2 `Search()` 空集 404 / `FindPackagesById()` 缺 id 404 | OData 语义（空 feed 以 404 表达；缺 id 非参数错误） | 核对 id 拼写（小写）；单包探测用 `FindPackagesById()?id=` |
| v2 直推 409 `Package already exist: …` | 同 id/version 已存在且当前用户无 `delete` 权限 | 授 delete（可覆盖）或升版本 |
| v2 直推 400 `…can only be performed on local repositories.` | 对 remote / 未路由 virtual 推送 | 推到 local 仓（virtual 配 defaultDeploymentRepo 可路由） |

## 下一步

- 档位与门控语义：[License 与 Add-ons 管理](../admin/license.md)
- remote/virtual 仓配置细节：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 其它包型接入：[Go](golang.md) · [Cargo](cargo.md) · [Maven](maven.md) · [npm](npm.md) · [PyPI](pypi.md)
