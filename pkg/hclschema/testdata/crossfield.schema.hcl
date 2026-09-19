__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://crossfield"

body {
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
}
