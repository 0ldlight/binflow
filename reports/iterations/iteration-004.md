# Iteration Report — LOOP 004（304 透传 + C14 定谳 + storage-admin 契约化 + ADR-0049）

- **Iteration**: 004
- **Date**: 2026-09-12
- **Gap Before**: §8-C docker 面半落地（归属失实）；ping 消息粒度 UNKNOWN；C14 消失机理未谳；storage-admin 零契约；storage-v2 终案无 ADR
- **Goal**: 补票清偿 + 探针定谳 + 契约第二波 + authority 落册

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L004-1 §8-C+ping 分型 | dev-registry-adapter ×3 振 → **conductor 接管** | ✅ client_conditional.go 四纯函数 + bearerRefusalMessage 单决策点分型；30 case×2 STABLE 差分 |
| L004-2 C14 双臂探针 | differential-qa-engineer | ✅ 定谳：消失是幻象（T-363 诞生即缺）；C14→BUG；ADR-0048 自动作废 |
| L004-3 契约第二波 | compatibility-engineer | ✅ 25 条目账实一致；E3-2/E3-3 勘误段；D1/D2/D3 登记；并行交错 YAML 损坏自愈 |
| L004-4 ADR 正式化 | architect | ✅ ADR-0049 + Errata×2（0048 自动作废/0015 P2 债吸收）+ B1 设计稿勘误 |

## Implementation（L004-1，conductor 收编）
- 客户端条件 GET 面：quoted INM/IMS→304 裸头本地应答（manifest 仅 CacheHit 臂）；blob 面 quote 不敏感；非匹配在场封死日期臂
- 上游臂：304→reland 窗口滑动+REVALIDATED（与引擎腿同常量）——§8-C 活体直证
- ping 分型：TokenRegistry.Verify 单决策点（unknown→Props Token not found / expired→Token failed verification: expired / Basic 族→Bad Credentials）；revoked 臂模型级不可达（D2 登记）
- gosec G101 误报 #nosec 注记（隔离模块复现实证为字面量误报）

## Differential（30 case ×2 轮 STABLE）
- SAME 22（客户端可见 26）；DIVERGENT 3：D1 unquoted INM（BUG，真实客户端均 quoted 影响低）、D2 revoked 臂（INTENTIONAL 候选）、D3 过期 blob 回源（UNKNOWN，客户端不可见）
- ping 三消息臂逐字节一致；§8-C 过期 tag 重验证链直证（上游 304→窗口滑动→客户端 200）
- **E3-2 勘误成立**：参照三拼写均回 304；原「永不 304」存活子集={非匹配/HEAD/过期窗}；L000-F C08 的 SAME 锚在伪前提上——契约已重写翻 DIVERGENT

## Review
- A（correctness）：APPROVE，0 blocking（2 条 LOOP 005 随带：D1 注释连修、origin 不对称）
- B（architecture）：REQUEST_CHANGES→唯一 blocking=docs-only（设计稿 §5.2 旧禁令与 as-built 冲突的回退诱雷）→architect 勘误清偿，代码面无需重审
- 双审战绩 5/5（本轮：设计稿-代码失同步由 B 揭出）

## Interruptions / Loop Health
- 限额击落第二波（L004-1/2，断点复活）；L004-1 三振（2×负载尖峰失速+1×串行后仍失速）→ conductor 亲自收编（半成品 build 过续作）——**接管制首次实战**
- 机器负载尖峰（load 118）教训入纪律：agent 串行命令、禁后台扇出

## Compatibility Score
- Before: 契约 23 条目（20 VERIFIED）；C14 UNKNOWN；storage-admin 无契约
- After: **契约 25 条目**（storage-admin 10 + docker-remote 25 合计两文件；VERIFIED 含 blob-conditional/revalidation-serve 新立）；**matrix 行级翻态 +2**（累计 4）；divergence 台账 15 条目（resolved 7 / BUG 3 / UNKNOWN 5 含待裁）；ADR +1 新 +2 勘误
- 证伪闭账：E3-2/E3-3（L000-B 观察）、C14 双前提（L000「更优」+E6-3「参照未生效」）

## Fixed Gaps
- §8-C 完整落地（引擎腿+docker 客户端面+上游臂闭环）；ping 分型；storage-admin 契约化；storage-v2 authority；ADR-0048 前提修正

## Next Priority（LOOP 005）
1. **C14 实现 + D1 修复合票**（manifest 冷 miss 负缓存 + unquoted INM 按值匹配——修后**重跑全 304-matrix**（M6/M2b 偶合可能翻面）+ 连修三处证伪注释）
2. backup 成功臂装配实例复验 → storage-admin/backup 翻 VERIFIED
3. 6 新端点行入账 + D01-R30 重分类（compatibility-engineer 增量）
4. storage N1-N8 微票批（progress 钳制/ErrBlobNotFound 良性分类/E5 测试/gc 审计行）
5. matrix partial P0 面（11 行）攻坚 + maven/npm 契约扩面
6. 用户待裁 9 项驱动（token TTL 批准即灭 D2 所在契约族大半）
