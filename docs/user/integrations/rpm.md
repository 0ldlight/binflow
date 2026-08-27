---
title: RPM（Yum/DNF）接入
sidebar_position: 29
---

# RPM（Yum/DNF）接入

> 适用版本：M11（rpm 包型为 **pro 档**能力——建仓/上传需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有包仍可 `dnf install`）。
> 验证客户端：**Rocky Linux 9 容器 + dnf**（T-311/T-315/T-322：rpmbuild 现造真包 → PUT → reindex → `dnf makecache` / `repoquery` / `install` / `rpm -q` 全链；`repo_gpgcheck=1` 的 gpg 签名链三腿）。行为基准 `docs/reverse/rpm.md`。
> 摘要算法：BinFlow 一律 **SHA-256**（索引文件名摘要、repomd `checksum type="sha256"`、primary `pkgid` 三处一致——有意与 Artifactory 默认 SHA-1 不同）。

BinFlow 的 rpm 仓 = YUM 仓库：`.rpm` 上传（header 解析登记 `rpm.metadata.*` 属性）+ **repodata 引擎**（primary / filelists / other 三索引 + repomd.xml）。**local / remote（代理）/ virtual（聚合）三类仓型齐备**。

- 仓 URL：`$BASE/binflow/<repoKey>`（repodata 在 `<repoKey>/repodata/`）。

## 前置条件

- 运行中的 BinFlow 实例；**pro 及以上** license。
- dnf（RHEL 系 8/9 实测）或 yum；上传侧 curl 或 `dnf` 配 `baseurl` + curl。

## local 仓：上传、生成 repodata、装机

### 1. 建仓

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/rpm-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"rpm"}' -o /dev/null -w '%{http_code}\n'   # 200
```

### 2. 上传 .rpm

```bash
curl -su admin:$ADMIN_PW -T binflow-e2e-1.0-1.el9.noarch.rpm \
  $BASE/binflow/rpm-local/binflow-e2e-1.0-1.el9.noarch.rpm -o /dev/null -w '%{http_code}\n'
# 201 —— 服务端解析 header 登记 rpm.metadata.*（name/version/release/arch/依赖六组等）
```

> **默认不自动重算 repodata**（`calculateYumMetadata` 默认 false）：PUT 只存储 + 登记属性；显式触发 reindex 或仓配置开启后台自动计算（`{"calculateYumMetadata": true}`）才生成。上传后客户端看不到包，先想这条。

### 3. 触发 reindex

```bash
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/yum/rpm-local?async=0"
# YUM metadata calculation for repository 'rpm-local' completed.
```

| 分支 | 状态码 | 说明 |
|---|---|---|
| `async=0`（默认，同步） | 200 | 跑完返回 |
| `async=1`（异步） | 202 `... accepted.` | 后台执行 |
| 仓开自动计算时 `async=0` | 409 | 拒绝在 auto-async 仓上跑同步计算 |
| virtual 仓 | 200/202 | 触发聚合重合并（`path` 参数自动补 `/repodata` 后缀） |
| remote 仓 | 400 | remote 原样镜像上游 repodata，无本地可重算 |

生成物：`repodata/repomd.xml` + `<sha256>-primary.xml.gz` / `-filelists.xml.gz` / `-other.xml.gz`（命名 = 内容摘要）；`comps.xml` 组文件可上传（自动改名 `<digest>-comps.xml` 入 repomd）；**3 代保留**（重算不堆叠）；`modules.yaml` 透传（用户上传拼写确定性 gzip 后登记 repomd `type=modules` 条目）。

### 4. dnf 消费（Rocky 9 容器实测）

```bash
dnf config-manager setopt binflow.baseurl=$BASE/binflow/rpm-local 2>/dev/null || \
cat > /etc/yum.repos.d/binflow.repo <<'EOF'
[binflow]
name=BinFlow
baseurl=http://<host>:8080/binflow/rpm-local
enabled=1
gpgcheck=0
repo_gpgcheck=0
EOF

dnf clean all && dnf makecache          # repomd → primary → .rpm 全链
dnf repoquery --repo binflow binflow-e2e
# binflow-e2e-0:1.0-1.el9.noarch        （NEVRA 出现）
dnf install -y binflow-e2e              # Complete!
rpm -q binflow-e2e
```

## GPG 元数据签名（repomd 签名 + gpgcheck）

为 repomd 配 keypair 后，reindex 产出两个固定名文件（keypair 生成/导入/关联的管理端点族见 [API 参考 · M11 增补速览](../api-reference.md#m11-增补速览t-328)）：

```bash
# 1) 生成/导入 keypair 并关联到仓（local rpm 仓接受 keyPairName；virtual/remote 不接受）
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v1/admin/security/keypair/generate \
  -H 'Content-Type: application/json' \
  -d '{"pairName":"rpm-signing","alias":"rpm","passphrase":"<口令>","keyBits":3072}' \
  -o /dev/null -w '%{http_code}\n'      # 201
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/v2/repositories/rpm-local/keyPairs \
  -H 'Content-Type: text/plain' -d 'rpm-signing' -o /dev/null -w '%{http_code}\n'

