# T-L011-1 — code-reviewer (reviewer-b)：元数据面计数污染修复 · 架构/兼容面评审

Ticket:        T-L011-1 · 元数据面下载计数污染修复（rest/metadata-plane-download-count-pollution，BUG）· LOOP 011 轨道 L011-1 · P1
Role:          code-reviewer (reviewer-b)
Area:          internal/httpapi（storage.go / search_ui.go / permissions.go + 新增测试）
Input:         conductor 派发（同 A 范围：三 Go 文件 + metadata_face_download_count_test.go）；证据链 T-L011-1.md、known-divergence.yaml#rest/metadata-plane-download-count-pollution（含工作区并发出现的 resolved 块与新 UNKNOWN 条目）、statsNode 先例（T-438）、docs/reverse/rest-api.md §3 语义、ADR-0044 K69 注释块
Changes:       评审了 storage.go storageNode 重写（Get→List 三臂解算）+ statsNode 注释与化简、search_ui.go leadFile 解算切换、permissions.go 注释出处改写、新测试三件；上下游追读深度：repo.Service.Get/List/listRowsChecked/listVirtual/getVirtualFolder 全文、properties 三 handler、writeFolderInfo/childInfos、metadata.nodePropStore.Merge/List（FK 语义）、markDownload 全部调用点、httpapi 内 ReposSvc.Get 残留扫描（零残留）
Files:         internal/httpapi/storage.go（storageNode List-based 三臂：精确/斜杠回退/children 证明 display 臂——通过；statsNode 化简行为中性——通过）；internal/httpapi/search_ui.go（leadFile 改走 storageNode——通过，同族污染收口）；internal/httpapi/permissions.go（注释级，decompiled 名→live-wire 差异出处——通过，clean-room 卫生改善）；internal/httpapi/metadata_face_download_count_test.go（行为命名合规、票号在头注释——通过）
Tests:         go build ./... rc=0；go vet ./... rc=0；go test ./internal/httpapi/ -run 'TestMetadataFace|TestVirtualMetadataReads' -v 全 PASS（3 件 20 子测）；go test ./internal/httpapi/ -run 'TestMetadataFaceResolutionArms|TestStorage|TestRemoteDegraded' -race 通过（44s）；go test ./internal/httpapi/ ./internal/storage/... -count=1 双 ok（218s / 79s——复现声称的「全包+storage 绿」，墙钟与声称 148s/76s 有差但通过态一致）
Commands:      go build ./... ；go vet ./... ；go test ./internal/httpapi/ -run 'TestMetadataFace|TestVirtualMetadataReads' -count=1 -v ；go test ./internal/httpapi/ -run 'TestMetadataFaceResolutionArms|TestStorage|TestRemoteDegraded' -race -count=1 ；go test ./internal/httpapi/ ./internal/storage/... -count=1 ；git diff 逐文件；grep storageNode/statsNode/markDownload/ReposSvc.Get 全调用面
Outputs:       本报告 reports/agents/T-L011-1-review-b.md
Compatibility: 台账 resolved 块与本文书一致性核验通过：九面 0→0→0 语义 = statsTimestamps 键 iff ≥1 真下载（storage.go:977 注释口径），markDownload 调用面经代码核验已无元数据面可达路径；脏实例历史行不回写处置已入台账 note（「历史污染行不回写=事实保留」——正确性方向正确：修复只切断新增污染源，不伪造历史）；旁观发现（virtual 面 ?properties/?stats 404 vs 200）已由 compatibility-engineer 立 UNKNOWN 条目 rest/virtual-metadata-faces-404-vs-200 并显式否证本票所引「T-438/FR-89.2 既有裁量」对本面的覆盖——登记路径完整，dev 侧引用失准为文书精度问题（见 non-blocking）
Security:      无新攻击面：解算收窄（remote 不回源=移除一个 SSRF-ish 上游触发面）；List 与 Get 同一 allow() 门（listRowsChecked 与 Get 同谓词同 401/403 区分）；无注入/穿越新增（display 臂 prefix 匹配在已验证路径串上）
Performance:   文件路径解算 = 前缀列举 1 行（LIKE prefix%）+内存匹配，净减旧 Get 的 blob Open+计数 UPDATE+audit 写；文件夹 item-info 存在双重子树列举（storageNode List + writeFolderInfo listWithNote 同前缀各一次）——见 non-blocking ①
Risks:         评审后仍存疑：virtual-over-remote 未缓存成员的元数据面（200+拉取→404）无本臂独立参照探针（只有 direct-remote 臂「参照同臂 404」证据）——已由 LOOP 012 UNKNOWN 门覆盖，建议探针补此臂（non-blocking ②）
Blockers:      无
Next:          ① LOOP 012 virtual 元数据面裁定时，探针清单建议补「virtual item-info/properties 于未缓存 remote 成员」臂（当前仅 direct-remote 臂有参照证据）② 台账已并发翻绿（resolved 块在工作区），conductor 收口时与产品码同批提交 ③ E1-E4 素材已由 compatibility-engineer 入 LOOP 012 池项，升级票可直接派

