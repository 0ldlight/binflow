# L028 — difftest v2 Maven 批次一（T-523）差分报告

- 日期：2026-09-28
- 执行人：conductor（差分批次派发票 T-523）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ 192.168.120.38:8082（X-Jfrog-Version 活体复验通过）
- B 面（被测）：BinFlow 本地构建 @ 127.0.0.1:18080（`/binflow` 前缀；UAT 管道当轮不可用，以同源本地构建替代，凭据同管理员账户）
- 框架：`tools/difftest/v2`（python3 stdlib-only，双发→逐 case 断言→judge 四态）
- 轮次：r1 → r4 共 4 轮；**稳定性判据满足：r3 与 r4 全部 5 case 的 (status, reason) 逐项一致**
- 证据：`tools/difftest/v2/run/maven-batch1-r3/`、`run/maven-batch1-r4/`（results.json + 逐 case evidence）

## 批次结论

| case | 状态 | 说明 |
|---|---|---|
| demo-ping | PASS | 框架自检 |
| maven-virtual-deploy | **PASS** | 真实 mvn 客户端 RELEASE+SNAPSHOT 经 virtual（defaultDeploymentRepo）双面全合规 |
| maven-virtual-metadata-merge | **PASS** | 双成员 versions union / latest / release 重算、virtual-cache 不落合并结果，双面合规 |
| maven-resolve-remote-cache | **FAIL** | B 面 spec violation（见 D-1） |
| maven-virtual-delete-passthrough | **FAIL** | B 面 spec violation（见 D-2） |

## Divergence 台账（本轮新测得）

### D-1：virtual→remote 解析与 `<K>-cache` 投影未实现（case maven-resolve-remote-cache）

- 规格：virtual-resolution §2（remote 成员解析）、remote-cache-projection §3（首拉写缓存）/§2.1（缓存投影可 GET）
- A 面：`GET /<virt>/<central-path>` 首拉=200+上游字节精确（sha256 比对 javax.annotation-api-1.3.2.pom）；`GET /<remote>-cache/<path>`=200；二次拉取一致
- B 面：virtual 解析=**400**（错误体 sha256 d05e3e0a…，Content-Type application/json）；`<remote>-cache` 投影 GET=**404**（F1 缝隙：缓存投影未落地）
- 归属：internal/remote 拉穿缓存引擎 + virtual 解析四桶（cache→local→remote→virtual 成员）——待实现票（T-520 设计已产出四桶口径）

### D-2：DELETE 经 virtual 返回 405（case maven-virtual-delete-passthrough）

- 规格：repo-semantics §8.2 L220 + virtual-resolution §7.5：DELETE /{virtual}/{path} 仅作用于 virtual 自身聚合缓存，无条目时 **404（ITEM_NOT_FOUND）**，成员物理制品存活
- A 面：404 + 成员制品存活（GET 成员=200）；随后成员直接 DELETE=204，virtual 再解析=404 —— 与规格一致
- B 面：**405**（方法不允许）；后续成员直接 DELETE/再解析行为与 A 一致
- 归属：internal/httpapi virtual 写路由（DELETE 动词处理）

### D-3：非空仓库 DELETE /api/repositories/&lt;key&gt; 语义分歧（harness 修复过程中测得）

- A 面：**200**，静默删除仓库及其全部内容（无确认语义）
- B 面：**400** `"repository is not empty … retry with deleteContent=true"`，需显式 `?deleteContent=true` 才级联删除
- 备注：B 行为更安全但与参照分歧；known-divergence 登记候选（倾向：登记为 intentional-divergence，需 compatibility-engineer 裁定四分类）

### D-4：A 面 POM↔部署路径 GAV 一致性校验（参照行为确认，clean-room 佐证）

- 实测：PUT pom 内容 version 与部署路径 version 不一致时，A 返回 **409**："The target deployment path '…' does not match the POM's expected path prefix '…'"
- 用途：佐证 docs/reverse/ maven 行为规格的部署校验段落；B 面是否实现同校验待后续批次覆盖

## 环境隔离记录（判 r1 部分无效的依据）

1. **Mac clash TUN（utun4）对直连 Java 连接注入 RST**：A 面直连时 mvn 部署腿在响应读取阶段被 RST（curl/python urllib 不受影响）。经本地环回 TCP relay（127.0.0.1:18099 → 192.168.120.38:8082）逐字节转发后同请求全数成功（PUT→201）。结论：r1-direct 轮的 A 面 mvn 失败为 Mac 本机环境噪声，非产品分歧；该轮已重命名 `run/maven-batch1-r1-direct-invalid/` 并附 INVALIDATED.txt。
2. **case 断言 bug 三处（r1 判部分无效）**：①② EXPECTED 值格式（`"404"` vs `"status=404"`）；③ maven-virtual-deploy 三处 virtual 路径漏拼 repo key（断言 GET 打到 `/com/diff/...` 无仓库段，双面皆 404）+ snapshotVersion `<value>` 语义误用（value 是版本串，文件名=`<artifactId>-<value>.<ext>`）。修复后 r3/r4 双面 virtual-deploy 全绿。r1/r2 的对应断言结果判无效，其余 case 结果不受影响。
3. **凭据**：双面同管理员账户，仅经环境变量注入 difftest 进程，未落盘、未入任何仓库文件与本报告。

## BinFlow 合规确认（正向证据）

- mvn deploy:deploy-file（RELEASE+SNAPSHOT，unique 快照）经 virtual→defaultDeploymentRepo 落 local 成员：exit 0、落盘 timestamped 形态、pom/jar 字节精确回读、sha1 端点值匹配、版本目录与组目录 maven-metadata.xml 服务端计算（buildNumber/snapshotVersions/extensions/versions/latest/release）全部合规（与 A 面逐项一致）。
- virtual 元数据合并：双成员 versions union 按 Maven 比较器排序、latest 含 SNAPSHOT、release 取最后非 SNAPSHOT、`<virtual>-cache` 不写合并结果——合规。

## 后续动作

- D-1 → virtual 解析四桶 + remote cache 实现票（dev-go-storage / dev-go-core，T-520 设计输入）
- D-2 → httpapi virtual DELETE 404 语义修复票
- D-3 → known-divergence.yaml 四分类登记（compatibility-engineer）
- D-4 → 反向用例候选（POM GAV 校验差分）
- en ratchet（T-526）下轮接入本批次 case 集
