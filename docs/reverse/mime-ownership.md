# mimeType 归属行为规格（T-569）

> 源版本：JFrog Artifactory 7.161.26（活体参照 `http://192.168.120.38:8082/artifactory`）+ 7.161.24 全量反编译树 `/Users/lzw/workspace/artifactory-decompiled`（软链 `reverse-src/artifactory`）。
> 活体证据：`reports/compatibility/L036-r7-followup-probes.md` ①（18 腿归属矩阵 + 15 扩展名表，双轮 `l036-r7fp≡r1`）；固化 case `tools/difftest/v2/cases/generic_mime_ownership.py`。
> 置信度：`高` = 活体取样 + 出厂配置文件双证；`中` = 出厂配置文件单源（活体未采样该扩展名）；`低` = 待动态验证。
> 证据锚：〔活〕= L036 活体；〔码〕= 反编译树 `backend/artifactory-config/META-INF/default/mimetypes.xml`（出厂默认表，随产品分发的配置文件）及 mime 查表模块（只读提取行为事实）。

## 1. 归属模型（行为句式）

1. 当客户端以任意 Content-Type 请求头部署任意路径到任意本地仓库，随后的下载响应 Content-Type 与该路径 FileInfo 的 `mimeType` 字段**只由路径扩展名查「mime 白名单表」得出；请求声明的 Content-Type 不参与该值的任何环节**（18 腿矩阵：全部显式声明腿——text/plain、application/json、application/xml、x-www-form-urlencoded、自定义值、带 charset 参数、.txt+json 声明、.json+text/plain 声明——活体返回值无一取自请求头）。〔活〕`高`
2. 当存储扩展名未命中表、或路径无扩展名时，下载响应与 FileInfo 的 mimeType 返回 `application/octet-stream`（.csv/.pdf/.svg/.bin 与全部无扩展名腿活体实证）。〔活〕`高`
3. 无内容嗅探：该值与制品字节内容无关——二进制体放 .txt 扩展名仍返回 text/plain，文本体无扩展名仍返回 octet-stream。〔活〕`高`
4. 冲突裁决：扩展名与请求声明不一致时，扩展名赢（.txt+声明 json → text/plain；.json+声明 text/plain → application/json，两腿互为镜像）。〔活〕`高`（第 1 条的推论，独立活体双证）
5. 扩展名匹配不区分大小写（表键按小写比对）。〔码〕`中`
6. 推导发生在响应渲染时按路径查表，不依赖部署时落库的存储值（参考实现渲染面只读佐证〔码〕；活体未区分采样——「渲染时查表」与「部署时查表落库」两实现形态对新建制品 wire 等价，仅影响既有行的再渲染语义）。`中`
7. 该表可被实例级配置文件覆盖（etc/mimetypes.xml），出厂默认表版本 17；参照实例 7.161.26 的全部活体取样（15 扩展名 + 18 腿）与出厂默认表一致。〔码〕`中`
8. 下载响应与 FileInfo 双面恒一致（取值同源）。〔活〕`高`

## 2. 出厂默认扩展名白名单表（版本 17）

行为句式：当存储扩展名为左列值时，GET mimeType 返回右列值。**不在表内的一切扩展名 → application/octet-stream。**

| mimeType | 扩展名 | 活体 |
|---|---|---|
| text/plain | txt, properties, mf, asc, log, yml, yaml, tf, md | txt/md/yaml ✓〔活〕 |
| text/html | htm, html | html ✓〔活〕 |
| text/css | css | — |
| text/xsl | xsl | — |
| text/xslt | xslt | — |
| text/x-java-source | java | — |
| text/x-javafx-source | fx | — |
| text/x-groovy-source | groovy, gradle | — |
| text/x-c | h, c, cc, cpp | — |
| application/xml-dtd | dtd | — |
| application/xml-schema | xsd | — |
| application/xml-external-parsed-entity | ent | — |
| application/xhtml+xml | xhtml | — |
| application/json | json | json ✓〔活〕 |
| text/x-python | py | — |
| application/x-java-pack200 | jar.pack.gz | — |
| application/x-java-archive-diff | jardiff | — |
| application/zip | zip | zip ✓〔活〕 |
| application/x-xz | xz, tar.xz, nar.xz | — |
| application/x-tar | tar | tar ✓〔活〕 |
| application/x-gzip | tgz, tar.gz, gz | tgz/gz ✓〔活〕 |
| application/x-bzip2 | bz2, tar.bz2 | — |
| application/x-7z-compressed | 7z | — |
| application/x-nupkg | nupkg | — |
| application/x-conda | conda | — |
| application/x-rar-compressed | rar | — |
| application/vnd.android.package-archive | apk | — |
| application/x-rpm | rpm | rpm ✓〔活〕 |
| application/x-rubygems | gem | — |
| application/x-ruby-marshal | rz | — |
| application/x-debian-package | deb, ddeb | deb ✓〔活〕 |
| application/x-vagrant-box | box | — |
| application/json+info | info | — |
| text/plain+mod | mod | — |
| text/x-swift␠ | swift（出厂表 type 值带尾随空格，逐字） | — |
| text/x-scala-source | scala | — |
| text/x-ruby-source | rb | — |
| text/x-script.sh | sh | — |
| text/x-csharp.sh | cs | — |
| application/xml | xml, xsl, xsi | xml ✓〔活〕 |
| application/x-maven-pom+xml | pom | — |
| application/x-ivy+xml | ivy | — |
| application/x-nuspec+xml | nuspec | — |
| application/x-java-jnlp-file | jnlp | — |
| application/x-checksum | sha1, sha256, md5 | sha1 见 §3-1 |
| application/java-archive | jar, war, ear, sar, har, hpi, jpi | jar/war ✓〔活〕 |

注记：
- **xsl 双登记**（text/xsl 与 application/xml 均列 xsl）：出厂表原样，先后命中顺序未活体实证——`中`，待验证（§3-3）。
- 表内值均不带 charset 参数（text/* 裸值）；.md → text/plain（非 markdown 专用值）、.yaml/.yml → text/plain（非 yaml 专用值）——与 RFC 推荐拼写相反，为兼容对齐的关键差异面。
- **不在表内**（活体实证 → octet-stream）：csv、pdf、svg、bin、无扩展名。〔活〕`高`
- 多段扩展名拼写（tar.gz/tar.bz2/jar.pack.gz）与「取最后一段」两种解析对 tar.gz/tar.bz2 收敛同值；jar.pack.gz 两形态发散（x-java-pack200 vs x-gzip）——解析规则未实证，见 §3-3。

## 3. 待验证清单

1. **.sha1 FileInfo 缺 mimeType 字段**：.sha1 在表内（application/x-checksum）但活体 FileInfo 响应无 mimeType 字段——sidecar 序列化特殊路径或字段省略怪癖，机制未定位〔活〕。`低`
2. **既有制品再渲染**：部署前已存在的行、或部署后经实例配置改表，再 GET 是否即时按新路径推导（第 6 条渲染时模型预测「是」；未活体设计实验）。`低`
3. 多段扩展名解析规则（最后一段 vs 全后缀匹配）；xsl 双登记的命中顺序。`低`
4. 仓库配置 `blockMismatchingMimeTypes` 开关（B 侧 repo_config_render.go 已渲染该键，默认 false）：开启后部署时是否校验声明 CT 与扩展名推导值一致并拒绝——出厂表注释提示存在该校验面，行为未活体实证。`低`
5. 实例级 mimetypes.xml 覆盖语义（版本迁移转换器族存在，覆盖加载顺序未走查）。`低`
