# L019-3 两域轻差分批 — goproxy 六臂复跑 + conan PUT 补臂（双系统对照）

- 日期：2026-09-14（LOOP 019 / L019-3 / differential-qa-engineer）
- 模式：**dual**（A=Artifactory ref :8082 pro 7.161.20；B=BinFlow UAT :8083 `uat-l0181-74656514`，pro 档 license 在装——`243531c2…` 余 89 天，conan/go 包型门通过）
- 前置兑现：A 残留仓 `l0182-go-local`（L018-2 中止残留，空仓）开跑前 DELETE 200、列表复核 0 残留；双端 healthz/ping 200（ref 健康门）
- 纪律：全程串行；goproxy 六臂（s1 双端 + g1-g6 双端）+ conan 补臂 4（A 侧单边取证）；复跑门两轮结论稳定（§5）；凭据 env（`ARTI_AUTH`/`.env.uat`）；audit-probe 实例残留双端列表复核=0
- 入口脚本：`tools/difftest/l019/goproxy-endpoints-probe.sh`（上游 probe 复跑版，含一行 `set -u` 修复——§6 提案回灌）+ `tools/difftest/l019/conan-put-wire.sh`
- 证据：`reports/compatibility/l019-goproxy-wire/`（run1/run2 log + run2-bodies/ 双端原始体）+ `reports/compatibility/l019-conan-put/a/`（conan/*.hdr+body、setup/、arms.txt）；入仓全量 token 扫描（唯一命中=上游 probe 既存 A 侧明文凭据行，同现状非新增泄露；复跑版已参数化为 `ARTI_AUTH` 可覆写）

## 1. goproxy 六臂矩阵（fixture：`example.com/l018mod@v1.0.0` 三件套）

| 臂 | A（ref 7.161.20） | B（UAT 74656514） | 判定 |
|---|---|---|---|
| s1-put-zip/mod/info | **201×3**，CT=`application/vnd.org.jfrog.artifactory.storage.ItemCreated+json;charset=UTF-8`，0B 体 | **201×3**，无 CT 头，0B 体 | **状态面一致**（A 侧 PUT 成功码活体首证——`put-upload-triplet` unobserved 主面解除）；响应 CT 差=记录级（契约无 PUT 响应 CT 断言面；go 客户端域不消费） |
| g1-list | 200 text/plain；体 `v1.0.0 2026-09-13T16:40:42Z\n`（27B——**版本+时间戳列**） | 200 text/plain; charset=utf-8；体 `v1.0.0\n`（7B——纯版本列） | **差异=既有 INTENTIONAL 活体双证**（known-divergence#goproxy/list-timestamp-column；S7 反编译单源→活体确认）；normalize `goproxy.list_timestamp_column` 剥列后一致；CT charset 差记录级 |
| g2-info | 200 CT=`application/json+info`；体=fixture 逐字节 | 200 CT=`application/json`；体=fixture 逐字节 | **体一致（PUT 回读不重写——双端字节平价 A≡B≡fixture）**；CT=**新差异面**（§3-G1 契约修订提案族） |
| g3-latest | 200 CT=`application/json+info`；体与 .info 逐字节同 | 200 CT=`application/json`；同 | **体一致**（取胜出版本 .info 响应——契约 candidate 序语义双端同）；CT 同 G1 |
| g4-zip | 200 application/zip；`X-Checksum-Md5/Sha1/Sha256` 三头全出 | 同 CT 同三头，**三 checksum 逐字同**（run2：`174d8085…`/`3f708c78…`/`cc229929…`）且 fixture/A-g4/B-g4 三方 sha256 唯一值=字节平价 | **一致**（契约核心断言面：CT literal+两 checksum present 全绿） |
| g5-sumdb | **404** JSON errors envelope `Not Found` | **404** text/plain `not found` | **姿态一致**（A 不服务 sumdb 代理实锤——`sumdb-404-posture` unobserved 解除，无 UNSUPPORTED 分歧）；错误体形态差=conan D2 同族（记录） |
| g6-mod | 200 CT=`text/plain+mod`；体=fixture 逐字节 | 200 CT=`text/plain; charset=utf-8`；体=fixture 逐字节 | **体一致**（original unmodified 官方义务双端字节平价）；CT 同 G1 |

## 2. conan PUT 补臂（A 侧单边取证；ref `hello19/1.0/l019/stable`，仓 `l019-conan-put`）

背景：L018 v1-15 真实 CLI 腿 wire 止于 upload_urls POST——conan 1.66 对绝对 URL 直发 PUT 绕过 wireproxy，**v1 files 直传通道的请求/响应形态在 A 侧从未落 wire**（契约 `conan/v1-url-family` side_effects 只有 URL 生成面 wire 证）。本轮 curl 模拟直发补齐。

| 臂 | A（ref）形态 | 判定 |
|---|---|---|
| cp-01 upload_urls | 200 application/json；绝对 URL `…:8082/artifactory/api/conan/l019-conan-put/v1/files/l019/hello19/1.0/stable/0/export/conanfile.py`——坐标序 user/name/ver/channel+`/0/` 默认修订段+export 面与 L018 v1-16 同形（新仓新 ref 再锚） | 形态一致（与前轮自一致） |
| cp-02 裸直发 PUT（无 checksum 头） | **`HTTP/1.1 201 Created`，Content-Length: 0，无 checksum 回显头、无 Location**，仅 vendor 头（X-Artifactory-*/X-Jfrog-Version） | **成功码 201+空体首证**（unobserved 主面） |
| cp-03 X-Checksum-Sha1 直发 PUT | 同 cp-02（201/0B，形态不变） | checksum 头不改变响应形态 |
| cp-04 副作用+回读 | files GET 200 **byte-parity SAME**（CT `text/x-python`——按扩展名）；v1 snapshot 200 md5 map `{conanfile.py: d844b13a…, conanmanifest.txt: 8c358354…}`，conanfile.py md5 与回读体复算**一致** | **直传 PUT 即登记修订/索引**（副作用 A 侧活体证——snapshot 立即可见） |

