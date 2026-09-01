---
title: Docker / OCI 镜像接入
sidebar_position: 10
---

# Docker / OCI 镜像接入

> 适用版本：M2（Docker Registry v2 + OCI + Helm OCI 承载；PRD milestone-2 v1.3）；[上传中断续传](#大层上传中断续传跨重启)为 M7 增补（FR-67 / ADR-0028）；[remote 仓（pull-through 代理上游）](#remote-仓pull-through-代理上游m14)为 M14 增补（FR-129）。
> 本文命令在 M2 烟测基线（commit `923db2e`，即 T-44/T-45 验收产物）的 compose 实例上复验，登录/推送/拉取/运行、oras、helm、buildx、podman/crane/skopeo 链路均退出码 0；续传链于 M7 代码（2026-08-23）以 `make test-m7-resume`（kill -9）与 `make test-m7-resume-sigterm`（SIGTERM）双探针 + curl 全链复验；remote 仓链于 M14 代码（2026-08-31）以双实例 + dind 29.7.2 真客户端复验（T-392 首航 + T-397 文档复跑）。

把 BinFlow 当作私有 Docker Registry：`docker login/push/pull` 直连可用，OCI 镜像与 Helm chart 都能存放（chart 以 OCI artifact 形态承载，无需任何 Helm 专有端点）。

## 前置条件

- 一个运行中的 BinFlow 实例（默认 `http://localhost:8080`）。M2 仅提供明文 HTTP（NFR-S6），见下节 insecure-registries 配置。
- 管理员凭据 `admin` / `$ADMIN_PW`（compose 部署时由 `BINFLOW_ADMIN_PASSWORD` 设置）。
- 一个 `packageType=docker` 的本地仓库（第 1 步创建）。

## 连接形态：直连，无路径前缀

docker（及 podman/crane/skopeo/oras）按 Registry spec 硬编码向 `/v2/...` 发请求，**无法配置子路径前缀**。因此 BinFlow 把 docker 端点挂在**根级 `/v2/**`**（ADR-0010）：登录目标就是 `host[:port]`，不带 `http://`、不带路径。

```bash
export BASE=http://localhost:8080   # 管理面 / curl 用（/binflow 前缀）
export REG=localhost:8080           # docker 客户端目标（host[:port]，无 scheme）
export ADMIN_PW=<你的管理员口令>
```

| 部署形态 | `REG` 取值 | insecure-registries |
|---|---|---|
| 本机实例（compose 默认绑 `127.0.0.1`） | `localhost:8080` | **免配**（daemon 默认把 `127.0.0.0/8` 视为 insecure） |
| 远程主机（明文 HTTP） | `registry.internal:8080` | **必配**（见下节） |
| 前置反代 + TLS 域名 | `registry.example.com` | 免配（HTTPS） |

认证对用户无感：`docker login` 一次，后续 push/pull 由客户端自动完成 Bearer 协商——未认证请求会收到 `401 + Www-Authenticate: Bearer realm=".../v2/token"` 挑战，客户端自动携凭据去 realm 换 token，全程无需手工干预（curl 手工协商见[token 说明](#token-说明)）。

## 明文 HTTP 必配：insecure-registries（最高频卡点）

M2 仅明文 HTTP。docker daemon 对**非 localhost** 地址默认强制 HTTPS，不配置时 push/pull 报错：

```
http: server gave HTTP response to HTTPS client
```

三种配置形态（任选其一）：

| 形态 | 做法 |
|---|---|
| 本机 localhost | **零配置**。`REG=localhost:8080` 直接用（daemon 默认 insecure 列表含 `127.0.0.0/8`；T-45 干净环境实证） |
| Docker Desktop / Linux daemon.json | 编辑 daemon 配置加入数组后重启 daemon：Docker Desktop 为 Settings → Docker Engine；Linux 为 `/etc/docker/daemon.json` + `systemctl restart docker` |
| dind（CI 容器内 daemon） | 启动旗标注入，不动宿主配置：`docker run -d --privileged docker:dind --insecure-registry <host:port>` |

daemon.json 内容（两种平台同形）：

```json
{
  "insecure-registries": ["registry.internal:8080"]
}
```

> 提醒：insecure-registries 意味着明文传输。仅在内网/评估环境使用；对外请走前置反代 TLS（见[差异清单](#有意不兼容与差异清单)后的反代片段）。

## 接入步骤

### 1. 创建 docker 仓库

docker 端点不建仓——仓库经管理面 API 创建（复用 M1 仓库 API，`packageType` 取 `docker`）：

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"docker"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

### 2. 登录

```bash
echo "$ADMIN_PW" | docker login $REG -u admin --password-stdin
# Login Succeeded（退出码 0；口令错误则报 unauthorized、退出码 1）
```

### 3. 推送（全名标签形态）

镜像名的**首段是仓库 key，其余段是镜像名**（ADR-0010 name 模型）：

```
<host>/<repoKey>/<image>:<tag>
        ↑          ↑
   docker-local  acme/app（可多级，如 acme/team/app）
```

```bash
mkdir -p demo && cd demo
printf 'FROM alpine:3.20\nCMD ["sh","-c","echo ok"]\n' > Dockerfile
docker build -t $REG/docker-local/acme/app:v1 .
docker push $REG/docker-local/acme/app:v1
# v1: digest: sha256:dafe594c... size: 855（退出码 0）
```

注意：**单段 name（只有 repoKey、无镜像段）→ 404**；仓库不存在或类型不是 docker 同样 404（`NAME_UNKNOWN`）。

### 4. 拉取与运行

```bash
docker rmi $REG/docker-local/acme/app:v1
docker pull $REG/docker-local/acme/app:v1
docker run --rm $REG/docker-local/acme/app:v1      # 输出 ok
```

### 验证

```bash
curl -su admin:$ADMIN_PW $REG/v2/_catalog
# {"repositories":["docker-local/acme/app"]}
curl -su admin:$ADMIN_PW $REG/v2/docker-local/acme/app/tags/list
# {"name":"docker-local/acme/app","tags":["v1"]}
docker manifest inspect --insecure $REG/docker-local/acme/app:v1 | jq -r '.mediaType'
# application/vnd.oci.image.index.v1+json（docker 29 默认推 OCI index）
```

> `docker manifest inspect` 走 CLI 直连（不经 daemon），明文 HTTP 需带 `--insecure`。
> 匿名拉取：`anonymous_access` 默认开——`docker logout` 后 `docker pull` 仍可用（匿名 token 通道）；**push 永远需要认证**。关闭匿名见部署篇 `BINFLOW_SECURITY_ANONYMOUS_ACCESS=false`。

## remote 仓：pull-through 代理上游（M14）

M14 起 docker **local + remote 两态齐装**（virtual 聚合暂不做）：remote 仓代理任一 Registry v2 上游——`docker pull <host>/<remote仓key>/<镜像名>:<tag>` 首拉回源缓存，此后命中本地副本零回源；上游故障时已缓存镜像照常可拉。**community 档即可用**（不占 pro 槽）。

```bash
# 1. 建仓（url 指向上游 distribution 根，含 /v2；公网 registry 形如
#    https://registry-1.docker.io/v2，上游是另一台 BinFlow 时形如
#    http://<上游host>:<port>/v2/<上游仓key>——下例为实测形态）
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/docker-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"docker",
       "url":"http://upstream.example.com:8080/v2/docker-local"}' \
  -o /dev/null -w '%{http_code}\n'        # 200

# 2. 照常 pull——名字里换成 remote 仓 key 即可
docker pull $REG/docker-remote/acme/app:v1
# Digest: sha256:… ← 与直连上游推送的 digest 全等（实测逐位一致）

# 3. 观测命中：docker 客户端不显示响应头，用 curl 看（MISS→HIT→STALE 三态）
curl -s -o /dev/null -D - $REG/v2/docker-remote/acme/app/manifests/v1 \
  -H 'Accept: application/vnd.oci.image.index.v1+json' | grep -i x-binflow
# X-Binflow-Cache: HIT
```

要点（自指上游 + Bearer mock 全链实测，T-392/T-397；Docker Hub 等公网 registry 直连待公网环境验证——Bearer 舞步同构）：二次拉取**上游计数冻结**（零回源）；上游 401 + Bearer challenge 时自动完成 token 交换（舞步恰一轮，token 按 scope 复用）；上游故障 + TTL 过期时已缓存 manifest 答 **`STALE` + `X-Binflow-Upstream-Error`**（`Docker-Content-Digest` 不变，客户端无感），未缓存 ref 答 404 `MANIFEST_UNKNOWN`（带上游摘要，零 5xx）；上游在私网/本机需 admin 放行 `allowPrivateUpstream`。完整语义（URL 形态/降级矩阵/SSRF/dind 调试注记）见 [remote/virtual 管理指南 · docker remote 仓](admin/remote-virtual.md#docker-remote-仓m14fr-129)。

## 多架构镜像（buildx）

`docker-container` driver 的 builder 是独立容器，**不读宿主 daemon 的 insecure-registries**，也**不共享宿主的登录态来源**——plain-HTTP 上游必须给 builder 级配置，且推送前宿主先 `docker login`（两处缺一 push 报 `unauthorized`）：

```bash
cat > buildkitd.toml <<'EOF'
[registry."localhost:8080"]
  http = true                      # plain-HTTP 上游（builder 容器独立于 daemon 配置）
EOF
docker buildx create --name binflow --driver docker-container --buildkitd-config buildkitd.toml
docker buildx build --builder binflow \
  --platform linux/amd64,linux/arm64 \
  -t $REG/docker-local/acme/multi:1 --push .
# pushing manifest for .../multi:1@sha256:4db02f1f... done
docker buildx rm binflow           # 不再需要时清理 builder
docker pull $REG/docker-local/acme/multi:1 && docker run --rm $REG/docker-local/acme/multi:1
```

## Helm chart 承载（oras 与 helm 客户端）

chart 以 OCI artifact 形态存放：chart tgz 作 layer、helm config 作 manifest config（**config 类型不可作 layer**——helm 客户端按 layer 媒体类型找 chart，用错则报 `manifest does not contain a layer with mediatype ...helm.chart.content.v1.tar+gzip`）。

### oras（推荐客户端）

```bash
# 准备：chart.json（元数据）与 chart 归档 myapp-1.0.0.tgz
printf '{"name":"myapp","version":"1.0.0","description":"demo"}' > chart.json

oras login --plain-http $REG -u admin -p "$ADMIN_PW"          # Login Succeeded
oras push --plain-http $REG/charts/myapp:1.0.0 \
  --config chart.json:application/vnd.cncf.helm.config.v1+json \
  myapp-1.0.0.tgz:application/vnd.cncf.helm.chart.content.v1.tar+gzip
mkdir pull-out && cd pull-out
oras pull --plain-http $REG/charts/myapp:1.0.0
cmp ../myapp-1.0.0.tgz myapp-1.0.0.tgz && echo IDENTICAL       # 逐位一致
cd .. && oras repo tags --plain-http $REG/charts/myapp         # 1.0.0
```

> oras v1.2 的路径校验拒绝绝对路径（`absolute file path detected`）——在工作目录内用相对路径引用文件。

### helm 客户端（push/pull/show）

helm 3.16/3.17 的 `registry login` **没有** `--plain-http` 旗标（push/pull/show 有），明文 HTTP 下无法常规登录。绕过方案：直接写 helm 的凭据文件（Docker config.json 格式，路径见 `helm env HELM_REGISTRY_CONFIG`）：

```bash
mkdir -p chart && printf 'apiVersion: v2\nname: myapp\nversion: 1.0.0\n' > chart/Chart.yaml
helm package chart/                    # 生成 myapp-1.0.0.tgz

# 凭据文件方案（明文 HTTP 下替代 helm registry login）：
mkdir -p ~/.config/helm/registry
printf '{"auths":{"%s":{"auth":"%s"}}}' "$REG" \
  "$(printf 'admin:%s' "$ADMIN_PW" | base64)" \
  > ~/.config/helm/registry/config.json

helm push --plain-http myapp-1.0.0.tgz oci://$REG/charts
# Pushed: <REG>/charts/myapp:1.0.0（digest sha256:...）
helm pull --plain-http oci://$REG/charts/myapp --version 1.0.0
helm show chart --plain-http oci://$REG/charts/myapp --version 1.0.0
```

## 其它客户端（均已实测）

| 客户端 | 明文 HTTP 旗标 | 示例 |
|---|---|---|
| podman 5.x | `--tls-verify=false`（login/pull/push 各自带） | `podman login --tls-verify=false $REG -u admin`；`podman pull/push --tls-verify=false ...` |
| crane | `--insecure`（每个子命令自带） | `crane digest --insecure $REG/docker-local/acme/app:v1`；写操作先 `crane auth login $REG -u admin -p <pw>`（匿名 digest/ls 可直接用） |
| skopeo | inspect 用 `--tls-verify=false`；copy 用 `--src-tls-verify=false` / `--dest-tls-verify=false` | 见下 |

```bash
# crane：实例内两 repo 间复制（digest 逐一致）
crane auth login $REG -u admin -p "$ADMIN_PW"
crane copy --insecure $REG/docker-local/acme/app:v1 $REG/docker-local/copy/app:v1

# skopeo：体检与搬运（inspect/copy 拉取方向）
skopeo inspect --tls-verify=false docker://$REG/docker-local/acme/app:v1 | jq .Digest
skopeo copy --src-tls-verify=false docker://$REG/docker-local/acme/app:v1 docker-archive:app.tar
# skopeo 反向推送（docker-archive → 仓库）
skopeo copy --dest-tls-verify=false --dest-creds admin:$ADMIN_PW \
  docker-archive:app.tar docker://$REG/docker-local/skopeo/app:v1
```

## token 说明

`docker login` 触发的 token 流（distribution token 协议）：

1. 未认证请求 `/v2/**` → `401` + `Www-Authenticate: Bearer realm="<BASE>/v2/token",service="binflow"`（scope 由端点推导：读 `pull`、写 `pull,push`、删 `pull,delete`、catalog `registry:catalog:*`）；
2. 客户端携 Basic 凭据请求 realm → `200 {"token","access_token"（同值）,"expires_in","issued_at"}`；
3. 后续请求带 `Authorization: Bearer <token>`，权限逐请求按用户 ACL 判定。

要点：

- **TTL**：`expires_in` 默认 `2592000` 秒（= 720 小时 / 30 天）；compose 用 `BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS` 调整（对管理面与 docker token 同时生效）。
- **无 refresh**：不返回 `refresh_token`，token 过期后重新 `docker login` 即可；`refresh_token` grant 返回 `unsupported_grant_type`。`offline_token=true` 参数接受并忽略（docker daemon 登录必带，服务端不因此报错）。
- **吊销**：docker token 与管理面 token 同表同管——经管理面吊销后，该 Bearer 立即 401。
- **双入口并存**：`GET/POST /v2/token` 是 docker login 语义入口，**任意有效用户**可换取自身 scope 的 token；管理面 `POST /binflow/api/security/token` 是 admin-only 的 token 管理入口（签发/按 id 吊销）。

手工协商（脚本/curl 场景）：

```bash
TOKEN=$(curl -su admin:$ADMIN_PW \
  "$BASE/v2/token?service=binflow&scope=repository:docker-local/acme/app:pull,push" | jq -r .token)
curl -s -H "Authorization: Bearer $TOKEN" $REG/v2/_catalog   # 200
```

## 大层上传中断续传（跨重启）

M7 起，**本地 filestore 后端**的 docker blob 上传会话跨进程重启存活——传到一半遇到服务器重启，客户端向 upload URL 查询状态即可拿到**权威 offset** 从断点续传，不必从零重传。

适用口径（ADR-0028）：

- **三径对称**：`kill -9`（崩溃/断电）、SIGTERM（优雅停机）、`docker compose restart` 三种重启形态**一致可续传**——计划内维护窗口不打断在途大上传，「跨重启续传」不限于异常中断。
- **唯一回收路径**：上传会话的清除只有「启动 sweep + TTL 过期」一条路（默认 24h）。优雅停机会在日志打一条保留清单 INFO（`storage: close: preserving unexpired upload sessions ...`），在途数据文件原样保留到 TTL。
- **范围**：仅本地 filestore 后端。**S3 后端不适用**——multipart 状态由 S3 服务端持有，重启后旧 upload URL 一律 404 `BLOB_UPLOAD_UNKNOWN`（Q4 暂行不纳入）。generic/maven/npm/pypi 的上传是单体 PUT，无分块会话，也就没有续传面。
- **docker CLI 注记**：docker CLI 自身在中断后**从头重传**（客户端不实现续传逻辑），`docker push` 行为不变。规格级续传面向探针与自管传输脚本——按 Docker Registry HTTP API v2 的上传状态查询 + `Content-Range` 锚点用 curl（或任意实现续传的客户端）完成，形态如下。

完整续传链（每步状态码右侧为实测值）：

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>

# 0. 发起上传会话 → 202 + Location
LOC=$(curl -su admin:$ADMIN_PW -X POST $BASE/v2/docker-local/myimg/blobs/uploads/ \
  -o /dev/null -D - | awk -F': ' 'tolower($1)=="location"{sub(/\r/,"",$2);print $2}')
# LOC=/v2/docker-local/myimg/blobs/uploads/<uuid>

# 1. 传首块（512KiB）→ 202 + Range: 0-524287
head -c 524288 /dev/urandom > /tmp/chunk1
curl -su admin:$ADMIN_PW -X PATCH -H 'Content-Type: application/octet-stream' \
  --data-binary @/tmp/chunk1 "$BASE$LOC" -o /dev/null -D - | grep -i '^range:'
# Range: 0-524287

# 2. —— 此刻服务器重启（kill -9 / SIGTERM / docker compose restart 任一形态）——

# 3. 查询上传状态 → 204 No Content + 权威 Range（= 已收字节数，断点锚点）
curl -su admin:$ADMIN_PW "$BASE$LOC" -o /dev/null -D - | grep -iE '^(HTTP|range)'
# HTTP/1.1 204 No Content
# Range: 0-524287

# 4.（演示）起点错位——重放首块 Content-Range → 416 + 空 body + 权威 Range（会话不受影响）
curl -su admin:$ADMIN_PW -X PATCH -H 'Content-Type: application/octet-stream' \
  -H 'Content-Range: 0-65535' --data-binary @/tmp/chunk1 "$BASE$LOC" \
  -o /dev/null -D - | grep -iE '^(HTTP|content-length|range)'
# HTTP/1.1 416 Requested Range Not Satisfiable
# Content-Length: 0
# Range: 0-524287        ← 以此为锚续传，不要猜

# 5. 从权威 offset 续传剩余块（Content-Range 起点 == 已收字节数）→ 202
head -c 65536 /dev/urandom > /tmp/chunk2
curl -su admin:$ADMIN_PW -X PATCH -H 'Content-Type: application/octet-stream' \
  -H 'Content-Range: 524288-589823' --data-binary @/tmp/chunk2 "$BASE$LOC" \
  -o /dev/null -w '%{http_code}\n'                                    # 202

# 6. PUT 收尾（digest 校验）→ 201
DIG="sha256:$(cat /tmp/chunk1 /tmp/chunk2 | openssl dgst -sha256 -r | cut -d' ' -f1)"
curl -su admin:$ADMIN_PW -X PUT "$BASE$LOC?digest=$DIG" -o /dev/null -w '%{http_code}\n'   # 201

# 7. 回读逐位校验
curl -su admin:$ADMIN_PW "$BASE/v2/docker-local/myimg/blobs/$DIG" -o /tmp/got.bin
cat /tmp/chunk1 /tmp/chunk2 | cmp - /tmp/got.bin && echo IDENTICAL
```

要点与边界：

| 情形 | 响应 | 说明 |
|---|---|---|
| GET upload URL（未过期） | **204 + `Range: 0-<offset-1>`** | offset 为服务端权威已收字节数 |
| PATCH `Content-Range` 起点 ≠ 已收字节 | **416 + 空 body + `Range` 权威值** | 客户端应改用响应里的 Range 重锚；会话本身存活 |
| PUT `?digest=` 校验失败 | 400 | 会话保留，可修正后重试 |
| 会话过期（默认 24h）或行不存在 | 404 `BLOB_UPLOAD_UNKNOWN` | 唯一「作废」形态；重启后过期行由启动 sweep 清行清目录 |
| S3 后端（任何时刻的重启后） | 404 `BLOB_UPLOAD_UNKNOWN` | multipart 状态在 S3 服务端；见上文范围说明 |

## 有意不兼容与差异清单

与 Artifactory 对接过的用户注意以下差异（前四条为 BinFlow 有意设计，来源 PRD/ADR）：

| 行为 | BinFlow | Artifactory | 依据 |
|---|---|---|---|
| `DELETE /v2/<name>/blobs/<digest>` | **405 `UNSUPPORTED`**——blob 物理删除唯一入口是 GC | 支持 blob 删除 | DE-14；存储安全底线 |
| `DELETE /v2/<name>/manifests/<tag>` | **405 `UNSUPPORTED`**——官方 spec 禁止 by-tag 删除；tag 的「删除」由覆盖 push 或 by-digest 删除级联实现 | 支持 by-tag 删除 | FR-9-AC7 / DE-10 |
| `GET /v2/<name>/referrers/` | **404**——OCI referrers API 不做（manifest PUT 的 `OCI-Subject` 响应头仍会返回，oras 等客户端探测后安全回退） | — | DE-15 |
| 创建已存在的用户（`PUT /binflow/api/security/users/{name}`） | **409** `The user already exists: <name>`（拒绝重复建，自有语义） | 覆盖并返回 201 | M1 E-19 归档 |
| 空 tag 集的 `tags/list` | **200 且 `"tags":null`**（jq 断言写 `.tags == null`） | 404 `NO_TAGS_FOUND` | DE-12（对齐 OCI conformance） |

另有两条口径值得知道：未认证 `GET /v2/` **一律 401 挑战**（不随匿名开关变化，匿名拉取经匿名 token 通行——与 Docker Hub/GHCR/Harbor 同构）；manifest PUT 的 `Content-Type` **透传不白名单**（前瞻兼容 helm/attestation 等新类型）。

## 前置反向代理（可选）

compose/单二进制**不需要反代**即可 docker push（根级 `/v2` 在应用内实现）。已有 nginx/traefik 前置时，`/v2/` 必须**原样直通、不要 rewrite**——挑战头里的 `realm` 指向 BinFlow 自身 `/v2/token`，任何路径改写都会打断 token 协商。片段引自 `deploy/dev/README.md`：

```nginx
# nginx：TLS 终结 + /v2/ 与 /binflow/ 全直通（无 rewrite）
server {
    listen 443 ssl;
    server_name registry.example.com;
    # ssl_certificate ...; ssl_certificate_key ...;
    client_max_body_size 0;              # 大层上传不被 nginx 截断（无上限）

    location /v2/ {                      # docker Registry API（根级例外，ADR-0010）
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_request_buffering off;     # 流式转发 blob 上传
        proxy_buffering off;             # 流式下发 blob
    }
    location /binflow/ {                 # 管理面 API + 控制台 + 通用制品路径
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        client_max_body_size 0;
    }
}
```

```yaml
# traefik（file provider 或容器 labels）
http:
  routers:
    binflow-v2:
      rule: "PathPrefix(`/v2/`)"          # 直通，不 strip 前缀
      service: binflow
      tls: {}
    binflow-api:
      rule: "PathPrefix(`/binflow/`)"
      service: binflow
      tls: {}
  services:
    binflow:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8080"
```

经反代域名访问时仅 `REG` 取值变化（`REG=registry.example.com`，HTTPS 下无需 insecure-registries），命令本体不变。

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| `http: server gave HTTP response to HTTPS client` | daemon 未把目标列入 insecure-registries | 按[三形态](#明文-http-必配insecure-registries最高频卡点)配置并重启 daemon；localhost 目标无此问题 |
| login 报 `unauthorized: authentication required`（退出码 1） | 口令错误 | 核对口令；M2 起错口令登录确定失败（不会被 ping 放行） |
| push/pull 报 `name unknown` / 404 | 单段 name，或仓库未建 / 非 docker 类型 | 用全名 `<repoKey>/<image>`；先经管理 API 建 `packageType=docker` 仓库 |
| curl `GET manifests/<ref>` 404（带具体 Accept 类型） | Accept 协商：存储类型（如 OCI index）不在 Accept 列表 | Accept 加对应类型或 `*/*`；docker 客户端自带全列表不受影响 |
| buildx push `unauthorized` | builder 缺登录态或缺 `http = true` 配置 | 宿主先 `docker login`；`buildkitd.toml` 配 `[registry."<REG>"] http = true` |
| oras `absolute file path detected` | oras v1.2 路径校验拒绝绝对路径 | 工作目录内改用相对路径引用文件 |
| helm push 401 | 明文 HTTP 下 `helm registry login` 不可用（无 `--plain-http`） | 写 `HELM_REGISTRY_CONFIG` 凭据文件（见上文 helm 节） |
| PATCH upload 报 416（`Content-Length: 0`） | `Content-Range` 起点 ≠ 服务端已收字节数（错位重放） | 读响应里的 `Range` 头取权威 offset，从 `Range` 末尾 +1 处重锚续传 |
| 重启后 GET/PATCH upload URL 报 404 `BLOB_UPLOAD_UNKNOWN` | 会话过期（>24h TTL）、已被收尾/取消，或 **S3 后端**（multipart 状态在 S3 服务端，BinFlow 侧不续传） | 本地 filestore 下未过期会话不会 404（见[续传](#大层上传中断续传跨重启)）；S3 后端请整块重传 |
| dind 内 pull 一直卡住、BinFlow 侧零到达日志 | dind 29.x 默认 containerd snapshotter 对 plain-HTTP registry 的 blob 取数走 https 回退，不遵守 `--insecure-registry` | dind 启动加 `--feature containerd-snapshotter=false` 回经典 overlay2（见[remote 仓 dind 注记](#remote-仓pull-through-代理上游m14)与 `web/e2e/README.md`） |
| dind（macOS Docker Desktop）拉 ~MB 级层卡死、服务端 goroutine 停在写响应 | dind↔宿主方向大包 PMTU 黑洞（环境网络症，非产品面） | dind 内对 OUTPUT 与 INPUT 各加一条 `iptables -t mangle -A … -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss 1300` 后开新连接（T-392 环境注记） |
| remote 仓 pull 报 404 带 `ssrf-guard` 摘要 | remote 仓 `url` 指向私网/环回地址，被 SSRF 防护拒绝 | 内网上游由 admin 配 `allowPrivateUpstream: true`（见[管理指南](admin/remote-virtual.md#ssrf-防护与-allowprivateupstream-放行指引)） |

## 下一步

- 部署形态与端口/持久化调整：[Docker 运行](install/docker.md)、[docker-compose 部署](install/compose.md)（开发/评估用 compose 形态另见仓库内 `deploy/dev/README.md`，非文档站页面）
- 管理面 API（建仓/用户/token 吊销）：API 参考篇（随里程碑补齐）
- 从 Artifactory 迁移的概念对照：[faq.md](faq.md)
