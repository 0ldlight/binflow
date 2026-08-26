---
title: Cargo（Rust）接入
sidebar_position: 26
---

# Cargo（Rust）接入

> 适用版本：M10（cargo 包型为 **pro 档**能力——建仓/发布需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有 crate 仍可下载/构建）。协议行为基准：The Cargo Book「Registry Index」「Registry Web API」官方规范；逆向规格 `docs/reverse/cargo.md`。
> 验证客户端：**cargo 1.98**（1.98.0 实测，T-294 L-r1~L-r6：publish / add / build / yank / search 全链 + cksum 对账）。规格锚 1.8x，新版更强。

BinFlow 作为 **sparse HTTP registry**（crates.io 兼容）：`.cargo/config.toml` 注册一个 alternate registry，`cargo publish` 发内部 crate、`cargo add/build` 消费；索引与下载地址由 `config.json` 自述，cargo 全自动协商。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 实例已装 **pro 及以上** license（community 实例建 cargo 仓得 400 `package type 'cargo' is not available ...`）。
- cargo 1.70+（sparse registry 默认支持；实测锚 1.98）。
- M10 为 **local 仓全量**：remote（sparse pull-through）与 virtual 归 M11——建 remote/virtual cargo 仓暂不可用（对相应请求面按未实现 404 应答）。

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
- 校验链：crate 名 = 首字符字母 + `[A-Za-z0-9_-]`、≤64；`vers` 严格 SemVer 2.0（前导零拒绝）；**同 name+version（忽略 build metadata）重复发布 → 409**（crates.io 官方 must，重传覆盖不可用）。
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

## 边界与有意不做（M10）

| 项 | 行为 |
|---|---|
| remote / virtual cargo 仓 | M11 单票；当前建仓不可用（声明式 local-only） |
| git 索引协议（`info/refs` / `git-upload-pack`） | 不做（官方已弃用 sparse 之外的 git 索引），按未知路径 404 + 弃用文案 |
| owners 四端点 | 不做（无用户-所有权模型），按未知路径 404 |
| 手写索引/侧车（bare PUT 到 `index/**`、`.cargo/**`） | **403**——手写会破坏 cksum 对账闭环；其它路径 bare PUT 照常 201（外部导入入口，经 yank 触发索引重写可回填行） |
| 索引条件请求 | 支持 `ETag`（= 文件 sha256）/`Last-Modified` + `If-None-Match`/`If-Modified-Since` → 304 |
| 禁匿名实例 | `config.json` 输出 `"auth-required":true`，cargo 先裸取收到 401 后带凭据重试（官方握手） |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'cargo' is not available ...` | community 档（D3） | 装 pro/enterprise license |
| publish/yank 403 + 响应头 `X-Binflow-License-Required: cargo` | license 过期/卸载后的写门（D2）；**下载/索引读不受影响** | 重装 license |
| publish 401 | 未配 token / token 失效 | `cargo login --registry binflow` 或设 `CARGO_REGISTRIES_BINFLOW_TOKEN` |
| publish 409 | 同 name+version 已发布（版本不可变，忽略 build metadata） | 升 `vers`；yank 而非重传 |
| publish 400（解帧/元数据） | wire 截断、长度错位、crate 名或 SemVer 不合法 | 用 cargo 客户端发布（勿手拼 wire）；核对 `Cargo.toml` |
| `the remote server responded with an error:`（空 detail） | 客户端把响应体的 `errors` 键判为失败 | 看服务端日志定位具体 4xx/5xx（BinFlow 不用 200+errors 双轨） |
| `error: failed to parse manifest ... registry not listed` | `~/.cargo/config.toml` 缺 `[registries.binflow]` 或 `sparse+` 前缀漏写 | 按第 2 步补全配置 |
| 索引 404 但包已发布 | 索引路径档位算错（如 3 字符名走了两档前缀） | 按「索引路径四档」表推导 |
| 下载 404 `unable to download crate` | 版本号拼写不符（须归一 SemVer）或未发布 | 核对 `cargo add` 解析出的版本 |

## 下一步

- 档位与门控语义：[License 与 Add-ons 管理](../admin/license.md)
- 其它包型接入：[Go](golang.md) · [NuGet](nuget.md) · [Maven](maven.md) · [npm](npm.md) · [PyPI](pypi.md)
