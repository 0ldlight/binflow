# Artifactory 发行包行为规格（distribution lifecycle · 九动词）

> ARTIFACTORY FULL REIMPLEMENTATION PROGRAM · Phase 0 任务 #3。
> 本文件是 clean-room 行为规格：句式一律「当…时，…」，不含实现私有结构。
> 配套索引：[layout.yaml](./layout.yaml)（目录树 evidence index，evidence-index-only）。
> 证据等级：E1 反编译（7.161.24）/ E2 安装包（7.161.16）/ E3 端点探测（7.161.20）/ E4 运行时容器实况（7.161.20）。
> 置信度：高 = 双源以上印证；中 = 单源；低 = 推断或不可证（入待验证清单）。
> 引导期一手 E4 证据：reports/artifactory-full-audit.md 附录 A（2026-09-10 空卷引导实录）。

---

## 1. install（安装）

### 1.1 zip/tar 安装（免注册形态）
- 当操作者解压发行包后直接执行主脚本且不带参数时，产品以前台模式启动，不要求 root，不注册 OS 服务；此时若 var 目录不存在则自动创建并校验可写。【E2 artifactory.sh testPermissions | 置信度 高（E2 脚本 + E4 容器同路径印证）】
- 当安装包被解压到任意目录时，产品根 = 解压目录（脚本自定位「脚本所在目录上两级」），默认根路径取 /opt/jfrog/artifactory。【E2 | 高】

### 1.2 注册为 OS 服务（installService.sh）
- 当以非 root 用户运行服务安装脚本时，安装终止并报错「仅 root 可安装为服务」。【E2 | 高】
- 当以 root 运行服务安装脚本（可带 用户名 组名 两参，缺省 artifactory:artifactory）时，依序执行：创建组（已存在则跳过）→ 创建系统账号（系统账号语义：无登录 shell、home 指向产品根、不建家目录）→ 把产品根与 Tomcat 路径写进默认环境文件 → 创建 app/run 目录 → 安装 OS 服务 → 拷贝 Tomcat setenv 脚本 → 准备 Tomcat 工作目录 → 对 app 全树与 var 递归变更属主 → 把 用户/组 写入 system.yaml 的 shared.user/shared.group（已存在不覆盖）→ 打印成功横幅（含「推荐外部 PostgreSQL」与非 PostgreSQL 需显式放行的提示）。【E2 installService.sh 全文 | 高】
- 当目标系统存在 systemd 时，安装 systemd 单元：拷贝模板单元文件到系统单元目录（非 SUSE 取 /lib/systemd/system/，SUSE 取 /usr/lib/systemd/system/），把单元中的启动/停止命令绝对路径改写为实际安装位置，然后 daemon-reload 并 enable；当系统无 systemd 时，退回 init.d：拷贝 init 脚本到 /etc/init.d/artifactory 并按发行版用 update-rc.d / chkconfig / rc3.d 软链 三级兜底注册自启。【E2 | 高】
- 当安装时已存在同名 systemd 单元或 init.d 脚本时，旧文件被改名为 *.disabled 保留而非删除；当产品正在运行时，安装前先执行停止。【E2 | 高】
- 当安装完成时，systemd 单元的行为参数为：Type=forking、异常退出 60 秒后自动重启、启动超时上限 300 秒、退出码 143（SIGTERM）视为成功、PID 文件 /var/run/artifactory.pid、启动/停止命令指向专用管理脚本。【E2 evidence/C-artifactory.service | 高】

### 1.3 卸载（uninstallService.sh）
- 当以 root 运行卸载脚本时，依序执行：停止运行中的服务 → 注销 OS 服务（systemd disable 并删单元文件 / init.d 三级兜底反注册）→ 把 var/etc、var/data、var/log 整体移动到 产品根/artifactory.backup.<日期.时间> 备份目录，随后删除 var/backup、var/bootstrap、var/work → 杀死产品账号全部进程并删除账号与组 → 删除 var/etc 残留、恢复 Tomcat 原生日录结构。【E2 uninstallService.sh 全文 | 高】
- 当 Tomcat lib 下存在 MySQL 连接器 jar 时，卸载时一并移入备份目录。【E2 | 高】

