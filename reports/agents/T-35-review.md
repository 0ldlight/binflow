# T-35 评审报告（视角: correctness）

结论: **REQUEST_CHANGES**

- ticket: T-35 [P0] repo：docker 仓库类型启用 + docker 元数据用例编排（FR-7）
- 评审范围: `internal/repo/{api,service,validate,doc}.go`、`docker_test.go`、`fakes_test.go`、`repo_test.go`（commit ccd4577），对照 internal/metadata DockerStore（T-34）、docs/design/architecture.md §5.1/§5.3/§6/§11.12、docs/reverse/docker-registry.md §6/§8、ADR-0001/0010
- 评审取证命令（均实际执行）:
  - `go build ./...`、`go vet ./internal/repo/`、`gofmt -l internal/repo/` → 零输出
  - `go test -count=1 ./internal/repo/` → ok 9.0s
  - 临时探针（跑完即删，未入库）：
    - `-race` 下 8 writer 并发 PutManifest/DeleteManifest + 2 reader ListTags/ResolveTag（200 轮）：无 data race；终态不变量「每条 tag 行指向存活 manifest 行」成立
    - PutManifest vs DeleteRepo 并发：manifests/tags/nodes 由 FK 兜底（无 ghost、错误形态正确），但证实 docker_refs 泄漏（见 B1）
    - 同 digest 重推（无授权 alice）：证实 manifest 行 provenance/媒体类型被改写、TagRepointed 与文档相悖（见 B2）

---

## 必须修改（blocking）

### B1. DeleteRepo 拆库顺序存在 docker_refs 永久泄漏窗口

`service.go:1055-1071`（DeleteRepoDocker）与 `service.go:1253`（DeleteRepo 调用点）。

拆库顺序为 `DeleteImage(逐 image) → DeleteRepoRefs → Repos().Delete`。并发 `PutManifest` 落在 `DeleteRepoRefs` 之后、`Repos().Delete` 之前时：其 node/manifests/tags 写入会被 repositories 行删除的 FK 级联清掉（已用探针证实，含 nodes 的 FK 报错形态），但 **`PutRefs` 写入的 docker_refs 行没有任何 FK**（architecture §6 明示 docker_refs 不设 FK），将在仓死后永久存活：

- GC mark 集合 = `nodes ∪ docker_refs`（§6 迁移 002 注记 / §11.12 / T-42 AC②），泄漏行永久 pin 住 blob，永不可回收；
- 同名 repo 重建后，泄漏 refs 归入新仓名下，新仓从出生即带幽灵引用。

探针实测：`DeleteImage → DeleteRepoRefs → PutRefs → Repos().Delete` 之后 `RefsByBlob(repoKey, layer)=true`（泄漏成立）；且 **`Repos().Delete` 之后再调 `DeleteRepoRefs`（按字面 repo_key 的普通 DELETE）可正常清掉**——修复路径已验证可行。

建议改法（二选一）：
1. 把 `DeleteRepoRefs` 移到 `Repos().Delete` **成功之后**再执行（顺序变为：DeleteImage（manifests/tags，保持三表同动）→ Repos().Delete → DeleteRepoRefs 收尾清扫）；`Repos().Delete` 失败时 refs 与仓行同存，重试自愈——比现在「失败即已掏空 refs」更一致。
2. 或在 `Repos().Delete` 成功后无条件再跑一次 `DeleteRepoRefs`（幂等，成本一次空 DELETE）。

注意：`docker_test.go` 的 "delete failure leaves the repo row behind" 用例当前断言「失败后 docker 表已空（含 refs）」——该断言固化的是半删除状态，随本修复需反转为「失败后 refs 保留（与仓行一致，可重试）」。

### B2. 同 digest 幂等重推绕过 write 门槛后改写 manifest 行的 provenance 与不可变列；TagRepointed 契约三方互相矛盾

`service.go:728-813`（PutManifest）。

幂等分支（node sha == digest）跳过 write 门槛——对齐 generic Put 重传语义，本身可辩护；`putNode` 也确实保留了 node 的 created/createdBy（探针证实 node.CreatedBy 仍为 "admin"）。但随后的 `dk.PutManifest` upsert 无差别改写 `created_by/created_at/media_type/size`。探针实测：**零授权的 alice** 同 digest 重推后，`docker_manifests` 行变为 `created_by="alice"、media_type=OCI、size=999`：

1. 工作日志声称「同 digest 重推=幂等重发布……保留 provenance」——对 manifest 行**不成立**（仅 node 保留）。/v2 面将按被改写的 media_type 服务 Content-Type（DE-09「存储的 Content-Type」），一次无授权调用即可漂移既有 manifest 的服务元数据；而同一函数对 node 路径的异内容覆盖反而要求 delete 权限（service.go:738），防护不对称。
2. `TagRepointed` 三方矛盾：api.go:157-158 文档说「or "" when unchanged/new」（tag 已指向本 digest 时应为 ""）；实现无条件 `repointedFrom = prev.Digest`（service.go:761-762），同 digest 重推返回 digest 本身；`docker_test.go:434` 的断言 `if res.TagRepointed != d { t.Fatalf("same-digest repush reported a repoint") }` **把错误行为固化成了断言，且断言条件与它自己的失败文案相反**。T-39 消费该字段，契约必须先定于一尊。

建议改法：
- 幂等分支跳过 `dk.PutManifest` 行写（manifest 不可变：同 digest 即同 body，无需刷新；或探测既有行，media_type/size 有差异时按伪造内部调用要求 delete 权限，与 node 路径对齐）；tag/refs 两个 upsert 可照旧。
- `if prev.Digest != digest { repointedFrom = prev.Digest }`，并把 docker_test.go:434 的断言反转为 `res.TagRepointed != ""`。

