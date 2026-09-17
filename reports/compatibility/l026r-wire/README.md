# L026-5 wire 取证（2026-09-17，compatibility-engineer）

双端：A=Artifactory pro 7.161.15（http://172.16.58.130:8082，admin）；B=BinFlow dev.b79a2d51
（http://172.16.58.130:8083，admin；带 L026-3 修复〔3472e5bb〕**未部署**——本票只对拍新建空仓默认键面，不受该回归影响）。
版本快照：`a-system-version.json` / `b-system-version.json`。探针资产（仓/用户/权限 target，前缀 l026r-*）已双侧删净。

## 任务一+二：deb 包型条件键（A×B 新建空仓默认面）

| 文件 | 内容 |
|---|---|
| `a-l026r-optdeb-v1-deb.json` | A minimal deb 仓 v1 面（62 键；`optionalIndexCompressionFormats: []` 在场） |
| `a-l026r-optgen-v1-generic.json` | A minimal generic 仓 v1 面（61 键；该键**不在场**） |
| `a-l026r-optdeb-v2-deb.json` | A deb v2 面（21 键；deb 条件键 ddebSupported/debianTrivialLayout/optionalIndexCompressionFormats） |
| `a-l026r-optgen-v2-generic.json` | A generic v2 面（18 键） |
| `b-l026r-debd-put-400-license-gate.json` | B 建 deb 仓 400（license tier community < pro——ADR-0032 门，deb 面在 dev 不可达） |
| `b-l026r-debgen-v1-generic.json` | B minimal generic 仓 v1 面（61 键，=B 共享 local 键表代理） |
| `b-l026r-debgen-v2-generic.json` | B generic v2 面（18 键） |
| `b-l026r-debd-v1-unknown-400.json` | B GET v1 未建成仓=400 Bad Request 信封（v1-read-unknown 契约 SAME 臂旁证） |
| `b-l026r-debd-v2-unknown-404.json` | B GET v2 未建成仓=404 not found（v2-read 404 臂旁证） |

设值回显探针（未存文件，命令+输出见 reports/agents/L026-5.md Tests）：A POST 更新
`optionalIndexCompressionFormats=["bz2"]` → 200，GET 回显 `["bz2"]`（键数仍 62）。

## 任务三：m-holder repo 读位（A 规格探针 + B 对照）

夹具：A 侧用户 `l026r-holder`（plain，manage-only target l026r-pt 覆盖 l026r-hold）+ `l026r-noperm`
（无任何权限）；仓 l026r-hold（覆盖内）/ l026r-outside（覆盖外）。B 侧同构（v1 扁平方言建 target）。

| 文件 | 面 | A 结果 | B 结果 |
|---|---|---|---|
| `a-holder-v1-l026r-hold.json` / `b-holder-v1-l026r-hold.json` | v1 单仓（覆盖内） | 200，4 键 {key,rclass,packageType,description} | 200，同 4 键——SAME |
| `a-holder-v1-l026r-outside.json` / `b-holder-v1-l026r-outside.json` | v1 单仓（覆盖外） | **200，同 4 键投影**（与覆盖内无差） | **403** `administrator privileges required` |
| `a-holder-v1-repositories-list.json` / `b-holder-v1-repositories-list.json` | v1 列表 | **200 全量清单**（entries 与 admin 逐字节同，n=6） | **403** `administrator privileges required` |
| `a-holder-v2-l026r-hold.json` / `b-holder-v2-l026r-hold.json` | v2 单仓（覆盖内） | 200，4 键 {key,type,packageType,description} | 200，同——SAME |
| `a-holder-v2-l026r-outside.json` / `b-holder-v2-l026r-outside.json` | v2 单仓（覆盖外） | 200，同 4 键 | 200，同——SAME |
| `a-holder-configurations.json` / `b-holder-configurations.json` | configurations 面 | 403 errors 信封 `Forbidden` | 403 同——SAME |
| `a-noperm-v1-l026r-hold.json` | v1 单仓（无任何权限用户） | 200，同 4 键（=holder 同形——读面与权限解耦） | —（未建，票面范围外） |
| `a-noperm-v1-repositories-list.json` | v1 列表（无权限用户） | 200，entries 与 admin 逐字节同 | — |
| `a-noperm-v2-l026r-hold.json` | v2 单仓（无权限用户） | 200，4 键 | — |
| `a-noperm-configurations.json` | configurations（无权限用户） | 403 Forbidden 信封 | — |
| `a-admin-v1-l026r-hold.json` / `a-admin-v2-l026r-hold.json` | admin 对照面 | 61 / 18 键全量 | — |
| `a-admin-v1-repositories-list.json` | admin 列表对照 | n=6（holder/noperm 列表与之 entries 全等同） | — |
