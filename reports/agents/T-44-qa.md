# QA 报告 T-44 — 五客户端 conformance + 性能（PRD §8 剧本 6/8，FR-13）

- role: qa-engineer
- 日期: 2026-08-19
- 票据: T-44 [P0]（AC 全文见 reports/agents/T-32.md T-44 节）
- 验收口径: docs/prd/milestone-2.md **v1.2**（D 序列 name 全名 `<repoKey>/<image>` 形态，C1 已遵守）
- 被测对象: commit **c397d46**（`chore: sprint 136 - T-54 done`），独立 git worktree 构建，`binflow-server` 16.75 MB
- 环境: darwin/amd64；Docker Desktop daemon 29.7.2（x86_64 VM，经代理可拉公网镜像）；**全部客户端以容器形态执行**（宿主二进制全缺，见 §1）
- 实例: A=`:64750` 匿名开（默认配置）/ B=`:64751` 匿名关（挑战模式）——双实例贯穿全票，用于分离两种认证形态下的客户端行为
- 容器到宿主: LAN `192.168.1.70` 与 `host.docker.internal` 双路径均通

## 总结论: **FAIL（AC① docker P0 未全过——三个 P0 缺陷 D44-1/2/3 互相咬合，阻断默认配置下的 docker push / buildx push / helm push；性能与其余客户端全绿）**

- §5.3 P0 成员: **3/4 过**（podman/crane/oras 过，docker 挂）；P1 skopeo 过；P2 helm push 挂（记录不阻塞）；conformance suite 三组 55/60。
- 性能三项全绿：D20 冷启动 0.039s（<2s）；NFR-P7 100 并发 100×exit0 零 5xx；NFR-P6 已记录。
- 全程服务端日志 5xx 计数 = **0**（双实例）。

---

## 1. 环境盘点（分级执行面据实裁定）

| 工具 | 宿主 | 容器化 | 采用 |
|---|---|---|---|
| docker CLI 29.7.2 + daemon | CLI 有，daemon（Docker Desktop）无 insecure-registries 且不动用户配置 | docker:27-dind（daemon 27.5.1，容器内自由配 `--insecure-registry`） | **容器内 daemon（dind）**，两台（t44-dind / 全新 t44-dind2） |
| podman | 无 | quay.io/podman/stable:latest（v5.x，crun） | 容器化（--privileged） |
| crane | 无 | gcr.io/go-containerregistry/crane:debug | 容器化 |
| oras | 无 | ghcr.io/oras-project/oras:v1.2.0 | 容器化 |
| skopeo | 无 | quay.io/skopeo/stable:latest | 容器化 |
| helm | 无 | alpine/helm:3.16.4 与 3.17.2 | 容器化 |
| buildx | 无 | dind 内置 v0.20.1（docker-container driver） | dind 内 |
| conformance suite | — | ghcr.io/opencontainers/distribution-spec/conformance:v1.1.0（公网经代理可得 → **实跑**，不走降级） | 容器化 |

- 代理环境（http.docker.internal:3128）可拉公网镜像，R11 降级路径本票未触发（除 buildx push 腿，见 §4.6）。
- T-37/T-39 的「本机 daemon 无 insecure-registries」阻塞在 dind 路径下解除——派单指定的首选路径成立。

## 2. 核心发现：三个 P0 缺陷互相咬合，docker 认证两模式皆断

### D44-1 [P0] 匿名开（默认配置）时 `/v2/` ping 200 无挑战 → ping-缓存型客户端 push 全断、login 空洞

