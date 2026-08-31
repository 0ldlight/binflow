---
title: Helm Chart 仓库接入
sidebar_position: 28
---

# Helm Chart 仓库接入（经典 chart 仓 + HelmOCI）

> 适用版本：M11 起（helm / helmoci 包型均为 **pro 档**能力——建仓/上传需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有 chart 仍可 `helm pull`/`install`）。**M13 增补**：HelmOCI **remote（代理）/ virtual（聚合）** 两仓型 + `chartsBaseUrl` 分体基址 + `_external` 落盘缓存（见下文专节）。
> 验证客户端：**helm 4.2.4** + gpg 2.5.21 实测（T-309/T-313：package / repo add / update / search / show / pull / template / `--verify` 签名链 / kind 集群 install `STATUS: deployed`；M13 增量 T-363/T-365/T-367 与本文 T-375 复测：helmoci remote/virtual 推拉全链 + chartsBaseUrl 异构基址 + `_external` 折叠路径）。行为基准 `docs/reverse/helm.md` §6.1/§8.3/§8.4。
> 命名辨析：本文前半是 **helm 经典 chart 仓**（`index.yaml` + `.tgz`，包型 `helm`）；「HelmOCI」节是 **OCI 形态 chart 仓**（`helm push` 到 `oci://` 地址，包型 `helmoci`——走 /v2 栈但与 docker 是两个包型）；用 Helm Chart **部署 BinFlow 本身**见 [Helm Chart（Kubernetes）安装](../install/helm.md)。

