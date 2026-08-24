# git 历史瘦身执行手册（T-269）

> **状态：DRY-RUN 已验证，尚未执行。**
> 本手册由 T-269 产出；全部数据来自 2026-08-24 在 `/tmp` 试验场的实测（主仓零改动、远端零触碰）。
> 任何 force-push 动作前，必须获得用户显式授权（见【用户授权点】标注）。

---

## 0. 事故对象（截至 main@c89e07d 实测）

全历史仅存在 3 个 >1MiB 的 blob（`git rev-list --objects --all` 全量扫描）：

| blob | 原始大小 | 打包后 | 路径 | 引入 commit | 消除 commit | 含它的 tag |
|---|---|---|---|---|---|---|
| `1b610a24` | 70.18 MiB (73,587,119 B) | 0.54 MiB | `BOARD.md` | `5c34194` M7 closure（2026-08-23） | `047e789` 同日修复 | 仅 m8-done |
| `2117712f` | 52.75 MiB (55,314,725 B) | 0.46 MiB | `BOARD.md` | `9a44168` M8 closure（2026-08-24，即 m8-done 指向的 commit） | `663721f` M9 planning 修复 | 仅 m8-done |
| `84991477` | 21.16 MiB (22,190,720 B) | 11.24 MiB | `binflow-server`（误提交的二进制） | `679dcc9`（2026-08-21） | `bb04e12` 次一 commit 删除并 gitignore | m5/m6/m7/m8-done |

口径修正（与票面"52+55+70 三个 BOARD blob"的差异）：
- 实际只有 **2 个** BOARD 巨 blob：70.18 MiB（=73.6 MB 十进制，修复 commit 自述"73MB"）与 52.75 MiB（=55.3 MB 十进制）。票面"52MB/55MB"是同一对象的两种进位口径。
- 第三个大对象不是 BOARD.md，是误提交的 `binflow-server` 二进制；**打包体积的大头是它**（11.24 MiB，占打包历史的 57%）。两个 BOARD 巨 blob 因文本高度重复，打包后合计仅 ~1 MiB——但 GitHub 的 >50MB 告警、`cat-file`/checkout 内存峰值、`rev-list --objects` 扫描成本看的是**原始 144 MB**。
- **m7-done 不含** M7 巨 blob：损坏发生在打 tag 之后、修复后才进入 m8 线。两个 BOARD 巨 blob 只存在于 m8-done 的历史中。

## 1. dry-run 实测收益（/tmp 试验场，三种变体均验证）

| 指标 | 瘦身前 | 瘦身后 | 变化 |
|---|---|---|---|
| `.git` 磁盘占用（du） | 70 MB | 8.0–8.5 MB | **−89%** |
| 打包历史（size-pack） | 19.88 MiB | 7.64–7.71 MiB | **−62%** |
| fresh clone 体积 | ~26 MB（打包）| 8.0 MB | **−69%** |
| >50MB GitHub 告警对象 | 3 | 0 | 全消 |
| 全历史最大 blob | 73.6 MB | 0.67 MiB（`docs-site/package-lock.json`） | — |
| HEAD 树（tree sha） | `a11ed58` | `a11ed58`（逐字节一致） | 不变 |
| HEAD 的 BOARD.md | 156,331 B | 156,331 B（sha `371ba08f` 一致） | 不变 |

辅助事实：工作树磁盘 1.03 GB 主要是 docs-site 构建产物（非历史问题）；tracked checkout 仅 16.3 MB / 1710 文件。本地另有 216 个不可达对象（5.04 MiB，最大 6.4 MB）属 reflog 残留，re-clone 后自然消失。

## 2. 方案选型

工具：`git filter-repo`（本机 brew 安装 2.47.0；无 `--strip-blobs-biggest` 选项，只有 `--strip-blobs-bigger-than` / `--strip-blobs-with-ids`）。

