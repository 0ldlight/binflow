# T-45 — 部署烟测：Docker 镜像 + compose（FR-14 + O2）

- 票据: T-45 [P0]（AC 全文见 `reports/agents/T-32.md` T-45 节；PRD milestone-2 v1.3 FR-14 / §7 O2）
- 角色: release-engineer；area: `deploy/dev`、`deploy/README.md`
- 代码基线: **aadf114**（含 D44-1/2/3 修复 2f505da 与 PRD v1.3 口径；工作树当时干净，仅本票 deploy/ 改动后进入烟测）
- 环境: macOS 宿主（Docker Desktop，client/server 29.7.2，LAN 192.168.1.70）；docker 客户端 daemon 用 `docker:dind`（本地既有镜像，inner 29.7.2 + Compose v5.4.0）——T-44 验证过的同路径：**宿主 daemon.json / Docker Desktop 配置零改动**，insecure-registry 只配在 dind 容器内 daemon
- 结论: **FR-14 AC1/AC2/AC3 全过（3/3 P0），AC4 本报告归档；O2 干净环境默认 8080 完整复跑全过**。未发现新缺陷。

## 0. 环境裁定（与 PRD AC 的两点偏差，均按文档路径处理）

1. **宿主 8080 被用户自有工作负载（arca-frontend）占用**——不动用户容器。主轮按根 README Path B 既有文档路径覆写 `BINFLOW_BIND_PORT=18080`（这正是 O2 登记的原始担忧场景）；O2 要求的「干净环境默认端口 8080」在**全新 dind daemon** 内复跑（等效 CI runner：全新 network/volume/镜像表，端口 8080 空闲）。
2. **dind 客户端可达性**：主轮 compose 绑 `192.168.1.70:18080`（LAN IP，T-44 同法；评估实例 + 一次性随机口令，烟测后即拆，明文 HTTP 短暂暴露于可信内网——NFR-S6 语境下与 T-44 处置一致）。O2 轮则在 dind 内部走 `localhost:8080`（daemon 默认 insecure 列表含 `127.0.0.0/8`，**零 insecure 配置**，即 deploy/dev/README.md 写的零配置路径）。

## 1. FR-14-AC2 — 镜像构建 + `--help`（PASS）

```
$ docker build -f deploy/dev/Dockerfile -t binflow:m2 .   # HEAD aadf114
=> => naming to docker.io/library/binflow:m2 done
BUILD_EXIT=0

$ docker image inspect binflow:m2 --format 'ID={{.Id}} USER={{.Config.User}}'
ID=sha256:82c440799144594a487434a9c2c7169990c08a05e2f7dc7fb56774b6b861d9dc  USER=binflow   # 非 root

$ docker run --rm binflow:m2 --help        # 输出 usage 全文（serve/gc/-c/--help/--version/BINFLOW_* 环境）
HELP_EXIT=0
$ docker run --rm binflow:m2 --version
binflow-server dev (revision dev)
VER_EXIT=0
```

注：`--version` 为 `dev (revision dev)`——版本注入（`-X main.version`）是 M5 goreleaser 票（部署矩阵第 1 项）范围，本票不处理。

## 2. FR-14-AC1 — compose 实例上 D04/D05/D16（PASS，非本机裸进程）

实例：`docker compose -p t45main -f deploy/dev/docker-compose.yml up -d --build`（exit 0），带 M2 增量 env `BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS=1`（见 §6）。状态 `Up (healthy)`，端口 `192.168.1.70:18080->8080`；管理面 ping 200；建仓 `PUT /binflow/api/repositories/docker-local` → 200。

### D04（v1.3/C6 口径：未认证 ping 一律挑战）

```
$ curl -s -o /dev/null -w '%{http_code}\n' http://192.168.1.70:18080/v2/
401
$ curl -sI .../v2/ | grep -i www-authenticate
Www-Authenticate: Bearer realm="http://192.168.1.70:18080/v2/token",service="binflow"
$ curl -sI .../v2/ | grep -i docker-distribution-api-version
Docker-Distribution-Api-Version: registry/2.0
$ curl -su admin:$PW -o /dev/null -w '%{http_code}\n' .../v2/   → 200
$ curl -su admin:$PW .../v2/                                    → {}
```

