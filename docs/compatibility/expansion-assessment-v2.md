# 契约扩面评估 v2——第五域候选（LOOP 017 / L017-3，2026-09-13）

- 已开四域 62 条目：docker-remote 25 / npm 17 / pypi 10 / storage-admin 10（各域均走「证据→契约→差分→翻绿→matrix 行微分」全环）。
- 本页目标：一页可裁决——七候选三轴评分 + 推荐序。只评估不开工。

## 三轴总表

| 候选 | ① 规格厚薄（docs/reverse/） | ② 差分密度预估（wire 面宽 × 客户端腿） | ③ 实现缺口（契约/差分覆盖缺口） | 结论 |
|---|---|---|---|---|
| **conan** | **厚**：216 行/12 置信标记/30 端点行；双源纪律完整（官方 RREV/PREV 文档 + GitLab v2 de-facto 规范 + DE 补白逐条标注）；**真机实证存量**（conan 1.66 v1 十七端点全链 + conan 2.31.2 remote login——T-308/T-312 活体在案） | **高**：v1+v2 双端点族（修订链/握手认证三端点双前缀/能力响应头/upload_urls 绝对 URL 自知 base/文件面）——候选中未契约化 wire 面最宽；客户端腿现成（conan 1.x/2.x 双版本矩阵已有先例） | **零实现缺口**：adapter 在册 + 重索引 conan ×2 已 ✅（D12-R09）——纯契约化补背书，与 npm 域同构（认证族→包面→深水区分票路径可复制） | **推荐第五域** |
| goproxy | 厚：301 行/**23 置信标记（候选最高标记密度）** | 中：端点族窄（?go-get= 探测/list/latest/@v 五件套/download cache）；go CLI 腿零安装成本 | 零实现缺口（D12-R11 已 ✅/high）——契约化是纯背书增量 | 次选（可与 conan 并行的小票：面小、置信高、最快闭环——goproxy 版本化协议 quirks〔info 与 mod 版本串形态〕是唯二看点） |
| helmoci | 厚（335 行——含 C14 负缓存语义随 L005 更新）但 **/v2 面与 docker 域单码径共享**（L008-2：helmoci 经 docker /v2 plane 自动覆盖，测试同跑） | **低-中**：新增面仅 chart 特有语义（tag 严格=semver/.prov layer/config·layer media types）+ 经典 index.yaml 仓面（helm.md §1.1——独立简单 HTTP 面） | 增量价值最低——docker-remote 25 条目已背书其绝大部分 wire | 后段（经典 index.yaml 面可随 helm 域小票单独契约化） |
| debian | 中：239 行/6 标记/**无 live 层**；官方 DebianRepository/Format 锚扎实，DE 补白含 By-Hash 三档/GPG 签名链/trivial 布局 | 中：apt 腿 + Release/InRelease/PGP 签名深水（**签名面或含产品裁定义**——BinFlow 是否承签命 PGP 链是裁定项非差分项） | 零实现缺口（deb 重索引 ✅）；PGP 面前置裁定未定 | 后段（签名裁定先行否则差分面被切走一半） |
| rpm | 中：247 行/9 标记/无 live | 中：dnf/yum 腿 + metadata XML/checksum 族（与 debian 同族深浅） | 零实现缺口（yum 重索引 ✅ 同族） | 后段（与 debian 可捆绑评估为「系 rpm 系」一波） |
| nuget | 厚：283 行/**6 live 标记**（dotnet 8.x live 验证在案）；v3 五端点族官方规范完备 | **高但最重**：service index/SearchQueryService/registrations/flatcontainer 全 JSON facets 面——候选中 wire 面最大；dotnet 腿重 | **有实现缺口**：重索引 nuget 在 D12-R10 缺位清单（calc 面未实现）；flatcontainer 直推面系 BinFlow 特有增量锚（K59——Artifactory 无此面，对拍基线特殊） | 后段（面最大+缺 calc+特殊锚三因素叠加，宜独立成域排最后或第二波） |
| cargo | 中：236 行/7 标记/无 live | 中-高：index 两形态（git/稀疏 config.json）+ .crate 下载 + 异步 publish（cargo 1.68+ 202 语义） | 有实现缺口：重索引 cargo 在 D12-R10 缺位清单 | 后段 |

## 推荐序与理由

1. **conan（第五域）**——一句话：**未契约化 wire 面最宽 × 规格双源最厚 × 真机实证存量 × 零实现缺口**，四因素同频；且 v1+v2/握手认证/修订链结构与 npm 域三段式（认证族→包面→深水面）完全同构，模式复制成本最低。
2. **goproxy（并行小票）**——置信标记密度最高 + 端点族最窄 + D12-R11 已 ✅：一票两三条目即闭环，适合与 conan 首票同 LOOP 搭载（差分密度互补：宽面重票 + 窄面轻票）。
3. 后段序：helmoci 经典 index.yaml 面（小）→ debian+rpm 系（PGP 裁定前置）→ cargo → nuget（面最大，独立域排期）。

## 首票建议（若裁 conan）

沿用 LOOP 011 评估的四段闭环：probe 双端拍握手三端点双前缀 + 能力头（conan 2.31.2 腿）→ 契约首文件（握手族 3-5 条目——镜像 npm session 群打法，规格零补证区）→ 差分（curl 矩阵 + conan CLI 双版本腿）→ matrix 新行提案（conan 主面现无独立行——D12-R09 仅重索引行；冻结纪律走新行）。
