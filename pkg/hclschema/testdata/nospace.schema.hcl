__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://nospace"

body {
  block_header "foo" {
    id="fooRef"

    body {
      attribute "a" {
        required=true
      }
    }
  }

  block_header "bar" {
    ref=block_header.fooRef
  }
}
