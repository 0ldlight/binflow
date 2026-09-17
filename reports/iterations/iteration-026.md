# Iteration Report — LOOP 026（批次 4 D08 release-bundle / fe-rewrite 55 腿 / m-holder 读位 / fern 英文化）

- **Iteration**: 026
- **Date**: 2026-09-17
- **Goal**: absent-roadmap 批次 4 D08 release-bundle 主战线 + fe-rewrite spec 重推导票（run_e2e 重开前置）+ m-holder 专键读位投影 + nightly 首跑观察

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L026-1 D08 规格票 | reverse-engineer | ✅ 64 发活体探针定案：v1 `/api/release/*` 全存活 + v2 `/api/v2/release_bundle/*`（下划线）并存；store 四臂 + release-bundles 系统仓自动创建副作用 + INVALID_RB_REPO 双键信封；R06 权限矩阵（ANY DISTRIBUTION 非 REST 实体）；置信高 22/中 4/低 1；wire 64 件 l026a-wire 归档。commit f6058b1e |
| L026-5/5s A 探定谳 | compatibility-engineer | ✅ optionalIndexCompressionFormats=v1 local 面唯一包型条件键默认 []（修正首轮 65 键口径=实测 62）；A/B 共享面零差再确认（generic 61/61、v2 18/18 值级）；m-holder 读位 A 定谳=权限解耦（任意认证四键投影+全量列表）；发现 dev=community 档 deb 建仓被 ADR-0032 门挡。commit 715ef811 |
| L026-3 main P0 回归根修 | dev-go-core | ✅ appendUnmodeledBlobKeys 未建模键 blob 回显 + blobEchoDenyKeys 排除集——L025-6 键表化渲染逐出的策略键（deb/rpm 全族）PUT→GET 顶层往返恢复；TestRepoConfigUnmodeledKeyEcho 钉契约。commit 3472e5bb |
| L026-6 D08 六行实现 | dev-go-core | ✅ R01 AQL 装配五键命中形 / R02 事务三段错误臂 / R03 store 七臂链+系统仓幂等置备+双键信封 / R04 查询族三 404 文案族 / R05 config 720 面+fat_manifest/v2 读面+audit 七字段 / R06 权限矩阵落证；未探分支 UNKNOWN 不实现；四门全绿（httpapi 全包 838.8s）。commits 54b1d46e + 合流件 695d8989 |
| L026-7 m-holder 读位投影 | dev-go-core | ✅ v1/v2 单仓+列表读门放行任意认证（单仓走既有 per-rclass partial 投影 / 列表 admin 逐字节同）；configurations 403 维持；写面零改动；**八处旧断言 403→200 翻正**（httpapi×7 + realstack_test.go×1——后者 main CI 揪出，bf0513ed）；t215 门账本 92→90。commit 404c44ca |
| L026-8 fern 英文化 | tech-writer | ✅ 生成器 7 文件 979 处 prose 英文化；「未实现」→ intentionally unrouted 产品措辞；结构 diff 零、CJK 扫描零、redocly 零新增；新长期规则落地（docs-product-voice-only）。commit 44531b6b |
| L026-4 CI 门控+预算腿 | devops-engineer | ✅ GitHub CI e2e job workflow_dispatch+run_e2e 门控；Makefile metadata 预算腿独占串行段+t212 界 1s→2s（#121/#122 共居 flake 双层根治）；PR #124（main=c56e0c62），UAT 翻转 uat.c56e0c6。commit e10e2567 |
| L026-2 fe-rewrite 55 腿 | dev-frontend（2a 击落→2b 续作） | ✅ **2a 转录静默 2h13m=API 流卡死击落，2b 从 JSONL 断点重建零损失收尾**：18 e2e spec 重推导 + 8 web/src 根因修（i18n 先于 app import 解冻 nav 族/菜单 role/Link 深链/路径键行/epoch-millis/t390 挂载竞速）；账目 40 done/3 BEHAVIOR-CONTAMINATED 复检旗/10 票外未动/t441 licensed 腿 skip；28 探针全删（结论落 L026-2-ledger）；PR #127（main=8d80c303）。commit 3c0c3a87 |
| L026-ci/client-fix | conductor | ✅ main CI 三连红终结两战：run 35172836661 全绿（批次一波）；PR #126 squash 合并后 main 红（76f16bdb/8d80c303 两跑）=L026-7 翻断言漏的第八处（realstack_test.go）→ bf0513ed 翻正 → **run 35185482672 全绿**（ci=success/release-dryrun=success/e2e=skipped 门控生效） |
| L026-main/dev 换装 | conductor | ✅ PR #126（76f16bdb，squash）/PR #127（8d80c303）/PR #128（**4c20553a，merge commit——历史重聚**，a4b17138 本地 merge main 入 develop 取超集）；UAT=uat.4c20553（D08 401 形状探针+控制台 200）；dev=dev-3c0c3a87 双跳换装（补真控制台——首轮漏 make console 烤进 placeholder 壳）；fern docs.binflow.org 三探针实证；假警报澄清（PWA manifest .json 误读为缺 .js——grep 截断陷阱） |

