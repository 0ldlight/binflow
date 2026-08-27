---
title: Helm Chart 仓库接入
sidebar_position: 28
---

# Helm Chart 仓库接入（经典 chart 仓）

> 适用版本：M11（helm 包型为 **pro 档**能力——建仓/上传需 pro 及以上 license，见 [License 与 Add-ons 管理](../admin/license.md)；未解锁时既有 chart 仍可 `helm pull`/`install`）。
> 验证客户端：**helm 4.2.4** + gpg 2.5.21 实测（T-309/T-313：package / repo add / update / search / show / pull / template / `--verify` 签名链 / kind 集群 install `STATUS: deployed`）。行为基准 `docs/reverse/helm.md`。
> 命名辨析：本文是 **helm 经典 chart 仓**（`index.yaml` + `.tgz`，包型 `helm`）；用 Helm Chart **部署 BinFlow 本身**见 [Helm Chart（Kubernetes）安装](../install/helm.md)；OCI 形态的 chart（`helm push` 到 oci:// 地址）走 docker 包型的 OCI 面（见 [Docker 接入](../docker-registry.md)——oras 承载）。

BinFlow 的 helm 仓 = 标准 chart repository：上传 `.tgz` 自动解析 Chart.yaml 并**重算 `index.yaml`**，客户端 `helm repo add` 后即获得搜索/安装能力。**local / remote（代理）/ virtual（聚合）三类仓型齐备**。

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
{ cat mychart/Chart.yaml; echo; printf 'files:\n  %s: sha256:%s\n' \
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
- **URL 改写**（聚合 index 的 `urls` 指向本虚仓可服务的地址）：
  - 指向成员仓基址的 URL → 改写为成员相对路径；
  - 指向**外部 http(s) host** 的 URL → 折叠为 `_external/<scheme>/<host>/<path>`（`://` 折叠为 `/`；经虚仓的受控 egress 取数，响应头 `X-Binflow-Upstream` 标上游）；
  - 上游自身的 `_external` 面 → `_transitive/<...>`；
  - `oci://` 条目保留原样（客户端直连 OCI 仓）。
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

## 边界与有意不做（M11）

| 项 | 行为 |
|---|---|
| PUT `index.yaml` / `.index/**` | **403**（服务端生成的索引不可手写） |
| 聚合 index 缓存 | 按请求现算（成员变更下一请求即见）；`.index` 路径仍 403 防写 |
| namespace 模式（useNamespaces） | 未实现（规格默认关） |
| 分体 `chartsBaseUrl` 配置位 | 未实现（回源基址恒 = 仓 URL） |
| helmoci 包型 | 另有槽位规划（当前虚仓混仓校验已就位待其落地） |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| 建仓 400 `package type 'helm' is not available ...` | community 档 | 装 pro/enterprise license |
| 上传 403 + `X-Binflow-License-Required: helm` | license 过期/卸载后的写门；**下载/索引读不受影响** | 重装 license |
| `helm pull --verify` 报 `signature block not found` | `.prov` 不是 clearsign 两段式（如误用 detach-sign） | 按「provenance 验签」配方重产 `.prov` |
| 上传 `.tgz` 后 `helm search` 看不到 | 包无 Chart.yaml/坏 tar（跳过索引不拒传） | 修包重传；或 `helm package` 重新打包 |
| PUT index.yaml 403 | 索引是服务端生成物 | 用上传/删除/reindex 维护 |
| 虚仓建仓 400 `cannot mix the Helm and HelmOCI protocol families` | 成员家族混仓 | 拆成两个虚仓 |
| 虚仓上 PUT 405 | 未配 `defaultDeploymentRepo` | 建仓时指定 local 成员 |

## 下一步

- 三类仓型通用语义：[remote / virtual 仓库管理](../admin/remote-virtual.md)
- 用 Chart 部署 BinFlow 本身：[Helm Chart（Kubernetes）安装](../install/helm.md)
- OCI 形态 chart（oras push）：[Docker / OCI 镜像接入](../docker-registry.md)
