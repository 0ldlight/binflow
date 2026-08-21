/**
 * BinFlow help docs site (T-129 scaffold; ADR-0011 + T-130 K1 final ruling).
 *
 * Delivery shape (ADR-0011): `make docs` builds this into a static tree that
 * is copied verbatim into internal/docs/dist for go:embed — the binary
 * serves it at /binflow/docs/. Self-hosted assets are natively prefixed by
 * baseUrl (ADR-0011 增补②): zero post-processing, zero CDN, zero runtime
 * fetches (NFR-S29).
 *
 * Workflow (ADR-0011 workflow clause): tech-writers only ever edit
 * docs/user/*.md — this directory is build plumbing, not content.
 */
const { themes } = require('prism-react-renderer')

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'BinFlow 帮助文档',
  tagline: '云原生制品仓库 — 安装、接入、管理与 API 参考',
  // Self-hosted delivery: the canonical host is wherever the binary runs.
  // url only feeds sitemap/canonical generation — nothing is fetched from
  // it at runtime.
  url: 'http://localhost:8080',
  // The mount segment (ADR-0011). Docusaurus prefixes every asset and page
  // link from here, so the embed needs no path rewriting (T-130 增补② —
  // the console's relink-assets step has no docs-side counterpart).
  baseUrl: '/binflow/docs/',
  // docs/user/README.md (the nav index) links to matrix pages that land with
  // T-141/T-142; until then broken CONTENT links warn instead of failing
  // the build. Flip to 'throw' when the content matrix is complete.
  onBrokenLinks: 'warn',
  markdown: {
    hooks: {
      // Same posture as onBrokenLinks, in the Docusaurus 3.10 shape
      // (siteConfig.onBrokenMarkdownLinks is deprecated since 3.9).
      onBrokenMarkdownLinks: 'warn',
    },
  },
  favicon: 'img/favicon.svg',
  // zh is the ONLY locale for GA (PRD FR-41); en is M6+ debt. The i18n
  // skeleton (locale-aware config shape) is in place for that day.
  i18n: {
    defaultLocale: 'zh',
    locales: ['zh'],
  },
  themes: [
    // Offline full-text search (K1 final ruling, ADR-0011 增补①/T-130):
    // Docusaurus's built-in search indexes metadata only — body keywords
    // like 「配额」 would be structurally unreachable (G18). This theme
    // builds a local index at build time; the browser searches it offline.
    // zh tokenization rides the nodejieba dependency (build-time native
    // addon — never enters the Go binary or the runtime image, ADR-0005).
    [
      '@easyops-cn/docusaurus-search-local',
      {
        // Index filenames carry a content hash, so deploys cannot serve a
        // stale index under a fresh binary (the handler caches non-asset
        // files with no-cache; the hash makes even proxies safe).
        hashed: true,
        language: ['zh', 'en'],
        highlightSearchTermsOnTargetPage: true,
      },
    ],
  ],
  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      {
        docs: {
          // Content source of truth — the only place tech-writers write.
          path: '../docs/user',
          // Docs-only site: the segment root IS the site root (no landing
          // page of its own).
          routeBasePath: '/',
          sidebarPath: require.resolve('./sidebars.js'),
          showLastUpdateAuthor: false,
          showLastUpdateTime: false,
          // Versioning (K1 增补③): v1.x is the first version directory
          // (versioned_docs/version-v1.x, cut by `npm run version v1.x`).
          // The latest version serves at the segment root with NO version
          // segment in its path; the live docs/user tree rides /next/
          // until the release cut refreshes the snapshot.
          includeCurrentVersion: true,
          versions: {
            current: { label: '开发中 (next)' },
          },
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      },
    ],
  ],
  themeConfig: {
    navbar: {
      title: 'BinFlow 帮助文档',
      items: [
        // G22 (P1, formal coverage in T-146): the dropdown carries v1.x.
        { type: 'docsVersionDropdown', position: 'left' },
      ],
    },
    footer: {
      style: 'dark',
      links: [],
      copyright: 'BinFlow 帮助文档 — 随二进制交付，离线可用（ADR-0011）',
    },
    prism: {
      theme: themes.github,
      darkTheme: themes.dracula,
    },
    colorMode: {
      defaultMode: 'light',
      respectPrefersColorScheme: true,
    },
  },
}

module.exports = config
