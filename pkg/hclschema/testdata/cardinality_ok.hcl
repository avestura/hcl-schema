__schema = "cardinality.schema.hcl"

listener "http" {
  port = 80
}

listener "https" {
  port = 443
}
