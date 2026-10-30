Ticket:       T-L043-C-b 远端 manifest 冷取 Accept 逐字转发导致缓存表示污染（L043-wave3 活体 D2/D3 真分歧）+ P0
Role:         dev-go-core（area 实例：internal/adapter/docker，conductor 票面明确授权该包）
Area:         internal/adapter/docker
Input:        conductor 票面（根因行号清单 remote.go:1028/1168/1179/1205、virtual.go:230、命中路径 remote.go:993-995→1255）；
              docs/reverse/docker-registry.md §4（协商算法与 schema2→schema1 转换语义——A 参照会按 Accept 下转换，BinFlow 不转换）；
              docs/compatibility/contracts/docker-remote.yaml（schema1 拉取属「未收面」不立断言，本票以 L043-wave3 活体证据为权威）
Changes:      1) remote.go 新增包级 canonicalRemoteManifestAccept（schema2 / docker list / OCI manifest / OCI index 四类型；
                 不含 schema1/prettyjws——污染向量）；
              2) fetchManifest 删除 accept []string 参数，Accept 头固定取规范集，godoc 重写（移除 "forwarded verbatim /
                 BinFlow never converts" 旧自白，改为 representation-single 缓存立场说明）；
              3) remote.go serveRemoteManifest 与 virtual.go serveVirtualRemoteManifest 两个调用点不再传 r.Header.Values("Accept")；
              4) 客户端 Accept 仍守服务面（landing 的 acceptAllows 门与 serve 面），只是不再决定上游存什么——架构不变。
Files:        新增：internal/adapter/docker/remote_manifest_accept_test.go
              修改：internal/adapter/docker/remote.go、internal/adapter/docker/virtual.go、
                    internal/adapter/docker/remote_session_test.go（4 处调用点签名同步）
Tests:        remote_manifest_accept_test.go TestRemoteManifestFetchCanonicalAccept——上游会话捕获断言（每个上游 manifest
              会话的 Accept 恒为规范集、schema1 从不上游）；prettyjws-only 客户端冷取 404（landing 的 client-Accept 门拒
              规范类型，已登记残余分歧，缓存不落盘）；schema2 客户端 MISS→HIT 两相：Content-Type 恒 schema2、body 与上游
              规范字节逐位一致、HIT 不再开上游会话（hits 冻结）；direct 面与 virtual 成员遍历双腿覆盖；
              remote_session_test.go 三例仅签名同步，Bearer dance/plain/refused 行为不变。
Commands:     go test ./internal/adapter/docker/ -run TestRemoteManifestFetchCanonicalAccept -count=1（修复前先行运行）
              go test ./internal/adapter/docker/ -run TestRemoteManifestFetchCanonicalAccept -count=1（修复后）
              go build ./... && go vet ./internal/adapter/docker/ ./internal/repo/ && gofmt -l internal/adapter/docker/ internal/repo/
              golangci-lint run ./internal/adapter/docker/...
              go test ./internal/adapter/docker/ ./internal/repo/ -race -count=1
Outputs:      修复前：remote_manifest_accept_test.go:129 FAIL "prettyjws-only cold fetch = (200, {"schemaVersion":1,…prettyjws
              body…}), want 404"——旧代码在未修复树上精确复现活体 D2 污染（prettyjws 表示落缓存并 200 返回），测试敏感性已证。
              修复后：TestRemoteManifestFetchCanonicalAccept PASS。
              go build/vet 通过；gofmt -l 空输出（exit=0）；golangci-lint 0 issues（首跑 revive redefines-builtin-id 2 条，
              改 sawToken 后清零）。
              -race（分包运行）：./internal/adapter/docker/ → ok 67.484s；./internal/repo/ → ok 570.855s（-timeout 25m）。
              注：两包合并跑默认 10m 预算曾在 internal/repo 600.5s 超时 FAIL（race 放大+前台并行争用），延长超时分包复跑
              全绿，非数据竞争非回归（repo 包不 import adapter/docker，静态确认零依赖交集）。
Compatibility: 缓存内容从「协商表示」改为「规范表示」——与 A 参照对齐（A 缓存永持规范 schema2，服务时才按客户端转换；
              BinFlow 无转换故直接服务规范表示）。契约 docker-remote.yaml 对 schema1 协商本就「未收面」无断言，无既有契约
              断言被推翻。漂移点（建议登记 known-divergence）见 Risks。
Security:     无新增攻击面；上游 Accept 集固定为常量，客户端输入（Accept 头）不再进入上游会话头，缩小请求注入面；
              无凭证处理变化，无日志新增字段。
Performance:  无测量；每次冷取上游请求头从客户端集合变为 4 个固定值，无额外往返；HIT 路径零变化。
Risks:        残余分歧（票面「登记不修」，建议录 docs/compatibility/known-divergence.yaml）：
              (a) schema1-only 老客户端冷取由 200（缓存 prettyjws 可命中）变 404——上游只回规范类型、客户端 Accept 门拒绝，
                  A 参照为 200（服务端下转换 schema1）；B 口径与本地面一致（M2 不做 schema2→1 转换，docker-registry.md §4 校准）。
              (b) schema1-only 客户端后续 schema2 请求：旧缓存 miss→上游重取→200 schema2，为正确行为，无需登记。
              (c) direct remote 面 HIT 路径（remote.go serveRemoteManifestCopy）无 acceptAllows 检查（virtual 面 HIT 有）——
                  存量不对称，本票未触碰；规范集修复后 HIT 存的必是规范类型，风险降级为「老缓存残留 prettyjws 节点仍会被
                  verbatim 服务」，如需彻底清一律走缓存失效/重建。
              规格置信度：规范集四类型选型为中置信度推演（票面授权「或与 A 一致的规范集」；四类型=本平面 verbatim 存储能力，
              避免只发 schema2 令 OCI-only 上游镜像断供）；无活体 A 上游请求头抓包佐证，建议 differential 面后续补一条
              上游 Accept 观测探针。
Blockers:     无
Next:         1) compatibility-engineer 将残余分歧 (a) 登记 known-divergence.yaml（建议措辞：docker remote 冷取对 schema1-only
                 Accept 回 404，A 回 200 转换 schema1——BinFlow 无下转换）；
              2) 建议一条上游 Accept 观测差分探针补中置信度选型的活体证据；
              3) 若产品要求 prettyjws-only 客户端可用，需另立「服务时 schema2→schema1 转换」票（涉及 serve 面新能力，
                 非本票范围）。
