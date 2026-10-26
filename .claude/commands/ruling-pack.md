---
description: 月度决策呈批——汇总 D 系决策与兼容裁定开放面，生成呈批材料供用户逐项拍板
---

生成当月呈批材料（$ARGUMENTS 可指定输出月份，省略=当月）。产物写入 `reports/ruling-pack-<YYYY-MM>.md`，文首必须注明「呈批材料——conductor 待用户裁定，非执行记录」：

1. **机器校验先行**：跑 `python3 tools/ledger-check/ledger_check.py`——对账数（Z/resolved/open 四分类）、簿记漂移清单、交叉引用直接取其输出，不手抄不重算。
2. **D 系决策盘点**：从 `docs/ai-engineering/loop-state.yaml` `pending_user` 与最近 intake 段（BOARD.md 只读）提取未裁 D 项；每项给 背景/选项（含已公布默认）/建议/影响面。已事实消解的项（如 D2 类基建清理已完成）给「销项」建议而非问题。
3. **裁定面结构分析**：open 条目按「真决策 / 簿记翻面 / 工程票在途」三归——INTENTIONAL/UNSUPPORTED gate 已载事实裁定的列为簿记，BUG 有票在途的列工程，其余 UNKNOWN 进呈批面。
4. **Top 15–20 排序**：按影响面排，每条一句话说清「差异是什么、两个方向、建议」；v2.x 在册席引 `docs/prd/pending-rulings.md` 原席号，v2 后新增未入席的单独标注。
5. **逾期标红**：席位/gate 文本中可考的限期（LOOP 0xx 限期、受理日期）距今 >30 天即标「逾期」——34 行级积压不允许静默滚存。
6. **末节「用户最少需要回答的 N 个问题」**：每项一行，含整包批复例外项与用户侧待办（凭据/OAuth/资源类）。

只读分析；不改台账/matrix 状态、不 commit。完成后向用户口头给：材料路径 + 各 D 项一句话 + 对账式（如 63=44+10+7+2）。
