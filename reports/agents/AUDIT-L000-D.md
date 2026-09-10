# AUDIT-L000-D — BinFlow UAT 立起（LOOP 000 轨道 D）

Ticket:        AUDIT-L000-D · BinFlow UAT 立起（宪章差分/UAT 腿平台前置）· P0
Role:          devops-engineer（本票豁免：deploy/ 立起 + reports/agents/ 写入；不改核心代码、不 commit）
Area:          UAT 实例部署（compose 运行面 + release 镜像构建面），无 CI workflow 改动
Input:         conductor 派发（目标 :8083、develop HEAD 59f33ab5、隔离 ./data）；自读 deploy/compose/docker-compose.yml、deploy/README.md、deploy/ci/README.md、deploy/dev/README.md、deploy/release/build-release.sh、.goreleaser.yaml、Makefile、internal/httpapi/router.go
Changes:       ① 建 deploy/compose/.env.uat（UAT 专用 env，gitignored）② 镜像走 prebuilt 注入链构建（make console/docs/release → build-release.sh）③ compose 项目 binflow-uat 立起（隔离具名卷）④ smoke 全套实测 ⑤ 发现并登记 Dockerfile 源构建缺陷（见 Risks）
Files:         deploy/compose/.env.uat（新增，gitignored）、reports/agents/AUDIT-L000-D.md（本文件）；无其他改动、无 commit
Tests:         见下「自测证据」——readyz/healthz/version/ping/console/docs 200、匿名写 401 + admin 写 200、docker login/push/pull digest 一致、重启持久化
Commands:      见下「启停命令」与「自测证据」段（可复制重放）
Outputs:       运行实例 http://localhost:8083（容器 binflow-ga，镜像 binflow:uat-59f33ab5-alpine，linux/amd64）
Compatibility: 无闸门链改动；本票为平台前置。发现的 Dockerfile 缺陷（源构建路径断）建议转 release-engineer 票
Security:      凭据只落 deploy/compose/.env.uat（root .gitignore `.env.*` 命中，已 git check-ignore 验证）；实例绑 127.0.0.1:8083 不对外；匿名读默认开（差分需要，Q2 口径）
Performance:   镜像构建总时长 ≈ 11 min（console 42s + docs ≈5 min 冷 npm ci + goreleaser 六平台 ≈2 min + 注入 56s）；构建链走 buildx binflow-builder（缓存温）；实例冷启 <1s、healthcheck 10s 内绿
Risks:         ① **Dockerfile 源构建路径断**（详见下）② compose 容器名钉死 binflow-ga——用户将来起 GA compose（project binflow）会撞名 ③ 并行轨道已在消费本实例（18:47:21Z admin 建 audit-probe-docker-remote）
Blockers:      无（UAT 已立起；Dockerfile 缺陷不影响本实例，走的是 prebuilt 注入路径）
Next:          ① Dockerfile 缺陷转 release-engineer（加 `COPY web/public ./public`）② L000-B 差分可直接消费（接入形态见「给 L000-B 的关键事实」）

---

## 1. 最终形态

| 项 | 值 |
|---|---|
| URL | **http://localhost:8083**（端口 8083 空闲，无需改选；:8081-8082 被 Artifactory 参照占用，勿动） |
| 版本/commit | `{"version":"uat-59f33ab5","revision":"59f33ab5","product":"BinFlow"}`（GET /binflow/api/system/version 实测） |
| 镜像 | `binflow:uat-59f33ab5-alpine`（linux/amd64 —— 本机是 Intel Mac；goreleaser prebuilt 注入链，零-RUN runtime） |
| 容器/项目 | 容器 `binflow-ga`，compose 项目 `binflow-uat` |
| 数据隔离 | **唯一挂载 = docker 具名卷 `binflow-uat_binflow-data` → /var/lib/binflow**（docker inspect Mounts 实证）；用户实例 ./data 全程未触碰 |
| 重启策略 | unless-stopped（compose 内置）；实测 restart 后 healthy、仓库/制品保留 |
| 日志 | `docker logs binflow-ga`（json-file，10MB×3 轮转）；boot 行含 version/revision |
| 凭据获取 | `BINFLOW_ADMIN_PASSWORD`，读 `deploy/compose/.env.uat`（gitignored、本地文件）；用户名 `admin` |

