__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://variant"

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
      }
    }
  }
}
