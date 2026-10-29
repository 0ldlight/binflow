Ticket:       L042 Z 口径收账批（wave-1 报告「Z 口径更新建议」四条 + T-630/T-632 闭环记录；conductor 派票未编号，日志以 L042-Z1 记）
Role:         compatibility-engineer（本票实例域：docker-remote / console / rest 安全面收账）
Area:         docs/compatibility/（known-divergence.yaml + contracts/console.yaml + probes/console/）
Input:        reports/compatibility/L042-wave1-docker-remote-replay.md（分歧明细①②③ + 微差 N-1/N-2 + 新发现 F-1/F-3 + Z 建议 1-4）；
              docs/compatibility/rulings/T-631-allow-private-upstream.md（推荐案②，末尾防双账注记）；
              reports/agents/T-630.md + T-630-root-path-homepage.md（裸根首页实现票：f3bf1b78，真实实例 :18099 三点取证）；
              reports/agents/T-632.md + T-633.md（熔断实现票 99b9fd3a + 403 wire 接线票 c0c1ff1c）；
              活体：BinFlow dev-cfa5da05 @ 192.168.120.36:32083（k8s ns lzw，含 T-630/T-632/T-633 全部在案工作）
Changes:      按序——① known-divergence#docker/remote-token-response-shape 加基准复验注记（7.161.26 复验逐项一致，基准标注 7.161.15/20→7.161.26，加注不替换，维持 UNKNOWN）+ evidence 追加 L042 分歧明细②；② docker/remote-anonymous-token-policy 同款注记（分歧明细③）+ evidence 追加；③ docker/ssrf-private-upstream-stricter 加 note「L042 F-1 同源采认」（wave 提案 remote/allow-private-upstream-default 与本条目同一分歧，防双账不另立；条目本体 T-631 刷新版未动——classification 仍 UNKNOWN 待 authority，纪律遵守）；④ rest/login-failure-rate-limit 加实现进展注记（T-632 熔断已上线：阈值 5 保守 vs 参照观测约 2 / 窗口 4s / T-633 403 wire 已接线活体验证逐字文案 / per-username 已验；阈值精确计数差分在途 wave-2，阈值差=候选收敛项；条目维持 UNKNOWN 待差分绿由 conductor 翻账）；⑤ 新条目 docker/remote-ping-body-trailing-newline（note 级从轻：A ping 401 body 尾 \n 87B vs B 86B，JSON 语义等价，accepted 指纹注记不动 verdict、不进 normalize——L042 Z 建议 3 docker-remote 域维持 raw；classification=INTENTIONAL_DIFFERENCE 按 2026-09-11 LOOP 001 resolved 存证口径，authority=conductor_ruling 引 L042 N-1 + 本批派票）；⑥ T-630 闭环：已对齐无分歧不立台账条目，落 root-face 契约 contracts/console.yaml（console/root-path-homepage，三臂 200-shell/302-绝对/401-门序 VERIFIED + UI 锚臂 + 方法门 error_behavior，evidence=wave-1 分歧明细① + T-630 真实实例取证 + 本批新探针活体 + wave rerun case 文件）；⑦ 新探针 probes/console/root-face.sh（只读 GET 五臂，可复跑，实跑两轮）
Files:        修改：docs/compatibility/known-divergence.yaml（+36/-3：四条注记 + 一新条目）
              新建：docs/compatibility/contracts/console.yaml；docs/compatibility/probes/console/root-face.sh；
                    reports/agents/L042-Z1-accounting.md（本件）
              未动：matrix.yaml（195 冻结行集不增删——console/root-path-homepage 为新行为面，新行提案走 Next ③ 报 conductor）；
                    docs/reverse/（无新增规格——本批全部证据为活体第一手，规格缺口见 Next ④）；
                    known-divergence 其余 152 条目零触碰
Tests:        probes/console/root-face.sh 对 dev-cfa5da05 实例实跑两轮（首轮暴露脚本 grep 模式缺陷 HTTP/1.1 状态行被滤，修正后整跑）：
              P0 GET /binflow/api/system/version → 200 {"version":"dev-cfa5da05","revision":"cfa5da05"}
              P1 GET /（authed）→ 200 + Content-Type: text/html; charset=utf-8 + Cache-Control: no-cache + 1450B，sha256[:16]=cba474f12117c23b
              P2 GET /binflow/ui/ → 200 + 同 sha256 → BYTE-IDENTICAL（裸根 shell 与 /binflow/ui/ 逐字节同源）
              P3 GET /binflow/ 与 GET /binflow → 302 ×2 + Location: http://192.168.120.36:32083/binflow/ui/（绝对，两拼写一致）
              P4 GET /（匿名）→ 200（B 默认匿名开姿态——与台账 rest/anonymous-read-default-policy 记载一致，未另立账）
              P5 GET /（坏 Basic，轮唯一探针名 1 次，远低于熔断阈值 5）→ 401 + WWW-Authenticate: Basic realm="BinFlow Realm"（门序先于根分支）
              全程只读：零仓库写入、零熔断计数武装、凭据 env 注入零落盘零回显
