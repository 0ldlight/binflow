# 提案：mimeType 归属模型裁定素材（BIN-51 / T-569）

> 性质：proposal-only（不动产品码、不翻台账、不写 Linear；裁定权 conductor）。
> 行为规格依据：`docs/reverse/mime-ownership.md`（T-569 新建，7.161.26 活体 18+15 腿 + 出厂默认表双源）。
> 差分证据：`reports/compatibility/L036-r7-followup-probes.md` ①；固化 case `generic_mime_ownership.py` 双轮。
> 台账关联：`known-divergence.yaml#generic/mime-ownership-model`（UNKNOWN）+ `#generic/mime-stdlib-host-drift`（UNKNOWN）。

## 0. 待裁三项（速览）

| # | 裁定 | 选项 | 建议（§2） |
|---|---|---|---|
| 1 | 归属模型：generic 存储面 PUT 是否忽略请求声明 CT，改路径扩展名查表 | 甲=对齐 A / 乙=维持 B verbatim | **甲**（存储面范围） |
| 2 | 表值拼写：无声明腿的扩展名表取 A 拼写（去 charset、md→text/plain、gz→x-gzip、csv 删条目）还是 B 现值 | 随 #1 / 或独立对齐 | **独立先行对齐**（两方向都受益） |
| 3 | stdlib 回退：`mime.TypeByExtension`（OS 库，宿主漂移源）删掉还是扩表钉死 | 3a 删回退（A 同构）/ 3b 扩表钉 IANA | 随 #1（甲→3a，乙→3b） |

## 1. 全域 mimeType 决策点影响面清单（甲波及面；HEAD 静态 grep 全量）

「对齐甲后」= 存储面（generic 内容 PUT/GET/FileInfo、bare 内容面）改为渲染时按路径查 A 表、声明 CT 不参与；协议专用面（docker /v2、pypi simple、npm tarball、deb/rpm 索引族、helm index）不在本次裁定范围，维持各协议已裁形态。

| 渲染点（文件:行） | 面 | 当前取值来源 | 对齐甲后变化 |
|---|---|---|---|
| generic/handler.go:103 | 存储面 PUT 存储 mime | 声明 CT verbatim；空→generic 表+stdlib | 改恒路径查表（声明 CT 忽略）——核心变更点 |
| generic/handler.go:334 | 存储面 GET CT | `mimeForNode` 存储值赢 | 改路径查表（A 渲染时形态，免数据迁移） |
| maven/put.go:218,286 | 存储面 PUT 存储 mime | 声明 CT verbatim；空→maven 表 | 同 generic；**现注释「Artifactory honors the header too」与 A 活体事实相反，随票勘误** |
| maven/handler.go:316 | 存储面 GET CT | `mimeForPath` 存储值赢 | 改路径查表 |
| maven/handler.go:247,36 / calc.go:71 | .sha1 sidecar GET / 计算元数据存储 | x-checksum / application/xml 常量 | 不变（协议已裁面；A 表 .xml→application/xml 同值） |
| conan/v1.go:464, v2.go:145 | conan files PUT 存储 | 声明 CT verbatim，空→octet-stream | 波及：无扩展名文件+声明 CT 腿变化；需 conan 域确认（协议面 vs 存储面归属） |
| pypi/upload.go:143,241-244 | twine 上传存储 | multipart part CT verbatim，空→octet-stream | twine 实发 part CT≈octet-stream，wire 近无变化；.whl 路径腿 A=octet-stream 与现值收敛 |
| pypi/download.go:134 | 下载 CT | 存储值赢，兜底 .whl→application/zip | 存储面腿改查表（.whl 不在 A 表→octet-stream，与 twine 腿收敛）；协议面不变 |
| cargo/handler.go:336-341, publish.go:307,336 | derived 写/发布存储 | .json→application/json、.crate→octet-stream 常量 | wire 不变（A 表同值：.json→application/json、.crate 不在表→octet-stream） |
| deb/handler.go:404 | deb 包上传存储 mime | 常量 `application/vnd.debian.binary-package` | **现值即与 A 分歧**（A 表 .deb→application/x-debian-package，L036 ②活体）；渲染时查表形态自动收敛，或随票改常量 |
| deb/handler.go:318 | servePlainFile 存储/bare 面 | 恒 octet-stream | 波及：.deb 路径腿 A=x-debian-package（渲染时形态自动收敛） |
| rpm/handler.go:441 | rpm 上传存储 | 常量 x-rpm | 不变（A 表同值） |
| helm/handler.go:427 | chart 上传存储 | 常量 x-gzip | 不变（A 表 .tgz→x-gzip 同值） |
| nuget/flat.go:329,512 | nupkg push/bare PUT 存储 | 恒 octet-stream | **与 A 表分歧**（A .nupkg→application/x-nupkg）——渲染时形态自动收敛；协议面（v2/v3 客户端）不受影响（dotnet 不读包 CT，§2） |
| nuget/flat.go:343,349 | nuspec/sha512 sidecar 存储 | application/xml / text/plain; charset=utf-8 | A 表 .nuspec→application/x-nuspec+xml、.sha512 不在表→octet-stream（**两条邻接新差异**，见 §1-附）；渲染时形态自动收敛 |
| npm/publish.go:279 | tarball 存储 | 恒 octet-stream | 共享 FileInfo 面 A=.tgz→x-gzip（**邻接新差异**）；npm 客户端不读 CT |
| goproxy/local.go:356（handler.go:245） | 模块文件存储/下载 CT | .info→application/json、.mod→text/plain; charset=utf-8、.zip→application/zip | **A 表：.info→application/json+info、.mod→text/plain+mod**（邻接新差异，协议面另裁） |
| docker/manifest.go:166、helmoci（doc.go:41） | OCI manifest 存储 CT | 客户端 mediaType 透传（协议必需，T-32 R3 已裁） | **排除**（OCI 内容协商要求，非 generic 面） |
| httpapi/storage.go:522 | /api/storage FileInfo mimeType | 存储值（mimeOrDefault） | 改路径查表（或存储侧物化，两形态 wire 等价） |
| httpapi/uploads.go:458,475,991 | upload 会话面 | octet-stream 默认 | 不变（独立会话面） |
| repo/service.go:1254 | folder 行 mime | 恒 octet-stream | 不变（目录无扩展名，A 同收敛） |

