__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://strict"

body {
  block_header "opaque" {
    label_names = ["n"]
  }

  block_header "loose" {
    open = true

    body {
      attribute "known" {
        type = string
      }
    }
  }
}
