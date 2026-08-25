# Sprint 598 迭代报告 — 用户指令：license 控制体系入 M10 核心

**日期**: 2026-08-25 11:06
**上轮**: Sprint 597

## 指令与口径

「binflow 也需要拥有和 Artifactory 一样的 license 控制，例如控制高可用等」——落板（`163c004`）为 **M10 核心组件**：
- **自有密钥体系**（clean-room 边界：对齐「功能分级门控」行为模式；不复制 JFrog 的 license 格式/校验算法——那既是 clean-room 红线也涉规避他人授权机制）
- 分级门控对标 AddonType 三档行为模式（BinFlow 自定档位）；HA/Xray 集成/distribution 等企业功能按档解锁
- 全局禁用开关 + 管理 REST 面（/api/system/licenses 等价）
- 与十大缺口并列进入 M10 规划输入

## 阶段 0

无在途；HEAD=`163c004`。M10 规划输入现已三项：主矩阵 + 十大缺口 + license 体系。

## 下轮计划

下一配额窗启动 M10 规划 workflow（license 门控框架预计为 architect 的重点 ADR——门控点如何织入既有能力面）。
