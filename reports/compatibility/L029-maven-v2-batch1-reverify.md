# L029 — difftest v2 Maven 批次一复验轮（D-1/D-2 关闭）

- 日期：2026-09-28
- 执行人：conductor（R1 · BIN-M1 Maven 业务闭环收官轮）
- A 面（参照）：JFrog Artifactory **7.161.26 Enterprise+** @ 192.168.120.38:8082（经本地环回 relay 127.0.0.1:18099）
- B 面（被测）：BinFlow 本地构建 @ 127.0.0.1:18080（PR #156 合并后树 `go build`；`/binflow` 前缀；凭据经环境变量注入，未落盘）
- 框架：`tools/difftest/v2`（python3 stdlib-only）
- 轮次：r5 → r7 共 3 轮；**稳定性判据满足：r6 与 r7 全部 5 case 的 (status, reason) 逐项一致**
- 证据：`tools/difftest/v2/run/maven-batch1-r5/`（D-2 关闭时点）、`run/maven-batch1-r6/`、`run/maven-batch1-r7/`（稳定对）

## 批次结论（对照 L028）

| case | L028 | L029 | 说明 |
|---|---|---|---|
| demo-ping | PASS | PASS | 框架自检 |
| maven-virtual-deploy | PASS | PASS | 维持 |
| maven-virtual-metadata-merge | PASS | PASS | 维持 |
| maven-virtual-delete-passthrough | **FAIL（D-2）** | **PASS** | D-2 关闭（r5 起翻绿，稳定至 r7） |
| maven-resolve-remote-cache | **FAIL（D-1）** | **PASS** | D-1 关闭（r6 起翻绿，r6/r7 稳定） |

## D-1 关闭经过（重要：L028 归因修正）

**L028 归因**（"virtual→remote 解析与 `<K>-cache` 投影未实现"）只对了一半：

1. **F1 缓存投影缺失**（`GET /<remote>-cache/<path>` = 404）确系产品缺口——T-529/T-530（PR #156）实现后 r6 起翻绿（r5 时该 case 仍 FAIL，cache_projection_artifact b=404）。
2. **首拉 400 的真实根因是环境 × 安全控制交互，非解析缺失**：r5 复验仍 400，手工复现取回错误体真相——
   `Cannot fetch 'difftest-mvn-remote/javax/annotation/...pom': upstream target refused — private or suppressed upstream (remote difftest-mvn-remote: hop 0: upstream target repo1.maven.org rejected: private_ula address fdfe:dcba:9876::22)`
   ——差分宿主 Mac 的 clash TUN fake-ip DNS 将 repo1.maven.org 解析为私网 ULA（fdfe:dcba:9876::22），BinFlow 的 NFR-S13 SSRF 防护**正确拒绝**私网/ULA 上游（与 L028 记录的 400 体 sha256 d05e3e0a… 逐字节一致——L028 时即是此因，当时误归因为"解析未实现"）。四桶 walk 本身此前已有单测证明（TestVirtualTrueMissFallsThrough 等）。
3. **处置**：使用产品一等公民旋钮——仓库级 `allowPrivateUpstream` 豁免（admin 授予、审计，internal/remote NFR-S13）。手工验证：POST 更新 remote 配置置 `allowPrivateUpstream: true` 后，首拉经 virtual = **200 + sha256 46a4a251…（与 Maven Central 上游逐字节一致）**，`<K>-cache` 投影 GET = 200，二次拉取 200。
4. **case 固化**（`cases/maven_resolve_remote_cache.py`）：仅 B 腿建仓时带 `allowPrivateUpstream: true`，文件内注释注明环境原因；A 腿不动。这是测试环境适配（等价于生产环境上游 DNS 正常的情形），**不是放宽产品行为**——SSRF 防护默认姿态不变。
5. r6/r7 双轮稳定 PASS：三条断言（first_resolve 200+sha-ok / cache_projection 200 / second_resolve 200+sha-ok）双面一致。

## D-2 关闭经过

- T-531（PR #156）实现 virtual DELETE 语义：DELETE /{virtual}/{path} 无条目时 **404（ITEM_NOT_FOUND，"Could not locate artifact. Path: '…'."）**，成员物理制品存活；有条目时删 virtual 聚合缓存条目。
- r5 wire 证据：case 全断言双面一致（A 面 404+成员存活 ↔ B 面 404+成员存活），稳定至 r7。
- 历史包袱清理：replication 域两处旧 405 断言（`engine_integration_test.go`、`t317_two_instance_test.go`）按 D-2 裁定翻新（conductor 直改 + 定向复跑 ok 1.811s，d094bfce）。

## 维持正向的合规确认

- mvn deploy:deploy-file（RELEASE+SNAPSHOT）经 virtual→defaultDeploymentRepo：维持 PASS。
- virtual 元数据合并（versions union/latest/release、`<virtual>-cache` 不落合并结果）：维持 PASS。

## 遗留（移交下轮）

- **D-3**（非空仓 DELETE /api/repositories/{key}：A 200 静默级联 vs B 400 需 deleteContent=true）→ known-divergence.yaml 四分类登记（compatibility-engineer；倾向 INTENTIONAL 候选）。
- **D-4**（A 面 POM↔部署路径 GAV 一致性 409 校验）→ 反向用例候选 + B 面是否实现待覆盖。
- T-535 登记的 N1/N3/N4 三缝隙（BIN-18）→ F2/F4/F8 缝票统一收口。

## 矩阵落账

- 新行 **D12-R18**（Maven virtual 内容面）compatible，last_difftest=L028+L029（conductor 派票授权新行，L005-3/L007-4 先例款）。
- 四问基线（落账前）X=200 / Y=108.5 / Z=33 unresolved / coverage 60.28% → 落账后 X=201 / Y=109.5 / coverage **60.50%**（109.5/181）。
