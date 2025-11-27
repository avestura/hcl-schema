__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id = "local://ref_id_body"

body {
    block_header "foo" {
        label_names = ["a", "b"]

        body {
            attribute "something" {}
        }
    }
    block_header "bar" {
        ref = block_header.x
    }
}