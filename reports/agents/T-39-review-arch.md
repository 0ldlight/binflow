# T-39 架构一致性评审（manifest 链）

- 结论: **APPROVE**（0 blocking / 6 non-blocking + 1 条范围外交 conductor）
- 视角: consistency（架构分层 / 契约对照 / 规格依据）；正确性另有 reviewer
- 评审对象: commit `eef0d3f`（manifest.go 704 行 + 双测试文件 + 两处断言前移）
- 取证命令（均实际执行）:
  - `go vet ./internal/adapter/docker/` → 零输出；`gofmt -l internal/adapter/docker/` → 空
  - `go test -count=1 ./internal/adapter/docker/` → ok 12.123s
  - `go test -count=1 -run TestV2Manifest ./internal/httpapi/` → ok 10.731s
  - `go test -count=1 -race -run TestManifest ./internal/adapter/docker/` → ok 9.061s

## 一、R3 暂行口径执行面（重点 1）

**判定：实现形态与 PRD 字面一致，且「不白名单但验结构」的中间立场落地忠实。**

- `parseManifest`（manifest.go:511-552）：四种已知 CT 走专形；**未知 CT 不拒收**，按 body 自身形状判读（`manifests[]` → index 语义 / `config` → image 语义 / 皆无 → 400）。「不白名单」成立——未知 CT 的 201 行在验证表中有钉死用例（manifest_test.go:615-626，helm CT）+ 真栈往返（docker_manifest_test.go:473-506，serve-back CT 逐字一致）。「验结构」成立——纯透传会放过的「任意 JSON」在 `两者皆无 → 400` 分支被拦。
- PRD FR-9 原文「Content-Type 必须按 manifest 原始类型透传存储（……含未来新类型——透传不白名单）」：存储值 = `mediaTypeOfHeader(Header.Get)`（剥 `;` 参数后的裸 media type）。media type 语义上不含参数，剥参正确，但见 non-blocking #4 的注记建议。
- schema1 两 CT 拒收、缺 CT 拒收（透传没有值可存）均有表驱动用例。与 T-32 暂行口径四条逐条对上。

**与 architecture §5.3 的现实分歧（非本票缺陷，R3 未消歧的文档债）**：§5.3 校验链① 现文仍是「mediaType ∈ {四种}（其余 415/400）」，§6 DDL 注释仍是 `media_type TEXT NOT NULL, -- §5.3 四种之一`（architecture.md:593）。代码按 PRD 走、票据按暂行口径派发、实现者在 T-32/T-39 两处日志均上报——分歧链路完整可追溯，不构成本票 blocking；但 **R3 必须在 M2 收口前正式回写**，否则留下「代码 vs 架构文档」互相矛盾的死文本。正式消歧意见见第五节。

## 二、§5.3 校验链对照（重点 2）

实现三段与映射表校验链②③对应关系核对：

| §5.3 链 | 实现点 | 判定 |
|---|---|---|
| ② 逐 config/layer digest 查本 repo nodes 在场 | `childPresent` → `blobNodePath`，miss → 400 `MANIFEST_BLOB_UNKNOWN`（官方码，规格 §10 建议栏 1） | 符合 |
| ③ list/index 嵌套 manifest digest 同校验、只查在场性 | `isManifestFamilyMediaType` 四种 manifest/index 族改查 `manifestNodePath`；probe 只 `svc.Get` 判存在，**从不递归解析子 manifest** | 符合 lazy 口径 |

两个加分点：probe 的非 miss 失败（权限/store 故障）如实上抛渲染而非误报 BLOB_UNKNOWN（manifest.go:374-382）；空层 `sha256:a3ed95ca...` 豁免复用 T-38 常量。另有一个隐性鲁棒性：manifest 双 node 落盘使 index 子 manifest 即使 descriptor mediaType 未知/拼错也探测 blob 路径成功——mediaType 选路的失误被双行布局兜住。21 行验证表覆盖坏 JSON/缺字段/坏 digest/超限全部分支。

## 三、分层红线（重点 3）

**判定：无越界。** manifest.go 全部状态变更经 `repo.Service`：`svc.Put`（blob 面）/ `svc.PutManifest` / `svc.ResolveManifest` / `svc.ResolveTag` / `svc.DeleteManifest` / `svc.Get`。对 `storage`/`metadata` 的 import 逐点核查：