| 变体 | 命令核心 | 结果（实测） | 评价 |
|---|---|---|---|
| A. 阈值剥离 | `--strip-blobs-bigger-than 10M` | 832→830 commit：M7/M8 两个 closure commit 被整支剪除（剥离后成空 commit，默认 `--prune-empty=auto`）；**m8-done 会指向无关的 a11y fix commit**（`f7c85ed`） | 简单，但丢失里程碑 commit，tag 语义漂移 |
| B. 点名剥离 + 保留空 commit（**推荐**） | `--strip-blobs-with-ids <file>` + `--prune-empty never` | 832 commit 全保留（closure commit 以重写形态存在，message 不变）；m8-done 正确指向重写后的 M8 closure commit | 手术式：精确只剥 3 个 blob，commit 结构与 tag 语义完整 |

> 不采用 `--path BOARD.md --invert-paths`：会把 BOARD.md 全历史删除，看板演进记录（每次改动的 diff）全部丢失，且当前 BOARD.md 也会被剥掉，需要额外补回。收益与方案 B 相同（两个 BOARD 巨 blob 打包后仅 ~1 MiB），代价不可接受。

## 3. 前置条件（checklist，全部满足才可进入第 4 节）

1. **【用户授权点 #1】用户显式批准执行本手册的 force-push 序列。** 授权前只允许做第 3.1–3.3 节的只读准备。
2. **全员告知**：conductor 在团队渠道公告维护窗口（建议 ≥2 小时），窗口内禁止 push；窗口内各 agent 的 commit 由主会话暂存本地。
3. **推平差异**：确认本地与远端一致（执行时 `git status` 无未推 commit；dry-run 时本地领先 origin 1 commit 属正常滚动）。
4. **备份（两份）**：
   - 本地 mirror：`git clone --mirror git@github.com:lzwzzy/binflow.git ~/backup/binflow-pre-slim-$(date +%Y%m%d%H%M).git`
   - **【用户授权点 #2】** 若要把备份 mirror 推到任何远端位置（含 vm），需单独授权；否则本地磁盘备份即满足回滚需求。
5. **CI 停跑窗口**：GitHub Actions 在窗口内 disable 触发（Settings → Actions → disable，或逐 workflow 暂停）。
6. **GitHub 保护规则预检**：若 main 开启了 "Restrict force pushes" 或 tag protection，需临时放开（执行完恢复）。执行前还应核对：是否存在 Releases 绑定 m5–m8 tag（会自动跟随新 tag，无需操作）、有无除 main 外的远端分支（本地未见；若有可能含被剥 blob，一并处理）。
7. **清理本地 worktree**：`/private/tmp/bf-triage` 共享主仓对象库。执行落地采用"re-clone 主仓"方案（第 4.5 节）可天然规避；若改走原地重写，必须先 `git worktree remove`。

## 4. 命令序列（推荐：方案 B）

```bash
# 4.1 工作副本（独立于主仓，绝不原地操作）
git clone --mirror git@github.com:lzwzzy/binflow.git /tmp/binflow-slim.git
cd /tmp/binflow-slim.git

# 4.2 点名三个事故 blob
cat > /tmp/binflow-big-blob-ids.txt <<'EOF'
1b610a24a18e8b664f98a1f86ab8d45aaf642a25
2117712f9ef826a50eb9d2c0bc41c64d7fd702df
8499147770bddc5528a7dee8a906cb20a044b27c
EOF

# 4.3 剥离重写（保留全部 commit；mirror 克隆上无需 --force）
git filter-repo --strip-blobs-with-ids /tmp/binflow-big-blob-ids.txt --prune-empty never

# 4.4 验证（全绿才继续；任何一项失败=停止，用第 6 节回滚不了也不需要——远端尚未动过）
git rev-list --count main                          # 期望 = 重写前 commit 数
git cat-file -e 1b610a24a18e8b664f98a1f86ab8d45aaf642a25 2>/dev/null && echo FAIL || echo OK
git cat-file -e 2117712f9ef826a50eb9d2c0bc41c64d7fd702df 2>/dev/null && echo FAIL || echo OK
git cat-file -e 8499147770bddc5528a7dee8a906cb20a044b27c 2>/dev/null && echo FAIL || echo OK
git show main:BOARD.md | shasum                     # 与重写前主仓 HEAD 的 BOARD.md 一致
git count-objects -vH | grep size-pack             # 期望 ~7–8 MiB
cat .git/filter-repo/ref-map                        # 记录归档：tag 新旧 sha 映射
cat .git/filter-repo/commit-map > /tmp/commit-map-$(date +%Y%m%d).txt   # 归档备用

# ——【用户授权点 #1 已获批准后，方可执行 4.5 起的远端操作】——

# 4.5 推回 GitHub（filter-repo 已移除 origin，重挂）
git remote add origin git@github.com:lzwzzy/binflow.git
git push --force origin main
git push --force origin 'refs/tags/*:refs/tags/*'   # m1–m4 的 sha 未变，推送为 no-op；m5–m8 被重写

# 4.6 vm 远端同步（其 main 是旧 main 的祖先，同样 force 更新）
git push --force vm main 'refs/tags/*:refs/tags/*'

# 4.7 主仓落地：re-clone（推荐，自动清掉本地 reflog 残留与 bf-triage 旧对象引用）
cd /tmp && git clone git@github.com:lzwzzy/binflow.git dev-center-new
# 人工比对 dev-center-new 与旧 /Users/lzw/dev-center 的未跟踪文件（docs-site 构建产物等），搬移后替换
```

