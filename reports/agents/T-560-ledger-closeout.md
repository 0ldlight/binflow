# T-560 工作日志 — R6 轮末台账收官批（L034 双轮 PASS 后 conductor 授权翻面）

Ticket:       T-560 R6 轮末台账收官批（依据 reports/compatibility/L034-r6.md；conductor 授权：报告未判 PASS 的面不翻）
Role:         compatibility-engineer（rest + maven 双域实例，收官批特批跨域）
Area:         docs/compatibility/（known-divergence.yaml / contracts/ / golden/ / matrix.yaml）
Input:        reports/compatibility/L034-r6.md（Arms 1-8，r1≡r2 双轮）；修复锚 6812f271（T-555）/ 531ea002（T-559）/ 23b70a82（T-561）；fix commit 注记与 put.go 渲染点取证（git show 23b70a82）
Changes:
  1. 台账七翻：rest/nonempty-repo-delete-cascade-guard + d08/repo-delete-response-envelope（成功体半边；release-bundles 系统仓臂席位保留）；maven/handle-policy-reject-409-wording-family（note 含长路径定谳：A 无服务端截断、634 字符全量、300 上限系 L033 harness 证据截断）+ handle-policy-member-get-class-gate + plain-snapshot-path-resolve-404（存储面翻、walk 族归 T-562/BIN-44 维持红）+ derived-sidecar-lm-per-request-stamp（caveat 清偿注记 + LM 值级残差 raw 不裁）+ deploy-created-envelope-uri-missing-context-prefix——各加 resolved{date:2026-09-29, evidence:L034, fix_ref}，classification 不动（resolved 块即闭态，沿既有体裁）
  2. 新立账：maven/deploy-201-location-header-context-prefix（BUG；authority=L034 Arm 8 直测；渲染点 put.go requestBase 两调用点〔报告锚 437/585，T-563 修复在途行号漂移以函数锚为准〕；review_gate=T-563/BIN-45 修后 Location 臂 mini 差分双轮 PASS 翻 resolved）
  3. 契约翻面+升级：repositories.yaml#rest/repo-delete-nonempty-cascade → VERIFIED（计数口径回填：文件+folder 全量、仓库根不计；?deleteContent=true 实测冗余同形；empty/virtual 腿 adjacent_observed）；maven-virtual.yaml ⑤409 文案族/⑥成员 GET class 门/⑦plain 存储面/⑧sidecar LM → VERIFIED，其中 ⑧ derived-sidecar-lm-materialized-stamp confidence medium→high（L034 Arm 4 A 面跨秒直接腿 1.3s 实测 304×3，L033「不可达」caveat 清偿；LM 值级残差 A 晚 body ~2s vs B 相等入 value_level_note 如实不裁）；⑨auto-materialize 维持 DIVERGENT（L034 未触其面）
  4. matrix.yaml：D02-R05 golden_ref 回填 + last_difftest L034 Arm 1 链；D12-R18 last_difftest 追加 L034 收官轮注记（六面 PASS×2 + Location 立账 + walk 族维持红）；summary golden_ref_non_null 2→3、last_difftest_non_null 51→52；header changelog「L034 收官批」块（含台账实数 87=44 resolved+43 open）；195 冻结行集未增删
  5. 提金四组（12 文件，A 腿原文照 L034 报告记录、未自行改写）：G1 rest/repo-delete-report-body、G2 maven/handle-policy-409-wording-family（634 字符长路径锚在构造+长度级，逐字符实值以 wire run 为准——不造值）、G3 maven/deploy-201-envelope-uri-location（uri/downloadUri/Location 三形同源；B 面 Location 为修复前参照）、G4 maven/derived-sidecar-crosssec-304（=契约⑧ confidence 升级锚）
Files:
  改 docs/compatibility/known-divergence.yaml、contracts/maven-virtual.yaml、contracts/repositories.yaml、matrix.yaml
  新 docs/compatibility/golden/rest/repo-delete-report-body/{metadata,request,response}.yaml、golden/maven/handle-policy-409-wording-family/…、golden/maven/deploy-201-envelope-uri-location/…、golden/maven/derived-sidecar-crosssec-304/…（各三件）
Tests:       probe 无新跑（本轮纯台账/契约/金样；证据源=L034 r1/r2 双轮差分，差分执行域归 differential-qa-engineer）
Commands:
  - YAML 门 16 文件：python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" 逐文件 → 16/16 PASS（known-divergence/matrix/maven-virtual/repositories + golden×12）
  - 台账计数（python yaml 解析枚举）：total 87；classification 全量 BUG 41 / UNKNOWN 37 / INTENTIONAL_DIFFERENCE 6 / UNSUPPORTED_FEATURE 3；resolved 块 44；open 43 = BUG 6 / UNKNOWN 31 / INTENTIONAL 5 / UNSUPPORTED 1
  - 七翻复核：逐条 resolved@2026-09-29 + evidence=L034 锚（逐条打印核对）
  - 引用完整性：golden 四目录存在；matrix.yaml L944 golden_ref 命中；契约 evidence golden-capture 四锚命中（maven-virtual L329/L493、repositories L87）；新账 id 在 matrix L2856 D12-R18 命中；L034-r6.md 在档
  - 契约面核对门：router.go L1056 s.handleRepoDelete（D02-R05 面）；internal/adapter/maven/ X-Checksum-Deploy / handleReleases·handleSnapshots / If-Modified-Since（derived_sidecar_stamp_test.go L67-69 跨秒 304 断言）均 grep 命中
