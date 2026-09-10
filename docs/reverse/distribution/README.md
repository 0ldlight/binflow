# docs/reverse/distribution/ — Artifactory 发行包（distribution）逆向规格

> ARTIFACTORY FULL REIMPLEMENTATION PROGRAM · Phase 0 全量审计 任务 #3（宪章 §13：安装包 distribution 映射）。
> 生成：2026-09-11 · reverse-engineer（distribution 域实例）。

## 这个目录回答什么问题

「Artifactory 作为一个**可安装/可运维的发行物** behaves how」——不涉及任何业务 API 行为（那归 rest-api.md / 协议规格族），只覆盖：
安装包长什么样（app/var 两分区）、配置如何被解析与生成（system.yaml 链路）、
一个节点如何被拉起/停掉/重启（15 进程编排）、升级与 6.x 迁移搬什么、备份恢复的包内事实。

## 阅读地图

| 文件 | 内容 | 性质 |
|---|---|---|
| [behavior.md](./behavior.md) | **主产出**：九动词行为规格（install/configure/start/stop/restart/upgrade/rollback/backup/restore），逐条置信度 + 证据等级 + 待验证清单（8 项 UNKNOWN） | clean-room 行为规格（「当…时…」句式） |
| [layout.yaml](./layout.yaml) | 目录树 evidence index：app/ 23 目录逐服务载荷形态、misc/third-party/doc、var/{etc,data,log,work,backup,bootstrap}、docker 镜像层资产；每节点标 E1–E4 与来源版本 | **evidence-index-only**（非设计输入） |
| [evidence/](./evidence/) | 关键脚本/配置原样摘录 6 件，每件头部标来源路径与版本 | 原始证据 |

## evidence/ 清单

| 摘录件 | 来源 | 等级 |
|---|---|---|
| C-artifactory.default.sh | C 包 app/bin/artifactory.default（全文） | E2 |
| C-artifactory.service | C 包 app/misc/service/artifactory.service（全文） | E2 |
| C-migrationComposeInfo.yaml | C 包 app/bin/migrationComposeInfo.yaml（全文） | E2 |
| C-startup-actions-excerpt.txt | C 包 app/bin/artifactory.sh 关键节（带原行号） | E2 |
| C-service-enablement-defaults.txt | C 包 app/bin/artifactoryCommon.sh run* 函数族摘编 | E2 |
| C-diagnostics-ports.txt | C 包 app/bin/diagnostics/diagnostics.yaml 摘编（端口表+ulimit） | E2 |
| B-entrypoint-artifactory.sh | B 容器 /entrypoint-artifactory.sh（全文） | E4 |
| B-system.yaml.runtime.yaml | B 容器 var/etc/system.yaml（脱敏） | E4 |

## 证据源与版本偏斜

- 证据优先序（宪章）：**B 运行时 (7.161.20) > A 反编译 (7.161.24) > C 安装包 (7.161.16)**。
- B：docker 容器 `artifactory`（jfrog/artifactory-pro:7.161.20，只读 exec 取证；引导期一手实录见 reports/artifactory-full-audit.md 附录 A）。
- C：/Users/lzw/Downloads/artifactory-pro-7.161.16（zip 发行包 4.0G，只读扫描，未解压大文件）。
- A：反编译源仅做抽验（master.key 生成方/加密链路），未发现与 B/C 的结构性冲突。
- 三源均属 7.161.x 相邻补丁版；未发现版本间结构差异点。

## 核心结论速览（详见 behavior.md）

1. **两分区铁律**：app（代码，升级整体替换）/ var（状态，升级保留）是全部九动词的承重结构。
2. **配置解析序**：env（JF_ 大写下划线变换）> var/etc/system.yaml > 调用方缺省；模板每次启动强制刷新但 system.yaml 永不覆盖。
3. **密钥链**：access 首启生成 master.key（router 等待上限 5 分钟）；敏感值首读后以 aesgcm256 密文回写 system.yaml。
4. **服务编排**：jfconfig → access（先行）→ router 依赖组 + 12 服务非依赖组；enablement 缺省 12 true / 5 false / 1 条件，B 进程表逐一吻合。
5. **载荷四形态**：Tomcat war（artifactory/access）、SpringBoot war 启动器（jfconfig/topology）、SpringBoot jar（jfbus:8057）、Go 二进制（12 个）+ node（frontend）。
6. **升级/回退真相**：包内无 versioned 目录换装（UPG-1 UNKNOWN 转 helm/deb/rpm 取证）、无自动 rollback、无冷备/restore 工具——发行级运维靠 var 分区保全 + misc/db SQL。

## 下游消费建议

- compatibility-engineer：§2（configure）与 §3.6（端口表）适合优先契约化（BinFlow 单二进制部署模型与 15 服务模型的映射裁定需 architect 参与）。
- 任务 #6（configuration-map）：layout.yaml 的 var/etc 子树 + behavior.md §2 可直接派生。
- 任务 #4（日志体系）：console.log 分流规则 START-1 留了补扫入口。