### 1.4 安装预检（diagnostics）
- 当执行发行包自带的诊断工具时，它按内置清单串行检查：9 个默认端口可绑定（见 §3.6 端口表，被占用即失败）、ulimit 进程数 ≥1024 与打开文件数 ≥32000、防火墙/iptables 对各端口的拦截；检查项端口值支持从 system.yaml 取值（缺省用清单内默认）。【E2 diagnostics.yaml + diagnosticsUtil | 高（端口表另经 E4 印证）】

---

## 2. configure（配置）

### 2.1 配置解析优先级（单一事实源）
- 当任何启动/服务脚本需要读取配置键时，解析顺序固定为：环境变量 > system.yaml（可由 JF_SYSTEM_YAML 指定多文件列表，缺省 var/etc/system.yaml）> 调用方给出的缺省值；三处皆无且无缺省时报错退出。【E2 systemYamlHelper | 高】
- 当用环境变量表达 yaml 键时，键名转换规则为：点与横线转下划线、全大写、加 JF_ 前缀——例如 shared.database.url 对应 JF_SHARED_DATABASE_URL。【E2 | 高】
- 当环境变量与 yaml 同键时，环境变量获胜并在日志打印「resolved <key> (<value>) from environment variable」；敏感键（password/joinKey/masterKey 等）在一切日志输出中以掩码替代。【E2 | 高】

### 2.2 system.yaml 生成链与模板机制
- 当首次启动且 var/etc/system.yaml 不存在时，启动脚本从 app 内置最小模板复制生成它（不覆盖语义）；此后每次启动，两个模板文件（全键注释版 1402 行 / 精简注释版）都会从 app 内置源强制覆盖刷新到 var/etc，以保证模板与安装版本一致，但 system.yaml 本身永不被动覆盖。【E2 syncEtc | 高（E4：容器 var/etc 三文件共存）】
- 当 6.x 时代的环境变量（DB_TYPE、DB_HOST、HA_NODE_ID、EXTRA_JAVA_OPTS 等约 26 个）在环境中存在时，主脚本在启动早期把它们转换写入 system.yaml 对应键（一次性迁移映射，此后以 yaml 为准）。【E2 ART_ENV_MAP | 高】

### 2.3 Docker 形态的配置注入
- 当容器启动时，入口脚本先把镜像内 /artifactory_bootstrap 目录内容不覆盖地复制进 var/etc/artifactory、/bootstrap 内容不覆盖地复制进 var/etc——这是把自定义配置/密钥烘焙进镜像或挂载进容器的官方通道。【E4 entrypoint | 高】
- 当数据库类型为 Oracle 时，入口脚本自动把产品内 libaio 目录前置进 shared.env.LD_LIBRARY_PATH。【E4 | 高】

### 2.4 密钥（secrets）引导
- 当 var/etc/security/master.key 在首次启动时不存在，由 access 服务自动生成（模板注释明示「product 在首启生成」；B 实测全新引导后该文件出现）。【E2 注释 + E4 实况 | 高】
- 当 access 尚未生成 master.key 时，依赖方（router 等 Go 服务）以固定节奏打 WARN「Master key is missing. Pending for N seconds with 5m0s timeout」等待，最长 5 分钟超时（附录 A 实录 Pending 215s/timeout 5m0s）。【E4 附录A | 高】
- 当 var/etc/security/join.key（及 jfconnect/jfmelt 的 bootstrap keys 目录）需要预置时，操作者把自定义密钥放进 var/bootstrap 对应子目录（access 的自定义 join.key、两服务的自定义 keys），启动脚本确保这些目录存在并把资产投放到生效位。【E2+ E4 | 高】
- 当敏感配置值（如数据库密码）以明文写入 system.yaml 后首次被服务读取时，会被加密回写：文件中出现「<短前缀>.aesgcm256.<密文>」形态的值；解密依赖 master.key。B 实测全新引导实例的 system.yaml 中数据库密码即为该密文形态。【E4 system.yaml 实况 + E1（access 侧 AES 加密包裹服务存在）| 高】
- 当 system.yaml 的 <service>.env 列表被配置时，其中每个「KEY=VALUE」在对应服务启动时导出为环境变量；以 JF_ 开头的条目被拒绝并告警「请改用 yaml 路径」。【E2 exportEnv | 高】

