# GPG Keypair 体系规格（M11 T-319 mini 规格）

- 状态: Accepted（T-319 票内 mini 规格，锚定 ADR-0038 的 wire 面）
- 日期: 2026-08-27
- 归属: 本文件是 K-1（BOARD 风险登记③）的落笔规格。**keypair 体系属自设计 + Artifactory 行为对齐混合**：机制条款（存储/密封/关联/护栏/门）由 ADR-0038 裁定，本文件锚定 REST wire 字面量与生成默认值（ADR-0038 决策 3/6 指定的锚点票）。
- 消费票: T-321（debian 签名腿：InRelease clearsign + Release.gpg detached）/ T-322（rpm 签名腿：repomd.xml.asc detached + repomd.xml.key 公钥）。

## 0. 出处层级（本文件条款的证据分级）

| 层级 | 含义 | 证据源 |
|---|---|---|
| L-A | Artifactory 行为，高置信 | JFrog 官方 REST API 文档（docs.jfrog.com/administration/reference/{createkeypair,getallkeypairs,updatekeypair,getkeypair,deletekeypair,verifykeypair,getkeypairpublickeyperrepository,setrepositorykey,deleterepositorykey}.md，2026-08-27 抓取，含 OpenAPI 定义）+ docs/reverse/inv-4-addons.md M3、inv-2-surface.md §1.D、debian.md §5、rpm.md §4.3 |
| L-B | BinFlow 自设计 | ADR-0038 机制条款（决策 2/3/4/5/6）+ 本文件裁定 |
| L-C | 低置信，规格待验证 | Artifactory UI 面的 keygen 行为（无公开 REST 证据；inv-2 的 `security/keyPairs` UI REST 族未取证）——BinFlow 生成端点按 L-B 自设计落地，Artifactory 侧证据出现后按 Q8 先例翻转 |

## 1. Artifactory 兼容面（L-A，端点族与 wire）

BinFlow 挂载在 `/binflow/api` 前缀下（ADR-0008；与 users/token 族同段）。安全门为 BinFlow RBAC（§3.4，L-B 分歧登记）。

### 1.1 实例级 keypair CRUD

| 端点 | Artifactory 语义 | BinFlow 行为 |
|---|---|---|
| `POST /api/security/keypair` | 导入 keypair；**存在同名则整对替换**（create-or-replace） | 一致；201；响应 KeyPairSummary |
| `PUT /api/security/keypair` | 更新既有 keypair（pairName 在 body）；**不存在 → 404** | 一致；200；响应 KeyPairSummary。轮换面（同 名换钥） |
| `GET /api/security/keypair` | 列出全部 → KeyPairSummary 数组 | 一致；200 |
| `GET /api/security/keypair/{pairName}` | 单查 → KeyPairSummary | 一致；404 未知名 |
| `DELETE /api/security/keypair/{pairName}` | 删除；200 正文 `OK`（text/plain） | 一致 + ADR 引用护栏（§3.5） |
| `POST /api/security/keypair/verify` | 校验 keypair 有效性；200 正文 `Key was verified.`（text/plain）；400 无效 | 一致（body 全量材料校验）+ BinFlow 扩展（§2.3） |
| `GET /api/security/keypair/public/repositories/{repoKey}` | 返回该仓关联 keypair 的公钥（armored，text/plain） | 一致；无关联/无仓 → 404 |

**KeyPairInput**（请求体，`*` 必填）：

```json
{
  "pairName": "deb-signing",        // * 标识符（§3.2 字符集）
  "pairType": "GPG",                // * 枚举 RSA | GPG（BinFlow 仅 GPG，§2.1）
  "alias": "binflow-deb",           // * 别名（回显用）
  "privateKey": "-----BEGIN PGP PRIVATE KEY BLOCK-----\n…",  // * armored 私钥块
  "publicKey":  "-----BEGIN PGP PUBLIC KEY BLOCK-----\n…",   // * armored 公钥块
  "passphrase": "…"                 // 可选；私钥口令（无口令钥可省）
}
```

**KeyPairSummary**（GET/创建回显；**私钥与口令永不出库**，任何响应不含这两个字段）：

```json
{
  "pairName": "deb-signing",
  "pairType": "GPG",
  "alias": "binflow-deb",
  "publicKey": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n…"
}
```

### 1.2 仓关联（v2 面，7.19+）

| 端点 | Artifactory 语义 | BinFlow 行为 |
|---|---|---|
| `POST /api/v2/repositories/{repoKey}/keyPairs` | body 为 text/plain 的 keypair 名，设置该仓签名钥 | 一致；写 §3.3 的仓配置引用字段（经同一校验链） |
| `DELETE /api/v2/repositories/{repoKey}/keyPairs/{keyName}` | 解除该仓关联 | 一致；清除引用字段 |

primary/secondary 双槽 + promote 族（`…/keyPairs/primary|secondary|promote`、`…/primary/public`）**不进 M11**（§2.5）。