BinFlow 的 helm 仓 = 标准 chart repository：上传 `.tgz` 自动解析 Chart.yaml 并**重算 `index.yaml`**，客户端 `helm repo add` 后即获得搜索/安装能力。**local / remote（代理）/ virtual（聚合）三类仓型齐备**；OCI 形态（helmoci 包型）自 M13 起同样三型齐备（见[下文](#helmoci-仓型oci-形态m13local--remote--virtual)）。

- 仓 URL：`$BASE/binflow/<repoKey>`（index.yaml 在仓根）。
- index 条目 `urls` 恒为**相对路径**（客户端经当前仓 URL 取数；remote/virtual 场景由 BinFlow 改写，见下文）。

## 前置条件

- 运行中的 BinFlow 实例；**pro 及以上** license。
- helm 3+（实测 4.2.4）。

## local 仓：打包、上传、安装

### 1. 建仓

```bash
export BASE=http://localhost:8080 ADMIN_PW=<管理员口令>
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helm-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"helm"}' -o /dev/null -w '%{http_code}\n'   # 200
```

### 2. 打包并上传

```bash
helm create mychart && helm package mychart
# mychart-0.1.0.tgz

curl -su admin:$ADMIN_PW -T mychart-0.1.0.tgz $BASE/binflow/helm-local/mychart-0.1.0.tgz \
  -o /dev/null -w '%{http_code}\n'               # 201（index 随即重算）

# 对账：index 里该条目 digest == 本地文件 sha256
shasum -a 256 mychart-0.1.0.tgz
curl -s $BASE/binflow/helm-local/index.yaml | grep -A2 'digest:'
```

- 上传即解析：`.tgz` → tar 归档根的 Chart.yaml（+ requirements.yaml 与内嵌 dependencies 合并）；无 Chart.yaml 或坏 tar **跳过索引但不拒 PUT**（按普通文件落库）。
- 同 name+version 重传 = 覆盖（index 条目 remove+add）。
- 版本排序按 SemVer 降序；`version`/`appVersion`/`created` 恒双引号（helm 自身输出同形）。
- **`.prov` 签名文件是普通文件**：`curl -T mychart-0.1.0.tgz.prov …` 落库即可（不进 index、不经 keypair 体系）。

### 3. 客户端消费

```bash
helm repo add binflow $BASE/binflow/helm-local
helm repo update
# ...Successfully got an update from the "binflow" chart repository

helm search repo binflow/mychart
helm show chart binflow/mychart
helm pull binflow/mychart --version 0.1.0        # 下载后 sha256 与上传对账
helm template binflow/mychart
helm install my-rel binflow/mychart --wait       # STATUS: deployed（kind 集群实测）
```

### 4. provenance 验签（--verify）

helm 的验证器要求 `.prov` 是 **clearsign 文档**且正文为两段式（Chart.yaml 元数据 + `files:` sha256 块）——注意 `gpg --armor --detach-sign` 产出的 detached 签名**会被 helm 拒绝**（`signature block not found`）：

```bash
# 构造 clearsign payload：Chart.yaml 内容 + 空行 + files 块
# 注意两段之间必须有一行字面 "..."（helm 4.x 的解析器按 "\n...\n" 切段——
# 缺了它会报 "message block must have at least two parts"）
{ cat mychart/Chart.yaml; echo; printf '...\nfiles:\n  %s: sha256:%s\n' \
    mychart-0.1.0.tgz "$(shasum -a 256 mychart-0.1.0.tgz | cut -d' ' -f1)"; } > prov-payload.txt
gpg --clearsign prov-payload.txt                # 产出 prov-payload.txt.asc
curl -su admin:$ADMIN_PW -T prov-payload.txt.asc \
  $BASE/binflow/helm-local/mychart-0.1.0.tgz.prov -o /dev/null -w '%{http_code}\n'  # 201

helm pull binflow/mychart --verify --keyring <你的公钥 keyring>
# Signed by: <uid>
# Chart Hash Verified: sha256:<hex>
```

### 5. 只读别名面

`/binflow/api/helm/<repoKey>/…` 是内容面的**只读别名**（GET/HEAD 200；PUT/POST/DELETE 405 + `Allow: GET, HEAD`）——适合「管理面 URL 可达、内容面被网闸」的拓扑。

### reindex（管理面）

```bash
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/helm/helm-local/reindex        # 异步全仓
curl -su admin:$ADMIN_PW -X POST $BASE/binflow/api/helm/helm-local/reindex/mychart-0.1.0.tgz  # 同步单包
```

门 = 认证 + CanManageRepo；非 helm 仓 / 非 local → 400。

## remote 仓（代理上游 chart 仓）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helm-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"helm","url":"https://charts.example.com/stable"}' \
  -o /dev/null -w '%{http_code}\n'               # 200

helm repo add bf-remote $BASE/binflow/helm-remote
helm repo update && helm search repo bf-remote/
helm pull bf-remote/upchart --version 0.1.0
```

- `index.yaml` 与 `.tgz` 均 pull-through（metadata 600s TTL / content 长效；MISS→HIT 二次命中缓存，响应头 `X-BinFlow-Cache` 可见）。
- DELETE = 缓存驱逐（204/404），下一起请求回源；PUT → 405。

### chartsBaseUrl：分体回源基址（M13）

上游「索引」与「chart 实体」不在同一地址时（常见：index 挂在仓库站、tgz 在 CDN/对象存储），给 remote 仓配 `chartsBaseUrl`——**metadata（index.yaml）恒走仓 URL，content（tgz/.prov/`_external` 折叠路径）改走分体基址**；未配置回退仓 URL（镜像对齐默认）：

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helm-xbase \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"helm",
       "url":"https://charts.example.com/stable",
       "chartsBaseUrl":"https://cdn.example.com/charts"}' \
  -o /dev/null -w '%{http_code}\n'               # 200；GET 回显 "chartsBaseUrl": "https://cdn.example.com/charts"

helm repo add bf-xbase $BASE/binflow/helm-xbase && helm repo update
helm pull bf-xbase/upchart --version 0.1.0       # index 走 charts.example.com，tgz 走 cdn.example.com
```

- **仅 helm 包型 remote 接受该字段**：其它包型携带 → 400 点名字段（`chartsBaseUrl "…" is not accepted (the divergent charts fetch base is a helm remote-repository behavior)`）；值必须为绝对 http(s) URL（尾斜杠自动裁剪；`ftp://` 等 → 400）；显式空串 `""` = 清除回退仓 URL。
- **凭据边界**：分体基址与仓 URL 同 host → 复用仓凭据；**异 host → 无凭据出站**（仓上游凭据绝不发往第三方 host）。SSRF 链照走（第三方目标也要过守卫）。
- 取数走同一引擎缓存：负缓存 / TTL / 单飞 / stale-while-error 与普通路径一视同仁，**二次拉取本地 HIT 零 egress**（实测：分体基址命中后上游 tgz 计数冻结为 0）。

## virtual 仓（聚合 + URL 改写）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helm-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"helm",
       "repositories":["helm-local","helm-remote"],
       "defaultDeploymentRepo":"helm-local"}' -o /dev/null -w '%{http_code}\n'    # 200

helm repo add bf-virt $BASE/binflow/helm-virt
helm repo update && helm search repo bf-virt/
helm install t313 bf-virt/extchart --wait        # STATUS: deployed（实测）
```

聚合语义：

- **合并**：逐成员读 index.yaml，按 `(name, version)` **first-wins**（先成员即解析会服务的成员——digest 与下载面一致）；chart 内版本 SemVer 降序重排。
- **URL 改写**（聚合 index 的 `urls` 指向本虚仓可服务的地址；remote 成员条目的**识别基址 = 该仓 chartsBaseUrl，缺省回退仓 URL**——M13 起）：
  - 指向成员仓基址（或其 chartsBaseUrl）的 URL → 改写为成员相对路径；
  - 指向**外部 http(s) host** 的 URL → 折叠为 `_external/<scheme>/<host>/<path>`（`://` 折叠为 `/`；经虚仓的受控 egress 取数，响应头 `X-Binflow-Upstream` 标上游）；
  - 上游自身的 `_external` 面 → `_transitive/<...>`；
  - `oci://` 条目保留原样（客户端直连 OCI 仓）。