## 2. 给 L000-B 的关键事实（docker 差分接入形态）

- **仓 URL 形态**：docker Registry API 挂**根级 `/v2/**`、同端口 8083**（ADR-0010，无独立 registry 端口、无子域名）。镜像 name 模型 `<host>/<repoKey>/<image...>`——**首段是 BinFlow 仓库 key**（需先建 `packageType=docker` 仓），单段 name 404。例：`localhost:8083/docker-local/binflow/uat-59f33ab5`。
- **认证方式**：`docker login localhost:8083 -u admin`（密码 = deploy/compose/.env.uat 的 BINFLOW_ADMIN_PASSWORD）。挑战头 `Www-Authenticate: Bearer realm="http://localhost:8083/v2/token",service="binflow"`；token 为**不透明串（非 JWT）**，`expires_in=2592000`（720h，TTL 由 BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS 控制）。匿名读默认开（经匿名 token 通行）；写面/管理面恒需认证。`localhost/127.0.0.1` 目标 docker daemon 默认按 insecure 处理（明文 HTTP 无需配 insecure-registry）。
- **API base path**：管理面 `/binflow/api/...`（如 `/binflow/api/system/version`、`PUT /binflow/api/repositories/<key>`、`GET /binflow/api/v1/audit`）；通用制品内容路径 `/binflow/<repoKey>/<path>`；控制台 `/binflow/ui/`；docs `/binflow/docs/`；探针 `/readyz` `/healthz`（免认证）。
- 差分配对参照：Artifactory 参照实例 :8081-8082（admin，7.161.20）。

## 3. 启停命令（UAT 指南）

```bash
# 状态 / 日志 / 停 / 起 / 彻底清（清=删卷，慎用）
docker compose -p binflow-uat --env-file deploy/compose/.env.uat -f deploy/compose/docker-compose.yml ps
docker logs binflow-ga
docker compose -p binflow-uat --env-file deploy/compose/.env.uat -f deploy/compose/docker-compose.yml stop
docker compose -p binflow-uat --env-file deploy/compose/.env.uat -f deploy/compose/docker-compose.yml start
docker compose -p binflow-uat --env-file deploy/compose/.env.uat -f deploy/compose/docker-compose.yml down   # 保留卷
# 注意：容器名钉死 binflow-ga —— 与 GA compose（project binflow）互斥，勿同时起
```

镜像重建（换 HEAD 时）：

```bash
make console && make docs && make release VER=uat-59f33ab5   # VER=uat-<shortsha>
REGISTRY= PUSH=0 SMOKE=0 ARCHES=amd64 VARIANTS=alpine ./deploy/release/build-release.sh uat-59f33ab5
docker tag binflow:uat-59f33ab5-alpine-amd64 binflow:uat-59f33ab5-alpine
docker compose -p binflow-uat --env-file deploy/compose/.env.uat -f deploy/compose/docker-compose.yml up -d --force-recreate
```

（本机 Docker VM = linux/amd64；ARM Mac 上换 ARCHES=arm64。**不要**用 compose `up --build`——源构建 Dockerfile 当前是断的，见 §5。）

## 4. 自测证据（命令 + 输出摘录，2026-09-10T18:4xZ）

