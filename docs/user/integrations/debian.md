---
title: Debian（apt）接入
sidebar_position: 30
---

# Debian（apt）接入

> 适用版本：M11（debian 包型为 **pro 档**能力——建仓/上传需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时已有包仍可 `apt-get install`）。
> 验证客户端：**debian:bookworm 容器 + dpkg-deb/apt**（T-310/T-314/T-321：`dpkg-deb --build` 现造真 `.deb` → debPUT → `apt-get update`（apt 自身校验 Release SHA256 节）→ `apt-get install` 装机运行；`signed-by` gpg 校验链 262s 全绿）。行为基准 `docs/reverse/debian.md`。

BinFlow 的 debian 仓 = apt 仓库：`.deb`/`.dsc` 带**坐标矩阵参数**上传（debPUT），服务端解析 control 段并**自动生成** `dists/<dist>/…` 索引族（Packages / Sources / Release / by-hash）。**local / remote（代理）/ virtual（聚合）三类仓型齐备**。

- 仓 URL：`$BASE/binflow/<repoKey>`（apt 源行直接指到仓根）。

## 前置条件

- 运行中的 BinFlow 实例；**pro 及以上** license。
- apt（Debian 12 bookworm 实测）；上传侧 curl。

## local 仓：debPUT、索引生成、装机

### 1. 建仓

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/deb-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"debian"}' -o /dev/null -w '%{http_code}\n'  # 200
```

### 2. debPUT（坐标在矩阵参数里）

```bash
curl -su admin:$ADMIN_PW -T binflow-e2e_1.0-1_amd64.deb \
  "$BASE/binflow/deb-local/pool/main/b/binflow-e2e/binflow-e2e_1.0-1_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64" \
  -o /dev/null -w '%{http_code}\n'      # 201
```

- **三坐标必带**（`deb.distribution` / `deb.component` / `deb.architecture`）——缺失 400（文案附矩阵参数示例），拒绝件不落库。
- 坐标即声明：一个 `.deb` 属于哪个 suite/component/arch 由**上传者**指定（Artifactory 语义）；`pool/…` 路径自由布局。
- `.dsc`（源码包）：缺必填段落（如 `Files:` 校验块）→ 400（Sources 索引的唯一输入，从严）；`.deb` 解析失败则存而不引。
- 上传后**后台增量重算**索引（不阻塞 201）。

### 3. 索引族（自动）

`dists/stable/` 下生成：

- `Release`（Suite/Codename=dist、Components/Architectures（伪架构过滤，`all` 不出现在行内）、`Acquire-By-Hash`（见配置）、三校验节 `%17d` 右对齐）；
- `main/binary-amd64/Packages`（+ `.gz`；stanza = 控制字段原序透传 + 服务端尾字段 `Filename`/`Size`/`MD5sum`/`SHA1`/`SHA256`）；
- `main/binary-i386/`、`main/binary-all/`——**强制架构集默认 i386,amd64**（空家族亦生成空 Packages，Release 的 `Architectures:` 行如实声明；`debianDefaultArchitectures: "none"` 关闭）；
- `main/source/Sources`（仅有 `.dsc` 时）；
- by-hash 镜像（策略开启时，见下）。

常用仓配置 JSON（可选）：

```json
{"byHash": "ALL", "origin": "acme", "label": "acme-deb",
 "debianDefaultArchitectures": "amd64", "historyCycles": 3}
```

`byHash: "ALL"`（MD5+SHA1+SHA256 三算法目录）/ `"SHA256"`（单节）/ 缺省 `NONE`（不出 by-hash）。

### 4. apt 消费（bookworm 容器实测）

```bash
echo "deb [trusted=yes] http://<host>:8080/binflow/deb-local stable main" \
  > /etc/apt/sources.list.d/binflow.list
apt-get update          # apt 自身按 Release SHA256 节校验 Packages.gz —— 通过即证明校验链算术正确
apt-get install -y binflow-e2e
# Status: install ok installed / Version: 1.0-1
```

`[trusted=yes]` 是无签名姿势（开发/内网）；签名姿势见下节。

### 5. reindex（管理面）

```bash
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/deb/reindex/deb-local?async=0"
# Debian index calculation for repository 'deb-local' completed.
```

分支矩阵：`async=1` → 202；virtual/remote 类 → 400（虚仓索引按请求现算、remote 索引属上游，均无存储态可重算）；非 debian 仓 → 400 `Repository '<key>' doesn't handle debian requests.`

## Release 签名（InRelease / Release.gpg + signed-by）

