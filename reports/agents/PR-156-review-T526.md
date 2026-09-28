# PR-156 Review · T-526 Fern en 页数 ratchet 门（Reviewer A）

Ticket:        T-526 [P2] Fern 英文站页数只升不降门（CI ratchet）
Role:          code-reviewer (reviewer-a · correctness)
Area:          CI/工具链（workflow 域）——非双审强制域（storage/security/repository/remote cache/protocol/replication/migration 均不涉及），Reviewer A 单审合法
Input:         conductor 派发（T-id、AC 四条、文件清单、reviewer-a 形态）；通读 scripts/fern-en-ratchet.sh 全文、fern/en-pages.count、Makefile/.github/workflows/ci.yml/.circleci/config.yml 未提交 diff、reports/agents/T-526.md 实施记录；追读 .github/workflows/ci.yml 触发面（1-35 行）与 .circleci/config.yml build job 结构（150-190 行、parallelism 上下文）
Changes:       评审范围 = 六文件工作树改动（未 commit）：新增 scripts/fern-en-ratchet.sh（101 行）、fern/en-pages.count（基线 10）、Makefile 两 target + .PHONY + dev 前置链、GH ci.yml 加步、CircleCI config.yml 加步、T-526.md 实施记录。上下游追读：make dev DAG 解析、GH `ci` job 步序、CircleCI build job parallelism=2 shard 语义
Files:         scripts/fern-en-ratchet.sh — 通过：set -euo pipefail；全变量展开加引号、比较值恒为内生路径或纯数字（70 行 tr -dc '0-9' 后 BASELINE、69 行 find|wc 后 CURRENT），无注入面；三分支（DROPPED/GREW/缺基线）显式 exit 1 且各带修复指引；--write 拒绝空树写基线（43-46 行）；shellcheck 零告警。 fern/en-pages.count — 通过：内容 "10\n"，与独立实测计数一致。 Makefile — 通过：fern-en-ratchet / fern-en-baseline 与 .PHONY 扩展逐字一致；recipe 显式 `bash` 调脚本（脚本依赖 BASH_SOURCE 与 pipefail，不落 POSIX sh，正确）；dev 前置链次序 spec-check→fern-en-ratchet→vet→lint→test→build 经 `make -n dev` 证实。 .github/workflows/ci.yml — 通过：`ci` job 内 spec-check 后、"Set up Go" 前加 `make fern-en-ratchet`，无 id/输出依赖，仅 bash+make+find 依赖（该 job 已跑 make spec-check，工具存在性已被现网证明）；actionlint 零报错。 .circleci/config.yml — 通过：build job（parallelism: 2）spec-check 后同位加步；gate 模式只读、双 shard 各自 workspace 独立判定，幂等成立；`circleci config process` 通过。 reports/agents/T-526.md — 记录与实测一致（其声称的三态 FAIL/PASS、幂等往返、actionlint/circleci 校验结论本评审全部独立复现，无虚假证据）
Tests:         全部独立实跑（不采信 agent 自测）：① make fern-en-ratchet → PASS exit=0（current=10 baseline=10）；② 基线改 11 → DROPPED 分支 FAIL，脚本层 exit=1（make 层 exit=2，均非零即 CI FAIL）；③ 基线改 9 → GREW 分支 FAIL exit=1，指引 make fern-en-baseline；④ 基线移除 → missing-baseline 分支 FAIL exit=1 带修复指引；⑤ 还原后 cmp 逐字节一致 + 复跑 PASS exit=0；⑥ bash -n exit=0；⑦ shellcheck exit=0 零告警；⑧ actionlint ci.yml exit=0；⑨ circleci config process exit=0；⑩ python3 yaml.safe_load 双文件解析 OK（ci.yml 顶层 `True` 键为 YAML1.1 对 `on:` 的正常解析，非错误）；⑪ 独立计数 find fern/translations/en/pages -type f \( -name '*.md' -o -name '*.mdx' \) | wc -l → 10，且清单全为 .mdx（.md 0 个），基线值正确（AC3）；⑫ make -n dev → DAG 中 ratchet 位于 spec-check 之后、vet/lint 之前
Commands:      make -C <wt> fern-en-ratchet；bash -n <wt>/scripts/fern-en-ratchet.sh；shellcheck <wt>/scripts/fern-en-ratchet.sh；actionlint <wt>/.github/workflows/ci.yml；circleci config process <wt>/.circleci/config.yml；python3 -c "import yaml; yaml.safe_load(...)"（双文件）；find <wt>/fern/translations/en/pages -type f \( -name '*.md' -o -name '*.mdx' \) | sort / | wc -l；变异序列：printf '11\n' > fern/en-pages.count → make（FAIL）→ printf '9\n' → make（FAIL）→ mv 基线 → make（FAIL）→ mv 回 + cp 备份还原 → cmp + make（PASS）；直跑 bash scripts/fern-en-ratchet.sh（基线 11 态）证实脚本层 exit=1；make -n dev | grep -n "fern-en-ratchet.sh"（DAG 位次）；make -n fern-en-baseline（干跑确认 recipe = bash scripts/fern-en-ratchet.sh --write）
Outputs:       reports/agents/PR-156-review-T526.md（本报告）；基线文件经变异测试后逐字节还原（cmp 证实），工作树无评审残留改动
Compatibility: 本视角要点：闸门链九段零改动（两面均为纯加步、位于 spec-check 同位）；双 CI 触发面未触碰——GH ci.yml 1-16 行证实 push main only（+workflow_dispatch 仅 e2e 面），AC2「手动上调基线」定案的前提（CI 无 PR checkout 可回写）成立；与 spec-check 先例姿态一致，双面共用同一 make 入口无面间漂移
Security:      无攻击面：脚本无外部输入求值（$1 仅字面比较）、无 secret、CI 不写任何跟踪文件；路径全由 BASH_SOURCE 推导；失败方向恒 fail-loud（基线缺失/非数字/页树缺失均 exit 1 而非静默放行）
Performance:   单步 find+wc 实测量级 <0.1s；CircleCI 双 shard 各跑一次只读判定，无锁无共享态；GH 面置于工具链 setup 前 fail-fast 前移，不占缓存面
Risks:         既定边界（AC4，票面已定案）：同数置换（删 A 加 B）不拦——脚本头注释与 T-526.md Risks 均已载明，页级清单门属 T-521 后续域，不构成本票缺陷。make 层 exit=2（非 1）为 make 对 recipe 失败的标准包装，CI 只判非零，无影响
Blockers:      无——全部取证命令可跑且结果与实施记录一致
Next:          ① 合入后首轮 main push 观察双面 "Fern en-pages ratchet" 步真实绿灯（本评审环境无法触发远端 CI，属 contract 首轮验证，非缺陷）；② en 页扩充 PR 记得 make fern-en-baseline 随票提交（GREW 分支会强制提醒，流程自洽）；③ 范围外未见：internal/remote/projection.go(+projection_test.go)、internal/adapter/maven/virtual_metadata.go、reports/agents/T-524.md 为并行票在途文件，本评审未触碰

