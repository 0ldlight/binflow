# Sprint 785 迭代报告 — T-323 收口（S3 MPU 续传）；M11 20/32；T-323R 补票登记

**日期**: 2026-08-27 23:25
**上轮**: Sprint 784（23:08 熔断后续跑）

## T-323 → done（merge `b903a70`）

- 引擎层付清 §11.31 债：落表 + ListParts 重建 + **完成-后读回校验门**（live MinIO 实证设计事实）；Close 保留语义（SIGTERM≡kill-9）；complete-race B5 泄漏补丁。
- **真实 MinIO 真进程 kill -9 链 -race 全过**；8 路并发；错 sha 整包拒弃。
- conductor 落地 main.go 接线（生产 S3 路径此前 rows=nil）——storage 227s + cmd 21s 复验绿。
- **T-323R [P2] 补票登记**：REST 面重启可见性（mpuRegistry 进程态）——落地即翻探针 leg 4，docker 面 S3 续传自动获得。

## T-319 补遗二（`72bf3ee`）

两个漏网测试文件（8 测全过）+ 报告修订——正式通知晚于提交的清单时差问题再次出现，**规程修补**：今后 conductor 收口前以 `git status` 全量对照 agent 变更清单，未覆盖文件先问再合。

## 状态

M11：**20/32**。在途 ×1（T-321 签名腿收尾中）。HEAD[develop]=`72bf3ee` 已推双远端。UAT 密钥仍待用户重发（缺 2 字符副本不可用）。
