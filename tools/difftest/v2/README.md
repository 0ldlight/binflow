# tools/difftest/v2 — 差分测试框架 v2

双系统差分执行框架：**同一请求双发 A（Artifactory 参照）与 B（BinFlow），normalize 后比对，产出机读 `results.json`**。
v2 只用 python3 标准库，零第三方依赖；旧 `tools/difftest/l0xxx/` 批次脚本不动，v2 是并行新资产。

## 目录

```
tools/difftest/v2/
  runner.py     执行器：用例发现 → 双发 → normalize（注册制）→ 四态判定
  score.sh      读 results.json 输出单行机读 JSON
  cases/        用例目录（每用例一个 .py；_ 前缀文件跳过）
  run/          运行产物（results.json + evidence/，已 gitignore）
```

## 环境变量契约（凭据纪律，最高优先）

一切端点与凭据**只经环境变量注入**，代码与用例文件零字面量：

| 变量 | 含义 | 必需性 |
|---|---|---|
| `A_BASE` | A 侧（Artifactory 参照）base URL，如含上下文根则一并写全 | 双发用例必需 |
| `B_BASE` | B 侧（BinFlow）base URL | 双发用例必需 |
| `A_USER` / `A_PASSWORD` | A 侧认证 | `auth: true` 的用例必需 |
| `B_USER` / `B_PASSWORD` | B 侧认证 | `auth: true` 的用例必需 |

- 缺失必需 env → 该用例记 **BLOCKED**（reason 列出缺失变量名），绝不静默跳过。
- `results.json` 的 `env` 段只记 `set` / `MISSING`，不记值；evidence 文件中 `Authorization` 头恒写 `<redacted>`。

## 用例格式（cases/*.py）

### 形式一：声明式（推荐，HTTP 对照够用）

模块级 `CASE` 字典，无任何代码：

```python
CASE = {
    "id": "npm-dist-tags-get",          # 必填，小写 slug，全局唯一
    "title": "GET dist-tags …",          # 选填，人读
    "layer": "L5",                       # 选填，L0~L12 层级索引
    "domain": "npm",                     # 选填，normalize.yaml 的域键
    "auth": True,                        # 选填，默认 False；True 则六 env 全必需
    "timeout_s": 15,                     # 选填，缺省用 --timeout（默认 30，写死于 runner）
    "requests": [                        # 必填，非空；顺序执行，遇 FAIL 即停
        {"method": "GET", "path": "/api/npm/-/pkg/dist-tags",
         "body": None, "headers": {}},   # body 可 str/bytes；headers 附加请求头
    ],
    "normalize": ["drop-volatile-headers"],  # 选填；仅注册域才生效（见下节）
    "compare": {
        "status": True,                  # 比对 HTTP status（默认 True）
        "headers": ["Content-Type"],     # 头白名单：只比列出的头；空列表=不比头
        "body": "literal",               # literal 逐字 | json 结构化(键序无关) | none 不比
    },
}
```

### 形式二：编程式（真实客户端腿用）

模块级 `run(ctx)` 函数（curl / docker / mvn / pip 等 subprocess 双发腿、或 CASE 表达不了的多步序列）。与 `CASE` 可并存（`CASE` 提供元信息）：

```python
import subprocess

def run(ctx):
    outs = {}
    for name in ("a", "b"):
        p = subprocess.run(["curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
                            ctx.sides[name].base + "/api/system/ping"],
                           capture_output=True, text=True, timeout=ctx.timeout_s)
        outs[name] = p.stdout
    same = outs["a"] == outs["b"]
    ctx.write_evidence("0-curl.json", {"exitcodes": outs, "same": same})
    return {"status": "PASS" if same else "FAIL",
            "reason": None if same else "status codes differ",
            "evidence": ["evidence/<case-id>/0-curl.json"]}
```

`ctx` 提供：`ctx.sides["a"|"b"]`（dict：`["base"]` / `["user"]` / `["password"]`，全部来自 env）、
`ctx.http(side, method, path, body=None, headers=None)`（同 runner 双发腿，含 Basic auth）、
`ctx.timeout_s`、`ctx.write_evidence(name, payload)`（写 JSON 证据，返回相对路径）。
`run(ctx)` 必须返回 `{"status": PASS|FAIL|BLOCKED, "reason": str, ...}`；抛异常 → 用例记 BLOCKED（带异常信息），不拖垮整批。

## normalize 注册制

- runner 内置可执行规则词汇表：`drop-volatile-headers`（剔除 Date/X-Request-Id/X-Jfrog-Version 等易变头）、`base-url-placeholder`（各自实例 base URL → `<BASE>`）。
- 用例声明 `normalize: [...]` 后，规则**仅当** `docs/compatibility/fixtures/normalize.yaml` 中该用例 `domain` 的块带 `status: registered` 才应用。
- 未注册（当前 normalize.yaml 尚无任何 `status:` 行 → **全部域未注册**）→ 规则不应用，输出标注 `mode: "raw"` + note 说明原因。差异照实判 FAIL，绝不私自归一变绿；新归一需求提案给 compatibility-engineer。

## 四态判定

