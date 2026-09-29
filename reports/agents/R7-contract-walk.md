# R7-contract-walk — L035 walk 取证证据 + conductor 四裁定落账（T-562 stage 1 / BIN-44 契约定稿批）

```
Ticket:       T-562 stage 1（Linear BIN-44）walk 契约定稿批——L035 取证证据与 conductor 四裁定落 docs/compatibility/
Role:         compatibility-engineer（maven 域实例）
Area:         docs/compatibility/（contracts/ golden/ known-divergence.yaml matrix.yaml）
Input:        reports/compatibility/L035-r7-walk-forensics.md（主证据，r1-r5 + 人工 8 形态）；conductor R7 四裁定（2026-09-29 原文派发）；
              既有 contracts/maven-virtual.yaml（9 条目版）、known-divergence.yaml、matrix.yaml、golden/maven/ 三组在册金样（格式先例）
Changes:      ① maven-virtual.yaml 9→12 条目：⑨ auto-materialize 改写（A 面=module 级写路径物化/恒 +1/module；release 恒不物化；SNAPSHOT 同步物化；含 l033_erratum 块）；⑩ 新立 maven/plain-snapshot-unique-walk-resolve（裁定 1：W1 触发矩阵+W2 选择规则+W3 同面并票，规格闭合）；⑪ 新立 maven/non-snapshot-spelling-get-gate-404（裁定 3：t8 门对齐 A 404）；⑫ 新立 maven/virtual-plain-walk-cross-member-selection（裁定 2：跨成员 mtime 键另层另键）；⑦ unobserved 时间戳臂回填「与 walk 同面」；头部 changelog 追加 R7 批注记。
              ② known-divergence.yaml：auto-materialize 条目 UNKNOWN→BUG（authority=conductor_ruling 裁定 4；surface 修正 module 级；L033 否证扩证）；新增 maven/non-snapshot-spelling-400-vs-404（BUG，authority=conductor_ruling 裁定 3）；walk 主条目 plain-snapshot-path-resolve-404 review_gate 更新「规格闭合，实现票在途」+ resolved 残余段承接注记；derived-sidecar-lm-per-request-stamp rationale 追加 L033 前提勘误段。
              ③ matrix.yaml：**无新行**（195 冻结行集纪律，201 总行不变）；D12-R18 last_difftest 追加 L035 + note 追加 R7 段；summary golden_ref_non_null 注记 R7 四组挂链方式；头部 changelog 追加 R7 批注记（含 Z 中间值）。
              ④ golden 四组入册（A 腿 target 形态，B 腿待实现后补）：walk-trigger-matrix / walk-selection-rules / walk-virtual-cross-member-mtime / auto-materialize-module-level；derived-sidecar-validators known_gaps 追加 L033 勘误行。
Files:        修改 contracts/maven-virtual.yaml、known-divergence.yaml、matrix.yaml、golden/maven/derived-sidecar-validators/metadata.yaml；
              新建 golden/maven/walk-trigger-matrix/{metadata,request,response}.yaml、golden/maven/walk-selection-rules/{metadata,request,response}.yaml、
              golden/maven/walk-virtual-cross-member-mtime/{metadata,request,response}.yaml、golden/maven/auto-materialize-module-level/{metadata,request,response}.yaml
Tests:        无新 probe（本批为取证落账——evidence 取自 L035 已复跑双轮 r2≡r3 / r4≡r5 / case 级 r1≡r2≡r3；复跑入口 bash /tmp/l035-run.sh rN 见 L035 报告）
Commands:     YAML 门：python3 -c "import yaml;yaml.safe_load(open(f))" 逐文件过（契约+台账+矩阵+金样 12 新文件全 OK，输出见会话）；
              引用完整性：grep 契约 id（12 条目锚 maven/plain-snapshot-unique-walk-resolve:2 / non-snapshot-spelling-get-gate-404:3 / virtual-plain-walk-cross-member-selection:2 / deploy-put-version-metadata-auto-materialize:4）、台账锚（三 id grep 命中）、matrix D12-R18（row_id:2852 行命中）、契约 evidence golden-capture 四组反向命中（maven-virtual.yaml L592/679/680/772）；
              矩阵口径门：行数 201 不变、row-state Counter compatible 95/absent 57/not_applicable 20/partial 17/superset 12（与前拍一致）；
              台账枚举门：python3 逐条目计数（见 Compatibility 字段）
Outputs:      契约 3 份新立（maven/plain-snapshot-unique-walk-resolve、maven/non-snapshot-spelling-get-gate-404、maven/virtual-plain-walk-cross-member-selection）+ 1 份改写（maven/deploy-put-version-metadata-auto-materialize）+ 1 臂回填（⑦ unobserved）；
              matrix 行更新 1（D12-R18，无行增删）；known-divergence 新增 1 + 翻面 1（UNKNOWN→BUG）+ 承接注记 2 条目；金样 4 组 12 文件
Compatibility: 状态机变动：→DIVERGENT ×3（⑩⑪⑫ 新立即 DIVERGENT——B walk 缺席，对齐方向已裁）；⑨ 维持 DIVERGENT 改写（物化层级勘误+BUG 对齐已裁）；⑦ unobserved 臂回填（条目状态不变）。
              置信度：high 3（⑩⑪⑫）/ ⑨ high 维持（L035 Part 2 case 级 PASS ×3）。
              Z 中间值（R6 口径 python 枚举）：**total 88 = resolved 45 + open 43**；open 分布 BUG 7 / UNKNOWN 30 / INTENTIONAL_DIFFERENCE 5 / UNSUPPORTED_FEATURE 1；resolved 分布 BUG 36 / UNKNOWN 6 / UNSUPPORTED_FEATURE 2 / INTENTIONAL_DIFFERENCE 1。批前 87=45+42 → 批后 88=45+43：+1 新账（t8 open BUG）、auto-materialize open 内 UNKNOWN→BUG 移箱（open 总数 +1 纯新账贡献）；**本批不翻 resolved（实现未落地，Z 纪律）**。
Security:     契约 authentication 断言沿用 maven 域既有面（Basic admin 取证腿——L035 双 oracle 构造口径）；本批无新增权限位断言面、无凭据入册（金样 capture_instance 仅记主机/版本，无凭据）。
Performance:  契约 timing 面断言：⑨ 含物化时机锚（SNAPSHOT 写路径同步物化 39ms/58ms 观测值——时序断言非预算）；⑩⑪⑫ 无 p95 断言面（walk 族未立性能预算——L035 未拍时延维）。
Risks:        ① 金样四组 B 腿全空（walk 未实现）——stage 2 落地后须回补 B 腿并升双端对账形（各组 known_gaps 已标）；② L033 否证的 pom 前置解释为推定口径（「疑为」措辞在案，未单独复拍无 pom 腿）；③ m5 旁车臂 B 侧跳过（m0 未触发）待 stage 2 补测；④ W4 mtime 键跨主机部署时钟偏移面未取证（known_gaps 已标，超出本批域）。
Blockers:     无（规格闭合、authority 齐备——四裁定均有 conductor_ruling 引用）
Next:         待差分：T-562 stage 2 实现落地后复验本批 5 case（walk 族四取证腿 + auto-materialize）→ 契约 ⑩⑪⑫ DIVERGENT 复评、⑨ 对齐验收、金样四组补 B 腿。
              Followups（本批不动、报 conductor）：a) L033 报告本体（reports/compatibility/L033*）含「30s 不物化」记录——勘误只回填了 docs/compatibility 侧，报告侧按「历史报告不改」惯例留原样、由 conductor 决定是否加勘误头注；b) golden/maven/derived-sidecar-crosssec-304 有两处 L033 引用（grep 命中 metadata L15/response L12），但均指「跨秒直接腿不可达」caveat（LM 语义族）而非「30s 不物化」前提——无涉否证面，不改；c) T-555 报告的 level 归因（version 级→module 级）属 reports/ 侧历史文档，同 a) 口径。
```
