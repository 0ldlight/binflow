# Iteration Report — LOOP 016（pypi 扩章契约 / npm ghost 深水 / 呈批包 v2.3）

- **Iteration**: 016
- **Date**: 2026-09-13
- **Goal**: 第三扩章域开工 / npm ghost 收口 / 四裁定包整备

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L016-1 pypi 前段 | differential-qa | ✅ 19 臂（真实 pip/twine 腿）+契约首文件 10 条目（2V/8D）+三态矩阵定案 |
| L016-2 npm ghost | dev-registry-adapter | ✅ 守卫改 tarball 路径冲突语义（G1 三臂复验 403 逐字） |
| L016-3 呈批整备 | compatibility-engineer | ✅ v2.3 六席位（整包 20 席可一次批复） |

## Differential
- pypi：19 臂 9 同/10 差（2 BUG 候选〔requires-python 三源全丢/坏元数据入索引〕+6 UNKNOWN+legacy UNSUPPORTED）
- npm ghost：G1-G3 三臂双端复验（403/201/201 同形）；G4 归 L015 既有 UNKNOWN 不猜

## Fixed Gaps
- npm ghost publish 守卫（latest 劫持面收口）；T-261 预留决策兑现；误入 git 的 fixtures 解除

## Interruptions
- Docker Desktop 第三次僵死（25 分钟——runbook 自愈）；一次 agent 误卷文件（发现即清）

## Compatibility Score
- 维持 ✅74/◐17（49.2%）；契约四域（docker 25+storage 10+npm 17+pypi 10）；台账 44 条
- **呈批包 v2.3：20 席位可一次批复 + 3 例外点名**

## Next Priority（LOOP 017）
1. **用户批复驱动**（20 席位整包——批复即连锁：台账终态/契约冻结/实现票拆发）
2. pypi 双 BUG 实现票（requires-python 三源/坏元数据不入索引）+ maven C3 规格票（pom 前置假说）
3. pypi 六 UNKNOWN 中可预授权臂（若有）探针
4. crafted 文档门槛二分票（npm G4 素材备）
5. 扩章第四域评估（docker/storage/npm/pypi 后的下一高价值面）
