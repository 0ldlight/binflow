# Sprint 806 迭代报告 — T-307R 收口（SAML 证书卡）；误伤事件处置；M11 27/32

**日期**: 2026-08-28 05:45
**上轮**: Sprint 805（05:26）

## T-307R → done（merge `1e630b0`）

- SAML Tab 证书卡（下载/regenerate 确认框/指纹 CopyButton/§3.3 感知重挂）+ 锚册 v1.13 两锚 + CFG8 腿；四闸门绿（conductor 抽验 tsc/lint/ledger）；指纹 openssl byte 级一致。
- **误伤事件**：T-307R 清理 pkill 过宽，杀掉 T-327 两个无证实例——已通报 T-327（根因+重启指引，勿记 FAIL）。

## 状态

M11：27/32（T-307R 为补票不计入 32 基数）。在途 ×1（T-327 回归）。HEAD[develop]=`1e630b0` 已推双远端；main=`8305e67`。