### 2.5 服务启停开关与拓扑拼装
- 当服务编排器决定某服务是否纳入管理时，逐服务查询 <service>.enabled（解析序同 §2.1），缺省值见下表；运行 B 实例进程集合与该表完全吻合。【E2 + E4 | 高】

| 服务 | enabled 缺省 | 备注 |
|---|---|---|
| router_service / metadata / frontend / observability / event / evidence / onemodel / jfconfig / jfbus / topology / jfconnect_service | true | B 全部在跑 |
| access | （无 yaml 键）环境变量 JF_ACCESS_ENABLED ≠ "false" 即启用 | B 在跑 |
| jfmelt | true | 额外要求 jfbus 与 jfconnect 同时启用，否则打 WARN 不启动 |
| rtfs / platformFederation / apptrust / unifiedpolicy / evaluation / mc | false | B 均无进程；rtfs 载荷不在包内 |
- 当拓扑服务（topology）启用时，编排器同时把「access 使用外部拓扑」标志置真并把它计入路由必备服务类型；停用 topology 时走外部拓扑更新分支。【E2 setRouterTopology | 高】
- 当编排器拼装本节点必备服务类型时，基线为 jfrt,jfac,jfmd,jffe,jfob,jfcfg，再按上表逐个条件追加（jfcon/jfevt/jfevd/jftpl/jfomr/jfbus/jfmelt/jfpfed/jfapp/jfup/jfevl）；B 实例路由健康端点返回的 13 项必备类型与该拼装结果逐项一致。【E2 + E3 | 高】

### 2.6 数据库约束
- 当使用 PostgreSQL 以外的数据库（Derby/MySQL/Oracle/MSSQL/MariaDB）时，必须显式把「允许非 PostgreSQL」键置 true，否则安装横幅与配置模板均声明不支持；该约束在脚本层不阻断启动（由应用层执行）。【E2 | 中（应用层阻断行为未单独取证）】
- 当 rtfs/unifiedpolicy/evaluation 任一启用而共享数据库类型不是 postgresql 时，启动在脚本层直接报错退出（错误文案含「仅支持 PostgreSQL，请禁用该服务或改库」）。rtfs 若单独配置了自有数据库，其类型同样必须是 postgresql。【E2 databaseChecks | 高】
- 当容器形态启动且节点为主节点、数据库为外部库（非 Derby）时，入口脚本固定等待 30 秒再继续（给 compose 场景的 DB 容器就绪时间；可用 SKIP_WAIT_FOR_EXTERNAL_DB=true 跳过）；次节点/Derby/无 URL 时跳过等待。【E2+E4 | 高】
- 当需要建外部库时，发行包 misc/db 提供 6 个建库 SQL（postgres/mysql/mariadb/mssql ×3 变体，含 blob 独立库与重建变体）。【E2 | 高】

### 2.7 其他配置行为
- 当 artifactory.extraConf / access.extraConf 指向某目录时，其内容在每次启动时被不覆盖地复制进 var/etc/artifactory 与 var/etc/access。【E2 | 高】
- 当 HA 模式开启（shared.node.haEnabled=true）时，启动脚本确保 haDataDir 与 haBackupDir 存在且可写；当 /tmp 下存在 art*.lic 文件时，自动复制为 var/etc/artifactory/artifactory.lic。【E2 setupHA | 高】
- 当 var/bootstrap/artifactory/java/java.security 存在时，它被复制到运行时 JVM 安全区并追加到 JVM 参数（双等号 properties 语义=整体替换）。【E2 | 高】

---

## 3. start（启动）

### 3.1 容器入口序（docker 形态）
- 当容器启动时，入口脚本依序执行：加载脚本库 → 磁盘空间检查（var 使用率 >90% 警告、>98% 中止；可跳过）→ （可选）6.x→7.x 迁移 → Oracle libaio 路径处理 → 外部 DB 等待（§2.6）→ bootstrap 配置复制（§2.3）→ 以后台方式 exec 主编排脚本并记录其 PID 到 var/work/run/artifactory.pid → 等待主脚本退出。【E4 entrypoint 全文 | 高】

