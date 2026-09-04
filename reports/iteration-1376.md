# Sprint 1376 迭代报告 — 配额窗⑯复活 + 复测①全定谳：矩阵 7/10（三红逐一落刀）+ T-473 收编 + 三新票

**日期**: 2026-09-04 20:56~21:2x（/loop 20m 重启，cron `ca10187d`）
**上轮**: Sprint 1375（复测启动）

## 一、配额窗⑯ 处置

19:12 击落 T-457/T-472/T-473 → 20:56（重置 20:53 后）SendMessage 三复活，断点续零损失。T-473 随即收口。

## 二、复测①裁定（main 47f8385 双 CI + GH 矩阵 dispatch）

| 面 | 判定 | 定谳 |
|---|---|---|
| GH 矩阵（可读日志 run 33866554682） | **7/10 绿**——pypi 修复生效 ✅ | 红三腿：**helm=产品缺陷**（`spool body → /tmp: read-only file system` 500 铁证）；**nuget=runner dotnet SDK 10 遮蔽**；**conan=conan 2 废 --template** |
| GH ci | ❌ | npm audit 端点瞬断（外部抖动） |
| GH e2e（T-471 retries 首验） | 3 硬红/2 flaky/337 绿 | **retries 吸收 2 腿 ✅**；t443/t449/t451 三连败=CI 环境确定性（本地绿）→ T-475 |
| release-dryrun | ❌ | goproxy GOAWAY 家族再现 |
| CircleCI | build/deploy ✅；protocol_matrix ❌=同三腿；e2e ❌ 待查 | 同脚本单源，随修复走 |

## 三、本轮落刀（`616f1d2` + `5999b39` 已推）

1. **T-473 收编 done**：CircleCI 九项能力（缓存×3 + parallelism/split/junit + DLC + nightly + 两处存量缓存空转修复）
2. **conan 腿**：手写最小 recipe（废模板依赖）
3. **nuget 腿**：GH workflow `setup-dotnet@v4` 钉 8.0.x
4. **T-474 已派**（P1 热修）：helm spool → 存储同卷 staging（dev-registry-adapter 在途）
5. **T-475 立票**（todo）：e2e CI 环境确定性三 spec——候 FE lane 空出
6. Fern 重发布避让（T-472 在途重构 fern/ 树——CLI 已切 API 项目模式）；T-458 镜像页顺延统一发

## 四、在途与状态

- lane：**T-457**（FE，复活续）/ **T-472**（Fern API，复活续）/ **T-474**（helm 热修）
- M16: **23/35**；复测②候 T-474 收口一次合并全量验
