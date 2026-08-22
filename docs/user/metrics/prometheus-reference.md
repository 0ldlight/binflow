---
title: Prometheus 指标参考
sidebar_position: 60
---

# Prometheus 指标参考

> 适用版本：M6（ADR-0022 / PRD FR-61，T-163 交付；指标命名按 T-197 D5 校准）。指标族名、标签与预播行为逐项核对 `internal/httpapi/metrics.go`；端点行为（匿名 200 / require_auth 401→200 / TYPE 行）于本机构建实测。
> BinFlow 自带零依赖指标实现（无 client_golang），文本暴露格式 **0.0.4**。

`GET /metrics` 挂在**根级**（与 `/healthz`/`/readyz` 同族的前缀例外，不在 `/binflow` 下）：

```bash
curl -s http://127.0.0.1:8080/metrics | head
# # HELP binflow_auth_logins_total Console logins by identity provider source.
# # TYPE binflow_auth_logins_total counter
# binflow_auth_logins_total{source="ldap"} 0
# ...
```

## 端点行为

| 项 | 值 |
|---|---|
| 路径 | `/metrics`（根级；完整地址即 `http://<host>:<port>/metrics`） |
| 方法 | GET / HEAD（其余 405 + `Allow` 头） |
| Content-Type | `text/plain; version=0.0.4; charset=utf-8` |
| 默认暴露 | **匿名可读**（scrape 不付认证往返，探针同款姿态） |
| 收紧 | `metrics.require_auth: true` 后需任意有效凭据（非 admin 门）；匿名/坏凭据 401 + Basic challenge |
| env | `BINFLOW_METRICS__REQUIRE_AUTH=true` |

不想开认证又不想暴露？在反代层限制 `/metrics` 的来源（白名单 Prometheus 主机）。

## 指标族（四类，名字逐字对齐实现）

### HTTP

| 指标 | 类型 | 标签 | 语义 |
|---|---|---|---|
| `binflow_http_requests_total` | counter | `method` / `path` / `status` | 请求计数（全部路由） |
| `binflow_http_request_duration_seconds` | histogram | `method` / `path` | 延迟；桶 `.005 .01 .025 .05 .1 .25 .5 1 2.5 5 10`（+Inf 隐式） |
| `binflow_http_requests_in_flight` | gauge | — | 当前在途请求数 |

### 存储

| 指标 | 类型 | 标签 | 语义 |
|---|---|---|---|
| `binflow_storage_blobs` | gauge | `engine`（`disk`/`s3`） | blob 台账行数 |
| `binflow_storage_blob_bytes` | gauge | `engine` | **disk** = `blobs/` 物理字节数；**s3** = 配额口径的逻辑字节合计（仓用量求和） |

> **升级注意（T-197 D5 改名，无旧名别名）**：两个 storage gauge 的 v1 旧名 = 上表现名 + `_total` 后缀，已按 Prometheus 规范（`_total` 仅 counter）改为现名——已有面板 / 告警规则里的旧 PromQL 需随升级替换（去掉 `_total`），否则查不到序列。

### 认证

| 指标 | 类型 | 标签 | 语义 |
|---|---|---|---|
| `binflow_auth_logins_total` | counter | `source`（`local`/`oidc`/`ldap`） | 控制台登录成功计数（按认证来源；失败不计） |

### 复制

| 指标 | 类型 | 标签 | 语义 |
|---|---|---|---|
| `binflow_replication_tasks` | gauge | `status`（`pending`/`in_progress`/`success`/`failed`/`skipped`） | push 复制任务行数；**复制未装配的实例整族不出现**（不是零值） |

零流量时 counter/gauge 的关键序列**预播在场**（先置 0）——Prometheus 重启后不会因为「还没见过流量」而丢序列；histogram 在首次观测前只有 HELP/TYPE 头、无序列（实测确认），第一次请求后 `_bucket`/`_sum`/`_count` 才出现。

## path 标签的基数防护

`path` 不是原始 URL——变参路径折叠成模板（总基数 < 100）：

| 面 | 折叠为 |
|---|---|
| `/healthz` `/readyz` `/metrics` | 原样（3 个值） |
| docker `/v2/**` | `/v2` 整面 |
| `/binflow/ui|assets|docs/**` | 段级（如 `/binflow/ui`） |
| 内容路径 `/binflow/<repo>/<path>` | `/binflow/:repo/:path` |
| 已知变参 API 族 | 如 `/binflow/api/repositories/:key`、`/binflow/api/security/users/:name` |
| 未收录深路径 | 两段字面前缀 + `:rest` 兜底 |

## 抓取与告警示例

`prometheus.yml`：

```yaml
scrape_configs:
  - job_name: binflow
    scrape_interval: 15s
    metrics_path: /metrics
    static_configs:
      - targets: ["binflow-host:8080"]
    # metrics.require_auth=true 时：
    # basic_auth: { username: admin, password: <pw> }
```

常用 PromQL：

```promql
# QPS 与错误率（5xx 占比）
sum(rate(binflow_http_requests_total[5m])) by (path)
sum(rate(binflow_http_requests_total{status=~"5.."}[5m]))
  / sum(rate(binflow_http_requests_total[5m]))

# P99 延迟（按路径）
histogram_quantile(0.99,
  sum(rate(binflow_http_request_duration_seconds_bucket[5m])) by (le, path))

# 存量与登录面
binflow_storage_blobs{engine="disk"}
sum(increase(binflow_auth_logins_total[1h])) by (source)

# 复制积压
binflow_replication_tasks{status="pending"}
```

## 验证

```bash
curl -s -o /dev/null -w '%{http_code} %{content_type}\n' http://127.0.0.1:8080/metrics
# 200 text/plain; version=0.0.4; charset=utf-8

curl -s http://127.0.0.1:8080/metrics | grep -c '^# TYPE'   # 7 族
```

## 已知边界（如实）

- 复制**延迟**无直方图（仅任务计数；埋点为后续票）；`/metrics/json` 未实现。
- s3 后端的字节数是逻辑口径（见上表），物理桶占用需在对象存储侧看。

## 下一步

- 健康探针语义（`/healthz` vs `/readyz` vs `/api/v1/health`）：[治理指南](../admin/governance.md)
- S3 后端配置：[S3 指南](../guides/s3-config.md)
