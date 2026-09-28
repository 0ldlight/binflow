# PR-156 · T-530 Reviewer B（architecture 形态）限定复核 b2 轮

```
Ticket:        T-530 [P0] virtual 四桶解析序 + <K>-cache 直访缝（F1）+ virtual DELETE 404 语义修复（D-2）——前轮 blocking 1 消解复核（含 T-534 收口 + conductor replication 翻新 + helmoci 越票面收编）
Role:          code-reviewer (reviewer-b)
Area:          internal/adapter（七包+helmoci）+ internal/replication（复核面）；消费面属 dev-registry-adapter / dev-replication 域，本复核只读
Input:         conductor 派发（限定复核令）；前轮报告 PR-156-review-T530-b.md blocking 节；T-534 工作日志（reports/agents/T-534.md，含翻新前逐钉子取证表）；T-530.md 爆炸半径附录；docs/design/virtual-four-bucket.md §2.2/§2.4-D1/§3；docs/reverse/virtual-resolution.md §1/§7.5；DECISIONS.md ADR-0051；maven 先例 internal/adapter/maven/virtual_render_test.go:92/:120
Changes:       git diff 全读：npm/pypi/helm/rpm/cargo 五包测试翻新、cargo/virtual.go（纯注释）、deb+conan virtual.go refuseVirtualDelete 删除与 serveVirtualDelete 新增、deb/conan virtual_test.go 翻新、deb/conan virtual_delete_test.go 新增×2、helmoci virtual_test.go 重排腿、replication 两测试翻新；上下游追读：conan v2.go local 四删除臂（target 路径构造/成功态对照）、deb handler.go serveDeleteTracked（204 对照）、两包 writeError 404 映射、debRouteTarget/conanDeploymentTarget 残留调用点、T-530.md 附录
Files:         全部改动文件逐条结论见下节「逐条消解判定」——11 文件翻新合规、2 新增测试合规、refuseVirtualDelete 全仓清零、route-target 探针在用调用点未误伤
Tests:         独立复跑全绿：build/vet/lint 三门 + 八包+replication 全量 -count=1（8/8 ok）+ 具名测试 verbose PASS×6（deb/conan TestVirtualDeleteOwnStorage、helmoci TestVirtualFirstSeenShadowing、npm TestVirtualRenderSeams、replication 两翻新测试）
Commands:      go build ./... → BUILD_OK；go vet ./... → rc=0 无输出；golangci-lint run ./internal/adapter/... ./internal/replication/... → 0 issues.；
               go test ./internal/adapter/{npm,pypi,cargo,helm,helmoci,deb,conan}/... ./internal/replication/... -count=1 → ok×8（npm 145.8s / pypi 99.5s / cargo 106.7s / helm 99.9s / helmoci 79.5s / deb 136.8s / conan 131.4s / replication 134.2s）；
               go test ./internal/adapter/{deb,conan}/ -run TestVirtualDeleteOwnStorage -count=1 -v → PASS×2；go test ./internal/adapter/{helmoci,npm}/ -run 'TestVirtualFirstSeenShadowing|TestVirtualRenderSeams' -count=1 -v → PASS×2；go test ./internal/replication/ -run 'TestTwoInstancePushReplication|TestT317ReplicaIsolation' -count=1 -v → PASS×2；
               grep -rn refuseVirtualDelete internal/ → 零命中；grep -rn 'debRouteTarget|conanDeploymentTarget'（非测试）→ 残留=deb/virtual.go:800（recomputeTarget）+ conan/revision.go:196，均在用
Outputs:       reports/agents/PR-156-review-T530-b2.md（本文件）
Compatibility: 前轮 blocking 1 三子项全部消解且均「按实际新语义翻新」非放宽：(a) 五包翻新锚 §7.5（404+协议 ITEM_NOT_FOUND 措辞+退休 405 措辞反向断言+成员存活）与设计 §3（repeat=cache-facet local 语义：字节精确+Resolved-From=成员+X-BinFlow-Cache 头缺席+上游冻结），与 maven T-531 先例 :92/:120 同式；(b) deb/conan wire 面与 generic/maven 家族统一（DELETE 落 svc.Delete→deleteVirtualOwnStorage，404；conan target 构造与 local 四臂逐一对齐，成功态 conan 200/deb 204 与各自 local 面一致）；(c) T-530.md 爆炸半径附录补记两圈消费面。replication 两处同族翻新与 D-2 口径一致（404+"Could not locate artifact. Path:" 措辞+成员存活，engine 直读成员、t317 clause 4 经 virtual 面重 GET 已删路径=更强存活证明）
Security:      复核面无新增攻击面：serveVirtualDelete 透传 svc.Delete（principal 显式传递 conan 由参数 p、deb 经 adapter.PrincipalFrom(ctx)），service 层 gate 403 与 ACL 判定不变（T-530 已审）；无注入/穿越新面（target 路径由既有 route 解析构造，无用户原文拼接新路径）
Performance:   deb/conan DELETE 从 adapter 层同步自答改为一次 svc.Delete 调用，语义路径与 local 删除同阶；测试翻新零运行时影响；无新分配/锁面
Risks:         ① cargo repeat 腿无上游计数器，上游零增量为间接取证（字节精确+成员具名+头缺席）——T-534 ⑫ 已如实登记，差分腿覆盖；② 各协议 404 措辞为 BinFlow 渲染按实际值钉住，与参照 7.161.26 逐字节差分未做（§9 已列 §7.5 为高风险差分项）——维持前轮 non-blocking 1 同族处置（差分腿后落账）
Blockers:      无（全部取证可跑且已跑）
Next:          ① 建议 conductor 在收编说明中确认 helmoci 越票面收编裁定已记录（任务令称已复核接受，本复核独立判定=与设计 §2.2/§2.4-D1/ADR-0051 一致，见逐条判定 5）；② 差分腿覆盖面建议纳入：§7.5 各协议 DELETE 404 措辞族 + repeat 拉取头族缺席面 + cargo 上游零增量直证（与 T-534 ⑭ 一致）；③ 范围外发现：conan v1 packages/delete（POST）在 virtual 面落 serveVirtual default→plain 404 "not found"（非 envelope）——T-534 前后行为不变，非本轮回归，交 conductor 酌情登记（与前轮 non-blocking 5 的 F1 wire 覆盖缺 maven/npm 父同属后续小票族）
```

