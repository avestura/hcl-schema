# HCL Schema for VS Code

Validation, completion and hover for HCL files described by a `*.schema.hcl`
schema.

Documentation: <https://avestura.github.io/hcl-schema/>

## What it does

Add a `__schema` attribute to an HCL file:

```hcl
__schema = "./service.schema.hcl"

name = "checkout"

listener "https" {
  port = 443
}
```

and the extension gives you, against that schema:

- **Diagnostics** as you type, on the unsaved buffer
- **Completion** of declared attributes and blocks, and of an attribute's
  permitted values
- **Hover** documentation written once in the schema's `description`
- **Go to definition** from a use to its declaration in the schema
- A **document outline**

![Screenshot](assets/screenshot/vscode-scrshot.png)

## Settings

| Setting | Default | Meaning |
| --- | --- | --- |
| `hclSchema.cliPath` | `""` | Path to the `hclschema` binary. Empty uses the bundled one. |
| `hclSchema.strict` | `false` | Apply draft 2026-09 rules to schemas that pin an older draft |
| `hclSchema.offline` | `false` | Never fetch remote schemas; use the cache only |
| `hclSchema.cacheDir` | `""` | Where cached remote schemas live |

## Commands

- **HCL Schema: Validate active file**
- **HCL Schema: Restart language server**
- **HCL Schema: Show output**

## Requirements

None. The binary for your platform ships with the extension. The
[HashiCorp HCL](https://marketplace.visualstudio.com/items?itemName=HashiCorp.HCL)
extension is installed alongside it for syntax highlighting.

## License

MIT.

## Releasing

Pushing a version tag publishes to the Marketplace and attaches the packages to
a GitHub release:

```bash
$EDITOR package.json CHANGELOG.md   # bump the version, write the entry
git commit -am "chore(vsx): 0.2.0"
git tag v0.2.0 && git push origin main --tags
```

`.github/workflows/release.yml` verifies the tag against `package.json`, runs
the tests, builds all nine platform packages, checks each carries exactly one
binary, then publishes. Full details, including trusted-publishing setup, are
in [Releasing the extension](https://avestura.github.io/hcl-schema/releasing).
