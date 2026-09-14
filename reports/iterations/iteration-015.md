# Iteration Report — LOOP 015（三 UNKNOWN 入账 / likePrefix 潜伏 bug / 扩章择优）

- **Iteration**: 015
- **Date**: 2026-09-13
- **Goal**: 台账登记 / client 存量定性 / 扩章第三票取证

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L015-2 台账轮 | compatibility-engineer | ✅ 三 maven UNKNOWN 入账+双素材（build.timestamp R-17 候）+matrix 核验 |
| L015-3 client 存量 | dev-go-core | ✅ 双定性：断言翻面+**likePrefix 潜伏 bug**（T-10 起 LIKE 转义文本入精确比较——DeleteByPrefix 同病） |
| L015-4 扩章取证 | differential-qa | ✅ 24+6 臂双域；**pypi simple 择优**；npm ghost 守卫深坑另立 |
| L015-1 R-15a/b | conductor→user | ⏸ 候终裁 |

## Differential
- pypi 12 臂：头模板/上传/ETag 同；差异密集（requires-python 丢属性/链接段/rel/索引即时性/302 形态/legacy）
- npm 12 臂：主链 wire 逐字同（§5-2/3 挂账清偿）；ghost publish 守卫分叉（ref 403 vs B 201 劫持 latest）

## Fixed Gaps
- likePrefix % 路径 404+漏删（seam 修，9 测试腿）；5 断言翻面；全树 41 包绿

## Interruptions
- Docker Desktop 两次整体僵死（构建+同 VM 栈并发——停 penpot 栈自愈后复原）

## Compatibility Score
- 维持 ✅74/◐17（49.2%）；台账 44 条（resolved 27；UNKNOWN 增 3 maven）
- npm publish 主链实证同形（未契约化——并入后续票）

## Next Priority（LOOP 016）
1. **R-15a/b 终裁驱动**（P0 清零）+ login-missing/build.timestamp 裁定包合流
2. **pypi simple 扩章首票**（四段闭环开工——probe 补 pip/twine 腿→契约→实现→批跑）
3. npm ghost-publish 深水票（守卫语义+latest 劫持）
4. 三 maven UNKNOWN gate 到期裁定（XML 形态/手 PUT/C3 规格票）
5. Penpot 残留+4 硬阻塞 gap（候用户）