BinFlow 侧对照锚：B 同通道形态已由 L018 v1-15 CLI 腿消费成功（install 回读全绿）+ v1-16 双端 upload_urls 同构推断 201；本轮未重跑 B（A 形态取证为票面目标）。B 侧直发 PUT 逐字节 wire 对照留待下轮 conan 差分批（若需可 1 臂收口）。

## 3. 差异清单与分类建议（INTENTIONAL 终局裁定不在本角色）

| # | 面 | A | B | 分类建议 | 一句证据 |
|---|---|---|---|---|---|
| G1 | GET 面 CT vendor 后缀 | `.info/.latest`=`application/json+info`；`.mod`=`text/plain+mod` | `application/json` / `text/plain; charset=utf-8` | **契约修订提案**（非 B 偏离） | 契约 version-file-suite 断言 literal `application/json`——A 活体为 vendor 后缀形态；go 客户端不校验 CT；建议 expect 改前缀/present 形态+A 活体注记（归 compatibility-engineer） |
| G2 | PUT 成功响应 CT | vendor ItemCreated CT+0B | 无 CT+0B | 记录级 | 契约无 PUT 响应 CT 断言面；状态码 201 双端同 |
| G3 | list 时间戳列 | `v1.0.0 <RFC3339>`（上传时刻） | 纯版本列 | **INTENTIONAL 维持**（既有 known-divergence） | 双端活体 27B vs 7B；normalize 剥列后一致；反编译单源→活体双证（升置信素材） |
| G4 | sumdb 错误体形态 | JSON errors envelope | 裸 text `not found` | 记录级（conan/npm D2 同族） | 双端均 404——姿态一致即契约面达成；envelope 族归 conan D2 对齐票（L019-1）统一裁 |
| C1 | conan 直传 PUT 通道 | 201/0B/snapshot 即登记 | （未重跑——L018 CLI 消费成功+同构推断） | 无分歧主张（A 取证票） | cp-02/03/04 wire 在案 |