## 2. BinFlow 裁定与分歧登记

### 2.1 pairType 仅 GPG（L-B，基于 L-A 证据的裁剪）
`pairType` 只接受 `GPG`（armored OpenPGP 材料服务 debian/rpm 元数据签名——ADR-0038 决策 4 的 M11 范围）。`RSA`（PEM 对，Alpine 索引签名域）→ 400，文案点名不支持与原因。vault 字段（`vaultKey`/`vaultPublicKey`）按名拒绝（inert-field 陷阱纪律，同 m11RemoteFields 先例）。

### 2.2 无 Artifactory keygen REST（L-C → L-B 自设计）
官方 REST 面与逆向证据（inv-4 M3）均无服务端生成端点；Artifactory UI 生成行为无公开证据。BinFlow 落自设计生成端点（ADR-0038 决策 3「双入口」的 keygen 腿）：

`POST /binflow/api/v1/admin/security/keypair/generate`（CapSecurityWrite；dispatchAPI 显式路由族）

```json
{ "pairName": "deb-signing", "alias": "binflow-deb",
  "passphrase": "…",           // 可选；缺省生成无口令钥（仍整封 enc:v1）
  "keyBits": 4096,             // 可选；缺省 4096，仅接受 2048/3072/4096
  "uidName": "BinFlow", "uidComment": "repository metadata signing", "uidEmail": "binflow@localhost" }  // UID 三段，均可覆盖
```

响应 201 + KeyPairSummary。同名已存在 → 409（生成不覆盖；替换走 PUT）。

### 2.3 生成默认值（Q8 锚定结论）
ADR-0038 决策 6 的「暂行值」按 Q8 先例（20:35「全部照 Artifactory 实际值」）核对：**Artifactory 公开文档无 keygen 端点故无公开生成默认值**——暂行值无处翻转，按 ADR 机制转正并留痕：

- **RSA-4096**（keyBits 缺省；apt/dnf 旧客户端最大兼容——决策 6 原文）
- **不含过期**（keyLifetime 0）
- **主钥（签名）+ 加密子钥**的标准结构（openpgp NewEntity 形态；主钥即签名钥）
- UID 缺省 `BinFlow (repository metadata signing) <binflow@localhost>`
- 导入路径接受 Ed25519/ECC 密钥（ProtonMail 库原生支持，ADR 决策 6）

### 2.4 口令保护为可选（L-A wire 对齐）
passphrase 非必填（Artifactory schema 一致）。无口令钥 = 私钥块无 S2K 保护，但**两列仍整封 enc:v1**（静态密封不依赖口令强度，ADR 决策 2 的双列理由）。

### 2.5 单关联槽 + 轮换路径（L-B）
仓配置引用字段单值（ADR 决策 5 的单引用）；语义等同 Artifactory v2 面的 primary 槽。secondary/promote 不做。**轮换路径 = 导入/生成新钥（新名或 PUT 同名替换）→ 仓关联改指 → 重算索引重签**（debian/rpm 重算腿 T-321/T-322 承接）。

### 2.6 分歧与裁剪清单（对 Artifactory）

| # | 分歧 | 裁定 |
|---|---|---|
| D-1 | GET 族门：Artifactory「authenticated user 或 anonymous」，list/verify 为 admin | BinFlow 全族 CapSecurityRead（GET）/CapSecurityWrite（写+verify+generate）——ADR 决策 3 的门条款为准（BinFlow RBAC 姿态；公钥分发走内容面 repomd.xml.key 等文件，不靠 API 匿名读） |
| D-2 | DELETE 引用护栏：Artifactory 文档未载 | ADR 决策 3：被仓引用 → 400，文案点名引用仓清单 |
| D-3 | 回显字段：Artifactory KeyPairSummary 四字段 | BinFlow 附加 `createdAt/updatedAt/updatedBy/repositories`（被引用仓清单）——additive，四字段名与语义逐字对齐 |
| D-4 | 生成端点 | BinFlow 自有路径 `/binflow/api/v1/admin/security/keypair/generate`（Artifactory 无对应公开端点，§2.2） |
| D-5 | verify 扩展 | body 仅 `pairName`（无钥材料）→ 校验**存量**密封钥（解封→开口令→签名自试）；材料齐全 → 校验提交材料。Artifactory 只载全量材料形态 |
| D-6 | 3.3 时代 legacy GPG 端点（setgpgpublickey/setgpgprivatekey/setgpgpassphrase） | 不实现（被 keypair 体系整体取代） |
| D-7 | repo 关联面 | v2 双端点实现；primary/secondary/promote/public 变体不做（§2.5） |
| D-8 | `X-GPG-PASSPHRASE` 头（rpm.md §4.2：可经头传口令） | 不收（口令随 keypair 行密封存储，ADR 决策 2；签名腿不读头——T-321/T-322 按此） |

## 3. 机制条款（L-B，ADR-0038 落地形态）