### 3.2 主编排序（artifactory.sh）
- 当主编排脚本被调用时，启动前动作依序为：JDK 版本检查 → ulimit 检查（打开文件 ≥32000、进程 ≥1024）→ system.yaml 合法性校验 → 数据库连接预检 → var 权限测试 → etc 同步（§2.2）→ 节点详情（node id/ip）设置 → HA 设置 → extraConf 复制 → 追加 JVM 参数（含 FIPS 处理）→ 兼容性配置导入文件生成（仅在 AUTO_GEN_REPOS/ART_BASE_URL/ART_LICENSE 环境变量存在且文件未生成时；Pro 版必须同时给 ART_LICENSE 否则报错退出）→ 日志轮转注册 → 自定义证书进 Java 信任库 → Tomcat 工作目录准备 → server.xml 渲染 → java.security 引导 → jfconnect 自定义证书引导 → shared/artifactory 两节 env 导出 → 路由拓扑拼装 → 环境变量打印。【E2 startupActions | 高】
- 当主编排脚本开始拉起服务时，顺序为：jfconfig（若启用）→ access（若启用，前台后台挂起方式，其余服务等它先起）→ router →（非依赖组）metadata、frontend、observability 及其余启用者；随后等待默认 TLS 证书生成（仅当 https 连接器开启且未配置证书时，轮询 var/data/router/keys/server.crt+key，2 秒 × 120 次上限，超时报错退出），最后前台运行 artifactory Tomcat（容器形态下 console 流经 tee 同步转发到日志文件）。【E2 + E4 附录A（access 未就绪时下游全在重试）| 高】

### 3.3 单服务启动模式（按载荷形态）
- 当服务为 Go 二进制形态时（router/metadata/observability/event/evidence/onemodel/jfconnect/jfmelt 及默认关闭的 apptrust/unifiedpolicy/evaluation/platformFederation）：脚本以「PID 文件存在且进程活 → 转 restart；PID 在进程死 → 清 PID 后启动；干净 → 启动」三态判定；启动时先 cd var，二进制后台运行，PID 写 var/work/run/<服务>.pid；容器形态 console 流 tee 转发，非容器形态追加 var/log/console.log。【E2 metadata.sh 样板（各服务脚本同构）+ E4 | 高】
- 当服务为内嵌 Tomcat war 形态时（artifactory/access）：通过 Tomcat 启动脚本前台运行，JVM 参数来自默认环境文件（artifactory：-Xms512m -Xmx2g -XX:+UseG1GC + 一组 add-opens；access：内存百分比模式 MaxRAMPercentage=25 + CrashOnOutOfMemoryError——E4 进程命令行实证两者 JVM 参数集不同）。【E2 + E4 | 高】
- 当服务为 Spring Boot war 启动器形态时（jfconfig/topology）：java 以「运行时工作区 lib 目录优先 + app 内置 lib + war 本体」三元 classpath 内嵌启动，临时目录在 var/work/<服务>/tomcat/work——运行时工作区 lib 目录是 JDBC 驱动热插点（启动时会把 app 内置的 jf_* 驱动副本清出该目录）。【E2 脚本 + E4 进程命令行 | 高】
- 当服务为 Spring Boot jar 启动器形态时（jfbus）：同上热插机制，端口经 -Dserver.port 注入（缺省 8057）。【E2 + E4 | 高】
- 当服务为前端时：node 运行内置 bundle（B 实测 node v22.23.1）。【E4 | 高】

### 3.4 引导依赖与首启时序（E4 一手实录）
- 当空卷首次引导时，可观察的时序为：先出现数据库连接检查失败（DB 容器未就绪/配置未注入时为「Could not determine database type」）→ access 部署并生成 master.key → 其余 Go 服务对 access 内部地址持续重试（约百次量级）→ 11 个 war 在 Tomcat 内依序部署 → 路由聚合各服务 → 全程约 8 分钟后稳定（ping 200）。【E4 附录A 时间线 | 高】
- 当 master.key 长时间未生成时，router 等待 5 分钟超时；本实录中 access 最终完成部署、key 落盘、整体引导成功。【E4 | 高】

