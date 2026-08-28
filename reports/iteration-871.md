# Sprint 871 迭代报告 — T-339 收口（copy/move 核心，PR #30）；CI #35/#36 双绿（钥匙修复实证）；M12 8/25

**日期**: 2026-08-29 01:15
**上轮**: Sprint 870（00:46）

## T-339 → done（PR #30，develop=`058d630`）

五段管线 + messages[] + 401 override + **第 16 槽 pro 门控**（Q4 照搬）；22+5 用例 + 万节点树零 5xx + 真 curl 双形态；自擒两真缺陷。遗留：Observer 适配器接线票/trash 豁免 seam（T-345 消费）/镜像 manifest 漂移。

## CI 修复实证

**build #35、#36 双绿**——`UAT_SSH_KEY_B64` 主路径全链通过，deploy 稳定。钥匙链修复闭环确认。

## 状态

M12：**8/25**。在途 ×0。HEAD[develop]=`058d630` 已推双远端；main=UAT 稳定部署态。下一批：T-343（归档族，dep T-335 ✓）+ T-345（Trash BE）或 D-F/T-340 宽度补位。
