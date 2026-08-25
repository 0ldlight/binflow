// 客户端接入命令块（P3：console-ux §4.5[1]——命令内容与 docs/user 各接入
// 文档同源，UI 不发明新命令；来源：docker-registry.md §2~§4、
// integrations/maven.md §2~§4、integrations/npm.md §2~§3、
// integrations/pypi.md §2~§3；generic 的 curl roundtrip 是 M1 FR-4 契约
// 形态（generic.md 未写篇，见工作日志）。
//
// host 取当前请求 origin（§4.5：server.base_url 无查询端点，按请求推导
// ——与后端 requestBase 的 scheme://host 口径一致）。

import type { PackageType } from '../../lib/repos'

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
// 三个导出：包类型网格元数据 + 按凭据参数化的 Configure/Deploy 两侧命令。
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
    secret: '<TOKEN 或口令>',
    npmAuth: '<base64 of 用户名:令牌>',
  }
}

/** 包类型网格（console-m8 C7 五项闭集；图标与树页 PKG_ICON 同形） */
export const CLIENT_PKG_META: { id: PackageType; label: string; icon: string; desc: string }[] = [
  { id: 'generic', label: 'Generic', icon: '▫', desc: '任意文件（curl / CI 脚本直传）' },
  { id: 'docker', label: 'Docker', icon: '⬢', desc: 'OCI 镜像（docker login / push）' },
  { id: 'maven', label: 'Maven', icon: '⌬', desc: 'JVM 构件（settings.xml + mvn deploy）' },
  { id: 'npm', label: 'npm', icon: '⬒', desc: 'Node 包（.npmrc + npm publish）' },
  { id: 'pypi', label: 'PyPI', icon: '⬓', desc: 'Python 包（pip.conf / twine）' },
]

/** 门控包型回退块（M10 T-288）：命令块内容与 docs/user 同源（文件头注），
 *  go/nuget/cargo 的接入文档随文档票（T-296 面）落地——UI 不发明命令，
 *  给指路块兜底（旧穷尽 switch 在门控仓上落 undefined 会击穿渲染）。 */
function gatedPkgBlock(packageType: PackageType, repoKey: string): CommandBlock[] {
  return [
    {
      title: '客户端接入',
      lang: 'bash',
      text: [`# ${packageType} 仓库 ${repoKey}：接入命令暂未收入控制台，见帮助文档（docs/user）。`].join('\n'),
    },
  ]
}

