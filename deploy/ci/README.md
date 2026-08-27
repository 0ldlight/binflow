# deploy/ci — BinFlow 持续部署链（T-298）

用户指令（2026-08-25）：后续每一次变更经 Jenkins 持续部署到 `172.16.58.129`
作为测试环境；docs 站随同一链路部署。
用户指令（2026-08-26 21:10）：CircleCI 流水线部署 `52.79.109.153` 作为 UAT
环境（docs 服务随二进制内嵌同链部署）。

## 链路形态

两条链（gitflow 映射，2026-08-26 起）：**develop → Jenkins/VM 测试环境**；
**main → CircleCI/UAT**（develop→main 的 release 合并触发 UAT 换装）。

```
macOS develop ──git push──▶ VM bare repo /home/lzw/binflow.git (develop)
                              │ pollSCM ≤1min
                              ▼
                      binflow-ci-smoke  (go build + lint + 冒烟测试, ~21s 热)
                              │ upstream SUCCESS 才放行（红 smoke 不部署）
                              ▼
                      binflow-deploy    (本目录配置, ~2min 热 / 7min 冷)
                        ├─ checkout bare repo develop
                        ├─ make console      （SPA 嵌入）
                        ├─ make docs         （Docusaurus 站嵌入——docs 同链）
                        ├─ make build + 版本注入 v1.0.0-ci.<sha>
                        ├─ 宿主 deploy-vm.sh deploy：备份→停→换二进制→起
                        │   →就绪探针(/binflow/api/system/ping, 60s 有界)
                        │   →失败自动回滚旧二进制并复探（exit 42=已回滚）
                        └─ 部署烟测：ping+版本断言+建仓+上传+下载+删除
                            +docs 面断言；失败→显式 rollback→job 红

macOS main（release 合并）──▶ CircleCI workflow `uat`（仅 main 过滤）
                              ▼
                      build: console + docs + server + vet/lint/-short
                             + 版本注入 uat.<sha>（T-325 与 Jenkins 对齐）
                              ▼
                      deploy_uat: uat-deploy.sh → ubuntu@52.79.109.153
                        ├─ scp 分阶换装（.new → 备份×5 → install 原子替换）
                        ├─ healthz 探针 60s 有界 + 失败自动回滚复探
                        └─ 烟测：healthz + 版本断言（uat.<sha>）
                            + /binflow/docs/ 200；失败 → job 红
```

- **FORM=release**（参数化手动跑）：从 BinFlow 自托管的
  `172.16.58.129:8080/docker-local/binflow:$TAG-alpine-amd64` 镜像里取
  goreleaser 已盖版本戳的二进制走同一换装/探针/烟测路径（dogfood）。
- **docs 形态选型**：嵌入二进制（go:embed，T-89/T-129）而非 VM nginx——
  CD 模型下每次变更本来就重建重启发二进制，嵌入使 code+console+docs
  单产物原子部署/原子回滚、零新增 VM 组件；论证全记录见
  `reports/agents/T-298.md` §2。

## 文件

| 文件 | 作用 |
|---|---|
| `binflow-deploy.groovy` | Jenkins job `binflow-deploy` 的 Pipeline 全文（作业内联） |
| `deploy-vm.sh` | VM 宿主侧部署引擎（status / deploy / rollback 三模式） |
| `uat-deploy.sh` | CircleCI 侧 UAT 部署脚本（scp 分阶换装/5 备份/60s 探针/自动回滚/版本+docs 烟测） |

## main → CircleCI → UAT（52.79.109.153）链

- 触发：`.circleci/config.yml`（workflow `uat`，仅 main 过滤）——build
  （console+docs+server 全量构建 + vet/lint/-short 测试 + **版本注入
  `uat.<sha>`**）→ deploy_uat（`uat-deploy.sh`，SSH 上船单二进制）。
- 密钥：RSA 私钥 `~/.ssh/binflow-uat.pem`（仅本机，600，**绝不入仓库**）；
  CircleCI Project Settings > SSH Keys 上传同钥，MD5 指纹填入 config.yml
  `add_ssh_keys.fingerprints`（`add_ssh_keys` 不支持 env 插值）。
- 环境变量（Project Settings）：`UAT_HOST`（默认 52.79.109.153）、
  `UAT_USER`（默认 ubuntu）、`UAT_HOME`（默认 /opt/binflow-uat）。
- 烟测门（T-325 起）：healthz + `/binflow/api/system/version` 断言含
  `uat.<sha>`（证明换装真发生）+ `/binflow/docs/` 200（docs 面）。

## UAT 服务器预置清单（重装时照做，一次性的 sudo 动作）

以下在目标机 `ubuntu@52.79.109.153` 上以 sudo 执行。目录约定：
`/opt/binflow-uat`（家）、`/opt/binflow-uat/data`（数据）、
`/opt/binflow-uat/binflow.yaml`（配置）、`/opt/binflow-uat/backups`（部署
脚本自建）。

### 1) 目录与权限

```bash
sudo mkdir -p /opt/binflow-uat/data
sudo chown -R ubuntu:ubuntu /opt/binflow-uat
```

### 2) systemd 单元 `/etc/systemd/system/binflow-uat.service`

