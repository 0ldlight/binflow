# deploy/dev — 开发/评估用 compose 形态

T-17 交付（M1），M2 起该实例即一个**可用的 docker registry**（FR-14 烟测口径）。
生产形态（multi-arch 镜像、Postgres、Helm/K8s）见仓库根 `deploy/README.md` 部署矩阵，GA 前逐步交付。

## 快速开始

```bash
cd deploy/dev
cp .env.example .env          # 编辑 BINFLOW_ADMIN_PASSWORD（不设则 compose 拒绝启动）
cd ../..
docker compose -f deploy/dev/docker-compose.yml up -d --build
docker compose -f deploy/dev/docker-compose.yml ps    # STATUS = Up (healthy)
curl -s http://localhost:8080/binflow/api/system/ping # OK
```

数据在具名卷 `binflow_binflow-data`（挂 `/var/lib/binflow`）：`restart` 与 `down && up -d`（不带 `-v`）都保留制品；`down -v` 清空。
宿主 8080 被占时在 `.env` 设 `BINFLOW_BIND_PORT=18080`（README 根文件 Path B 同款说明）。

## docker 客户端接入（M2）

BinFlow 的 docker 端点挂根级 `/v2/**`（ADR-0010：docker 客户端硬编码 `/v2/` 前缀，无法配置），
`docker login <host>` **直连**，无路径前缀：

```bash
export REG=localhost:8080            # host[:port]，无 scheme
docker login $REG -u admin           # Login Succeeded
docker tag alpine:3.20 $REG/docker-local/acme/app:v1
docker push $REG/docker-local/acme/app:v1
```

要点：

- **name 模型**：`<host>/<repoKey>/<image...>`——首段是 BinFlow 仓库 key（先经管理 API 建好
  `packageType=docker` 的仓库，如 `docker-local`），其余段是镜像名；单段 name 404。
- **plain HTTP**：M1/M2 仅明文 HTTP（NFR-S6）。docker daemon 对非 localhost 地址需配
  `--insecure-registry <host:port>`（daemon.json 的 `insecure-registries` 数组）；
  `localhost`/`127.0.0.1` 目标 daemon 默认按 insecure 处理，无需配置。
  compose 默认绑 `127.0.0.1`，本机 daemon 直推即是这条零配置路径。
- **token**：`docker login` 走 `/v2/token` Bearer 协商；未认证 ping 一律 401 挑战（v1.3 口径），
  匿名读（默认开）经匿名 token 通行。token 无 refresh、按 `.env` 的
  `BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS`（默认 720h）计 TTL。
- **持久化**：push 过的镜像在 `docker compose restart` 后仍可 pull（D21/FR-14-AC3）。

## 前置反向代理（可选）

compose 产物**默认不含反代组件**（ADR-0010 §6：根级例外在应用内实现，裸单二进制/裸 compose 即可
docker push，反代非必需）。已有 nginx/traefik 前置时，`/v2/` **原样直通、不要 rewrite**——
`Www-Authenticate` 挑战头里的 `realm` 指向 BinFlow 自身 `/v2/token`，任何路径改写都会打断 token 协商：

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
    # 控制台入口跳转（/binflow 无尾斜杠）必须显式直通（T-106）：只配
    # location /binflow/ 时，无斜杠的 /binflow 不命中任何 location，nginx
    # 会自造 301（Location 用 $host 绝对化、端口被剥掉），非标准端口部署
    # 下浏览器落到死链；直通后由 BinFlow 自己 301 到 /binflow/ui/。
    location = /binflow {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
    }
    location /binflow/ {                 # 管理面 API + 控制台 + 通用制品路径
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        client_max_body_size 0;
    }
}
```

```yaml
# traefik（labels 片段，挂在 binflow 容器上或 file provider）
http:
  routers:
    binflow-v2:
      rule: "PathPrefix(`/v2/`)"          # 直通，不 strip 前缀
      service: binflow
      tls: {}
    binflow-api:
      rule: "Path(`/binflow`) || PathPrefix(`/binflow/`)"   # 无斜杠入口也要命中（T-106，同 nginx 注）
      service: binflow
      tls: {}
  services:
    binflow:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8080"
```

经反代域名访问时仅 `REG` 取值变化（`REG=registry.example.com`，HTTPS 下无需 insecure-registry），命令本体不变。
反代 TLS 终结后 BinFlow 侧仍是明文，保持 compose 绑 `127.0.0.1` 即「TLS 在边缘、明文不过网络」的常规形态。

## 烟测记录

| 里程碑 | 日期 | 范围 | 报告 |
|---|---|---|---|
| M2 | 2026-08-19 | FR-14-AC1~AC4 + O2（干净环境默认 8080 复跑） | `reports/agents/T-45-smoke.md` |
| M4 | 2026-08-21 | FR-33-AC5：console embed（镜像内 node 阶段构建）+ session/CSRF + 五协议 + GC/锁 + export/import + 反代链 | `reports/agents/T-106-qa.md` |
