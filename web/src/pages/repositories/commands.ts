// 客户端接入命令块（P3：console-ux §4.5[1]——命令内容与 docs/user 各接入
// 文档同源，UI 不发明新命令；来源：docker-registry.md §2~§4、
// integrations/maven.md §2~§4、integrations/npm.md §2~§3、
// integrations/pypi.md §2~§3；generic 的 curl roundtrip 是 M1 FR-4 契约
// 形态（generic.md 未写篇，见工作日志）。
//
// host 取当前请求 origin（§4.5：server.base_url 无查询端点，按请求推导
// ——与后端 requestBase 的 scheme://host 口径一致）。

import type { PackageType } from '../../lib/repos'
import { tr } from '../../i18n'

const t = tr('repositories')

export interface CommandBlock {
  title: string
  /** 代码块语言标注（§8：命令块保留英文原文 lang="en"） */
  lang: string
  text: string
  note?: string
}

function npmAuthLine(origin: string, repoKey: string): string {
  // .npmrc 凭据行必须按 registry 限定且不带 scheme（npm 10 实测坑，npm.md §2）
  return `//${origin.replace(/^https?:\/\//, '')}/binflow/api/npm/${repoKey}/`
}

// ---- Set Me Up 对话框数据源（T-242，console-m8 §4.1 / reverse §4.1） ----------
//
// clientCommands（上方，仓库详情页/树页命令块）保持原样；对话框族消费下面
// 的导出：包类型网格元数据 + 按凭据参数化的 Configure/Deploy/Resolve 三侧
// 命令（T-382 三 Tab 重组，映射见 smuResolveCommands 头部注记）。
// 凭据规则（§4.1）：铸币成功前给占位（<USERNAME> / <TOKEN 或口令>），成功后
// 由对话框回填真实 username/token（token 只在明文面板期存在，见组件注记）。

export interface ClientCreds {
  username: string
  /** docker login / curl -u / settings.xml / .pypirc 的口令位（口令或 token） */
  secret: string
  /** .npmrc 的 _auth 值（base64 of username:secret） */
  npmAuth: string
}

/** 未铸币时的占位凭据（§4.1：非 admin 凭据位给占位；admin 铸币前同形） */
export function placeholderCreds(username: string): ClientCreds {
  return {
    username: username || '<USERNAME>',
    secret: t('<TOKEN 或口令>'),
    npmAuth: t('<base64 of 用户名:令牌>'),
  }
}

/** 包类型网格（console-m8 C7 五项闭集；图标不在元数据里——SetMeUp 药丸
 *  的 PkgIcon 按 id 解析，T-390 起五枚几何字符图标退役） */
export const CLIENT_PKG_META: { id: PackageType; label: string; desc: string }[] = [
  { id: 'generic', label: 'Generic', desc: t('任意文件（curl / CI 脚本直传）') },
  { id: 'docker', label: 'Docker', desc: t('OCI 镜像（docker login / push）') },
  { id: 'maven', label: 'Maven', desc: t('JVM 构件（settings.xml + mvn deploy）') },
  { id: 'npm', label: 'npm', desc: t('Node 包（.npmrc + npm publish）') },
  { id: 'pypi', label: 'PyPI', desc: t('Python 包（pip.conf / twine）') },
]

/** 门控包型回退块（M10 T-288）：命令块内容与 docs/user 同源（文件头注），
 *  go/nuget/cargo 的接入文档随文档票（T-296 面）落地——UI 不发明命令，
 *  给指路块兜底（旧穷尽 switch 在门控仓上落 undefined 会击穿渲染）。 */
function gatedPkgBlock(packageType: PackageType, repoKey: string): CommandBlock[] {
  return [
    {
      title: t('客户端接入'),
      lang: 'bash',
      text: [t('# {packageType} 仓库 {repoKey}：接入命令暂未收入控制台，见帮助文档（docs/user）。', { packageType: packageType, repoKey: repoKey })].join('\n'),
    },
  ]
}

