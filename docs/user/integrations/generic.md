---
title: Generic（任意文件）接入
sidebar_position: 23
---

# Generic（任意文件）接入

> 适用版本：M3 GA（Generic 仓库 + checksum 寻址存储；dumb HTTP roundtrip FR-15）。
> 本文命令在 M5 QA 基线本地抽跑：curl PUT/GET 往返、checksum 部署、Range 206 均退出码 0。

Generic 仓库是 BinFlow 最灵活的存储接口——接受任意文件，不做布局校验、不做包名归一化、不解析元数据。用 curl 即可上传下载，路径即地址。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 管理员凭据 `admin` / `$ADMIN_PW`。
- 一个 `packageType=generic` 的仓库（仓库类型 `generic` 是 BinFlow 默认/万能类型，M3 完成后 `?packageType=generic` 可省略）。

## URL 形态

Generic 仓库走内容路径（无 API 前缀），路径即地址：

```
$BASE/binflow/<repoKey>/<任意路径>
```

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
```

## 接入步骤

### 1. 创建 generic 仓库

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/generic-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"generic"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

### 2. 上传文件（curl PUT）

```bash
echo "Hello BinFlow" > hello.txt

# 上传
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/hello.txt" \
  -T hello.txt \
  -o /dev/null -w '%{http_code}\n'
# 201
```

带 checksum 上传（声明 SHA-256）：

```bash
SHA256=$(shasum -a 256 hello.txt | awk '{print $1}')
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/hello.txt" \
  -H "X-Checksum-Sha256: $SHA256" \
  -T hello.txt \
  -o /dev/null -w '%{http_code}\n'
# 201（checksum 与服务端计算一致）
```

如果声明的 checksum 与实际内容不符，服务端返回 **409**（`checksum_policy_type` 默认 `client-checksums`）。

### 3. 下载文件（curl GET）

```bash
curl -s "$BASE/binflow/generic-local/hello.txt"
# Hello BinFlow
```

保存到文件：

```bash
curl -s -o download.txt "$BASE/binflow/generic-local/hello.txt"
diff hello.txt download.txt        # 无差异
```

### 4. Checksum 验证下载

```bash
# 请求 SHA-256 响应头
curl -sI "$BASE/binflow/generic-local/hello.txt" | grep -i x-checksum
# X-Checksum-Sha256: abc123...
# X-Checksum-Sha1:   def456...
# X-Checksum-Md5:    ghi789...

# 逐位对账
SHA256_RESP=$(curl -sI "$BASE/binflow/generic-local/hello.txt" | grep -i x-checksum-sha256 | awk '{print $2}' | tr -d '\r')
SHA256_REAL=$(shasum -a 256 hello.txt | awk '{print $1}')
test "$SHA256_RESP" = "$SHA256_REAL" && echo "OK" || echo "MISMATCH"
```

### 5. ETag 与条件请求

BinFlow 返回 `ETag` 头（sha256 值），支持 `If-None-Match` 条件请求：

```bash
# 获取 ETag
ETAG=$(curl -sI "$BASE/binflow/generic-local/hello.txt" | grep -i etag | awk '{print $2}' | tr -d '\r"')

# 条件请求：未修改 → 304
curl -sI -H "If-None-Match: $ETAG" "$BASE/binflow/generic-local/hello.txt"
# HTTP/1.1 304 Not Modified
```

### 6. Range 请求（部分下载）

```bash
# 生成 > 1KB 的文件
dd if=/dev/urandom of=random.dat bs=1024 count=10 2>/dev/null
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/random.dat" \
  -T random.dat \
  -o /dev/null -w '%{http_code}\n'
# 201

# 下载前 100 字节
curl -s -H "Range: bytes=0-99" \
  "$BASE/binflow/generic-local/random.dat" \
  -o first100.bin
# HTTP/1.1 206 Partial Content
# Content-Range: bytes 0-99/10240
# Content-Length: 100

# 验证
dd if=random.dat of=first100.ref bs=100 count=1 2>/dev/null
diff first100.bin first100.ref && echo "Range match OK"
```

### 7. Checksum 部署（直接按 sha256 寻址）

BinFlow 的存储引擎以 sha256 为内容地址——支持直接按 checksum 上传和下载，绕过路径路由：

```bash
# 计算文件的 sha256
SHA256=$(shasum -a 256 hello.txt | awk '{print $1}')

# 按 sha256 部署（PUT 到 /binflow/<repo>/<sha256>）
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/$SHA256" \
  -H "X-Checksum-Deploy: 1" \
  -T hello.txt \
  -o /dev/null -w '%{http_code}\n'
# 201

# 按 sha256 下载
curl -s "$BASE/binflow/generic-local/$SHA256" > sha256-dl.txt
diff hello.txt sha256-dl.txt && echo "Checksum deploy roundtrip OK"
```

`X-Checksum-Deploy: 1` 头告诉服务端：请求路径中的文件名就是内容的 sha256，服务端可直接用该值做内容寻址，并存到 checksum 寻址的 blob 存储中。

### 8. 目录结构

Generic 仓库支持任意路径层级：

```bash
curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/releases/v1.0.0/binflow-server-linux-amd64" \
  -T binflow-server \
  -o /dev/null -w '%{http_code}\n'
