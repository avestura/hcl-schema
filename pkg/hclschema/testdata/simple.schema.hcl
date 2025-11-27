__schema = "https://raw.githubusercontent.com/avestura/hcl-schema/refs/heads/main/schema/draft/2025-10/.schema.hcl"
__id     = "local://simple"

body {
    attribute "myattr" {
        required = true
    }

    block_header "tag" {
        label_names = ["name"]

        body {
            attribute "x" {}
        }
    }
}
