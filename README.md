# HCL Schema

Describe the shape of an HCL file using HCL and check files against it.

This repository contains:

- a **Go library** for parsing `*.schema.hcl` documents, validating HCL against them, and decoding HCL into typed values;
- a **CLI** for validating, formatting, inferring, documenting and bundling schemas, plus a language server;
- a **VS Code extension** built on that language server.

Documentation: **<https://avestura.github.io/hcl-schema/>**

## Defining a schema

```hcl
# example.schema.hcl
__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "https://example.com/example.schema.hcl"

body {
  attribute "myattr" {
    type        = string
    required    = true
    description = "The name of the thing this file configures."
    pattern     = "^[a-z][a-z0-9-]*$"
  }

  block_header "tag" {
    label_names   = ["name1"]
    unique_labels = true
    max_items     = 8

    body {
      attribute "x" {
        type = number
        min  = 0
      }
    }
  }
}
```

An instance points at its schema and is checked against it:

```hcl
__schema = "./example.schema.hcl"

myattr = "service"

tag "name" {
  x = 2
}

tag "name2" {}
```

```console
$ hclschema validate .
```

## Why it is shaped this way

The three core declarations mirror HCL's own types:

| hcl-schema | HCL |
| --- | --- |
| `body` | `hcl.BodySchema` |
| `attribute` | `hcl.AttributeSchema` |
| `block_header` | `hcl.BlockHeaderSchema` |

Those types describe only the *shallow* shape of a body — they are the input to
`Body.Content()`. Types, defaults and cardinality live one layer up in HCL, in
`hcldec.Spec`, and draft 2026-09 mirrors that layer too:

| hcl-schema | HCL |
| --- | --- |
| `type` | `hcldec.AttrSpec.Type` |
| `default` | `hcldec.DefaultSpec` |
| `min_items` / `max_items` | `hcldec.BlockListSpec` |
| `required` on a block | `hcldec.BlockSpec.Required` |
| `label` | `hcldec.BlockLabelSpec` |
| `validation` | `hcldec.ValidateSpec` |
| `open` | `hcldec.BlockAttrsSpec` |

`enum`, `pattern`, `min`, `max`, `deprecated`, `sensitive` and the cross-field
constraints have no HCL counterpart; they are hcl-schema's own, as `id` and
`ref` always were.

## Drafts

A schema pins its meta-schema through `__schema`, and the `draft/<id>/` segment
of that reference selects the draft.

- **[2025-10](./schema/draft/2025-10/.schema.hcl)** — the original. `attribute`
  takes only `required`; `block_header` takes only `label_names`, `id` and
  `ref`.
- **[2026-09](./schema/draft/2026-09/.schema.hcl)** — adds the type system,
  cardinality, labels, cross-field constraints, imports and variants, and
  tightens two rules:
  - a `block_header` that declares no `body` must be empty, where before its
    contents went entirely unchecked;
  - a name may be declared only once per body, where before the first
    declaration silently won.

Existing documents keep working: every key added after 2025-10 is optional, and
the tightened rules apply only from 2026-09. `--strict` opts an older document
into the new rules without editing it.

## CLI

```
hclschema validate [files or directories...]   Check files against their schemas
hclschema fmt [-w] [--check] [files...]        Rewrite schemas in canonical form
hclschema infer <files...>                     Derive a starting schema
hclschema docs <schema.hcl>                    Render Markdown reference docs
hclschema bundle <schema.hcl>                  Inline imports into one document
hclschema lsp                                  Run the language server
hclschema version
```

`validate` exits `1` when it reports an error and `2` on a usage problem, so it
works in CI directly. `--format` selects `text` (default, with source
snippets), `json`, `github` (inline PR annotations) or `sarif` (code scanning).
`--offline` forbids network access; `--stdin-filename` checks a buffer instead
of a file.

```yaml
- run: hclschema validate --format github --offline .
```

## Go library

```go
schema, diags := hclschema.ParseSchemaFile("example.schema.hcl")
if diags.HasErrors() { /* ... */ }

res := hclschema.CheckFile("config.hcl", hclschema.LoadOptions{})
if res.HasErrors() { /* ... */ }

// Decode rather than only validate.
var cfg struct {
    MyAttr string `json:"myattr"`
}
diags = schema.DecodeInto(res.File.Body, nil, &cfg)
```

The main entry points are `ParseSchema`, `ParseSchemaFile`, `ParseSchemaFS`,
`Check`, `CheckFile`, `Schema.Validate`, `Schema.Decode`, `Schema.DecodeSpec`,
`Schema.Markdown`, `Schema.Bytes`, `Schema.Bundle`, `Infer` and `Schema.At`
(the basis for completion and hover).

Remote schemas are fetched over https only, capped in size, cached per user
with owner-only permissions, revalidated with `ETag`, and refused if a redirect
would downgrade the connection. `__schema_sha256` pins a schema to an exact
digest.

> **Go API note.** `ParseSchemaFile` now returns `*Schema` rather than
> `*BlockHeaderAndBodySchema`. Call `Schema.Legacy()` for the previous shape.
> `ValidateFileWithSchema` and `ValidateHCLWithLinkedSchema` are unchanged.

## VS Code

Install from the
[marketplace](https://marketplace.visualstudio.com/items?itemName=avestura.hcl-schema).

The extension runs `hclschema lsp` and provides diagnostics for the *unsaved*
buffer, completion of declared attributes and blocks, hover documentation from
`description`, go-to-definition into the schema, and a document outline.

![VS Code screenshot](./vsx/assets/screenshot/vscode-scrshot.png)

## Development

```console
$ go test ./...
$ cd vsx && yarn install && yarn compile
$ cd vsx && yarn package        # one VSIX for this platform
$ cd vsx && yarn package:all    # one VSIX per platform
```

The documentation site lives in [`website/`](./website).

## License

MIT. See [LICENSE](./LICENSE).