### 3.1 存储与密封
- 表 `gpg_keypairs`（migration 016，双方言同文）：`pair_name TEXT PRIMARY KEY`（ADR DDL 的 keypair_id 列按 wire 拼写更名 pair_name——值即 pairName，标识合一）、`pair_type`、`alias`、`public_key`（armored 明文，公开物）、`private_key_enc`（**导入原样 armored 块**整封 `enc:v1:`）、`passphrase_enc`（`enc:v1:`）、`algorithm`（如 RSA-4096 / Ed25519）、`created_at`、`updated_at`、`updated_by`。
- POSTURE：无主密钥（`BINFLOW_REMOTE_CREDENTIALS_KEY` 未设）→ 一切写路径拒绝（400 点名环境变量）；存量行存在而无主密钥 → 启动 fail-fast。
- 导入校验：armored 块可解析、公私钥主钥指纹一致、有口令保护的私钥必须随附口令且口令可开（无口令钥合法——口令可选，§2.4；**带保护无口令拒收**：不可能签名成功的钥是 inert 陷阱）。

### 3.2 pairName 字符集
`[a-zA-Z][a-zA-Z0-9_-]{0,63}`（64 上限；与 repoKey 规则同族但允许大写/下划线——keypair 非路由段）。alias 同则同规则、上限 128。

### 3.3 仓配置引用字段
- 拼写 **`keyPairName`**（KeyPairInput 的 wire 拼写延伸到仓配置；ADR 决策 5「拼写随 mini 规格」）。
- **local debian/rpm**：接受；建/改仓校验引用存在（不存在 → 400）；空串 = 清除引用。
- **local 其他包型（含 helm——HL-4）**：按名拒绝（400，文案点名 signing 是 debian/rpm local 行为；helm `.prov` 走普通文件存储）。
- **virtual / remote 全包型**：按名拒绝（签名是 local 写路径行为）。
- 一 keypair 多仓共用（共享池，跨 debian/rpm 同钥）；GET 回显 `repositories` 清单。

### 3.4 能力门与审计
- GET 族 = CapSecurityRead；POST/PUT/DELETE/verify/generate/v2 关联 = CapSecurityWrite。
- 审计动作字面量：`keypair.create`、`keypair.update`、`keypair.generate`、`keypair.delete`、`keypair.verify`、`keypair.associate`（ADR 指定 create/delete；其余为本规格补全——同 authconfig 先例的字面量姿态，Actions() picker 增补归 audit 域）。detail 仅携带 pairName/alias/actor/仓清单，**零密钥材料**。

### 3.5 删除护栏
DELETE 扫描仓配置引用（local deb/rpm 的 keyPairName）：非空引用集 → 400，文案点名引用仓清单。空 → 删除行，200 `OK`。

### 3.6 签名时序与 seam（T-321/T-322 消费面）
取行 → 主密钥解封两列 → openpgp 解析 → 口令解开私钥 → 签名 → **即弃**（零缓存、零日志密钥材料、零响应体携带）。错误分类（消费腿据此走「跳过签名 + 清旧签名」或失败）：

| 错误 | 语义 | 消费腿预期（rpm.md §4.3 / debian.md §4 同构） |
|---|---|---|
| `ErrNoKeypair` | 仓未关联（或关联字段空） | 跳过签名 + 清理既有旧签名文件 |
| `ErrUnavailable` | 无主密钥/行损坏/口令解不开 | 同上（记 WARN——配置态问题非内容态） |
| 其他（签名执行错） | openpgp 执行失败 | 失败浮出（签名腿票面裁量） |

seam 形态：`internal/keypair` 提供具体服务 `SigningService`（`DetachedArmor(ctx, repoKey, data) (string, error)` / `Clearsign(ctx, repoKey, data) ([]byte, error)` / `PublicKey(ctx, repoKey) (string, error)`）；消费侧（deb/rpm 适配器）按需自定义窄接口注入（Go 规范：接口在消费侧定义）。

- detached armor：`repomd.xml.asc` / `Release.gpg` 形态（RFC 4880 armor，公开规范为准）。
- clearsign：`InRelease` 形态（RFC 4880 cleartext signature framework）。

## 4. 验收锚（本票自测面）

1. CRUD/generate/verify/关联全矩阵（含 401/403/404/400 分支与 D-2 护栏）——`internal/httpapi/t319_keypair_test.go`。
2. 密封不变量：库内两列均为 `enc:v1:` 前缀；响应无钥材料字段——`internal/keypair` 测试。
3. 真实 gpg 客户端往返：服务端生成/导入的钥经 `gpg --import` 后 `--verify` 验 clearsign 与 detached 双形态——`internal/keypair` 集成测试（gpg 二进制缺席则 skip + 容器腿补）。
4. 仓配置矩阵（§3.3 六分支）——`internal/repo` 测试。
5. 启动 fail-fast（存量行 + 无主密钥）——`cmd` 接线测试。