/** Configure 侧（解析/拉取）：把客户端指向 BinFlow 并完成登录 */
export function smuConfigureCommands(packageType: PackageType, repoKey: string, creds: ClientCreds): CommandBlock[] {
  const origin = window.location.origin
  switch (packageType) {
    case 'generic':
      return [
        {
          title: '下载与校验（curl）',
          lang: 'bash',
          text: [
            `# 下载与校验（响应头携带服务端实测 sha256；匿名读默认开）`,
            `curl -sI ${origin}/binflow/${repoKey}/acme/app.tar.gz | grep -i x-checksum-sha256`,
            `curl -O ${origin}/binflow/${repoKey}/acme/app.tar.gz`,
          ].join('\n'),
          note: '需要认证的读路径加 -u <用户名>:<令牌>。',
        },
      ]
    case 'docker':
      return [
        {
          title: 'Docker 登录与拉取',
          lang: 'bash',
          text: [
            `echo "${creds.secret}" | docker login ${origin} -u ${creds.username} --password-stdin`,
            `docker pull ${origin}/${repoKey}/acme/app:v1`,
          ].join('\n'),
          note: '镜像名首段是仓库 key（单段 name 404）；明文 HTTP 需配置 insecure-registries（见 docs/user/docker-registry.md）。',
        },
      ]
    case 'maven':
      return [
        {
          title: 'settings.xml（服务器定义）',
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
          note: '放置位置：${user.home}/.m2/settings.xml / ${maven.home}/conf/settings.xml / 自定义 -s settings.xml。',
        },
        {
          title: '解析：pom <repositories>',
          lang: 'xml',
          text: [
            `<repositories>`,
            `  <repository>`,
            `    <id>bf</id>`,
            `    <url>${origin}/binflow/${repoKey}</url>`,
            `  </repository>`,
            `</repositories>`,
          ].join('\n'),
          note: '团队统一出口建议 settings.xml <mirror> 收口到 virtual 仓（见 docs/user/integrations/maven.md §4）。',
        },
      ]
    case 'npm':
      return [
        {
          title: '.npmrc（项目根目录）',
          lang: 'ini',
          text: [
            `registry=${origin}/binflow/api/npm/${repoKey}/`,
            `${npmAuthLine(origin, repoKey)}:_auth=${creds.npmAuth}`,
            `always-auth=true`,
          ].join('\n'),
          note: creds.npmAuth.startsWith('<')
            ? '_auth 生成：printf \'%s:%s\' "<用户名>" "<令牌>" | base64；裸 _auth 会被 npm 10 拒绝，凭据行必须带 //host/路径/ 前缀。'
            : '凭据行必须带 //host/路径/ 前缀（npm 10 实测坑，npm.md §2）。',
        },
      ]
    case 'pypi':
      return [
        {
          title: '安装侧：pip.conf',
          lang: 'ini',
          text: [`[global]`, `index-url = ${origin}/binflow/api/pypi/${repoKey}/simple`].join('\n'),
          note: '一次性用法：pip install --index-url <index-url> <包名>。',
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
          title: '上传（curl PUT）',
          lang: 'bash',
          text: [
            `# 上传（deploy 需认证；同路径重传 = 覆盖）`,
            `curl -T app.tar.gz ${origin}/binflow/${repoKey}/acme/app.tar.gz -u ${creds.username}:${creds.secret}`,
          ].join('\n'),
          note: '携带 X-Checksum-Sha256 请求头可做客户端校验与秒传；控制台内上传走「部署 Deploy」对话框。',
        },
      ]
    case 'docker':
      return [
        {
          title: '构建与推送',
          lang: 'bash',
          text: [
            `docker build -t ${origin}/${repoKey}/acme/app:v1 .`,
            `docker push ${origin}/${repoKey}/acme/app:v1`,
          ].join('\n'),
          note: '登录见「配置」Tab；docker 是三步会话协议，不走浏览器上传。',
        },
      ]
    case 'maven':
      return [
        {
          title: '发布命令（id 与 settings.xml 一致）',
          lang: 'bash',
          text: `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::${origin}/binflow/${repoKey}`,
          note: '服务器 id「binflow」必须与 Configure Tab 的 settings.xml <id> 一致。',
        },
      ]
    case 'npm':
      return [
        {
          title: '发布与验证',
          lang: 'bash',
          text: [`cd my-pkg && npm publish`, `npm whoami        # ${creds.username}`, `npm view demo-pkg version`].join('\n'),
        },
      ]
    case 'pypi':
      return [
        {
          title: '发布侧：.pypirc + twine',
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
          note: '上传：pip wheel . -w dist/（或 python -m build）→ twine upload --repository binflow dist/*。',
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
          title: '上传 / 下载（curl）',
          lang: 'bash',
          text: [
            `# 上传（deploy 需认证；匿名读默认开）`,
            `curl -T app.tar.gz ${origin}/binflow/${repoKey}/acme/app.tar.gz -u admin`,
            ``,
            `# 下载与校验（响应头携带服务端实测 sha256）`,
            `curl -sI ${origin}/binflow/${repoKey}/acme/app.tar.gz | grep -i x-checksum-sha256`,
            `curl -O ${origin}/binflow/${repoKey}/acme/app.tar.gz`,
          ].join('\n'),
          note: '同路径重传 = 覆盖；携带 X-Checksum-Sha256 请求头可做客户端校验与秒传。',
        },
      ]
    case 'docker':
      return [
        {
          title: 'Docker 登录与推送',
          lang: 'bash',
          text: [
            `echo "$ADMIN_PW" | docker login ${origin} -u admin --password-stdin`,
            `docker build -t ${origin}/${repoKey}/acme/app:v1 .`,
            `docker push ${origin}/${repoKey}/acme/app:v1`,
            `docker pull ${origin}/${repoKey}/acme/app:v1`,
          ].join('\n'),
          note: '镜像名首段是仓库 key（单段 name 404）；明文 HTTP 需配置 insecure-registries（见 docs/user/docker-registry.md）。',
        },
      ]
    case 'maven':
      return [
        {
          title: '发布：settings.xml + altDeploymentRepository',
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
          title: '发布命令（id 与首段一致）',
          lang: 'bash',
          text: `mvn -B -DskipTests deploy -DaltDeploymentRepository=binflow::default::${origin}/binflow/${repoKey}`,
        },
        {
          title: '解析：pom <repositories>',
          lang: 'xml',
          text: [
            `<repositories>`,
            `  <repository>`,
            `    <id>bf</id>`,
            `    <url>${origin}/binflow/${repoKey}</url>`,
            `  </repository>`,
            `</repositories>`,
          ].join('\n'),
          note: '团队统一出口建议 settings.xml <mirror> 收口到 virtual 仓（见 docs/user/integrations/maven.md §4）。',
        },
      ]
    case 'npm':
      return [
        {
          title: '.npmrc（项目根目录）',
          lang: 'ini',
          text: [
            `registry=${origin}/binflow/api/npm/${repoKey}/`,
            `${npmAuthLine(origin, repoKey)}:_auth=<base64 of admin:口令>`,
            `always-auth=true`,
          ].join('\n'),
          note: '_auth 生成：printf \'admin:%s\' "$ADMIN_PW" | base64；裸 _auth 会被 npm 10 拒绝，凭据行必须带 //host/路径/ 前缀。',
        },
        {
          title: '发布与验证',
          lang: 'bash',
          text: [`cd my-pkg && npm publish`, `npm whoami        # admin`, `npm view demo-pkg version`].join('\n'),
        },
      ]
    case 'pypi':
      return [
        {
          title: '安装侧：pip.conf',
          lang: 'ini',
          text: [`[global]`, `index-url = ${origin}/binflow/api/pypi/${repoKey}/simple`].join('\n'),
          note: '一次性用法：pip install --index-url <index-url> <包名>。',
        },
        {
          title: '发布侧：.pypirc + twine',
          lang: 'ini',
          text: [
            `[distutils]`,
            `index-servers =`,
            `    binflow`,
            ``,
            `[binflow]`,
            `repository = ${origin}/binflow/api/pypi/${repoKey}`,
            `username = admin`,
            `password = <你的管理员口令>`,
          ].join('\n'),
        },
        {
          title: '上传命令',
          lang: 'bash',
          text: [`pip wheel . -w dist/    # 或 python -m build`, `twine upload --repository binflow dist/*`].join('\n'),
        },
      ]
    default:
      return gatedPkgBlock(packageType, repoKey)
  }
}
