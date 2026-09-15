# 运行时分析 · 管理与运维面（证据指针文档——正文在既有产物）

> 截图基线 ：8082（7.161.20）。路径前缀 `pc/` = `docs/reverse/frontend/parity-capture/`。

## 1. 截图

| 资产 | 内容 |
|---|---|
| `pc/screenshots/screens/storage-summary.png` | Storage Summary（存储总结） |
| `maintenance.png` | Maintenance（GC/Cleanup 族维护窗） |
| `backups.png` | Backups（备份定时 CRUD 面） |
| `import-export.png` + `ie-repositories.png`（归 repo.md） | Import/Export 管理页 |
| `system-logs-af.png` / `system-logs-monitoring.png` | System Logs Viewer 两入口视图（Service/Node/LogFile 三选择器） |
| `log-analytics.png` | Log Analytics（日志转发配置） |
| `mail.png` | Mail 设置 |
| `general-config.png` / `af-general-settings.png` / `config-descriptor.png`（归 console.md） | General Config 三视图 |
| `builds.png`（归 artifact.md） | Builds 管理面 |
| `release-bundles.png` / `rb-target.png` / `bundles-source.png` | Release Bundles 三视图 |
| `pc/screenshots/dialogs/release-bundle-create-*.png`（step1/source/filled/review/result/before 6 张） | RB 创建流（止步 review——signing-key 前置缺失） |
| `lifecycle.png` / `retention-policies.png` / `retention-monitoring.png` | Lifecycle / Retention 策略与监控（企业面静态页） |
| `pc/screenshots/dialogs/retention-policy-create-form.png` | Retention 策略创建表单 |
| `migration-tool.png`（归 console.md） | 迁移工具 |
| `states/toast-user-created.png` / `toast-user-created-late.png` / `toast-repo-name-invalid.png` | Toast 反馈族（成功/迟到/非法名） |

## 2. 行为规格（正文）

- `docs/reverse/cron-scheduling.md`——Quartz 语法域/出厂调度默认值（backup/GC/cleanup 族）/校验拒绝文案/next-run（`/ui/api/crontime`）。
- `docs/reverse/metrics.md`——内部指标框架/Prometheus 集成现状。
- `docs/reverse/logging/`——format-spec/taxonomy/evidence（日志格式与 taxonomy）+ `docs/compatibility/logging-matrix.yaml`。
- `docs/reverse/import-export-api.md`——导入/导出 REST（系统/仓库级，marker 文件）。
- `docs/reverse/storage/`——binary-provider-chain.md / prune-gc-admin.md（GC/prune 管理面行为）。
- `docs/compatibility/matrix.yaml` D06（系统与运维 30 行，超集 7）+ contracts/storage-admin.yaml（10 条目）。
- `docs/reverse/release-bundle.md` + `docs/reverse/distribution/`——RB 域 18 端点 + Distribution 侧。
- `docs/reverse/frontend/screens.yaml`——maintenance/backups-list/backup-form/storage-summary/system-logs-viewer/import-export 屏四态。

## 3. 已知运行时事实

- System Logs 尾随/三选择器形态——t459-probe 逐字对位。
- RB 创建：实例无 signing keypair（REST keypair 空集）——创建流不可完成（pro-inner-flows partial 项，安全纪律不改实例配置）。

## 4. 缺口声明

- HA 集群管理 UI：单节点实例不可达，零证据（反编译侧见 inv-4-addons）。
- Xray/Pipelines/Insights 等 Platform 外部产品管理面：不在采集范围（D14 ⛔）。
- Retention/Lifecycle 仅静态页截图，无交互流证据。