| 状态 | 语义 | 典型来源 |
|---|---|---|
| `PASS` | 声明的比对维度（status / 白名单头 / body）双端一致 | 差分一致 |
| `FAIL` | 任一声明维度 normalize 后仍不一致 | 差分差异 |
| `BLOCKED` | 环境或前置不可用：缺 env、端点不可达、用例文件本身坏、`run(ctx)` 返回 BLOCKED | 环境断供 |
| `NOT_RUN` | 未执行、无证据：`--dry-run`、被 `--case` 过滤 | 发现但没跑 |

skip ≠ PASS；HTTP 4xx/5xx 是合法响应，正常参与比对（不算 BLOCKED）；连接拒绝/超时/DNS 失败才算 BLOCKED。

## 运行方式

```bash
# 全量跑（env 先注入，示例用占位符，不是真实值）
export A_BASE="http://<artifactory-ref>/artifactory" B_BASE="http://<binflow-uat>/binflow"
export A_USER=… A_PASSWORD=… B_USER=… B_PASSWORD=…
python3 tools/difftest/v2/runner.py                 # 产物 tools/difftest/v2/run/results.json
bash tools/difftest/v2/score.sh                  # 单行 {"pass":..,"coverage":..}

# 常用变体
python3 tools/difftest/v2/runner.py --list          # 只列用例
python3 tools/difftest/v2/runner.py --dry-run       # 全部 NOT_RUN（发现自检）
python3 tools/difftest/v2/runner.py --case demo-ping --out tools/difftest/v2/run/demo
bash tools/difftest/v2/score.sh tools/difftest/v2/run/demo/results.json
```

> `--out` 是相对**当前目录**的路径：默认值是 runner 同级的 `run/`（gitignored）；从仓库根显式传 `--out tools/difftest/v2/run/<name>`，别把产物撒到仓库根。

- runner 退出码：`0` = 本轮跑完（含 FAIL——状态走 results.json/score.sh）；`2` = 用法/发现层错误（坏用例文件、未知 `--case`）。runner 自己**不因 FAIL 非零**，CI 门用 score.sh 的 JSON。
- 超时：每请求独立超时，`--timeout`（默认 30s，写死于 `runner.py` 的 `DEFAULT_TIMEOUT_S` 并记入 results.json）或用例 `timeout_s` 显式覆盖；**永不自动放宽**。
- 证据：每个请求的 A/B 原始响应（status / headers / body，Authorization 脱敏）落 `run/evidence/<case-id>/`，`results.json` 逐用例带相对路径，结论可反向对到证据。

## Maven 首批用例（T-523）

`cases/maven_*.py` 四例（共享助手 `cases/_mavenlib.py`，`_` 前缀不参与发现）：

| case id | 层 | 行为依据（docs/reverse/） |
|---|---|---|
| `maven-virtual-deploy` | L5 | repo-semantics §8.2 L217（defaultDeploymentRepo 写路由）+ maven-npm-pypi §1.1/§1.3/§1.4/§1.5 + virtual-resolution §5.2（pom 清洗对无 repositories 的 fixture 为 no-op）；真实客户端腿 = `mvn deploy:deploy-file`（RELEASE+SNAPSHOT），settings.xml 用 `${env.DIFFTEST_MVN_USER/PASS}` 插值，凭据零落盘 |
| `maven-resolve-remote-cache` | L6 | remote-cache-projection §2.1 L56/§3/§4 + virtual-resolution §2；上游 fixture = Maven Central `javax.annotation-api-1.3.2.pom`（sha256 常量内嵌）；缓存命中判据 = `<remote>-cache` 投影仓直 GET 200 + 双拉字节精确（wire 级 HIT 证明需实例日志，v2 首批不做） |
| `maven-virtual-metadata-merge` | L7 | virtual-resolution §5.1：versions 并集重排、latest/release 重算、合并结果不写 `<virtual>-cache`；成员 metadata 异步计算（maven-npm-pypi §1.4）→ 用例内 poll（15s 预算，不自动放宽） |
| `maven-virtual-delete-passthrough` | L7 | repo-semantics §8.2 L220 / virtual-resolution §7.5：virtual DELETE 只作用于自身聚合缓存 → 404 且成员制品存活；对照腿 = 成员直删 204 |

- 判定口径：断言值来自行为规格推导（`EXPECTED` 字典），双端各自对照规格 + A/B 互比；`PASS` = 双端全部断言命中规格。不是「捕获即真相」。
- 命名空间：所有临时仓 key 以 `difftest-mvn-` 前缀，用例 finally 双端清理。
- 无凭据时的接线自检：`python3 _smoke_mock.py <port>` 起哑端点（非差分 oracle，只证明 runner→case→mvn 子进程→evidence 管线），配 `A_BASE/B_BASE` 指向本机两端口实跑——用例应稳定 FAIL（断言值记录 404 等），不应 BLOCKED/崩溃。

## score.sh 输出

单行 JSON：`{"pass":N,"fail":N,"blocked":N,"not_run":N,"total":N,"coverage":C}`；
`coverage = pass / (total − blocked − not_run)`，分母为 0 时 `coverage` 为 `null`。退出码恒 0。

## 约束与红线

- python3 标准库 only；新增依赖 = 架构变更，须先过 conductor。
- 任何凭据/密码字面量禁止出现在代码、用例、README、测试中（安全审计 grep 面）。
- 双端实例操作限定 difftest 专用 repo/keyspace，测试后清理。
- 与旧 `tools/difftest/l0xxx/` 完全隔离：不改旧目录一行，旧脚本继续服务过渡期批次。
