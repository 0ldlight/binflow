# 评审报告 T-8（视角: correctness 为主，兼顾 consistency/security）

结论: **REQUEST_CHANGES**
日期: 2026-08-17 · reviewer: code-reviewer
对象: `internal/config/`（config.go / api.go / load.go / validate.go / doc.go / config_test.go）

复跑取证环境：race 全绿、`golangci-lint` 0 issues、`gofmt` 空、`go vet` OK、测试计数 24 顶层 / 88 子用例与日志一致、运行后无 `data/` 残留 —— 实现者自测声明全部属实。默认值 13 项与 architecture §8 逐项一致。以下问题全部来自独立构造的恶意/边界输入探针（/tmp 临时模块，未污染仓库）。

---

## 必须修改（blocking）

### B1 [blocker] 多文档 YAML：第 2+ 个文档被静默丢弃，秘密扫描与 strict 校验全部失效

- 位置: `internal/config/load.go:75`（`yaml.Unmarshal(src, &root)`）、`load.go:88-92`（第二次 `dec.Decode(r)` 从头解码，只解 doc 1）
- 问题: `yaml.Unmarshal` 对多文档流返回 DocumentNode，`root.Content[0]` 只含**第一个**文档。`rejectSecrets`、mapping 检查、`KnownFields(true)` strict 解码全部只作用于 doc 1；`dec.Decode(r)` 从流头部再解一次同样只得到 doc 1，且从未再 Decode 验证流已耗尽。
- 证据（实测）:
  - `server:\n  listen: :9000\n---\nserver:\n  admin_password: hunter2\n` → **Load 成功**，秘密键 hunter2 静默通过（既没触发秘密报错，也没触发 unknown key 报错）
  - `...listen: :9000\n---\nserver:\n  listen: :9999\n` → Load 成功但 listen 仍为 `:9000`，doc 2 整体不可见
  - `...---\nadmin_password: top`（顶层秘密在 doc 2）→ err=nil
- 影响: 双重违背本包两条核心承诺——「YAML 出现秘密形状键即报错」（ADR-0009）与「未知键报错防拼写静默失效」；同时是静默丢配置的正确性缺陷（运维拼接/模板追加 `---` 即踩中）。merge-key 别名（`<<: *a`）在单文档内已被 strict 解码兜住（实测报 field not found），唯一逃逸路径就是这个多文档洞。
- 建议改法: `decodeRaw` 里在 `dec.Decode(r)` 成功后再 `if _, err := dec.Decode(&yaml.Node{}); err != io.EOF { return nil, errors.New("config file must contain exactly one YAML document") }`；或在 Node 阶段检查 `len(root.Content) != 1`（注意 Unmarshal 折叠了文档节点，用 Decoder 逐个读更直接）。补 3 个用例：doc2 含秘密、doc2 含覆盖、doc2 含未知键，均须报错。

### B2 [blocker] sqlite `file::memory:` 绕过 `:memory:` 防线，直通驱动层

- 位置: `internal/config/validate.go:104-111`（只匹配裸 `":memory:"`；`?` 检查可被无 query 的 URI 形式绕过）
- 问题: 守卫意图是「拒绝内存库：每连接各得私有库」。SQLite 标准的 URI 拼写 `file::memory:` 不带 `?`，两条规则都拦不住。
- 证据（实测）: `metadata: {driver: sqlite, dsn: "file::memory:"}` → **Load/Validate 通过**；而下游 T-10 `internal/metadata/store.go:97` 明确放行 `file:` 前缀（`if strings.HasPrefix(path, "file:") ... return path`），modernc 按 URI 语义解析为内存库——config 的防线被端到端击穿。（对照：`file::memory:?cache=shared` 因带 `?` 被拦，说明只是拼写运气。）
- 影响: 运维写 `file::memory:` 时服务可启动，数据只活在单连接里（T-10 MaxOpenConns=1），重启即全量蒸发——正是该规则要防的静默数据丢失场景；将来连接池放宽即私有库分裂。
- 建议改法: sqlite 分支改为白名单语义——允许「空」或「不含 `:` scheme 前缀的纯路径」（或显式拒绝 `file:` 前缀 / 归一化后匹配 `:memory:`，如 `strings.TrimPrefix(dsn, "file:")` 再查 `:memory:` 与 `?`）。当前 `file:foo.db` 透传（实测通过）与「plain file path」注释也自相矛盾，一并收口。补 `file::memory:`、`file:test.db`、`file::memory:?cache=shared` 三用例。