### B3. 错误哨兵错位：mediaType/size 校验与 ListImages 游标校验复用 ErrInvalidImage

`service.go:702-707`（空 mediaType、负 size → ErrInvalidImage）、`service.go:931-933`（跨仓游标 → ErrInvalidImage）。

`ErrInvalidImage` 的文档语义是「镜像相对名非法」（api.go:78-81），httpapi/T-39/T-40 将按哨兵翻译 /v2 错误码。空 Content-Type 应属 `MANIFEST_INVALID`（400），坏游标应属 `PAGINATION_NUMBER_INVALID` 或 400——都错译成「镜像名非法」会给下游适配层制造无法区分的错误身份。本票是这 8 个方法导出面的定约点（T-38/T-39/T-40 都 dep 本票），现在改是单行，T-39 落地后改就要动三个包。

建议改法：新增 `ErrInvalidManifest`（或复用/扩一个 manifest 形状哨兵）承接 mediaType/size；游标校验单独一个哨兵（如 `ErrInvalidCursor`）或至少映射到 ErrInvalidTag 同族的分页错误。同步更新 docker_test.go 对应断言（522/525/722 三处）。

---

## 建议改进（non-blocking）

1. **DeleteManifest 与并发 PutManifest 的跨 digest 交错**：单 digest 单元（manifest+tags+refs 级联）事务原子已确认；跨 digest 的 tag 重指与删除并发时最终收敛正确（-race 探针 200 轮不变量成立）。但 heal 分支与并发同 digest PutManifest 交错时，理论上可出现「manifest 行在、node 被 heal 掉」的 indexed-but-nodeless 态（读路径 Get node 会 404）。建议在 DeleteManifest heal 分支删除 node 前重探 manifest 行（一次 GetManifest 已有，可在 node 删除前二次确认），或接受为已知窗口并在注释标注。
2. **api.go:246-249 PutManifest 文档**提到「another manifest occupies the tag's node path」——本布局没有 tag 维度的 node 路径（node 按 digest 寻址），文档与实现（只查 digest 路径）不符，建议修文档。
3. `ErrPackageTypeNotSupported`（api.go:34）在 docker 开闸后已成死哨兵，注释「M1 repositories are generic only」失真。建议删除或改注释，避免误导后续读者。
4. `TestDockerWritePathFailures`（docker_test.go:1037-1039）中 `healed := newEnvAt(...); _ = healed` 是死代码（新建即弃的 env），建议删除。
5. **权限路径 `<image>/` 尾斜杠口径**：与 auth pathmatch 的 folder 语义（pathmatch.go:45 目录前缀规则以尾斜杠为 folder 判定）自洽，`app`、`app/`、`app/**` 三种授权拼写均能命中 `app/` 形态——本层无问题。但 ADR-0010 第 5 条给 T-37 的映射是「余段并入 path」，T-37 若拼成无尾斜杠的 `app`，两侧拼写需对齐；建议 T-37 review 时交叉核对（dockerPermPath 单点改动面已备好）。
6. **httpapi 层缺正向 D01**：compat_test 只有 remote docker 的 400 拒绝（C26），无「local docker 经 /binflow/api 建 200 + C06 回读 docker」的 httpapi 级用例。本票 area 限 internal/repo，服务层等价物已全覆盖；建议记入 T-43 回归清单。

---

## 已核实为正确的关键点（取证记录）

- **级联同事务**：DeleteManifest 直接调 `DockerStore.DeleteManifest`（存储层单事务清三行集），未在 Service 层重复实现级联——T-34 遗留 #2 的要求满足；`TestDockerDeleteManifestFailureIsAtomic` 的注错重试自愈路径含 foreign-node 负向用例，heal 条件（node.sha==digest 且行已不存在）保守正确。
- **blob-first 序**：PutManifest 经 `putNode` 先落 blobs 行再落 node；`blobStore.Put` 为 ON CONFLICT DO NOTHING（substores.go:200-201），adapter 已写入的真实 sha1/md5 不会被空值覆盖。
- **并发不变量**：-race 下 8 并发写删 + 2 读，「每条 tag 指向存活 manifest」终态成立；PutManifest vs DeleteRepo 的 FK 兜底使 manifests/tags/nodes 不产生 ghost（泄漏仅 docker_refs，即 B1）。
- **类型矩阵**：known/supported 两层保持（validate.go:20-34）；maven/npm/pypi/remote/virtual docker 的 M3 文案经 repo_test.go:74-81 + httpapi compat_test.go:65-75（C26，repositories.go:280 翻译为 400）双层验证；`ErrRepoTypeNotSupported` 哨兵去 "M1" 字样无回归（正文仍带 "M3"，fmt 动词拼接）。
- **ErrImageNotFound 双语义**：ListTags 的 manifests 探测就在函数内（service.go:902-910），T-40 改 null/[] 区分确为单点改动，遗留 #3 成立。
- **fakes 扩展**：hookDocker 为纯装饰器（nil fail 即 pass-through），未触碰 M1 既有 hookStore/failRepoDelete 语义；`newEnvCustom` 挂载缝不改变缺省 env 行为。
- **clean-room**：代码为惯用 Go，无反编译 Java 结构痕迹；行为对齐引用的是 docs/reverse/docker-registry.md 行为规格（§6 分页、§8 删除语义）而非源码，ADR-0001 流程合规。

## 范围外发现（交 conductor）

- 无（internal/adapter/docker 与 internal/httpapi 的工作区改动属 T-33/T-37 在途票据，未纳入本评审）。
