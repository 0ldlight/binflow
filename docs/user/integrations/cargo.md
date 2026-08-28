---
title: Cargo（Rust）接入
sidebar_position: 26
---

# Cargo（Rust）接入

> 适用版本：M10（local 仓全量）+ **M11 增补**（remote pull-through 与 virtual 聚合两类仓型，T-316/T-318；publish 失败面 CG-2 双轨终裁，T-316）。cargo 包型为 **pro 档**能力——建仓/发布需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有 crate 仍可下载/构建。协议行为基准：The Cargo Book「Registry Index」「Registry Web API」官方规范；逆向规格 `docs/reverse/cargo.md`。
> 验证客户端：**cargo 1.98**（1.98.0 实测——local 面 T-294：publish / add / build / yank / search 全链 + cksum 对账；remote/virtual 面 T-316/T-318 + M11 终验：缓存续供 / 写路由 / 混合构建全链）。规格锚 1.8x，新版更强。

BinFlow 作为 **sparse HTTP registry**（crates.io 兼容）：`.cargo/config.toml` 注册一个 alternate registry，`cargo publish` 发内部 crate、`cargo add/build` 消费；索引与下载地址由 `config.json` 自述，cargo 全自动协商。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（community 实例建 cargo 仓得 400 `package type 'cargo' is not available ...`）。
- cargo 1.70+（sparse registry 默认支持；实测锚 1.98）。
- 三类仓型 M11 起齐备：`local`（下文接入步骤）、`remote`（sparse pull-through 代理缓存）、`virtual`（聚合入口）——后两类见[下文专节](#remote-仓pull-through-代理缓存)。

## 接入步骤

### 1. 创建 cargo 仓库

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
export REG=$BASE/binflow/cargo-local
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/cargo-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"cargo"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

# 探针：sparse 入口配置（dl/api 指回 BinFlow 自身；base 取 server.base_url，空则按请求推导）
curl -s "$REG/index/config.json"
# {"dl":"http://localhost:8080/binflow/cargo-local/v1/crates","api":"http://localhost:8080/binflow/cargo-local"}
```

### 2. 配置 alternate registry 与 token

项目无关的用户级配置 `~/.cargo/config.toml`（旧拼写 `.cargo/config` 同效）：

```toml
[registries.binflow]
index = "sparse+http://localhost:8080/binflow/cargo-local/index/"
```

- `sparse+` 前缀**必带**（sparse HTTP 协议声明）；尾斜杠按上例保留。
- http 明文地址 cargo 会告警但不阻断；生产建议 TLS。

token 三选一（发布/yank 必须认证；下载与索引按实例匿名策略，默认免凭据）：

```bash
# ① 交互登录（粘贴 BinFlow 用户 token，存 ~/.cargo/credentials.toml）
cargo login --registry binflow

# ② 环境变量（CI 推荐）
export CARGO_REGISTRIES_BINFLOW_TOKEN=<BinFlow 用户 token>

# ③ 凭据文件直接写
# ~/.cargo/credentials.toml:
# [registry.binflow]
# token = "<BinFlow 用户 token>"
```

> wire 形态注意（T-294 真机探针实证）：cargo 发的是**裸 `Authorization: <token>` 头——无 `Bearer` scheme**（crates.io 兼容形态）。BinFlow 的认证面自 M10 起接受该形态；token 用[管理面签发](../faq.md#高频场景高-qps-请用-access-token)的 access token 即可。

### 3. 发布（publish）

```bash
cargo new mycrate --lib && cd mycrate
echo 'description = "demo crate"' >> Cargo.toml      # 元数据随需补
cargo publish --registry binflow
# Published mycrate v0.1.0 at registry `binflow`

# 发布后索引立即可见（同步重写，无需等待）
curl -s "$REG/index/my/cr/mycrate"
# {"name":"mycrate","vers":"0.1.0","deps":[],"cksum":"b82c1c70…f69","features":{},"yanked":false}
```

- 索引路径按 crate 名长度四档推导：1 字符 `index/1/{n}`、2 字符 `index/2/{n}`、3 字符 `index/3/{首字符}/{n}`、≥4 字符 `index/{前两字符}/{第3-4字符}/{n}`（**文件名小写**，行内 `name` 保留原大小写）。
- 校验链：crate 名 = 首字符字母 + `[A-Za-z0-9_-]`、≤64；`vers` 严格 SemVer 2.0（前导零拒绝）。**同 name+version（忽略 build metadata）重复发布不再答 409**（M11 翻转，T-316）：由删除权限闸门裁决——调用方可删该版本 → 覆盖上传（索引按忽略 build metadata 收敛为唯一行）；不可删 → 403。**注意 cargo 1.98 客户端会预检索引并在本地拒绝重复发布**（`already exists on ... index`，PUT 根本不发）——日常重复发布走「删后重发」，见[常见报错对照](#常见报错对照)。
- publish wire = `[u32 LE JSON 长度][元数据 JSON][u32 LE crate 长度][.crate tarball]`，服务端流式解帧、实测 sha256 入库——全部由 cargo 客户端自动构造，手工 curl 不必复刻。

### 4. 消费（add / build）

```bash
cd .. && cargo new consumer && cd consumer
cargo add mycrate --registry binflow
# Locking 1 package to latest Rust 1.98.0 compatible version
cargo build
# Finished `dev` profile [unoptimized + debuginfo] target(s) in 1.57s
```

依赖固定用 registry 限定形态写入 `Cargo.toml`：

```toml
[dependencies]
mycrate = { version = "0.1", registry = "binflow" }
```

### 5. yank / unyank（撤回不删除）

```bash
cargo yank --registry binflow mycrate@0.1.0
# Yank mycrate@0.1.0
curl -s "$REG/index/my/cr/mycrate"       # 行内 "yanked":true

# cargo 1.98 无 `cargo unyank` 子命令——现行拼写是 --undo（旧版 1.8x 两者皆可）
cargo yank --undo --registry binflow mycrate@0.1.0
# Unyank mycrate@0.1.0
```

yank 语义（官方）：**不是删除**——只翻索引行的 `yanked` 布尔；已写进 `Cargo.lock` 的项目照常构建，新解析不再选该版本，**下载持续 200**。crate 或版本不存在 → 404（errors 信封）。

### 6. search（curl 直查）

```bash
curl -s "$REG/api/v1/crates?q=mycrate&per_page=10" | jq
# {"crates":[{"name":"mycrate","max_version":"0.1.0",...}],"meta":{"total":1}}
```

按官方契约：`q` 匹配名称+描述（通配）、排除 yanked、按名去重取最高非 yank 版本；`per_page` 默认 10、上限 100。search 不带 Authorization（官方姿势），按实例全局匿名策略执行。

## cksum 对账（发布完整性）

三方一致是 BinFlow 的发布完整性闭环（T-294 真机实证 `b82c…f69 == b82c…f69`）：

```
索引行 "cksum"  ==  .crate 节点存储实测 sha256  ==  下载体 sha256
```

```bash
# 手工对账：下载 .crate 并本地验 sha256
curl -s -o /tmp/mycrate.crate "$REG/v1/crates/mycrate/0.1.0/download"
shasum -a 256 /tmp/mycrate.crate | cut -d' ' -f1     # 应与索引行 cksum 逐字符一致
```

cargo 客户端每次下载都按索引行 cksum 自动校验——对账失败（内容被篡改）会在客户端侧报错。

## remote 仓（pull-through 代理缓存）

M11 起 cargo 支持 `remote` 仓型：索引与下载首次回源、此后命中本地缓存，上游故障不传染（cargo 1.98 实测，T-316 + M11 终验 L36）。

### 1. 建仓

```bash
export REG_REMOTE=$BASE/binflow/cargo-remote
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/cargo-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"cargo",
       "url":"http://cargo-up.example.com:8080/binflow/cargo-local"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

# 探针：config.json 合成自指（dl/api 指回本 remote 仓——下载必经缓存层）
curl -s "$REG_REMOTE/index/config.json"
# {"dl":"http://localhost:8080/binflow/cargo-remote/v1/crates","api":"http://localhost:8080/binflow/cargo-remote"}
# 上游 config.json 原文缓存在仓根 config.original.json（排障可直取比对）
```

- **上游语法前提**：上游须为**单主机 sparse wire 语法**（其 `config.json` 的 dl/api 指向同主机路径——BinFlow / Artifactory cargo 面 / 自建兼容源均属此类）。crates.io 直连（index/dl 双主机形态）不支持；需要 crates.io 制品时以上游为兼容源级联，或待后续版本扩展。
- 内网上游（如同机/内网 BinFlow 实例）需 `"allowPrivateUpstream": true`——SSRF 守卫默认拒绝私网/环回目标（`upstream target refused — private or suppressed upstream`），见 [remote/virtual 管理](../admin/remote-virtual.md#ssrf-防护与-allowprivateupstream-放行指引)。

### 2. 客户端接入与行为

registry 指向 remote 仓即可，token/凭据配置与第 2 步完全同形：

```toml
[registries.binflow-remote]
index = "sparse+http://localhost:8080/binflow/cargo-remote/index/"
```

```bash
cargo add cup --registry binflow-remote && cargo build   # 经 remote 解析+下载全绿
curl -s -o /dev/null -D - "$REG_REMOTE/v1/crates/cup/0.5.0/download" | grep X-Binflow
# X-Binflow-Cache: HIT        ← 二次下载命中本地缓存
```

| 面 | 行为 |
|---|---|
| index / 下载 / `.cargo` sidecar GET | pull-through：MISS 回源流式落地、期内命中缓存；命中带 `X-Binflow-Cache: HIT` |
| search（`api/v1/crates?q=`） | 查询串逐字代理上游（响应体逐字服务，按查询独立缓存）——实测回 `{"crates":[{"name":"cup","max_version":"0.5.0",...}]}`，即上游事实；上游硬故障（hardFail/超限 502）→ 409 + errors 信封，assumed-offline 静默期 → 404 |
| **缓存续供** | 上游删除 crate（文件+sidecar+索引全删）后，**全新项目 `cargo add` + `build` 仅凭 remote 缓存照常成功**（实测铁证）——上游故障/误删不传染 |
| 写操作（publish / yank / 索引面写） | **405** `Remote repository '<key>' is a read-only proxy cache; deployments to remote repositories are not accepted.` + `Allow: GET` |
| `DELETE /binflow/<remote>/<path>` | 缓存驱逐（204/404，不触达上游）——下次 GET 重新回源，见 [缓存管理](../admin/remote-virtual.md#缓存管理与强刷手法) |

search 响应有 TTL 缓存：窗口内上游新发布的 crate 暂不可见于 search（直取索引不受影响）。

## virtual 仓（聚合入口）

把 local 成员与 remote 成员缝成单一 registry 入口。索引面做**行级归并去重**——同一 (name, version) 多成员持有时只保留首见成员的行（Artifactory 裸拼接会产出重复行、违反官方唯一性约束，BinFlow 不采纳）。

### 1. 建仓（写路由指向 local 成员）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/cargo-virtual \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"cargo",
       "repositories":["cargo-local","cargo-remote"],
       "defaultDeploymentRepo":"cargo-local"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

- **经虚仓发布必须配 `defaultDeploymentRepo`**（指向 local 成员；未配置 → 一切写 405 `No local repository was configured as local deployment repository for the (<key>) virtual repository.`）。发布整链换址到该成员执行，虚面立即可见——实测 `cargo publish --registry cvirt` → `Published vloc v0.1.0 at registry `binflow``，仅目标成员索引有行、其余成员零旁漏。

### 2. 实测行为（T-318 + M11 终验 L37）

| 面 | 行为 |
|---|---|
| index 归并 | 按成员序读各成员索引全文，**去重键 = (name, 忽略 build metadata 的 vers)**、首见成员的原始行逐字保留，再按 SemVer 排序输出；`X-Binflow-Resolved-From` = 首见贡献成员 |
| download | first-hit（成员序首个命中者服务：local 成员节点 / remote 成员缓存链），响应头带来源与缓存状态 |
| yank / unyank | first-hit 解析持有成员：**local 持有者**正常翻转（成员与合并行同步 yanked/unyanked）；**remote 持有者 → 405 只读**（缓存副本不可本地标记，上游拥有真相）；未知名/版本 → 404 |
| search | local 事实行 + remote 成员上游行按 crate 名**首见胜**并集；`max_version` 取首见成员行（后成员知更高版本不抬升） |
| 条件请求 | 合并体 `ETag` = 渲染后合并文件 sha256、`Last-Modified` = 最新贡献成员节点时间；`If-None-Match` → 304 |
| DELETE | **不透传成员删除**（405）——删缓存对 remote 成员直接操作；删已发布版本（删后重发）也须对持有成员仓直接操作，见[常见报错对照](#常见报错对照) |

混合构建实测：本地路由发布的 crate + 上游经 remote 合并进来的行，双依赖 `cargo build` 全绿（`Finished `dev` profile`）——合并索引可直接驱动真实 cargo 解析与构建。

## 边界与有意不做（M10/M11）

| 项 | 行为 |
|---|---|
| crates.io 直连上游（index/dl 双主机形态） | 不支持——remote 上游须为单主机 sparse wire 语法（BinFlow / Artifactory cargo 面 / 自建兼容源），见 [remote 仓](#remote-仓pull-through-代理缓存) 一节 |
| git 索引协议（`info/refs` / `git-upload-pack`） | 不做（官方已弃用 sparse 之外的 git 索引），按未知路径 404 + 弃用文案 |
| owners 四端点 | 不做（无用户-所有权模型），按未知路径 404 |
| 手写索引/侧车（bare PUT/DELETE 到 `index/**`、`.cargo/**`） | M11 起接受（PUT 201 / DELETE 204）并触发**索引收敛**：有存储事实的 crate 手写行立即被重算覆盖（cksum 对账闭环由重算保证），无事实的（外部导入形态）手写行保留；`index/config.json` 写仍 **403**（请求时合成面，无落点） |
| 索引条件请求 | 支持 `ETag`（= 文件 sha256）/`Last-Modified` + `If-None-Match`/`If-Modified-Since` → 304 |
| 禁匿名实例 | `config.json` 输出 `"auth-required":true`，cargo 先裸取收到 401 后带凭据重试（官方握手） |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'cargo' is not available ...` | community 档（D3） | 装 pro/enterprise license |
| publish/yank 403 + 响应头 `X-Binflow-License-Required: cargo` | license 过期/卸载后的写门（D2）；**下载/索引读不受影响** | 重装 license |
| publish 401 | 未配 token / token 失效 | `cargo login --registry binflow` 或设 `CARGO_REGISTRIES_BINFLOW_TOKEN` |
| `crate mycrate@0.1.0 already exists on ... index`（cargo 1.98 本地拒绝，PUT 未发） | 客户端预检索引发现重复——**服务端已无 409 冲突臂**（M11 翻转）：可删权限者 PUT 同版本为覆盖上传，但现代 cargo 不发 PUT | **删后重发**（两步、对持有成员仓直接操作——经虚仓 DELETE 恒 405）：`curl -su admin:$ADMIN_PW -X DELETE "$REG/crates/mycrate/mycrate-0.1.0.crate"` 删存储件（204），再 `curl -su admin:$ADMIN_PW -X DELETE "$REG/index/my/cr/mycrate"` 删索引行（204；**顺序勿倒**——有存储事实时索引文件删除会被重算回填）。随后照常 `cargo publish`。或升 `vers` / yank 旧版 |
| publish 命令回 200 但输出 `Failed to publish with error '...'` 警告、随后 `registry may have a backlog` 等索引等待超时 | **CG-2 双轨的 200 臂**：IOException 族失败（元数据/SemVer 校验、帧体截断、落盘、副作用面）答 **200 + `warnings.other` 载荷、无顶层 `errors` 键**——crate 实际未落地（Artifactory 兼容 wire；响应带 `errors` 键 cargo 即判失败，故失败细节走 warnings） | 看 `warnings.other` 里的错误原文处置（如 `version "0.2.0.abc" is not valid SemVer 2.0`），修正后重发 |
| publish 500（`errors` 信封） | 帧形状违例：声明长度超 1MiB 上界 / 空 crate 帧 / 尾随字节 | 用 cargo 客户端发布（勿手拼 wire）；核对 `Cargo.toml` |
| publish 403 `permission denied`（`errors` 信封） | 实名无写权限（含重复版本且调用方不可删的覆盖臂） | 核对权限/换 admin 凭据，或走删后重发 |
| `error: failed to parse manifest ... registry not listed` | `~/.cargo/config.toml` 缺 `[registries.binflow]` 或 `sparse+` 前缀漏写 | 按第 2 步补全配置 |
| 索引 404 但包已发布 | 索引路径档位算错（如 3 字符名走了两档前缀） | 按「索引路径四档」表推导 |
| 下载 404 `unable to download crate` | 版本号拼写不符（须归一 SemVer）或未发布 | 核对 `cargo add` 解析出的版本 |

## 下一步

- 档位与门控语义：[License 与 Add-ons 管理](../admin/license.md)
- 其它包型接入：[Go](golang.md) · [NuGet](nuget.md) · [Maven](maven.md) · [npm](npm.md) · [PyPI](pypi.md)
