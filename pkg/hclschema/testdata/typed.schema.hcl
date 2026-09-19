__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2026-09/.schema.hcl"
__id     = "local://typed"

body {
  attribute "name" {
    type     = string
    required = true
    pattern  = "^[a-z][a-z0-9-]*$"
  }

  attribute "port" {
    type    = number
    default = 8080
    min     = 1
    max     = 65535
  }

  attribute "mode" {
    type = string
    enum = ["fast", "safe"]
  }

  attribute "debug" {
    type = bool
  }

  attribute "tags" {
    type = list(string)
  }

  attribute "replicas" {
    type = number
    validation {
      condition     = self % 2 == 1
      error_message = "replicas must be odd"
    }
  }
}