**附：本次梳理新识别的邻接差异**（不在本裁定范围，建议入 R8 差异池，均未立台账）：goproxy .info/.mod 下载 CT 与 A 表拼写分歧；nuget .nupkg FileInfo 面（A=x-nupkg vs B=octet-stream）；deb 存储常量 vnd.debian.binary-package vs A x-debian-package；npm .tgz 共享 FileInfo 面（A=x-gzip）；maven put.go 注释与 A 事实相反（勘误级）。

## 2. 方案甲 vs 方案乙 + 建议裁定

**方案甲（对齐 A）**：代价 = generic/maven/conan 三个声明 CT 消费点翻转 + 表值重写（§3 群1/群2）+ 上述邻接差异自动收敛（若采渲染时查表形态，免数据迁移）。收益 = 18 腿矩阵全部翻 SAME、12/15 表差分翻 SAME、mimeType 类 UNKNOWN 两条可 resolved、stdlib 宿主漂移类问题连根消除（A 无 stdlib 回退）。

**方案乙（登记分歧）**：代价 = `generic/mime-ownership-model` 翻 INTENTIONAL（authority=本裁定票）+ 表值差分维持（除非独立裁 #2）+ stdlib 漂移仍需 §3-3b 扩表止血 + 后续每张涉 CT 的契约都要带 divergence_ref 常态化脏账。

**真实客户端对响应 CT 的依赖评估**（工程评估，中置信；L036 真实客户端腿 NOT_RUN 未实证）：pip/twine 不读下载响应 CT（pip 只解析 simple index 与字节流，twine 只读状态与响应体）；mvn resolver 不校验制品 CT（只校 checksum）；npm 不读 tarball CT；dotnet/nuget 不读 nupkg CT（按 zip 解析）。**实际读 CT 的消费者只有**：浏览器（渲染 vs 下载、字符集推断——A 裸 text/plain 在浏览器默认按 latin-1 解码 utf-8 文本会 mojibake，A 自身如此，对齐即接受）与 `curl -J`（按 CT 推文件名后缀，边缘用法）。**结论：两个方向都不破坏包管理器客户端；分歧的实际暴露面是浏览器/UI 预览与 wire 级差分。**

