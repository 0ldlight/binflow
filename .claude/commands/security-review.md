---
description: 触发安全审计（旁路插入 security-auditor）
---

执行一轮安全审计（$ARGUMENTS 可指定焦点域，省略=全局面）：

派 `security-auditor`（后台，旁路不打断在途）：
- 焦点：制品仓库威胁模型——越权（ACL 探针）/路径穿越/SSRF（remote 出站）/供应链（依赖漏洞 `govulncheck`+npm audit）/密钥管理/容器配置。
- 范围：internal/ + web/ + deploy/ + charts/（只读，只发现不改码）。
- 联动：Security 相关在途票的 negative test 覆盖核对（DoD 硬门）。
- 产出：`reports/security/<date>-audit.md`（风险清单：severity/CVSS 级估/复现路径/修复建议票面）。

收编：Critical/High → P0/P1 缺陷票即时入 BOARD；红线类（对外暴露面）→ 上报用户。
