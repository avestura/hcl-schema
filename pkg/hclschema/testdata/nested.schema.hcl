__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id     = "local://nested"

body {
    attribute "a" {
        required = false
    }

    block_header "outer" {
        label_names = ["o"]

        body {
            attribute "inner_attr" {}

            block_header "inner" {
                label_names = ["i"]
            }
        }
    }
}
