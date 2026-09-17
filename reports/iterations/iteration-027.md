# Iteration Report — LOOP 027（D08 差分六行翻绿 / deb 条件键 / FE 双臂退役 / api-reference 英文化二波）

- **Iteration**: 027
- **Date**: 2026-09-17
- **Goal**: D08 差分票（pro 模拟档腿——license 门解锁后六行翻绿）+ D08 契约固化 + deb 条件键实现票 + L025-7 FE 双臂退役 + BEHAVIOR-CONTAMINATED 三腿复检 + api-reference 英文化第二波 + nightly 首跑观察（09-18 03:17 UTC）

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L027-1 D08 差分 | differential-qa-engineer | ✅ 55 case r7/r8 双轮稳 **SAME 52 / DIVERGENT 3**（c42 A 侧竞态/c53 预置合成 REST 呈现/c55 repo DELETE 信封——全相邻或候裁，零 B 侧未修 BUG）；**四 B BUG 随轮修复验证**（AQL E1 锚点〔共享面 search+release 同修〕/v2 audit 405 Allow 头/release-bundles 系统仓 DELETE 卡死/HEAD CT）；pro 模拟档解锁（仓库根私钥 `bf license issue --tier pro`，毕后回 community 复核）；dev 两次换二进制；提金候选 27 案；wire l027q-wire 110 组归档；httpapi 一红=TestBigTreeCopyNo5xx 预存性能臂（干净 HEAD 同败+load~99 定谳）。commits 4efcb561 + 9721e37f |
| L027-2 D08 契约固化 | compatibility-engineer | ✅ contracts/release-bundle.yaml 域首 7 条目（断言锚 l026a-wire 64 件；U1-U9 未覆盖分支零虚构；C1-C8 候裁面登记）；matrix contract_ref 31→37；M1 勘误顺手清。commit c9957da1 |
| L027-3 deb 条件键 | dev-registry-adapter | ✅ debian local 条件配置座位（v1 62 键/v2 21 键面，optionalIndexCompressionFormats 默认 [] 按 L026-5 定谳）；httpapi 全包复验=预存性能红腿基线隔离在案。commits 3e407c27 + 5e7be67d |
| L027-4 FE 双臂退役+三腿复检 | dev-frontend→conductor 断点接管 | ✅ **done-part**：前会话 16:28 中断、conductor 接管收编——getRepoDetail 双臂退役（v1 详读面一跳+LIST_FACE_ONLY_KEYS 五键收窄第二读位）+ isNarrowRepoFace 窄投影写保险 + 三 spec 重锚（三腿复检旗销旗）；验证=asserts 双闸亲跑过+闭包型 tsc 五改档全净+dist 16:02 改后树构建证据；**欠账=三 spec 运行时腿（VM 沙盒 chromium SIGSEGV）+全量 tsc/eslint——Mac 侧复跑清偿**。commit 2ea971b7 |
| L027-5 api-reference 英文化二波 | tech-writer | ✅ 全文英文重写 886→~740 行（fern 措辞族+端点以 router.go 逐条核实+两处老页漂移顺手修）；CJK/过程词扫描零。commits 7a4eff76 + bceba6a1 |
| L027-6 D08 落账 | conductor | ✅ matrix D08-R01..R06 六行翻 compatible（**88/63→94/57**）；契约 7 条目 VERIFIED（difftest 证据追锚）；known-divergence +3（c42/c53/c55——全 UNKNOWN 候 v2.8）；yaml 机读门+行实对账双过。commit 16288ad6 |
| nightly 首跑观察 | conductor | ⏳ **未到期**（09-18 03:17 UTC=11:17 CST）——观察点：race_full×4 分片+十腿矩阵 vs 常驻 UAT；零触发则需 CircleCI UI 建 schedule（用户动作，API 不可达在案）；**宿主失联（见 Interruptions）为该观察的地面复核前置** |

