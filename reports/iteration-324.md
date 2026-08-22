# Sprint 324 迭代报告 — T-168 红测试根治收口 + batch 5 + T-172 QA 派发

**日期**: 2026-08-22 05:0x
**上轮**: Sprint 323（等待回合）
**本轮焦点**: 悬置最久的红测试票落地；首个干净全量基线诞生

## 阶段 2 — 收口

### T-168 — Go 1.26.6 + 优雅停机 + nginx SSL ✅（重派+429 续跑收口）

- **两处红根治**：根因 = 同包两个全量装配调用方撞「仅装配一次」adapter 注册表契约；重写为真 runServe+真 SIGTERM
- nginx 模板修 3 真实缺陷 + 容器 nginx -t + 端到端 TLSv1.3+HTTP/2 实测
- conductor 复核：cmd 套件 **无 skip 26.4s 绿** + lint 0；agent 自跑 make test **21 包 exit 0**
- 遗留：auth TTL 并行负载 flake（chore 票候选）

## 阶段 3 — 派发

- **T-172 [P0] M6 回归基线 QA**（dep T-168 ✅ 解锁）：M1~M5 全 P0 序列复跑 + 真实客户端矩阵 + H68 报告——**首个干净基线**（无 skip 全绿）上跑
- 在途 2/4：T-190（token 实现）· T-172

## 阶段 4 — 落盘

- ✅ **M6 batch 5 提交 `00f73e7`**（精确路径：main_test.go 首次入库全绿 + deploy/nginx + internal/client（T-165 收口后解禁）+ cmd/bf + PRD v1.2 + QA 报告）
- ✅ BOARD.md：T-168 → done（done 区 **36 票**）；T-172 → doing
- ✅ 本报告

## 阶段 5 — 战报

| 指标 | 数值 |
|------|------|
| 收口 | 1（T-168 ✅ 红测试根治） |
| 提交 | batch 5（00f73e7） |
| 派发 | 1（T-172 P0 回归 QA） |
| 在途 | T-190 · T-172 |
| done 区 | **36 票** |

五批次累计：ed9de87 → 9593bb7 → 1d27288 → cca3ade → 00f73e7。`-skip` 时代结束。

下轮重点：T-190/T-172 收口；组队 T-185/T-189/T-167。