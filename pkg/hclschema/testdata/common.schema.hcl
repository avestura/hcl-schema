__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://common"

body {
  block_header "retry" {
    id = "retryBody"

    body {
      attribute "attempts" {
        type     = number
        required = true
      }
    }
  }
}