### 3.5 健康探测面
- 当客户端带管理员凭据 GET /router/api/v1/system/health 时，返回聚合健康：router 节点态（HEALTHY/消息）+ 本节点必备服务类型清单 + 每服务 service_id（形如 <类型>@<ULID>）与各自健康态。【E3 实测 | 高】
- 当客户端 GET /artifactory/api/system/ping（无凭据）时返回 200 空体——无鉴权 ping 可作存活探针。【E3 | 高】
- 当服务注册时，各服务向 router 内部入口（缺省 8046）注册自身路由与拓扑；服务间调用走内部入口，外部统一走 8082。【E2+E3 | 高】

### 3.6 默认端口表（安装预检 + 运行时实证合并）
| 端口 | 归属 | 证据 |
|---|---|---|
| 8082 | router 对外入口 | E2+E3+E4 |
| 8046 | router 内部（服务间）入口 | E2+E4 |
| 8049 | router traefik API | E2 |
| 8047 | router gRPC | E2 |
| 8081 | artifactory 直连（Tomcat） | E2+E4 |
| 8040 / 8045 | access HTTP / gRPC | E2 |
| 8086 | metadata | E2 |
| 8070 | frontend | E2 |
| 8057 | jfbus | E4+E2（不在预检表） |
| 8015 | artifactory Tomcat 管理端口（stop 探测用） | E2 |

---

## 4. stop（停止）

- 当主编排脚本执行 stop 时：先探测 Tomcat 管理端口（8015）判断是否在跑；未跑则直接判定「已停止」成功返回；在跑则调用 Tomcat shutdown 脚本，随后进入强制收割循环——2 秒后检查，10 秒未死发 kill，25 秒未死发 kill -9，30 秒为总上限；成功后删除 PID 文件、移除日志轮转注册，再对全部受管服务脚本逐一执行 stop。【E2 | 高】
- 当容器收到 SIGTERM/SIGINT/SIGHUP 时，入口脚本先对本机 artifactory 端口调用应用层预停端点（/artifactory/api/v1/system/preStop，实测无凭据也返回 200——应用完成排空后返回消息），再执行主编排 stop。【E4 entrypoint + E3 实测 | 高】
- 当单个 Go 服务执行 stop 时：kill 记录的 PID 并删 PID 文件；「PID 在但进程已死」时清 PID 文件后成功返回；未运行直接成功——停止幂等。【E2 metadata.sh 样板 | 高】
- 当 systemd 管理下主进程以 143（SIGTERM）退出时，单元把它记为成功退出。【E2 SuccessExitStatus=143 | 高】

## 5. restart（重启）

- 当主编排脚本收到 restart 时，语义严格为「先完整执行 stop，再完整执行 start」（顺序非并行）。【E2 case 分派 | 高】
- 当单服务脚本收到 restart 时，执行自身 stop → 固定 sleep 1 秒 → start。【E2 | 高】
- 当服务已在运行时收到 start，Go 服务族自动转 restart（见 §3.3 三态判定）。【E2 | 高】
- 当 systemd 管理下进程异常退出时，60 秒后自动拉起（Restart=always）。【E2 | 高】

## 6. upgrade（升级）

### 6.1 7.x → 7.x（补丁/次版本）
- 当在同一安装上升级 7.x 版本时，包内机制为「替换 app 分区、保留 var 分区」：全部代码载荷在 app 下、全部状态在 var 下，安装包不含任何 app 版本化目录或指向版本化目录的换装软链工具。版本指纹单独落在 app 顶层版本属性文件（四键：version/revision/timestamp/buildNumber）。【E2 全包扫描（无 versioned/symlink 资产）| 高（对 zip 形态）】
- 当升级走服务安装脚本时，安装器先停止运行中的服务再替换（§1.2）；systemd/init.d 旧单元改名为 .disabled 保留可回退痕迹。【E2 | 高】
- 当升级后首次启动时，etc 同步把两个 system.yaml 模板强制刷成新版（§2.2），而 system.yaml 本体不动——新版默认值对存量实例不生效，需手工对照模板迁移。【E2 推导自 syncEtc 语义 | 中】
- 当部署在 Helm/Deb/RPM 渠道时是否存在「版本化目录 + 软链原子换装」机制：**UNKNOWN**（本 zip 包与 docker 镜像内均无此资产；见待验证清单 UPG-1）。

