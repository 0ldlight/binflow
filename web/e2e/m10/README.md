# M10 守护与验收基座（web/e2e/m10/）

T-277 交付（B0 首票）。M10 全部实现票（license 门控 / addon 注册表 / Go·NuGet 试点 /
属性系统 / 快赢包）与 QA L 序列的 Playwright spec 落这里。
**断言口径沿 ADR-0029 决策 3（交互断言制，见 `../m8/README.md` 全文）+ M10 增量口径
（本文 §2）**；规格锚 = PRD milestone-10 §5.5 L01~L30 + ADR-0032/0033 + architecture §15。

> wire 路径以 ADR 为准：license 三端点 = `/binflow/api/system/license`（§15.1.4），
> addon 清单 = `/binflow/api/v1/addons`（§15.2.5）。PRD v1.0 §4.1 的
> `/api/v1/system/license` 拼写在 PRD↔ADR 分歧裁定的败方——已路由 T-293 as-built 收口，
> spec 一律走 ADR 拼写。

---

## 1. 运行口径（真栈 + 新鲜二进制纪律 + 多档位编排）

```sh
# 仓库根：console 与二进制（probe 同款新鲜度守卫）
make console && make build

# 起被测实例（无 license = community 形态，Q2 暂行口径）
./bin/binflow-server serve &        # 默认 127.0.0.1:8080；ADMIN_PW 缺省 password

# 种子（FR-89-AC4 存量 ';' 回归腿载体；幂等，可重复跑）
cd web && node scripts/seed-m10.mjs

# 跑 M10 面（选择项目段；全量 = npx playwright test）
npx playwright test --project=m10

# 多档位姿态矩阵（五形态编排：community/pro/enterprise/expired/disabled）
make test-m10-matrix EXPECT=1       # 契约基线闸门（偏离须白名单登记）

# 不变量闸门（无 license ≡ m9-done 的可执行证明，§15.6-1）
make test-m10-invariant
```

- `BASE`（默认 `http://127.0.0.1:8080`）指向被测实例；`ADMIN_USER`/`ADMIN_PW` 沿既有
  探针约定。
- **二进制新鲜度纪律**（M7 T-216 陷阱的教训）：跑 e2e 前确认
  `find cmd internal go.mod go.sum tools.go -type f -newer bin/binflow-server`
  为空；非空即 `make build`。
- spec 不 autostart 服务器（沿 T-89 起的 harness 约定）。
- **多档位实例**不在 playwright 里起：五形态（community 无 license / pro / enterprise /
  过期降级 / `addons.disabled` 熔断）由 `scripts/m10-tier-matrix.sh` 编排（每形态一次性
  实例 + 契约基线闸门）；license 文档挂载点 = `--license FILE` 或
  `BINFLOW_M10_LICENSE_FILE`（T-281 keygen 就绪后注入，缺省时 license 形态按未装姿态
  如实记录）。

## 2. 断言口径（M10 增量）

### 2.1 license / 门控断言 = REST 对账 + 行为翻转，不断言文案

```ts
// 对：档位断言走 GET 回显（tier 是闭集枚举，不是文案）
const lic = await m10Client().request('GET', '/binflow/api/system/license')
expect(JSON.parse(lic.text).tier).toBe('pro')

// 对：门控行为断言 = 三入口状态码翻转（D1~D7 降级闭集，architecture §15.1.3）
expect(await putRepo('go-local', 'go')).toBe(403)   // D3 建仓面（community）
await installLicense(proDoc)
expect(await putRepo('go-local', 'go')).toBe(200)   // 档位解锁
// 降级不劫持：D1 读放行 / D2 写 403 + X-Binflow-License-Required 头
expect(await getArtifact()).toBe(200)
expect(await putArtifact()).toBe(403)
```

- 403 形态断言 errors[] 信封**含 addon id 与所需档位**（可断言信封里有 `go` 与 `pro`
  两个 token），不断言完整文案（文案会随 K25 定稿微调）。
- `X-Binflow-License-Required: <addonID>` 响应头是可编程锚（console/CI 消费），必须断言。
- 切换时延（L08）：安装/卸载 REST 返回后 `sleep 1` 内行为翻转——脚本断言，不做竞态
  采样（无撕裂腿归 Go 侧测试 + QA 并发腿）。

### 2.2 档位 × addon 矩阵 = 表驱动，§5.2 是唯一期望源