```
$ docker inspect binflow-ga --format '{{range .Mounts}}{{.Type}} {{.Source}} -> {{.Destination}}{{"\n"}}{{end}}'
volume /var/lib/docker/volumes/binflow-uat_binflow-data/_data -> /var/lib/binflow     ← 唯一挂载，./data 零接触

$ curl -s http://localhost:8083/readyz   → OK [200]
$ curl -s http://localhost:8083/healthz  → OK [200]
$ curl -s http://localhost:8083/binflow/api/system/version
  {"version": "uat-59f33ab5", "revision": "59f33ab5", "product": "BinFlow"} [200]
$ curl -s -o /dev/null -w '%{http_code}' http://localhost:8083/binflow/ui/   → 200   （/binflow 301→/binflow/ui/）
$ curl -s -o /dev/null -w '%{http_code}' http://localhost:8083/binflow/docs/ → 200

# 匿名写面（负对照）与 admin 写面：
$ curl -o /dev/null -w '%{http_code}' -X PUT -d '{"rclass":"local","packageType":"docker"}' \
    http://localhost:8083/binflow/api/repositories/docker-local            → 401
$ curl -u "admin:$BINFLOW_ADMIN_PASSWORD" -X PUT -H 'Content-Type: application/json' \
    -d '{"rclass":"local","packageType":"docker"}' \
    http://localhost:8083/binflow/api/repositories/docker-local
  Successfully created repository 'docker-local' [200]

$ curl -s -D - -o /dev/null http://localhost:8083/v2/ | grep Www-Authenticate
  Www-Authenticate: Bearer realm="http://localhost:8083/v2/token",service="binflow"    （HTTP/1.1 401）

$ echo "$PW" | docker login localhost:8083 -u admin --password-stdin   → Login Succeeded
$ docker tag binflow:uat-59f33ab5-alpine localhost:8083/docker-local/binflow/uat-59f33ab5
$ docker push localhost:8083/docker-local/binflow/uat-59f33ab5
  latest: digest: sha256:efdc53a341c7204249154d2f9de48905fc5766a2c440d963e4b93cf39f38b390 size: 1808
# 匿名 token + 匿名 manifest 读（匿名读面）：
$ curl "http://localhost:8083/v2/token?service=binflow&scope=repository:docker-local/binflow/uat-59f33ab5:pull"
  {"token":"2fee117c…","access_token":"…","expires_in":2592000,…}   （不透明 token）
$ curl -H "Authorization: Bearer $TOK" …/v2/docker-local/binflow/uat-59f33ab5/manifests/latest  → [200]
$ docker pull localhost:8083/docker-local/binflow/uat-59f33ab5   → Status: Image is up to date
  回读 digest = sha256:efdc53a3…（与 push 一致，逐字节同）

# 重启持久化：
$ docker compose … restart → healthy；GET /binflow/api/repositories → ['docker-local','audit-probe-docker-remote']
$ docker inspect binflow-ga --format '{{.HostConfig.RestartPolicy.Name}}' → unless-stopped
$ docker logs binflow-ga | head -1
  time=… level=INFO msg="binflow starting" version=uat-59f33ab5 revision=59f33ab5 … driver=sqlite
```

观察：18:47:21Z（本实例立起约 30s 后）另一 actor 以 admin 建 `audit-probe-docker-remote`（remote/docker，审计事件 id=2）——并行轨道已在消费本实例，符合 L000-B 前置预期，无冲突。

## 5. 发现缺陷：源构建 Dockerfile 路径断（登记，未越界修）

`deploy/release/Dockerfile.alpine`（distroless 与 deploy/dev/Dockerfile 同病）console 阶段只 `COPY web/package.json… web/scripts web/src`，**缺 `COPY web/public ./public`**。HEAD 树 `web/public/brand/` 是 git 跟踪资产（favicon/icon/manifest 7 件，M15 wire-brand 面），镜像内缺失 → vite 构建后 `dist/brand/` 为空 → `wire-brand-assets` 步骤 fail：`dist/brand/ is empty — web/public/brand/ missing?`，`npm run build` exit 1，compose `up -d --build` 整链断。

实锤（首次尝试 compose 源构建，构建号 docker-desktop://…/3lcgi35zogj04hihmve06oe5）：

```
#27 35.48 wire-brand-assets: dist/brand/ is empty — web/public/brand/ missing? Run scripts/gen-brand-assets.mjs.
#27 ERROR: process "/bin/sh -c npm run build" did not complete successfully: exit code: 1
```

宿主上同一构建绿（`wire-brand-assets: 7 brand files fingerprinted`，含 favicon-996d22f6.ico，与 M15 指纹一致）——证明差集就在 Dockerfile 的 COPY 面。M14/M15 release 验证全走 prebuilt 注入链（build-release.sh），故源构建路径自 wire-brand 落地起静默断链，**GA compose「15 分钟从零」G10 路径当前不可用**。修复（release-engineer 票）：三处 Dockerfile console 阶段各加一行 `COPY web/public ./public`。本票按「deploy/ 只增 uat- 文件、不改核心资产」未动它，UAT 改走已验证的 prebuilt 注入链绕开。
