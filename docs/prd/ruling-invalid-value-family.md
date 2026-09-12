# 裁定素材：仓配置标量非法值三臂（rest/repo-config-invalid-value-family）

| 项 | 值 |
|---|---|
| 状态 | **素材（非裁定）**——L007-3 architect 整理，供 product-manager 折入 pending-rulings（建议编号 R-10）与 authority 终裁 |
| 台账锚 | `docs/compatibility/known-divergence.yaml` → `rest/repo-config-invalid-value-family`（UNKNOWN，LOOP 007 限期） |
| 证据 | E4 活体逐字（reports/compatibility/L007-3-update-merge-evidence.md §6，2026-09-12 :8082）+ L006-a-b-diff.md §1.3 |
| 图例 | 「对齐案」=复刻参照 wire 行为；「维持案」=保留 BinFlow 现行姿态并登记 INTENTIONAL |

## 0. 共同背景

三臂同属仓配置写面（PUT/POST `/api/repositories/{key}`）的**输入合法性**问题。参照（Artifactory 7.161.20）自身姿态不均：String→Integer 转换失败答 **500**（服务端异常面），String→Boolean 不可转答 **400**，未知布局名答 **400**——即「对齐参照」并不等于得到一个统一的错误模型，反而要复刻它的**不一致**。客户端影响面事实：该面是**管理面**（admin 凭据），真实消费方是 curl/CI 脚本与 terraform provider 类工具，不是 docker/mvn/npm 等部署期客户端——后者永不触达仓配置 CRUD。400 vs 500 的行为差异主要在**重试语义**：规范 HTTP 客户端（含 terraform/大多数 SDK 默认）对 5xx 重试、对 4xx 不重试——复刻 500 会让确定性输入错误被客户端反复重试（本仓无 live 工具对拍记录，此为协议语义推理，据 RFC 9110 §15.5/15.6 重试语义）。

## 1. 臂一：mistyped 数字（`maxUniqueSnapshots:"seven"`）

- **现状对比**：参照 **500** `Error converting from 'String' to 'Integer' For input string: "seven"`（逐字）；BinFlow **400**（decode 报文带字段名——全域统一 decode 姿态，非本域特例）。
- **对齐案**：为「字符串填数字席位」复刻 500 + Java 转换器逐字报文。收益=差分断言逐字同；成本=① 仓配置族获得与全域其余面相反的错误码姿态（BinFlow 其余 decode 失败恒 400）；② 500 触发规范客户端重试确定性错误；③ 逐字报文耦合 Java 转换器实现细节（`For input string:` 是 JDK NumberFormatException 文案），脆弱且 clean-room 边缘（库癖性泄漏）。
- **维持案**：400 + 字段名报文，登记 INTENTIONAL_DIFFERENCE（authority=ADR）。收益=错误模型一致、可调试性好、无重试误导；成本=差分断言需 normalize 层（tools/difftest 已有 400/500 归一先例可挂）。
- **建议立场（architect）**：**维持 400，INTENTIONAL**。500 是参照的异常泄漏而非契约设计；两案客户端实际后果等价（均失败），唯重试语义 400 更正确。
- **影响面**：差分矩阵 D02 行 3/4 note 增 INTENTIONAL 注记；contracts/ 仓配置契约错误臂按 400 冻结。

## 2. 臂二：宽容布尔（`blackedOut:"yes"`）

- **现状对比**：参照**接受并转换**（实测 "yes"/"on"/"TRUE"→true、"off"→false 全 200 且落库回显；"maybe"→400 `Can't convert value 'maybe' to type class java.lang.Boolean` 逐字）——commons-lang `BooleanUtils` 真值集 {true,on,yes,y,t,1}/假值集 {false,off,no,n,f,0}（大小写不敏感，Apache Commons Lang 公开文档）；BinFlow **400**（严格 bool）。
- **对齐案**：bool 席位接受 commons-lang 真假值词表、静默转换。收益=照抄坏数据的脚本不炸；成本=① 静默改写输入（"yes"≠JSON true，数据完整性气味—— typo 被吞）；② 词表要逐词维护（含 'y'/'t' 单字母这类惊喜）；③ BinFlow 其余 bool 席位（全域）要么跟改要么双姿态。
- **维持案**：400，INTENTIONAL（Java 库癖性，非有意契约）。
- **建议立场（architect）**：**维持 400，INTENTIONAL**。受害脚本形态（用字符串填 bool）在 JSON API 消费方中属罕见病；宽容转换的静默性反噬可调试性。
- **影响面**：同臂一（matrix note + 契约错误臂冻结 400）。
- **附注**：若产品侧判断「照抄迁移脚本」场景真实存在，可折衷为**仅接受字符串 "true"/"false"**（JSON 惯用双拼写）——比 commons-lang 词表窄、比严格 bool 宽；本素材不预裁，列作备选。

## 3. 臂三：未知布局名（`repoLayoutRef:"no-such-layout"`）

- **现状对比**：参照 **400** `Unable to find repository layout by the name: <n>`（逐字，create 与 update 双面实测）；BinFlow **接受并存读**（无布局注册表；K73 已定谳布局为 presentation-only；参照实例自身 `/api/repo_layouts` 404）。
- **对齐案**：按**内置布局名冻结清单**（公开 xsd/官方文档的 26 内置布局名——matrix D02 行 12 已载）做存在性校验，未知名 400 逐字。收益=灭一个真实客户端可见分歧（typo 布局名在 BinFlow 静默存成无效配置，在参照被即刻拦截）；成本=① 维护一份冻结名单（闭集、低频变更）；② **迁移注意**：Artifactory 支持**自定义布局**，迁来的 configJSON 若引用自定义布局名会被 BinFlow 400——需随裁定明确（拒 or 白名单放行带 WARN），本素材不预裁。
- **维持案**：接受并存读（超集姿态），登记 INTENTIONAL/超集差。收益=零实现零维护；成本=静默无效配置（错名不报错）长期在账。
- **建议立场（architect）**：**对齐 400（按冻结名单校验）**，自定义布局名的迁移臂随裁定二选一（建议：名单外一律 400，与参照对未知名的拦截一致——BinFlow 无自定义布局能力，放行即造无效配置）。名单来源必须是官方 xsd/文档（clean-room），实现票内冻结进规格票。
- **影响面**：实现票一枚（校验+400 逐字+差分臂）；matrix D02 行 3/4；K73 的 presentation-only 定谳不受影响（本臂只裁**名字存在性**，不实现布局引擎）。

## 4. 折入建议（PM 动作）

- 建议并入 pending-rulings 为 **R-10**（三臂可一行一臂列状态），authority=产品（臂一/二建议 INTENTIONAL 登记）+产品/ADR（臂三建议开实现票）。
- 终裁后回写路径：known-divergence `rest/repo-config-invalid-value-family` 拆臂定分类（INTENTIONAL×2 + 实现票）→ contracts 仓配置契约错误臂冻结 → matrix D02 行 3/4 note。
- 与 update-merge ADR 候选稿（docs/design/repo-update-merge.md）同面不同账：merge 语义=省略字段的处理；本素材=**出现**的值非法。实现票可同轨不同 PR。