## Differential
- **D08 域首差分**（L027-1）：55 判定单元，8 轮迭代（r1 39/15 → r7/r8 终局 52/3）；normalize 提案 RB1-RB5（归一层复用 npm 域 wire_format 姿态吸收 Jackson 渲染差）；真实 curl 客户端六腿（A/B 各 list+assemble+v2 names 退出码 0）
- 代偿面：L027-4 运行时 e2e 腿未跑（环境限制，见 Interruptions③）——以闭包型 tsc+断言闸+dist 构建证据代偿，运行时复跑欠账在册

## Fixed Gaps
- **matrix：absent 63→57（D08 六行）——compatible 88→94**；P1 absent 22→16；last_difftest_non_null 44→50；contract_ref 37（L027-2）
- 四 B 侧 BUG 修复（差分轮内定位+修复+复验）：AQL 语法错锚点（lexAll 预词法抢跑）/v2 audit 405 缺 Allow 头/release-bundles 系统仓 DELETE 400 卡死/DeleteRepo 域自有行豁免/HEAD 404 缺 CT
- FE：L025-7 双臂退役（m-holder 全量清单扇出消除——local/virtual 详情 2 请求→1）+窄投影写保险（数据完整性防线）
- 文档：api-reference.md 产品语态英文全量（docs-product-voice-only 规则二波兑现）

## Compatibility Score
- **四问**：X（行集）=200（⛔20）；Y=✅94+0.5×◐17+0.5×超集12=**108.5**；Coverage=108.5/180=**60.3%**（LOOP 026 收口 56.9%→**+3.4pp**）；✅/total=94/200=47.0%
- Z（known-divergence 开放）=56 条（BUG 28/UNKNOWN 31〔含本轮 +3〕/INTENTIONAL 6/UNSUPPORTED 3；resolved(parity) 12 另计）——候批 31 席（v2.7）+3 候 v2.8
- N（unknown 面）：unknown.yaml 本轮未动；D08 U1-U9 开放臂维持（签名链/有记录成功形双端同不可达=wire 局限）
- P1 absent 余 16 行（D03-R10/R16/R17 三行已实现待差分落账〔LOOP 024 遗留〕+D06 P1 族+长尾）

## Interruptions / 环境事件
1. **前会话中断**（~16:28–16:45 后）：L027-4 实现段完成后会话断（web/ 七文件在途未提交）——本会话（conductor）19:0x 前接管，断点零损失收编
2. **宿主 172.16.58.130 整机失联**（19:0x 起 ping 100% loss；18:0x 前双实例实测可达：ref 401 正常/dev healthz 200）——参照/dev/UAT 地面腿全部暂停；恢复后须复核（差分/probe/nightly 观察地面腿）
3. **conductor VM 环境限制**（本会话验证方法学）：单调用 178s 时限+进程不跨调用存活+chromium SIGSEGV（沙盒套沙盒）——大闸门改「闭包型 tsc+断言闸+dist mtime 证据」组合拳；node_modules 为 Mac 侧安装（Linux 原生腿已 --no-save 补装）
4. chunk pre-commit 钩子在 VM 无 chunk 二进制——按 LOOP 000 在册纪律 `--no-verify` 出口（lint 后）

## Commits
6814c3d7 之前一批见 LOOP 026 seal；本轮：4efcb561 / 9721e37f / c9957da1 / 7a4eff76 / d5e6b36c / bceba6a1 / 3e407c27 / 5e7be67d / **16288ad6（L027-6）** / **2ea971b7（L027-4）**

## Next（LOOP 028 候选池）
- nightly 首跑观察+零触发处置（09-18 11:17 CST；宿主恢复为前置）
- L027-4 欠账清偿（Mac 侧三 spec 复跑+全量 tsc/eslint——命令在 L027-4 报告 Commands）
- pending-rulings v2.8 呈批（+3 席：c42/c53/c55 族）
- 宿主失联恢复复核 + dev 二进制状态核对（L027-1 两次换装后版本戳）
- absent-roadmap 批次 5a：D06 轻面（R04 /api/system 文本 DUMP〔参照 149,917B text/plain〕+R05 serverTime+R14 service_id）+ config.xml 深水票评估；D03-R10/R16/R17 三行差分+落账（已实现未落账——LOOP 024 遗留）
- e2e 债族残余：design-baseline 金样方法票（run_e2e 重开前置）
