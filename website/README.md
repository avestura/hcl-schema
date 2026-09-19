# Documentation site

The [hcl-schema documentation](https://avestura.github.io/hcl-schema/), built
with [Docusaurus](https://docusaurus.io/) and published to GitHub Pages.

## Running locally

```bash
yarn install
yarn start
```

## Building

```bash
yarn build      # static site in build/
yarn serve      # serve the built site
```

## The generated reference

[`docs/reference/meta-schema.md`](./docs/reference/meta-schema.md) is produced
from the meta-schema by the project's own `hclschema docs` command, so the
reference cannot drift from what the tool actually accepts.

```bash
node scripts/gen-reference.js     # needs Go on PATH
```

The result is committed, so the site builds without Go. CI regenerates it and
fails if the committed copy is stale.

## Publishing

`.github/workflows/docs.yml` builds and deploys on every push to `main` that
touches `website/`, `schema/` or the docs generator, and can be run manually
from the Actions tab.

One-time repository setup: **Settings → Pages → Build and deployment →
Source: GitHub Actions**.

The site is served from `/hcl-schema/`, which `baseUrl` in
`docusaurus.config.js` reflects. To publish to a different path or a custom
domain, change `url`, `baseUrl`, `organizationName` and `projectName` there,
and add a `static/CNAME` file for a custom domain.