realm 随实际 Host:port 生成（端口重映射下正确）；token TTL 透传断言：`GET /v2/token?...` → `{"token_len":64,"expires_in":3600,"issued_at":"…"}`（1h = 本轮 env 覆写值，微调项端到端生效）。

### D05（dind 内 docker 29.7.2，`--insecure-registry 192.168.1.70:18080`）

```
$ echo "$PW"   | docker exec -i t45-dind docker login 192.168.1.70:18080 -u admin --password-stdin
Login Succeeded        # exit 0
$ echo "definitely-wrong" | docker login ... (同参) 
Error response from daemon: Get "http://192.168.1.70:18080/v2/": unauthorized: authentication required
wrong_pw_exit=1        # FR-11-AC4（T-44 修复后）在 compose 实例上保持
```

### D16（login → build → push → rmi → pull → run）

```
$ docker build -t 192.168.1.70:18080/docker-local/acme/app:v1 .   # FROM alpine:3.20（宿主 save|load 入 dind，零公网）
BUILD_OK  →  $ docker push .../docker-local/acme/app:v1
v1: digest: sha256:3bcfaa710595dfd8bd220bea7a89c1211de94a1c581baa44bb880bffb4f89d04 size: 855   # exit 0
$ docker rmi && docker pull && docker run --rm .../acme/app:v1
ok        # 全链 exit 0（run 输出 ok）
```

## 3. FR-14-AC3 — restart 持久化 D21（PASS）

```
$ docker compose -p t45main ... restart          # Container binflow-dev Started
health_after_restart=healthy
$ (dind) docker rmi ...; docker pull ...; docker run --rm 192.168.1.70:18080/docker-local/acme/app:v1
ok  D21_EXIT=0
$ curl -su admin:$PW .../v2/_catalog
{"repositories":["docker-local/acme/app"]}
```

restart 后已 push 镜像仍可 pull 且可 run；卷 `t45main_binflow-data` 持久化成立。

## 4. O2 — 干净环境默认端口 8080 完整复跑（PASS，留档即本节）

干净环境模拟（任务书允许的「docker 全新 network + 无卷状态」）：**全新 dind daemon**（t45o2-dind，无任何 `--insecure-registry` 自定义 flag，默认 insecure 列表仅 `127.0.0.0/8`/`::1/128`），宿主仓库以只读挂载 `/src`。镜像经 `docker save | docker load` 预载（内容 = §1 同一源码构建，digest 82c44079…；AC2 已在宿主证明从源构建，此处不重复拉公网），compose 以 `up -d`（镜像已存在不重建）。

- compose：`docker compose -p t45o2 -f /src/deploy/dev/docker-compose.yml up -d`（env 仅 `BINFLOW_ADMIN_PASSWORD` + `BINFLOW_BIND_ADDR=0.0.0.0`——后者仅为让宿主观测窗 `-p 127.0.0.1:18081:8080` 可达；**`BINFLOW_BIND_PORT` 未设 = 默认 8080**，容器侧映射 `0.0.0.0:8080->8080`）→ `Up (healthy)`。
- **D04**（宿主观测窗 127.0.0.1:18081）：401 / `Www-Authenticate: Bearer realm="http://127.0.0.1:18081/v2/token",service="binflow"` / `registry/2.0` / 带凭据 200 `{}`——全过。
- **D05**（dind 内，目标 `localhost:8080`，零 insecure 配置）：对口令 `Login Succeeded`；错口令 exit 1（unauthorized）。
- **D16**：build → push（digest `sha256:20d8d50c4b7bb00394cb0f8ffd181a56722588638bd1f399f485cb254b61ad5d`）→ rmi → pull → run `ok`，全 exit 0。
- **D21**：`compose restart` → healthy → pull+run `ok`。
- 服务端健康：整轮 access log 49 请求，`"status":5xx` 计数 **0**。

结论：默认端口 8080、零客户端配置的「本机直推」路径在干净环境成立；README 既有 env 覆写说明不变（主轮已顺带实证 `BINFLOW_BIND_PORT` 覆写路径）。