## 4. 升格/回流建议（状态机反馈——compatibility-engineer 裁定）

1. **goproxy/put-upload-triplet IMPLEMENTED→VERIFIED 候选**：s1 成功臂双端 201（A 成功码活体首证——unobserved「成功码未活体」解除）；unobserved 注记建议**收窄**为「error 面（400 族/409 checksum）A 未活体——系 BinFlow PRD 加严面非对齐面」。置信 medium→high 候选。
2. **goproxy/sumdb-404-posture INTENTIONAL 维持+unobserved 解除**：g5 双端 404——A 不服务 sumdb 实锤，无 UNSUPPORTED 分歧（该条目显式不做决策双端成立）。
3. **goproxy/version-file-suite / list-latest unobserved 解除候选**：g1-g6 全臂双端活体——体面全字节平价（.info/.mod/.zip≡fixture）；升格素材=G1（CT vendor 后缀契约修订提案）+ G3（时间戳列反编译单源→活体双证）。
4. **conan/v1-url-family side_effects 注记升级**：「直传通道（PUT files/<path>）」由 URL 生成面推断→A 侧直发 wire 亲证（cp-02/03：201/0B；cp-04：snapshot 即登记）。注：dispatch 所称「契约 put-upload-triplet」在 conan 域无同名条目——conan 侧对应面即本条 side_effects（goproxy/put-upload-triplet 由 §4-1 承接）。
5. **提金候选**（A 侧逐字落档）：g2/g3 `.info` 体+CT、g6 `.mod` 体+CT、g4 三 checksum 头形、g1 时间戳列形态、s1 PUT 201 vendor CT 形、conan cp-02 直传 201/0B 头形（`l019-goproxy-wire/run2-bodies/` + `l019-conan-put/a/conan/`）。
6. **normalize 零新增**：本批全差异落在已登记规则（list_timestamp_column）或契约断言面外——无新归一提案。

## 5. 复跑门与回归对照

- goproxy 探针两轮全跑（run1/run2 log 在档）：**结论逐臂稳定**（状态面/判定全同；逐值差仅非确定字段——g1 时间戳列时刻、g4 zip 三 checksum 因 zip 内嵌 mtime 逐轮重建，轮内 A≡B≡fixture 恒成立）。
- 回归对照：goproxy 域为差分腿首跑（L018-2 因 docker daemon 全灭未成——无上轮清单）；conan 域对照 L018 双端面未重跑（本轮 A 侧新取证），无回归主张。
- 双端清理：`l0182-go-local`（两轮各建删一次，终态 0 残留双端）+ `l019-conan-put`（A，DELETE 200，0 残留）；audit-probe 前缀双端仓列表 0 残留；/tmp 脚手架（l0182/l019cp/日志副本）不入仓。
- 环境注记：A 侧现存 `l0172-mvn-u`（L017-2 maven 轨道命名空间）——非本票资产未动，报 conductor 知悉。

## 6. 上游修复提案（docs/compatibility/ 归 compatibility-engineer，本角色不改）

`docs/compatibility/probes/goproxy/endpoints-probe.sh` line 28：`local … file=$5` 在 `set -u` 下遇 4 参调用（全部 GET 臂）即崩 `unbound variable`——该 probe 前次从未完整执行故未暴露。修复=一行 `file=${5:-}`。复跑版已落本角色域 `tools/difftest/l019/goproxy-endpoints-probe.sh`（另 AAUTH 参数化），建议上游同款回灌后再归档 L018-2 残留注记（脚本尾部「复跑前置」注记本轮已兑现可清）。
