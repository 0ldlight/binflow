# T-631 — remote 仓 allowPrivateUpstream 守卫与 7.161.26 活体行为分歧（配置面）

| 项 | 值 |
|---|---|
| 票据 | T-631（2026-10-28，L042 wave-1 立） |
| 类型/状态 | divergent（配置面/运行时拉穿守卫）；**SPECIFIED** |
| 派发建议 | dev-go-core（repo 配置面）+ dev-go-storage（internal/remote 拉穿守卫） |
| 优先级建议 | P2（阻塞私有 HTTP 上游场景的开箱对齐；workaround=显式 allowPrivateUpstream=true） |
| 输入 | L042 round-4/5 探针：双端同 PUT 载荷、上游 http://192.168.120.36:32100（私有 HTTP） |
| 证据 | reports/compatibility/L042-wave1-docker-remote-replay.md 新发现 F-1 |

## 行为差异（双端活体）

- 参照（7.161.26）：GET 配置面**不回显** allowPrivateUpstream 字段；私有 HTTP 上游建仓即拉穿（无守卫）。
- BinFlow：配置面含 `allowPrivateUpstream`，**默认 false**；建仓 200，但运行时拉穿 `400 Cannot fetch '<repo>/<image>': upstream target refused — private or suppressed upstream (… upstream target <host> rejected: priva…)`；显式置 true 后拉穿正常（L042 全批 PASS 依赖此步）。

## 验收标准

1. compatibility-engineer 先核实 7.161.26 官方语义（该字段属较新 JFrog 安全增强，7.161.26 活体无此字段/默认放行）：若属「新版本才有的能力」，裁 either 默认值对齐参照活体（放行）or 登记 known-divergence（意图性安全差异 + 文档化 workaround）。
2. 若裁对齐：同 PUT 载荷双端行为一致（建仓 + 首次拉穿成功）；差分用例（docker_wave_setup.sh 去掉 B 侧特有 allowPrivateUpstream 注入）全批 PASS。
3. 建仓 API 配置回读面差异（B 多出的字段）在 docs/compatibility 配置面账本挂注记。

## 关联

- 上游 zot 对 Accept 严格白名单的交互（环境观察）与本票无关，见 L042 环境观察节。