- **现象（docker daemon，A 实例）**: `docker login` → `Login Succeeded`（exit 0）；`docker push` → blob 上传段 401 重试 5 轮后 `unauthorized` 失败。服务端日志：**零次** `/v2/token` 请求——daemon 收到 401+Bearer 挑战后从未去 realm 换 token。
- **波及面**: docker daemon、skopeo、podman 的 **push**（containers/image 与 moby 同为 ping-缓存模型：ping 200 → 判定免认证 → 后续 401 不协商直接报 unauthorized）。skopeo/podman push 到 A 实例实测同样失败（`writing blob: initiating layer upload ... unauthorized`）。
- **加重项**: A 实例上 `docker login` 用**错误口令**也返回 `Login Succeeded`（exit 0）——ping 200 使客户端跳过凭据校验，FR-11-AC4 的意图在默认配置下被掏空（服务端全程未收到该 login 的任何请求）。
- **根因**: PRD DE-01 定案「匿名开 → ping 200 `{}`」。真实世界（Docker Hub/GHCR/Harbor）即便匿名 pull 可用，ping 对未认证请求仍 401+挑战，匿名访问经**匿名 token** 通行（BinFlow `/v2/token` 已支持匿名 pull token 签发，T-43 2.11）。moby/containers-image 的认证器只认 ping 时缓存的挑战，ping 无挑战 = 永不携带凭据。
- **修法方向（architect/PM 裁定）**: 未认证 ping 一律 401+Bearer 挑战（匿名读改走匿名 token——客户端原生支持）；或 PRD 收紧「docker push 场景必须 anonymous_access=false」并写文档（体验降级）。**建议前者**。
- **归属**: T-33/T-37 路由与挑战域 + PRD DE-01/FR-11-AC2 勘误。

### D44-2 [P0] `/v2/token` GET 拒绝 `offline_token=true`（400）→ 挑战模式下 docker 29 login 失败

- **现象（B 实例，匿名关）**: `docker login` → daemon 走挑战协商 → `GET /v2/token?...&offline_token=true`（Basic admin）→ BinFlow 400 `{"error":"invalid_request","error_description":"offline_token is not supported"}` → login 失败：`error parsing HTTP 400 response body: no error details found...`。
- **对照**: podman/oras/crane（不发该参数）在同实例 login 全成功——证明挑战模式本身可用，独 docker daemon 被 400 拦截。
- **根因**: PRD v1.1 §6.5② 定案「offline_token=Artifactory 私有扩展，400 拒绝」。**该定案与官方 distribution token spec 冲突**：官方 spec 明文定义 `offline_token` 参数且「server MAY ignore」；docker daemon（29.x CLI 实测）在 login 的 token GET 中必带 `offline_token=true`。逆向规格 §5.2 也记录 Artifactory「接受但走同一 provider」。
- **修法**: 接受并忽略该参数（返回普通 token）+ PRD §6.5②/FR-11-AC7 回写。一行级改动。
- **归属**: T-37 token 域 + PM 勘误。

### D44-3 [P0] `/v2/token` POST 的 OAuth 表单凭据被忽略 → 签发匿名 token → Bearer 被拒（buildx/helm push 断）

- **现象**: `POST /v2/token`（form: `grant_type=password&username=admin&password=***&service=binflow&scope=...`，无 Basic 头）→ 200 返回 64B token——**但该 token 是匿名 scope**（用它 POST uploads → 401，服务端日志 `token owner disabled`——T-37 设计：匿名 token 不得作 Bearer）。
- **波及面**: buildkit（`docker buildx build --push`，containerd 解析器）与 helm 3.16/3.17 push——两者都以 OAuth POST 表单传凭据。实测：buildx push `unauthorized`；helm push 401。oras CLI v1.2.0（同族库但用 Basic POST）成功——反证服务端 Basic 路径完好。
- **根因**: 官方 distribution token spec 的 OAuth 流（grant_type=password）凭据在 **form body**；T-37 实现只读 Basic 头。
- **修法**: POST 路径无 Basic 时解析 form 的 username/password（两者都无才按匿名）。
- **归属**: T-37 token 域。

### 咬合关系与最小修复面

修 **D44-2** 即可打通「匿名关」实例的 docker daemon 全链（podman 已实证挑战模式全通）；修 **D44-1** 可打通默认配置全客户端；修 **D44-3** 补齐 buildx/helm。三个都是 token/ping 层小改动 + 两处 PRD 勘误。

