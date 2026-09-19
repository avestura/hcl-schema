---
id: reuse
title: Reuse and imports
sidebar_position: 3
---

# Reuse and imports

## `id` and `ref`

`id` registers a block's body under a name. `ref` reuses it elsewhere.

```hcl
body {
  block_header "primary" {
    id = "endpointBody"

    body {
      attribute "url" {
        type     = string
        required = true
      }

      attribute "timeout" {
        type = number
      }
    }
  }

  block_header "fallback" {
    ref = block_header.endpointBody
  }
}
```

`fallback` now accepts exactly what `primary` does. The two share one body
rather than a copy, so editing the definition changes both.

A `ref` may point at an id declared **later** in the document. Ids are
collected in a first pass and every reference is resolved in a second, so
declaration order does not matter.

:::info Fixed in draft 2026-09
Forward references used to fail, because ids were registered as the parser
walked the document and a reference was resolved the moment it was seen.
:::

## Recursion

A body may refer to itself, which is how a schema describes an arbitrarily
nested structure. This is exactly how the meta-schema describes `body`:

```hcl
block_header "body" {
  id = "bodyRef"

  body {
    block_header "block_header" {
      label_names = ["block_header_type"]

      body {
        block_header "body" {
          ref = block_header.bodyRef
        }
      }
    }
  }
}
```

The id is registered before its own body is parsed, so a body can reach itself.

:::note What recursion costs you
Validation handles recursion fine — it walks a finite document. Decoding does
not: an `hcldec.Spec` is a finite tree. `Schema.DecodeSpec` cuts the recursion
where it closes and reports a warning. Documentation generation links back to
the already-documented body rather than looping.
:::

## Imports

An `import` block makes another schema's ids available under an alias.

```hcl title="common.schema.hcl"
__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "https://example.com/common.schema.hcl"

body {
  block_header "retry" {
    id = "retryBody"

    body {
      attribute "attempts" {
        type     = number
        required = true
      }
    }
  }
}
```

```hcl title="service.schema.hcl"
import "common" {
  source = "./common.schema.hcl"
}

body {
  block_header "http" {
    body {
      block_header "retry" {
        ref = common.block_header.retryBody
      }
    }
  }
}
```

`source` takes a path relative to the importing document, or an `https` URL.
`sha256` pins it to an exact digest:

```hcl
import "common" {
  source = "https://example.com/common.schema.hcl"
  sha256 = "9f2c...64 hex characters..."
}
```

Import cycles and chains deeper than `MaxDepth` (16 by default) are reported
rather than followed.

## Bundling

Because `ref` resolution replaces names with pointers, a parsed schema already
holds everything its imports contributed. `bundle` writes that back out as one
self-contained document:

```bash
hclschema bundle service.schema.hcl -o vendor/service.schema.hcl
```

Shared bodies — including recursive ones — get a generated `id` and are emitted
once, so the bundle validates identically to the original and needs no network
access. That is what makes a schema vendorable and an offline CI job possible.