**compose 无反代组件 + 前置反代直通文档**：compose 未加任何反代组件（维持 ADR-0010 §6）；nginx/traefik「`/v2/` 直通不 rewrite」示例片段已落 `deploy/dev/README.md`（新增，见 §6），并说明 realm 指向自身 `/v2/token` 故不可 rewrite 的原因、`client_max_body_size 0` 与流式转发要点。

## 5. FR-14-AC4 — 产物与 digest 清单（仅记录；发布动作待用户确认）

| 产物 | digest / 标识 | 去向 |
|---|---|---|
| `binflow:m2`（AC2 构建产物，源码 aadf114） | manifest list `sha256:82c440799144594a487434a9c2c7169990c08a05e2f7dc7fb56774b6b861d9dc`；config `sha256:182a37791905554c3a968899b501a8fe7a896f3e65d3268f9793ad5ecc488555`；amd64，13.8MB，USER binflow | 本地，烟测后已删标签 |
| `binflow:dev`（主轮 compose `--build` 重建，同源码） | `sha256:d0a9141a3a7826a9f308671ff331514d6c03ad903e342db11684738f4df22f76` | 宿主保留（替换 M1 旧 tag f9ee4625，内容=当前 HEAD） |
| 烟测推送的镜像 `docker-local/acme/app:v1` | 主轮 `sha256:3bcfaa71…d04`；O2 轮 `sha256:20d8d50c…5d` | 随实例销毁（`down -v`），不在任何外部 registry |
| **外部 registry 上传** | **无**（本票零外发） | **未执行任何对外发布/推送；如需推送外部 registry，清单即上表，待用户确认** |

## 6. M2 增量微调（deploy/dev，均注明理由）

1. `docker-compose.yml`：新增 `BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS: ${…:-720}` 透传。理由：M2 的 `/v2/token` TTL（`expires_in`）是新增主旋钮，此前 .env 无法设置需改 yml；`:-720` 兜底 = `config.DefaultTokenTTL`（空串会被 config 的正整数 env 解析拒绝，故必须带兜底值）。**烟测实证**：设 1 → `expires_in=3600`（§2）。
2. `.env.example`：补上述 TTL 条目（注释含默认值 720h 与作用域：管理面 + docker token 同源）。
3. `deploy/dev/README.md`（新增）：O2 文档硬要求——前置反代直通 `/v2/` 的 nginx/traefik 片段（ADR-0010 §6：不 rewrite、realm 自洽说明）+ M2 docker 接入口径（REG 直连、name 模型 `<repoKey>/<image>`、localhost 零 insecure 配置路径、TTL 旋钮）+ 烟测记录表。
4. `deploy/README.md`：约定区补一行 M2 烟测记录（报告路径回链）。

Dockerfile 零改动（M1 形态对 M2 代码直接成立：多阶段/非 root/healthcheck 无需变更）。

## 7. 缺陷与遗留

- **新缺陷：无**（三组 AC + O2 复跑零异常；唯一非预期是宿主 8080 被占——用户工作负载，非缺陷，按文档覆写路径处理）。
- 已知未修欠账（非本票范围，仅记录）：D44-4（超长 tag 400 vs 404）、D44-5（upload 状态 GET 缺 Location）、D44-6（删光 manifest 后 tags/list 404 vs PRD v1.3 的 200 `"tags":null`）——BOARD 已记归 T-35/T-40 域 T-45 后收口。
- 观察：dind 内 daemon 就绪约 20–30s（TLS 弃用警告减速，与 T-44 注记一致），就绪探测勿只等 5s。
- M5 提醒：`binflow-server --version` 仍为 `dev`——版本注入留给 goreleaser 票。

## 8. 清理（已执行）

- 主轮：`docker compose -p t45main down -v`（容器/卷/网络全删）；t45-dind 已删。
- O2 轮：dind 内 `compose -p t45o2 down -v`；t45o2-dind 已删（内部镜像随之消失）。
- 宿主：`binflow:m2` 标签已删；`binflow:dev` 保留（同源码重建，见 §5）；一次性口令文件 /tmp/t45 已销毁；宿主 daemon.json / Docker Desktop 配置零改动；用户 arca-frontend/backend 容器未动；`alpine:3.20`/`docker:dind` 等既有镜像未动。
- 终态核查：无 t45/binflow 容器与卷残留；工作树仅本票 4 个 deploy/ 文件改动。
