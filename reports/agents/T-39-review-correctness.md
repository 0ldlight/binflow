# T-39 评审报告（视角: correctness）

- ticket: T-39 [P0] manifest 链：schema2/OCI 存取 + 校验链 + tag 语义（FR-9）
- 评审人: code-reviewer（正确性视角，双 reviewer 票之一）
- 结论: **REQUEST_CHANGES**
- 日期: 2026-08-18
- 输入: `reports/agents/T-39.md`、`internal/adapter/docker/manifest.go` + `manifest_test.go` + `handler.go`/`errors.go`/`blob_test.go`（fake 扩展）、`internal/httpapi/docker_manifest_test.go`、`internal/repo/service.go`（PutManifest/DeleteManifest/Resolve*）、`internal/metadata/migrations/sqlite/002_docker.sql`、T-32 T-39 节、docs/reverse/docker-registry.md §3/§4/§8/§10、PRD v1.1 FR-9

## 验证执行记录（只读取证）

```
go vet ./internal/adapter/docker/                       → 0
gofmt -l (cmd internal)                                 → 空
golangci-lint run ./internal/adapter/docker/...         → 0 issues
go test -race -count=1 -run TestManifest  ./internal/adapter/docker/  → ok 11.6s
go test -race -count=1 -run TestV2Manifest ./internal/httpapi/        → ok 14.7s
go test -race -count=1 ./internal/adapter/docker/       → ok 14.1s
```

真栈探针（`/tmp/binflow-t39rev`，独立 dataDir，127.0.0.1:18255/18256，Bearer token 全程）：

- **非规范化 JSON roundtrip**：手造 body（键序颠倒、双空格、tab、`é` 转义、472B）push 201 → GET **逐位一致**（`cmp` 通过），`Docker-Content-Digest` == 本地 sha256sum。
- **并发 20 路 PUT 同 tag**：20×201；终态 by-tag 恰好服务其中一个 manifest，body 与其自身 digest 对应字节一致，by-digest 取回相同 → 两写者竞态最终一致成立。
- **并发 PUT+DELETE+GET（10 轮 × 8 并发）**：GET 74×200 / 6×404（删除窗口），30/30 次 GET 的 body 与响应头 `Docker-Content-Digest` 逐一相符 → 无 tag/digest 漂移。
- **DELETE 级联**：删当前 digest 后 tag 404；删非当前 digest（tag 已被并发重指）tag 存活 → 级联按 digest 精确，非按 tag。
- **Accept 矩阵**：不相交 → 404 MANIFEST_UNKNOWN（GET 与 HEAD 一致）；`*/*` / `application/*` → 200；q 参数剥离。
- **uppercase hex 引用**：`sha256:<大写 hex>` GET 折叠后 200（与存储层折叠一致）；算法段大写（`shA256:`）→ 400 MANIFEST_INVALID（tag 文法拒绝，合理）。
- **shape 混淆**：index CT + image body → 400（"carries no manifests field"）；未知 CT 按 body 形状判读 → 201；subject digest 畸形 → 201 无 OCI-Subject 头（静默降级，符合 §3#8）。
- **发现缺陷**：重复 descriptor digest 的 manifest → **500**（详见 B1）。

## 必须修改（blocking）

### B1. 同一 manifest 内重复 descriptor digest → 500 UNKNOWN + 幽灵 manifest（假失败真发布）

