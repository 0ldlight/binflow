/**
 * Sidebar = the five-category information architecture (PRD FR-41).
 *
 * Doc ids are file paths under docs/user/ without extension; positions
 * come from each file's frontmatter sidebar_position (docker 10, maven/npm/
 * pypi 20-22, console 30, admin 40-43, faq 90) so the hand-written grouping
 * and the frontmatter order can never disagree about sequence.
 *
 * The 安装指南 (7 forms) and API 参考 categories land with the content
 * matrix tickets (T-141/T-142) — their seams are marked below. Five
 * categories exist at GA (G20); today the tree carries what docs/user has.
 */
/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  main: [
    { type: 'doc', id: 'README', label: '文档中心首页' },

    // --- 客户端接入 (docker + the four protocol guides) ---
    {
      type: 'category',
      label: '客户端接入',
      link: {
        type: 'generated-index',
        title: '客户端接入',
        description: '各包管理器与客户端接入 BinFlow 的方式。',
        slug: '/integrations',
      },
      items: [
        'docker-registry',
        'integrations/maven',
        'integrations/npm',
        'integrations/pypi',
        // generic(raw) 接入篇 lands with T-141 (FR-41 内容矩阵).
      ],
    },

    // --- 管理指南 (console + admin/) ---
    {
      type: 'category',
      label: '管理指南',
      link: {
        type: 'generated-index',
        title: '管理指南',
        description: '实例的日常管理与运维。',
        slug: '/admin',
      },
      items: [
        'console',
        'admin/remote-virtual',
        'admin/groups-permissions',
        'admin/governance',
        'admin/backup-restore',
      ],
    },

    'faq',

    // --- 安装指南 (7 deployment forms + upgrade notes) lands with T-141 ---
    // {
    //   type: 'category', label: '安装指南',
    //   link: { type: 'generated-index', slug: '/install', ... },
    //   items: ['install/binary', 'install/docker', 'install/compose',
    //           'install/helm', 'install/k8s', 'install/systemd',
    //           'install/offline', 'install/upgrade'],
    // },

    { type: 'doc', id: 'api-reference', label: 'API 参考' },
  ],
}

module.exports = sidebars
