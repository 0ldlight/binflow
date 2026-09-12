# Iteration Report — LOOP 000（SYSTEM BASELINE）

- **Iteration**: 000
- **Date**: 2026-09-11
- **Mode**: dual-track（审计基线 + 首轮实现；LOOP 宪章 33 节首程）
- **Gap Before**: matrix v1 冻结集 ✅57/◐27/❌83/⛔17/超集11（195 行，机读账未建）；U-STG-01/U-PROTO-01 两条 P0 UNKNOWN open；UAT down；差分报告零存档
- **Goal**: LOOP 000 = 系统基线——主账单一化 + P0 双取证 + UAT 立起 + 首批有界修复

## Evidence
- Artifactory 参照（:8082）：存储链 E4 因果实验（L000-A）；docker remote E4+E1 测绘（L000-B 证据期）
- BinFlow UAT（:8083，uat-l000f-59f33ab5）：差分重放真实客户端腿（docker:27-dind + registry:3 上游）

## Tasks / Agents
| 轨 | Agent | 结果 |
|---|---|---|
| L000-A U-STG-01 存储链取证 | reverse-engineer | ✅ → partial（24 模板矩阵 + 行为 29 条；修正旧规格 4 处；新 UNKNOWN 8 条 U-STG-12~19） |
| L000-B U-PROTO-01 docker remote 对拍 | differential-qa-engineer | ✅ → partial（首份 E5 差分 19 case；P0 BUG 根因定位） |
| L000-C M3-1 主账迁移 | compatibility-engineer | ✅（rows=193 权威化；诚实勘误源 tally 漂移） |
| L000-D UAT 立起 | devops-engineer | ✅（:8083 healthy；发现 DEFECT-L000-1） |
| L000-E DEFECT-L000-1 修复 | release-engineer | ✅（3 Dockerfile +11 行；字节级出证） |
| L000-F P0 wire path 修复 | dev-registry-adapter | ✅ 含 Review A B1 返工（helmoci 6 测试红→绿） |

## Implementation
- `internal/adapter/docker/remote.go`：`v2WireManifestPath`/`v2WireBlobPath` 补 `/v2/` 前缀（P0——remote docker 拉取全灭的根因）；+`remote_wire_path_test.go` table-driven；两测试 fixture 语义迁移
- `internal/adapter/helmoci/{remote,virtual}_test.go`：同 seam fixture 迁移（test-only）
- `deploy/{release/Dockerfile.alpine,Dockerfile.distroless,dev/Dockerfile}`：补 `COPY web/public ./public`（vite publicDir 产物入镜像，wire-brand-assets 不再空）

## Tests
- `go test ./internal/adapter/docker/ -count=1` ok（33s）；`go test ./internal/adapter/helmoci/ -count=1` ok（108s，六点名例复跑绿）；`go test ./internal/repo/ -run 'Docker|Remote|V2'` ok
- build/vet/gofmt/golangci-lint 全零告警（conductor 复跑确认）

## Differential（验收主体）
- L000-docker-remote 批次 19 case：修复前 SAME 2/DIVERGENT 11/UNKNOWN 5 → **修复后 SAME 6/DIVERGENT 12/UNKNOWN 0**（五阻断案 C05~C08/C10 重放闭合；C04 pull digest 与上游逐字一致、上游日志单 /v2/ 直证灭因）
- 残余 DIVERGENT 12 已登记 known-divergence.yaml 六条目（BUG 4 族 + UNKNOWN 2）

## Performance
- NOT APPLICABLE：改动为纯字符串构造与构建链资产，无性能敏感路径；C09 Range 206 行为对齐顺带验证

## Security
- wire path 纯 egress 构造，入参全经校验（Reviewer A 确认）；SSRF 链未触碰；匿名 401/写面控制面 UAT smoke 实证；上游凭据不落盘（.env.uat gitignored）

## UAT
- :8083 = uat-l000f-59f33ab5（healthy）；smoke：login/push/pull/匿名读/401/持久化重启全绿；conductor 亲验 version/healthz/ui 200
- 遗留：`BINFLOW_REMOTE_CREDENTIALS_KEY` 未配（上游凭据静默降级匿名）→ LOOP 001 devops 票

## Regression
- **1 起，在环内捕获并修复**：wire 修复击穿 helmoci 6 测试（旧补偿 URL fixture 双 /v2）——双审 Reviewer A 定位、返工迁移、双包复跑绿。无出环回归。
- DEFECT-L000-1（compose 全量构建断链，M14/15 静默漏网）在环内发现并修复。

## Compatibility Score
- Before: ✅57 / ◐28 / ❌80 / ⛔17 / 超集11（matrix 193 行权威基线，本轮建立）
- After: 行级四态不变（契约化未开始，行翻态须 contract+difftest 判定）；**差分面实动**：docker remote surface SAME 2→6、UNKNOWN 5→0；unknown 总量 110→118（+8 存储），P0 unknown 3→1 partial（U-PROTO-01 residual=1 案族）
- divergences：0→6 条目（BUG 4 族待修 + UNKNOWN 2 待裁/取证）

## New Unknowns
- U-STG-12~19（存储 GC 资格窗/云链运行时/HA 协议等 8 条）；docker remote residual：C01b token TTL 字段集、C03 匿名默认策略、C14 负缓存对齐方向

## New Divergences
- known-divergence.yaml 首批 6 条目（见 Differential 节）

## Fixed Gaps
- P0：docker remote 上游 wire path（拉取全灭）——差分闭合
- 回归：DEFECT-L000-1 构建断链；helmoci fixture 击穿
- 基建：主账单一化（193 行）、UAT 平台、loop-state 状态账

## Next Priority（LOOP 001）
1. docker remote DIVERGENT 族实现票（P1 tags/_catalog 聚合 + P2 错误形态族一票收族 + P3 service 回显 + D21）
2. C10 marker 门控语义 → remote-cache-v2 设计输入（architect，联动 smart-remote）
3. 契约化起步：docker remote 面首份可执行契约（contracts/ 0→1）+ D12 行翻态
4. UAT 补 BINFLOW_REMOTE_CREDENTIALS_KEY + url 尾缀 /v2 的 Artifactory 取证（解 UNKNOWN 两条）
5. Phase 1 架构族继续：storage-v2（消费 L000-A 24 模板矩阵）
