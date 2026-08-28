# T-329-DGH 工作日志 — m11-done 收口前置④：D-G/D-H 文档回刷 + make docs 重跑

- 角色：tech-writer ｜ 日期：2026-08-28 ｜ 状态：**done（make docs SUCCESS）**
- 票据来源：T-329 §7 D-G/D-H 登记 + §10 收口前置清单④；范围以登记原文为准
- 纪律遵守：**共享工作树零 git 操作**；只动 `docs/user/`（五文件，见清单）；未触 `docs/reverse/`（T-318 遗留#1 归 reverse-engineer）、未触锚册/产品代码；docs-site 构建产物 dirty 为 make docs 工作产物（T-328 谱系同源）

## 1. D-G 回刷清单（cargo 三处 + remote-virtual 一处 + 连带）

| # | 登记点位 | 处置 |
|---|---|---|
| G1 | cargo.md 前置「remote 与 virtual 归 M11——暂不可用（404）」 | 改为「三类仓型 M11 起齐备」+ 锚链接指向新专节；头部适用版本同步 M10+M11 增补注记 |
| G2 | cargo.md 边界表「当前建仓不可用（声明式 local-only）」 | 该行删除，换为「crates.io 直连上游不支持」（T-316 差异 7 的用户面表述）；表题改 M10/M11 |
| G3 | cargo.md 报错对照「BinFlow 不用 200+errors 双轨」（与 CG-2 as-built 相反） | 整表 publish 族重写为 CG-2 双轨 as-built：200+`warnings.other` 臂（IOException 族，无顶层 errors 键）/ 500+信封（帧形状违例）/ 403 `permission denied`（覆盖臂不可删）；旧「publish 409」「publish 400（解帧/元数据）」两行随之翻转（409 冲突臂已删除——T-316 D-3；元数据失败入 200 臂） |
| G4 | remote-virtual.md L56「cargo remote/virtual 暂未交付」 | 改为已交付（T-316/T-318，pro 档，指向 cargo.md）；头部 M11 增补段同步追加 cargo |
| G5（连带） | cargo.md publish 校验链行「重复发布 → 409」 | 翻转为 as-built：无 409 臂、删除权限闸门裁决（可删→覆盖上传/不可删→403）+ cargo 1.98 客户端预检索引本地拒绝提示（T-316 差异 6 / T-329 观察②） |
| G6（连带） | cargo.md 手写索引/侧车行「403」 | 翻转为 D-5 as-built：接受（PUT 201/DELETE 204）+ 索引收敛；`index/config.json` 写仍 403 |
| G7（新节） | cargo remote/virtual 用法（票据范围明示项） | 新增「## remote 仓（pull-through 代理缓存）」「## virtual 仓（聚合入口）」两专节：建仓命令/config.json 自指探针/行为表（缓存续供铁证、写拒绝 405、索引归并去重键、yank first-hit、写路由、混合构建）——命令与输出取 T-316 §3 / T-318 §3 / T-329 L36-L37 实测 |
| G8（新节） | CG-2 双轨失败形态「删后重发」提示（票据范围明示项） | 报错对照新增专行：两步删除法（先 `DELETE crates/<n>/<n>-<v>.crate` 再 `DELETE index/<四档>/<n>`，**顺序勿倒**——有存储事实时索引文件删除会被重算回填；经虚仓 DELETE 恒 405 须对成员仓操作）。两步顺序经代码核实（handler.go serveDerivedDelete/convergeIndex：blob 删除不即时收敛、索引文件删除触发重算） |
| G9（QA 观察①） | remote-virtual.md 负缓存 0 值语义 | `missedRetrievalCachePeriodSecs` 行补一句：显式 0 = 回落默认 1800 而非禁用（代码核实 internal/repo/config.go:313「explicit zero: keep the product default」） |

## 2. D-H 回刷清单（auth-config + api-reference）