// T-382 三 Tab 重组（console-artifactory-parity D1 v1.1）：Configure/Deploy
// 双侧改 Configure/Deploy/Resolve 三侧，按语义三分（映射留痕见组件头注）：
//   Configure = 客户端初始配置（端点 + 凭据：docker login / settings.xml
//               servers / .npmrc _auth）；generic/pypi 无配置步（curl/pip
//               开箱即用）→ 空数组，由组件给导航提示
//   Deploy    = 推（发布命令，原 Deploy 侧原样）
//   Resolve   = 拉（原 Configure 侧的解析/下载/安装命令迁入）

/** Configure 侧（客户端配置）：把客户端指向 BinFlow 并完成登录 */
export function smuConfigureCommands(packageType: PackageType, repoKey: string, creds: ClientCreds): CommandBlock[] {
  const origin = window.location.origin
  switch (packageType) {
    case 'generic':
      // curl 无配置步（匿名读默认开）——下载/校验命令在 Resolve 侧
      return []
    case 'docker':
      return [
        {
          title: t('Docker 登录'),
          lang: 'bash',
          text: [`echo "${creds.secret}" | docker login ${origin} -u ${creds.username} --password-stdin`].join('\n'),
          note: t('登录一次后 push/pull 自动完成 Bearer 协商；明文 HTTP 需配置 insecure-registries（见 docs/user/docker-registry.md）。'),
        },
      ]
    case 'maven':
      return [
        {
          title: t('settings.xml（服务器定义）'),
          lang: 'xml',
          text: [
            `<!-- ~/.m2/settings.xml -->`,
            `<settings>`,
            `  <servers>`,
            `    <server>`,
            `      <id>binflow</id>`,
            `      <username>${creds.username}</username>`,
            `      <password>${creds.secret}</password>`,
            `    </server>`,
            `  </servers>`,
            `</settings>`,
          ].join('\n'),
          note: t('放置位置：${user.home}/.m2/settings.xml / ${maven.home}/conf/settings.xml / 自定义 -s settings.xml。'),
        },
      ]
    case 'npm':
      return [
        {
          title: t('.npmrc（项目根目录）'),
          lang: 'ini',
          text: [
            `registry=${origin}/binflow/api/npm/${repoKey}/`,
            `${npmAuthLine(origin, repoKey)}:_auth=${creds.npmAuth}`,
            `always-auth=true`,
          ].join('\n'),
          note: creds.npmAuth.startsWith('<')
            ? t('_auth 生成：printf \'%s:%s\' "<用户名>" "<令牌>" | base64；裸 _auth 会被 npm 10 拒绝，凭据行必须带 //host/路径/ 前缀。')
            : t('凭据行必须带 //host/路径/ 前缀（npm 10 实测坑，npm.md §2）。'),
        },
      ]
    case 'pypi':
      // pip 无登录步（解析配置 pip.conf 在 Resolve 侧；发布凭据 .pypirc 在
      // Deploy 侧）——Configure 无命令块
      return []
    default:
      return gatedPkgBlock(packageType, repoKey)
  }
}

/** Resolve 侧（解析/拉取）：从 BinFlow 解析与下载制品（凭据在 Configure 侧
 *  一次配置——docker login / settings.xml / .npmrc；此处命令不内嵌凭据） */