- **`_external`/`_transitive` 落盘缓存（M13）**：折叠路径的取数经引擎落盘在**该 remote 成员**的同形路径下（content 类 TTL）——二次拉取 `X-Binflow-Cache: HIT` + `X-BinFlow-Resolved-From: <成员>`、上游零 egress；落盘节点经 storage REST 可见（`GET /api/storage/<remote仓>/_external/http/<host>/<path>`）。第三方目标的故障**不写** assumed-offline 窗口（外部健康与仓上游独立）。`helm dependency update` 对依赖 chart 走的就是这条链（实测：依赖经虚仓 `_external` 路径取数并缓存）。
- **写**：PUT 路由到 `defaultDeploymentRepo` 的 local 成员（enforce 与 index 重算跟随落点成员）；未配置 → 405。
- 子目录 index（`<sub>/index.yaml`）→ 404（聚合 index 只在仓根）。
- **helm 与 helmoci 不混仓**：虚仓自身与全体成员的包型家族不得同时含 `helm` 与 `helmoci`（建/改仓 400）。

## 布局约束（enforce，可选）

仓配置 JSON 两开关（默认关）：

```json
{"forceMetadataNameVersion": true, "forceNonDuplicateChart": true}
```

开启后 `.tgz` 上传在字节落库前校验（403 文案逐字）：

- Chart.yaml 缺 name/version 或坏包：`This action is prevented due to the Enforce Layout Policy, the metadata of the package <repo>/<file> could not be read or is malformed.`
- 同名同版本重复：`This action is prevented due to the Enforce Layout Policy, a package with the same name and version <n>-<v> already exists in the repository.`（查重按文件名 `<name>-<version>.tgz` + 合法 SemVer2）

## HelmOCI 仓型（OCI 形态，M13：local + remote + virtual）

`helmoci` 包型走 **/v2 OCI Distribution 栈**（与 docker /v2 面同协议同端口，但**是独立包型**）：`helm push`/`helm pull oci://…` 全链；local 仓自 M12、**remote（代理）与 virtual（聚合）自 M13** 三型齐备。

```bash
export REG=<host>          # 例 127.0.0.1:8080（明文 HTTP 时客户端加 --plain-http）
helm registry login $REG -u admin -p <口令> --plain-http
helm push mychart-0.1.0.tgz oci://$REG/helmoci-local --plain-http
# Pushed: …/helmoci-local/mychart:0.1.0
# Digest: sha256:77f42292…           ← 与 tgz 本地 shasum 对账（chart 以 OCI manifest 承载）
helm pull oci://$REG/helmoci-local/mychart --version 0.1.0 --plain-http
helm install rel oci://$REG/helmoci-virt/mychart --version 0.1.0    # 经 virtual 聚合
```

### remote 仓（代理上游 OCI registry）

`url` 指向上游 registry 的 **distribution API 根（含 `/v2`）**——Docker Hub 形态 `https://registry-1.docker.io/v2`；上游是另一台 BinFlow 时 `http://<host>:<port>/v2/<上游仓key>`：

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helmoci-remote \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"remote","packageType":"helmoci",
       "url":"https://registry-1.docker.io/v2"}' \
  -o /dev/null -w '%{http_code}\n'               # 200

helm pull oci://$REG/helmoci-remote/mychart --version 0.1.0 --plain-http
```

- manifest/blob 按需回源、以**实测 sha256 寻址落盘**（blob 流式 + commit 时 digest 强校验——不符永不落盘 502）；**二次拉取零回源**（`X-Binflow-Cache: HIT`，实测上游内容 GET 计数冻结）。
- **上游认证链**：上游 401 + `WWW-Authenticate: Bearer` → 仓凭据（username/password；`enableTokenAuthentication` 时 Bearer）完成 token 交换 → Bearer 重试 → token 按 scope 缓存（Docker Hub 等公用 registry 无需凭据）。
- **降级**：上游 5xx/断连/token 失败 → 有过期副本答 `STALE` + `X-Binflow-Upstream-Error` 续服务；无副本 404（`MANIFEST_UNKNOWN` 族，**零裸 5xx**）。上游删除后缓存照常服务。
- **写动词全量 405**（`Allow: GET`）；缓存驱逐走 REST 面 `DELETE /binflow/<repo>/<path>`（同经典仓）。
- **tags/list 与 _catalog 只见已缓存的 tag**（tag 随 manifest 落地，不代理上游 tags/list）——上游已有但未拉过的版本在 tags/list 不可见；`helm pull --version` 显式拉取不受影响（实测 by-tag/by-digest 均直达）。

### virtual 仓（成员聚合）

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/helmoci-virt \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"virtual","packageType":"helmoci",
       "repositories":["helmoci-local","helmoci-remote"]}' \
  -o /dev/null -w '%{http_code}\n'               # 200

helm pull oci://$REG/helmoci-virt/mychart --version 0.1.0 --plain-http
curl -su admin:$ADMIN_PW $REG/v2/helmoci-virt/mychart/tags/list
# {"name":"helmoci-virt/mychart","tags":["0.1.0"]}   ← 成员 tag 并集
```