| # | 登记点位 | 处置 |
|---|---|---|
| H1 | auth-config.md 已知边界「SAML 证书动作未落」 | 行删除（已交付项不入边界表）；SAML 运行时登录臂行补「证书管理面已随 T-331/T-307R 交付」指引 |
| H2 | auth-config.md 无三端点 | REST 面表「九端点」→「十二端点」+3 行（GET key/public / PUT regenerate / POST saml/key，含能力门与 404 空态文案）；新增「## SAML SP 加密证书」专节（五步实测往返 + 行为要点 + T-307R 证书卡）；审计表 +`auth.config.samlkey.{generate,regenerate}` 行；控制台 SAML Tab 要点补证书卡指引 |
| H3 | api-reference.md 无三端点 | M11 增补速览认证配置域表 +3 行；节题补「+ SAML SP 证书三端点」；头部 M11 增补清单与速览导语同步（含 cargo remote/virtual 登记——建仓走通用 PUT /repositories，协议面指 cargo.md） |
| H4（复核） | T-332 MPU 面（随票已翻） | 逐项对照 ADR-0039/T-332 §1：create QueryParam 200 `{"token"}` / config 探测 200 `{"supported"}` / urlPart `?partNumber=` 200 `{"url"}` 带 `?token=` / status 词表 PARTS/PROCESSING/FINISHED/NON_RETRYABLE_ERROR / part PUT 200 / complete `?sha1=` 202 / abort 204 / 旧形状退役 404——**全部一致，零修改** |
| H5（复核） | cleanup / keypair 是否齐 | cleanup 域（POST/GET `/api/v1/system/cleanup` + 引擎三腿 + 报告字段）与 keypair 域（9 端点含 BinFlow 原生 generate + v2 仓关联）均在档——**齐备，零修改** |
| H6（复核） | cargo remote/virtual 在 api-reference 是否齐 | 原文无「不可用」断言（M10 速览仅列挂载面，无类限制）→ 无过时点；为对齐 M11 交付态在速览导语补一行登记（见 H3） |

## 3. 附带

- docs/user/README.md 导航行 Cargo 条目：补 remote/virtual + CG-2 双轨关键词、版本标注 M10→M10+M11（与 conan/helm/rpm/debian 邻行同格式）。
- 全 docs/user 扫描 `暂未交付/暂不可用/local-only/未落`：命中即登记四处（G1/G2/G4/H1），另两处（nuget 符号服务器 M11+ 规划、console.md token 管理面占位）为准确表述，不动。

## 4. 验证

| 项 | 结果 |
|---|---|
| `make docs` | **SUCCESS**：`[SUCCESS] Generated static files in "build"`；docs site size (raw total): **4.00 MB (4189229 bytes)**（T-329 轮 3.94MB → +0.06MB 新内容）；断链告警 0（仅既存 docsDir/blogDir 提示） |
| 新内容进站抽查 | 删后重发/行级归并去重 → integrations/cargo/index.html；saml/config/key/public → api-reference + admin/auth-config 页 ✓ |
| 内链锚点抽查（构建产物 id=） | `remote-仓pull-through-代理缓存` / `saml-sp-加密证书` / `ssrf-防护与-allowprivateupstream-放行指引` / `缓存管理与强刷手法` / `常见报错对照` 全部存在 ✓ |
| 命令证据来源 | cargo 命令/输出：T-316 §3、T-318 §3、T-329 §2.2 L36/L37（cargo 1.98.0 真机）；SAML 三端点：T-331 自测表（curl+openssl+sqlite 三方断言）；删后重发两步顺序与负缓存 0 值语义：本轮代码核实（internal/adapter/cargo/handler.go serveDerivedDelete/convergeIndex；internal/repo/config.go:313）；SAML 404 信封形态：internal/httpapi/envelope.go writeError |
| 路由代码核对 | internal/httpapi/router.go:559-565 三路由（GET key/public / PUT regenerate / POST saml/key）与文档一致；internal/adapter/cargo gate RepoTypes = [local remote virtual] 一致 |

## 5. 遗留 / 待确认

1. **`docs/reverse/cargo.md` §8 virtual 行仍写「T-318 承载，未落地」**——T-318 遗留#1 登记移交 reverse-engineer（tech-writer 纪律只动 docs/user/），随本票提请 conductor 转派；T-318 §6-1 已给出逐条回写素材（首见去重/写路由/yank 双持有者/差异 2、3、4）。
2. remote-virtual.md「M3 有意不兼容清单」表内 `packageType` 行为 M3 历史快照（含已交付的 conan/go 旧归属）——按「里程碑级全表（PRD §2.2）」历史定位保留未动；如 conductor 认为该表应为活表，另开小票统一收口（不属 D-G/D-H 登记范围）。
3. cargo.md config.json 探针输出行为按 local 面已档形态 + T-316「自指」实测结论合成（键形一致、仓 key 换 cargo-remote）——形态正确性由 T-331 同款 scratch 实测佐证，如需逐字节实证可随下轮 QA 顺带 curl 一次（非阻塞）。