### B3 [blocker] `BINFLOW_ADMIN_PASSWORD` 大小写不敏感承诺破洞 → 静默回落到文档化缺省弱口令

- 位置: `internal/config/load.go:208-210`（`env[SecretEnvVar]` 精确大小写读取）、`load.go:324-325`（`case envSecret: return nil` 不赋值）、`config.go:96-97`（splitEnvKey 按归一化大写识别该变量）
- 问题: `applyEnv` 对所有 `BINFLOW_*` 变量名做大小写归一（包文档也声明大小写不敏感），识别出 `ADMIN_PASSWORD` 后却什么都不做；真正的赋值在 `build` 里用**精确大小写**键查一次 map。
- 证据（实测）: `binflow_admin_password=T0pSecret` / `Binflow_Admin_Password=...` → Load **成功**且 `AdminPassword == ""`，无任何报错。其余全部变量（如 `binflow_logging__level`）小写都生效，唯独最关键的秘密变量静默失效。
- 影响: 运维以为已设强口令，实际 T-10 `seedAdmin` 落到 ADR-0009 的评估缺省 `password`——「自认为加固的部署带弱口令启动」，安全后果直接。
- 建议改法: 删掉 `build` 里的精确读；在 `setEnvValue` 的 `case envSecret` 中直接 `if value != "" { c.AdminPassword = value }`。若同进程存在多个大小写变体，`applyEnv` 按排序遍历天然确定次序（后写胜出），注释说明即可。补 3 用例：小写生效、混合大小写生效、空值=未设。

### M1 [major] postgres DSN 校验错误原文回显口令

- 位置: `internal/config/validate.go:119`、`validate.go:122`（`got %q, dsn`）
- 问题: DSN 常内嵌 `user:pass@`；两条报错把原始 DSN 完整打进错误串，而 Load 的错误会被 cmd 打到 stderr/启动日志——正是实现者自己在 T-8.md 遗留④里给下游划的红线（「打印 Config 时须跳过 DSN」），本包自己先违反了。
- 证据（实测）: `postgres://admin:SuperSecret@/binflow` → 错误消息含 `got "postgres://admin:SuperSecret@/binflow"`，口令明文。
- 建议改法: 报错里去掉 DSN 原文，或脱敏后输出（剥 userinfo：`postgres://admin:***@…`，或只输出 scheme+host）。同理检查 119/122 两处。补一个「错误不含口令子串」断言用例。

---

## 建议改进（non-blocking）

### m1 [minor] `BINFLOW_DATA_DIR` 便捷拼写未进 doc.go 例外清单，也不在架构 §8

- 位置: `internal/config/config.go:100-101`；`internal/config/doc.go:11-17`（写着 "Three names are deliberate exceptions"，实际已有第 4 个）
- 去留建议（conductor 问询项）: **建议保留**——与扁平的 `BINFLOW_ADMIN_PASSWORD`、`BINFLOW_SECURITY_ANONYMOUS_ACCESS` 一脉相承，docker `-e`/compose 场景最常打；但必须: ① doc.go 例外清单补上它；② 请 architect 在 §8 补一句（或记入 T-22 回写清单）；③ 与 T-16 的 `BINFLOW_HOME` 语义划清边界（HOME 是目录解析根，DATA_DIR 是 storage.data_dir 直配，文档写明优先级）。若 architect 要砍，成本一行 + 一用例。

### m2 [minor] `ensureDataDir` 缺任务要求的 TOCTOU 说明注释

- 位置: `internal/config/load.go:415-427`
- 探测与实际使用之间存在权限/挂载状态变化的窗口（探针文件名固定可预测）；可接受，但按票面要求应有一句注释声明「probe 即 fail-fast 尽力而为，不保证后续可写」，防止后人当安全边界。顺带可说明探针并发删写无害。