Outputs:      台账翻面 7 条 resolved + 新立 1 条；契约 VERIFIED 翻面 5 条（rest 1 + maven 4）；confidence 升级 1 条（maven/derived-sidecar-lm-materialized-stamp medium→high）；matrix 行更新 2（D02-R05/D12-R18）+ summary 2 计数器 + header changelog；金样 4 组 12 文件
Compatibility: 台账 BUG 净变化 = -6 翻 +1 立 = -5（open BUG 6，含新 Location）；四分类终值：全量 BUG 41/UNKNOWN 37/INTENTIONAL 6/UNSUPPORTED 3，resolved 44，open 43（BUG 6/UNKNOWN 31/INTENTIONAL 5/UNSUPPORTED 1，其中 4 INTENTIONAL resolved-by-*/parity 闭态 + 1 UNSUPPORTED 维持不做闭态——Z 由 conductor 复算）；契约状态机 DIVERGENT→VERIFIED ×5；⑨ auto-materialize 维持 DIVERGENT；plain-SNAPSHOT walk 族（T-562/BIN-44）按报告维持红未翻
Security:     契约/金样 authentication 面均为 Basic admin 差分腿（金样 metadata 注明凭据环境注入零持久，L034 既有实践）；无新增权限位断言
Performance:  本批无 timing/p95 面断言（Arm 4 时间语义=LM 物化稳定戳与跨秒 304 判定，非性能预算面；1.3s 间隔为构造参数）
Risks:        ①Location 渲染点行号锚随 T-563 修复漂移（账内已注以函数锚 requestBase 为准）②G2 长路径 634 字符锚在构造+长度级（逐字符以 wire 为准——防造值取舍，复验时按 wire run 比对）③worktree 内 put.go/created_envelope_uri_test.go 存在并行 dev 未提交改动（T-563 修复在途），本批未触碰任何代码文件
Blockers:     无
Next:         转 differential-qa-engineer：T-563 修复后 Location 臂 mini 差分（双轮 PASS → 翻 maven/deploy-201-location-header-context-prefix）+ T-562 walk 族（维持红非回归）；转 conductor：Z 复算用上方四分类终值；G3 金样 B 面 Location 臂修复后随翻正更新

---

# 复工微腿 — Location 账翻面（T-563 mini-diff 验收，2026-09-29 同日追加）

Input:   L034-r6.md 末尾「Location mini-diff」节（已提交 601b15ac）：B 自 4481359f 重建 scratch，case maven-deploy-201-location-header 六臂 r3≡r4 双轮 PASS——字节部署三臂+零传输臂 Location==envelope uri（带 contextPath、GET 200+sha-ok）；checksum-file .sha1/.md5 注册式 Location==带前缀 TARGET（.pom 路径）+ body 空（len=0）+ TARGET GET 200；裸根形态消除；r3 首跑 meta_loc_get 失配=case 侧误钉字节（T-561 口径只钉 200），如实注记不立账
Changes:
  1. maven/deploy-201-location-header-context-prefix → resolved（review_gate 满足）：resolved{date:2026-09-29, evidence=mini-diff 节六臂双轮+裸根消除+meta_loc_get 注记, fix_ref=T-563/BIN-45（4481359f，票 Done）+差分门 601b15ac}
  2. 金样增补（并入 G3 deploy-201-envelope-uri-location 组，同族面不另立）：request 增 checksum-file .sha1/.md5 注册式两步；response 增两臂（Location==带前缀 TARGET、body 空——rest-api §1.5 该面首个双端活体锚）+ b_face_location 改修复后断言面 + b_face_location_pre_fix 存档；metadata 更新 capture/history/instance（B @ 4481359f）/known_gaps（全串逐字符实值锚 wire r{3,4}，表内省略号不展开造值）
  3. matrix：D12-R18 last_difftest 追加 mini-diff 链（引号形态保持）；header changelog 增「L034 Location mini-diff 复工微腿」段（台账实数更新 87=45 resolved+42 open、open BUG 5）
Commands: YAML 门 5 文件（known-divergence/matrix/G3×3）5/5 PASS；计数复报（python 枚举）：total 87、全量 BUG 41/UNKNOWN 37/INTENTIONAL 6/UNSUPPORTED 3（不变——resolved 是闭态叠加不换 classification）、resolved 块 44→45、open 43→42 = BUG 6→5 / UNKNOWN 31 / INTENTIONAL 5 / UNSUPPORTED 1；Location 条目 resolved@2026-09-29 且 evidence 含 4481359f+601b15ac 校验 True/True；BUG 净变化本批 -1（收官批 -5 后再 -1，R6 全轮 BUG 净 -6）
Outputs: 台账翻面 1（maven/deploy-201-location-header-context-prefix→resolved）；金样文件 3 改（无新组）；matrix 1 行链追加+changelog 1 段
Risks:   checksum-file 臂 Location 全串在金样锚表形+wire（L034 表内省略号省 host/组段——不造值，复验按 wire r{3,4} 比对）；泛化面（其他协议 deploy 201 Location）仍未取证，维持原注
Next:    T-562 walk 族维持红非回归（差分侧）；其余 open BUG 5 条各归既有票
