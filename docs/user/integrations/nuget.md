---
title: NuGet 接入（v3 / v2）
sidebar_position: 25
---

# NuGet 接入（v3 / v2）

> 适用版本：M10（nuget 包型为 **pro 档**能力——建仓/推送需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有包仍可还原）。协议行为基准：learn.microsoft.com/nuget/api 官方规范（clean-room 有公开规范以官方为准）。
> 验证客户端：**dotnet SDK 8**（8.0.412 实测，T-287：push / add package / restore / run 全链 + remote 缓存断言 + 公网 LIVE 腿）。

BinFlow 同时提供 **v3 主面**（service index / flatcontainer / registration / search / publish——`dotnet` 与现代客户端走这里）与 **v2 最小面**（`FindPackagesById()` + `$metadata`——nuget.exe 等老客户端）。`dotnet` 工作流只需一个 `nuget.config`。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（community 实例建 nuget 仓得 400 `package type 'nuget' is not available ...`）。
- dotnet SDK 8（或任何走 v3 协议的 NuGet 客户端）。

## URL 形态

| 平面 | 地址 |
|---|---|
| v3 service index | `$BASE/binflow/api/nuget/v3/<repoKey>/index.json` |
| v2（OData） | `$BASE/binflow/api/nuget/v2/<repoKey>/...` |

v3 index 自述全部资源端点（SearchQueryService / RegistrationsBaseUrl / PackageBaseAddress/3.0.0 / PackagePublish/2.0.0 / LegacyGallery）——客户端只认 index 里的 `@id`，无需手拼其余 URL。

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

remote 拉取的 registration 文档由 BinFlow 重写为本仓地址（分页保留）；`packageHash`（SHA-512）随包校验，`dotnet` 还原时自动核对。

## v3 行为速查（flatcontainer / search / DELETE）

| 端点（相对 v3 仓面） | 行为 |
|---|---|
| `<id>/index.json` | 版本清单（local=事实生成 / remote=透传 / virtual=成员并集）；**id 键小写折叠**（`Acme.Lib` → `acme.lib`），展示名保留 nuspec 原大小写 |
| `<id>/<ver>/<id>.<ver>.nupkg` | 包文件下载；同名 `.nupkg.sha512`/`.nuspec` 可直取 |
| `SearchQueryService`（`q`/`take`/`skip`/`prerelease`） | **存储事实检索**：local 全量 + remote/virtual **已落地缓存**——不转发上游搜索（上游 azuresearch 异主机异 query 面）。缓存未命中的 remote 包要先 restore 一次才可被搜到 |
| 版本归一化 | 官方规则：补零到三段、第四段保留、prerelease/build 小写（`1.0` → `1.0.0` 寻址） |
| DELETE（包或版本） | **硬删**版本目录，无 listed/unlisted 位；relist（重推同版本）不适用旧形态 → 405 |

## v2 最小面（老客户端）

```
$BASE/binflow/api/nuget/v2/<repoKey>/FindPackagesById()?id='<包id小写>'
$BASE/binflow/api/nuget/v2/<repoKey>/$metadata
```

- OData Atom feed（V2FeedPackage 属性族；`IsLatestVersion`=最新稳定 / `IsAbsoluteLatestVersion`=最新含预发布）；未知 id = 空 feed。
- `$metadata`（EDMX）随面提供，老客户端可发现。
- `Search()` / `$count` / `Packages()Id=` 有意不做（404）——现代客户端走 v3 search。

## 符号服务器边界（M11+）

BinFlow M10 **不提供符号服务器**：`.pdb` 的 SYMSRV 路径与 `.snupkg` 符号包端点未实现（请求按未知路径 404），符号分发能力归 M11+ NuGet 硬化。`.nupkg` 内嵌的文件（含随包打入的 pdb）随包原样存取不受影响；Visual Studio 的符号加载请继续指向独立符号源。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'nuget' is not available ...` | community 档（D3） | 装 pro/enterprise license |
| push 403 + 响应头 `X-Binflow-License-Required: nuget` | license 过期/卸载后的写门（D2）；**restore/读不受影响** | 重装 license |
| push 401 | 匿名写被拒（push 永远需要认证） | 补 `packageSourceCredentials` |
| push 400（id/version 校验） | nuspec 与寻址形态不一致 / 包损坏 / nuspec 超 4MiB | 重新 `dotnet pack` |
| push 409 | 同 id/version 已存在 | 升版本；或先 DELETE（硬删） |
| `dotnet add package`（无版本号）在 remote 仓搜不到包 | search = 存储事实，未缓存的 remote 包不在结果里 | 带 `--version` 走版本清单，或先 restore 一次落地缓存 |
| 版本寻址 404 | 未做归一化（如用 `1.0` 寻址存储为 `1.0.0`） | 用三段版本 `1.0.0` |
| 老客户端 Search()/`$count` 404 | v2 最小面有意不做 | 用 `FindPackagesById()` 或 v3 |

## 下一步

- 档位与门控语义：[License 与 Add-ons 管理](../admin/license.md)
- remote/virtual 仓配置细节：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 其它包型接入：[Go](golang.md) · [Cargo](cargo.md) · [Maven](maven.md) · [npm](npm.md) · [PyPI](pypi.md)
