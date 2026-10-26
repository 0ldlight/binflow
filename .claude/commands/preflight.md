---
description: 差分/协议测试前置检查——环境变量、端口、双实例健康与基准版本断言一次过
---

差分批或协议探腿开跑前执行（$ARGUMENTS 可指定端口段或域，省略=全项）：

1. **凭据在位（只查存在，禁止回显值）**：`BINFLOW_REF_USER`/`BINFLOW_REF_TOKEN` 与差分六 env（`A_BASE`/`B_BASE`/`A_USER`/`A_PASSWORD`/`B_USER`/`B_PASSWORD`）逐一 `[ -n "$VAR" ]` 判存在——缺失即列 BLOCKED 清单并停止；任何步骤不得把凭据值打进输出/报告/终端（T-625 zsh 回显事故红线）。
2. **端口预检**：对拟用 18xxx 端口段逐个 `lsof -i :<port>`——占用即换段并提示（18312 长期占用教训）；建议选段后复检一轮。
3. **双实例健康**：A（参照 192.168.120.36:32082——2026-10-26 起，API 基路径带 `/artifactory` 前缀，裸 `/api/*` 404）与 B（UAT）各发 `/artifactory/api/system/ping` 或根探针；不可达即降级 golden-only 模式并显式标注。
4. **基准版本断言**：A 面版本须为 `7.161.26`（`/artifactory/api/system/version` 需凭据——env 注入 `ARTIFACTORY_REF_PASSWORD`（gitignored `.env.ref`），401 时以 UI 探针降级断言并标注 confidence≤medium；E+ 档以 addons/entitlements 面（federated/release-bundle/xray 在册）佐证）——旧 7.161.15 与 .38:8082 参照均已退役，误连旧实例属结论作废级事故。
5. **docker 通道**：远程 docker 经 `ssh lzw@172.16.58.130` 可达性抽检（本地 docker 禁用纪律）。
6. **产出**：一行总结 `PREFLIGHT PASS/BLOCKED` + 阻塞清单；PASS 才进差分派发。
