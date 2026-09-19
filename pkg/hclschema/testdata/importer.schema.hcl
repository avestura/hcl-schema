__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://importer"

import "common" {
  source = "./common.schema.hcl"
}

body {
  block_header "http" {
    body {
      block_header "retry" {
        ref = common.block_header.retryBody
      }
    }
  }
}
