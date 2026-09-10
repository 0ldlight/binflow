# L001-3 取证报告：remote docker 仓 url 的 `/v2` 尾缀行为（解 known-divergence UNKNOWN 条目 1）

- Ticket: L001-3（devops 轨道，任务 2）
- 日期: 2026-09-11
- 对象条目: `docs/compatibility/known-divergence.yaml` → `docker/remote-url-v2-suffix-silent-404`（classification: UNKNOWN，authority: pending「LOOP 001 取证票（:8082 建带尾缀 remote 仓观察）」——本报告即该取证）
- 参照: Artifactory 7.161.20（http://localhost:8082，admin 凭据经命令行变量，未落盘）
- 上游: 本地 registry:3（`l001-sfx-upstream` 127.0.0.1:5588，无认证，已种 hello-world:latest，digest `sha256:d1a8d0a4eeb63aff09f5f34d4d80505e0ba81905f36158cc3970d8e07179e59e`）
- 客户端: 真实 docker CLI 27.5.1（一次性 dind `l001-dind`，`--insecure-registry=host.docker.internal:8082`）
- 观察手段: registry:3 请求日志（`http.request.uri` + `useragent` + `err.code`），以 UA 区分流量（`Artifactory/7.161.20` vs 宿主 `docker/29.7.2` 种镜像噪声）

## 三形态实测

| # | repo key | url 配置 | 建仓 | 上游实收（UA=Artifactory/7.161.20） | 客户端结果 |
|---|---|---|---|---|---|
| ① | audit-probe-sfx-v2 | `http://host.docker.internal:5588/v2` | 200 接受，无警告 | `GET /v2/v2/hello-world/manifests/latest` → 404（`err.code="manifest unknown" err.detail="unknown tag=latest"`） | pull 失败：`manifest unknown: The named manifest is not known to the registry.` |
| ② | audit-probe-sfx-v2sl | `http://host.docker.internal:5588/v2/` | 200 接受，无警告 | 同①：`GET /v2/v2/hello-world/manifests/latest` → 404 | 同① |
| ③ | audit-probe-sfx-root | `http://host.docker.internal:5588`（根形态） | 200 接受 | `GET /v2/hello-world/manifests/latest` + 两 blob → 全 200 | pull 成功，digest 与上游逐字一致 |

关键判别事实：

1. **Artifactory 不剥剪**——url 带 `/v2` 或 `/v2/` 尾缀时，上游请求路径是 `<url>/v2/<image>/manifests/<ref>` 的直拼结果（`/v2/v2/...` 双前缀），没有任何归一化。
2. **不拒绝建仓**——三种形态 PUT /artifactory/api/repositories 全部 200，无校验错误、无警告字段。
3. **静默 404 透传**——双前缀导致上游 404 后，Artifactory 折叠为标准 `MANIFEST_UNKNOWN`（"The named manifest is not known to the registry."）返回客户端；错误形态与 C12 已采证据同源（detail 键差异见 L000 差分报告，非本条目范围）。
4. 尾斜杠无差别（①=②）。

## BinFlow 现状（UAT live A/B，2026-09-11）

同上游、同镜像、同客户端对 BinFlow UAT（:8083）做同配置腿：

- repo `l001-sfx-v2`（remote/docker，url=`http://host.docker.internal:5588/v2`，allowPrivateUpstream:true）建仓 200，无警告；
- dind pull → 失败 `manifest unknown to registry: latest`；
- 上游实收（UA=`binflow-remote/1.0`）：`GET /v2/v2/hello-world/manifests/latest` ×6 → 404。

即 BinFlow 与 Artifactory 在该配置下的上游 wire 行为**逐字一致**（同双前缀路径、同 404 折叠为 MANIFEST_UNKNOWN）。代码路径佐证：`internal/remote/client.go#JoinURL`（base 尾斜杠剪 + path 头斜杠剪 + 直拼）× `internal/adapter/docker/remote.go#v2WireManifestPath/#v2WireBlobPath`（恒产 `/v2/<image>/...`）。known-divergence 条目所述「树内 helmoci fixtures 曾实证」与本 live 证据一致。

## 裁定建议

**BinFlow 维持「照抄」（现状即兼容），条目从 UNKNOWN 改判 INTENTIONAL（parity-compatible），登记即结。**

理由：参照系统 Artifactory 对同配置的行为与 BinFlow 逐点一致——不剥剪、不拒绝、不警告，双前缀 404 以相同错误码族（MANIFEST_UNKNOWN）透传。剥剪/拒绝/警告任一选项都会制造与参照的行为分歧（严格 parity 口径下是新的 DIVERGENT）。

可选增强（超出 parity，需产品单独裁定，不并入本条目）：建仓时对 docker/helmoci remote url 的 `/v2`(/`/v2/`) 尾缀发一条 WARN 日志（不改行为）——两侧今天都是静默失败，运维排障成本高；但任何「改行为」的选项（剥剪/拒绝）都应否决。

## 环境清理（执行记录，2026-09-11）

- Artifactory：DELETE /artifactory/api/repositories/audit-probe-sfx-{v2,v2sl,root} ×3 → 200（root 腿 deletedArtifactsCount=5——成功腿的缓存制品；两个双前缀腿 0——从未成功拉过）；删后 audit-probe 前缀仓零残留。
- BinFlow UAT：l001-auth-remote（deleteContent=true，含 3 缓存节点）、l001-auth-remote-noauth、l001-sfx-v2 全删 200；仓列表仅剩既有 `docker-local` 与 L000-F 保留的 `audit-probe-docker-remote`（非本票资产，未动）。
- 一次性容器：l001-dind、l001-sfx-upstream、l001-auth-upstream rm -f，`docker ps -a` 零残留；dind 内两侧登录凭据随容器销毁。
- 宿主：docker logout 127.0.0.1:5589；5588/5589 的 hello-world 测试 tag 已 rmi（L000 会话保留的 busybox/probe-img 标签非本票资产，未动）；/tmp 下 htpasswd、上游口令、cookie jar 全删。
- 凭据不落任何 git 跟踪文件：Artifactory admin 与 BinFlow admin 口令经命令行变量；registry 测试口令只存 /tmp（已删）与容器生命周期内。
- UAT 终态：binflow-ga healthy，:8083，原卷，`BINFLOW_REMOTE_CREDENTIALS_KEY` 常驻 `deploy/compose/.env.uat`（gitignored，D20 修复持久化）。