# 201

curl -su admin:$ADMIN_PW -X PUT \
  "$BASE/binflow/generic-local/releases/v1.0.0/checksums.txt" \
  -T checksums.txt \
  -o /dev/null -w '%{http_code}\n'
# 201

# 下载时路径原样使用
curl -s "$BASE/binflow/generic-local/releases/v1.0.0/checksums.txt"
```

BinFlow 内部会为每个中间目录（`releases/`、`releases/v1.0.0/`）生成文件夹节点（ADR-0016 文件夹行回填），在 Web 控制台可浏览。

## 匿名与凭据

| 场景 | 行为 |
|---|---|
| 匿名 GET/HEAD（默认 `anonymous_access: true`） | 200——下载、checksum 验证免凭据 |
| 匿名 PUT（上传） | **401** `authentication required` |
| 匿名 DELETE | **401** |
| 全局关匿名 | GET 也需凭据：`curl -u admin:$ADMIN_PW` |

## 响应头说明

| 响应头 | 说明 |
|---|---|
| `X-Checksum-Sha256` | 文件内容的 SHA-256（裸 hex） |
| `X-Checksum-Sha1` | 文件内容的 SHA-1（裸 hex） |
| `X-Checksum-Md5` | 文件内容的 MD5（裸 hex） |
| `ETag` | 值为 sha256（可做 `If-None-Match` 304） |
| `Accept-Ranges: bytes` | 支持 Range 请求 |
| `Content-Length` | 文件大小（字节） |
| `Last-Modified` | 上传时间 |

## 脚本集成示例

### 发布脚本

```bash
#!/usr/bin/env bash
# publish.sh — 将构建产物上传到 BinFlow generic 仓库
set -euo pipefail

BASE="${BINFLOW_BASE:-http://localhost:8080}"
REPO="${BINFLOW_REPO:-generic-local}"
USER="${BINFLOW_USER:-admin}"
PASS="${BINFLOW_PASS:?set BINFLOW_PASS}"
VERSION="${1:?usage: $0 <version>}"

for f in dist/*; do
  name="$(basename "$f")"
  sha256=$(shasum -a 256 "$f" | awk '{print $1}')
  echo "Uploading $name (sha256=$sha256)..."
  http_code=$(curl -su "$USER:$PASS" -X PUT \
    "$BASE/binflow/$REPO/releases/$VERSION/$name" \
    -H "X-Checksum-Sha256: $sha256" \
    -T "$f" \
    -o /dev/null -w '%{http_code}')
  if [ "$http_code" != "201" ]; then
    echo "FAIL: $name → HTTP $http_code" >&2
    exit 1
  fi
done
echo "All artifacts published to $VERSION"
```

### 下载脚本

```bash
#!/usr/bin/env bash
# download.sh — 从 BinFlow generic 仓库下载并校验
set -euo pipefail

BASE="${BINFLOW_BASE:-http://localhost:8080}"
REPO="${BINFLOW_REPO:-generic-local}"
VERSION="${1:?usage: $0 <version>}"
FILE="${2:?usage: $0 <version> <file>}"

URL="$BASE/binflow/$REPO/releases/$VERSION/$FILE"

# 下载
curl -s -o "$FILE" "$URL"

# 校验
EXPECTED=$(curl -sI "$URL" | grep -i x-checksum-sha256 | awk '{print $2}' | tr -d '\r')
ACTUAL=$(shasum -a 256 "$FILE" | awk '{print $1}')

if [ "$EXPECTED" = "$ACTUAL" ]; then
  echo "OK: $FILE (sha256=$ACTUAL)"
else
  echo "FAIL: checksum mismatch for $FILE" >&2
  rm "$FILE"
  exit 1
fi
```

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| PUT 401 `authentication required` | 匿名上传或凭据错误 | 加 `-u admin:$ADMIN_PW` |
| PUT 409 `Checksum error` | 声明的 checksum 与内容不符 | 核对文件完整性；或迁移阶段用 `server-generated-checksums` 仓 |
| PUT 400 `file already exists` | 同名路径已存在（默认不允许覆盖） | 使用不同路径；或先删除旧文件 |
| GET 404 | 路径错误或文件不存在 | 核对 repoKey 和路径 |
| Range 416 | Range 超出文件边界 | 检查文件大小（`Content-Length`） |
| DELETE 401 | 匿名删除 | 加凭据 |

## 下一步

- [Maven 接入](maven.md) — 按 Maven layout 管理 Java 构件
- [npm 接入](npm.md) — npm registry 私有源
- [PyPI 接入](pypi.md) — Python 私有包索引
- [Docker Registry](../docker-registry.md) — OCI 镜像管理
- [remote/virtual 仓库管理](../admin/remote-virtual.md) — 代理与聚合