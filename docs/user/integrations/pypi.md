---
title: PyPI 接入
sidebar_position: 22
---

# PyPI 接入

> 适用版本：M3（PEP 503 simple index + twine upload + 下载链；PRD milestone-3 v1.2）。
> 本文核心链在 M3 QA 基线（commit `0f86229`，T-74/T-76 验收产物）上复跑：`pip.conf` 安装、`.pypirc` twine 上传、`#sha256=` hash 对账均退出码 0（复跑记录见 `reports/agents/T-77.md`）；依赖链、PEP 691、remote 代理、virtual 聚合取自 T-74/T-75/T-76 验收记录。客户端锚定 pip 24+（26.2.1 实测）+ twine 6+（7.0.0 实测）。

把 BinFlow 当作私有 Python 包索引：`pip.conf` 指向 BinFlow 安装、`twine upload` 发布——simple index 按 PEP 503/629 规范实现，`#sha256=` 哈希校验、wheel+sdist 并存、依赖链解析全部成立。

## 前置条件

- 运行中的 BinFlow 实例（`BASE=http://localhost:8080`）。
- 管理员凭据 `admin` / `$ADMIN_PW`。
- 一个 `packageType=pypi` 的仓库（第 1 步创建）。

## URL 形态

PyPI 域挂在 `/binflow/api/pypi/` 前缀下；pip 消费的是 simple index：

```
安装索引：$BASE/binflow/api/pypi/<repoKey>/simple
上传端点：$BASE/binflow/api/pypi/<repoKey>
```

包名按 PEP 503 归一化（lower + `[-_.]+`→`-`）：`Demo_Pkg`、`demo_pkg`、`demo-pkg` 三个 URL 返回同一 index 页。

```bash
export BASE=http://localhost:8080
export ADMIN_PW=<你的管理员口令>
```

## 接入步骤

### 1. 创建 pypi 仓库

```bash
curl -su admin:$ADMIN_PW -X PUT $BASE/binflow/api/repositories/pypi-local \
  -H 'Content-Type: application/json' \
  -d '{"rclass":"local","packageType":"pypi"}' \
  -o /dev/null -w '%{http_code}\n'        # 200
```

### 2. 安装侧：`pip.conf`（即拷即用）

用户级 `~/.config/pip/pip.conf`（Linux；macOS 为 `~/Library/Application Support/pip/pip.conf`）或项目级 `pip.conf`，也可用 `PIP_CONFIG_FILE` 显式指定：

```ini
[global]
index-url = http://localhost:8080/binflow/api/pypi/pypi-local/simple
```

```bash
pip install demo-pkg                     # 经 BinFlow 解析安装
```

一次性用法（不动配置文件）：`pip install --index-url $BASE/binflow/api/pypi/pypi-local/simple demo-pkg`。

### 3. 发布侧：`.pypirc` + twine（即拷即用）

`~/.pypirc`：

```ini
[distutils]
index-servers =
    binflow

[binflow]
repository = http://localhost:8080/binflow/api/pypi/pypi-local
username = admin
password = <你的管理员口令>
```

```bash
pip wheel . -w dist/          # 或 python -m build（产 .whl 与 .tar.gz）
twine upload --repository binflow dist/*
```

一次性用法：`twine upload --repository-url $BASE/binflow/api/pypi/pypi-local -u admin -p $ADMIN_PW dist/*`。

上传协议要点：multipart 的 `:action` 必须为 `file_upload`（其它值 400 `unknown action '<action>'`）；`md5_digest` 可选（twine ≥ 6.2 不再发送，服务端自算 sha256；提供且不一致走 409 校验链）；响应统一 200。存储路径为 `<name>/<version>/<filename>`（原始文件名），与 Artifactory 布局对齐。

### 4. 验证：hash 对账与依赖链

```bash
# simple 页的 #sha256= fragment == 实际下载文件 sha256（pip 安装即自动校验）
curl -s $BASE/binflow/api/pypi/pypi-local/simple/demo-pkg/ | grep -o '#sha256=[a-f0-9]*'
curl -s -o dl.whl $BASE/binflow/api/pypi/pypi-local/packages/demo-pkg/0.1.0/demo_pkg-0.1.0-py3-none-any.whl
shasum -a 256 dl.whl
```

依赖链：包 A 声明依赖包 B（同仓另一包），`pip install A` 自动从同一 index 装齐两者。wheel 与 sdist 同版本并存：index 页两文件俱在，`--only-binary` 取 whl、`--no-binary` 取 tar.gz。