- **成员序 first-seen**：manifest/blob 按成员序逐成员解析（local 成员查节点；remote 成员查缓存→回源→落进该成员）；第一个能产出正文的成员胜出，响应带 `X-BinFlow-Resolved-From: <成员key>`（local 成员服务无缓存标头）。
- **tags/list / _catalog 并集**：tag 按成员序 first-wins 去重 + 全局排序 + 官方分页；catalog 以 virtual key 前缀渲染。remote 成员只贡献**已缓存** tag（同上口径）。
- **写面一律 405**：未配 `defaultDeploymentRepo` → `No local repository was configured as local deployment repository for the (helmoci-virt) virtual repository.`（实测逐字）；已配 → 如实文案点名目标（/v2 面的 push-through 路由未实现，登记后续票）。
- **成员同型校验**：helmoci virtual 的成员必须 helmoci 型——配 docker 成员 → 400（`virtual repository cannot mix the helmoci and docker package types …`，实测）；helm×helmoci 混仓维持双向 400。
- docker 客户端族对同一 virtual 同样可用（/v2 族协议忠实；docker virtual 建仓仍属 docker 矩阵，两者是不同包型）。

## 边界与有意不做（M11；M13 增补）

| 项 | 行为 |
|---|---|
| PUT `index.yaml` / `.index/**` | **403**（服务端生成的索引不可手写） |
| 聚合 index 缓存 | 按请求现算（成员变更下一请求即见）；`.index` 路径仍 403 防写 |
| namespace 模式（useNamespaces） | 未实现（规格默认关） |
| 分体 `chartsBaseUrl` 配置位 | **已实现（M13）**——仅 helm remote 接受；metadata 恒走仓 URL、content 走分体基址（见上文专节） |
| helmoci remote/virtual | **已实现（M13）**——代理与聚合读面齐备；`_external` 面仅 GET（HEAD → 405） |
| helmoci /v2 push-through | 未实现——virtual 写面一律 405（如实文案点名 defaultDeploymentRepo 目标） |
| 上游 tags/list 代理（helmoci remote） | 不做（M13 口径）——tags/list 只见已缓存 tag；`--version` 拉取不受影响 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'helm' is not available ...` | community 档 | 装 pro/enterprise license |
| 上传 403 + `X-Binflow-License-Required: helm` | license 过期/卸载后的写门；**下载/索引读不受影响** | 重装 license |
| `helm pull --verify` 报 `signature block not found` | `.prov` 不是 clearsign 两段式（如误用 detach-sign） | 按「provenance 验签」配方重产 `.prov` |
| 上传 `.tgz` 后 `helm search` 看不到 | 包无 Chart.yaml/坏 tar（跳过索引不拒传） | 修包重传；或 `helm package` 重新打包 |
| PUT index.yaml 403 | 索引是服务端生成物 | 用上传/删除/reindex 维护 |
| 虚仓建仓 400 `cannot mix the Helm and HelmOCI protocol families` | 成员家族混仓 | 拆成两个虚仓 |
| helmoci virtual 建仓 400 `cannot mix the helmoci and docker package types` | v2 家族成员须同包型（M13 收紧） | 成员全部换成 helmoci 型 |
| remote 建仓 400 `chartsBaseUrl … is not accepted` | 非 helm 包型的 remote 携带该字段（明拒优于暗弃） | 去掉该字段；分体基址只有 helm remote 有 |
| `helm pull oci://…` 404 `MANIFEST_UNKNOWN` | remote 未缓存且上游 404/不可达无副本；或 tag 未缓存 | 显式 `--version` 触发回源；核对上游存在性；看 `X-Binflow-Upstream-Error` 头 |
| 虚仓上 PUT 405 | 未配 `defaultDeploymentRepo` | 建仓时指定 local 成员 |

## 下一步

- 三类仓型通用语义：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 用 Chart 部署 BinFlow 本身：[Helm Chart（Kubernetes）安装](../install/helm.md)
- /v2 栈的协议细节（token 协商、断点续传）：[Docker / OCI 镜像接入](../docker-registry.md)