- `storage.BlobRef{Sha256: dgst}`（:219）——构造 `svc.Put` 签名的实参，类型经 Service 签名同源（§5.1 例外二原文口径），blob.go:103 已有 T-38 先例。
- `metadata.DockerRef`（:234-237）——`PutManifest` 签名参数类型，同上。
- `storage.ErrEngineClosed`（:411）——错误映射的 `errors.Is` 判定，uploads.go:671 T-38 先例同构（repo 未导出对应哨兵，见 non-blocking #6）。

不触 storage.Engine / Session / BlobLedger，例外一（上传会话）与例外二（台账只读）均未被 manifest 路径触碰。`manifestNodePath` 镜像 repo 未导出 helper 属有意的拼写重复，且 `TestManifestNodePathLayout` + httpapi 布局测试双向钉死。

双 node 布局（`<image>/blobs/<hex>` + `<image>/manifests/<hex>`）是 T-35 契约「先走普通 blob 路径」的字面落地 + 本票裁定，日志已记收益/代价；它偏离 §6 DDL 草图的单路径表述，且已成为 index 探测锚点与 FR-7-AC4 的测试钉死面——需 architect 追认（non-blocking #2）。

## 四、spec 错误码与 Accept 协商（重点 4）

- 三新码 `MANIFEST_UNKNOWN` / `MANIFEST_INVALID` / `MANIFEST_BLOB_UNKNOWN` 全部是官方码表码；`MANIFEST_BLOB_UNKNOWN` 用于 PUT 400（区分于 blob GET 面的 404 `BLOB_UNKNOWN`），采纳 §10 建议栏 1 而非 Artifactory 折进 MANIFEST_INVALID 的文案——与 PRD DE-08 一致。
- `DIGEST_INVALID` 双使用点（文法坏 / by-digest 失配）与 FR-9-AC4、DE-05 一致。
- Accept 协商：集合不含存储类型且非通配 → **404 `MANIFEST_UNKNOWN`**，无 schema2→1 转换——正是 §4 校准（#3 前半采纳、#5 不做）的字面落地。Accept 缺失 = 全收、纯畸形 token 集 = 无约束，是对官方「客户端应带 Accept」的温和补全，测试表覆盖。
- `OCI-Subject` 头：subject digest 良构才回、坏 subject 静默降级（§3#8 姿态）。一个偏差：§3#10 的条件是「OCI subject **且 repo 开 referrers API**」，BinFlow DE-15 明确不做 referrers 端点（404 + E-26），实现却无条件回头。oras 客户端会因该头认为支持 referrers、随后 404 回退 tag 模式——无害但头信号与端点能力不一致，见 non-blocking #3。
- 两处桩期断言前移（重点 5）：git diff 核实为最小改动（token_test +5 行 / v2_test 两表行），均注明「T-39 真实协议面」来源，且改后的断言锚定的是稳定协议答案（过 gate 后 400 MANIFEST_INVALID / MANIFEST_UNKNOWN / DIGEST_INVALID），非脆弱实现细节。回归面最小性达标。

## 五、R3 正式消歧意见（交 architect）

**建议裁定：采纳实现的中间立场为最终口径——Content-Type 透传存储（非白名单）+ 结构性验证；唯一显式拒收的 CT 家族是 schema1 两种（400 `MANIFEST_INVALID`）。不建议裁回白名单。**理由：

