# T-562 stage 2 — plain-SNAPSHOT walk family 对齐 A（walk.go 落地 + t8 读面 404）

```
Ticket:       T-562 / BIN-44 — walk family alignment A（plain-SNAPSHOT walk resolve 五点：W1/W2/W3/W4 + t8），P1
Role:         dev-registry-adapter（领域实例：dev-package-maven）
Area:         internal/adapter/maven
Input:        docs/compatibility/contracts/maven-virtual.yaml 契约⑩ plain-snapshot-unique-walk-resolve（W1/W2/W3）、
              ⑪ non-snapshot-spelling-get-gate-404（t8）、⑫ virtual-plain-walk-cross-member-selection（W4 mtime key）、
              ⑨ deploy-put-version-metadata-auto-materialize（t7 版本元数据不 walk 的边界）；reports/compatibility/
              L035-r7-walk-forensics.md（W1 t1–t10 矩阵、W2 s1–s5+s2b 规则、W3 c1–c4 同面、W4 v1/v2/v3 mtime 判别）；
              docs/reverse/maven-npm-pypi.md 与 virtual-resolution.md §3.6（walk 只在 miss 上触发）；
              既有 fence：plain_snapshot_resolve_test.go（T-559 边界锚点，票面要求翻面）
Changes:      1) 新增 walk.go：isPlainSnapshotArtifact 触发门（请求文件名带 -SNAPSHOT 拼写）+ versionDirPrefix +
              walkCandidates（(module, baseRev, classifier, extension) 四元组家族过滤）+ selectWalkCandidate
              （W2：filename ts 字典序=时间序优先，ts 平手比 NUMERIC buildNumber，上传 mtime 无关）+
              servePlainWalk（仅 svc.Get miss=ErrNodeNotFound 时触发，保 T-559 已存 plain 拼写面；remote 仓不触发）+
              serveVirtualWalk（镜像 repo 层 getVirtual 的 virtualMetadataSteps+filterMetadataSteps 跳过 cache facet
              与 handleSnapshots=false 成员；成员内先跑 W2 选择，跨成员用候选 storage mtime key，秒粒度平手取
              walk 序靠后成员——L035 c4/v1/v2/v3 全判别通过）
              2) handler.go：handleGet 在类门之后、sidecar 分支之前挂 servePlainWalk；serveSidecar 改走
              serveSidecarOfPath（sha512 404 前置保留）；抽出 serveNode 作为 M1 下载契约渲染唯一出口
              （digestTriple/validators/Range），plain walk 与 rewritten 拼写同出此口（W3 同面）；新增
              writeSidecarDigest 统一 sidecar 摘要渲染
              3) t8：ServeHTTP 解析门 GET/HEAD 失败 → 404 notFoundMessage（读面不再 400 parse-refuse）；
              PUT/DELETE 保持 400（A 的 PUT 面未观测，不猜）
              4) snapshot.go：snapshotDirFacts 前缀改用共享 versionDirPrefix（消重）
              5) 翻面锚点：T-559 unique-home 边界测试与 L014-2 fence（rewrite wire / sidecar registration）按
              契约⑩新面翻正（plain GET=200 walk-resolved；-SNAPSHOT sidecar GET=200 解析目标摘要），存储不物化
              断言改用 ListByPrefix 显式化
Files:        新增 internal/adapter/maven/walk.go、internal/adapter/maven/plain_snapshot_walk_test.go；
              修改 internal/adapter/maven/handler.go、snapshot.go、maven_test.go、plain_snapshot_resolve_test.go、
              snapshot_rewrite_test.go
Tests:        plain_snapshot_walk_test.go：TestPlainSnapshotWalkTriggerMatrix（t1 GET/t2 HEAD validators/t3 jar/
              t4 Range 206 16 字节/t5+t6 sha1+md5 sidecar/t7 版本元数据直读 200/t9 仅 jar 候选 404）、
              TestPlainSnapshotWalkSelection（s1 filename-ts 压上传序、s2/s2b ts 平手 bn 数值胜、s3 bn 10>9、
              s4 扩展名家族隔离、s5 classifier 家族隔离）、TestPlainSnapshotWalkNonSnapshotGate（t8→404）、
              TestPlainSnapshotWalkLiveResolve（W3：二次 PUT 翻面 + HEAD ETag 跟随）、
              TestVirtualPlainWalkCrossMemberMtime（W4 v2 判别：new-ts 先传、old-ts 后落 → vOLD 胜；virtual
              sidecar 同胜者；t10 单成员 virtual 透传）；翻面：TestPlainSnapshotUniqueHomeBoundary、
              TestSnapshotRewriteWire、TestSnapshotSidecarRewriteAndRegistration、TestLayoutRefusals（GET 腿 404）
Commands:     gofmt -l internal/adapter/maven/（空）；go vet ./internal/adapter/maven/；go build ./...；
              GOLANGCI_LINT_CACHE=/tmp/lint-t562 golangci-lint run ./internal/adapter/maven/...（0 issues）；
              go test ./internal/adapter/maven/ -count=1；go test ./internal/httpapi/ ./internal/repo/ -count=1；
              差分（参照 /tmp/l035-run.sh，A_BASE=http://192.168.120.38:8082/artifactory，
              B_BASE=http://127.0.0.1:18080/binflow，B=本 worktree 构建 /tmp/binflow-r7-impl/binflow-server，
              数据目录 /tmp/binflow-r7-impl/data，绝不 8080/主 checkout ./data）：
              python3 tools/difftest/v2/runner.py --out tools/difftest/v2/run/l035-r7-impl-r{1,2} --case
              maven-walk-trigger-matrix --case maven-walk-selection-tiebreak --case maven-walk-sameface-mvn
              --case maven-virtual-walk-selection
Outputs:      单测 ok github.com/lzwzzy/binflow/internal/maven 27.5s；回归 ok httpapi 353.0s / ok repo 106.0s；
              差分 r1 与 r2 均 PASS=4 FAIL=0（四 case 连续两轮双端一致）；summary.json 关键对照（A==B==expected）：
              trigger-matrix t1 w1pom / t2 len=280 / t4 status=206;len=16 / t5+t6 200=sha_of_pom / t7 200 /
              t8 404 / t9 404；selection s1 s1-newts / s2 s2-bn2 / s2b s2b-bn2 / s3 s3-bn10 / s4 jar-newts+
              pom-oldts 各家族独立 / s5 pom-bn2+sources-bn1；sameface c1 byte-identical 双次 / c2 200/280/etag_eq /
              c3 成员=virtual=mvn-pom / c4 c4-m2-new；virtual v1 vNEW / v2 vOLD（mtime key 判别） / v3 vNEW
Compatibility: 契约⑩⑪⑫逐条对照全过（t1–t10、s1–s5+s2b、c1–c4、v1–v3）；t8 全 GET/HEAD 解析失败面统一 404
              与 A 路由层模型一致；remote 成员 walk 面与 remote 仓 walk 面 L035 即 NOT_RUN（未观测），本实现
              取 ListVirtualMember 拒绝=无候选的保守语义并在代码注释声明；无漂移点新增
Security:     walk 候选仅取 versionDirPrefix 前缀下节点（前缀由 Layout 解析产物拼接，含 ../ 的 relPath 在
              Parse 层已拒）；virtual 跨成员读取走既有 ReadVirtualMember（成员白名单内），无任意上游连接
Performance:  候选集=单版本目录 ListByPrefix，walk 仅在 plain 拼写 miss 时触发一次；大制品零拷贝（serveNode
              流式，digestTriple 复用节点已存摘要）；无回归面
Risks:        节点 mtime 为秒粒度（metadata.Now RFC3339）：跨成员秒级平手取 walk 序靠后成员，L035 c4 依赖
              此语义（双端一致已两轮验证）；已覆盖客户端形态 mvn/deployer 风格 raw HTTP + mvn 实客（sameface
              case c3），旧版 Maven 对 plain 拼写带 Range 的组合面未单独探测
Blockers:     无
Next:         T-566/BIN-48 auto-materialize 写路物化（本票收口后立即执行）；金样 B 腿补录候选=四 walk case
              evidence（run/l035-r7-impl-r1/r2）；green-flip 裁定归 conductor 台账批次
```

差分矩阵（L030 口径：双端连续两轮 PASS 方记 PASS）：

| 轮次 | case | A | B | 结论 |
|---|---|---|---|---|
| r1 | maven-walk-trigger-matrix | PASS | PASS | PASS |
| r1 | maven-walk-selection-tiebreak | PASS | PASS | PASS |
| r1 | maven-walk-sameface-mvn | PASS | PASS | PASS |
| r1 | maven-virtual-walk-selection | PASS | PASS | PASS |
| r2 | maven-walk-trigger-matrix | PASS | PASS | PASS |
| r2 | maven-walk-selection-tiebreak | PASS | PASS | PASS |
| r2 | maven-walk-sameface-mvn | PASS | PASS | PASS |
| r2 | maven-virtual-walk-selection | PASS | PASS | PASS |

B 实例：/tmp/binflow-r7-impl/binflow-server（listen 127.0.0.1:18080，数据目录 /tmp/binflow-r7-impl/data，
全程未触碰 8080 与主 checkout ./data；两轮差分完成后进程已停）。差分结论只报不裁；金样 B 腿补录归
compatibility-engineer。
