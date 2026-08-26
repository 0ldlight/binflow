# Sprint 719 迭代报告 — UAT 环境开通首部署绿；B0 全清（gitflow 首航）；B1 双票派发

**日期**: 2026-08-26 21:25
**上轮**: Sprint 718

## UAT 环境落地（用户指令当日闭环）

- **服务器开通**（ubuntu@52.79.109.153，AWS Ubuntu 24.04）：/opt/binflow-uat + hardened systemd unit（ProtectSystem=strict）+ 数据目录
- **首部署实测**：21.3MB linux/amd64 单二进制（嵌入控制台 + 文档站，含 `7b84a71` 修复）——**/healthz 200、/binflow/docs/ 200、license 面 401 community 地板** ✅
- **CircleCI 链**：`.circleci/config.yml`（main 过滤：build → deploy_uat）+ `uat-deploy.sh`（分阶段换装/5 备份/60s 探针/自动回滚/双面烟测）已提交 develop
- **密钥纪律**：RSA 私钥仅存本机 ~/.ssh/binflow-uat.pem（600），**绝不入仓库**；待用户完成：CircleCI Project Settings > SSH Keys 上传同钥 + 指纹填入 config.yml 占位

## B0 全清（gitflow 首航）

- **T-301 → done**（`feature/T-301-adr-pack` → `4950675`）：ADR-0035/0036/0038 + openpgp = ProtonMail/go-crypto v1.4.1（三平台零 CGO）
- **T-302 → done**（`feature/T-302-auth-spec-refresh` → `103ffce`）：规格 v2 367 行、58 字段逐条双出处、两低置信区推翻、变更即生效高置信定案
- 两个 feature 分支 --no-ff 合入 develop——gitflow 流程首航成功

## B1 双票派发（21:22）

- **T-303** config-formats §1 复核 + 规格尾巴两处（reverse-engineer）
- **T-299** MUI 迁移批一（Login/壳层/仓库组，四闸门 + 交互零变化）

## 状态

M11：2/32（B0 全清）。在途 ×2（B1）。HEAD[develop]=`3da6dfe`。
