---
title: Go Modules 接入（GOPROXY）
sidebar_position: 24
---

# Go Modules 接入（GOPROXY）

> 适用版本：M10（go 包型为 **pro 档**能力——建仓/推送需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有内容仍可拉取）。协议行为基准：go.dev/ref/mod「GOPROXY protocol」官方规范；逆向规格 `docs/reverse/goproxy.md`（客户端节 §7）。
> 验证客户端：**go 1.26**（1.26.6 实测，T-285：`go mod download` / `go build` 全链 + 大小写模块 + remote 回源计数 + virtual 聚合）。

BinFlow 作为 Go module proxy：`GOPROXY` 一处配置，`go build` 从 BinFlow 解析模块；local 仓可 PUT 三件套发布私有模块，remote 仓代理 proxy.golang.org 等上游并缓存，virtual 仓本地优先聚合。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（`GET /api/system/license` 的 `tier` 为 `pro`/`enterprise`）——community 实例建 go 仓会得到 400 `package type 'go' is not available (license tier 'community' < 'pro')`。
- go 1.21+（sparse 之外的 GOPROXY 协议面历代兼容；实测锚 1.26）。

## GOPROXY 地址形态

```
GOPROXY=$BASE/binflow/<repoKey>
```

无额外 `goproxy` 路径段——repoKey 直接作第二段，与其余包型内容面同构。仓探针：`curl -s $BASE/binflow/go-local/` → 200 空体。

## 接入步骤

### 1. 创建 go 仓库

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/go-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"go"}' \
  -o /dev/null -w '%{http_code}\n'        # 200（无 license 时 400，见前置条件）
```

remote / virtual 同族（`url` 指 GOPROXY 上游，缺省 `https://proxy.golang.org`）：

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/go-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"go","url":"https://proxy.golang.org"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/go-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"go","members":["go-local","go-remote"]}' \
  -o /dev/null -w '%{http_code}\n'        # 200（成员序 = 优先序：local 在前即本地优先）
```

### 2. 发布模块（curl PUT 三件套）

官方 GOPROXY 协议是只读的——PUT 三端点是仓库侧扩展（发布私有模块用），`$version` 须 canonical（`v1.2.3`）或 pseudo-version 或 `+incompatible` 形态：

```bash
# .zip / .mod / .info 三件（.info 为两键 JSON：{"Version":"...","Time":"RFC3339"}）
curl -su admin:$ADMIN_PW -T mymod.zip \
  "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.zip" -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW -T mymod.mod \
  "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.mod" -o /dev/null -w '%{http_code}\n'   # 201
curl -su admin:$ADMIN_PW -T info.json \
  "$BASE/binflow/go-local/example.com/mymod/@v/v1.0.2.info" -o /dev/null -w '%{http_code}\n'  # 201

# 版本清单（只有 .zip 落盘的版本才进 list）
curl -su admin:$ADMIN_PW "$BASE/binflow/go-local/example.com/mymod/@v/list"
# v1.0.2
```

- 可带 `X-Checksum-Sha256/-Sha1/-Md5` 头：格式错 400、与实测不符 **409**。
- 部署属性走矩阵参数：`.../@v/v1.0.2.zip;build=77`（语法与限制见[属性系统](../properties.md#矩阵参数部署时打标)）。
- `.info` 缺失时服务端会按 zip 的时间戳合成（`PUT 什么 GET 什么`——已存的 `.info` 逐字节回读，服务端不重写）。

### 3. 配置 go 客户端环境变量

| 变量 | 作用 | BinFlow 场景 |
|---|---|---|
| `GOPROXY` | 代理 URL 列表 | `GOPROXY=$BASE/binflow/go-virt`；多源 `GOPROXY=$BASE/binflow/go-virt,https://proxy.golang.org,direct` |
| `GOSUMDB` | 公网校验和数据库；`off` 完全关闭 | **私有模块走 BinFlow 必设 `GOSUMDB=off`**（或 `GONOSUMDB` 前缀法，见下） |
| `GONOSUMDB` | 不对 sum.golang.org 校验的模块前缀 | `GONOSUMDB=example.com`（官方「私有代理」配方：仅私有前缀跳过校验，公共模块照常验） |
| `GOPRIVATE` | 私有模块 glob（= GONOPROXY/GONOSUMDB 的默认值） | **慎用**（见下「高频卡点」） |
| `GOINSECURE` | 允许 http 明文拉取的模块前缀 | http dev 实例：`GOINSECURE=$HOST` |

> **高频卡点（go1.26 实测）**：网上常见的 `GOPRIVATE='*'` 全私有姿势**会让所有模块绕过 GOPROXY 直连 VCS**（GOPRIVATE 同时是 GONOPROXY 的默认值），报 `unrecognized import path ... "?go-get=1"`。「全走 BinFlow」的可操作形态是 `GOPROXY=<binflow> + GOSUMDB=off`。另注意 `GONOSUMCHECK` 不是真实 go 环境变量（历史讹传），真实面是 `GONOSUMDB`/`GOPRIVATE`/`GOSUMDB`。

BinFlow **不代理 sumdb**（`sumdb/sum.golang.org/*` 三端点 M10 不做，按未知路径 404）——校验要么留公网（GONOSUMDB 前缀法），要么关（GOSUMDB=off，air-gapped 必设，否则 sumdb 校验卡死）。

