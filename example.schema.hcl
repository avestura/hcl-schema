__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/example.schema.hcl"

body {
  attribute "myattr" {
    type        = string
    required    = true
    description = "The name of the thing this file configures."
    pattern     = "^[a-z][a-z0-9-]*$"
  }

  attribute "mode" {
    type        = string
    default     = "safe"
    enum        = ["fast", "safe"]
    description = "How aggressively to run."
  }

  block_header "tag" {
    label_names   = ["name1"]
    unique_labels = true
    max_items     = 8
    description   = "A named tag."

    label "name1" {
      pattern = "^[a-z0-9_]+$"
    }

    body {
      attribute "x" {
        type = number
        min  = 0
      }
    }
  }
}

// Accepts an HCL file like this:
//
//   myattr = "service"
//   mode   = "fast"
//
//   tag "name" {
//     x = 2
//   }
//
//   tag "name2" {}
//
// Everything beyond `required` and `label_names` is new in draft 2026-09. A
// schema that pins draft 2025-10 keeps its original meaning; see
// schema/draft/2025-10/.schema.hcl.