## 3. §5.3 分级矩阵结果（AC①）

「全过」= 操作退出码 0 且服务端无 5xx（全程 5xx=0 满足）。

| 客户端 | 分级 | 操作明细 | 结果 |
|---|---|---|---|
| docker | P0 | D05 login（A：exit 0 但**空洞**——错口令同样 Succeeded；B：**失败**，D44-2）；build（exit 0）；push（A/B **均失败**，D44-1/D44-2）；rmi→pull→run（**绿**，`docker run --rm ... echo ok` → `ok` exit 0）；**全新 daemon**（t44-dind2 空镜像表）pull→run `ok` exit 0；manifest inspect `--insecure` exit 0，config digest 与镜像 ID 一致，pull 输出 digest `ec7ca0b6…` == 服务端 DCD == crane push digest（三方一致）；logout exit 0 | **FAIL**（login 语义 + push） |
| podman | P0 | login（A/B 均 `Login Succeeded!`）；pull（A 匿名 / B 认证均过）；tag+push（**B 绿**：manifest 写入成功；A 挂 D44-1）；run：容器内 crun exec EINVAL（**嵌套容器环境限制**，非仓库问题）→ 等效证明 `podman save` → dind `docker load` → `run echo ok` 输出 `ok` exit 0 | **PASS**（注记：push 仅挑战模式；run 为等效路径） |
| crane | P0 | `crane digest`（`ec7ca0b6…`）；`crane manifest`（合法 schema2 JSON）；`crane ls`（v1/v1-copy 字典序）；`crane copy` 实例内两 repo（docker-local→docker-local-2，digest 逐一致，exit 0）——另承担播种与 index 推送工具角色 | **PASS** |
| oras | P0 | D15（FR-12）：`oras login --plain-http` → `Login Succeeded`；`oras push` charts/myapp:1.0.0（helm config media type，exit 0，digest `6c7be0b9…`）；`oras pull` 空目录 + `cmp` **逐位一致**（sha256 `f354cc5d…` 双侧同值）；`oras repo tags` → `1.0.0` | **PASS** |
| skopeo | P1 | D19（B 实例）：`skopeo inspect`（Digest=`ec7ca0b6…` 合法 JSON）；copy docker://→docker-archive:（app.tar 落盘）；copy 反向 docker-archive:→docker://docker-local-2/skopeo/app:v1（成功，服务端 DCD `65c199b0…`）。注：push 到 A 挂 D44-1（containers/image 同源） | **PASS**（注记） |
| helm OCI | P2 观察 | 3.17.2：`helm show chart` / `helm pull`（helm 规范 media type artifact：config=`…helm.config.v1+json` + layer=`…helm.chart.content.v1.tar+gzip` 经 oras 推入）→ 绿，pull 产物 cmp **逐位一致**；`helm registry login` **无 --plain-http 旗标**（3.16/3.17 均无）+ push 撞 D44-3（匿名 token 被拒）→ chart push 失败 | **push 挂 / pull 绿**（P2 记录不阻塞；PRD D15 字面命令的 layer media type 用了 helm **config** 类型——helm 客户端不认，PRD 命令宜改双类型形态，见勘误 C4） |
| buildx 多架构 | P1 | `docker buildx create`（builder 内置多平台）+ `--platform linux/amd64,linux/arm64` 构建 **exit 0**（真实 buildx 产物）；push 腿撞 D44-3 → **降级**：`--output=type=oci` 导出 → `crane push --index`（真实客户端推送）→ 服务端 index（`oci.image.index`）含 linux/amd64+linux/arm64+2 attestation，子 manifest by-digest 全 200；dind `docker pull` 选 amd64 → `run echo ok` exit 0 | **等效通过**（push 腿降级注记；协议级 index 语义 T-39 已验） |
| conformance suite | P2 | **实跑**（非降级）：AC 三组（pull/push/content-management）**55 过 / 5 挂**；全四组 66 过 / 9 挂（+4 挂全为 referrers API——DE-15 设计内不做）。5 挂见 D44-4/5/6 | **跑通未全绿**（P2 记录） |

