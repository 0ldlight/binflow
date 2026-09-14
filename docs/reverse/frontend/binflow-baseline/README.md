# BinFlow 控制台截图基线（UI Phase 1 批 0）

设计系统迁移（docs/design/design-system-plan.md §6）的「当前值 golden」——
批 1（token 收敛）/批 5（旧 CSS 退役）的「截图差分≈0」验收门以此为底；
批 2/3 集中承受 G4 视觉变更时在此翻新基线。

## 资产

| 位置 | 内容 |
|---|---|
| `web/e2e/design-baseline.spec.ts` | 基线 spec：15 核心页 × 亮/暗 + 登录页（31 golden）+ axe 双主题现值采集 |
| `web/e2e/design-baseline.spec.ts-snapshots/` | golden PNG（go:embed 只吞 `dist/`——不进二进制；平台后缀 chromium-darwin） |
| `axe-current-{light,dark}.json` | axe 全谱现值（impact: null，WCAG 2.x A/AA + best-practice）——底片记录，不作闸门 |

## 运行

```sh
cd web && BASE=http://172.16.58.130:8083 ADMIN_PW=password \
  npx playwright test e2e/design-baseline.spec.ts            # 差分门（漂移即红）
npx playwright test e2e/design-baseline.spec.ts --update-snapshots  # 翻新基线
```

目标实例 = dev（172.16.58.130:8083，admin/password）。种子数据（demo-local 3 制品 /
demo-remote / demo-virtual / dev-baseline 用户 / qa-baseline 组）已在实例内，勿清卷。

## 已知动态面（mask/容差清单——spec 内 ROUTES 表）

- dashboard 审计卡：spec 自身登录写审计行 → mask `dashboard-audit-card`
- dashboard/storage 字节计数器：随测试活动单调增长（数字位漂移非布局）→ maxDiffPixels 12000 / 2000
- audit 页顶行滚动（~2 行登录事件）→ maxDiffPixels 4000
- users 页 admin 行 lastLogin 秒级时间戳 → mask `user-row-admin td[title]`

## 参照侧（双端说明）

Artifactory 参照截图语料 = `docs/reverse/frontend/parity-capture/`（104+ 图，
捕自 7.161.20 本地实例）。新参照（172.16.58.130:8082）为 7.161.15，控制台 UI
无实质差异，语料继续有效；若未来批次需要新参照像素，复用 tools 爬虫而非本 spec。
