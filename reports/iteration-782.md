# Sprint 782 迭代报告 — T-319 收口（GPG keypair 体系）；M11 19/32；签名腿解锁

**日期**: 2026-08-27 21:35
**上轮**: Sprint 781（21:06 T-323 派发）

## T-319 → done（merge `531b557`）

- **mini 规格三档出处**：L-A = JFrog 官方 REST 实时取证（9 端点 + schema 逐字）；**Q8 锚定新事实：Artifactory 官方面无 keygen 端点**——「暂行 RSA-4096」转正、生成端点落 BinFlow 自有管理面（兼容面 import-only）。
- internal/keypair（Manager/openpgp 力学/**签名 seam**）+ 迁移 016 + repo 六钩点 + httpapi 9 端点 + cmd 接线；**真 gpg 2.5.21 双向验签全过**；+0.35MB < 1.5MB。
- conductor 复验：build/vet/keypair 包（52s ok）全绿；当前 lint 红均为 T-323 在途 storage 测试文件（非本票域）。
- **T-321/T-322（debian/rpm 签名腿）依赖就绪**——T-323 收口后即派（B11 双票）。

## 状态

M11：**19/32**。在途 ×1（T-323 storage）。HEAD[develop]=`531b557` 已推双远端。
