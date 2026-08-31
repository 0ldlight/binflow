# Sprint 962 迭代报告 — T-365 收口（`af2f6c0`，B3 全清）；T-367+rider 派发

**日期**: 2026-08-30 20:4x
**上轮**: Sprint 961（等待轮）

## T-365 → done（develop=`af2f6c0`，双远端）——M13 8/23

/v2 virtual 聚合面全量：V2VirtualPlane 缝 + 四读 virtual 臂 + 降级矩阵 + 写拒绝 405/C5。**真客户端**：helm 4.2.4 双实例双域 pull（digest/tgz 逐字节一致/二跳 HIT/上游冻结/tags 并集）+ kind install→upgrade + dind 回归 + 门控 live（混仓 400/卸载降级 D1）。顺手修 /v2/_catalog 500。conductor 复验 build/helmoci/repo 定向绿。三条裁决登记：① 成员类型收紧 → T-367 rider；② push-through 候 T-380 批；③ catalog 口径入 helm.md §8.4。

## 派发

- **T-367**（dev-registry-adapter，B4 后半）：chartsBaseUrl 分体基址 + FetchAbsolute 缝 + `_external`/`_transitive` 落盘 + **rider：virtual 成员类型收紧**（照 Artifactory 同包型语义）。dep T-363 ✓（T-365 刚清同域串行）。

## 在途 ×2

- **T-366**（webhook FE）：活跃（摸底后进入实现预期）。
- **T-367**：本轮派发。

## 状态

M13：**8/23**。在途 ×2。HEAD[develop]=`af2f6c0`。