配 keypair 后每次重算（debPUT 自动链或 reindex）产出 `InRelease`（clearsign）+ `Release.gpg`（detached armor）。管理端点族见 [API 参考 · M11 增补速览](../api-reference.md#m11-增补速览t-328)：

```bash
# 1) 生成 keypair 并关联（local debian 仓接受 keyPairName）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/admin/security/keypair/generate \
  -H 'Content-Type: application/json' \
  -d '{"pairName":"deb-signing","alias":"deb","passphrase":"<口令>","keyBits":3072}' \
  -o /dev/null -w '%{http_code}\n'      # 201
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v2/repositories/deb-local/keyPairs \
  -H 'Content-Type: text/plain' -d 'deb-signing' -o /dev/null -w '%{http_code}\n'

# 2) 客户端（bookworm 容器实测）：公钥 dearmor 后入 keyrings，源行带 signed-by
curl -su admin:$ADMIN_PW $BASE/binflow/api/security/keypair/deb-signing \
  | jq -r .publicKey | gpg --dearmor > /usr/share/keyrings/binflow.gpg
echo "deb [signed-by=/usr/share/keyrings/binflow.gpg] http://<host>:8080/binflow/deb-local stable main" \
  > /etc/apt/sources.list.d/binflow.list
apt-get update && apt-get install -y <pkg>
```

- 无 signed-by 且无 trusted=yes 的裸源行 → `apt-get update` 拒绝（`NO_PUBKEY …`）——校验真开启的阴性对照。
- **armored 公钥直用 signed-by 会挂**（`Unknown error executing apt-key`）——务必 `gpg --dearmor` 成 keyring 文件。
- 无钥/钥不可开 → 跳过签名 + 清扫旧 `InRelease`/`Release.gpg`；**`X-GPG-PASSPHRASE` 头不收**。
- **virtual 聚合不签名**：成员签名不覆盖聚合重渲体——虚仓的 `InRelease`/`Release.gpg` 404，apt 官方回退到 Release + `signed-by` 无签名形态（用 trusted=yes 或经 local 仓分发签名）。

## remote 仓（代理上游 apt 源）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/deb-mirror \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"debian","url":"http://deb.debian.org/debian"}' \
  -o /dev/null -w '%{http_code}\n'      # 200

echo "deb [trusted=yes] http://<host>:8080/binflow/deb-mirror stable main" \
  > /etc/apt/sources.list.d/mirror.list
apt-get update && apt-get install -y <pkg>
```

- 上游 URL 钉 **archive-root 姿态**（URL 即源根，请求路径原样拼接——`http://deb.debian.org/debian` 这类形态）。
- `dists/` 元数据短 TTL / `pool/` 制品类长 TTL；PUT → 405；DELETE = 缓存驱逐（204）。

## virtual 仓（聚合）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/deb-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"debian",
       "repositories":["deb-mirror","deb-local2"],
       "defaultDeploymentRepo":"deb-local2"}' -o /dev/null -w '%{http_code}\n'  # 200

echo "deb [trusted=yes] http://<host>:8080/binflow/deb-virt stable main" \
  > /etc/apt/sources.list.d/virt.list
apt-get update && apt-get install -y <成员包>      # local/remote 成员混合装机（实测）
```

- `Release` 按请求**现算重渲**（Components/Architectures = 成员并集；校验节描述虚仓自己渲染的字节——apt 校验通过是交付验收项）；Packages/Sources 家族 stanza 级合并（去重键 = Package+Version+Architecture+下载地址，首见成员胜）。
- 成员索引任意压缩拼写可读（plain/.gz/.bz2/.xz/.lzma 按需探测解压）。
- by-hash 地址与签名族在虚仓上 404（apt 官方回退：正名文件 + Release）。
- 写：debPUT 路由 `defaultDeploymentRepo`，后台重算目标 = 落地成员；索引族直写 403。

## 边界与有意不做（M11）

| 项 | 行为 |
|---|---|
| 索引压缩集 | plain + `.gz` 恒有；`.xz`/`.lzma` 可配（仓配置 JSON）；`.bz2` 暂不渲染（配了该档仅 WARN） |
| 无坐标 `.deb` PUT | **400**（非 Artifactory 的静默存储）——坐标是索引的输入 |
| 索引族写保护 | `dists/**` 生成族 PUT/DELETE → 403（文案含路径 + debPUT 指引）；`dists/stable/README` 等非生成族可写 |
| 虚仓签名/by-hash | 404（apt 回退语义） |
| 空 dist | 组件清零后整树清扫，不留空骨架 Release |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'debian' is not available ...` | community 档 | 装 pro/enterprise license |
| PUT 403 + `X-Binflow-License-Required: debian` | license 过期/卸载后的写门；**下载不受影响** | 重装 license |
| debPUT 400（提示矩阵参数） | 缺 `deb.distribution/component/architecture` 坐标 | URL 尾补 `;deb.distribution=…;deb.component=…;deb.architecture=…` |
| `apt-get update` `NO_PUBKEY …` | 源行无 `signed-by` 也无 `trusted=yes`（校验开启） | 配 keypair + `signed-by`，或内网用 `trusted=yes` |
| `apt-key` / `Unknown error executing apt-key` | armored 公钥直用 `signed-by` | `gpg --dearmor` 成 keyring 文件再用 |
| `apt-get update` 校验失败 | 聚合面签名族 404 后回退姿势不对 / 代理链陈旧 | 虚仓用 trusted=yes；remote 强刷 `DELETE <path>` 后重 update |
| PUT `dists/…` 403 | 索引是服务端生成物 | 用 debPUT / reindex 维护 |

## 下一步

- 三类仓型通用语义：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- keypair 管理端点族：[API 参考 · M11 增补](../api-reference.md#m11-增补速览t-328)
- 同族 OS 包型：[RPM（Yum/DNF）接入](rpm.md)
