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
  }
}