## Differential
- 本轮**无差分批**：D08 差分票被 dev=community 档 license 门挡（ADR-0032——release-bundle 面 pro-only），需 **pro 模拟档腿**（LOOP 027 首票）
- 代偿验证：D08 实现面以 L026-1 的 64 探针活体记录为锚（A 侧 7.161.15 wire 全归档）+ UAT/dev 双端路由形状探针（401 形状/列表空集/404 文案族三探针绿）
- fe-rewrite 55 腿以 spec 重推导+根因修替代差分（BEHAVIOR-CONTAMINATED 三腿复检旗待 dev 换装后复核——已换装 3c0c3a87，可复检）

## Fixed Gaps
- matrix：**本轮无行翻绿**（88/63 不动）——D08 六行实现完成但行态翻绿需差分验证（批次 4 与批次 1-3 节奏不同的诚实口径：dev community 档无 license 面，差分腿未跑前不虚翻）
- t215 权限门账本 92→90（读位开放两门）；八处陈旧断言翻正
- CI 债：GitHub Actions e2e 45 分钟债族门控；metadata 预算腿共居 flake 双层根治；main 三连红清零
- 文档债：fern 979 处英文化 + 新长期规则（对外文档国际惯例/产品视角）

## Compatibility Score
- Coverage ~44%（✅88/200 不动）；D08 六行 absent→(实现 in，差分待) —— LOOP 027 差分+契约固化后预期 88→94
- 候批 31 席（v2.7 不变）；pro 模拟档腿解锁 D08 差分族 + deb 差分族（L026-5s 登记）

## Interruptions
- L026-2a 僵尸（转录静默 2h13m，SendMessage 排队不泵动）→ TaskStop 收尸 + 2b 断点续作——零损失
- PR #128 合并 DIRTY 两因：本地 origin/develop 追踪 ref 陈旧（ssh 全 URL 推送不更新——fetch 后见真冲突）+ squash 历史分叉 both-added → 本地 merge 取超集 + **今后 develop→main 一律 merge commit**（裁定入册）
- dev 换装首轮 placeholder 壳事故（干净 worktree 无 make console）——手工构建配方补纪律：**先 make console 再交叉编译**

## Next Priority（LOOP 027）
1. **D08 差分票**（pro 模拟档腿——参照 A 侧 pro 面已开，dev 需 license 模拟；差分全绿后六行翻 ✅）
2. D08 契约固化票（contract_ref 空→release-bundle.yaml 契约文件）
3. deb 条件键实现票（L026-5s 候选清单已登记：A 65 键 vs B 62 键）
4. L025-7 FE 双臂退役票（L026-7 后端根治已落地）
5. BEHAVIOR-CONTAMINATED 三腿复检（dev 已换装 3c0c3a87）
6. docs/user/api-reference.md 英文化第二波
7. nightly 首跑观察（09-18 03:17 UTC）
8. 用户批复驱动：31 席整包（pending-rulings v2.7）