### 4. 真实构建 roundtrip（go 1.26）

```bash
export GOPROXY="$BASE/binflow/go-local" GOSUMDB=off
mkdir scratch && cd scratch
go mod init example.com/scratch
go mod edit -require=example.com/mymod@v1.0.2
go mod download example.com/mymod@v1.0.2
cat > main.go <<'EOF'
package main

import _ "example.com/mymod"

func main() {}
EOF
go build ./...        # 构建成功（exit 0）——.info → .mod → .zip 三段全部来自 BinFlow
```

### 5. remote 代理与 virtual 聚合

```bash
# remote 直连可用（BinFlow 有意允许——无 Artifactory 的「remote 必须经 virtual」限制）
export GOPROXY="$BASE/binflow/go-remote" GOSUMDB=off
go mod download golang.org/x/mod@v0.17.0
go mod download golang.org/x/mod@v0.17.0   # 二次执行：上游接触恰 1 次，其余命中缓存

# virtual：本地模块命中 local，未命中走 remote
export GOPROXY="$BASE/binflow/go-virt" GOSUMDB=off
go mod download example.com/mymod@v1.0.2     # 命中 go-local
go mod download golang.org/x/mod@v0.17.0     # 未命中 → go-remote 回源
```

virtual 语义：`.zip/.mod/.info` 按成员序首个命中（命中成员经 `X-Binflow-Resolved-From` 响应头可见）；`@v/list` 跨成员并集；`@latest` 取全局最优候选。

## `!lower` 转义（大小写模块）

GOPROXY 协议用 case-encoding 消歧大小写不敏感文件系统：**模块路径与版本里的每个大写字母在 wire 上替换为 `!` + 小写**：

```bash
# 存储态模块 example.com/MyMod（PUT 时用解码形态或转义形态均可）
curl -su admin:$ADMIN_PW "$BASE/binflow/go-local/example.com/!my!mod/@v/v1.0.0.info"
# {"Version":"v1.0.0"}
```

- 版本同一套规则：`v1.0.0-BETA` 的 wire 形态是 `v1.0.0-!b!e!t!a`。
- 存储与展示保持解码形态（大写还原）；转义只存在于 wire 与回源 URL。
- `!!`（连续双叹号）与尾随 `!` 非法 → 400。

## 校验和链

| 层 | 谁负责 |
|---|---|
| 服务端实测 | PUT/GET 全程 `X-Checksum-Sha256/-Sha1/-Md5` 实测头；blob 内容寻址不可变 |
| `h1:` dirhash | **客户端域**——对 zip 内文件清单+内容的 SHA-256，zip 容器不参与；服务端不计算不存储，`go.sum` 校验全在客户端 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'go' is not available ...` | community 档（D3） | 装 pro/enterprise license（[License 管理](../admin/license.md)） |
| push 403 + 响应头 `X-Binflow-License-Required: go` | license 过期/卸载后的写门（D2）；**读与 `go mod download` 不受影响** | 重装 license；期间继续拉取 |
| `unrecognized import path ... "?go-get=1"` | GOPRIVATE 把模块送去了 VCS 直连 | 移除 GOPRIVATE，用 `GOPROXY=<binflow> + GOSUMDB=off` |
| 拉取卡死在 sumdb 校验 | 公网 sum.golang.org 不可达且未关 | `GOSUMDB=off`（全关）或 `GONOSUMDB=<私有前缀>` |
| `http: server gave HTTP response to HTTPS client` 类明文错误 | http 实例未放行 | `GOINSECURE=$HOST`（注意它不关 sumdb，常需同设 `GOSUMDB=off`） |
| PUT 400（版本文法） | `$version` 非 canonical/pseudo/+incompatible 形态 | 用 `vX.Y.Z` 规范拼写 |
| PUT 400（major 一致性） | 模块路径 `/vN` 后缀与版本 major 不一致（gopkg.in 的 `.vN` 同查） | 对齐路径与版本 |
| PUT 400（.info/.mod 内容） | `.info` 的 `Version` 缺失/非 canonical/与 URL 不一致；`.mod` 首个 `module` 指令与路径不一致 | 修内容重传 |
| PUT 409 `Checksum error ...` | 声明校验和与实测不符 | 重新计算校验和 |
| remote 仓 PUT → 405 | remote 仓只读（回源代理） | 发布走 local 仓或 virtual 的部署路由 |
| `@v/list` 404 但 `.mod`/`.info` 已传 | list 只聚合有 `.zip` 的版本 | 补传 `.zip` |

## 有意不做（M10 边界）

- **sumdb 代理**（`sumdb/sum.golang.org/{supported,lookup,tile}`）：不实现，按未知路径 404——校验策略用 `GOSUMDB`/`GONOSUMDB`。
- **external dependencies 重定向**（virtual 按模式分流独立远端）、**VCS git 直连 remote**、**curation 版本过滤**：不做。
- 410 Gone（retract 语义）M10 未启用；`HEAD .../@v/<v>.mod` 未命中回 410 是存在性探测的契约形态。

## 下一步

- 档位与门控语义：[License 与 Add-ons 管理](../admin/license.md)
- remote/virtual 仓配置细节：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 其它包型接入：[NuGet](nuget.md) · [Cargo](cargo.md) · [Maven](maven.md) · [npm](npm.md) · [PyPI](pypi.md)
