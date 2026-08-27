# Sprint 775 迭代报告 — T-314 报告落盘（待 T-315 连收）；B7 收口预案

**日期**: 2026-08-27 19:26
**上轮**: Sprint 774（19:06）

## T-314 (deb remote+virtual) → done 待连收

- 交付（全部 internal/adapter/deb/，12 文件）：remote 面（svc.Get 引擎链双档 TTL/PUT in-handler 405/RE-06 驱逐）+ virtual 面（按请求现算：Release 重算校验节=自渲染字节、stanza 级合并 R3 去重键、写路由后台重算目标=路由成员、签名族/by-hash 404=apt 回退语义、成员失败容忍不掩盖）+ **P2 段**（xz/lzma 渲染落地零新依赖；bz2 因 dsnet 不可得降级 WARN——构建网络 20s 探测实证）+ control.go 多段 reader 修复。
- **真实 apt 容器链 41.69s PASS**：remote 腿（by-hash 回源+apt 自校验+install 可跑）+ virtual 腿（新容器双成员双包装机：local 首命中 + remote 回源）+ T-310 本地腿回归 PASS。
- 自测：internal 树 build/vet/lint 0；deb 包 47 用例；`go test ./internal/...` 29 包 ok；M10 双跑 0 deviations。
- **cmd 构建暂红**：`rpm.Register` 签名被在途 T-315 变更（接线纪律预期态，main.go 待 conductor）——**T-314 提交挂起至 T-315 收口**，届时顺序：T-314（adapter/deb）→ T-315（adapter/rpm + main.go 一行）双 --no-ff 连收。

## T-315 (rpm remote+virtual) 在途

实现推进中（+9 文件；apt/dnf 容器进程活跃——真实客户端腿在跑）。

## 状态

M11：15/32（T-314 已验待连收）。在途 ×1（T-315）。HEAD[develop]=`94a87ab`。