Commands:     python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" 双文件（known-divergence.yaml / contracts/console.yaml）→ 双 YAML OK
              台账结构校验（yaml.safe_load + 遍历）：entries total: 157（批前 156，净 +1=新条目）、五目标条目 id 唯一定位、duplicate ids: 0
              引用完整性：evidence 引用 10 路径逐一 [ -f ] → 全 EXISTS（L042 报告/T-630/T-632/T-633/L020/L000/两 case 文件/探针/裁定素材）
              契约面核对：internal/httpapi/router.go grep 命中——:199 s.serveRootShell(w, r)（裸根直出）、:220 http.Redirect(..., http.StatusFound)（302 绝对）、:221 /ui 挂载、:264-266 serveRootShell doc（byte for byte /binflow/ui/）
Outputs:      契约 1 份（console/root-path-homepage，VERIFIED）；探针 1 件；台账条目 +1（docker/remote-ping-body-trailing-newline）；台账注记 4 条（token-shape/anonymous-policy/ssrf 采认/login-failure 进展）；matrix 行变更 0；known-divergence 分类分布：四目标条目维持原 UNKNOWN，新条目 INTENTIONAL_DIFFERENCE（resolved 存证口径，不占用四分类裁定语义）
Compatibility: 状态机变动：console/root-path-homepage DISCOVERED→VERIFIED（跳过 SPECIFIED/IMPLEMENTED——对齐已 deploy+活体三证在案，骨架落账即 VERIFIED，置信 high）；无条目翻 DIVERGENT/BLOCKED/INTENTIONAL（裁定类变动全部维持待 authority 纪律）；置信度分布：high 1 / medium 0 / low 0（本批新契约）；normalize 零变更（docker-remote 维持 raw，Z 建议 3 采纳落地）
Security:     契约 authentication 断言：401-门序臂（坏 Basic → 401 Basic challenge 先于根分支，拒绝凭证永不见 shell）；匿名臂不立断言（实例姿态面，显式转介 rest/anonymous-read-default-policy 防双账）；error_behavior 钉方法门（非 GET/HEAD 裸根 404）与裸根下其余路径 404 保位。探针纪律：坏凭据臂用轮唯一用户名单次尝试（远低于阈值 5，不武装熔断、不污染共享实例）——L042 工装纪律（Z 建议 4）在本批探针落实
Performance:  无（本面契约零 timing/p95 断言；裸根直出无重定向腿，参照形态即如此）
Risks:        ① wave 报告本体无「rerun-2026-10-28」章节（派票简报所述与磁盘文件不符——报告停于 e48a8110 原始版）；console rerun FAIL→PASS 的运行产物（run/wave1*）不在本 checkout，本批以自跑探针三臂活体 + T-630 真实实例取证独立补强，VERIFIED 不悬空于该缺失证据，但 rerun 记录本体建议 differential-qa 回填报告或 run 产物入库；② console 契约三处参照侧未单拍臂已显式登记 unobserved_reference_arms（GET / 的 Cache-Control/CT charset 逐值、坏凭据打裸根 401 形态、https 直连）——断言面未越界；③ ssrf 条目 classification 仍 UNKNOWN（authority pending），本批仅加采认 note 未越权裁 INTENTIONAL——T-631 裁定票仍待 authority 落锚；④ login-failure 条目阈值差（5 vs 观测约 2）为差分红候选——wave-2 若以「第 3 次起 403」断言会红，已在该条目 note 预告为收敛项
Blockers:     无（派票四任务全部落地；authority 缺位项——ssrf 条目翻态、token 两条件裁——均按纪律维持 pending，非本批 blocker）
Next:         ① differential-qa：wave-2 执行 login-failure-rate-limit 阈值精确计数差分（候选收敛项：第二参照窗取证后校正 recurrentFailureThreshold 一常量）；console-root-path 用例纳入例行批（B 侧断言已对齐）；wave rerun 产物/章节回填；② conductor：ssrf-private-upstream-stricter 待 authority 定谳（rulings/T-631 素材齐备，两案并陈）；console/root-path-homepage 新 matrix 行提案（195 冻结行集外，需 conductor 采认新行）；③ reverse-engineer：console 根路径面规格缺口（GET / 的 Cache-Control/CT charset 逐值参照细节，T-630 log Next④ 同源上报）；④ 差分用例清单：本批可差分契约=console/root-path-homepage（L2 层，fixtures 指针=无归一 raw 比较字节同源臂）——tools/difftest/v2/cases/console_root_path.py 已在案即其差分载体
