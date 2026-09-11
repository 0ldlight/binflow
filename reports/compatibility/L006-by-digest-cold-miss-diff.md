# L006 — C14 by-digest 冷 miss 补臂（U-PROTO 面，双审微票池 L006-2 #5）

- Ticket: L006-2 item 5（dev-registry-adapter；L005-1 双审 Reviewer A/B non-blocking「by-digest 无活体臂」清偿）
- 日期: 2026-09-12（UTC 时钟 2026-09-11 晚间）
- 模式: **dual**（BinFlow UAT http://localhost:8083 vs 参照 :8082 Artifactory-pro 7.161.20）
- BinFlow 基线: **uat-l0051-792c347c**（未被并行轨重建——/binflow/api/system/version 回读 version=uat-l0051-792c347c、revision=792c347c；healthz OK。本臂验证的是 L005-1 已实现的直读面 by-digest 冷 miss 负缓存，与 L006-2 的 virtual seam 补丁无代码依赖，基线无须重建）
- 参照: :8082 运行时 7.161.20（未动，ping 200）
- 上游: 全新 registry:3（l0062-upstream，127.0.0.1:5591），种子 l0062/busybox:t1（manifest sha256:1cfa4e2b…——与 L004-1/L005-1 同一 busybox 内容 digest）
- 测试仓: 双端各 1——l0062-remote{url:http://host.docker.internal:5591, missedRetrievalCachePeriodSecs:60}（BinFlow 另 allowPrivateUpstream:true；配置键回读各自 API 200）。**用后已删净**
- 客户端: curl 8.7.1（--noproxy；Basic 直连）
- 判据: status / body 逐字节比对（归一口径沿用 L004-1：尾换行不判）/ 上游 access 行计数（锚 `^[0-9.]+ - - `，grep 指纹 digest）
- 缺席引用: sha256:9850fa23d6d63ed0ce3619c3f492780d2270169c02319fd2a857fe321c0b54a3（脚本派生，已知 image 上的不存在 digest——M-a 的 by-digest 对偶）

## 1. 结果矩阵（by-digest 冷 miss ×3 连发 + 60s 到期复问）

| 态 | 参照 :8082 | BinFlow :8083 | 判定 |
|---|---|---|---|
| try1 | 404 / 21ms / 上游 **HEAD ×1** | 404 / 480ms（含建连）/ 上游 **GET ×1** | SAME（首问一次；动词差归 N1 形状类，不判） |
| try2 | 404 / 8ms 本地 | 404 / 21ms 本地 | SAME |
| try3 | 404 / 8ms 本地 | 404 / 22ms 本地 | SAME |
| 上游计数 / 3 | **1** | **1** | SAME |
| 404 体 | `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"The named manifest is not known to the registry.","detail":{"manifest":"l0062/busybox"}}]}` | 同串（仅尾换行差——既有归一规则内） | SAME |
| 60s 到期复问 | 404 / 上游 **+1**（HEAD），行重写 | 404 / 上游 **+1**（GET），行重写 | SAME |
| 重写后再问 | 本地（计数冻结） | 本地（计数冻结，总计数不动） | SAME |

## 2. sqlite 行直证（BinFlow 侧，docker cp db+WAL）

```
l0062-remote | l0062/busybox/manifests/9850fa23…54a3 | negative | 19:35:42Z → 19:36:42Z   ← 首问落行
l0062-remote | l0062/busybox/manifests/9850fa23…54a3 | negative | 19:36:46Z → 19:37:46Z   ← 到期复问重写（重写时刻=复问时刻、窗长恰 60s）
```

键法与 L005-1 设计一致：by-digest 冷 miss 键 **digest→manifest 节点路径**（`image/manifests/<hex>`，与 standing 臂同键）——活体直证。

## 3. 结论

- **by-digest 形活体闭环补齐**：机制与 tag 形共享 core（同一 CacheRemoteMiss 写、同一冷 miss 读探针），本臂双端同语义——首问一次（参照 HEAD / BinFlow GET，N1 类形状差不判）→ missedTTL 窗口内本地 → 到期回源 +1 + 行重写 + 再冻结。
- 与 tag 形（L005-c14-d1-diff §3.1/§3.2）完全同形——C14 两键法（tag→tags/ 行、digest→manifests/<hex>）至此均有活体双端证据。
- 无新增分歧、无回归；N1（首问动词 HEAD vs GET）维持既有登记。

## 4. 契约建议（归 compatibility-engineer，本报告只出证）

`docker/remote-manifest-404-no-negative-cache` 的 timing.arms 目前只有 `known_image_tag_miss` / `unknown_image_name` 两臂（surface 亦以 nonexistent-tag 表述）；建议增补 `known_image_digest_miss` 臂（或 surface 扩 tag-or-digest），evidence 追加本报告——side_effects 里的双键法描述已有，无须改。

## 5. 环境清理记录（已执行）

- 仓：双端 l0062-remote DELETE 200（BinFlow deleteContent=true）；双端列表零残留（grep l0062 = 0）。
- 容器：l0062-upstream 已 rm -f；docker ps -a 无 l0062 残留。
- 落盘：BinFlow db/WAL 快照与 body 临时件已删。
- UAT :8083 保留 uat-l0051-792c347c 运行（healthz OK）；参照 :8082 未动（ping 200）。

## 6. 复现骨架（凭据脱敏）

```bash
# 上游：docker run -d --name l0062-upstream -p 127.0.0.1:5591:5000 registry:3
#   种子：docker pull busybox && docker tag busybox 127.0.0.1:5591/l0062/busybox:t1 && docker push …
# 仓：双端 PUT …/api/repositories/l0062-remote
#   {rclass:remote,packageType:docker,url:http://host.docker.internal:5591,missedRetrievalCachePeriodSecs:60[,allowPrivateUpstream:true]}
# 臂：ABSDIG=sha256:$(printf <seed> | shasum -a 256 | cut -d' ' -f1)
#   curl -u … -H 'Accept: …manifest.v2+json,…' :<port>/v2/l0062-remote/l0062/busybox/manifests/$ABSDIG ×3
# 计数：docker logs l0062-upstream | grep -E '^[0-9.]+ - - ' | grep -c <hex>
# 行证：docker cp binflow-ga:/var/lib/binflow/binflow.db{,-wal} → sqlite3 remote_cache
```