export function smuResolveCommands(packageType: PackageType, repoKey: string): CommandBlock[] {
  const origin = window.location.origin
  switch (packageType) {
    case 'generic':
      return [
        {
          title: t('下载与校验（curl）'),
          lang: 'bash',
          text: [
            t('# 下载与校验（响应头携带服务端实测 sha256；匿名读默认开）'),
            `curl -sI ${origin}/binflow/${repoKey}/acme/app.tar.gz | grep -i x-checksum-sha256`,
            `curl -O ${origin}/binflow/${repoKey}/acme/app.tar.gz`,
          ].join('\n'),
          note: t('需要认证的读路径加 -u <用户名>:<令牌>。'),
        },
      ]
    case 'docker':
      return [
        {
          title: t('拉取镜像'),
          lang: 'bash',
          text: [`docker pull ${origin}/${repoKey}/acme/app:v1`].join('\n'),
          note: t('镜像名首段是仓库 key（单段 name 404）；登录见「配置」Tab。'),
        },
      ]
    case 'maven':
      return [
        {
          title: t('解析：pom <repositories>'),
          lang: 'xml',
          text: [
            `<repositories>`,
            `  <repository>`,
            `    <id>bf</id>`,
            `    <url>${origin}/binflow/${repoKey}</url>`,
            `  </repository>`,
            `</repositories>`,
          ].join('\n'),
          note: t('团队统一出口建议 settings.xml <mirror> 收口到 virtual 仓（见 docs/user/integrations/maven.md §4）。'),
        },
      ]
    case 'npm':
      return [
        {
          title: t('安装与验证'),
          lang: 'bash',
          text: [
            t('# registry 已在 Configure 侧的 .npmrc 指向本仓（配置不跨目录继承）'),
            `npm install demo-pkg`,
            `npm cache clean --force && rm -rf node_modules package-lock.json`,
            `npm install demo-pkg && node -e 'console.log(require("demo-pkg"))'`,
          ].join('\n'),
          note: t('consumer 目录也需要 .npmrc（registry 配置不继承，缺省走公网——npm.md §4 实测坑）。'),
        },
      ]
    case 'pypi':
      return [
        {
          title: t('安装侧：pip.conf'),
          lang: 'ini',
          text: [`[global]`, `index-url = ${origin}/binflow/api/pypi/${repoKey}/simple`].join('\n'),
          note: t('一次性用法：pip install --index-url <index-url> <包名>。'),
        },
      ]
    default:
      return gatedPkgBlock(packageType, repoKey)
  }
}

/** Deploy 侧（发布）：把制品推到该仓库 */
export function smuDeployCommands(packageType: PackageType, repoKey: string, creds: ClientCreds): CommandBlock[] {
  const origin = window.location.origin
  switch (packageType) {
    case 'generic':
      return [
        {
          title: t('上传（curl PUT）'),
          lang: 'bash',
          text: [
            t('# 上传（deploy 需认证；同路径重传 = 覆盖）'),
            `curl -T app.tar.gz ${origin}/binflow/${repoKey}/acme/app.tar.gz -u ${creds.username}:${creds.secret}`,
          ].join('\n'),
          note: t('携带 X-Checksum-Sha256 请求头可做客户端校验与秒传；控制台内上传走「部署 Deploy」对话框。'),
        },
      ]
    case 'docker':
      return [
        {
          title: t('构建与推送'),
          lang: 'bash',
          text: [
            `docker build -t ${origin}/${repoKey}/acme/app:v1 .`,
            `docker push ${origin}/${repoKey}/acme/app:v1`,
          ].join('\n'),
          note: t('登录见「配置」Tab；docker 是三步会话协议，不走浏览器上传。'),
        },
      ]
    case 'maven':
      return [
        {
          title: t('发布命令（id 与 settings.xml 一致）'),
          lang: 'bash',
          text: `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::${origin}/binflow/${repoKey}`,
          note: t('服务器 id「binflow」必须与 Configure Tab 的 settings.xml <id> 一致。'),
        },
      ]
    case 'npm':
      return [
        {
          title: t('发布与验证'),
          lang: 'bash',
          text: [`cd my-pkg && npm publish`, `npm whoami        # ${creds.username}`, `npm view demo-pkg version`].join('\n'),
        },
      ]
    case 'pypi':
      return [
        {
          title: t('发布侧：.pypirc + twine'),
          lang: 'ini',
          text: [
            `# ~/.pypirc`,
            `[distutils]`,
            `index-servers =`,
            `    binflow`,
            ``,
            `[binflow]`,
            `repository = ${origin}/binflow/api/pypi/${repoKey}`,
            `username = ${creds.username}`,
            `password = ${creds.secret}`,
          ].join('\n'),
          note: t('上传：pip wheel . -w dist/（或 python -m build）→ twine upload --repository binflow dist/*。'),
        },
      ]
    default:
      return gatedPkgBlock(packageType, repoKey)
  }
}

