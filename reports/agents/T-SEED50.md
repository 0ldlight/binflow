# T-SEED50 — e2e seed 基建跟上 ADR-0050（PUT-as-replace → GET-first ensure）

Ticket:        T-SEED50 · e2e seed 基建跟上 ADR-0050 · P1（T-UIB1 实测 251/267 seed 失败的存量债根因之一）
Role:          devops-engineer
Area:          e2e seed/fixture 脚本（web/scripts/seed-*.mjs + .d.mts）——ticket 明示的 seed 基建子域，非 CI workflow/Makefile 面
Input:         派发单（ADR-0050 背景 + GET-first 修法 + 禁 docker/禁触碰 172.16.58.130）；自读 DECISIONS.md ADR-0050 全文（决策 2：PUT=create-only，PUT-on-existing → 400 `Repository key already exists`；决策 1：POST=merge 三列矩阵）；internal/httpapi/repositories.go handleRepoPut 现状（ADR-0050 已实现：create-only + 400 字面）；三个 seed 脚本全文
Changes:
               1. seed-m8.mjs：新增共享 `ensureRepo(client, def)` —— GET-first：404 → PUT create；200 → POST 同全量 body merge（merge-on-omit 下 seed 自有席位全显式，收敛于确定性配置且不动仓内容——DELETE+重建会毁掉 10k 节点树与 m9 usage fixtures，故 POST merge 是唯一诚实选择）；PUT 撞 400 时 re-GET 见 200 则 POST merge 收敛（fullyParallel 并发首建的竞态收敛，T-326 D-9① 的 ADR-0050 后继形态）。`seedRepos` 改为委托 ensureRepo（默认 description 保持原 wire body 不变）
               2. seed-m9.mjs：seedM9 内联 PUT 循环（x50 仓）改调 ensureRepo；头部 wire 图与「converges on re-run」注释按 ADR-0050 更新
               3. seed-m10.mjs：删除本地 `ensureRepo`（注释还写 "Full-replace repo upsert (the M1 wire posture; 200 replace)"），改为 import + re-export seed-m8 的共享实现（导出面不变，e2e/m10/support/seed.ts 零改动）；seedM10 docstring 的「steps are idempotent full-replace PUTs」改为区分 user PUT 与 repo ensureRepo 两形态
               4. 过时注释清理：seed-m8 头部 `repos PUT ... (200)`、seedRepos "Accepts 200 (replace) and 201 (create)"、converge() 注释补 ensureRepo 的 400 竞态例外——全部与 ADR-0050 对齐
               5. **同病第二处（盘点发现，非 ADR-0050 面）**：seed-m8 `countTreeNodes` 用 `?list&deep=1` 计数，但 53522a6c（L009，2026-09-12）起 folder rows 仅在 `listFolders=1` 时进 files[]——树计数 1'121 vs 计划 10'291（files-only 漏 9'170 个 folder 节点），seed 自带验证门恒 exit 1。加 `&listFolders=1` 修复（实测该参数下 deep list 恰返回 10'291）
               6. 类型面：seed-m8.d.mts 增 `ensureRepo` 声明；seed-m10.d.mts 注明 re-export
Files:         web/scripts/seed-m8.mjs · web/scripts/seed-m9.mjs · web/scripts/seed-m10.mjs · web/scripts/seed-m8.d.mts · web/scripts/seed-m10.d.mts · reports/agents/T-SEED50.md
Tests:
               - ADR-0050 语义活体取证（隔离实例 127.0.0.1:18085）：PUT-on-existing → **400**；POST merge-on-existing → **200**（疾病与药方两端都钉死）
               - seed-m8 ×2：run1（fresh）tree 1120 files 落地、exit 0；run2（共用实例）repos=200（POST merge 路径）、tree skipped（0.2s recount）、**exit 0 零 400**
               - seed-m9 ×2：50 repos / 20 users / 10 groups / 4 targets，verify 双轮 green（u8 granted=404 denied=403、u9 covered=201 uncovered=403、E1 usage 双腿）；run2 targets 全 'present'，零 400
               - seed-m10 ×2：双仓 + 5 个 literal-';' fixture byte readback 全 200/bytes=true，verify 双轮 green；run2 grant='present'，零 400
               - Playwright（已 seed 两轮的实例 = ticket 的「共用/二次实例」失败面）：`e2e/m8/artifacts-tree.spec.ts`（seedRepos 10 个调用点）+ `e2e/m8/helpers.spec.ts`（直接断言 seed 模块与 countTreeNodes ≥10,000 门）→ **17 passed (17.1s)**；helpers:27 树门在 listFolders=1 修复后 951ms 过
               - 门：eslint（5 个改动文件）零输出；`tsc --noEmit` 零错；`go build ./cmd/binflow-server` 零错（隔离实例即此产物）
