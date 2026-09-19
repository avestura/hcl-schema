__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://forward"

body {
  block_header "bar" {
    ref = block_header.fooBody
  }

  block_header "foo" {
    id = "fooBody"

    body {
      attribute "a" {
        type     = string
        required = true
      }
    }
  }
}
