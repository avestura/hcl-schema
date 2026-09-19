---
id: attributes
title: Attributes
sidebar_position: 1
---

# Attributes

An `attribute` block declares one argument of a body. The label is the
attribute's name.

```hcl
attribute "port" {
  type        = number
  required    = true
  description = "The port to listen on."
  min         = 1
  max         = 65535
}
```

## `type`

`type` holds a **type constraint expression**, not a string. It is read by
HCL's own [`ext/typeexpr`](https://pkg.go.dev/github.com/hashicorp/hcl/v2/ext/typeexpr),
so the whole vocabulary you know from Terraform variables is available:

```hcl
attribute "name"     { type = string }
attribute "count"    { type = number }
attribute "enabled"  { type = bool }
attribute "tags"     { type = list(string) }
attribute "labels"   { type = map(string) }
attribute "ports"    { type = set(number) }
attribute "pair"     { type = tuple([string, number]) }
attribute "anything" { type = any }

attribute "retry" {
  type = object({
    attempts = number
    backoff  = optional(string)
  })
}
```

Omitting `type` leaves the attribute untyped, which is how every attribute
behaved before draft 2026-09.

:::note Expressions the validator cannot evaluate
A value that references variables or calls functions is left unchecked rather
than reported as an error, because only the application embedding hcl-schema
can supply that context. Pass an `hcl.EvalContext` through
`ValidateOptions.EvalContext` to have those checked too.
:::

## `required` and `default`

```hcl
attribute "name" {
  type     = string
  required = true
}

attribute "mode" {
  type    = string
  default = "safe"
}
```

A `default` on a `required` attribute is an error: it could never apply.
Defaults are substituted during [decoding](../go-library.md#decoding), not
during validation.

## `description` and `deprecated`

```hcl
attribute "timeout" {
  type        = number
  description = "Seconds to wait before giving up."
}

attribute "timeout_ms" {
  type       = number
  deprecated = "use timeout instead"
}
```

`description` is the single source for editor hover text and for
`hclschema docs`. Using a `deprecated` attribute produces a warning, never an
error, so a deprecation never breaks a build on its own.

## `sensitive`

```hcl
attribute "token" {
  type      = string
  sensitive = true
  pattern   = "^tok_"
}
```

A sensitive value is replaced with `(sensitive value)` in diagnostics.
Diagnostics routinely end up in CI logs, so a failing pattern check on a
credential should not print the credential.

## Value constraints

### `enum`

```hcl
attribute "mode" {
  type = string
  enum = ["fast", "safe"]
}
```

The permitted values also become the completion list in the editor when the
cursor is on that attribute.

### `pattern`

A Go regular expression the value must match. String values only.

```hcl
attribute "name" {
  type    = string
  pattern = "^[a-z][a-z0-9-]*$"
}
```

An invalid pattern is reported when the schema is parsed, not when a file
happens to trip over it.

### `min` and `max`

What they bound depends on the value:

| Value | Bounded |
| --- | --- |
| number | its magnitude |
| string | its length in characters |
| list, set, map, tuple, object | its element count |

```hcl
attribute "replicas" {
  type = number
  min  = 1
  max  = 9
}

attribute "name" {
  type = string
  min  = 3
  max  = 63
}
```

### `validation`

For anything the fixed constraints cannot express. `self` is bound to the
attribute's value.

```hcl
attribute "replicas" {
  type = number

  validation {
    condition     = self % 2 == 1
    error_message = "replicas must be odd so that quorum is well defined"
  }
}
```

The condition is an ordinary HCL expression and must produce a boolean. This
mirrors [`hcldec.ValidateSpec`](https://pkg.go.dev/github.com/hashicorp/hcl/v2/hcldec#ValidateSpec).

## Cross-field constraints

These describe relationships between sibling attributes. The declaring
attribute is always part of its own group, so you name only the others.

| Key | Meaning |
| --- | --- |
| `conflicts_with` | Must not be set alongside the named siblings. |
| `required_with` | Setting this requires the named siblings to be set. |
| `exactly_one_of` | Exactly one of this attribute and the named siblings must be set. |
| `at_least_one_of` | At least one of this attribute and the named siblings must be set. |

```hcl
attribute "inline" {
  type           = string
  conflicts_with = ["from_file"]
  exactly_one_of = ["from_file"]
}

attribute "from_file" {
  type = string
}

attribute "checksum" {
  type          = string
  required_with = ["from_file"]
}
```

A group is reported once, not once per member, so declaring
`exactly_one_of` on both sides produces one diagnostic rather than two.