## 4. 关键证据摘录

### 4.1 docker daemon push 失败（A，D44-1）

```
$ docker push 192.168.1.70:64750/docker-local/acme/app:v1
The push refers to repository [192.168.1.70:64750/docker-local/acme/app]
08bc4e534116: Retrying in 5 seconds ... （5 轮退避）
unauthorized
（服务端日志：POST /v2/.../blobs/uploads/ 401 user=anonymous 反复；/v2/token 零请求）
```

### 4.2 docker login 失败（B，D44-2）

```
$ docker login 192.168.1.70:64751 -u admin --password-stdin
Error response from daemon: Get "http://192.168.1.70:64751/v2/": error parsing HTTP 400
response body: no error details found in HTTP response body:
"{\"error\":\"invalid_request\",\"error_description\":\"offline_token is not supported\"}\n"
（服务端日志：GET /v2/token status=400 user=admin ×2）
```

### 4.3 表单凭据被忽略（D44-3，curl 复现）

```
$ curl -s -X POST ".../v2/token" -d "grant_type=password&...&username=admin&password=***"
{"token":"<64B>","issued_at":"..."}        # 200，但为匿名 scope
$ curl -X POST -H "Authorization: Bearer <该token>" .../v2/.../blobs/uploads/
401                                        # 服务端: token owner disabled
```

### 4.4 通过路径（节选）

```
podman push (B):  Writing manifest to image destination  → PODMAN_PUSH_OK
crane copy:       docker-local/acme/app:v1 → docker-local-2/copy/app:v1  digest ec7ca0b6… 一致
oras:             Pushed charts/myapp:1.0.0 → pull → cmp IDENTICAL（sha256 f354cc5d… 双侧同）
docker run:       docker run --rm .../app:v1 echo ok  →  ok（exit 0；全新 daemon 同）
helm pull:        Digest: sha256:a0876797… → myapp tgz cmp IDENTICAL
buildx:           platform linux/amd64,linux/arm64 → OCI layout → crane push --index → docker pull/run ok
```

## 5. 性能（剧本 8，AC③）

| 项 | 门槛 | 实测 | 结果 |
|---|---|---|---|
| D20 冷启动（空库裸二进制，含 /v2 路由） | <2s | run1 **0.039s** / run2 **0.010s**（`/v2/` 200 `{}`） | ✅ |
| NFR-P7 100 并发 pull（`xargs -P100 crane pull`） | 全 exit 0、零 5xx | **100/100 exit 0**，100 个 tarball md5 逐一同值，抽查可 `docker load`；服务端 5xx=0 | ✅ |
| NFR-P6 吞吐（100MB 样本，记录不设门） | — | docker 路径：PUT 201 **33.1 MB/s**（3.17s）/ GET **360.9 MB/s**；generic 路径：PUT **65.3 MB/s** / GET **309.4 MB/s**；双路径 roundtrip `cmp` 一致 | 记录（**观察 O-1**：docker PUT 约为 generic 一半，超 ±20% 参考带——疑 session 中转写放大；NFR-P6 明言硬门归 M5/GA，不判 FAIL，建议 M5 基准时排查 sessions/ 中转成本） |

## 6. conformance suite 5 挂明细（三组内，均 P2）

