# Playwright e2e（web/e2e/）

套件 = 交互断言制（ADR-0029 决策 3；锚 = data-testid，见各里程碑子目录
README）。运行口径（真栈 + 新鲜二进制 + 种子）见 `m8/README.md §1`；
本文只记**两条环境前提**——都是真实踩过、复跑两轮以上实证的坑
（T-391 / FR-128-AC2 落笔；证据：reports/agents/T-374.md §3、
T-377.md §9 与 D2）。

## 1. 全量跑 = 纯净 community 实例前提

`npx playwright test`（全量）按**一次性、community 档、新种子的实例**
设计；license 档与实例状态都会成片误红：

- **license 档**：pro 宿主上全量 **132 红两轮**（T-374 / T-377 实证）——
  登录/授权面、trash-locked 卡、L27 community 地板、a11y 路由面全是
  「community 形态断言」撞 pro 行为，环境问题非产品缺陷；换纯净
  community 宿主即 236 绿。
- **实例状态**：同一数据目录连跑两轮，用户/组等夹具残留也会误红
  （T-374 §3-1）；全量前起 scratch 数据目录的新实例。

## 2. dind 调试：`--feature containerd-snapshotter=false`

dind 若处于 **containerd snapshotter** 形态，`docker pull` 对 plain-HTTP
registry 的 blob 取数会走 https 回退（该 resolver 不遵守
`--insecure-registry`）→ 拉取超时；BinFlow 访问日志零到达（请求根本
没到服务端，非服务端缺陷）。经典 overlay2 snapshotter 同实例全链绿。
docker 相关腿调试建议给 dind 传
`--feature containerd-snapshotter=false`（T-377 D2）。
