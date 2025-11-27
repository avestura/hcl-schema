schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/master/schema/draft/2025-10/.schema.hcl"
id     = "local://nested"

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
