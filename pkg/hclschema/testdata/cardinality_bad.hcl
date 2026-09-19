__schema = "cardinality.schema.hcl"

listener "http" {
  port = 80
}

listener "http" {
  port = 8080
}

listener "BAD" {
  port = 1
}