### 6.2 6.x → 7.x（一次性数据迁移，migrate.sh）
- 当 docker/compose 形态设 ENABLE_MIGRATION=y 时，入口脚本在启动前触发迁移脚本，旧数据根固定为 /var/opt/jfrog/artifactory；迁移日志落 app/bin/migration.log 并在结束后归档到 var/log/migration.log；迁移失败则中止启动。zip/rpm 形态由各自安装器触发（映射表不同）。【E2 + E4 | 高】
- 当迁移执行时，步骤序固定 16 步：建迁移日志 → 校验旧版本 → 定位 system.yaml 路径 → 建必需目录 → 旧目录软链化 → 目录复制 → 目录搬移 → 配置文件归位（旧 db.properties / ha-node.properties / default 移入 etc/*/old 子目录留档）→ 属性文件迁移（旧 properties 键 → system.yaml 键，映射表随安装形态）→ XML 迁移（旧 server.xml 的端口/线程/连接器属性 → tomcat 相关键，仅 zip/rpm）→ default 文件迁移（仅 zip/rpm）→ yaml 迁移 → system.yaml 更新 → 旧数据目录清理（映射表列名目录）→ 旧文件清理 → node.id 注释化（改为按主机名/环境变量自动推导，文件内留说明注释）→ 收尾横幅；每步间有可暂停钩子。【E2 migrate.sh 主流程 | 高】
- 当迁移搬移目录时，按安装形态的映射表整树移动，docker/compose 形态核心映射：旧 etc → var/etc/artifactory 与 var/etc/access；旧 data → var/data/{artifactory,access}；旧 backup → var/backup/{artifactory,access}；旧 logs → var/log/archived/{artifactory,access}；旧 access/tmp → var/work/access；compose 形态额外把旧内嵌 postgresql → var/data/postgres/data；master.key 从旧 etc/security 复制到 var/etc/security。【E2 migrationComposeInfo.yaml | 高】
- 当迁移完成后，旧数据根中 access/backup/etc/data/metadata/logs 子树被清理（其余保留）。【E2 cleanUpOldDataDir | 高】
- 当迁移过程中存在 logback.xml 时，旧 etc 下的 access/artifactory logback.xml 被移入备份目录（docker 形态下 etc 为软链，另删软链根下的 logback.xml）。【E2 | 高】

## 7. rollback（回退）

- 当需要回退 7.x 版本时，发行包内**无自动回退工具**；可用的包内痕迹仅两处：卸载器生成的整目录时间戳备份（§1.3）与迁移器的步骤级暂停钩子+迁移日志。【E2 全包扫描 | 高（「无工具」为确证事实）】
- 当回退到 6.x（迁移后反悔）时：**UNKNOWN**——迁移对旧数据根做了搬移+清理，包内无反向迁移资产；官方知识库是否有 7→6 回退方案未取证（待验证清单 RBK-1）。
- 当升级失败需要回到旧 7.x 版本时：机制应为「恢复 app 分区 + var 不动」，但 var 中若有新版已写入的 schema/标记，旧版能否直接读：**UNKNOWN**（需版本回退实测，待验证清单 RBK-2）。

## 8. backup（备份）

- 当产品运行备份（应用层定时备份）时，备份目标目录为 var/backup（system.yaml 备份键指向；全新引导实例该目录即出现 access/ 与 artifactory/ 两空骨架）。此为应用层行为，发行包层面只提供目录骨架与配置键。【E4 | 高】
- 当操作者做发行级（冷备）备份时，所需最小集合为 var 分区（etc 含密钥与配置、data 含制品与 DB 内嵌库、log 可选）+ 外部数据库转储；发行包未内置冷备脚本，但 misc/db 的建库 SQL 是 DB 侧恢复的基础件。【E2+E4 | 中（最小集合为结构性推导，官方灾备文档未交叉取证）】
- 当卸载产品时，etc/data/log 三目录被自动移入时间戳备份目录（§1.3）——这是包内唯一的自动备份行为。【E2 | 高】
- 当需要备份密钥时：master.key/join.key 位于 var/etc/security（+两服务 bootstrap keys 目录），丢失 master.key 意味着 system.yaml 内 aesgcm256 密文不可解（§2.4 推论）。【E4+E2 | 中（不可解后果为加密语义推论，未做丢失复现实测）】

## 9. restore（恢复）

- 当需要恢复实例时，发行包内**无 restore 工具**；机制为「重建 var 分区 + 重建外部 DB + 转储回灌」。【E2 全包扫描 | 高（「无工具」为确证事实；恢复操作细节 UNKNOWN，见待验证清单 RST-1）】
- 当需要向新实例注入存量配置/密钥时，官方通道是 var/bootstrap（§2.4）与 docker 镜像的 /bootstrap、/artifactory_bootstrap 挂载位：启动时以不覆盖语义复制进 var/etc——restore 配置=把备份的 system.yaml/密钥放进这些位置。【E2+E4 | 高】
- 当内嵌 Derby 被使用且 var/data/artifactory/derby 存在而 var/data/access/derby 不存在时，启动链路自动把前者复制为后者（7 升 7 场景 access 库补建）。【E2 | 高】
- 当 postgres（compose 内嵌形态）数据需要恢复时，落位 var/data/postgres/data（与 6.x 迁移搬移目标一致）。【E2 | 中】

---

## 待验证清单（UNKNOWN / 低置信度）

| 编号 | 问题 | 为何未知 | 需要什么证据 |
|---|---|---|---|
| UPG-1 | Helm/Deb/RPM 渠道是否存在「版本化 app 目录 + 软链原子换装」升级机制 | zip 包与 docker 镜像内均无该资产；本任务证据源不含 helm chart 与 deb/rpm 包 | 取 artifactory helm chart（chart 内 immutable versioned dirname + symlink 段）或 deb/rpm 安装器脚本实物 |
| RBK-1 | 6.x→7.x 迁移后能否回退 6.x | 迁移器只有正向 16 步序与旧目录清理表，无反向资产 | JFrog 官方升级/回滚知识库文档或支持案例；或对迁移前快照做逆向实测 |
| RBK-2 | 跨补丁/次版本降级时 var 中新版 schema/标记与旧版代码的兼容行为 | 需要版本回退实验；本实例仅有一个运行版本 | 双版本实测：7.161.20 引导后换 7.161.16 app 分区观察启动结果 |
| RST-1 | 官方灾备恢复的精确步骤（var + DB + 密钥的最小恢复序） | 包内无 restore 工具；官方灾备文档未在本任务取证范围内 | JFrog 官方 backup/restore 文档（WebSearch 取证）或照官方步骤做一次 E4 实录 |
| CFG-1 | 非 PostgreSQL 库缺省时应用层的精确阻断行为（报错文案/时机） | 脚本层只做安装期提示与 rtfs/unifiedpolicy/evaluation 检查；通用阻断在应用层 | 用 Derby 显式配置引导一次（E4），或 A 源定位数据库类型校验逻辑 |
| LAY-1 | third-party/cosmo（npm CLI）的消费方与用途 | 包内只有 node_modules，无引用脚本 | 容器内全局搜 cosmo 调用点；或 A 源/前端构建产物引用检索 |
| LAY-2 | rtfs 载荷的分发渠道（包内仅脚本无二进制） | 发行包不含 rtfs 二进制 | JFrog 官方 rtfs/addon 文档；或 Pro+ 高级许可安装包对比 |
| START-1 | console.log 与各 <service>-console.log 的完整分流规则（哪些服务 tee、哪些直写） | metadata.sh 样板 + E4 console.log 存在已证，但逐服务差异未全数核对 | 对 13 个 pid 服务逐一比对脚本 redirectServiceLogsToFile 分支（E2 补扫） |

## 与公开规范的差异/补充说明

- 本文件全部条目来自包内脚本、容器实况与端点实测，属「官方安装文档未写的空白」（错误响应细节、等待超时、收割时序、端口表、密钥加密形态）——按角色纪律标注：以上均为**此条补充官方规范**。
- 官方文档层面的安装/升级流程（如 RPM 升级步骤、HA 安装）未在本任务取证；引用官方行为时应以 JFrog 官方文档为准，本文件不替代。