**建议裁定：甲，分两票落地。** 理由：其一，A 的模型已被完整捕获（出厂表 + 18 腿活体 + 渲染面参考实现佐证），对齐是有界、可差分回归的确定性变更，而 B 的 verbatim 模型在 A 无对应物——维持它意味着每个声明 CT 消费点永久带 divergence_ref，矩阵与契约面常态脏账。其二，真实客户端无一方依赖响应 CT，翻转的实际风险集中在浏览器预览体验（与 A 同形即为目标形态）。其三，甲顺带消除 stdlib 宿主漂移类（A 无回退），一票清两类 UNKNOWN。**票序**：票 1 = 表值对齐（§3 群 1/群 2，裁定 #2——即使最终裁乙也值得，把 15 腿表差分 ~10 腿翻 SAME）；票 2 = 归属模型翻转 + stdlib 回退删除（裁定 #1+#3a），范围限定存储面（§1 表），协议面逐族维持既有裁定。若 conductor 裁乙：票 1 仍执行，模型登记 INTENTIONAL（authority=本裁定票），§3-3b 扩表止血。

## 3. 表值对齐与扩表提案（裁定 #2 / #3 素材）

**群 1：现 B 自有表值改 A 拼写**（依据 = A 活体 + 出厂表，`docs/reverse/mime-ownership.md` §2）：

| 扩展名 | B 现值 | 改为（A） | 依据 |
|---|---|---|---|
| .txt | text/plain; charset=utf-8 | text/plain（去 charset） | A 活体 |
| .csv | text/csv; charset=utf-8 | **删条目**（→octet-stream） | A 活体（表无 csv） |
| .md | text/markdown; charset=utf-8 | text/plain | A 活体（表内挂 txt 族） |
| .html/.htm | text/html; charset=utf-8 | text/html | A 活体 |
| .yaml/.yml | application/yaml | text/plain | A 活体 |
| .gz/.tgz | application/gzip | application/x-gzip | A 活体 |

**群 2：A 表有、B 表缺/值异的条目**（generic 与 maven 两张自有表同步；依据 = 出厂表〔码〕中置信，未活体采样）：.deb/.ddeb→x-debian-package、.rpm→x-rpm、.nupkg→x-nupkg、**.pom→application/x-maven-pom+xml（maven 表现值 application/xml，与 A 异）**、.nuspec→application/x-nuspec+xml、.sar/.har/.hpi/.jpi→java-archive（maven 已有 .ear，generic 缺）、.info→application/json+info、.mod→text/plain+mod（后两条与 goproxy 协议面取值冲突，随 goproxy 邻接差异一并裁）、.properties/.log/.tf/.asc→text/plain、.zip/.tar/.json/.xml/.jar/.war/.txt 族/.gz 族/.sha1/.sha256/.md5 已同值或随群 1 对齐。**注意**：A 表无 .sha512——B generic/maven 两表现有 .sha512→x-checksum 是超集，甲全对齐应删（→octet-stream）；sidecar GET 面的 x-checksum 是协议常量不受影响。

**群 3：stdlib 回退处置**（裁定 #3）：
- **3a（甲配套，建议）**：删除 `mimeByExtension` 的 stdlib 回退——A 即此形态（表外一律 octet-stream），删码消除宿主漂移。~~无扩表。~~ **勘误（R9 / T-571 落地时）**：「无扩表」表述有误——删回退的前提是自有表已对齐 v17 全量：原由 stdlib 回答的表外键（.pdf/.svg/.css/.swift 等）在删回退后全部翻 octet-stream，若 B 表仍缺这些键所属的 v17 行，删除动作本身会制造一族新 DIFF。落地票 BIN-53 因此将 3a 与「表全量补齐 v17」绑定为同一不可拆决策（双审 rider ①②）。
- **3b（乙配套）**：保回退但扩表钉死常见扩展（依据 = 本机 darwin stdlib 实测〔T-569 探针，/tmp 留存〕+ IANA 注册值；与 A 分歧面固化，A 均为 octet-stream）：.pdf→application/pdf、.svg→image/svg+xml、.png→image/png、.jpg/.jpeg→image/jpeg、.gif→image/gif、.ico→image/x-icon、.webp→image/webp、.woff/.woff2→font/woff、font/woff2、.mp4→video/mp4、.7z→application/x-7z-compressed、.bz2→application/x-bzip2、.xz→application/x-xz、.rar→application/x-rar-compressed、.apk→application/vnd.android.package-archive。darwin 实测另证漂移实例：.exe darwin=application/x-msdownload（linux 瘦容器无 OS 库→octet-stream）；slim linux 容器取证腿未拍（NOT_RUN，随票补）。

**回归面**：表值改动波及 generic/maven 两族无声明 CT 腿全部 wire 输出——落地票必带 `generic_mime_ownership.py` 双轮差分 + 两族 GET/FileInfo 单测快照更新；`blockMismatchingMimeTypes` 配置键（B 已渲染默认 false）行为面随票走查（规格 §3-4 待验证）。