# 2) 重算 → repodata/repomd.xml.asc（detached 签名）+ repomd.xml.key（armored 公钥）
curl -su admin:$ADMIN_PW -X POST "$BASE/binflow/api/yum/rpm-local?async=0" -o /dev/null

# 3) 客户端开启元数据验签（rockylinux:9 实测）
curl -s -o /etc/pki/rpm-gpg/binflow.asc $BASE/binflow/rpm-local/repodata/repomd.xml.key
rpm --import /etc/pki/rpm-gpg/binflow.asc
cat > /etc/yum.repos.d/binflow.repo <<'EOF'
[binflow]
baseurl=http://<host>:8080/binflow/rpm-local
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/binflow.asc
EOF
dnf makecache -y && dnf install -y binflow-e2e   # 验签通过 → Complete!
```

要点：

- 未导入公钥时 `dnf makecache` 直接拒（`Failed to retrieve GPG key …`）——先 `rpm --import`。
- **`X-GPG-PASSPHRASE` 头不收**：口令随 keypair 行密封，签名时解封即弃。
- 无钥/钥不可开 → 跳过签名 + 清扫旧签名文件（防旧签名验新 repomd）。
- **virtual 聚合不签名**：成员签名对合并 repomd 必验不过——虚仓的 `.asc`/`.key` 恒 404。

## remote 仓（代理上游 YUM 仓）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/rpm-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"rpm","url":"https://mirrors.example.com/pub/el9"}' \
  -o /dev/null -w '%{http_code}\n'      # 200
```

- repodata / `.rpm` 全走 pull-through；**上游 repodata 原样缓存、不重算**。
- 落地副本异步解析 header 回填 `rpm.metadata.*`（搜索面可用）。
- PUT → 405；DELETE = 缓存驱逐（204/404），重取回源。

## virtual 仓（聚合）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/rpm-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"rpm",
       "repositories":["rpm-l2","rpm-remote"]}' -o /dev/null -w '%{http_code}\n'  # 200
```

- `repomd.xml` 聚合：成员序合并 primary/filelists/other（按 `<name>/<arch>` 对优先成员去重）+ modules/updateinfo（仅非优先成员收集）+ group；摘要前缀索引以新 digest 命名。聚合缓存进程内 TTL 30s（成员**清单**变更即时失效，内容变化 ≤TTL）。
- `.rpm` 下载两桶序首命中；PUT 路由 `defaultDeploymentRepo`（解析与 RP-2 重算指向落地成员），未配置 → 405。
- dnf 消费形态与 local 相同（`dnf makecache` 拉聚合 repomd → `repoquery` 双成员包同现 → install，实测）。

## 边界与有意不做（M11）

| 项 | 行为 |
|---|---|
| repodata 自动计算 | **默认 false**（PUT 只存）——reindex 或仓配置 opt-in |
| repodata 生成族写保护 | `repomd.xml` / 摘要前缀索引 / sqlite 的 PUT/DELETE → 403 |
| `.rpm` 校验和旁车（`.sha1` 等） | GET → 404（校验和经响应头 `X-Checksum-*` 提供） |
| 虚仓聚合签名 | 不做（404）——成员签名对合并 repomd 必验不过 |
| legacy sqlite 索引 | 不生成（dnf 5 默认不取；老客户端按普通文件 404） |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'rpm' is not available ...` | community 档 | 装 pro/enterprise license |
| PUT 403 + `X-Binflow-License-Required: rpm` | license 过期/卸载后的写门；**下载不受影响** | 重装 license |
| 上传成功但 `dnf repoquery` 空 | repodata 未重算（默认不自动） | `POST /api/yum/<repo>?async=0` |
| reindex 409 `auto-async calculation enabled` | 仓开了自动计算又请求同步 | 去 `?async=1` 或关掉仓配置开关 |
| `dnf makecache` 报 `Failed to retrieve GPG key` | `repo_gpgcheck=1` 但公钥未导入 | `rpm --import` 仓上 `repomd.xml.key` |
| PUT repomd.xml 403 | 生成族写保护 | 走 reindex |
| 无 gpgkey 需求却验签失败 | `repo_gpgcheck=1` 但仓无 keypair | 关 `repo_gpgcheck` 或配 keypair |

## 下一步

- 三类仓型通用语义：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- keypair 管理端点族：[API 参考 · M11 增补](../api-reference.md#m11-增补速览t-328)
- 同族 OS 包型：[Debian（apt）接入](debian.md)