- **位置**: `internal/adapter/docker/manifest.go:234-238`（refs 构造）× `internal/metadata/substores_docker.go:180-205`（PutRefs 逐行 INSERT）× `internal/metadata/migrations/sqlite/002_docker.sql:47`（`docker_refs` PK = `(repo_key, image, manifest_digest, blob_digest)`）。
- **问题**: adapter 按 descriptor 逐条构造 `DockerRef`（一个 layer 一行）。manifest 里同一 digest 出现两次时（同 layer 重复引用、`config.digest == layer.digest`、index 两个 child 同 digest、甚至空层 digest 列两次），`PutRefs` 的 INSERT 撞 UNIQUE 约束失败。而 `PutManifest` 的写入顺序是 node → manifests 行 → tag 指针 → refs（`internal/repo/service.go:779-827`），refs 是**最后一步**——于是：
  1. 客户端收到 **500 UNKNOWN**（spec 合法 body 被打 5xx；多层镜像/多阶段构建在现实中会重复引用同 digest）；
  2. **状态已被变更**：manifest 行 + tag 指针 + node 全部落盘。实测 `PUT layers:[d,d] → 500`，随后 `GET manifests/<tag> → 200` 且 body 与 push 原文逐位一致——**"失败"的 push 实际发布成功**，直接违反本票 AC 自己的「无幽灵 manifest」不变量与 `putManifest` 注释宣称的「every failure answers before any state changes」（manifest.go:112-113）；
  3. 该 manifest 的 refs 台账为空：GC 的「第二事实源」（架构 11.12）对它失真（M2 里被引 blob 另有 nodes 行兜底，暂不致误回收，但台账语义已坏）。
- **实测复现**（四行全中）: `layers:[d,d]` → 500；`config==layer` 同 digest → 500；index 同 child ×2 → 500；空层 digest ×2 → 500。服务端日志：`docker refs put row ...: constraint failed: UNIQUE constraint failed: docker_refs.repo_key, image, manifest_digest, blob_digest`。
- **建议改法**（最小面，adapter 侧去重——与 DDL 注释「one row per referenced blob」的台账语义一致，非绕过）:

  ```go
  refs := make([]*metadata.DockerRef, 0, len(parsed.refs))
  seen := make(map[string]struct{}, len(parsed.refs))
  for _, mr := range parsed.refs {
      hexPart, _ := parseDigestParam(mr.Digest)
      if _, dup := seen[hexPart]; dup {
          continue
      }
      seen[hexPart] = struct{}{}
      refs = append(refs, &metadata.DockerRef{BlobDigest: hexPart, ChildMediaType: mr.MediaType})
  }
  ```

  并在 `TestManifestValidationChain` / httpapi 集成各补一行：`layers:[d,d]` → 201（+ refs 台账恰一行）、空层 ×2 → 201。纵深防御（non-blocking）：`PutRefs` 改 `INSERT OR IGNORE` / `ON CONFLICT DO NOTHING`，使 store 对重集幂等。
- **为什么 blocking**: 真实客户端输入（合法 manifest）触发 5xx + 假失败真发布的状态分叉；测试矩阵（21 行失败表 + 17 集成）恰好无重复 digest 行，全绿掩盖了它。

## 建议改进（non-blocking）

1. **Accept q 权重未实现**（manifest.go:669）：`;q=0` 剥参数后视为接受。docker 客户端不发 q=0，行为可接受；在注释标注「q 值不参与」即可，避免后续 conformance 误判。
2. **Content-Type 未归一大小写**（manifest.go:491）：`Application/Vnd...` 按未知类型走 body-shape 判读并原样存储——透传裁定下自洽（push 什么存什么），但与 canonical 大小写的 Accept 协商会 404。无真实客户端如此发送；记档即可。
3. **manifest GET/HEAD 无 ETag**：blob 面有（=sha1），manifest 面无。spec 可选、逆向规格仅对 blob 要求；如后续想统一条件请求再补。
4. **HEAD manifest 无元数据快路径**：当前 `svc.Get` 打开 blob 后丢弃 body。逆向规格 §7 记录 Artifactory 的 `isHeadManifestEnabled` 快路径（仅元数据构响应）。纯优化，M2 不需要。
5. **DELETE 后 manifest body 仍可经 `/v2/<name>/blobs/<digest>` 取回**（实测 200）：blob node 不随 DeleteManifest 删除，回收归 GC——与 DE-14（无单 blob 删除）一致，属设计内；建议在 T-39.md 遗留节记一笔，防 QA 误报。
6. **并发覆盖零测试**：两套 suite 无任何 `go func`（blob 域有并发测试，manifest 域没有）。我的真栈探针证实最终一致成立（20 路 PUT 全 201、终态唯一、digest 与 body 30/30 相符），但这是服务层 SQLite 单写者 + upsert 的偶然而非被钉住的性质——建议补一个「并发 N 路 PUT 同 tag → 全 201、终态 by-tag body == 其 Docker-Content-Digest」的回归测试，防 store 换并发后端（Postgres 迁移，T-34 线）时回归。
7. **fakeService.PutRefs 语义弱于真 store**（blob_test.go:251-259）：fake 直接 append 全量，不带 PK 去重约束——这正是 B1 逃过单测的原因之一。修 B1 时给 fake 补上同款 PK 语义（重复 digest 只留一行），让两个夹具对齐。
8. **`schemaVersion` 为非整数（如 2.5）时报文**走 "manifest is not valid JSON" 分支（probe Unmarshal 到 `*int` 失败）——行为正确（400 MANIFEST_INVALID），文案略误导，可选改为 "manifest schemaVersion is not an integer"。

