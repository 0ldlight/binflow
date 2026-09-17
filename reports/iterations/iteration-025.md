# Iteration Report — LOOP 025（批次 3 D02 配置族 / e2e 债族根因 / docs-site 退役）

- **Iteration**: 025
- **Date**: 2026-09-17
- **Goal**: absent-roadmap 批次 3（D02 配置族全行）+ CI e2e 债族根因收口 + docs-site 全退役 + fern 唯一化收尾

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L025-1 规格核对 | reverse-engineer | ✅ D02 五面规格（configurations/v1/v2/非 admin/批写族）；**三大翻案**：v2 batch 动词映射=PUT 批建/POST 批改（旧记反）；207 混合态只属 DELETE（ghost 记 success:true）；/api/repo_layouts 已撤→真身 /api/admin/repolayouts（25 布局非 26）。commit 6814c3d7 |
| L025-3A 读族实现 | dev-go-core | ✅ configurations 公共投影（local 18/remote 46/virtual 12 键）+existence+v2 读+repolayouts 25/25（列表逐字节转录+单读 wire 对齐）。commit 7661853b |
| L025-3B 批写族实现 | dev-go-core | ✅ PUT 批建（全有或全无+federated-first+回滚，201 text/plain 逐字节）/POST 批改（ADR-0050 merge 逐项）/DELETE 207 状态机（空体 400/预校验中止臂/ghost success:true/全败首失败码/deletedArtifactsCount=文件节点数）。commit 498683c5 |
| L025-4 差分首轮+规格回写 | differential-qa | ✅ SAME 22；两证伪（G1 键面账实不符定位）+实测键面锚回写 §2.1.9。commits c3cc4068/1d673ead |
| L025-5 微返工 G4/G2/G5 | dev-go-core | ✅ PUT 403 裸 Forbidden/blank-key ghost 200/v1 未知 key 400 quirk。commit 256571d1 |
| L025-2 CI e2e 债族 | devops-engineer | ✅ **55 红根因链**：pro 租约单消费方根修（共享 helper）；五 spec 重钉；run_e2e 维持 false+重开条件入注（55 腿三桶→fe-rewrite spec 重推导票）；nightly trigger_source 双向门（首跑 09-18 03:17 UTC）；docs-site 构建面三处 de-wire。commit b13a86d2 |
| L025-6 G1 六面渲染 | dev-go-core | ✅ 六面键表+渲染器（blob 值优先/未建模键 wire 默认形/password 恒""/contentSynchronisation 平铺→嵌套/virtual repoLayoutRef 条件键）+非 admin 分 rclass partial（local 4 键无 url——修账实不符）。commit 989ad5ed |
| L025-7 FE 读位修复 | dev-frontend（conductor 收编） | ✅ repos.ts 读位合并：v1 平铺键 ∪ 列表面 configuration blob（403 容错）；priorityResolution/quotaBytes 编辑翻转隐患根修+回归腿；seed-m8 400 quirk 按 absent 处理。commit b79a2d51 |
| L025-8 终轮差分 | differential-qa | ✅ dev.b79a2d51 终轮：**四层键集面键名差=0**（v1 61/50/102、v2 18/12/46、configurations 18/12/46、非 admin 4/5/5）+L025-5 三臂全 SAME；实质口径 31 一致/5 残差（全为登记候裁）；**八面全翻绿建议（VERIFIED）**；w13 ghost 检查次序新发现；资产删净（residue []/404）；wire 44 件归档。commit d7a70797 |
| L025-9 Distroless docs 段 | conductor | ✅ alpine 同款手术：docs node 段撤除+段号重排；build-release.sh/prebuilt 无悬引用。commit 5fa9ad65 |
| L025-10 落账 | compatibility-engineer | ✅ matrix D02 五行翻 compatible（R08 动词映射+R12 真身/25 布局能力行勘误；contract_ref 26→31/last_difftest 39→44，ruby 逐行对账）；契约 7 条目全 VERIFIED；台账 +3 族（63 条在册）；pending-rulings v2.7 +R-25a/b/c → 31 席；loop-state after 88/63+iteration_events 起新键 |
| conductor 间隙件 | conductor | ✅ wire 444 件入库（9514d199）；BOARD 024 补账（bd848b75）；UAT 换装比对基线（healthz 200） |

## Differential
- L025-4 首轮：SAME 22 / DIVERGENT 3（G1 键面+G4 403 形+G2/G5 quirk 族）——全部经 L025-5/6 修复
- L025-8 终轮：**实质口径 31 一致 / 5 残差**（批跑原始 SAME 28/DIVERGENT 8——v03 驱动器怪癖以显式 CT 探针定谳、w11/w14 集合化后行集全等）；四层键集面键名差=0；三臂 SAME；G7 真 207 不可构造维持开放备案
- 残差 5 面全为登记候裁不挡翻：G6 头族×3、plain-user 403 族×2（含新发现 w13 ghost 检查次序）、hexPublicKey 值形

## Fixed Gaps
- matrix：compatible **83→88**、absent **68→63**（批次 3 翻绿五行 D02-R06/R07/R08/R09/R12 ✅——八面归五行；R11 Federation 族不在批次范围）
- 台账：known-divergence +3 族登记（G6 头族/plain-user 403 族/hexPublicKey 值形——候裁通道）
- pending-rulings v2.6→v2.7：+3 席 → **31 席候批**

## Compatibility Score
- 契约十文件（d02-config.yaml 7 条目全 VERIFIED）；候批 31 席（v2.7）
- Coverage ~44%（✅88/200）

## Interruptions
- 限额第十三波击落 L025-8 于夹具重建段（l025q-u 用户被上轮清理删除致双端 401+A 限流）——SendMessage 断点复活零损失；轮中一次断网 ~8 分钟自愈
- 环境铁律新增：该主机 postgres 绝不单文件重启（compose 项目隔离）

## Next Priority（LOOP 026）
1. 批次 4 D08 release-bundle（absent-roadmap 主战线推进）
2. fe-rewrite spec 重推导票（55 腿三桶→run_e2e 重开前置）
3. nightly 首跑观察（09-18 03:17 UTC——零触发需用户 UI 建 schedule）
4. m-holder 专键读位后端投影面（照 storage-usage 先例）
5. 用户批复驱动（28 席整包——R-23a/R-24a/b/c 含 D8 族整族翻绿钥匙）