```ini
[Unit]
Description=BinFlow UAT (CircleCI continuous deploy target)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
WorkingDirectory=/opt/binflow-uat
ExecStart=/opt/binflow-uat/binflow-server serve -c /opt/binflow-uat/binflow.yaml
Restart=on-failure
RestartSec=5s
KillSignal=SIGTERM
# 排空窗口：server.graceful_timeout_seconds 默认 30s，45s 留余量
TimeoutStopSec=45

# 加固（与 contrib/systemd/binflow.service 同族；ProtectSystem=strict 下
# /opt 需显式可写——数据/备份/换装都在这里）
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/opt/binflow-uat
PrivateTmp=yes
PrivateDevices=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
UMask=0027
LimitNOFILE=65536

# 可选 env 文件（承载 BINFLOW_ADMIN_PASSWORD 与实例主密钥
# BINFLOW_REMOTE_CREDENTIALS_KEY——root 属主 0600；M11 面）
EnvironmentFile=-/opt/binflow-uat/env

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable binflow-uat   # 先 enable 不 start：二进制由首次部署放置
```

### 3) 初始配置 `/opt/binflow-uat/binflow.yaml`

```yaml
server:
  listen: ":8080"
storage:
  data_dir: /opt/binflow-uat/data
```

（M11：如需 binstore.yaml 存储链，放 `/opt/binflow-uat/binstore.yaml`——
与 binflow.yaml 同目录即被自动发现；主密钥走上面的 env 文件。）

### 4) sudoers：CircleCI 部署腿所需的最小授权

`/etc/sudoers.d/binflow-uat-deploy`（visudo 语法；`uat-deploy.sh` 的远程
腿只做 systemctl restart/stop/start 与 install/mkdir/ls/cp/rm——都在
/opt/binflow-uat 内）：

```
ubuntu ALL=(root) NOPASSWD: /usr/bin/systemctl restart binflow-uat, /usr/bin/systemctl stop binflow-uat, /usr/bin/systemctl start binflow-uat, /usr/bin/systemctl show binflow-uat*
```

注意：`uat-deploy.sh` 的远程主体是 `sudo bash -s -- <home> <label>` 整段
执行（换装/备份/探针/回滚一体），上面这行覆盖 systemctl 面；若策略要求
bash 也白名单，加：

```
ubuntu ALL=(root) NOPASSWD: /usr/bin/bash
```

（收紧替代：把远程主体改成逐条 sudo 白名单命令——功能等价，改动归
release-engineer 票。）

### 5) 验证预置

```bash
sudo -l -U ubuntu          # 上述 NOPASSWD 行在列
systemctl cat binflow-uat  # 单元内容回显
```

首次由 CircleCI 部署（或手动 `bash deploy/ci/uat-deploy.sh <host> <user>
<home> <label>`，需本机 ~/.ssh/binflow-uat.pem）放置二进制并 start。

## 数据纪律（UAT）

- `uat-deploy.sh` 绝不触碰 `/opt/binflow-uat/data`：只换
  `/opt/binflow-uat/binflow-server`（分阶：scp 到 `binflow-server.new` →
  备份 → stop → `install` 原子替换 → start）。
- 备份：`/opt/binflow-uat/backups/binflow-server.<ts>`，保留最近 5 份。
- 回滚：探针失败（60s 有界）→ 自动回滚最新备份并复探；烟测失败 →
  显式非零退出（job 红）。

## VM 侧布置（本票已执行，重装时照做）

```bash
# 1) 宿主脚本落位（Jenkins agent 与 nsenter helper 都按宿主路径读它）
install -m 0755 deploy-vm.sh /srv/jenkins/t298/deploy-vm.sh
install -m 0755 deploy-vm.sh /home/lzw/t298/deploy-vm.sh   # 存档副本
mkdir -p /srv/jenkins/t298/incoming                        # 产物中转位
# 2) hostctl helper 镜像（alpine + util-linux 的 nsenter；chroot 连不上宿主 systemd bus）
docker build -t binflow-t298-hostctl:alpine - <<'EOF'
FROM docker.m.daocloud.io/library/alpine:3
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.tuna.tsinghua.edu.cn|g' /etc/apk/repositories \
 && apk add --no-cache util-linux
EOF
# 3) Jenkins job：走 T-248 的 /home/lzw/t248/push-jobs.sh 推送 groovy
bash /home/lzw/t248/push-jobs.sh binflow-deploy.groovy
# 4) 凭据 binflow-admin（部署烟测的建仓腿需要 admin；走 credential store，
#    重启安全块已幂等追加进 /srv/jenkins/init.groovy.d/t247-init.groovy）
```

## 数据纪律与回滚

- deploy-vm.sh **绝不触碰** `/var/lib/binflow`（数据目录）与
  `/etc/binflow`、unit 文件——只换 `/usr/local/bin/binflow-server`。
- M11 面（T-325 注）：实例主密钥与 binstore.yaml 属 operator-owned
  配置——分别落在 VM 的 `/etc/binflow/env`（`BINFLOW_REMOTE_CREDENTIALS_KEY`）
  与 `/etc/binflow/binstore.yaml`（如有），CD 链不管理、不覆盖；密封行
  存在后缺钥拒启属预期 fail-fast（回滚路径同上）。
- 备份：`/srv/binflow-backups/binflow-server.<ts>`，保留最近 5 份；
  与 `/usr/local/bin` 同文件系统，最终替换是原子 rename。
- 审计：`/srv/binflow-backups/deploy.log`（每次部署前后 systemctl
  status + 版本回显；>1MB 自动截尾）。
- 回滚路径两条：探针失败 → 脚本内自动回滚（exit 42，job 红）；
  部署烟测失败 → Jenkins 显式调 `rollback` 模式（exit 43 = 回滚也失败，
  需要人）。

## 信任边界说明

Jenkins agent 容器本就挂着宿主 docker socket（等价宿主 root）；helper
容器（`--privileged --pid=host --network host` + nsenter 进 PID 1 命名空间）
没有引入新权限，只是把既有能力用在明处。deploy-vm.sh 是 conductor 入库的
受控脚本，VM 侧按上表落位。