## 评审报告 T-530 复核（形态: reviewer-b，b2 轮）
结论: APPROVE

### 前轮 blocking 1 逐条消解判定

1. **子项 (a) 五包存量钉子红树 → 消解**。T-534 翻新前逐钉子取证表（日志 ⑥）证明翻新前实际响应全部吻合四桶/D-2 新语义（无真回归），翻新手法=断言按实际正确值收紧而非放宽：
   - npm virtual_render_test.go：repeat 腿=200+无 X-BinFlow-Cache+Resolved-From=npmv-rem+字节精确 REMOTE-TARBALL+hits=3 冻结；unpublish 腿=404+npm envelope"not found"+无 Allow+「No local repository was configured」退休措辞反向断言；
   - pypi 同式（Resolved-From=pyv-rem+hits=1）；helm/rpm 各 404+协议措辞（`'<repo>/<path>' not found`）+成员存活；cargo 两测全翻（repeat 字节精确+无头；两处 DELETE 404 envelope+cargo-a 索引行存活+cargo-b 仍无）；
   - 与设计 §3（cache 步=probeLocalMember 复用、local 语义、fetch 判定头只随 FR-20 本体路径）逐项吻合，与 maven 先例 :92/:120 同式同锚。
2. **子项 (b) deb/conan adapter 层 405 自答 → 消解**。两包 refuseVirtualDelete 删除（全仓 grep 零残留），DELETE 统一落 svc.Delete→service 虚拟分支（§7.5 自有存储删）：
   - conan：v2 四 DELETE kind 的 target 构造与 local 臂（v2.go serveRecipeDelete/serveRevisionDelete/servePackagesDelete/servePkgRevDelete）逐一镜像（coordinateRoot / +rRev / +dirPackage / pkgFilePrefix），404 经 writeError 落 conan envelope"Not Found"（L018 wire），成功态 200=local 臂一致，index 行维护正确缺席（virtual 无自有 index）；
   - deb：三处 DELETE 调用点全部改 serveVirtualDelete（principal=adapter.PrincipalFrom，与 serveDeleteTracked 同式），404 落 `'<repo>/<path>' not found`，成功态 204=local serveDeleteTracked 一致；
   - 未误伤：debRouteTarget 保留且在用（recomputeTarget，virtual.go:800）；conanDeploymentTarget 保留且在用（revision.go:196 注册尾）；lint 0 issues 佐证无孤儿符号；
   - 新增 TestVirtualDeleteOwnStorage×2（含 404+Allow 缺席+退休措辞双反向断言+成员直接 GET 存活+virtual 解析仍服务=双层存活证明），式样照 maven 先例。
3. **子项 (c) T-530 日志门 → 消解**。T-530.md 新增「爆炸半径附录」：两圈消费面（adapter 五包红+deb/conan 分叉、replication 两处）+ 裁定记录 + T-534 派发 + 教训（DELETE×virtual fixture grep 面）。
4. **replication 翻新（conductor）→ 合规**。TestTwoInstancePushReplication:287 / TestT317ReplicaIsolation:199 两处按 D-2 翻新：404+逐字 "Could not locate artifact. Path: 'replica/…'."（generic 家族 ITEM_NOT_FOUND，与 repo/virtual_delete_test.go 同款）+成员存活反断言（engine 直读 replica-local 200；t317 以 clause 4 经 virtual 面重 GET 已删路径 200——比直读更强的存活证明）；PUT 405 腿语义未动（写路由语义未变），翻新面精确限定在 DELETE 腿。
5. **helmoci 越票面收编（任务令 5）→ 独立判定：与设计一致，接受**。TestVirtualFirstSeenShadowing 重排腿：virt-local/virt-remote 重排（remote 声明在前）后赢家仍为 local 成员（body=manifestLocal+Resolved-From=virt-local）。设计 §2.2「全部 local 类……locals 恒排在 remotes 之前，无论声明序如何穿插」+§2.4 D1「local 类恒先于 remote 本体」+ADR-0051（DECISIONS.md:1435 确认存在，勘误节同口径）——重排只移动桶内赢家、不跨桶，翻新断言即设计的可观测直译；注释锚（spec §1+design §2.2+ADR-0051）如实。

### 建议改进（non-blocking，本轮新增）

- 无新增必须项。两条风险提示（cargo 上游零增量间接取证、协议 404 措辞差分未做）已在 T-534 ⑫/⑭ 如实登记且处置路径合理（差分腿），维持登记即可。

### 范围外发现（交 conductor）

- conan v1 `POST /v1/conans/<ref>/packages/delete` 在 virtual 面落 serveVirtual default 分支 → plain 404 "not found"（非 conan envelope）——T-534 前后行为不变（旧 405 自答只覆盖 v2 DELETE kinds），非本轮回归；方向上恰与 §7.5 的 404 家族一致，仅措辞载体欠协议化。建议随后续 conan 小票或差分腿登记。