## remote / virtual 仓的用法

- **remote 仓**（代理 pypi.org 等）：`index-url` 指向 `$BASE/binflow/api/pypi/pypi-remote/simple`——首次回源缓存、二次安装零上游流量；simple 页 href 重写为 BinFlow 路径、`#sha256=` fragment 保留上游值。**pypi.org 真上游实测可用**（其 `/packages/` 302 到 files.pythonhosted.org，BinFlow 逐跳过 SSRF 校验后跟随成功，T-75 观察 O3），受限集群离线复用场景友好。
- **virtual 仓**（聚合 local + remote）：`index-url` 指向 `$BASE/binflow/api/pypi/pypi-virtual/simple`——本地包与上游包一次装齐，simple 索引为成员条目并集（每次现算）。
- 代理仓上游日志会看到 pip 对 `simple/pip/` 的自检探测（版本检查），属正常噪音。

## 匿名与凭据

| 场景 | 行为 |
|---|---|
| 匿名 GET simple/文件（默认 `anonymous_access: true`） | 200——`pip install` 免凭据 |
| 匿名 upload（POST） | **401** `authentication required` |
| 全局关匿名 | GET 也需凭据：pip 用 `--user`/`$PIP_USER` 环境变量或 URL 内嵌凭据；twine 走 `.pypirc` |

## simple index 细节（脚本/工具对接时需要知道）

- 文档头含 `<meta name="api-version" value="2" />`（PEP 629）；条目按文件名排序、HTML 转义。
- 无尾斜杠 `GET .../simple/<name>` → **302** 补尾斜杠（`Location` 为相对路径）。
- `ETag` 为稳定内容哈希（不透明串），`If-None-Match` 命中 → **304**。
- **PEP 691 JSON simple 已支持**：`Accept: application/vnd.pypi.simple.v1+json` → JSON 形态（`files[]` 与 HTML 等价，`Vary: Accept`）。
- 哈希算法**仅 sha256**（index fragment 与服务端校验一致）；不提供 md5 fragment。
- 未知名 → 404（E-01 `errors[]` 信封）。

## 有意不兼容与差异（PyPI 域）

| 行为 | BinFlow | 依据 |
|---|---|---|
| 同 filename 重复上传 | **400** `file '<f>' already exists in repository '<repo>'; overwriting is not allowed (path <name>/<version>/<f>)`（twine 报 HTTPError 400） | warehouse 语义，防覆盖 |
| PyPI JSON API（`/pypi/<pkg>/json`） | **404**——不做 | §2.2（M4+ 评估） |
| PEP 592（yank）、PEP 658（分离 metadata） | 不做——`yanked` 字段收下作元数据、不影响索引 | §2.2 |
| `rel="internal|external"` 属性 | 不输出（Artifactory 私有属性，pip 忽略；PEP 503 无此定义） | 有意不补充 |
| 托管 UI 前缀 `/binflow/api/pypi-ui/**` | **404** | M1 边界维持 |
| setuptools legacy upload（python 3.8- 老客户端） | 不做——以 twine 现代形态为准 | §5.3 |

## 常见报错对照

| 症状 | 原因 | 处置 |
|---|---|---|
| twine `HTTPError: 400 Bad Request` + `file ... already exists` | 同名文件已上传过（不允许覆盖） | 版本号升级重新构建；或先删旧构件 |
| upload 400 `unknown action 'submit'` | multipart `:action` 不是 `file_upload` | 用 twine（自动携带正确 action），勿手工构造 legacy 表单 |
| upload 401 `authentication required` | 匿名上传或凭据错误 | `.pypirc` 配置 username/password |
| install 404（包明明存在） | index-url 少了 `/simple` 后缀，或仓不对 | 核对 URL 形态（上文）；包名大小写/下划线无关（PEP 503 归一化） |
| `pip download` hash 校验失败 | 上游内容与 fragment 不一致（理论上不应发生——服务端逐位落盘） | 清 pip 缓存重试；仍失败请提工单附路径 |
| curl simple 页 302 | 无尾斜杠（规范行为） | 跟随重定向或补尾斜杠 |

## 下一步

- remote/virtual 仓的创建与缓存管理：[remote/virtual 管理指南](../admin/remote-virtual.md)
- Maven / npm 接入：[maven](maven.md) · [npm](npm.md)
- 从 Artifactory 迁移的概念对照：[faq.md](../faq.md)
