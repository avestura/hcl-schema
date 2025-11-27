__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id = "local://ref_id_body"

body {
    block_header "foo" {
        id = "foo"
        label_names = ["a", "b"]

        body {
            attribute "something" {
                required = true
            }
        }
    }
    block_header "bar" {
        ref = block_header.foo
    }
}