1. **Helm/生态前瞻兼容是硬需求**（PRD FR-9 明文 + FR-12 依赖）：白名单直接挡掉 cosign 签名 manifest（`application/vnd.dev.cosign.simplesigning.v1+json`）、旧版 oras artifact manifest（`application/vnd.cncf.oras.artifact.manifest.v1+json`）与未来 `artifactType` 生态类型——全是真实客户端在今天就会推的 CT。注意 Helm 本体的 manifest CT 其实是标准 OCI manifest（helm 特有 CT 在 config/layer descriptor 上），真正吃透传的是 cosign/oras 这一类。
2. **结构性验校封住了纯透传的口子**：未知 CT 不是直接 201，而是套用同一条结构校验（body 形状判读），校验链②③的在场性探测因此对透传类型依然全部生效——链①的白名单职责（防任意 JSON 假 manifest）已由结构判读接管，白名单没有增量安全收益。
3. Artifactory 的「未知 CT 兜底按 Schema1Signed → blocked」是 schema1 时代的防御性兜底，属被规格明确抛弃的行为，不具参考价值。
4. **回写动作清单**（一张小票）：§5.3 校验链① 改为「四种已知类型走专形解析；未知类型透传存储并按 body 结构判读：`manifests[]` → index、`config` → image、皆无 → 400；schema1 两 CT → 400」；§6 DDL 注释 `-- §5.3 四种之一` 同步改为「客户端透传值」；顺带追认双 node 布局与「`manifests[]` 优先于 `config`」的判读序（当前实现顺序，未见于任何文档，见 non-blocking #5）。

## 六、遗留三条备查的逐条判断（重点 6）

1. **同 digest 换 CT 两源 mime 分歧**：记录即可。serving truth 是 manifests 行（稳定，不受重推影响），item info 的 node mime 是展示面；病态前提（同字节换 CT）M2 客户端不产生。若要根治属 repo.putNode 幂等分支的 mime 刷新策略（T-35 面），不是本票。
2. **schemaVersion 值域**：记录即可。distribution 参考实现同样只验结构不验值域；T-43 conformance 若暴露客户端依赖再加。
3. **防御性 tag 指向已删分支（`t.Digest == ""`）**：记录即可，且这是正确的防御编码——repo api.go:275 明文承诺该状态可观测（「empty digest → the manifest was deleted」），adapter 必须总态处理。正常流不可达依赖 store 同事务级联，QA 用例已按 §11.12 要求覆盖。

## 必须修改（blocking）

无。

## 建议改进（non-blocking）

1. **[architect 回写票]** architecture.md:450 校验链① 与 :593 DDL 注释仍是四类型白名单陈旧文本，与代码/PRD 矛盾——按第五节意见回写（R3 正式消歧）。
2. **[architect 追认]** 双 node 布局（`blobs/<hex>` + `manifests/<hex>`）偏离 §6 草图单路径表述，且已是 index 探测锚点 + FR-7-AC4 测试钉死面——需在 §5.3/§6 追认为正式布局（含「DELETE manifest 后 blob node 存活、manifest GET 404 而 blob GET 仍 200」的既定语义）。
3. **[QA/T-43 观察]** `OCI-Subject` 无条件返回 vs §3#10 的「repo 开 referrers API」条件——头信号与 DE-15（referrers 404）能力不一致；oras 会安全回退，建议或加 PRD 注记或改为条件返回（倾向注记，改条件反而伤 oras 推送体验）。
4. **[文档注记]** `mediaTypeOfHeader` 剥 `;charset` 参数：push 带 `; charset=utf-8` 时 serve-back 是裸 media type——语义正确（media type 不含参数），但与「透传」措辞的字面差异有误导空间，消歧票里补一句「透传对象 = 裸 media type（剥参数）」。
5. **[消歧票钉死]** 未知 CT 且 body 同时含 `manifests[]` 与 `config` 时按 index 判读（manifest.go:544 的优先序）——OCI 1.1 无此形态的合法用例，属 edge case，但判读序目前只存在于代码里，应进 §5.3 文本。
6. **[范围外 → conductor]** `storage.ErrEngineClosed` 在 adapter 错误映射直接引用（manifest.go:411，uploads.go:671 先例同构）：建议 repo 导出 shutdown 哨兵（`repo.ErrUnavailable` 之类）让 adapter 错误映射彻底脱离 storage 类型名，独立 dev-go-core 小票，不塞 T-39/T-40。

## clean-room 抽查

无逐行对应嫌疑：命名/结构无反编译侧对应物（无 `ManifestType#from`/`chooseManifestType`/`uploadManifest` 类 Java 形迹）；行为依据全部锚定官方 [DIST-API]/[OCI] 码表与 docker-registry.md 行为规格（规格文档本身经 clean-room 流程产出）；Go 习语与注释引用的是公开规范而非反编译代码路径。