## 5. 后果与影响面（dry-run 实测映射样本）

正式执行时以当次 `ref-map` 输出为准；以下为 2026-08-24 dry-run（方案 B 变体）实测样本：

- **commit**：832 个中前 531 个（2026-08-21 `679dcc9` 之前）sha 不变；其后 301 个全部重写。样本：`679dcc9→50b57ea`、`5c34194→5cf3f21`、`9a44168→da55ce3`、`c89e07d→d49ae71`。
- **tag**：

| tag | 旧（tag 对象 sha） | 新 | 说明 |
|---|---|---|---|
| m1-done … m4-done | — | **不变** | 目标 commit 早于 binflow-server 引入点 |
| m5-done | `cffde22` | `54f4ba8` | 重写（含 binflow-server blob） |
| m6-done | `7419919` | `5067a60` | 重写 |
| m7-done | `10372a8` | `33cc553` | 重写（仅因 binflow-server；本身不含 BOARD 巨 blob） |
| m8-done | `1bfbddf` | `6e36b0f` | 重写，peel 目标 = 重写后的 M8 closure commit `da55ce3` |

- **所有协作者**：窗口结束后必须 re-clone；或 `git fetch && git reset --hard origin/main` 后把未推工作 cherry-pick 回来（旧分支基址已不存在，禁止直接 `git pull` 产生合并巨怪）。
- **GitHub**：PR/issue 中引用旧 commit sha 的链接悬空（旧对象在服务端 GC 前仍可直链访问，但不再被任何 ref 引用，>50MB 告警随之消除）；分支/tag 保护规则临时放开后记得恢复；Actions 重新启用后 runner 全新 checkout，无需适配。
- **内容零损失**：当前工作树、全部文档、看板现状、832 个 commit 的 message 与结构均保留；仅 3 个事故 blob 从历史树中移除（`binflow-server` 路径在受影响历史 commit 中整体消失；两个 BOARD 巨态版本不再可 checkout，当前 BOARD.md 及其正常历史版本不受影响）。

## 6. 回滚

远端异常时，从第 3.4 节的备份 mirror 原样推回：

```bash
cd ~/backup/binflow-pre-slim-<timestamp>.git
git push --force origin 'refs/heads/*:refs/heads/*' 'refs/tags/*:refs/tags/*'
```

回滚后所有人再次 re-clone（历史 sha 二次变化）。回滚仅用于"新历史有内容缺失"这类实质故障；单纯体积不达预期不构成回滚理由。

## 7. 禁止事项

- 禁止在 `/Users/lzw/dev-center` 主仓原地跑 filter-repo（存在共享 worktree 与 reflog，风险不可控）。
- 禁止使用 `git filter-branch`（官方已弃用，慢且易留垃圾对象）。
- 禁止在授权范围外向任何远端（含 vm）push 备份或结果。
- 执行窗口外发现新的 >10MB 误提交：立即 `git rm --cached` + 正常修复 commit；若已 push，扩充 ids 文件后重跑本手册（流程完全相同）。