Commands:      （可复制重放；端口 18085 为本票隔离实例）
               ```
               mkdir -p /tmp/bf-seed50 /tmp/bf-seed50-data
               go build -o /tmp/bf-seed50/bf ./cmd/binflow-server
               printf 'server:\n  listen: 127.0.0.1:18085\nstorage:\n  data_dir: /tmp/bf-seed50-data\n' > /tmp/bf-seed50/binflow.yaml
               (cd /tmp/bf-seed50 && ./bf serve &)   # healthz: 200
               curl -s -o /dev/null -w '%{http_code}\n' -u admin:password -X PUT -H 'Content-Type: application/json' \
                 -d '{"rclass":"local","packageType":"generic"}' http://127.0.0.1:18085/binflow/api/repositories/m8-perf-local   # 400
               curl -s -o /dev/null -w '%{http_code}\n' -u admin:password -X POST -H 'Content-Type: application/json' \
                 -d '{"rclass":"local","packageType":"generic","description":"x"}' http://127.0.0.1:18085/binflow/api/repositories/m8-perf-local  # 200
               cd web
               node scripts/seed-m8.mjs  --base http://127.0.0.1:18085   # x2, 第二轮 exit 0
               node scripts/seed-m9.mjs  --base http://127.0.0.1:18085   # x2, verify green
               node scripts/seed-m10.mjs --base http://127.0.0.1:18085   # x2, verify green
               BASE=http://127.0.0.1:18085 npx playwright test e2e/m8/artifacts-tree.spec.ts e2e/m8/helpers.spec.ts  # 17 passed
               npx eslint scripts/seed-m8.mjs scripts/seed-m9.mjs scripts/seed-m10.mjs scripts/seed-m8.d.mts scripts/seed-m10.d.mts
               npm run typecheck
               pkill -f '/tmp/bf-seed50/bf serve'; rm -rf /tmp/bf-seed50 /tmp/bf-seed50-data
               ```
Outputs:       共享助手 `ensureRepo`（web/scripts/seed-m8.mjs，m9/m10 复用）；修复后的 seed-m8/m9/m10 + .d.mts；countTreeNodes 的 listFolders=1 探针修复
Compatibility: 正向——seed 与 ADR-0050（PUT=create-only / POST=merge）语义对齐，共用/二次实例不再大面积 400；POST merge 不动仓内容，10k 树/usage fixtures 跨 re-seed 存活（幂等 skip 路径保持）；wire body 与旧 PUT 逐字节同构（默认值原位保留），fresh 实例首跑行为不变。countTreeNodes 修复对齐 L009 后的 storage list 契约
Security:      无新增面：不触 secret、不外发；admin 凭据沿用既有 env-first 链（ADMIN_USER/ADMIN_PW），验证实例为本机 throwaway（已删）
Performance:   二次 seed 显著变便宜：repo 面 GET+POST 两次轻调用替代整轮 PUT；树面 recount-skip 0.2s（修复前计数 bug 使 skip 门永远不满足、每轮全量重 PUT 1120 文件）；Playwright 17 specs 17.1s
Risks:         ① ensureRepo 的 POST merge 依赖「seed 自有席位全显式」——未来给 seed 增加可省略席位时须记得 merge-on-omit 不重置未传字段（ADR-0050 三列矩阵）；② spec 文件内仍有大量自有 repo PUT（repositories-admin/t415/t416/m16 族等 ~20 文件）——fresh+uniq-key 形态不受影响，但共用实例二次跑同样会 400，属 dev-frontend 的 e2e 域存量债，建议另派票；③ m8 seed 用户面 PUT-on-existing 回 201（非 200）——2xx 语义内、非本票范围，留痕
Blockers:      无
Next:          ① 派 dev-frontend 票清 spec 内联 repo-PUT-as-replace（共用实例面）；② T-UIB1 在其 dev 实例上重跑 e2e 复核 251/267 失败面收敛；③ known-divergence/matrix 域无需动作（本票纯 test 基建，行为面零改动）
