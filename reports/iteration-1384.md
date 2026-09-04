# Sprint 1384 迭代报告 — 矩阵⑤在跑：nuget 三层洋葱剥完（包版本→装配版本 UInt16）

**日期**: 2026-09-04 22:5x~23:0x
**上轮**: Sprint 1383（T-472 收编 + Fern 上线）

## 矩阵④裁定（run 33884189963 @2eca84f）

**9/10**：conan ✅（package() 补齐）+ helm ✅ + 八腿全绿。**nuget 进深一层**：NVER epoch 秒过了 NuGet 包版本门（Int32 ✅）→ 新卡点 **CS7034 装配版本**——派生 `AssemblyVersion=1.0.1788532310.0` 超其 **UInt16 件限**（major.minor.build.revision 每件 ≤65535，任何时间派生数都装不下）。

## ⑤修（`9b4fd0e`）

包身份保 `<Version>$NVER</Version>`；`<AssemblyVersion>1.0.0.0</AssemblyVersion><FileVersion>1.0.0.0</FileVersion>` 钉固定解耦（标准实践）。run 33884989165 在跑。

## nuget 洋葱全录（复现成本可溯）

SDK 10 吞错（MSB4181）→ SDK 8 后：①包版本 Int32 上限（14 位戳）→ ②装配版本 UInt16 上限（epoch 秒）→ ③预期终绿。

## 状态

M16: **24/35**；⑤裁定挂下轮——绿则终局合 main（复测循环收官）。
