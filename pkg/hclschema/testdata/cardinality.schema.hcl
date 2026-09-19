__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://cardinality"

body {
  block_header "listener" {
    label_names   = ["name"]
    min_items     = 1
    max_items     = 2
    unique_labels = true

    label "name" {
      pattern = "^[a-z]+$"
    }

    body {
      attribute "port" {
        type     = number
        required = true
      }
    }
  }
}
