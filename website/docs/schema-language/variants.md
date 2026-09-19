---
id: variants
title: Variants
sidebar_position: 4
---

# Variants

A `one_of` block declares alternatives. The enclosing body must match exactly
one of them, in addition to whatever the body declares directly.

```hcl
body {
  attribute "name" {
    type     = string
    required = true
  }

  one_of {
    variant "inline" {
      body {
        attribute "content" {
          type     = string
          required = true
        }
      }
    }

    variant "external" {
      body {
        attribute "url" {
          type     = string
          required = true
        }

        attribute "checksum" {
          type = string
        }
      }
    }
  }
}
```

Both of these are accepted:

```hcl
name    = "greeting"
content = "hello"
```

```hcl
name     = "greeting"
url      = "https://example.com/greeting.txt"
checksum = "9f2c..."
```

This is not:

```hcl
name = "greeting"
```

```
Error: No matching variant

This body must match one of "inline", "external". The closest is "inline";
its errors follow.

Error: Missing required argument

The argument "content" is required, but no definition was found.
```

## How matching works

Each variant is checked against the body, merged with the declarations the body
makes outside the `one_of`. A variant may narrow an outer declaration by
redeclaring it under the same name.

- Exactly one variant matches: its result is the result.
- No variant matches: the one with the fewest errors is reported, along with a
  summary naming all of them. Reporting every variant's errors would bury the
  one you care about.
- More than one matches: that is an error too. `one_of` means one, and an
  ambiguous document usually means the variants are not actually distinct.

## Limitations

A variant constrains validation but has no single decoded shape, so
`Schema.DecodeSpec` omits variant-only declarations and says so with a warning.
If you need the decoded value, give each alternative its own block instead and
use `max_items` to keep them exclusive.
