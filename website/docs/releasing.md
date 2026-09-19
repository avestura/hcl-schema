---
id: releasing
title: Releasing the extension
sidebar_position: 10
---

# Releasing the extension

Pushing a version tag publishes the VS Code extension to the Marketplace and
attaches the packages to a GitHub release.

```bash
# 1. Bump the version and write the changelog entry.
$EDITOR vsx/package.json vsx/CHANGELOG.md
git commit -am "chore(vsx): 0.2.0"

# 2. Tag and push.
git tag v0.2.0
git push origin main --tags
```

That is the whole process. `.github/workflows/release.yml` does the rest.

## What the workflow does

| Job | What it does |
| --- | --- |
| **verify** | Checks the tag against `vsx/package.json`, runs `go test ./...`, compiles and lints the extension |
| **package** | Builds all nine platform packages and checks each carries exactly one binary and the tagged version |
| **publish** | Publishes every package to the Marketplace in one call |
| **release** | Creates the GitHub release with the packages attached |

Nothing is published until the tests pass and the packages check out. A
marketplace version cannot be republished once it is live, so the ordering
matters more here than in ordinary CI.

## Tag format

| Tag | Result |
| --- | --- |
| `v1.2.3` | Stable release of version `1.2.3` |
| `v1.2.3-pre` | Version `1.2.3` published with `--pre-release` |

The version that reaches the Marketplace comes from `vsx/package.json`, not
from the tag. The **verify** job fails if they disagree, because a mismatch
would otherwise only surface after the release is live under the wrong number.

The Marketplace accepts only `major.minor.patch`, so a tag cannot carry a
semver suffix like `-rc.1`; `-pre` is a marker this workflow understands, not
part of the published version. VS Code convention also reserves odd minor
versions for pre-releases, and the workflow warns if you pre-release an even
one.

## Authentication

The workflow supports both methods and picks between them automatically:

- **A `VSCE_PAT` secret is set** → publishes with that Personal Access Token.
- **No `VSCE_PAT` secret** → publishes with OIDC trusted publishing.

:::warning Personal Access Tokens are being retired
Global PATs in Azure DevOps stop working on **1 December 2026**. Once trusted
publishing is set up, delete the `VSCE_PAT` secret — that is the entire
migration, since the workflow then falls through to OIDC on its own.
:::

### OIDC trusted publishing (recommended)

`vsce` asks GitHub Actions for an OIDC token and exchanges it with the
Marketplace directly. There is no stored credential to leak or rotate.

Much of Microsoft's published guidance describes an Azure Pipelines flow
involving a service connection, a user-assigned managed identity and federated
credentials. **None of that applies here.** `vsce` has native GitHub Actions
support, so the setup is:

1. Open the [publisher management page](https://marketplace.visualstudio.com/manage/publishers/avestura).
2. Configure trusted publishing for the extension, naming the repository
   (`avestura/hcl-schema`) and the workflow (`release.yml`).
3. Delete the `VSCE_PAT` secret if one exists.

The workflow already grants the `id-token: write` permission that the token
request needs.

:::note
`vsce`'s `--oidc` flag is present in version 4.0.0 but not yet listed in
`vsce publish --help`, so treat trusted publishing as newer ground than the PAT
path. If the exchange fails, set a `VSCE_PAT` secret to fall back while you
sort it out.
:::

### Personal Access Token (works until 1 December 2026)

1. Create a token following
   [the VS Code documentation](https://code.visualstudio.com/api/working-with-extensions/publishing-extension#_get-a-personal-access-token):
   an Azure DevOps token, **all accessible organizations**, scope
   **Marketplace → Manage**.
2. Add it as the `VSCE_PAT` repository secret.

The workflow logs a warning on every run that uses a PAT, so the deadline does
not go unnoticed.

## Approval before publishing

The publish job runs in a GitHub environment named `marketplace`. It is created
automatically and, by default, imposes no restrictions.

Because a published version cannot be replaced, it is worth adding a gate:
**Settings → Environments → marketplace → Required reviewers**. Every publish
then waits for a human, while everything before it — tests, packaging, the
content checks — still runs unattended.

If you would rather not have the gate at all, remove the `environment:
marketplace` line from the publish job.

## Rehearsing a release

Run the workflow by hand from **Actions → Release extension → Run workflow**,
with a tag and **dry run** left on. It verifies the tag, runs the tests and
builds all nine packages, then stops before publishing. The packages are
attached to the run as an artifact so you can install one and try it:

```bash
code --install-extension hcl-schema-0.2.0-linux-x64.vsix
```

## If a release goes wrong

A Marketplace version cannot be republished. `vsce unpublish` removes the whole
extension, not one version, so it is almost never what you want.

Release a new patch version instead: fix, bump `vsx/package.json`, tag
`v0.2.1`. The workflow passes `--skip-duplicate`, so re-running a job for a
version that is already live succeeds quietly rather than failing — which makes
a partial failure safe to retry.

## Publishing by hand

If the workflow is unavailable:

```bash
cd vsx
yarn install
yarn package:all
./node_modules/.bin/vsce publish --packagePath dist/*.vsix
```

Publish every package or none. Per-platform publishing has no universal
fallback, so a partial publish leaves the missing platforms with nothing
installable.

## The Go CLI

`go install` serves the CLI from the module proxy, so a tag makes the new
version available without any publishing step:

```bash
go install github.com/avestura/hcl-schema/cmd/hclschema-cli@v0.2.0
```

If you would rather also ship prebuilt CLI binaries, add a job that
cross-compiles `./cmd/hclschema-cli` and attaches the results to the same
release — `vsx/scripts/build-go-cli.js` already builds every target.