槽位三态（unlocked/locked/disabled）× 三档的期望表抄 PRD §5.2（含 `addons.disabled`
最高优先级叠加规则）。矩阵断言只对 `GET /api/v1/addons` 的 `{id, state, minTier}` 三字
段做逐格对照——`displayName/description/reason` 是呈现面，不断言（K25 回写前会动）。

### 2.3 属性断言 = 部署对账 + 存量字面回归

```ts
// 矩阵参数部署对账：PUT 带 ;k=v → ?properties 读回
await client.request('PUT', '/binflow/m10-legacy-generic/ci/app.bin;build=77;env=prod', { raw: true, ... })
const props = JSON.parse((await client.request('GET',
  '/binflow/api/storage/m10-legacy-generic/ci/app.bin?properties=build,env')).text)
expect(props).toEqual({ build: ['77'], env: ['prod'] })

// 存量 ';' 回归（L22，FR-89-AC4）：seed-m10 fixture 原路径逐字节回读
import { fixtureBody, legacyFixtures } from './support/seed'
for (const fx of legacyFixtures()) {
  const r = await fetch(`${BASE}/binflow/${fx.repo}/${fx.path}`, { headers: auth })
  expect(r.status).toBe(200)
  expect(await r.text()).toBe(fixtureBody(fx.repo, fx.path))
}
```

存量集的形态学与「为何不种子成对形态」见 `web/scripts/seed-m10.mjs` 头注
（`a;b.bin` / `file;name.jar`〔ADR-0033 典例〕/ `x;y;z.txt` / 文件夹段 `dir;d/` /
maven 布局合规臂）。

### 2.4 UI 断言（License & Add-ons 页 / Properties Tab）

沿 M8 全套规矩：锚 = data-testid 且**先入 console-ux §10 锚册再落码**；禁像素 diff；
四态缺省锚；readonly 只读态 = `disabled` 断言 + 反断言，不截图。gated 入口**可见带档位
徽章**（D5——与 ADR-0029 决策 5 的有意差异，§11.41 已登记）：断言入口存在 + 徽章锚存在，
不断言视觉。

### 2.5 试点协议面（Go/NuGet）= 真实客户端命令级证据

`go mod download` / `dotnet restore` 的退出码与产物 sha256 是断言对象（spec 内
`child_process` 执行或 QA 脚本腿）；浏览器 spec 只覆盖 L21/L27 UI 腿。sumdb 代理 /
symbol server 是 M11+ Non-goal，出现即越界。

## 3. L 序列 → spec 映射（填充票号随 conductor M10 派单回填）

| spec | 腿 | FR | 填充票 |
|---|---|---|---|
| `license-lifecycle.spec.ts` | L01~L05 | FR-84 | license 核心 + REST 票（keygen = T-281） |
| `gating-matrix.spec.ts` | L06~L09 | FR-85/86 | 门控织入 + 注册表票 |
| `go-pilot.spec.ts` | L10~L13 | FR-87 | T-279（规格前置 T-278） |
| `nuget-pilot.spec.ts` | L14~L17 | FR-88 | NuGet 票（试点取舍见 ADR-0033 决策 5 分歧登记） |
| `properties-matrix.spec.ts` | L18~L22 | FR-89 | 属性 BE/FE 票（L22 数据 = seed-m10，已就绪） |
| `quickwins.spec.ts` | L23~L25 | FR-90 | MPU + smart-remote 票 |
| `console-license-addons.spec.ts` | L26~L27 | FR-91/86 | L26 = QA 文档走查腿（非浏览器）；L27 = FE 票 |
| `regression-nfr.spec.ts` | L28~L30 | 回归/NFR | QA 收口票 |

L28 的「无 license 实例 M9 守护面」**今天已经可跑**：`make test-m10-invariant`。

## 4. 给后续票的规矩

1. 新槽位/新端点先过 ADR（K23~K29 校准项回写后）再写 spec 断言——wire 细节冲突时以
   ADR 为准。
2. license 文档内容**永不入断言输出/快照**（NFR-S52：redact，仅 tier/licensee/期限
   元数据可入证据）。
3. 期望文件纪律（沿 T-250）：`scripts/m10-tier-matrix.baseline` 的任何翻转须先登记
   ADR/票号进 `scripts/m10-tier-matrix.whitelist` 才放行；改基线文件本身须先有 ADR。
4. 契约冻结延续：spec 不得为断言方便私加端点（ADR-0029 决策 4）。
