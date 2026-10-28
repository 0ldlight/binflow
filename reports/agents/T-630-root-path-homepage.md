# T-630 — 裸根路径无首页（console root-path → homepage，case #0）

| 项 | 值 |
|---|---|
| 票据 | T-630（2026-10-28，L042 wave-1 立） |
| 类型/状态 | divergent → bug 修复票；**SPECIFIED**（Linear 回填待 OAuth） |
| 派发建议 | dev-go-core（internal/httpapi 根路由面）；波次内不修码 |
| 优先级建议 | P1（首页可达性 UX——「裸端口访问即达首页」的参照形态） |
| 输入 | L042 wave-1 活体差分：A=Artifactory 7.161.26 @ .36:32082 vs B=BinFlow dev-9e7ea000 @ .36:32083 |
| 证据 | reports/compatibility/L042-wave1-docker-remote-replay.md 分歧明细 ①；tools/difftest/v2/run/wave1/evidence/console-root-path/checks.json |

## 行为规格（clean-room，双端活体 2026-10-28）

- 参照：`GET /` → **200 text/html 首页**（与 `/ui/` 同源 HTML）；`GET /artifactory/` → **302** + 绝对 `Location: http://<host>:<port>/ui/`；`GET /ui/` → 200。
- 现值（BinFlow）：`GET /` → 404 JSON `no root mirror: … the legacy repository context prefix is not emulated`（自述设计决定）；`GET /binflow/` → 301 + 相对 `Location: /binflow/ui/`；`GET /binflow/ui/` → 200。

## 涉三个 delta

1. 裸根可达性：参照裸端口一次无先验请求即达首页；BinFlow 404。
2. 上下文根跳转状态码：302 vs 301。
3. Location 形态：绝对 URL vs 相对路径。

## 验收标准

1. 裸根 `/` 一次请求可达控制台首页（200 直出或 3xx 跳转均可，断言=可达性；具体落法实现裁量，但需与部署形态（容器直连端口 vs ingress）一起过架构裁定——现 404 文案系有意设计，翻案须产品/architect 拍板，不能实现自决）。
2. 上下文根跳转与参照同姿（302 + 绝对 Location），或经裁定登记 301/相对为 accepted 指纹后维持。
3. 差分 case `console-root-path`（tools/difftest/v2/cases/console_root_path.py）B 侧断言按对齐后语义调整并翻 PASS。

## 关联

- known-divergence 提案：`console/root-path-homepage`（compatibility-engineer 落账）。
- 拓扑注记：BinFlow docker 面在根 `/v2/*`（与 no-root-mirror 文案并存）——根路由分层（v2 vs 其余）已在位，本票只动非 v2 根路径。
