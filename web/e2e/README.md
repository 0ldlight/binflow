# Playwright e2e（web/e2e/）

套件 = 交互断言制（ADR-0029 决策 3；锚 = data-testid，见各里程碑子目录
README）。运行口径（真栈 + 新鲜二进制 + 种子）见 `m8/README.md §1`；
本文记**两条环境前提**（§1~§2，T-391 / FR-128-AC2 落笔，证据：
reports/agents/T-374.md §3、T-377.md §9 与 D2）与 **T-424（FR-140.2）
起的四节工程纪律 + 两条 flake 注记**（§3~§6——每条都是真实事故的成文，
不是风格建议；违反口径的先例全部带报告锚）。

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

## 3. 破坏性动作纪律：默认禁点 + 差集法 + 共享 fixture 快照前置

> 事故源：**INC-1**（2026-08-31，reports/agents/T-381.md §INC-1）——探测脚本
> 的行选择器误匹配 AG Grid 容器级元素，「行尾按钮」探针实际点开了首行
> example-repo-local 的删除确认框，脚本随后的 `/^delete$/i` 点击**满足了
> 它**——t226 活体语料仓（120 制品）被误删。靠 VM 快照全量回灌恢复
> （120/120 + filestore 159 blob 按校验和寻址字节等价），属侥幸，不重演。

- **默认禁点**：对任何非自备实体，删除/写路径动作**一律不点击**。探测 =
  只读观察（DOM dump / computed style / hover 态采样）；要看「点击后会
  怎样」，先自备一次性实体再看。
- **差集法**：形态探测用「操作前后 DOM 差集」推断控件行为，不以点击
  验证存在性。INC-1 的事后还原正是用差集法证明 trash 点击即弹 V8 确认框
  ——确认框本来就挡住了直删，是探测脚本替用户按了确认。
- **永不点确认**：探测脚本对 `/^(delete|confirm|ok)$/i` 类确认钮零点击，
  没有例外条款。
- **spec 内的破坏性腿只打自备夹具**：uniq key + API 直备 + 收尾
  `?deleteContent=true`（m8/repositories-admin §3 形态）。spec 永不把删除/
  覆盖类动作指向：共享留验实例、t226 语料仓、用户本地实例的数据目录。
- **共享 fixture 先快照**：确需在共享现场旁路验证时，前置条件 = 数据目录
  副本或 VM 快照（用户实例报障的「沙箱复现」纪律同源）——恢复上限以内
  的事故可逆，超出即不可逆。

## 4. 进程清理纪律：pkill 按端口精确杀（禁宽模式串）

> 事故源（M14~M15 四起，全在共享开发机上）：T-307R（`pkill -f
> "binflow-server serve"` 击落 licensed 18280/18290，T-327.md §过程）；
> T-384（同模式误伤 T-383 留验 :8143，T-384.md §4 过程注记）；T-389
> （`pkill -f binflow-server` 面更宽，误杀 :8143/:8144 两只留验实例，
> T-389.md §5 处置记录）；2026-09-02 04:08:34 conductor 会话两次自杀
> （全机 binflow-server 同秒 SIGTERM——审计 workflow 实例与他人 scratch
> 双中招，iteration-1193 / T-426.md §环境观察）。M15 拆票口径即按
> T-382/T-384/T-389 三起计（M15-SPLIT §1.2 T-424 AC2）。

- **唯一合法形态：按端口杀**——`lsof -ti :PORT | xargs kill`。端口是实例
  的唯一事实源：只杀自己起的那个实例，杀完 `lsof -nP -iTCP:PORT` 复核
  端口确已释放。
- **禁用宽模式串**：`pkill -f binflow-server` / `pkill -f "binflow-server
  serve"` 一类命令行子串匹配，在共享机上命中的是**所有人的实例**——
  留验实例（他人复验现场）、审计实例、用户自起实例全在杀伤半径内。
- 宽模式串还有静默漏杀的反向坑（T-111/T-239：`./binflow-server` 的
  argv[0] 不含路径子串，模式不命中还以为杀掉了）——端口法两个方向都
  诚实：不是自己的端口就不动，端口不在监听就报空。
- 自备实例起服后核对「listening」日志行 + lsof 端口占用再跑腿（T-111：
  `serve &` 的 bind 失败会被 `&` 吞掉）。

## 5. assert-tokens 闸门：属性选择器豁免口径（T-390）

> 背景：`npm run build` 的第一闸（web/scripts/assert-tokens.mjs）要求一切
> 色值走设计 token（`var(--bf-*)`），按行扫 `#hex` 字面量。T-390 落地
> 包型图标时撞出**盲区**（reports/agents/T-390.md §遗留-1）。

- pkg-icon.css 用无井号子串形属性选择器（`[stroke*='d70a53' i]`）承载
  **官方色匹配键**——语义是「定位哪个厂商图标」（选择器），不是「声明
  落什么色」（属性值）；落色恒为 `var(--bf-*)`。这类选择器内的 hex 字面
  量**豁免**于「裸色值」读法。
- 判别口径一句话：hex 出现在**声明的值位**（`color:` / `fill:` 等冒号后）
  → 必须token 化；出现在**选择器的匹配串里**（方括号内）→ 定位键，豁免。
- 闸门脚本本身未为该形态开显式规则（治理工具的放宽单独走票——T-390
  §遗留建议「属性选择器匹配键豁免」显式化，仍待票）；在新 css 撞红时
  先按本节口径判别，属豁免形的写法保持子串选择器形态，属声明形的必须
  改 token，不许靠挪位置绕闸。
- 相邻既有豁免：`color-mix(... var(--bf-*) ...)` 形脚本头注释明示放行
  （T-391 L1 修复的消费形态）。

## 6. 已知 flake 注记（两条——甄别口径：串行复跑 + 家族归因）

- **m9 N01 spec 级竞态**（T-384 §4.1，A/B 对照归因在案）：users/groups 页
  的页面级请求预算断言（N01，≤3 个数据 GET）可被**仪表盘惰性取数**
  （`/api/repositories` + `/api/v1/storage/stats`）污染——tracker 挂在登录
  页与目标页 goto 之间，仪表盘惰性取数恰落进这个窗口被计入目标页预算。
  约 1/3 轮现红（并行/串行皆可现，机器负载敏感），与被测 diff 无关
  （A/B 摘除票据 diff 后同签名复现）。
  处置：`--workers=1` 串行复跑甄别（6/6 绿即归本家族）；修复方向已登记
  （tracker 挂载时机 / 预算排除仪表盘路径），归 conductor 拆测试基建票。
- **m9 seed 并行互撞**（T-391 L-b）：角色用户 seed 的套件级 create-if-
  absent PUT 在并行 worker 间竞态——先到者 201、后到者 UNIQUE 冲突冒
  5xx（T-297 D-9 同族）。loginAs 的 converge() 重试在常态下吸收；CI
  `--workers=2` 理论可复现残留竞态。处置同上（串行甄别）；修法方向登记
  在案（撞 500 时重读 / spec 内用户名唯一化），属 QA 域。

> 以上两条均**在册**（非新发现）：全量跑撞红时先按家族甄别再定性，不要
> 把已知 flake 记成产品回归；运行口径与二进制新鲜度纪律见 `m8/README.md
> §1`，锚口径见 console-ux §10（anchor-audit 三方对账）。
