__schema = "nested.schema.hcl"

a = false

outer "o1" {
  inner_attr = "x"

  inner "i1" {}

  extra = "value"
}