## 评审报告 T-L011-1（形态: reviewer-b）
结论: APPROVE

### 架构判定（派发审查面逐项）

1. **非计数解算收敛性**：通过。httpapi 内 `ReposSvc.Get`（内容面计数通道）残留为零（grep 全量核验），元数据面全部收敛到三个 List 通道消费点——storageNode（item-info/properties×3/permissions/?list/leadFile 七处调用）、statsNode（?stats）、listWithNote/serveRootFolder（listing 族）。无散落。
2. **statsNode 同构性**：通过。storageNode 与 statsNode 现为同构（List 前缀列举+内存精确匹配+同门），差异仅在 storageNode 多两臂（斜杠回退、display 标记）且各有注释钉死理由（「render 面」vs「无统计源诚实 404」）。statsNode 本次改动为行为中性化简（旧 want 计算恒等于 relPath，代数核验）+注释。
3. **List-based vs 新增非计数 Get 接口**：取舍成立。不扩 repo.Service（既有手写 adapter fake 的维护痛点有 remoteBrowseViewer 注释在案）；与 statsNode 先例一致；代价=文件夹解算整子树列举，其中文件夹 item-info 双重列举（见 non-blocking ①）。md.Nodes().Get 点查 + 新 Service 方案本可省一次列举，但接口面代价不成比例——接受论证。
4. **display 臂消费面核验**：合成 Node 无 sentinel（emptyFolderSHA）/无时间戳。逐消费点核验：item-info→writeFolderInfo（isFolderPath 分支，不碰 Sha256）；list→同；properties Merge 无 node 行 FK（node_props 独立表，实证 substores_nodeprops.go:44）；webhook 信封 Sha256/Size 空值为 cosmetic。旧 T-406 读时物化（putFolderRow）与 getVirtualFolder 合成被 display 臂替代——「元数据读不落行」不变式自此处处成立（ADR-0013 姿态扩展到 rest 面）。
5. **门等价**：listRowsChecked 与 Get 同一 allow()（service.go:218→az.Can），同 401/403 区分；拼写差（"d1/" as-sent vs "d1" trimmed）与 statsNode/listing 族既有姿态一致（T-438 已差分绿）。
6. **remote 未缓存 200→404**：与参照同臂对齐（台账 resolved 块载明）；影响面=控制台 remote 树元数据（未缓存路径 item-info 不再触拉取渲染）——UI 消费方评估已挂 LOOP 012 UNKNOWN 门。
7. **E1-E4 递归臂素材**：完备（参照四臂取证 + BF 对照 + 根因定位 recordPropsAudit addressed-only + 两条实现方向），已入台账 LOOP 012 池项，可直接派升级票。

### 必须修改（blocking）
- 无

### 建议改进（non-blocking）
- storage.go:426-459 文件夹 item-info 双重子树列举（storageNode List 一次 + writeFolderInfo listWithNote 同前缀一次）——可后续让 storageNode 把 rows 传给 writeFolderInfo 复用；当前量级（与 listing 族同级）不成票
- T-L011-1.md Compatibility 段「T-438 K69 决策与 FR-89.2 既有裁量」对本面不构成 authority（台账 UNKNOWN 条目已否证）——文书精度：后续新观察登记时先查 authority 覆盖面再引
- virtual-over-remote 未缓存成员元数据臂无独立参照探针（只有 direct-remote 臂证据）——LOOP 012 探针清单补该臂
- display 臂合成 Node 不带 emptyFolderSHA sentinel——当前无消费方分支于它；若未来消费方需要区分 marker 行/display 行，补 sentinel 一行（升级路径留给下一位触者）