### m3 [minor] 布尔 env 空值静默当 false，可无声关掉 audit

- 位置: `internal/config/load.go:406`（`case ..., "": return false`）
- `BINFLOW_AUDIT_ENABLED=`（compose 里 `VAR:` 空值常见）= 静默关审计；匿名键上空值恰好 fail-closed，审计键上 fail-open，语义不一致。godoc 有声明属有意为之，故 non-blocking；建议：空布尔值一律报错，或至少 audit 键报错。

### m4 [minor] 「校验失败不留目录」的顺序契约没有测试钉住

- 位置: `internal/config/config_test.go:477-618`
- Validate 纯值检查在前、fs 探测在后的契约（validate.go:60-63 已实现）值得一条显式断言：构造 listen 非法 + data_dir 指向新路径，断言 Validate 失败后该目录不存在。

### m5 [minor] 指针零值矩阵覆盖不全（YAML/env 侧）

- 显式 `graceful_timeout_seconds: 0`、`argon2_memory_mb: 0`（YAML）与 `BINFLOW_SERVER__GRACEFUL_TIMEOUT_SECONDS=0`、`BINFLOW_AUTH__TOKEN_DEFAULT_TTL_HOURS=0`（env）没有直接用例——目前只经 `Validate` 改 Duration 字段间接覆盖，raw 指针 → Duration → 校验的整条链在这几个键上未被钉住。bool/字符串键的显式零值（anonymous_access: false、base_url: ""）已覆盖良好。

### n1 [nit] 顶层误置的 `dsn:` 报「looks like a secret」并指向 BINFLOW_ADMIN_PASSWORD

- 位置: `internal/config/config.go:73-76`
- 正确建议应是「移到 metadata: 下」而非「改用 BINFLOW_ADMIN_PASSWORD」，现消息误导。

### n2 [nit] 仅含 `---` 的文件报 "must be a YAML mapping"，而纯注释文件走默认

- null 文档（`---` / `null`）与空文件语义应一致；修 B1 时顺带把 null 文档按空文件处理即可。

---

## 已验证无问题的关注点（取证记录）

- 全指针 raw schema 的「未给 vs 显式零」：bool 双向、`session_ttl_hours: 0`、`audit.enabled: false`、`base_url: ""` 均正确区分（显式零值生效并被校验拦截，未给走默认）。
- strict 模式嵌套未知键、顶层/嵌套重复键、tab、未闭合引号、非 mapping、merge-key（`<<:`）注入秘密——单文档内全部正确报错；秘密扫描先于 strict 解码，错误优先级正确。
- env 边界：`BINFLOW_STORAGE___DATA_DIR`（三下划线）、数字后缀 `LISTEN2`、裸 `BINFLOW_`、`BINFLOW_HOME`（含小写）行为均正确且未知变量错误点名原拼写；类型化赋值失败点名变量（`BINFLOW_LOGGING__LEVEL: ...`）。
- 双键合并 7 态语义与日志声明一致；env 对双键的覆盖在 merge 之后生效，无冲突检测遗漏（env 一致覆盖两键等价面）。
- 默认值 13 项对照 architecture §8 全量一致；ADR-0009 匿名默认 true 正确。
- 依赖 `go.yaml.in/yaml/v3 v3.0.5` 在 ADR-0005 白名单内。
- clean-room 抽查：reverse-src 为 Java/XML 配置体系（`artifactory.config.xml` / `*.properties`），与本实现无标识符或结构对应；`anonymous_access` 等概念名出自 PRD/ADR-0009，非反编译源。无逐行翻译嫌疑。

## 范围外发现（交 conductor）

- `BINFLOW_DATA_DIR` 与架构 §8 的差异需 architect 定夺（见 m1），建议挂 T-22 回写清单。
- `internal/metadata/store.go:97` 对 `file:` 前缀 DSN 的直接放行放大了 B2；即使 config 侧修复，metadata 侧对内存 URI 的态度（T-10 area）值得同步知会。