export function clientCommands(packageType: PackageType, repoKey: string): CommandBlock[] {
  const origin = window.location.origin
  switch (packageType) {
    case 'generic':
      return [
        {
          title: t('上传 / 下载（curl）'),
          lang: 'bash',
          text: [
            t('# 上传（deploy 需认证；匿名读默认开）'),
            `curl -T app.tar.gz ${origin}/binflow/${repoKey}/acme/app.tar.gz -u admin`,
            ``,
            t('# 下载与校验（响应头携带服务端实测 sha256）'),
            `curl -sI ${origin}/binflow/${repoKey}/acme/app.tar.gz | grep -i x-checksum-sha256`,
            `curl -O ${origin}/binflow/${repoKey}/acme/app.tar.gz`,
          ].join('\n'),
          note: t('同路径重传 = 覆盖；携带 X-Checksum-Sha256 请求头可做客户端校验与秒传。'),
        },
      ]
    case 'docker':
      return [
        {
          title: t('Docker 登录与推送'),
          lang: 'bash',
          text: [
            `echo "$ADMIN_PW" | docker login ${origin} -u admin --password-stdin`,
            `docker build -t ${origin}/${repoKey}/acme/app:v1 .`,
            `docker push ${origin}/${repoKey}/acme/app:v1`,
            `docker pull ${origin}/${repoKey}/acme/app:v1`,
          ].join('\n'),
          note: t('镜像名首段是仓库 key（单段 name 404）；明文 HTTP 需配置 insecure-registries（见 docs/user/docker-registry.md）。'),
        },
      ]
    case 'maven':
      return [
        {
          title: t('发布：settings.xml + altDeploymentRepository'),
          lang: 'xml',
          text: [
            `<!-- ~/.m2/settings.xml -->`,
            `<settings>`,
            `  <servers>`,
            `    <server>`,
            `      <id>binflow</id>`,
            `      <username>admin</username>`,
            `      <password>$ADMIN_PW</password>`,
            `    </server>`,
            `  </servers>`,
            `</settings>`,
          ].join('\n'),
        },
        {
          title: t('发布命令（id 与首段一致）'),
          lang: 'bash',
          text: `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::${origin}/binflow/${repoKey}`,
        },
        {
          title: t('解析：pom <repositories>'),
          lang: 'xml',
          text: [
            `<repositories>`,
            `  <repository>`,
            `    <id>bf</id>`,
            `    <url>${origin}/binflow/${repoKey}</url>`,
            `  </repository>`,
            `</repositories>`,
          ].join('\n'),
          note: t('团队统一出口建议 settings.xml <mirror> 收口到 virtual 仓（见 docs/user/integrations/maven.md §4）。'),
        },
      ]
    case 'npm':
      return [
        {
          title: t('.npmrc（项目根目录）'),
          lang: 'ini',
          text: [
            `registry=${origin}/binflow/api/npm/${repoKey}/`,
            t('{v1}:_auth=<base64 of admin:口令>', { v1: npmAuthLine(origin, repoKey) }),
            `always-auth=true`,
          ].join('\n'),
          note: t('_auth 生成：printf \'admin:%s\' "$ADMIN_PW" | base64；裸 _auth 会被 npm 10 拒绝，凭据行必须带 //host/路径/ 前缀。'),
        },
        {
          title: t('发布与验证'),
          lang: 'bash',
          text: [`cd my-pkg && npm publish`, `npm whoami        # admin`, `npm view demo-pkg version`].join('\n'),
        },
      ]
    case 'pypi':
      return [
        {
          title: t('安装侧：pip.conf'),
          lang: 'ini',
          text: [`[global]`, `index-url = ${origin}/binflow/api/pypi/${repoKey}/simple`].join('\n'),
          note: t('一次性用法：pip install --index-url <index-url> <包名>。'),
        },
        {
          title: t('发布侧：.pypirc + twine'),
          lang: 'ini',
          text: [
            `[distutils]`,
            `index-servers =`,
            `    binflow`,
            ``,
            `[binflow]`,
            `repository = ${origin}/binflow/api/pypi/${repoKey}`,
            `username = admin`,
            t('password = <你的管理员口令>'),
          ].join('\n'),
        },
        {
          title: t('上传命令'),
          lang: 'bash',
          text: [t('pip wheel . -w dist/    # 或 python -m build'), `twine upload --repository binflow dist/*`].join('\n'),
        },
      ]
    default:
      return gatedPkgBlock(packageType, repoKey)
  }
}
