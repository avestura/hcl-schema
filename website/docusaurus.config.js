// @ts-check
const { themes } = require('prism-react-renderer');

const organizationName = 'avestura';
const projectName = 'hcl-schema';
const marketplaceUrl =
  'https://marketplace.visualstudio.com/items?itemName=avestura.hcl-schema';

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'HCL Schema',
  tagline: 'Describe the shape of an HCL file, in HCL',
  favicon: 'img/favicon.svg',

  // Served from GitHub Pages at https://<org>.github.io/<project>/.
  url: `https://${organizationName}.github.io`,
  baseUrl: `/${projectName}/`,
  organizationName,
  projectName,
  deploymentBranch: 'gh-pages',
  trailingSlash: false,

  onBrokenLinks: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          // Docs-only mode: the site is a manual, not a blog with a manual
          // attached.
          routeBasePath: '/',
          sidebarPath: require.resolve('./sidebars.js'),
          editUrl: `https://github.com/${organizationName}/${projectName}/tree/main/website/`,
        },
        blog: false,
        theme: {
          customCss: require.resolve('./src/css/custom.css'),
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/social-card.png',
      colorMode: {
        respectPrefersColorScheme: true,
      },
      navbar: {
        title: 'HCL Schema',
        logo: {
          alt: 'HCL Schema',
          src: 'img/logo.svg',
        },
        items: [
          { to: '/', label: 'Docs', position: 'left', activeBaseRegex: '^/hcl-schema/$' },
          { to: '/schema-language/attributes', label: 'Schema language', position: 'left' },
          { to: '/editors', label: 'VS Code', position: 'left' },
          { to: '/cli', label: 'CLI', position: 'left' },
          { to: '/go-library', label: 'Go API', position: 'left' },
          {
            href: marketplaceUrl,
            label: 'Install for VS Code',
            position: 'right',
            className: 'navbar-install-link',
          },
          {
            href: `https://github.com/${organizationName}/${projectName}`,
            label: 'GitHub',
            position: 'right',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Docs',
            items: [
              { label: 'Getting started', to: '/getting-started' },
              { label: 'Schema language', to: '/schema-language/attributes' },
              { label: 'Drafts', to: '/drafts' },
            ],
          },
          {
            title: 'Tools',
            items: [
              { label: 'VS Code extension', href: marketplaceUrl },
              { label: 'Editor setup', to: '/editors' },
              { label: 'CLI', to: '/cli' },
              { label: 'Go library', to: '/go-library' },
            ],
          },
          {
            title: 'More',
            items: [
              { label: 'GitHub', href: `https://github.com/${organizationName}/${projectName}` },
              { label: 'Security model', to: '/security' },
              { label: 'CI recipes', to: '/ci' },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} Avestura. MIT licensed.`,
      },
      prism: {
        theme: themes.github,
        darkTheme: themes.dracula,
        additionalLanguages: ['hcl', 'bash', 'go', 'json', 'yaml'],
      },
    }),
};

module.exports = config;