| # | spec 断言 | 实际 | 定性 |
|---|---|---|---|
| D44-4 [P2] ×3 | GET/HEAD 不存在 manifest（**超长 tag**，134 字符）应 404 | **400**（tag 长度/字符集校验在 GET 路径也生效；手工复核普通不存在 tag 是 404，仅超长/非法首字符触发 400） | GET/HEAD 侧非法 ref 宜归 404 `MANIFEST_UNKNOWN`（PUT 侧 400 正确保留） |
| D44-5 [P2] | 上传状态 GET 应 204 + Range + **Location** | 204 + Range + Docker-Upload-UUID，**缺 Location 头**（T-43 3.9 只断言了 Range） | 补 Location 响应头（ PATCH/PUT 的 202/201 均已带） |
| D44-6 [P2] | 删最后一个 manifest 后 tags/list 应 200 空表 | **404 NAME_UNKNOWN**（manifest 行删光 → name 判不存在；手工复现：202 删除后 GET tags/list 404） | PRD DE-12/FR-10-AC3 现行口径（name 不存在→404）与 OCI conformance 期望（name 持续存在、tags 空）冲突——**PRD 校准项**，与 T-52 零 tag 契约同域 |

另有 4 挂为 referrers API（content-discovery 组，非 AC 三组）：DE-15 设计内不做，不判缺陷。

## 7. AC 逐条判定

| # | 验收标准 | 结果 | 证据 |
|---|---|---|---|
| ①a | docker（D05/D08 login→build→push→rmi→pull→run）P0 全过 | ❌ | login 空洞（A）/失败（B）、push 双模式断（D44-1/2）；build/rmi/pull/run/inspect/logout 全绿；§3、§4 |
| ①b | podman（D17）P0 | ✅（注记） | B 实例 login/pull/push 全 0；run 经 save→load→run 等效 `ok`（嵌套 crun 限制） |
| ①c | crane（D18）P0 | ✅ | digest/manifest/ls/copy 两 repo 全 0，digest 一致 |
| ①d | oras（D15，FR-12）P0 | ✅ | push/pull/tags 全 0 + cmp 一致 |
| ①e | skopeo（D19）P1 或注明补救 | ✅（注记） | inspect/双向 copy 全 0（B 实例；A 受 D44-1 波及——同因已记） |
| ①f | helm OCI P2 观察 | 记录 | pull/show 绿 + cmp；push 挂（客户端无 plain-http login + D44-3）；不阻塞 |
| ①g | buildx 多架构 D08b（P1 视环境） | ✅ 等效 | 真实 buildx 双平台构建 + crane --index 推送 + 子 manifest 可取 + docker run ok；push 腿降级注记（D44-3） |
| ①h | conformance suite 三组（FR-13-AC6） | 跑通未全绿 | 55/60（5 挂见 D44-4/5/6，P2）；suite 经代理离线可得故**未走降级**、实跑留档 |
| ②a | D08 pull 至全新 daemon 后 run 输出 ok | ✅ | t44-dind2（空镜像表）pull → `docker run --rm … echo ok` → `ok` exit 0 |
| ②b | manifest digest 与 docker manifest inspect 一致 | ✅ | 三方一致：pull 输出 == 服务端 DCD == crane push == `ec7ca0b6…`；inspect `--insecure` exit 0 |
| ②c | D11 去重（stats 不变）+ 跨协议变体 | ✅ | crane copy 同内容新 tag：blobs **3→3**；跨协议变体 T-43 3.32 已过（curl 级）；docker 客户端变体被 D44-1 阻，等效覆盖 |
| ②d | D15 tgz `cmp` 一致 | ✅ | sha256 `f354cc5d…` 双侧同值 + `cmp` IDENTICAL（oras 与 helm 两客户端） |
| ③a | D20 冷启动 <2s（含 docker 路由） | ✅ | 0.039s / 0.010s |
| ③b | NFR-P7 100 并发 pull 零 5xx | ✅ | 100×exit0、5xx=0、产物逐一同值 |
| ③c | NFR-P6 吞吐记录 | ✅ | 见 §5（附观察 O-1） |
| ③d | 报告归档 + DoD §9 第 1/2 条结论 | ✅ | 本文件 + §8 |

## 8. DoD §9 第 1/2 条判定（合并 T-43）

