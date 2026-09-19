---
id: editors
title: Editors
sidebar_position: 6
---

# Editors

## VS Code

Install
[avestura.hcl-schema](https://marketplace.visualstudio.com/items?itemName=avestura.hcl-schema).
The binary for your platform ships with the extension, so there is nothing else
to install.

| Setting | Default | Meaning |
| --- | --- | --- |
| `hclSchema.cliPath` | `""` | Path to the binary. Empty uses the bundled one. |
| `hclSchema.strict` | `false` | Apply draft 2026-09 rules to older schemas |
| `hclSchema.offline` | `false` | Never fetch remote schemas |
| `hclSchema.cacheDir` | `""` | Where cached remote schemas live |

Commands: **HCL Schema: Restart language server**, **Show output**, and
**Validate active file**.

## The language server

Any LSP client can use it:

```bash
hclschema lsp
```

| Capability | What it gives you |
| --- | --- |
| `publishDiagnostics` | Validation of the **unsaved buffer** |
| `completion` | Declared attributes and blocks; enum values on an attribute |
| `hover` | The `description`, type, bounds and default from the schema |
| `definition` | Jump from a use in the instance to its declaration in the schema |
| `documentSymbol` | An outline of the document |

:::info Why a server rather than a linter invocation
The previous integration ran the CLI against the file **on disk** on every
keystroke, so unsaved edits were checked against stale content. The server
holds the buffer and checks that.
:::

### Neovim

```lua
vim.api.nvim_create_autocmd('FileType', {
  pattern = 'hcl',
  callback = function(args)
    vim.lsp.start({
      name = 'hclschema',
      cmd = { 'hclschema-cli', 'lsp' },
      root_dir = vim.fs.root(args.buf, { '.git' }),
    })
  end,
})
```

### Helix

```toml
# languages.toml
[language-server.hclschema]
command = "hclschema-cli"
args = ["lsp"]

[[language]]
name = "hcl"
language-servers = ["hclschema"]
```

### Anything else

The server speaks LSP over stdio with `Content-Length` framing. Point your
client at `hclschema-cli lsp` and give it the `hcl` language id. It advertises
full-document sync, so there is no incremental-sync support to negotiate.
