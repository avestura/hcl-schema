---
id: getting-started
title: Getting started
sidebar_position: 2
---

# Getting started

## Install

### Go

```bash
go install github.com/avestura/hcl-schema/cmd/hclschema-cli@latest
```

The binary is named `hclschema-cli`. The rest of these docs call it
`hclschema`; rename it or add an alias if you want the shorter name.

### VS Code

Install
[avestura.hcl-schema](https://marketplace.visualstudio.com/items?itemName=avestura.hcl-schema)
from the marketplace. It bundles the binary for your platform, so there is
nothing else to install.

### From source

```bash
git clone https://github.com/avestura/hcl-schema
cd hcl-schema
go build ./cmd/hclschema-cli
```

## Start from files you already have

If you already have HCL files, do not start from a blank page:

```bash
hclschema infer --id "https://example.com/service.schema.hcl" \
  config/*.hcl > service.schema.hcl
```

Inference describes what the samples happen to contain, so read the result
before committing it. It will not guess a type where the samples disagree, and
it marks nothing required unless you pass `--required-when-ubiquitous`.

## Link a file to its schema

Add a `__schema` attribute to each instance. It takes a path relative to the
file, or an `https` URL:

```hcl
__schema = "./service.schema.hcl"
```

To pin a remote schema to an exact revision, add its digest:

```hcl
__schema        = "https://example.com/service.schema.hcl"
__schema_sha256 = "9f2c...64 hex characters..."
```

## Validate

```console
$ hclschema validate .
```

With no arguments the current directory is walked. `validate` exits `1` when it
reports an error, so it drops straight into a build script.

```console
$ hclschema validate config/service.hcl
Error: Value above maximum

  on config/service.hcl line 6, in listener "https":
   6:   port = 99999

"port" has 99999, above the maximum of 65535.
```

## Generate documentation

```bash
hclschema docs service.schema.hcl -o docs/service.md
```

Every `description` you wrote in the schema becomes a row in the generated
table and hover text in the editor, so the two cannot drift apart.

## Decode, do not just validate

The same schema can produce values:

```go
res := hclschema.CheckFile("service.hcl", hclschema.LoadOptions{})
if res.HasErrors() {
    return res.Diagnostics
}

var cfg struct {
    Name     string `json:"name"`
    Listener []struct {
        Name string  `json:"name"`
        Port float64 `json:"port"`
    } `json:"listener"`
}
if diags := res.Schema.DecodeInto(res.File.Body, nil, &cfg); diags.HasErrors() {
    return diags
}
```

See [the Go library](./go-library.md) for the full surface.

## Next

- [Attributes](./schema-language/attributes.md) — types, defaults, constraints
- [Blocks](./schema-language/blocks.md) — labels and cardinality
- [CI](./ci.md) — annotations and code scanning