| 项 | 判定 | 依据 |
|---|---|---|
| DoD-1（§4 全部 P0/P1 AC 经 qa 全绿） | **未满足** | FR-13-AC1（docker D16 全链）❌、FR-11-AC1（D05 login，默认配置下语义空洞）❌、FR-11-AC4（默认配置下错口令 login 成功）❌；FR-13-AC2/AC3/AC4/AC5 ✅、FR-12-AC1/AC2 ✅、FR-11-AC3/AC6（curl 级，T-43）✅。合并 T-43：其余 P0/P1 AC 绿（T-43 附 D1/D2/D3 三个 P2 + F1 flake 待收口） |
| DoD-2（§8 剧本全绿 + §5.3 P0 全过 + skopeo 过/补救） | **未满足** | 剧本 6（五客户端）❌（docker P0 挂）；剧本 8（性能）✅；skopeo ✅；剧本 1-5/7/10 ✅（T-43） |

**修复面收敛度**: D44-1/2/3 全部位于 /v2 ping 与 /v2/token 两处（预估 3 个小改动 + PRD 两处勘误 DE-01/§6.5②）；修复后建议只复验 T-44 §3 docker 行 + conformance 三组，无需重跑全矩阵。

## 9. PRD 口径勘误（转 conductor → PM/architect）

| # | 项 | 建议 |
|---|---|---|
| C4 | D15 字面命令 `oras push … ./x.tgz:application/vnd.cncf.helm.config.v1+json` 把 helm **config** 类型用作 **layer** 类型——helm 客户端不认（实测报 `manifest does not contain a layer with mediatype …helm.chart.content.v1.tar+gzip`） | D15 改双类型形态：`oras push … --config chart.json:…helm.config.v1+json x.tgz:…helm.chart.content.v1.tar+gzip`（本票已按此形态补验 helm pull 绿） |
| C5 | §6.5②「offline_token=Artifactory 私有扩展 → 400」与官方 distribution token spec 冲突（参数官方定义、server MAY ignore），且 docker daemon 实发 | 定案改「接受并忽略」；FR-11-AC7 断言随之改（D44-2） |
| C6 | DE-01「匿名开 → ping 200 {}」与真实客户端 ping-缓存认证模型冲突（Docker Hub/GHCR/Harbor 均挑战） | architect/PM 裁定 ping 挑战化（匿名经匿名 token）或文档化「push 需匿名关」（D44-1） |
| C7 | FR-10-AC3/DE-12「name 不存在 → 404」在「删光最后一个 manifest」场景与 OCI conformance 期望（200 空表）冲突 | 与 D44-6 一并校准（name 持续性语义） |

## 10. 环境与清理

- t44-dind / t44-dind2 已删；实例 A/B 已停；`git worktree` 已移除；`/tmp/t44` 已删；测试拉取的 8 个客户端镜像已 `docker rmi`（用户既有镜像缓存未动）；宿主 daemon.json / Docker Desktop 配置零改动。
- 测试口令一次性随机值，仅存在于已删除的 /tmp/t44/env.sh，未入任何提交文件。
- 主仓工作树未动（QA 全程在 /tmp/t44/src worktree @ c397d46）。

## 11. 方法论留档

- 容器化客户端凭据需持久化（挂卷 /root/.docker），跨 `docker run` 的 login 不共享——首轮 crane「UNAUTHORIZED」为 harness 误报。
- crane:debug 的 ENTRYPOINT 是 crane 本体，脚本化需 `--entrypoint sh`。
- dind 启动有 TLS 弃用警告减速（~15s），就绪探测别只等 5s。
- buildkit 对 plain-HTTP 上游需 builder 级 `buildkitd.toml`（`[registry."host:port"] http=true`），daemon 的 insecure-registry 不覆盖 builder 容器。
- conformance suite 必须显式给 `OCI_CROSSMOUNT_NAMESPACE`，否则 cross-mount 组假挂（本票三轮对照后确认）。
