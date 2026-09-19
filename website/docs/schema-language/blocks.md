---
id: blocks
title: Blocks
sidebar_position: 2
---

# Blocks

A `block_header` block declares one block type. The label is the block's type
name; `label_names` gives names to its positional labels, and their count is
the block's arity.

```hcl
block_header "listener" {
  label_names = ["name"]

  body {
    attribute "port" {
      type     = number
      required = true
    }
  }
}
```

That accepts:

```hcl
listener "https" {
  port = 443
}
```

## Nested bodies

A block's `body` is an ordinary body, so it can declare attributes and further
blocks to any depth. This recursion is the one place hcl-schema goes beyond
`hcl.BodySchema`, which is deliberately shallow because it is the input to a
single `Body.Content()` call.

:::warning A block with no `body`
From draft 2026-09, a `block_header` that declares no `body` must be **empty**.

Before 2026-09 the same declaration accepted *arbitrary* content, silently:

```hcl
block_header "opaque" {
  label_names = ["n"]
}
```

```hcl
opaque "z" {
  anything = true          # accepted under 2025-10, rejected from 2026-09
  nested "deep" {}
}
```

If you want a block that really does accept anything, say so with
[`open`](#open).
:::

## Cardinality

```hcl
block_header "listener" {
  label_names = ["name"]
  min_items   = 1
  max_items   = 4
}
```

| Key | Meaning |
| --- | --- |
| `min_items` | Fewest instances permitted in the parent body. |
| `max_items` | Most instances permitted. `0` means unbounded. |
| `required` | Shorthand for `min_items = 1`. |

These mirror
[`hcldec.BlockListSpec`](https://pkg.go.dev/github.com/hashicorp/hcl/v2/hcldec#BlockListSpec).
`max_items = 1` also changes how the block decodes: to a single object rather
than to a list.

## Labels

`unique_labels` forbids two sibling blocks of the same type sharing a label
tuple:

```hcl
block_header "listener" {
  label_names   = ["name"]
  unique_labels = true
}
```

A `label` block constrains one positional label:

```hcl
block_header "route" {
  label_names = ["method", "path"]

  label "method" {
    enum        = ["GET", "POST", "PUT", "DELETE"]
    description = "The HTTP method."
  }

  label "path" {
    pattern = "^/"
  }
}
```

When `label` blocks are present without `label_names`, their labels supply the
names. When both are given they must agree in count.

## `open`

An open body permits attributes and blocks it does not declare. Anything
declared is still checked; anything else is left alone.

```hcl
block_header "annotations" {
  open = true

  body {
    attribute "managed_by" {
      type = string
    }
  }
}
```

```hcl
annotations {
  managed_by = "platform"
  anything   = "else"   # allowed
}
```

`open` can also appear directly on a `body`:

```hcl
body {
  open = true

  attribute "name" {
    type = string
  }
}
```

:::note `open` and `ref` do not combine
A body reached through [`ref`](./reuse.md) is shared with every other use of
that id, so opening it in one place would silently open it everywhere. Declare
`open` where the body is defined instead. Combining them is an error.
:::

## Deprecating a block

```hcl
block_header "legacy_listener" {
  label_names = ["name"]
  deprecated  = "use listener instead"
}
```

Like a deprecated attribute, this produces a warning.

## One declaration per type

A block type may be declared only once per body.

`hcl.BodySchema` keys its blocks by type name alone — hclsyntax builds a
`map[string]BlockHeaderSchema` — so the same type at two label arities is not
something HCL can enforce. Allowing it in the schema language would mean
advertising a rule the parser cannot apply. Under draft 2025-10 a repeat is a
warning and the first declaration wins; from 2026-09 it is an error.
