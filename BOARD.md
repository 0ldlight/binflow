# 任务看板（BOARD）

> 唯一事实来源。**只有主会话（conductor）可以写本文件**，所有 subagent 只读。
> ticket 由 tech-lead 生成、主会话录入。

## 票据格式

```
- **T-<编号>** [P0|P1|P2] 标题 `role:<agent类型>` `area:<目录/模块>` `dep:T-x,T-y`
  AC: ①可验证的验收标准 ②… ③…
```

- 优先级：P0 阻塞他人/当前里程碑关键路径；P1 本里程碑应完成；P2 可延后。
- `area` 是并行安全的关键：同一轮并行派发的 ticket，area 不得重叠。
- `dep` 列出必须先完成的 ticket。

## 📥 待办（todo)

（空——首轮迭代会先派 product-manager 产出 PRD，再由 tech-lead 分解出第一批 ticket）

## 🔨 进行中（doing）

（空）

## 👀 评审中（review）

（空）

## 🧪 测试中（qa）

（空）

## ✅ 已完成（done）

（空）

## 🚫 阻塞（blocked）

（空）
