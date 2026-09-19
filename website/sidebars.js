// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docs: [
    'intro',
    'getting-started',
    {
      type: 'category',
      label: 'Schema language',
      collapsed: false,
      items: [
        'schema-language/attributes',
        'schema-language/blocks',
        'schema-language/reuse',
        'schema-language/variants',
      ],
    },
    'drafts',
    'cli',
    'go-library',
    'editors',
    'ci',
    'security',
    'reference/meta-schema',
  ],
};

module.exports = sidebars;
