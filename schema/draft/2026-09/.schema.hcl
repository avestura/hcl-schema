// The hcl-schema meta-schema, draft 2026-09.
//
// This document describes the shape of a `*.schema.hcl` file, including
// itself. Compared with draft 2025-10 it adds the type system, cardinality,
// per-label constraints, cross-field constraints, imports and variants, and it
// tightens two structural rules: a `block_header` that declares no `body` must
// be empty, and a name may be declared only once per body.

__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"

body {
  attribute "__schema" {
    type        = string
    required    = true
    description = "The meta-schema this document is written against. Its draft/<id>/ path segment selects the draft."
  }

  attribute "__id" {
    type        = string
    required    = true
    description = "A stable identity for this document."
  }

  attribute "__schema_sha256" {
    type        = string
    pattern     = "^[0-9a-fA-F]{64}$"
    description = "Pins the meta-schema to an exact digest."
  }

  block_header "import" {
    label_names   = ["alias"]
    unique_labels = true
    description   = "Makes another schema's ids available under an alias, as alias.block_header.<id>."

    body {
      attribute "source" {
        type        = string
        required    = true
        description = "A path relative to this document, or an https URL."
      }

      attribute "sha256" {
        type        = string
        pattern     = "^[0-9a-fA-F]{64}$"
        description = "Pins the imported document to an exact digest."
      }
    }
  }

  block_header "body" {
    id          = "bodyRef"
    max_items   = 1
    description = "The root body every instance of this schema must match."

    body {
      attribute "open" {
        type        = bool
        description = "Permit attributes and blocks this body does not declare."
      }

      block_header "attribute" {
        label_names   = ["attribute_name"]
        unique_labels = true
        description   = "Declares an attribute. Mirrors hcl.AttributeSchema, extended with hcldec.AttrSpec's type and default."

        body {
          attribute "required" {
            type        = bool
            description = "The attribute must be present."
          }

          // `type` holds a type constraint expression such as string or
          // object({ a = string }), so it is deliberately left untyped here:
          // its value is an expression to be read by ext/typeexpr, not a value
          // to be evaluated.
          attribute "type" {
            description = "A type constraint expression, as understood by ext/typeexpr."
          }

          attribute "default" {
            description = "Substituted when the attribute is absent. Only meaningful when it is optional."
          }

          attribute "description" {
            type        = string
            description = "Documentation, surfaced as editor hover text and in generated reference docs."
          }

          attribute "deprecated" {
            type        = string
            description = "Marks the attribute deprecated, with the reason. Using it produces a warning."
          }

          attribute "sensitive" {
            type        = bool
            description = "Keeps the value out of diagnostics."
          }

          attribute "enum" {
            description = "The permitted values."
          }

          attribute "pattern" {
            type        = string
            description = "A regular expression a string value must match."
          }

          attribute "min" {
            type        = number
            description = "Lower bound on a number's value, a string's length, or a collection's element count."
          }

          attribute "max" {
            type        = number
            description = "Upper bound on a number's value, a string's length, or a collection's element count."
          }

          attribute "conflicts_with" {
            type        = list(string)
            description = "Sibling attributes that must not be set alongside this one."
          }

          attribute "required_with" {
            type        = list(string)
            description = "Sibling attributes that must be set when this one is."
          }

          attribute "exactly_one_of" {
            type        = list(string)
            description = "This attribute and the named siblings form a group of which exactly one must be set."
          }

          attribute "at_least_one_of" {
            type        = list(string)
            description = "This attribute and the named siblings form a group of which at least one must be set."
          }

          block_header "validation" {
            description = "A custom predicate over the attribute's value."

            body {
              attribute "condition" {
                required    = true
                description = "A boolean expression with `self` bound to the value."
              }

              attribute "error_message" {
                type        = string
                required    = true
                description = "Reported when the condition is false."
              }
            }
          }
        }
      }

      block_header "block_header" {
        label_names   = ["block_header_type"]
        unique_labels = true
        description   = "Declares a block. Mirrors hcl.BlockHeaderSchema, extended with hcldec's cardinality."

        body {
          attribute "label_names" {
            type        = list(string)
            description = "Names for the block's positional labels. Their count is the block's arity."
          }

          attribute "ref" {
            description = "Reuses the body registered under another declaration's id, as block_header.<id>."
          }

          attribute "id" {
            description = "Registers this block's body so that a ref elsewhere can reuse it."
          }

          attribute "description" {
            type = string
          }

          attribute "deprecated" {
            type        = string
            description = "Marks the block deprecated, with the reason."
          }

          attribute "required" {
            type        = bool
            description = "Shorthand for min_items = 1."
          }

          attribute "min_items" {
            type        = number
            min         = 0
            description = "Fewest instances of this block permitted in its parent body."
          }

          attribute "max_items" {
            type        = number
            min         = 0
            description = "Most instances permitted. Zero means unbounded."
          }

          attribute "unique_labels" {
            type        = bool
            description = "Sibling blocks of this type must not repeat a label tuple."
          }

          attribute "open" {
            type        = bool
            description = "Permit content this block's body does not declare."
          }

          block_header "label" {
            label_names   = ["label_name"]
            unique_labels = true
            description   = "Constrains one positional label. Mirrors hcldec.BlockLabelSpec, extended with a pattern and an enum."

            body {
              attribute "description" {
                type = string
              }

              attribute "pattern" {
                type        = string
                description = "A regular expression the label must match."
              }

              attribute "enum" {
                type        = list(string)
                description = "The permitted labels."
              }
            }
          }

          block_header "body" {
            ref = block_header.bodyRef
          }
        }
      }

      block_header "one_of" {
        description = "A group of alternatives, exactly one of which the enclosing body must match."

        body {
          block_header "variant" {
            label_names   = ["variant_name"]
            unique_labels = true
            min_items     = 2

            body {
              block_header "body" {
                ref = block_header.bodyRef
              }
            }
          }
        }
      }
    }
  }
}
