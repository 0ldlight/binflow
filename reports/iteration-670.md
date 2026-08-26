# Sprint 670 迭代报告 — T-293 关账（`3fcd74f`），M10 文档层收口完毕

**日期**: 2026-08-26 10:44
**上轮**: Sprint 669

## T-293 关账链

- 11/11 分歧处置（9 终裁落档 + T-287 七裁定全维持）；K23~K29 校准回写表 7/7
- **ADR-0034 新增**：五协议管理面统一 `/binflow/api/<proto>/` 走 dispatchAPI 族 + 对外绝对 URL 统一 `server.base_url`（M11 拆票直接依赖的横切定案）
- PRD → **v1.1 as-built 收口稿**；ADR-0032/0033 as-built 附段；architecture §7.1/§11/§12/§15 路由表与契约补齐；api-reference POST 陈旧行删除（conductor 复验 grep 为空）
- **conductor 三终裁**：T-280 免补票（合规路径 + M11 随票补）；PRD 转正随 T-297；公钥覆盖仅记录
- 提交 `3fcd74f`（9 文件点名，剔除 T-294 在途的 internal/auth + main.go + adapter/cargo）→ 双远端推送

## 在途 ×1

T-294（Cargo adapter——范围比预期深：internal/auth 裸 token 臂〔cargo 官方 wire 形态，`Authorization: <token>` 无 scheme，probed live cargo 1.98〕+ main.go 接线 + adapter/cargo 包）。

**M10：16/21 done。** 剩余：T-294（在途）→ B9（T-295 部署接线 ∥ T-296 文档五项）→ T-297 终验。

## 下轮计划

T-294 收口（真实客户端 L-r1~L-r6 + 门控全链 + 三方 cksum 对账复验）→ B9 派发。宽度空位 1：T-296（文档五项，tech-writer）可与 T-294 并行——等 T-294 剩余工作量明朗后决定是否提前穿插。