## 评审报告 T-526（形态: reviewer-a）

结论: APPROVE

### 必须修改（blocking）

无。四条 AC 逐条核实：AC1 双面各一步（spec-check 同位、make 单源入口）成立；AC2 票内定案「上升也 FAIL + 手动上调基线随票提交」且实现与定案一致（双向 FAIL、修复指引明确、CI 零写入，触发面前提成立）；AC3 基线=实测 10（独立复数证实）且票面记录了实测命令；AC4 count-only 口径与票面一致（md+mdx、当前树全 .mdx），同数置换边界已文档化。

### 建议改进（non-blocking，Low）

- scripts/fern-en-ratchet.sh:42 — 未知参数（如 `--wriet` 手误）静默落入 gate 模式而非报 usage；可加一行非 `--write` 参数即拒绝。影响极小（唯一调用面是 Makefile 固定传参）。
- scripts/fern-en-ratchet.sh:70 — `tr -dc '0-9'` 对 "1a0" 类手误基线会静默取 "10"；空值已拦（71-74 行），且 Makefile help 已声明 "Never hand-edit"，可接受。

### 范围外发现

无（工作树中 internal/remote/projection.go、projection_test.go、internal/adapter/maven/virtual_metadata.go、reports/agents/T-524.md 为并行票 T-529/T-524 在途文件，依派发指令未评审）。