## 核对结论（按检查单）

1. **校验链顺序与完备**：①引用形状 → ②有界读体（MaxBytesReader 4MB，实测 400+零落盘）→ ③结构性解析（schema1/缺 CT/坏 JSON/字段缺失/schemaVersion:0 合法区分）→ ④digest 比对（by-digest 失配 DIGEST_INVALID）→ ⑤引用完整性（逐条在场 + 空层豁免 + index lazy；probe 非 miss 失败如实上抛）——顺序、短路、状态码均符合 T-32 AC① 与 docker-registry.md §3 高置信度部分；**唯落盘步（⑥）在 refs 台账上被 B1 打穿**。
2. **GET/HEAD 逐位一致**：非规范化 JSON 实测逐位一致；serving truth = manifests 行（CT/size），不重序列化；Accept 矩阵边界（缺省/`*/*`/`type/*`/多值/全畸形 token）行为合理；`Docker-Content-Digest`/api-version 头齐全。通过。
3. **tag 语义**：覆盖重指（by-tag 新、旧 by-digest 仍取，FR-9-AC2）；T-35 `TagRepointed` 契约由 service 层测试钉住（docker_test.go:404）；DELETE by-digest 202 → tag/refs 同事务级联（经真栈 + `Docker().GetTag` 直查双证）；by-tag 405 UNSUPPORTED + Allow + tag 存活。通过。
4. **并发**：最终一致实测成立（见上）；唯一缺口是缺回归测试（N6）。
5. **T-38 交接面**：`blobNodePath`/`manifestNodePath` 与上传落位（uploads.go:628）同布局，且有 `TestManifestNodePathLayout` + httpapi 布局测试双钉；B4 语义（挂账失败 5xx）在 `writeManifestPutError` default 分支落实（ERROR 日志 + 500 UNKNOWN）。通过。
6. **测试**：21 行失败表 + 17 集成顶层断言质量高（逐位 cmp、spec 码、零落盘断言、restart 持久化、refs 级联清零）；D08b 降级构造（curl 手造 amd64+arm64 index）与 buildx 产物形状等价（schemaVersion/manifests[]/platform/digest 结构全同）。**盲区一处：重复 descriptor digest（B1）**。

## 范围外发现（交 conductor）

- `docs/reverse/oss-structure.md` 为工作树未跟踪新文件（非本票产物，T-49 线），不属本评审范围，仅报备。
- T-39.md 遗留 3（同 digest 换 CT 重推导致 node mime 与 manifests 行 media_type 两源分歧）确认为真，但 M2 无现实触发路径，维持记档级别。

## 修复验收建议

B1 修复后需见：`go test -race ./internal/adapter/docker/ ./internal/httpapi/` 全绿 + 新增 4 行重复 digest 用例（dup layer / config==layer / dup index child / 空层×2 → 全 201、refs 台账行数正确）+ 一行真栈 curl 复测（重复 layer push → 201，GET by-tag 逐位一致）。
