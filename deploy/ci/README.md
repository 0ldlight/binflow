# deploy/ci — BinFlow 持续部署链（T-298）

用户指令（2026-08-25）：后续每一次变更经 Jenkins 持续部署到 `172.16.58.129`
作为测试环境；docs 站随同一链路部署。

## 链路形态

```
macOS main ──git push──▶ VM bare repo /home/lzw/binflow.git (main)
                              │ pollSCM ≤1min
                              ▼
                      binflow-ci-smoke  (go build + lint + 冒烟测试, ~21s 热)
                              │ upstream SUCCESS 才放行（红 smoke 不部署）
                              ▼
                      binflow-deploy    (本目录配置, ~2min 热 / 7min 冷)
                        ├─ checkout bare repo main
                        ├─ make console      （SPA 嵌入）
                        ├─ make docs         （Docusaurus 站嵌入——docs 同链）
                        ├─ make build + 版本注入 v1.0.0-ci.<sha>
                        ├─ 宿主 deploy-vm.sh deploy：备份→停→换二进制→起
                        │   →就绪探针(/binflow/api/system/ping, 60s 有界)
                        │   →失败自动回滚旧二进制并复探（exit 42=已回滚）
                        └─ 部署烟测：ping+版本断言+建仓+上传+下载+删除
                            +docs 面断言；失败→显式 rollback→job 红
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
