# Sprint 409 迭代报告 — T-217 关账（B1 闭环）+ 波 5 三线派发

**日期**: 2026-08-23 10:31
**上轮**: Sprint 408（双 review 收敛 B1，返修在途）；其间返修完成 → conductor 复验 → 关账 → 波 5 派发。

## 阶段 2 — T-217 收尾

1. **B1 返修**：替换臂 union(body, 存量) ⊆ 覆盖集 + 矩阵腿（403 + 清单字节不变）+ 对照腿（合法替换不误伤）。红绿自证剥离并集判定 → 201（恰为 reviewer 探针的洞形态）→ 还原绿。
2. **conductor 复验**：TestT217CoverageMatrix -race 绿（4.1s）+ build OK + `EXPECT=1` 矩阵 0 deviations。
3. **提交 `04f88fb`**，树干净。RBAC 后端全量就位（T-212/215/217）。

## 阶段 3 — 波 5 派发（3/4，area 正交）

- **T-218**（dev-frontend）：角色下拉/manage 复选/readonly 只读态 + Playwright V12~V14 + governance.ts 词表（T-215 移交）。
- **T-219**（dev-go-core）：step-up 全契约（ADR-0027 修订版：step_up_password/mint grant/双错误码/双 config 键默认 off）+ p.Admin→CanManage 统一（T-215 移交）+ Keycloak/LDAP 容器集成测试。
- **T-221**（qa-engineer）：V01~V11 全表 + 真实客户端三协议（docker/mvn/npm）+ usage 翻转腿 + B1 回归腿 + binary 新鲜度纪律。

## 阶段 0（本轮触发时）

三线 transcript 全活跃（10:30-10:31）。HEAD=`c57c4bd`。

## 下轮计划

波 5 收齐 → T-218/T-219 review + T-221 qa 结论 → 波 6（T-220 债务 / T-223 文档 I / T-226 等价回归）。
